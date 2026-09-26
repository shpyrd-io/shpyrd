package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	// InstanceCapable says whether this chart can be broken down by instance.
	// The edge charts cannot: nginx's series name the ingress controller's own
	// pod and the backend service, never the backend pod. The UI disables the
	// control rather than sending a parameter that would be ignored.
	InstanceCapable bool `json:"instanceCapable"`
	// Note explains a series the chart chose not to draw — instances that were
	// replaced during the window, or the ones past the cap.
	Note string `json:"note,omitempty"`
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

	opts := metricsOptions{
		Mode:     c.DefaultQuery("mode", "percent"),
		By:       c.DefaultQuery("by", "process"),
		Process:  c.Query("process"),
		Replaced: c.Query("replaced") == "true",
		Agg:      c.DefaultQuery("agg", "none"),
	}
	if opts.Mode != "percent" && opts.Mode != "total" {
		abort(c, http.StatusBadRequest, errors.New("mode must be percent or total"))
		return
	}
	if opts.By != "process" && opts.By != "instance" {
		abort(c, http.StatusBadRequest, errors.New("by must be process or instance"))
		return
	}
	switch opts.Agg {
	case "none", "sum", "avg", "max":
	default:
		abort(c, http.StatusBadRequest, errors.New("agg must be none, sum, avg or max"))
		return
	}
	// Percent mode never draws a reference line (applyReference discards it
	// on its first line), so skip the catalog fetch and a Resolve per
	// process on the path every open Metrics tab repeats every 30 seconds.
	var allocs map[string]allocation
	if opts.absolute() {
		allocs = s.allocations(c.Request.Context(), app)
	}
	// One namer for the whole request, so "replaced 1" means the same pod on
	// every chart. Only the instance breakdown needs it, and listing the pods
	// is a call the by-process default should not pay for on a path every open
	// Metrics tab repeats every 30 seconds.
	var namer *instanceNamer
	if opts.byInstance() {
		namer = s.newInstanceNamer(c.Request.Context(), app)
	}
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
			resp.Charts[i] = s.runChart(c.Request.Context(), q, start, end, step, allocs, opts, namer)
		}(i, q)
	}
	wg.Wait()
	c.JSON(http.StatusOK, resp)
}

