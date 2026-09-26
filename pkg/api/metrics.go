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
	// pod and process are the series' identity as Prometheus reported it,
	// carried alongside the display name rather than parsed back out of it.
	// Reading them off the name was wrong twice over: the name of a replaced
	// instance is "replaced 1", which no longer says which process it was, and
	// a pod in the name is a pod name one missed branch away from the response
	// (RFC-0011). Both stay unexported, so no amount of new marshalling can
	// put a pod in the JSON. pod is empty for a series that is not one
	// instance — the by-process default, and the namespace-wide fallback.
	pod     string
	process string
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
	// One namer for the whole request, because "replaced 2" has to mean the
	// same pod on every chart of a response. Only the instance breakdown needs
	// it, and listing the pods is a call the by-process default should not pay
	// for on a path every open Metrics tab repeats every 30 seconds.
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
			resp.Charts[i] = s.runChart(c.Request.Context(), q, start, end, step, opts)
		}(i, q)
	}
	wg.Wait()
	// Naming, the reference and the aggregation all run here rather than in
	// each goroutine: they need the whole response. The instance names do
	// because the charts do not see the same pods (cpu and memory filter on
	// container!="", the network query does not), so no single chart knows the
	// set that has to be numbered consistently; the other two follow it
	// because they read the names it assigns.
	finishCharts(resp.Charts, namer, allocs, opts)
	c.JSON(http.StatusOK, resp)
}

func (s *Server) runChart(ctx context.Context, q chartQuery, start, end time.Time, step time.Duration, opts metricsOptions) Chart {
	ch := q.Chart
	ch.Series = []Series{}
	perInstance := ch.InstanceCapable && opts.byInstance()
	if len(q.Fixed) > 0 {
		names := make([]string, 0, len(q.Fixed))
		for n := range q.Fixed {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if perInstance {
				// Network is the only chart of this shape that splits: its
				// sub-queries are directions, not pods, so each direction
				// contributes one series per instance and carries the
				// direction as the suffix the naming pass appends to the
				// instance name.
				raw, err := s.prom.QueryRangeSeries(ctx, q.Fixed[n], start, end, step)
				if err != nil {
					// Whatever the earlier direction appended is keyed by pod
					// and will never be named now, so the series go with the
					// error: a chart reporting a failure has nothing to draw,
					// and half a direction drawn under bare "in" would read as
					// real data.
					ch.Series, ch.Error = []Series{}, err.Error()
					return ch
				}
				for _, r := range raw {
					ch.Series = append(ch.Series, Series{Name: n, pod: r.Labels["pod"], process: r.Labels[processLabel], Points: r.Points})
				}
				continue
			}
			pts, err := s.prom.QueryRange(ctx, q.Fixed[n], start, end, step)
			if err != nil {
				ch.Error = err.Error()
				return ch
			}
			name, process := n, ""
			if ch.ID == "network" && opts.Process != "" {
				// The query already filters to this one process (it joins the
				// pod labels in whenever a process is selected), but "in" and
				// "out" say nothing about that, so selecting a process left
				// this chart looking unfiltered even though it wasn't. Latency
				// and throughput share this Fixed shape but never carry a
				// process, so the rename is scoped to network by chart ID
				// rather than to every fixed-query chart.
				name, process = opts.Process+" "+n, opts.Process
			}
			ch.Series = append(ch.Series, Series{Name: name, process: process, Points: pts})
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
			// No pod: this is the namespace-wide total the chart falls back to
			// when nothing carries a request, which the naming pass leaves
			// alone rather than calling it an instance.
			ch.Series = append(ch.Series, Series{Name: q.FallbackName, Points: pts})
			if q.FallbackUnit != "" {
				ch.Unit = q.FallbackUnit
			}
		}
		return ch
	}
	for _, r := range raw {
		if perInstance && r.Labels["pod"] != "" {
			// Named later, once every chart has answered: the name depends on
			// pods this chart may not have seen. Until then the series is
			// identified by the labels it came with and nothing else.
			ch.Series = append(ch.Series, Series{pod: r.Labels["pod"], process: r.Labels[processLabel], Points: r.Points})
			continue
		}
		name := r.Labels[q.LabelKey]
		if q.nameMap != nil {
			name = q.nameMap(name)
		}
		if name == "" {
			name = "all"
		}
		ch.Series = append(ch.Series, Series{Name: name, process: r.Labels[processLabel], Points: r.Points})
	}
	sort.Slice(ch.Series, func(i, j int) bool { return ch.Series[i].Name < ch.Series[j].Name })
	return ch
}

// maxInstances is the most instances one chart may carry. Instances, not
// series: the network chart draws two series for each of them.
const maxInstances = 40

