package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/project"
)

// The calls a project received at its internal name (#311), for its
// Connections page: per calling project, how many in the last day and when
// the last one was. Read from Prometheus, where the server's counter
// shpyrd_edge_service_calls_total is scraped; nothing is kept in the server.

// callsWindow is how far back the Connections page looks.
const callsWindow = 24 * time.Hour

// ProjectCalls is GET /api/projects/:slug/calls.
type ProjectCalls struct {
	// Available is false where metrics are not configured: the page then
	// shows nothing about calls rather than zeros.
	Available bool         `json:"available"`
	Callers   []CallerView `json:"callers"`
}

// CallerView is one calling project.
type CallerView struct {
	Caller string `json:"caller"`
	Calls  int64  `json:"calls"`
	// LastAt is when the last call was counted, to the minute; absent when
	// the count comes from before the window's resolution.
	LastAt *time.Time `json:"lastAt,omitempty"`
}

// projectCalls answers who called the project at its internal name in the
// last day, most calls first.
func (s *Server) projectCalls(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	if s.prom == nil {
		c.JSON(http.StatusOK, ProjectCalls{Callers: []CallerView{}})
		return
	}
	out, err := s.callsOf(c.Request.Context(), s.workspace(c), project.SlugOf(app))
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// callsOf runs the two queries: calls per caller over the window, and the
// last minute in the window at which a caller's count moved.
func (s *Server) callsOf(ctx context.Context, ws, slug string) (ProjectCalls, error) {
	selector := fmt.Sprintf(`shpyrd_edge_service_calls_total{workspace=%q,project=%q}`, ws, slug)
	window := fmt.Sprintf("%dh", int(callsWindow.Hours()))
	counts, err := s.prom.Query(ctx, fmt.Sprintf(`sum by (caller) (increase(%s[%s]))`, selector, window))
	if err != nil {
		return ProjectCalls{}, err
	}
	// timestamp() of the series at each minute the count moved, the latest
	// kept: when the last call was, to the minute.
	lasts, err := s.prom.Query(ctx, fmt.Sprintf(`max by (caller) (max_over_time((timestamp(%s) and (increase(%s[2m]) > 0))[%s:1m]))`, selector, selector, window))
	if err != nil {
		return ProjectCalls{}, err
	}
	lastBy := map[string]time.Time{}
	for _, s := range lasts {
		if s.Value > 0 {
			lastBy[s.Labels["caller"]] = time.Unix(int64(s.Value), 0).UTC()
		}
	}
	out := ProjectCalls{Available: true, Callers: []CallerView{}}
	for _, s := range counts {
		caller := s.Labels["caller"]
		if caller == "" || s.Value < 0.5 {
			continue
		}
		v := CallerView{Caller: caller, Calls: int64(s.Value + 0.5)}
		if t, ok := lastBy[caller]; ok {
			v.LastAt = &t
		}
		out.Callers = append(out.Callers, v)
	}
	sort.Slice(out.Callers, func(i, j int) bool {
		if out.Callers[i].Calls != out.Callers[j].Calls {
			return out.Callers[i].Calls > out.Callers[j].Calls
		}
		return out.Callers[i].Caller < out.Callers[j].Caller
	})
	return out, nil
}
