package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "shpyrd/api/v1alpha1"
)

// fakeSeries is one Prometheus result series.
type fakeSeries struct {
	Labels map[string]string
	Values []Point
}

// fakeAnswer replies to any query containing Match. Tests name a fragment of
// the PromQL rather than reproducing it, so a query can be reworded without
// rewriting every expectation.
type fakeAnswer struct {
	Match  string
	Series []fakeSeries
}

// fakeRecorder keeps the queries the server actually sent, which is the real
// subject of most of these tests.
type fakeRecorder struct {
	mu      sync.Mutex
	queries []string
}

func (r *fakeRecorder) add(q string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queries = append(r.queries, q)
}

func (r *fakeRecorder) Queries() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.queries...)
}

// Queried reports whether any query contained sub.
func (r *fakeRecorder) Queried(sub string) bool {
	for _, q := range r.Queries() {
		if strings.Contains(q, sub) {
			return true
		}
	}
	return false
}

func newFakeProm(t *testing.T, answers ...fakeAnswer) (*PromClient, *fakeRecorder) {
	t.Helper()
	rec := &fakeRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		rec.add(q)
		var result []map[string]any
		for _, a := range answers {
			if a.Match == "" || !strings.Contains(q, a.Match) {
				continue
			}
			for _, s := range a.Series {
				vals := make([][2]any, 0, len(s.Values))
				for _, p := range s.Values {
					// Prometheus sends the value as a string.
					vals = append(vals, [2]any{p[0], formatFloat(p[1])})
				}
				result = append(result, map[string]any{"metric": s.Labels, "values": vals})
			}
			break
		}
		if result == nil {
			result = []map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data":   map[string]any{"resultType": "matrix", "result": result},
		})
	}))
	t.Cleanup(srv.Close)
	return &PromClient{BaseURL: srv.URL, HTTP: srv.Client()}, rec
}

func formatFloat(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

// metricsApp is a project with two process types, which is what makes the
// process filter and per-instance grouping meaningful.
func metricsApp() *shpyrdv1.App {
	app := sampleApp("shop", "Running")
	app.Spec.Processes = map[string]shpyrdv1.Process{
		"web":    {Size: "shared-s"},
		"worker": {Size: "shared-s"},
	}
	app.Annotations = map[string]string{}
	return app
}

func getMetrics(t *testing.T, s *Server, query string) MetricsResponse {
	t.Helper()
	rec := do(t, s, "GET", "/api/projects/shop/metrics"+query, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics%s: %d %s", query, rec.Code, rec.Body.String())
	}
	var out MetricsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Review Focus 1: a project Prometheus knows nothing about must render as
// empty charts, not as an error and not as a missing chart.
func TestMetricsWithNoDataReturnsEmptyCharts(t *testing.T) {
	prom, _ := newFakeProm(t) // every query answers with no series
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app})

	out := getMetrics(t, s, "?range=1h")
	if len(out.Charts) == 0 {
		t.Fatal("no charts returned")
	}
	for _, ch := range out.Charts {
		if ch.Error != "" {
			t.Errorf("chart %q reported an error with no data: %s", ch.ID, ch.Error)
		}
		if ch.Series == nil {
			t.Errorf("chart %q has nil series; it must marshal as [] not null", ch.ID)
		}
	}
	want := []string{"throughput", "latency", "instances", "cpu", "memory", "network"}
	got := make([]string, 0, len(out.Charts))
	for _, ch := range out.Charts {
		got = append(got, ch.ID)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("chart set = %v, want %v", got, want)
	}
}

// The default request must keep behaving exactly as it does today: series
// named after the process, not the pod.
func TestMetricsByProcessIsTheDefault(t *testing.T) {
	prom, rec := newFakeProm(t, fakeAnswer{
		Match: "container_memory_working_set_bytes",
		Series: []fakeSeries{
			{Labels: map[string]string{"label_shpyrd_io_process": "web"}, Values: []Point{{1000, 40}}},
			{Labels: map[string]string{"label_shpyrd_io_process": "worker"}, Values: []Point{{1000, 10}}},
		},
	})
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app})

	out := getMetrics(t, s, "?range=1h")
	mem := chartByID(t, out, "memory")
	if len(mem.Series) != 2 || mem.Series[0].Name != "web" || mem.Series[1].Name != "worker" {
		t.Fatalf("memory series = %+v, want web and worker", mem.Series)
	}
	// Grouping by pod is what the instance mode adds; the default must not do it.
	if rec.Queried("by (pod") {
		t.Error("the default request grouped by pod")
	}
	// Task 9 touched the instances query and the network series names with no
	// process selected; a regression in either would have slipped past this
	// test if it only ever looked at memory.
	if rec.Queried(`deployment="`) {
		t.Error("the default instances query must not filter by deployment")
	}
	if net := chartByID(t, out, "network"); strings.Join(seriesNames(net), ",") != "in,out" {
		t.Errorf("default network series = %v, want in,out unchanged", seriesNames(net))
	}
}

func chartByID(t *testing.T, out MetricsResponse, id string) Chart {
	t.Helper()
	for _, ch := range out.Charts {
		if ch.ID == id {
			return ch
		}
	}
	t.Fatalf("no chart %q in %+v", id, out.Charts)
	return Chart{}
}