func (s *Server) runChart(ctx context.Context, q chartQuery, start, end time.Time, step time.Duration, allocs map[string]allocation, opts metricsOptions, namer *instanceNamer) Chart {
	ch := q.Chart
	ch.Series = []Series{}
	if len(q.Fixed) > 0 {
		names := make([]string, 0, len(q.Fixed))
		for n := range q.Fixed {
			names = append(names, n)
		}
		sort.Strings(names)
		perInstance := ch.InstanceCapable && opts.byInstance()
		for _, n := range names {
			if perInstance {
				// Network is the only chart of this shape that splits: its
				// sub-queries are directions, not pods, so each direction
				// contributes one series per instance and carries the
				// direction as a suffix nameInstances keeps.
				raw, err := s.prom.QueryRangeSeries(ctx, q.Fixed[n], start, end, step)
				if err != nil {
					// Whatever the earlier direction appended is still keyed by
					// pod and nameInstances is no longer going to run, so the
					// series go with the error. A chart reporting a failure has
					// nothing to draw anyway, and this is the one branch where
					// keeping them would put a pod name in the response.
					ch.Series, ch.Error = []Series{}, err.Error()
					return ch
				}
				for _, r := range raw {
					ch.Series = append(ch.Series, Series{Name: r.Labels["pod"] + " " + n, Points: r.Points})
				}
				continue
			}
			pts, err := s.prom.QueryRange(ctx, q.Fixed[n], start, end, step)
			if err != nil {
				ch.Error = err.Error()
				return ch
			}
			ch.Series = append(ch.Series, Series{Name: n, Points: pts})
		}
		nameInstances(&ch, namer, opts)
		applyReference(&ch, allocs, opts)
		if opts.byInstance() {
			ch.Series = aggregate(ch.Series, opts.Agg)
		}
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
	nameInstances(&ch, namer, opts)
	applyReference(&ch, allocs, opts)
	if opts.byInstance() {
		ch.Series = aggregate(ch.Series, opts.Agg)
	}
	return ch
}

// maxInstances is the most instances one chart may carry. Instances, not
// series: the network chart draws two series for each of them.
const maxInstances = 40

// nameInstances turns pod-keyed series into instance-named ones, applies the
// replaced rule and caps how many instances a chart may carry. The cap is what
// stops a fifty-replica project over a week from returning a response no
// browser will chart.
//
// A series name is the pod, optionally followed by a word the chart added to
// tell two series of the same instance apart — the network chart's direction.
// That suffix survives the renaming, so "web.1 in" and "web.1 out" both count
// as the one instance when a replaced one is reported.
func nameInstances(ch *Chart, namer *instanceNamer, opts metricsOptions) {
	if !opts.byInstance() || !ch.InstanceCapable {
		return
	}
	if namer.err != nil {
		// Without the live pods every instance falls through to "replaced",
		// which is hidden by default, so the chart would quietly claim the
		// project churned from end to end instead of admitting the lookup
		// failed. Say so on the channel the UI already renders.
		ch.Series, ch.Error = []Series{}, namer.err.Error()
		return
	}
	// Number the replaced pods off a stable order first: Prometheus promises no
	// particular order, and every chart's goroutine shares the namer, so
	// "replaced 1" must not depend on which chart's answer arrived first.
	sort.Slice(ch.Series, func(i, j int) bool { return ch.Series[i].Name < ch.Series[j].Name })
	// A series carries its instance and its ordering key alongside it, because
	// neither the ordinal nor a live instance's precedence survives in the name
	// as a plain string sort would read it.
	type ordered struct {
		Series
		instance string
		replaced bool
		process  string
		ordinal  int
	}
	kept := make([]ordered, 0, len(ch.Series))
	hidden := map[string]bool{}
	for _, sr := range ch.Series {
		pod, suffix := sr.Name, ""
		if i := strings.IndexByte(pod, ' '); i > 0 {
			pod, suffix = pod[:i], pod[i:]
		}
		name := namer.name(pod)
		replaced := strings.HasPrefix(name, replacedPrefix)
		if replaced && !opts.Replaced {
			hidden[name] = true
			continue
		}
		sr.Name = name + suffix
		process, ordinal := splitInstance(name)
		kept = append(kept, ordered{Series: sr, instance: name, replaced: replaced, process: process, ordinal: ordinal})
	}
	// Live instances first, so the cap can never drop one in favour of a pod
	// that no longer exists — asking to see the replaced ones must not cost the
	// series that matter. Then by process, then by ordinal as a number, because
	// a string sort puts web.10 before web.2 and would make both the legend and
	// the cap's choice of survivors look arbitrary. The name breaks the last
	// tie, which is the network chart's two directions.
	sort.SliceStable(kept, func(i, j int) bool {
		a, b := kept[i], kept[j]
		switch {
		case a.replaced != b.replaced:
			return !a.replaced
		case a.process != b.process:
			return a.process < b.process
		case a.ordinal != b.ordinal:
			return a.ordinal < b.ordinal
		}
		return a.Name < b.Name
	})
	// The cap and both counts are about instances, so a note's number means the
	// same thing on every chart: counting series would make the cap admit half
	// as many instances on network as on cpu, and call five missing instances
	// ten. Every series of one instance is adjacent after the sort, so the cap
	// keeps or drops an instance whole.
	shown := map[string]bool{}
	dropped := map[string]bool{}
	ch.Series = make([]Series, 0, len(kept))
	for _, k := range kept {
		if !shown[k.instance] {
			if len(shown) == maxInstances {
				dropped[k.instance] = true
				continue
			}
			shown[k.instance] = true
		}
		ch.Series = append(ch.Series, k.Series)
	}
	var parts []string
	if len(hidden) > 0 {
		parts = append(parts, plural(len(hidden), "replaced instance hidden", "replaced instances hidden"))
	}
	if len(dropped) > 0 {
		parts = append(parts, plural(len(dropped), "more instance not shown", "more instances not shown"))
	}
	ch.Note = strings.Join(parts, ", ")
}

// splitInstance takes an instance name apart for ordering: "web.10" is process
// web, ordinal 10, and "replaced 3" is ordinal 3 with no process.
func splitInstance(name string) (process string, ordinal int) {
	if num, ok := strings.CutPrefix(name, replacedPrefix); ok {
		n, _ := strconv.Atoi(num)
		return "", n
	}
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		if n, err := strconv.Atoi(name[i+1:]); err == nil {
			return name[:i], n
		}
	}
	return name, 0
}

