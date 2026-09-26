# Application Metrics v2 (RFC-0027) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the Metrics tab per-instance series with an instance filter, aggregation, a percentage/total toggle showing each process's allocation, a 15-minute range, and per-process views — without ever showing a pod name.

**Architecture:** The existing handler keeps its declarative chart table; the table moves to its own file and each entry learns how to build a per-pod variant of its query. Instance names are resolved in Go from the live pod list (Prometheus cannot join the annotation), so Logs and Metrics agree by construction. Everything new is an additive query parameter, so the call the dashboard makes today returns exactly what it returns today.

**Tech Stack:** Go, gin, Prometheus HTTP API (`pkg/api/prometheus.go`), kube-state-metrics + cAdvisor series, React 19, Recharts, TanStack Query.

**Spec:** `rfcs/0027-application-metrics-v2.md`

## Global Constraints

- Branch: `rfc-0027-app-metrics-v2`, already claimed (`b79dd94`, `71b653e`), based on main at `a4fcd35`.
- Conventional Commits, as commitlint requires. Every commit message ends with:
  ```
  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01CFQobmze26mnGVmyUF8oVp
  ```
- Go: `go test ./...`, `go build ./...`, `go vet ./...`. UI: `cd ui && npm run build && npm run lint`.
- **No pod names anywhere in a response or the UI** (RFC-0011). Instances are `web.1`; pods that no longer exist are `replaced 1`, `replaced 2`, … in order of first appearance.
- `by` defaults to `process`. The existing request `GET …/metrics?range=1h` must keep returning the same chart set with the same series names.
- Ranges are exactly `15m`, `1h`, `6h`, `24h`, `7d`. Anything else is 400.
- Which charts honour which parameter: `process`, `by` and `agg` apply to **cpu, memory, network**; `throughput` and `latency` ignore them. `mode` applies to **cpu and memory** only.
- `agg` passed with `by=process` is ignored, not refused.
- Percentage is relative to the process's **request** (the allocation). Shared sizes may exceed 100% and that is correct, not a bug to clamp.
- Comments explain *why*, in prose, matching the density of the surrounding files.
- This environment condenses command output. If a command's output looks garbled or empty when you expected output, re-run it as `rtk proxy <command>`.

## Review Focus

Failure modes the spec implies but no happy path exercises. Each has its test assigned to the task that owns the code.

1. **A project with no metrics yet** (just created, or Prometheus only just installed) — every chart must come back present and empty, not an error and not a missing chart. → Task 1.
2. **A size that is no longer in the catalog** (renamed or deleted after deploy) — `Catalog.Resolve` returns an error; the reference line must be omitted for that process while the series still render. A 500 for the whole tab would be wrong. → Task 3.
3. **A process type in the metric data that the App no longer declares** (renamed `web` → `frontend`, old series still inside the range) — it must appear under the name the data carries rather than being dropped silently or crashing the name lookup. → Task 4.
4. **An instance count large enough to swamp the response** (`by=instance` on 50 replicas over 7 days) — the response must stay bounded; series are capped with the overflow reported, not truncated silently. → Task 4.
5. **Every pod in the window already replaced** (a project redeployed twice inside the range, so nothing in the data is live) — with the toggle off the chart is empty but must say why, rather than looking broken. → Task 4.

---

### Task 1: A test seam for metrics, and the file split

Metrics have no tests at all today, and this change is almost entirely query construction — the thing that fails silently. Build the harness and lock current behaviour *before* changing any behaviour.

**Files:**
- Create: `pkg/api/metricqueries.go` (moved code only, no behaviour change)
- Create: `pkg/api/metrics_test.go`
- Modify: `pkg/api/metrics.go` (remove what moved)

**Interfaces:**
- Consumes: `PromClient{BaseURL, HTTP}`, `RawSeries{Labels, Points}`, `Point [2]float64`, `newTestServer`, `do`, `sampleApp`.
- Produces:
  - `func newFakeProm(t *testing.T, answers ...fakeAnswer) (*PromClient, *fakeRecorder)`
  - `type fakeAnswer struct { Match string; Series []fakeSeries }`
  - `type fakeSeries struct { Labels map[string]string; Values []Point }`
  - `type fakeRecorder struct { … }` with `Queries() []string`
  - moved unchanged: `chartQueries(app *shpyrdv1.App) []chartQuery`, `hostRegex(app *shpyrdv1.App) string`

- [ ] **Step 1: Write the failing test**

Create `pkg/api/metrics_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

var _ = metav1.Now // keep the import if a later task drops its only use
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/api/ -run TestMetrics -v`
Expected: FAIL — `undefined: sampleApp` will resolve (it exists in `server_test.go`), but the tests fail because `Chart.Series` is currently `[]Series{}` only on the paths that run, and because nothing yet guarantees the chart set. Read the failure and confirm it is about behaviour, not a typo. If `TestMetricsWithNoDataReturnsEmptyCharts` passes immediately, that is fine — it is a characterisation test — but `TestMetricsByProcessIsTheDefault` must exercise the fake, so check `rec.Queries()` is non-empty.

- [ ] **Step 3: Make the tests pass and split the file**

No behaviour change. Move `chartQueries` and `hostRegex` verbatim from `pkg/api/metrics.go` into a new `pkg/api/metricqueries.go`:

```go
package api

// The chart table for a project's Metrics tab. Kept apart from the handler
// because RFC-0027 multiplies the query variants (by process or instance, an
// aggregation, percentage or absolute) and the table is worth keeping
// declarative rather than branching inside the handler.

import (
	"fmt"
	"regexp"
	"strings"

	shpyrdv1 "shpyrd/api/v1alpha1"
)
```

followed by the two functions exactly as they are. Remove them from `metrics.go`, along with the imports that only they used (`regexp`; keep the others if still referenced).

If `TestMetricsWithNoDataReturnsEmptyCharts` fails on a chart with nil `Series`, fix it in `runChart` by keeping `ch.Series = []Series{}` on every return path — it is already set at the top, so confirm no path replaces it with nil.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/api/ -run TestMetrics -v && go test ./... && go build ./... && go vet ./...`
Expected: PASS, everything clean. The split must not change a single byte of behaviour.

- [ ] **Step 5: Commit**

```bash
git add pkg/api/metrics.go pkg/api/metricqueries.go pkg/api/metrics_test.go
git commit -m "test(api): a fake Prometheus for the metrics handler, and split the chart table

Metrics had no tests, and RFC-0027 is almost entirely query construction — the
thing that fails silently. The fake answers on a fragment of the PromQL and
records what was asked, so a test can say what a query should return without
reproducing PromQL. The chart table moves to its own file ahead of the variants
the RFC adds. No behaviour change.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CFQobmze26mnGVmyUF8oVp"
```

---

### Task 2: The 15-minute range and an explicit step table

**Files:**
- Modify: `pkg/api/metrics.go` (`rangeOptions`, the step calculation in `appMetrics`)
- Test: `pkg/api/metrics_test.go`

**Interfaces:**
- Produces: `var rangeOptions map[string]time.Duration` gains `"15m"`; `func stepFor(d time.Duration) time.Duration`.

- [ ] **Step 1: Write the failing test**

Append to `pkg/api/metrics_test.go`:

```go
func TestMetricsRanges(t *testing.T) {
	prom, _ := newFakeProm(t)
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app})

	// Every documented range is accepted, and the step keeps the number of
	// points per series in a range a chart can actually draw.
	for _, tc := range []struct{ rng string; wantStep int }{
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
	if rec := do(t, s, "GET", "/api/projects/shop/metrics?range=30s", "", true); rec.Code != http.StatusBadRequest {
		t.Errorf("range=30s: %d, want 400", rec.Code)
	}
	if !strings.Contains(do(t, s, "GET", "/api/projects/shop/metrics?range=30s", "", true).Body.String(), "15m") {
		t.Error("the 400 should name the ranges that are allowed, including 15m")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/api/ -run TestMetricsRanges -v`
Expected: FAIL — `range=15m` currently returns 400, and the error message does not mention `15m`.

- [ ] **Step 3: Write minimal implementation**

In `pkg/api/metrics.go`:

```go
var rangeOptions = map[string]time.Duration{
	"15m": 15 * time.Minute,
	"1h":  time.Hour,
	"6h":  6 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
}

// stepFor keeps every range at 60 points: enough shape to read, few enough
// that a week of per-instance series stays a response a browser can chart.
func stepFor(d time.Duration) time.Duration {
	step := d / 60
	if step < time.Minute {
		step = time.Minute
	}
	return step
}
```

Replace the inline step calculation in `appMetrics` with `step := stepFor(dur)`, and make the 400 name the options:

```go
	dur, ok := rangeOptions[rng]
	if !ok {
		abort(c, http.StatusBadRequest, errors.New("range must be one of 15m, 1h, 6h, 24h, 7d"))
		return
	}
```

Note for the implementer: with a 15-minute range `d/60` is 15s, below the minute floor, so `15m` yields a 60-second step — which is why the test expects 60 for both `15m` and `1h`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/api/ -run TestMetrics -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/api/metrics.go pkg/api/metrics_test.go
git commit -m "feat(api): a 15-minute metrics range, with the step in one place

stepFor holds every range to 60 points, which is what keeps a week of
per-instance series a response a browser can chart. The 400 now names the
ranges it will accept.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CFQobmze26mnGVmyUF8oVp"
```

---

### Task 3: `mode=percent|total` and the allocation reference

**Files:**
- Modify: `pkg/api/metrics.go` (`Series`, `appMetrics`), `pkg/api/metricqueries.go` (cpu and memory entries)
- Modify: `rfcs/0027-application-metrics-v2.md` (one correction, see Step 5)
- Test: `pkg/api/metrics_test.go`

**Interfaces:**
- Consumes: `s.catalog(ctx) (*sizes.Catalog, error)`, `sizes.Catalog.Resolve(sizeName string, override corev1.ResourceRequirements) (corev1.ResourceRequirements, string, error)`, `app.Spec.Processes map[string]shpyrdv1.Process` with fields `Size string` and `Resources corev1.ResourceRequirements`.
- Produces:
  - `Series` gains `Reference float64 \`json:"reference,omitempty"\`` and `Burst float64 \`json:"burst,omitempty"\``
  - `func (s *Server) allocations(ctx context.Context, app *shpyrdv1.App) map[string]allocation`
  - `type allocation struct { CPUCores, MemoryBytes, BurstCores float64 }`

**Why the reference is per series, not per chart.** The spec's Proposal says "each chart carries a `reference`", which cannot work: a `by=process` chart draws `web` and `worker` together and they can have different sizes. It belongs on the series. Task 3 Step 5 corrects the spec text.

- [ ] **Step 1: Write the failing test**

Append to `pkg/api/metrics_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/api/ -run 'TestMetricsTotal|TestMetricsUnknownSize' -v`
Expected: FAIL — `Series` has no `Reference` field, so this will not compile. That is the expected first failure.

- [ ] **Step 3: Write minimal implementation**

In `pkg/api/metrics.go`, extend `Series` and add the allocation lookup:

```go
// Series is a named time series. Reference is the allocation the series is
// measured against in absolute mode — the process's request, which is what the
// project pays for — and Burst the higher ceiling when the size has one. Both
// are zero in percentage mode, where 100% is the line, and on charts that
// measure something with no allocation at all.
type Series struct {
	Name      string  `json:"name"`
	Points    []Point `json:"points"`
	Reference float64 `json:"reference,omitempty"`
	Burst     float64 `json:"burst,omitempty"`
}

// allocation is what one process type was given.
type allocation struct {
	CPUCores    float64
	MemoryBytes float64
	// BurstCores is the CPU ceiling when the size sets one. Shared sizes
	// usually do not, which is why percentages above 100% are normal and why
	// this is not simply "the limit".
	BurstCores float64
}

// allocations resolves every process type's size to numbers the charts can
// draw a line at. A size missing from the catalog costs that process its
// reference line and nothing else: the series still matter.
func (s *Server) allocations(ctx context.Context, app *shpyrdv1.App) map[string]allocation {
	out := map[string]allocation{}
	cat, err := s.catalog(ctx)
	if err != nil || cat == nil {
		return out
	}
	for name, p := range app.Spec.Processes {
		res, _, err := cat.Resolve(p.Size, p.Resources)
		if err != nil {
			continue
		}
		a := allocation{}
		if q, ok := res.Requests[corev1.ResourceCPU]; ok {
			a.CPUCores = float64(q.MilliValue()) / 1000
		}
		if q, ok := res.Requests[corev1.ResourceMemory]; ok {
			a.MemoryBytes = float64(q.Value())
		}
		if q, ok := res.Limits[corev1.ResourceCPU]; ok {
			burst := float64(q.MilliValue()) / 1000
			if burst > a.CPUCores {
				a.BurstCores = burst
			}
		}
		out[name] = a
	}
	return out
}
```

Add `corev1 "k8s.io/api/core/v1"` to the imports if absent.

In `appMetrics`, read the mode and pass it plus the allocations into the table:

```go
	mode := c.DefaultQuery("mode", "percent")
	if mode != "percent" && mode != "total" {
		abort(c, http.StatusBadRequest, errors.New("mode must be percent or total"))
		return
	}
	allocs := s.allocations(c.Request.Context(), app)
	queries := chartQueries(app, metricsOptions{Mode: mode})
```

In `pkg/api/metricqueries.go`, introduce the options struct and make cpu/memory honour the mode:

```go
// metricsOptions are the request's shaping parameters. The table reads them so
// the handler does not have to branch per chart.
type metricsOptions struct {
	Mode string // percent (default) or total
}

// absolute reports whether this chart is expressed in its own units rather
// than as a proportion of an allocation.
func (o metricsOptions) absolute() bool { return o.Mode == "total" }
```

For the cpu entry, when `opts.absolute()` the query becomes the numerator alone and the unit becomes `cores`:

```go
		cpuQuery := fmt.Sprintf(`sum by (label_shpyrd_io_process) (rate(container_cpu_usage_seconds_total{%s}[2m]) * on (namespace, pod) group_left (label_shpyrd_io_process) %s)`, containers, podLabels)
		cpuUnit := "cores"
		if !opts.absolute() {
			cpuQuery = fmt.Sprintf(`100 * %s / sum by (label_shpyrd_io_process) (kube_pod_container_resource_requests{%s,resource="cpu"} * on (namespace, pod) group_left (label_shpyrd_io_process) %s)`, cpuQuery, containers, podLabels)
			cpuUnit = "%"
		}
```

Apply the same shape to memory with `container_memory_working_set_bytes` and unit `bytes`. Keep both existing `Fallback` paths untouched.

Finally, in `runChart`, stamp the reference onto each series in absolute mode. `runChart` needs the allocations and the chart's identity, so give it the options:

```go
// reference fills in the line a series is measured against. Only the charts
// that measure a share of an allocation have one, and only in absolute mode —
// in percentage mode the line is 100%.
func applyReference(ch *Chart, allocs map[string]allocation, opts metricsOptions) {
	if !opts.absolute() {
		return
	}
	for i := range ch.Series {
		a, ok := allocs[processOf(ch.Series[i].Name)]
		if !ok {
			continue
		}
		switch ch.ID {
		case "cpu":
			ch.Series[i].Reference, ch.Series[i].Burst = a.CPUCores, a.BurstCores
		case "memory":
			ch.Series[i].Reference = a.MemoryBytes
		}
	}
}

// processOf takes the process type out of a series name: "web" stays "web",
// "web.2" and "web.2 in" become "web".
func processOf(series string) string {
	name := series
	if i := strings.IndexByte(name, ' '); i > 0 {
		name = name[:i]
	}
	if i := strings.IndexByte(name, '.'); i > 0 {
		name = name[:i]
	}
	return name
}
```

Call `applyReference(&ch, allocs, opts)` just before `runChart` returns, and thread `allocs`/`opts` through the goroutine that calls it.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/api/ -run TestMetrics -v && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Correct the spec, then commit**

In `rfcs/0027-application-metrics-v2.md`, the Proposal bullet currently reads "Each chart carries a `reference`". Replace it with:

```markdown
- Each **series** carries a `reference`: the allocation it is measured against in absolute
  mode (for example 64 MiB for a `shared-s` process), plus a `burst` when the size sets a
  higher CPU ceiling. It cannot live on the chart, because one chart draws several
  processes and they may have different sizes. In percentage mode neither is set: the line
  is 100%.
```

```bash
git add pkg/api/metrics.go pkg/api/metricqueries.go pkg/api/metrics_test.go rfcs/0027-application-metrics-v2.md
git commit -m "feat(api): percent or total metrics, with each series' allocation

Absolute mode drops the division by the request and reports the allocation the
series is measured against, so the UI can draw the line without a second
request. It belongs on the series, not the chart: one chart draws several
processes and their sizes can differ — the RFC said chart and is corrected here.
A size missing from the catalog costs that process its line and nothing else.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CFQobmze26mnGVmyUF8oVp"
```

---

### Task 4: `by=instance` and the `process` filter

The heart of the RFC. Per-pod queries, names resolved in Go, replaced instances labelled honestly, and a cap so a large project cannot swamp the response.

**Files:**
- Modify: `pkg/api/metrics.go` (`appMetrics`, `runChart`), `pkg/api/metricqueries.go`
- Test: `pkg/api/metrics_test.go`

**Interfaces:**
- Consumes: `logs.InstanceNames(pods []corev1.Pod) map[string]string`, `shpyrdv1.LabelApp`, `shpyrdv1.LabelProcess`, `metricsOptions` from Task 3, `Series.Reference` from Task 3.
- Produces:
  - `metricsOptions` gains `By string`, `Process string`
  - `Chart` gains `InstanceCapable bool \`json:"instanceCapable"\`` and `Note string \`json:"note,omitempty"\``
  - `func (s *Server) instanceNamer(ctx context.Context, app *shpyrdv1.App) func(pod string) string`
  - `const maxInstanceSeries = 40`

- [ ] **Step 1: Write the failing test**

Append to `pkg/api/metrics_test.go`:

```go
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
	if !strings.Contains(mem.Note, "replaced") {
		t.Errorf("the chart should say a replaced instance was hidden, note = %q", mem.Note)
	}

	out = getMetrics(t, s, "?range=1h&by=instance&replaced=true")
	mem = chartByID(t, out, "memory")
	names := seriesNames(mem)
	if strings.Join(names, ",") != "replaced 1,web.1" {
		t.Fatalf("series = %v, want replaced 1 and web.1", names)
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
	for i := 0; i < maxInstanceSeries+10; i++ {
		pod := fmt.Sprintf("shop-web-%03d", i)
		series = append(series, fakeSeries{
			Labels: map[string]string{"pod": pod, "label_shpyrd_io_process": "web"},
			Values: []Point{{1000, float64(i)}},
		})
		objs = append(objs, podFor("shop", "web", pod, maxInstanceSeries+10-i))
	}
	prom, _ := newFakeProm(t, fakeAnswer{Match: "container_memory_working_set_bytes", Series: series})
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app}, objs...)

	out := getMetrics(t, s, "?range=1h&by=instance")
	mem := chartByID(t, out, "memory")
	if len(mem.Series) != maxInstanceSeries {
		t.Errorf("series = %d, want the cap of %d", len(mem.Series), maxInstanceSeries)
	}
	if !strings.Contains(mem.Note, "more") {
		t.Errorf("the chart must report what it left out, note = %q", mem.Note)
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

func seriesNames(ch Chart) []string {
	out := make([]string, 0, len(ch.Series))
	for _, s := range ch.Series {
		out = append(out, s.Name)
	}
	return out
}
```

Add `"fmt"`, `"time"`, `corev1 "k8s.io/api/core/v1"` to the test imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/api/ -run TestMetricsBy -v`
Expected: FAIL to compile — `maxInstanceSeries`, `Chart.InstanceCapable` and `Chart.Note` do not exist.

- [ ] **Step 3: Write minimal implementation**

`Chart` gains two fields in `pkg/api/metrics.go`:

```go
// InstanceCapable says whether this chart can be broken down by instance. The
// edge charts cannot: nginx's series name the ingress controller's own pod and
// the backend service, never the backend pod. The UI disables the control
// rather than sending a parameter that would be ignored.
	InstanceCapable bool `json:"instanceCapable"`
	// Note explains a series the chart chose not to draw — instances that were
	// replaced during the window, or the ones past the cap.
	Note string `json:"note,omitempty"`
```

`metricsOptions` gains the rest of the request:

```go
type metricsOptions struct {
	Mode     string // percent (default) or total
	By       string // process (default) or instance
	Process  string // one process type, or "" for all
	Replaced bool   // include instances that no longer exist
}

func (o metricsOptions) byInstance() bool { return o.By == "instance" }
```

Parse them in `appMetrics`, rejecting only what is meaningless:

```go
	opts := metricsOptions{
		Mode:     c.DefaultQuery("mode", "percent"),
		By:       c.DefaultQuery("by", "process"),
		Process:  c.Query("process"),
		Replaced: c.Query("replaced") == "true",
	}
	if opts.Mode != "percent" && opts.Mode != "total" {
		abort(c, http.StatusBadRequest, errors.New("mode must be percent or total"))
		return
	}
	if opts.By != "process" && opts.By != "instance" {
		abort(c, http.StatusBadRequest, errors.New("by must be process or instance"))
		return
	}
```

`agg` is read in Task 5 and deliberately never rejected.

Name resolution reuses the log viewer's function so the two tabs cannot disagree:

```go
// instanceNamer maps a pod to the instance name the Logs tab would show. Pods
// absent from the live list were replaced during the window; they are numbered
// in order of first appearance rather than named, because the name they held
// has since moved to another pod.
//
// Prometheus cannot do this join: kube-state-metrics exposes no annotations on
// a default install, so shpyrd.io/instance is invisible to PromQL.
func (s *Server) instanceNamer(ctx context.Context, app *shpyrdv1.App) func(pod string) string {
	live := map[string]string{}
	// The typed clientset, not the controller-runtime client: pods are ordinary
	// core objects here and this is the same call the log viewer makes.
	if pods, err := s.kube.Kube.CoreV1().Pods(app.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: shpyrdv1.LabelApp + "=" + app.Name,
	}); err == nil {
		live = logs.InstanceNames(pods.Items)
	}
	var mu sync.Mutex
	replaced := map[string]string{}
	// Shared by every chart's goroutine, so the numbering is consistent across
	// charts — and therefore needs the lock.
	return func(pod string) string {
		if n, ok := live[pod]; ok {
			return n
		}
		mu.Lock()
		defer mu.Unlock()
		if n, ok := replaced[pod]; ok {
			return n
		}
		n := fmt.Sprintf("replaced %d", len(replaced)+1)
		replaced[pod] = n
		return n
	}
}
```

Imports needed in `metrics.go`: `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"`, `"shpyrd/pkg/logs"`, `"sync"` (already present).

In `metricqueries.go`, give the cpu, memory and network entries a per-pod form and mark them capable. Group by pod to collapse sidecars into one series per instance:

```go
	// The process label comes along so a series can be traced back to its
	// allocation; grouping by pod collapses several containers into one
	// instance.
	group := "label_shpyrd_io_process"
	if opts.byInstance() {
		group = "pod, label_shpyrd_io_process"
	}
	procFilter := ""
	if opts.Process != "" {
		procFilter = fmt.Sprintf(`,label_shpyrd_io_process=%q`, opts.Process)
	}
	podLabels := fmt.Sprintf(`kube_pod_labels{namespace="%s",label_shpyrd_io_app="%s"%s}`, ns, name, procFilter)
```

Set `LabelKey` to `pod` when `opts.byInstance()`, keep `label_shpyrd_io_process` otherwise, and set `InstanceCapable: true` on cpu, memory and network while leaving throughput, latency and instances at false. Network's two `Fixed` queries become per-pod when asked, with the direction kept in the series name:

```go
			// One chart, two directions: with instances the direction rides in
			// the series name so the chart set stays the same shape.
			Fixed: map[string]string{
				"in":  fmt.Sprintf(`sum by (%s) (rate(container_network_receive_bytes_total{namespace="%s"}[2m]) * on (namespace, pod) group_left (label_shpyrd_io_process) %s)`, group, ns, podLabels),
				"out": fmt.Sprintf(`sum by (%s) (rate(container_network_transmit_bytes_total{namespace="%s"}[2m]) * on (namespace, pod) group_left (label_shpyrd_io_process) %s)`, group, ns, podLabels),
			},
```

In `runChart`, rename via the namer, drop or keep replaced series, and cap:

```go
const maxInstanceSeries = 40

// nameInstances turns pod-keyed series into instance-named ones, applies the
// replaced rule and caps how many series a chart may carry. The cap is what
// stops a fifty-replica project over a week from returning a response no
// browser will chart.
func nameInstances(ch *Chart, namer func(string) string, opts metricsOptions) {
	if !opts.byInstance() {
		return
	}
	kept := make([]Series, 0, len(ch.Series))
	hidden := 0
	for _, sr := range ch.Series {
		name := namer(sr.Name)
		if strings.HasPrefix(name, "replaced ") && !opts.Replaced {
			hidden++
			continue
		}
		sr.Name = name
		kept = append(kept, sr)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Name < kept[j].Name })
	over := 0
	if len(kept) > maxInstanceSeries {
		over = len(kept) - maxInstanceSeries
		kept = kept[:maxInstanceSeries]
	}
	ch.Series = kept
	switch {
	case hidden > 0 && over > 0:
		ch.Note = fmt.Sprintf("%d replaced instances hidden, %d more not shown", hidden, over)
	case hidden > 0:
		ch.Note = fmt.Sprintf("%d replaced instances hidden", hidden)
	case over > 0:
		ch.Note = fmt.Sprintf("%d more instances not shown", over)
	}
}
```

Call `nameInstances(&ch, namer, opts)` before `applyReference`, so the reference is matched against the instance name. For the network chart the raw series name is `in`/`out` rather than a pod, so build those names as `"<pod> in"`; the implementer should pass the namer a pod extracted from the series labels. Concretely: in `runChart`'s `Fixed` branch, when `opts.byInstance()` iterate the raw series per direction and name each `namer(r.Labels["pod"]) + " " + direction`.

The namer is built once per request in `appMetrics` and shared by every chart, so "replaced 1" means the same pod on all of them:

```go
	namer := s.instanceNamer(c.Request.Context(), app)
```

Pass it into the goroutine alongside `opts` and `allocs`. Note it is not safe for concurrent use — it memoises — so guard it with a mutex, or build the replaced numbering once up front. Prefer the mutex; the contention is trivial and the alternative needs every pod name in advance.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/api/ -run TestMetrics -v && go test ./pkg/api/ -race -count=2 && go vet ./...`
Expected: PASS with no race reports — the namer is shared across the chart goroutines, so `-race` is the point here.

- [ ] **Step 5: Commit**

```bash
git add pkg/api/metrics.go pkg/api/metricqueries.go pkg/api/metrics_test.go
git commit -m "feat(api): per-instance metrics series, named not numbered by pod

Queries group by pod and the names come from logs.InstanceNames, the same
function the Logs tab uses, so the two cannot disagree. Prometheus cannot do
this join: kube-state-metrics exposes no annotations on a default install.
Instances that no longer exist become 'replaced N' series, hidden unless asked
for, and a cap keeps a fifty-replica week from returning a response no browser
will chart — both reported in the chart's note rather than dropped silently.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CFQobmze26mnGVmyUF8oVp"
```

---

### Task 5: `agg=none|sum|avg|max`

**Files:**
- Modify: `pkg/api/metrics.go`
- Test: `pkg/api/metrics_test.go`

**Interfaces:**
- Consumes: `metricsOptions`, `Series`, `Point`.
- Produces: `metricsOptions` gains `Agg string`; `func aggregate(series []Series, agg string) []Series`.

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/api/ -run TestMetricsAggregation -v`
Expected: FAIL — `agg` is not read, so all three cases still return two series.

- [ ] **Step 3: Write minimal implementation**

```go
// aggregate collapses per-instance series into one. It is deliberately done
// here rather than in PromQL: the series have already been named, so the result
// is the same whichever instances existed, and one query serves every
// aggregation the UI offers.
func aggregate(series []Series, agg string) []Series {
	if agg == "" || agg == "none" || len(series) == 0 {
		return series
	}
	sums := map[float64]float64{}
	counts := map[float64]int{}
	maxes := map[float64]float64{}
	var order []float64
	for _, s := range series {
		for _, p := range s.Points {
			if _, seen := sums[p[0]]; !seen {
				order = append(order, p[0])
				maxes[p[0]] = p[1]
			}
			sums[p[0]] += p[1]
			counts[p[0]]++
			if p[1] > maxes[p[0]] {
				maxes[p[0]] = p[1]
			}
		}
	}
	sort.Float64s(order)
	out := Series{Name: agg, Points: make([]Point, 0, len(order))}
	for _, ts := range order {
		v := sums[ts]
		switch agg {
		case "avg":
			v = sums[ts] / float64(counts[ts])
		case "max":
			v = maxes[ts]
		}
		out.Points = append(out.Points, Point{ts, v})
	}
	// Every instance of a process shares its allocation, so the reference
	// survives aggregation only when it is the same for all of them.
	ref, burst := series[0].Reference, series[0].Burst
	for _, s := range series[1:] {
		if s.Reference != ref {
			ref, burst = 0, 0
			break
		}
	}
	if agg == "sum" {
		// A sum is measured against the sum of the allocations.
		ref, burst = ref*float64(len(series)), burst*float64(len(series))
	}
	out.Reference, out.Burst = ref, burst
	return []Series{out}
}
```

Read and validate it in `appMetrics`:

```go
	opts.Agg = c.DefaultQuery("agg", "none")
	switch opts.Agg {
	case "none", "sum", "avg", "max":
	default:
		abort(c, http.StatusBadRequest, errors.New("agg must be none, sum, avg or max"))
		return
	}
```

Call it in `runChart` after `nameInstances` and `applyReference`, and only when grouping by instance:

```go
	if opts.byInstance() {
		ch.Series = aggregate(ch.Series, opts.Agg)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/api/ -run TestMetrics -v && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/api/metrics.go pkg/api/metrics_test.go
git commit -m "feat(api): aggregate per-instance series (sum, avg, max)

Collapsed in Go rather than PromQL so one query serves every aggregation the UI
offers. A sum is measured against the sum of the allocations; a mixed set of
allocations drops the reference rather than drawing a line that means nothing.
agg with the default grouping is ignored, an undefined aggregation is a 400.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CFQobmze26mnGVmyUF8oVp"
```

---

### Task 6: The chart component learns references, notes and instance colours

**Files:**
- Modify: `ui/src/components/metric-chart.tsx`
- Modify: `ui/src/lib/api.ts` (types and the metrics call)

**Interfaces:**
- Consumes: the API shape from Tasks 2-5.
- Produces:
  - `ui/src/lib/api.ts`: `Series` gains `reference?: number` and `burst?: number`; `Chart` gains `instanceCapable: boolean` and `note?: string`; `api.metrics(slug, params)` takes an options object.
  - `metric-chart.tsx`: `MetricChart` draws reference and burst lines and renders `note`.

- [ ] **Step 1: Extend the API client**

In `ui/src/lib/api.ts`, extend the metrics types (match the Go JSON tags exactly) and replace the call:

```ts
export type MetricsQuery = {
  range: string;
  process?: string;
  by?: "process" | "instance";
  agg?: "none" | "sum" | "avg" | "max";
  mode?: "percent" | "total";
  replaced?: boolean;
};

  metrics: (slug: string, q: MetricsQuery) => {
    const p = new URLSearchParams({ range: q.range });
    if (q.process) p.set("process", q.process);
    if (q.by) p.set("by", q.by);
    if (q.agg && q.agg !== "none") p.set("agg", q.agg);
    if (q.mode) p.set("mode", q.mode);
    if (q.replaced) p.set("replaced", "true");
    return request<MetricsResponse>(`${project(slug)}/metrics?${p}`);
  },
```

Add `reference?: number` and `burst?: number` to the `Series` type, and `instanceCapable: boolean` plus `note?: string` to `Chart`.

- [ ] **Step 2: Draw the lines and the note**

In `metric-chart.tsx` the 100% line is drawn at line 203 gated on `const percent = chart.unit === "%"` (line 75). Add the absolute-mode equivalents beside it. `metricValue(v, unit)` (from `@/lib/format`) already formats bytes and cores, so reuse it rather than adding a formatter:

```tsx
  // In absolute mode the line is the allocation the series is measured
  // against, which the API sends per series; every instance of a process
  // shares it, so the first series that has one speaks for the chart.
  const allocation = chart.series.find((s) => s.reference)?.reference ?? 0;
  const burst = chart.series.find((s) => s.burst)?.burst ?? 0;
```

and, next to the existing `percent && …` block:

```tsx
                {!percent && allocation > 0 && (
                  <ReferenceLine
                    y={allocation}
                    stroke="oklch(0.65 0.22 25)"
                    strokeDasharray="2 4"
                    label={{
                      value: `Allocated ${metricValue(allocation, chart.unit)}`,
                      position: "insideTopLeft",
                      fontSize: 10,
                    }}
                  />
                )}
                {!percent && burst > 0 && (
                  <ReferenceLine
                    y={burst}
                    stroke="oklch(0.80 0.16 85)"
                    strokeDasharray="1 5"
                    label={{
                      value: `Burst ${metricValue(burst, chart.unit)}`,
                      position: "insideTopLeft",
                      fontSize: 10,
                    }}
                  />
                )}
```

Render the note inside the existing `CardTitle` grid (line 82), under the title row, so a chart that hid series says so where the reader is already looking:

```tsx
          {chart.note && (
            <span className="text-xs font-normal text-muted-foreground">
              {chart.note}
            </span>
          )}
```

Two subtitles in the `subtitles` map (line 33) now describe only the percentage view — `cpu: "of each process allocation; shared sizes can burst above 100%"` and `memory: "of each process allocation"`. Make them honest in both modes: drop the "of each process allocation" framing and say what the chart is, for example `cpu: "per process; shared sizes can burst above their allocation"`. Keep the release markers untouched, and do not touch the tooltip: it renders series names, which by construction never contain a pod name.

- [ ] **Step 3: Verify**

Run: `cd ui && npm run build && npm run lint`
Expected: both clean. There is no UI test harness in this repo, so the build, the lint and reading the diff are the verification — say so plainly in the report rather than claiming test coverage.

- [ ] **Step 4: Commit**

```bash
git add ui/src/lib/api.ts ui/src/components/metric-chart.tsx
git commit -m "feat(ui): metric charts draw the allocation and report what they hid

The API now says what each series is measured against, so absolute charts can
show the allocation without a second request, and a chart that hid replaced
instances or capped its series says so instead of looking incomplete.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CFQobmze26mnGVmyUF8oVp"
```

---

### Task 7: The Metrics tab controls

**Files:**
- Modify: `ui/src/pages/app-detail.tsx` (the `Metrics` component, from line 1058)

**Interfaces:**
- Consumes: `api.metrics(slug, MetricsQuery)`, `Chart.instanceCapable`, `Chart.note`.
- Produces: nothing other tasks use.

- [ ] **Step 1: Add the controls**

Extend the `Metrics` component's state (it currently holds only `range`, line 1059) and add controls beside the existing range picker, using the `Select` primitive already imported in that file. The pattern for each, taking the View control as the worked example:

```tsx
  const [by, setBy] = useState<"process" | "instance">("process");
  const [agg, setAgg] = useState<"none" | "sum" | "avg" | "max">("none");
  const [mode, setMode] = useState<"percent" | "total">("percent");
  const [process, setProcess] = useState("");
  const [replaced, setReplaced] = useState(false);
```

```tsx
        <Select value={by} onValueChange={(v) => setBy(v as "process" | "instance")}>
          <SelectTrigger className="w-40" size="sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="process">By process</SelectItem>
            <SelectItem value="instance">By instance</SelectItem>
          </SelectContent>
        </Select>
```

The remaining controls follow that shape:

- **Process**: All, plus one entry per key of `app.status.processes` (already available to the component).
- **View**: By process (default) / By instance.
- **Aggregation**: only enabled when View is By instance — None, Sum, Average, Max.
- **Units**: Percentage (default) / Total.
- **Include replaced instances**: a checkbox, only when View is By instance.

Add `15m` to the range picker as "Last 15 minutes". Include every state value in the TanStack Query key so a change refetches:

```tsx
  const m = useQuery({
    queryKey: ["metrics", app.slug, range, by, agg, mode, process, replaced],
    queryFn: () => api.metrics(app.slug, { range, by, agg, mode, process, replaced }),
    refetchInterval: 30_000,
    enabled: config.data?.metrics !== false,
  });
```

Disable the instance controls on charts the server says cannot honour them — the flag is per chart, so the simplest honest treatment is to render the controls once and note on the two edge charts that they are always aggregate. Update the existing explanatory copy, which currently says "CPU and memory as a percentage of each process allocation", so it still describes what the selected units actually show.

- [ ] **Step 2: Verify**

Run: `cd ui && npm run build && npm run lint`
Expected: both clean, and the build should not have grown a new chunk — this is existing-component work.

- [ ] **Step 3: Commit**

```bash
git add ui/src/pages/app-detail.tsx
git commit -m "feat(ui): instance, aggregation and units controls on the Metrics tab

Every control is in the query key, so changing one refetches. The two charts
measured at the edge say they are always aggregate rather than offering a
control the server would ignore.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CFQobmze26mnGVmyUF8oVp"
```

---

### Task 8: Verify against the cluster, then close the RFC

Fakes cannot tell you whether the PromQL is valid, whether the joins return what they should against real kube-state-metrics, or whether a real project's instances line up with the Logs tab.

**Files:**
- Modify: `rfcs/0027-application-metrics-v2.md`, `rfcs/README.md`

- [ ] **Step 1: Get a project with real metrics**

The kind cluster `shpyrd` runs on 8080/8443. `make dev-deploy` is **broken on this machine** — `kind load docker-image` fails with `unknown containerd config version: 4` (kind v0.30.0 against a v1.37.0 node image), and piping make hides the failure because the pipe's exit code wins. Use the working path:

```sh
make dev-image
docker save shpyrd-server:dev -o /tmp/s.tar
for n in shpyrd-control-plane shpyrd-worker; do
  docker cp /tmp/s.tar $n:/s.tar && docker exec $n ctr -n k8s.io images import /s.tar
done
./bin/shpyrd cluster init --context kind-shpyrd --only shpyrd --set SHPYRD_SERVER_IMAGE=shpyrd-server:dev
kubectl --context kind-shpyrd -n shpyrd-system rollout restart deployment/shpyrd-server
```

Deploy a project with two process types and more than one web replica, so instance series are meaningful — `examples/hello-docker` declares `web` and `worker` and needs its volume first:

```sh
./bin/shpyrd projects create hello-docker
cd examples/hello-docker
../../bin/shpyrd volumes create data --size 1Gi
../../bin/shpyrd deploy
```

Note that `web` mounts a single-instance volume, so scale `worker` rather than `web` for a multi-instance chart.

- [ ] **Step 2: Check each claim the fakes could not**

With a port-forward to the server, walk these and record what you saw:

1. `?range=1h` returns the same chart set and series names as before this branch.
2. `?range=1h&by=instance` names series `worker.1`, `worker.2` — and those names **match the Logs tab** for the same pods.
3. `?range=15m` works and its step is 60.
4. `?mode=total` shows bytes and cores with a `reference` equal to the size's memory (64 MiB for `shared-s`) and CPU request.
5. `?by=instance&agg=sum` returns one series whose values equal the sum of the per-instance series.
6. Delete one worker pod, wait for its replacement, then `?by=instance&replaced=true` — the dead pod appears as `replaced 1` and the live ones keep their names.
7. `?by=instance&process=web` returns only web instances, and the throughput and latency charts still return their aggregate series rather than an error.
8. No response anywhere contains a pod name: `curl … | grep -c 'hello-docker-worker-'` must be 0.
9. The Metrics tab renders all of it, with the allocation line visible in Total mode.

A Prometheus port-forward (`kubectl -n monitoring port-forward svc/monitoring-prometheus 19090:9090`) is useful for checking a query by hand when a chart looks wrong. Note that a port-forward is not a usable data path under load, but metrics queries are small.

Record failures as fixes in this task rather than moving on.

- [ ] **Step 3: Confirm the suite**

```sh
go test ./... && go vet ./... && go build ./... && cd ui && npm run build && npm run lint
```

- [ ] **Step 4: Close the RFC**

Set the status to `implemented`, add the Implementation History entry naming what the cluster run covered, and add an Implementation status section for anything the text still promises that the code does not do — the unstable-instance-name limitation belongs there, since it is a known gap rather than a bug. Update the index line in `rfcs/README.md`.

Do not write `implemented` unless Step 2 actually passed; if part of it did not, say so in the status ("implemented, gaps") and list what in Implementation status.

- [ ] **Step 5: Commit**

```bash
git add rfcs/0027-application-metrics-v2.md rfcs/README.md
git commit -m "docs(rfcs): RFC-0027 implemented

Verified on the kind dev cluster against examples/hello-docker.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CFQobmze26mnGVmyUF8oVp"
```

---

## Notes for the reviewer

- The only structural change is `metricqueries.go`; `metrics.go` keeps the handler, the types and the series post-processing. Task 1 moves code with no behaviour change so the rest of the branch has a clean baseline and a test harness to argue with.
- `aggregate` and `nameInstances` are deliberately Go rather than PromQL. Naming has to be (kube-state-metrics exposes no annotations), and once the series are named, aggregating them there means one query serves every aggregation instead of four query shapes.
- The instance namer memoises `replaced N` numbering and is shared by the chart goroutines, so it needs a mutex. Task 4 says so; `-race` in that task is aimed at exactly this.