func TestMetricsRanges(t *testing.T) {
	prom, _ := newFakeProm(t)
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app})

	// Every documented range is accepted, and the step keeps the number of
	// points per series in a range a chart can actually draw.
	for _, tc := range []struct {
		rng      string
		wantStep int
	}{
		{"15m", 60},
		{"1h", 60},
		{"6h", 360},
		{"24h", 1440},
		{"7d", 10080},
	} {
		out := getMetrics(t, s, "?range="+tc.rng)
		if out.Range != tc.rng {
			t.Errorf("range echoed as %q, want %q", out.Range, tc.rng)
		}
		if out.Step != tc.wantStep {
			t.Errorf("range %s step = %d, want %d", tc.rng, out.Step, tc.wantStep)
		}
	}
	rec := do(t, s, "GET", "/api/projects/shop/metrics?range=30s", "", true)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("range=30s: %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "15m") {
		t.Error("the 400 should name the ranges that are allowed, including 15m")
	}
}

func TestMetricsTotalModeCarriesTheAllocation(t *testing.T) {
	prom, rec := newFakeProm(t, fakeAnswer{
		Match: "container_memory_working_set_bytes",
		Series: []fakeSeries{
			{Labels: map[string]string{"label_shpyrd_io_process": "web"}, Values: []Point{{1000, 33554432}}},
		},
	})
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app})

	out := getMetrics(t, s, "?range=1h&mode=total")
	mem := chartByID(t, out, "memory")
	if mem.Unit != "bytes" {
		t.Errorf("memory unit in total mode = %q, want bytes", mem.Unit)
	}
	if len(mem.Series) != 1 {
		t.Fatalf("series = %+v", mem.Series)
	}
	// shared-s is 64Mi, so the line the UI draws is 67108864 bytes.
	if mem.Series[0].Reference != 67108864 {
		t.Errorf("reference = %v, want 67108864 (64Mi for shared-s)", mem.Series[0].Reference)
	}
	// Absolute mode must not divide by the request.
	if rec.Queried("kube_pod_container_resource_requests") {
		t.Error("total mode divided by the request")
	}

	// Percentage stays the default and keeps dividing by the request.
	out = getMetrics(t, s, "?range=1h")
	mem = chartByID(t, out, "memory")
	if mem.Unit != "%" {
		t.Errorf("default memory unit = %q, want %%", mem.Unit)
	}
	if mem.Series[0].Reference != 0 {
		t.Error("percentage mode needs no reference; 100% is the line")
	}
}

func TestMetricsTotalModeIgnoredOnAbsoluteCharts(t *testing.T) {
	prom, _ := newFakeProm(t, fakeAnswer{
		Match:  "nginx_ingress_controller_requests",
		Series: []fakeSeries{{Labels: map[string]string{"class": "2xx"}, Values: []Point{{1000, 5}}}},
	})
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app})

	out := getMetrics(t, s, "?range=1h&mode=total")
	tp := chartByID(t, out, "throughput")
	if tp.Unit != "rps" {
		t.Errorf("throughput unit = %q; mode must not touch charts that are always absolute", tp.Unit)
	}
	for _, sr := range tp.Series {
		if sr.Reference != 0 {
			t.Errorf("throughput series %q got a reference; it has no allocation", sr.Name)
		}
	}
}

// Review Focus 2: the size was renamed or removed from the catalog after the
// project was deployed. The series must still render; only the line is lost.
func TestMetricsUnknownSizeDropsOnlyTheReference(t *testing.T) {
	prom, _ := newFakeProm(t, fakeAnswer{
		Match: "container_memory_working_set_bytes",
		Series: []fakeSeries{
			{Labels: map[string]string{"label_shpyrd_io_process": "web"}, Values: []Point{{1000, 1024}}},
		},
	})
	app := metricsApp()
	app.Spec.Processes["web"] = shpyrdv1.Process{Size: "size-that-was-deleted"}
	s, _ := newTestServer(t, prom, []client.Object{app})

	out := getMetrics(t, s, "?range=1h&mode=total")
	mem := chartByID(t, out, "memory")
	if mem.Error != "" {
		t.Errorf("an unknown size must not fail the chart: %s", mem.Error)
	}
	if len(mem.Series) != 1 || len(mem.Series[0].Points) != 1 {
		t.Fatalf("series lost with an unknown size: %+v", mem.Series)
	}
	if mem.Series[0].Reference != 0 {
		t.Errorf("reference = %v, want 0 when the size is unknown", mem.Series[0].Reference)
	}
}

// podFor builds a live pod of a process, which is what gives an instance its
// name. Creation order decides the ordinal.
func podFor(app, process, name string, ageMinutes int) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "app-" + app,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Duration(ageMinutes) * time.Minute)),
			Labels:            map[string]string{shpyrdv1.LabelApp: app, shpyrdv1.LabelProcess: process},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func TestMetricsByInstanceNamesSeriesNotPods(t *testing.T) {
	prom, rec := newFakeProm(t, fakeAnswer{
		Match: "container_memory_working_set_bytes",
		Series: []fakeSeries{
			{Labels: map[string]string{"pod": "shop-web-old", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 10}}},
			{Labels: map[string]string{"pod": "shop-web-new", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 20}}},
		},
	})
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app},
		podFor("shop", "web", "shop-web-old", 30),
		podFor("shop", "web", "shop-web-new", 10),
	)

	out := getMetrics(t, s, "?range=1h&by=instance")
	mem := chartByID(t, out, "memory")
	names := seriesNames(mem)
	if strings.Join(names, ",") != "web.1,web.2" {
		t.Fatalf("series = %v, want web.1,web.2", names)
	}
	for _, n := range names {
		if strings.Contains(n, "shop-web") {
			t.Errorf("series %q leaks a pod name", n)
		}
	}
	if !rec.Queried("by (pod") {
		t.Error("by=instance must group the query by pod")
	}
	if !mem.InstanceCapable {
		t.Error("memory must report itself instance capable")
	}
}