// finishCharts turns the raw answers into what the response promises: instance
// names, the allocation each series is measured against and the aggregation
// that was asked for, in that order because each reads what the one before it
// wrote.
//
// It runs after every chart has answered, not inside the goroutines, because
// the instance names are a property of the whole response rather than of one
// chart. See replacedNames.
func finishCharts(charts []Chart, namer *instanceNamer, allocs map[string]allocation, opts metricsOptions) {
	replaced := replacedNames(charts, namer, opts)
	for i := range charts {
		ch := &charts[i]
		nameInstances(ch, namer, replaced, opts)
		applyReference(ch, allocs, opts)
		// Aggregation is an instance-breakdown control, so a chart that never
		// broke down by instance in the first place has nothing for it to
		// collapse. Without this guard, by=instance&agg=sum on latency folded
		// p50, p95 and p99 into one series named "sum" — blending percentiles,
		// which is meaningless, on a chart the UI already marks as not
		// instance capable — and on throughput it blended status classes.
		if opts.byInstance() && ch.InstanceCapable {
			ch.Series = aggregate(ch.Series, opts.Agg)
		}
	}
}

// replacedNames numbers the pods that appear in the metric data but are no
// longer live, off the union of every chart's pods and a single sorted list of
// them.
//
// What that buys, and it is worth being exact because an earlier version
// claimed more: within one response "replaced 2" is the same pod on every
// chart, and two requests over the same data number the same pods the same
// way. Numbering per chart could not manage even that — cpu and memory filter
// on container!="" while the network query does not, so the charts see
// different sets, and whichever goroutine reached a shared counter first
// decided the number. What it is not: an order anyone can read meaning into
// (pod names end in a hash), nor stable when the set itself changes, since a
// replacement sorting earlier renumbers every pod after it.
func replacedNames(charts []Chart, namer *instanceNamer, opts metricsOptions) map[string]string {
	if !opts.byInstance() || namer == nil || namer.err != nil {
		return nil
	}
	var dead []string
	seen := map[string]bool{}
	for _, ch := range charts {
		if !ch.InstanceCapable {
			continue
		}
		for _, sr := range ch.Series {
			if sr.pod == "" || seen[sr.pod] {
				continue
			}
			seen[sr.pod] = true
			if _, live := namer.live[sr.pod]; !live {
				dead = append(dead, sr.pod)
			}
		}
	}
	sort.Strings(dead)
	out := make(map[string]string, len(dead))
	for i, pod := range dead {
		out[pod] = fmt.Sprintf("%s%d", replacedPrefix, i+1)
	}
	return out
}

