package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"

	shpyrdv1 "shpyrd/api/v1alpha1"
)

// Chart is one panel of the app metrics view.
type Chart struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Unit   string   `json:"unit"`   // rps, ms, cores, bytes, count, bytes/s
	Kind   string   `json:"kind"`   // line or stacked
	Series []Series `json:"series"` // one or more named series
	Error  string   `json:"error,omitempty"`
}

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

// ReleaseMarker lets the UI draw deploy lines on the charts.
type ReleaseMarker struct {
	Number int     `json:"number"`
	Time   float64 `json:"time"`
	Label  string  `json:"label"`
}

// MetricsResponse holds the charts for an app.
type MetricsResponse struct {
	Range    string          `json:"range"`
	Step     int             `json:"step"`
	Charts   []Chart         `json:"charts"`
	Releases []ReleaseMarker `json:"releases"`
}

var rangeOptions = map[string]time.Duration{
	"15m": 15 * time.Minute,
	"1h":  time.Hour,
	"6h":  6 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
}

// stepFor returns a step duration that targets ~60 points per range, but
// floors to one minute to avoid sub-minute intervals that don't benefit from
// higher precision. Ranges shorter than an hour therefore yield fewer points
// (15m and 1h both use a 60-second step), which is an acceptable trade-off
// to keep weeks of per-instance series in a response a browser can chart.
func stepFor(d time.Duration) time.Duration {
	step := d / 60
	if step < time.Minute {
		step = time.Minute
	}
	return step
}

// chartQuery describes how to build one chart. Multi-series queries return
// one series per label value (labelKey); fixed queries produce a series
// each with a given name.
type chartQuery struct {
	Chart
	// labelKey: name series after this label of a single query (Query).
	Query    string
	LabelKey string
	// nameMap post-processes label values (e.g. strip a prefix).
	nameMap func(string) string
	// fixed: several queries, each one series.
	Fixed map[string]string
	// fallback runs when Query returns nothing (e.g. kube-state-metrics
	// label allowlist not applied yet, or pods without limits).
	Fallback     string
	FallbackName string
	FallbackUnit string
}

func (s *Server) appMetrics(c *gin.Context) {
	if s.prom == nil {
		abort(c, http.StatusNotImplemented, errors.New("metrics are not configured"))
		return
	}
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	rng := c.DefaultQuery("range", "1h")
	dur, ok := rangeOptions[rng]
	if !ok {
		abort(c, http.StatusBadRequest, errors.New("range must be one of 15m, 1h, 6h, 24h, 7d"))
		return
	}
	end := time.Now().Truncate(time.Minute)
	start := end.Add(-dur)
	step := stepFor(dur)

	mode := c.DefaultQuery("mode", "percent")
	if mode != "percent" && mode != "total" {
		abort(c, http.StatusBadRequest, errors.New("mode must be percent or total"))
		return
	}
	allocs := s.allocations(c.Request.Context(), app)
	opts := metricsOptions{Mode: mode}
	queries := chartQueries(app, opts)
	resp := MetricsResponse{Range: rng, Step: int(step.Seconds()), Charts: make([]Chart, len(queries)), Releases: []ReleaseMarker{}}
	for _, r := range app.Status.Releases {
		if r.CreatedAt.Time.After(start) {
			resp.Releases = append(resp.Releases, ReleaseMarker{Number: r.Number, Time: float64(r.CreatedAt.Unix()), Label: "v" + fmt.Sprint(r.Number)})
		}
	}

	var wg sync.WaitGroup
	for i, q := range queries {
		wg.Add(1)
		go func(i int, q chartQuery) {
			defer wg.Done()
			resp.Charts[i] = s.runChart(c.Request.Context(), q, start, end, step, allocs, opts)
		}(i, q)
	}
	wg.Wait()
	c.JSON(http.StatusOK, resp)
}

func (s *Server) runChart(ctx context.Context, q chartQuery, start, end time.Time, step time.Duration, allocs map[string]allocation, opts metricsOptions) Chart {
	ch := q.Chart
	ch.Series = []Series{}
	if len(q.Fixed) > 0 {
		names := make([]string, 0, len(q.Fixed))
		for n := range q.Fixed {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			pts, err := s.prom.QueryRange(ctx, q.Fixed[n], start, end, step)
			if err != nil {
				ch.Error = err.Error()
				return ch
			}
			ch.Series = append(ch.Series, Series{Name: n, Points: pts})
		}
		applyReference(&ch, allocs, opts)
		return ch
	}
	raw, err := s.prom.QueryRangeSeries(ctx, q.Query, start, end, step)
	if err != nil {
		ch.Error = err.Error()
		return ch
	}
	if len(raw) == 0 && q.Fallback != "" {
		pts, err := s.prom.QueryRange(ctx, q.Fallback, start, end, step)
		if err != nil {
			ch.Error = err.Error()
			return ch
		}
		if len(pts) > 0 {
			ch.Series = append(ch.Series, Series{Name: q.FallbackName, Points: pts})
			if q.FallbackUnit != "" {
				ch.Unit = q.FallbackUnit
			}
		}
		applyReference(&ch, allocs, opts)
		return ch
	}
	for _, r := range raw {
		name := r.Labels[q.LabelKey]
		if q.nameMap != nil {
			name = q.nameMap(name)
		}
		if name == "" {
			name = "all"
		}
		ch.Series = append(ch.Series, Series{Name: name, Points: r.Points})
	}
	sort.Slice(ch.Series, func(i, j int) bool { return ch.Series[i].Name < ch.Series[j].Name })
	applyReference(&ch, allocs, opts)
	return ch
}

// applyReference fills in the line a series is measured against. Only the
// charts that measure a share of an allocation have one, and only in
// absolute mode — in percentage mode the line is 100%.
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