// Review Focus 5 and the replaced-instance rule: a pod in the data that is no
// longer live is its own series, hidden unless asked for, and the chart says so.
func TestMetricsReplacedInstances(t *testing.T) {
	prom, _ := newFakeProm(t, fakeAnswer{
		Match: "container_memory_working_set_bytes",
		Series: []fakeSeries{
			{Labels: map[string]string{"pod": "shop-web-gone", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 10}}},
			{Labels: map[string]string{"pod": "shop-web-live", "label_shpyrd_io_process": "web"}, Values: []Point{{2000, 20}}},
		},
	})
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app}, podFor("shop", "web", "shop-web-live", 5))

	out := getMetrics(t, s, "?range=1h&by=instance")
	mem := chartByID(t, out, "memory")
	if strings.Join(seriesNames(mem), ",") != "web.1" {
		t.Errorf("replaced instances must be hidden by default, got %v", seriesNames(mem))
	}
	if mem.Note != "1 replaced instance hidden" {
		t.Errorf("note = %q, want the singular \"1 replaced instance hidden\"", mem.Note)
	}

	out = getMetrics(t, s, "?range=1h&by=instance&replaced=true")
	mem = chartByID(t, out, "memory")
	names := seriesNames(mem)
	if strings.Join(names, ",") != "web.1,replaced 1" {
		t.Fatalf("series = %v, want web.1 ahead of replaced 1", names)
	}
	for _, n := range names {
		if strings.Contains(n, "shop-web-gone") {
			t.Errorf("series %q leaks a pod name", n)
		}
	}
}

// Review Focus 3: the data carries a process the App no longer declares.
func TestMetricsUnknownProcessStillAppears(t *testing.T) {
	prom, _ := newFakeProm(t, fakeAnswer{
		Match: "container_memory_working_set_bytes",
		Series: []fakeSeries{
			{Labels: map[string]string{"pod": "shop-frontend-1", "label_shpyrd_io_process": "frontend"}, Values: []Point{{1000, 10}}},
		},
	})
	app := metricsApp() // declares web and worker, not frontend
	s, _ := newTestServer(t, prom, []client.Object{app})

	out := getMetrics(t, s, "?range=1h&by=instance&replaced=true")
	mem := chartByID(t, out, "memory")
	if len(mem.Series) != 1 {
		t.Fatalf("a process the App no longer declares was dropped: %+v", mem.Series)
	}
	if mem.Error != "" {
		t.Errorf("unexpected chart error: %s", mem.Error)
	}
}

// Review Focus 4: a big project must not swamp the response.
func TestMetricsInstanceSeriesAreCapped(t *testing.T) {
	var series []fakeSeries
	var objs []interface{}
	for i := 0; i < maxInstances+10; i++ {
		pod := fmt.Sprintf("shop-web-%03d", i)
		series = append(series, fakeSeries{
			Labels: map[string]string{"pod": pod, "label_shpyrd_io_process": "web"},
			Values: []Point{{1000, float64(i)}},
		})
		objs = append(objs, podFor("shop", "web", pod, maxInstances+10-i))
	}
	prom, _ := newFakeProm(t, fakeAnswer{Match: "container_memory_working_set_bytes", Series: series})
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app}, objs...)

	out := getMetrics(t, s, "?range=1h&by=instance")
	mem := chartByID(t, out, "memory")
	if len(mem.Series) != maxInstances {
		t.Fatalf("series = %d, want the cap of %d", len(mem.Series), maxInstances)
	}
	if !strings.Contains(mem.Note, "more") {
		t.Errorf("the chart must report what it left out, note = %q", mem.Note)
	}
	// The ordinal is a number, not text: the cap keeps web.1 to web.40 rather
	// than the lexicographic slice that would stop at web.19 and web.2.
	names := seriesNames(mem)
	if names[0] != "web.1" || names[1] != "web.2" || names[maxInstances-1] != fmt.Sprintf("web.%d", maxInstances) {
		t.Errorf("series = %v, want web.1, web.2 ... web.%d in numeric order", names, maxInstances)
	}
}

func TestMetricsProcessFilterAndIgnoringCharts(t *testing.T) {
	prom, rec := newFakeProm(t, fakeAnswer{
		Match:  "nginx_ingress_controller_requests",
		Series: []fakeSeries{{Labels: map[string]string{"class": "2xx"}, Values: []Point{{1000, 5}}}},
	})
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app})

	out := getMetrics(t, s, "?range=1h&by=instance&process=web")
	// The process reaches the queries that have a process...
	if !rec.Queried(`label_shpyrd_io_process="web"`) {
		t.Error("the process filter never reached a query")
	}
	// ...and the edge charts say plainly that they cannot honour it.
	tp := chartByID(t, out, "throughput")
	if tp.InstanceCapable {
		t.Error("throughput is measured at the edge and cannot be per instance")
	}
	for _, q := range rec.Queries() {
		if strings.Contains(q, "nginx_ingress_controller_requests") && strings.Contains(q, "by (pod") {
			t.Error("an edge query was grouped by pod")
		}
	}
}