// nameInstances turns one chart's pod-keyed series into instance-named ones,
// applies the replaced rule and caps how many instances the chart may carry.
// The cap is what stops a fifty-replica project over a week from returning a
// response no browser will chart.
//
// A per-instance series holds its pod alongside a name that is so far only the
// word the chart added to tell two series of the same instance apart — the
// network chart's direction. That word becomes the suffix, so "web.1 in" and
// "web.1 out" both count as the one instance when a replaced one is reported.
func nameInstances(ch *Chart, namer *instanceNamer, replaced map[string]string, opts metricsOptions) {
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
	// A series with no pod is not an instance — the namespace-wide fallback is
	// the one that reaches here — so it keeps the name it was given and is
	// counted against nothing.
	var pass, pods []Series
	for _, sr := range ch.Series {
		if sr.pod == "" {
			pass = append(pass, sr)
			continue
		}
		pods = append(pods, sr)
	}
	// Prometheus promises no particular order, and the sort below is stable, so
	// give it a deterministic starting order: by pod, then by the direction
	// suffix so "in" precedes "out" whatever order the two sub-queries
	// returned in.
	sort.Slice(pods, func(i, j int) bool {
		if pods[i].pod != pods[j].pod {
			return pods[i].pod < pods[j].pod
		}
		return pods[i].Name < pods[j].Name
	})
	// A series carries its instance and its ordering key alongside it, because
	// neither the ordinal nor a live instance's precedence survives in the name
	// as a plain string sort would read it.
	type ordered struct {
		Series
		instance string
		replaced bool
		group    string
		ordinal  int
	}
	kept := make([]ordered, 0, len(pods))
	hidden := map[string]bool{}
	for _, sr := range pods {
		name, live := namer.live[sr.pod]
		if !live {
			name = replaced[sr.pod]
		}
		if !live && !opts.Replaced {
			hidden[name] = true
			continue
		}
		if suffix := sr.Name; suffix != "" {
			sr.Name = name + " " + suffix
		} else {
			sr.Name = name
		}
		group, ordinal := splitInstance(name)
		kept = append(kept, ordered{Series: sr, instance: name, replaced: !live, group: group, ordinal: ordinal})
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
		case a.group != b.group:
			return a.group < b.group
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
	ch.Series = append(make([]Series, 0, len(pass)+len(kept)), pass...)
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

// splitInstance takes an instance name apart for ordering: "web.10" sorts
// under web at ordinal 10, and "replaced 3" at ordinal 3 under no process at
// all. It reads the display name on purpose — this is the order the legend is
// drawn in, and nothing downstream depends on what it returns.
func splitInstance(name string) (group string, ordinal int) {
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

// instanceNamer holds the live pods of one request. Exactly one of the fields
// is set: err records a failure to list them, which is not a detail the charts
// can paper over — with no live list every instance looks replaced — so a chart
// reports the error and reads live not at all.
//
// It is a plain map rather than the memoising function it used to be: every
// chart's goroutine shared that function, and a counter inside it made
// "replaced 1" depend on which chart's Prometheus answer came back first. The
// numbering now happens once, after every chart has answered, in
// replacedNames.
type instanceNamer struct {
	live map[string]string
	err  error
}

// newInstanceNamer lists the live pods and maps each to the instance name the
// Logs tab would show. Pods in the metric data but absent from this list were
// replaced during the window; replacedNames numbers those instead, because the
// name they held has since moved to another pod.
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
	return &instanceNamer{live: InstanceNames(pods.Items)}
}

// applyReference fills in the line a series is measured against. Only the
// charts that measure a share of an allocation have one, and only in
// absolute mode — in percentage mode the line is 100%.
func applyReference(ch *Chart, allocs map[string]allocation, opts metricsOptions) {
	if !opts.absolute() {
		return
	}
	for i := range ch.Series {
		// The process comes off the series' own label. Recovering it from the
		// display name instead cost every replaced instance its reference:
		// "replaced 1" is not a process anyone allocated, so the lookup missed
		// and the whole chart then lost its line to the agreement rule in
		// aggregateGroup — ticking "Replaced: shown" silently erased the
		// allocation line.
		a, ok := allocs[ch.Series[i].process]
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

// directionOf reads the network chart's direction off the end of a series
// name, if it carries one. It is deliberately not "whatever follows the first
// space": a replaced instance is itself named "replaced 3", so a network
// series for one reads "replaced 3 in" and a first-space split would cut it
// as "replaced" plus "3 in" instead of the instance plus its direction.
//
// It knows only "in" and "out" — network's own suffixes — rather than
// treating any trailing word as a direction. That is a closed set today, but
// a future chart adding a different per-instance suffix would otherwise fold
// silently into the unsuffixed group instead of getting its own.
func directionOf(name string) string {
	if i := strings.LastIndexByte(name, ' '); i > 0 {
		if last := name[i+1:]; last == "in" || last == "out" {
			return last
		}
	}
	return ""
}

// aggregate collapses a chart's per-instance series into one per process and
// direction. Both boundaries are there because crossing either produces a
// number that is not a quantity: network's "in" and "out" summed together
// measure nothing, and two process types at different sizes summed in percent
// mode reported 170% — 90% of one allocation plus 80% of another — under a
// legend that no longer said which processes made it. So instances collapse,
// and nothing else does: a project with web and worker gets "sum web" and
// "sum worker", which is also what lets each keep the reference line its own
// allocation gives it.
//
// It runs here rather than in PromQL because one query then serves every
// aggregation the UI offers, and because the series have already been named,
// so the result does not depend on which instances happened to exist.
func aggregate(series []Series, agg string) []Series {
	if agg == "" || agg == "none" || len(series) == 0 {
		return series
	}
	type group struct{ process, direction string }
	groups := map[group][]Series{}
	var order []group
	for _, s := range series {
		g := group{s.process, directionOf(s.Name)}
		if _, seen := groups[g]; !seen {
			order = append(order, g)
		}
		groups[g] = append(groups[g], s)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].process != order[j].process {
			return order[i].process < order[j].process
		}
		return order[i].direction < order[j].direction
	})
	out := make([]Series, 0, len(order))
	for _, g := range order {
		// A process the metric data does not label — a build pod, which is the
		// gap RFC-0027 records — has nothing to name, so it stays plain "sum"
		// rather than "sum ".
		name := agg
		if g.process != "" {
			name += " " + g.process
		}
		if g.direction != "" {
			name += " " + g.direction
		}
		out = append(out, aggregateGroup(groups[g], agg, name, g.process))
	}
	return out
}

// aggregateGroup combines the series of one process and direction into a single
// named series.
func aggregateGroup(series []Series, agg, name, process string) Series {
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
	// Every instance of a process shares its allocation, so grouping by process
	// already guarantees this agreement; the check stays as the thing that
	// makes that a property of the data rather than of a grouping key chosen
	// elsewhere. It compares both halves because a shared and a dedicated size
	// can request the same CPU while capping it at a different burst ceiling,
	// and that disagreement is as disqualifying as the requests differing.
	ref, burst := series[0].Reference, series[0].Burst
	for _, s := range series[1:] {
		if s.Reference != ref || s.Burst != burst {
			ref, burst = 0, 0
			break
		}
	}
	if agg == "sum" {
		// A sum is measured against the sum of the allocations.
		ref, burst = ref*float64(len(series)), burst*float64(len(series))
	}
	out.Reference, out.Burst, out.process = ref, burst, process
	return out
}
