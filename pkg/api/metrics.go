package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
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

// Series is a named time series.
type Series struct {
	Name   string  `json:"name"`
	Points []Point `json:"points"`
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

// stepFor keeps every range at 60 points: enough shape to read, few enough
// that a week of per-instance series stays a response a browser can chart.
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

	queries := chartQueries(app)
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
			resp.Charts[i] = s.runChart(c.Request.Context(), q, start, end, step)
		}(i, q)
	}
	wg.Wait()
	c.JSON(http.StatusOK, resp)
}

func (s *Server) runChart(ctx context.Context, q chartQuery, start, end time.Time, step time.Duration) Chart {
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
	return ch
}