// The owner's manual test found this: with a process selected, cpu, memory
// and network series narrowed down but instances kept showing every process
// type. Deployments are named "<app>-<process>", so the fix is a query
// filter, not a client-side one — proven here by two answers that only the
// filtered query text can tell apart.
func TestMetricsInstancesChartHonoursProcess(t *testing.T) {
	prom, rec := newFakeProm(t,
		fakeAnswer{
			Match: `deployment="shop-worker"`,
			Series: []fakeSeries{
				{Labels: map[string]string{"deployment": "shop-worker"}, Values: []Point{{1000, 3}}},
			},
		},
		fakeAnswer{
			Match: "kube_deployment_status_replicas_available",
			Series: []fakeSeries{
				{Labels: map[string]string{"deployment": "shop-web"}, Values: []Point{{1000, 2}}},
				{Labels: map[string]string{"deployment": "shop-worker"}, Values: []Point{{1000, 3}}},
			},
		},
	)
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app})

	// No process selected: every deployment still shows, query unfiltered.
	out := getMetrics(t, s, "?range=1h")
	inst := chartByID(t, out, "instances")
	if strings.Join(seriesNames(inst), ",") != "web,worker" {
		t.Fatalf("instances series = %v, want web,worker with no process filter", seriesNames(inst))
	}
	if inst.InstanceCapable {
		t.Error("instances has no per-instance breakdown; instanceCapable must stay false")
	}
	if rec.Queried(`deployment="shop-`) {
		t.Error("the default instances query must not filter by deployment")
	}

	// process=worker: the query itself is constrained to that one deployment,
	// so only the worker series comes back.
	out = getMetrics(t, s, "?range=1h&process=worker")
	inst = chartByID(t, out, "instances")
	if strings.Join(seriesNames(inst), ",") != "worker" {
		t.Fatalf("instances series = %v, want only worker when process=worker", seriesNames(inst))
	}
}

// The network chart already filters correctly when a process is selected —
// its query joins the process label in — but both series were named "in" and
// "out" regardless, so the chart looked unfiltered on screen. Selecting one
// process, with the default by=process grouping, must rename the series.
func TestMetricsNetworkNamesSeriesByProcess(t *testing.T) {
	prom, _ := newFakeProm(t,
		fakeAnswer{Match: "container_network_receive_bytes_total", Series: []fakeSeries{
			{Labels: map[string]string{}, Values: []Point{{1000, 10}}},
		}},
		fakeAnswer{Match: "container_network_transmit_bytes_total", Series: []fakeSeries{
			{Labels: map[string]string{}, Values: []Point{{1000, 20}}},
		}},
	)
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app})

	out := getMetrics(t, s, "?range=1h&process=worker")
	net := chartByID(t, out, "network")
	if strings.Join(seriesNames(net), ",") != "worker in,worker out" {
		t.Fatalf("network series = %v, want worker in and worker out with process=worker", seriesNames(net))
	}

	// With no process the query genuinely sums across every process, so
	// naming it after one would be wrong; the series stay in/out.
	out = getMetrics(t, s, "?range=1h")
	net = chartByID(t, out, "network")
	if strings.Join(seriesNames(net), ",") != "in,out" {
		t.Fatalf("default network series = %v, want in,out", seriesNames(net))
	}
}

// by=instance already names network series "<instance> in"/"<instance> out"
// off the pod, a code path entirely separate from the by=process rename
// above; selecting a process too must not double up the naming.
func TestMetricsNetworkByInstanceIgnoresProcessNaming(t *testing.T) {
	prom, _ := newFakeProm(t,
		fakeAnswer{Match: "container_network_receive_bytes_total", Series: []fakeSeries{
			{Labels: map[string]string{"pod": "shop-worker-live", "label_shpyrd_io_process": "worker"}, Values: []Point{{1000, 10}}},
		}},
		fakeAnswer{Match: "container_network_transmit_bytes_total", Series: []fakeSeries{
			{Labels: map[string]string{"pod": "shop-worker-live", "label_shpyrd_io_process": "worker"}, Values: []Point{{1000, 20}}},
		}},
	)
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app}, podFor("shop", "worker", "shop-worker-live", 5))

	out := getMetrics(t, s, "?range=1h&by=instance&process=worker")
	net := chartByID(t, out, "network")
	if strings.Join(seriesNames(net), ",") != "worker.1 in,worker.1 out" {
		t.Fatalf("network series = %v, want worker.1 in and worker.1 out, unchanged by the process-naming fix", seriesNames(net))
	}
}

// directionOf and processOf are what nameInstances, applyReference and
// aggregate lean on to read a series name back apart; the new
// "<process> in"/"<process> out" shape must still parse through both.
func TestMetricsNetworkProcessNamesParseThroughHelpers(t *testing.T) {
	if got := directionOf("worker in"); got != "in" {
		t.Errorf(`directionOf("worker in") = %q, want "in"`, got)
	}
	if got := directionOf("worker out"); got != "out" {
		t.Errorf(`directionOf("worker out") = %q, want "out"`, got)
	}
	if got := processOf("worker in"); got != "worker" {
		t.Errorf(`processOf("worker in") = %q, want "worker"`, got)
	}
}

func seriesNames(ch Chart) []string {
	out := make([]string, 0, len(ch.Series))
	for _, s := range ch.Series {
		out = append(out, s.Name)
	}
	return out
}