// plural counts a noun, because "1 replaced instances hidden" reads like a bug
// in the page rather than a note about the data.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// replacedPrefix names an instance that is no longer live. It is a prefix
// rather than a whole name because the pods are numbered.
const replacedPrefix = "replaced "

// instanceNamer names pods for one request. Exactly one of the fields is set:
// err records a failure to list the live pods, which is not a detail the charts
// can paper over — with no live list every instance looks replaced — so a chart
// reports the error and never calls name.
type instanceNamer struct {
	name func(pod string) string
	err  error
}

// newInstanceNamer maps a pod to the instance name the Logs tab would show.
// Pods absent from the live list were replaced during the window; they are
// numbered in order of first appearance rather than named, because the name they
// held has since moved to another pod.
//
// Prometheus cannot do this join: kube-state-metrics exposes no annotations on
// a default install, so shpyrd.io/instance is invisible to PromQL.
func (s *Server) newInstanceNamer(ctx context.Context, app *shpyrdv1.App) *instanceNamer {
	// The typed clientset, not the controller-runtime client: pods are ordinary
	// core objects here. Selector and all, this is the call the log viewer
	// makes — a pod with no process label, a build or a one-off, is not an
	// instance there and must not become one here.
	pods, err := s.kube.Kube.CoreV1().Pods(app.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: shpyrdv1.LabelApp + "=" + app.Name + "," + shpyrdv1.LabelProcess,
	})
	if err != nil {
		return &instanceNamer{err: fmt.Errorf("listing the project's instances: %w", err)}
	}
	live := InstanceNames(pods.Items)
	var mu sync.Mutex
	replaced := map[string]string{}
	// Shared by every chart's goroutine, so the numbering is consistent across
	// charts — and therefore needs the lock.
	return &instanceNamer{name: func(pod string) string {
		if n, ok := live[pod]; ok {
			return n
		}
		mu.Lock()
		defer mu.Unlock()
		if n, ok := replaced[pod]; ok {
			return n
		}
		n := fmt.Sprintf("%s%d", replacedPrefix, len(replaced)+1)
		replaced[pod] = n
		return n
	}}
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

// directionOf reads the network chart's direction off the end of a series
// name, if it carries one. It is deliberately not "whatever follows the first
// space": a replaced instance is itself named "replaced 3", so a network
// series for one reads "replaced 3 in" and a first-space split would cut it
// as "replaced" plus "3 in" instead of the instance plus its direction.
func directionOf(name string) string {
	if i := strings.LastIndexByte(name, ' '); i > 0 {
		if last := name[i+1:]; last == "in" || last == "out" {
			return last
		}
	}
	return ""
}

// aggregate collapses a chart's per-instance series into one per direction —
// cpu and memory have no direction, so they collapse to one series overall,
// while network's "in" and "out" are aggregated separately so a sum or an
// average never mixes inbound and outbound traffic into a line that means
// nothing. It runs here rather than in PromQL: the series have already been
// named, so the result is the same whichever instances existed, and one query
// serves every aggregation the UI offers.
func aggregate(series []Series, agg string) []Series {
	if agg == "" || agg == "none" || len(series) == 0 {
		return series
	}
	groups := map[string][]Series{}
	var directions []string
	for _, s := range series {
		dir := directionOf(s.Name)
		if _, seen := groups[dir]; !seen {
			directions = append(directions, dir)
		}
		groups[dir] = append(groups[dir], s)
	}
	sort.Strings(directions)
	out := make([]Series, 0, len(directions))
	for _, dir := range directions {
		name := agg
		if dir != "" {
			name = agg + " " + dir
		}
		out = append(out, aggregateGroup(groups[dir], agg, name))
	}
	return out
}

// aggregateGroup combines the series of one direction (or the whole chart,
// for cpu and memory) into a single named series.
func aggregateGroup(series []Series, agg, name string) Series {
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
	out := Series{Name: name, Points: make([]Point, 0, len(order))}
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
	return out
}
