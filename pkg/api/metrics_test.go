package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

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