// Network is built from two sub-queries keyed by direction rather than by pod,
// so its per-instance names are the one set this handler composes itself: one
// series per instance per direction. Latency has the same shape and must stay
// untouched, since it is measured at the edge.
func TestMetricsNetworkByInstanceKeepsTheDirection(t *testing.T) {
	prom, _ := newFakeProm(t,
		fakeAnswer{Match: "container_network_receive_bytes_total", Series: []fakeSeries{
			{Labels: map[string]string{"pod": "shop-web-live", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 10}}},
			{Labels: map[string]string{"pod": "shop-web-gone", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 30}}},
		}},
		fakeAnswer{Match: "container_network_transmit_bytes_total", Series: []fakeSeries{
			{Labels: map[string]string{"pod": "shop-web-live", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 20}}},
			{Labels: map[string]string{"pod": "shop-web-gone", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 40}}},
		}},
	)
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app}, podFor("shop", "web", "shop-web-live", 5))

	out := getMetrics(t, s, "?range=1h&by=instance")
	net := chartByID(t, out, "network")
	if strings.Join(seriesNames(net), ",") != "web.1 in,web.1 out" {
		t.Fatalf("network series = %v, want web.1 in and web.1 out", seriesNames(net))
	}
	// Both directions of one replaced pod are one hidden instance, not two.
	if !strings.HasPrefix(net.Note, "1 replaced") {
		t.Errorf("note = %q, want one hidden instance, not one per direction", net.Note)
	}
	if lat := chartByID(t, out, "latency"); lat.InstanceCapable ||
		strings.Join(seriesNames(lat), ",") != "p50,p95,p99" {
		t.Errorf("latency is measured at the edge and must keep its percentiles, got %v", seriesNames(lat))
	}

	// The default keeps the two whole-project series it draws today.
	out = getMetrics(t, s, "?range=1h")
	if net = chartByID(t, out, "network"); strings.Join(seriesNames(net), ",") != "in,out" {
		t.Errorf("default network series = %v, want in,out", seriesNames(net))
	}
}

// Asking for the replaced instances must never cost the live ones: the cap is
// applied after an ordering that puts every live instance first. Without that,
// a project with a cap's worth of churn would chart nothing but dead pods.
func TestMetricsReplacedInstancesNeverDisplaceLiveOnes(t *testing.T) {
	var series []fakeSeries
	for i := 0; i < maxInstances; i++ {
		series = append(series, fakeSeries{
			Labels: map[string]string{"pod": fmt.Sprintf("shop-web-gone-%03d", i), "label_shpyrd_io_process": "web"},
			Values: []Point{{1000, float64(i)}},
		})
	}
	// Last in the data, so only the ordering can bring it to the front.
	series = append(series, fakeSeries{
		Labels: map[string]string{"pod": "shop-web-live", "label_shpyrd_io_process": "web"},
		Values: []Point{{1000, 99}},
	})
	prom, _ := newFakeProm(t, fakeAnswer{Match: "container_memory_working_set_bytes", Series: series})
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app}, podFor("shop", "web", "shop-web-live", 5))

	out := getMetrics(t, s, "?range=1h&by=instance&replaced=true")
	mem := chartByID(t, out, "memory")
	names := seriesNames(mem)
	if len(names) != maxInstances {
		t.Fatalf("series = %d, want the cap of %d", len(names), maxInstances)
	}
	if names[0] != "web.1" {
		t.Errorf("series[0] = %q, want the live instance ahead of every replaced one", names[0])
	}
	for _, n := range names[1:] {
		if !strings.HasPrefix(n, "replaced ") {
			t.Errorf("series %q is live but sorted after a replaced one", n)
		}
	}
	// Replaced instances are ordered by their own number too, so the one the cap
	// drops is the last of them.
	if names[1] != "replaced 1" || names[maxInstances-1] != fmt.Sprintf("replaced %d", maxInstances-1) {
		t.Errorf("series = %v, want replaced 1 ... replaced %d", names, maxInstances-1)
	}
	if mem.Note != "1 more instance not shown" {
		t.Errorf("note = %q, want the singular \"1 more instance not shown\"", mem.Note)
	}
}

// The two network sub-queries are two Prometheus calls, so one can fail after
// the other has already produced pod-keyed series. Nothing the handler returns
// on that path may carry a pod name.
func TestMetricsNetworkPartialFailureLeaksNoPodName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		// "in" is queried first and succeeds; "out" times out.
		if strings.Contains(q, "container_network_transmit_bytes_total") {
			w.WriteHeader(http.StatusGatewayTimeout)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "error", "errorType": "timeout", "error": "query timed out",
			})
			return
		}
		result := []map[string]any{}
		if strings.Contains(q, "container_network_receive_bytes_total") {
			result = append(result, map[string]any{
				"metric": map[string]string{"pod": "shop-web-live", "label_shpyrd_io_process": "web"},
				"values": [][2]any{{1000, "10"}},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data":   map[string]any{"resultType": "matrix", "result": result},
		})
	}))
	t.Cleanup(srv.Close)
	app := metricsApp()
	s, _ := newTestServer(t, &PromClient{BaseURL: srv.URL, HTTP: srv.Client()},
		[]client.Object{app}, podFor("shop", "web", "shop-web-live", 5))

	rec := do(t, s, "GET", "/api/projects/shop/metrics?range=1h&by=instance", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	// The whole response, not just the one chart: a pod name anywhere is a leak.
	if strings.Contains(rec.Body.String(), "shop-web-live") {
		t.Errorf("a pod name reached the response: %s", rec.Body.String())
	}
	var out MetricsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	net := chartByID(t, out, "network")
	if net.Error == "" {
		t.Error("the failed direction must be reported, not swallowed")
	}
	if len(net.Series) != 0 {
		t.Errorf("a failed chart drew %d series: %+v", len(net.Series), net.Series)
	}
}

// A failed pod list must not read as a project whose every instance was
// replaced: hidden by default, that would return empty charts and no error.
func TestMetricsPodListFailureIsReportedNotMislabelled(t *testing.T) {
	prom, _ := newFakeProm(t, fakeAnswer{
		Match: "container_memory_working_set_bytes",
		Series: []fakeSeries{
			{Labels: map[string]string{"pod": "shop-web-live", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 10}}},
		},
	})
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app}, podFor("shop", "web", "shop-web-live", 5))
	s.kube.Kube.(*kubefake.Clientset).PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("etcdserver: request timed out")
	})

	out := getMetrics(t, s, "?range=1h&by=instance")
	mem := chartByID(t, out, "memory")
	if mem.Error == "" {
		t.Errorf("a failed pod list must reach the chart as an error, got note %q and series %+v", mem.Note, mem.Series)
	}
	if len(mem.Series) != 0 {
		t.Errorf("series drawn from names that could not be resolved: %+v", mem.Series)
	}
	if strings.Contains(mem.Note, "replaced") {
		t.Errorf("note = %q; a live instance was labelled replaced because the list failed", mem.Note)
	}
	// By process the charts need no pod names at all, so they still render.
	out = getMetrics(t, s, "?range=1h")
	if mem = chartByID(t, out, "memory"); mem.Error != "" || len(mem.Series) != 1 {
		t.Errorf("by process must not depend on the pod list: error %q, series %+v", mem.Error, mem.Series)
	}
}

// The cap counts instances, not series, so it admits the same number of
// instances on the chart that draws two series for each of them.
func TestMetricsNetworkCapCountsInstancesNotSeries(t *testing.T) {
	const over = 5
	var in, outbound []fakeSeries
	var objs []interface{}
	for i := 0; i < maxInstances+over; i++ {
		pod := fmt.Sprintf("shop-web-%03d", i)
		labels := map[string]string{"pod": pod, "label_shpyrd_io_process": "web"}
		in = append(in, fakeSeries{Labels: labels, Values: []Point{{1000, float64(i)}}})
		outbound = append(outbound, fakeSeries{Labels: labels, Values: []Point{{1000, float64(i) * 2}}})
		objs = append(objs, podFor("shop", "web", pod, maxInstances+over-i))
	}
	prom, _ := newFakeProm(t,
		fakeAnswer{Match: "container_network_receive_bytes_total", Series: in},
		fakeAnswer{Match: "container_network_transmit_bytes_total", Series: outbound},
	)
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app}, objs...)

	got := chartByID(t, getMetrics(t, s, "?range=1h&by=instance"), "network")
	if len(got.Series) != 2*maxInstances {
		t.Errorf("series = %d, want %d instances at two series each", len(got.Series), maxInstances)
	}
	// Both directions of a kept instance are kept, and the note counts the
	// instances that are missing rather than their series.
	names := seriesNames(got)
	if names[0] != "web.1 in" || names[1] != "web.1 out" {
		t.Errorf("series = %v, want both directions of web.1 first", names[:2])
	}
	if got.Note != fmt.Sprintf("%d more instances not shown", over) {
		t.Errorf("note = %q, want %d instances missing", got.Note, over)
	}
}

// The by-process default must not pay for the pod list, which is the only
// thing keeping a Kubernetes call off a request that repeats every 30 seconds.
func TestMetricsByProcessListsNoPods(t *testing.T) {
	prom, _ := newFakeProm(t)
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app}, podFor("shop", "web", "shop-web-live", 5))
	cs := s.kube.Kube.(*kubefake.Clientset)

	getMetrics(t, s, "?range=1h")
	for _, a := range cs.Actions() {
		if a.GetVerb() == "list" && a.GetResource().Resource == "pods" {
			t.Fatal("the by-process default listed the pods")
		}
	}
	// And the instance breakdown does, since that is what needs the names.
	cs.ClearActions()
	getMetrics(t, s, "?range=1h&by=instance")
	listed := false
	for _, a := range cs.Actions() {
		if a.GetVerb() == "list" && a.GetResource().Resource == "pods" {
			listed = true
		}
	}
	if !listed {
		t.Error("by=instance must list the pods to name them")
	}
}

// The namer selects exactly what the Logs tab selects, so a pod with no process
// label — a build, a one-off — is an instance on neither tab. Named off the app
// label alone it would become "app.1", a name the Logs tab never shows.
func TestMetricsPodWithoutProcessIsNoInstance(t *testing.T) {
	prom, _ := newFakeProm(t, fakeAnswer{
		Match: "container_memory_working_set_bytes",
		Series: []fakeSeries{
			{Labels: map[string]string{"pod": "shop-build-abc"}, Values: []Point{{1000, 10}}},
			{Labels: map[string]string{"pod": "shop-web-live", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 20}}},
		},
	})
	build := podFor("shop", "web", "shop-build-abc", 20)
	delete(build.Labels, shpyrdv1.LabelProcess)
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app}, build, podFor("shop", "web", "shop-web-live", 5))

	out := getMetrics(t, s, "?range=1h&by=instance&replaced=true")
	names := seriesNames(chartByID(t, out, "memory"))
	for _, n := range names {
		if strings.HasPrefix(n, "app.") {
			t.Errorf("series %q names a pod the Logs tab would not call an instance", n)
		}
	}
	if strings.Join(names, ",") != "web.1,replaced 1" {
		t.Fatalf("series = %v, want web.1 and the build pod as replaced 1", names)
	}
}

func TestMetricsAggregation(t *testing.T) {
	answer := fakeAnswer{
		Match: "container_memory_working_set_bytes",
		Series: []fakeSeries{
			{Labels: map[string]string{"pod": "shop-web-a", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 10}, {2000, 30}}},
			{Labels: map[string]string{"pod": "shop-web-b", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 20}, {2000, 50}}},
		},
	}
	for _, tc := range []struct {
		agg  string
		name string
		at1  float64
		at2  float64
	}{
		{"sum", "sum", 30, 80},
		{"avg", "avg", 15, 40},
		{"max", "max", 20, 50},
	} {
		prom, _ := newFakeProm(t, answer)
		app := metricsApp()
		s, _ := newTestServer(t, prom, []client.Object{app},
			podFor("shop", "web", "shop-web-a", 20), podFor("shop", "web", "shop-web-b", 10))

		out := getMetrics(t, s, "?range=1h&by=instance&agg="+tc.agg)
		mem := chartByID(t, out, "memory")
		if len(mem.Series) != 1 {
			t.Fatalf("agg=%s produced %d series, want 1: %v", tc.agg, len(mem.Series), seriesNames(mem))
		}
		if mem.Series[0].Name != tc.name {
			t.Errorf("agg=%s series name = %q, want %q", tc.agg, mem.Series[0].Name, tc.name)
		}
		pts := mem.Series[0].Points
		if len(pts) != 2 || pts[0][1] != tc.at1 || pts[1][1] != tc.at2 {
			t.Errorf("agg=%s points = %v, want %v then %v", tc.agg, pts, tc.at1, tc.at2)
		}
	}

	// agg with the default grouping is meaningless, and ignored rather than
	// refused: the aggregation is already implied.
	prom, _ := newFakeProm(t, answer)
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app})
	if rec := do(t, s, "GET", "/api/projects/shop/metrics?range=1h&agg=sum", "", true); rec.Code != http.StatusOK {
		t.Errorf("agg with by=process: %d, want 200 (ignored)", rec.Code)
	}
	// An aggregation nobody defined is a client bug worth naming.
	if rec := do(t, s, "GET", "/api/projects/shop/metrics?range=1h&by=instance&agg=median", "", true); rec.Code != http.StatusBadRequest {
		t.Errorf("agg=median: %d, want 400", rec.Code)
	}
}

// Network's series carry a direction suffix ("web.1 in", "web.1 out"), so
// collapsing every instance into a single series would sum inbound and
// outbound traffic together and silently lose which was which. Aggregation
// must instead run once per direction.
func TestMetricsAggregationKeepsDirectionsSeparate(t *testing.T) {
	prom, _ := newFakeProm(t,
		fakeAnswer{Match: "container_network_receive_bytes_total", Series: []fakeSeries{
			{Labels: map[string]string{"pod": "shop-web-a", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 10}}},
			{Labels: map[string]string{"pod": "shop-web-b", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 20}}},
		}},
		fakeAnswer{Match: "container_network_transmit_bytes_total", Series: []fakeSeries{
			{Labels: map[string]string{"pod": "shop-web-a", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 100}}},
			{Labels: map[string]string{"pod": "shop-web-b", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 200}}},
		}},
	)
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app},
		podFor("shop", "web", "shop-web-a", 20), podFor("shop", "web", "shop-web-b", 10))

	out := getMetrics(t, s, "?range=1h&by=instance&agg=sum")
	net := chartByID(t, out, "network")
	if len(net.Series) != 2 {
		t.Fatalf("agg=sum produced %d network series, want 2 (one per direction): %v", len(net.Series), seriesNames(net))
	}
	names := seriesNames(net)
	if names[0] != "sum in" || names[1] != "sum out" {
		t.Fatalf("network series = %v, want sum in and sum out, not merged across directions", names)
	}
	if v := net.Series[0].Points[0][1]; v != 30 {
		t.Errorf("sum in = %v, want 30 (10+20)", v)
	}
	if v := net.Series[1].Points[0][1]; v != 300 {
		t.Errorf("sum out = %v, want 300 (100+200)", v)
	}
}

// The reference line an aggregate is measured against only means something
// when every instance folded into it shares the same allocation: a sum is
// measured against the sum of those allocations, an average or a max against
// the single allocation they share, and a mix of allocations draws no line
// at all rather than one that is wrong for some of the instances.
func TestMetricsAggregationReference(t *testing.T) {
	// shared-s (both web and worker's default size): CPU request 0.5 cores,
	// burst to 4x = 2 cores.
	t.Run("sum multiplies the reference and the burst", func(t *testing.T) {
		prom, _ := newFakeProm(t, fakeAnswer{
			Match: "container_cpu_usage_seconds_total",
			Series: []fakeSeries{
				{Labels: map[string]string{"pod": "shop-web-a", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 0.1}}},
				{Labels: map[string]string{"pod": "shop-web-b", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 0.2}}},
			},
		})
		app := metricsApp()
		s, _ := newTestServer(t, prom, []client.Object{app},
			podFor("shop", "web", "shop-web-a", 20), podFor("shop", "web", "shop-web-b", 10))

		out := getMetrics(t, s, "?range=1h&mode=total&by=instance&agg=sum")
		cpu := chartByID(t, out, "cpu")
		if len(cpu.Series) != 1 {
			t.Fatalf("series = %d, want 1: %v", len(cpu.Series), seriesNames(cpu))
		}
		if cpu.Series[0].Reference != 1.0 {
			t.Errorf("reference = %v, want 1.0 (0.5 x 2 instances)", cpu.Series[0].Reference)
		}
		if cpu.Series[0].Burst != 4.0 {
			t.Errorf("burst = %v, want 4.0 (2 x 2 instances)", cpu.Series[0].Burst)
		}
	})

	t.Run("avg and max keep the single allocation", func(t *testing.T) {
		for _, agg := range []string{"avg", "max"} {
			prom, _ := newFakeProm(t, fakeAnswer{
				Match: "container_cpu_usage_seconds_total",
				Series: []fakeSeries{
					{Labels: map[string]string{"pod": "shop-web-a", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 0.1}}},
					{Labels: map[string]string{"pod": "shop-web-b", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 0.2}}},
				},
			})
			app := metricsApp()
			s, _ := newTestServer(t, prom, []client.Object{app},
				podFor("shop", "web", "shop-web-a", 20), podFor("shop", "web", "shop-web-b", 10))

			out := getMetrics(t, s, "?range=1h&mode=total&by=instance&agg="+agg)
			cpu := chartByID(t, out, "cpu")
			if len(cpu.Series) != 1 {
				t.Fatalf("agg=%s series = %d, want 1: %v", agg, len(cpu.Series), seriesNames(cpu))
			}
			if cpu.Series[0].Reference != 0.5 {
				t.Errorf("agg=%s reference = %v, want 0.5, unmultiplied", agg, cpu.Series[0].Reference)
			}
			if cpu.Series[0].Burst != 2.0 {
				t.Errorf("agg=%s burst = %v, want 2.0, unmultiplied", agg, cpu.Series[0].Burst)
			}
		}
	})

	t.Run("differing allocations drop the reference", func(t *testing.T) {
		prom, _ := newFakeProm(t, fakeAnswer{
			Match: "container_cpu_usage_seconds_total",
			Series: []fakeSeries{
				{Labels: map[string]string{"pod": "shop-web-a", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 0.1}}},
				{Labels: map[string]string{"pod": "shop-worker-a", "label_shpyrd_io_process": "worker"}, Values: []Point{{1000, 0.5}}},
			},
		})
		app := metricsApp()
		// dedicated-s requests a whole core, genuinely different from web's
		// shared-s 0.5 — not merely a different burst ceiling on the same
		// request, which is the other test below.
		app.Spec.Processes["worker"] = shpyrdv1.Process{Size: "dedicated-s"}
		s, _ := newTestServer(t, prom, []client.Object{app},
			podFor("shop", "web", "shop-web-a", 20), podFor("shop", "worker", "shop-worker-a", 20))

		out := getMetrics(t, s, "?range=1h&mode=total&by=instance&agg=sum")
		cpu := chartByID(t, out, "cpu")
		if len(cpu.Series) != 1 {
			t.Fatalf("series = %d, want 1: %v", len(cpu.Series), seriesNames(cpu))
		}
		if cpu.Series[0].Reference != 0 || cpu.Series[0].Burst != 0 {
			t.Errorf("reference = %v, burst = %v, want both 0 when the aggregated instances disagree",
				cpu.Series[0].Reference, cpu.Series[0].Burst)
		}
	})
}

// A shared size and a dedicated size can request the same CPU while capping
// it at a different ceiling. Comparing only the reference would let that
// slip through and draw a burst line that is wrong for at least one of the
// aggregated instances.
func TestMetricsAggregationDropsReferenceWhenBurstDiffers(t *testing.T) {
	prom, _ := newFakeProm(t, fakeAnswer{
		Match: "container_cpu_usage_seconds_total",
		Series: []fakeSeries{
			{Labels: map[string]string{"pod": "shop-web-a", "label_shpyrd_io_process": "web"}, Values: []Point{{1000, 0.1}}},
			{Labels: map[string]string{"pod": "shop-worker-a", "label_shpyrd_io_process": "worker"}, Values: []Point{{1000, 0.1}}},
		},
	})
	app := metricsApp()
	// Same 0.5-core request as web's shared-s, but its limit is capped down
	// to the request instead of the size's usual 4x burst.
	app.Spec.Processes["worker"] = shpyrdv1.Process{
		Size:      "shared-s",
		Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")}},
	}
	s, _ := newTestServer(t, prom, []client.Object{app},
		podFor("shop", "web", "shop-web-a", 20), podFor("shop", "worker", "shop-worker-a", 20))

	out := getMetrics(t, s, "?range=1h&mode=total&by=instance&agg=sum")
	cpu := chartByID(t, out, "cpu")
	if len(cpu.Series) != 1 {
		t.Fatalf("series = %d, want 1: %v", len(cpu.Series), seriesNames(cpu))
	}
	if cpu.Series[0].Reference != 0 || cpu.Series[0].Burst != 0 {
		t.Errorf("reference = %v, burst = %v, want both 0: the requests agree but the burst ceilings do not",
			cpu.Series[0].Reference, cpu.Series[0].Burst)
	}
}
