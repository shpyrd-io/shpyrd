package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Out-of-memory kills (#119): the controller records every one in the
// store; the project's metrics carry its own as markers for the Memory
// chart, and the operator reads the cluster's, the platform's components
// among them.

// oomMarkers lists the project's kills since start, oldest first as the
// releases are, those of one process when the charts are filtered to it.
// A store that cannot answer costs the markers and nothing else: the
// charts are the page.
func (s *Server) oomMarkers(ctx context.Context, app *shpyrdv1.App, start time.Time, process string) []OOMMarker {
	out := []OOMMarker{}
	if s.store == nil {
		return out
	}
	kills, err := s.store.QueryOOMKills(ctx, store.OOMKillQuery{Namespace: app.Namespace, From: start})
	if err != nil {
		s.log.Warn("listing a project's out-of-memory kills failed", "project", app.Name, "err", err)
		return out
	}
	var sizeOf func(process string, limit int64) string
	for i := len(kills) - 1; i >= 0; i-- {
		k := kills[i]
		if process != "" && k.Process != process {
			continue
		}
		if sizeOf == nil {
			sizeOf = s.sizeNamer(ctx, app)
		}
		out = append(out, OOMMarker{
			Time: float64(k.At.Unix()), Process: k.Process, Instance: k.Instance,
			Limit: k.LimitBytes, Size: firstNonEmpty(k.Size, sizeOf(k.Process, k.LimitBytes)), Release: k.Release,
		})
	}
	return out
}

// sizeNamer names the size a process has today when its memory is the
// limit a kill crossed. The pod does not say its size, so the name is
// inferred: it is the current one unless the process was resized since,
// in which case the limit alone is told.
func (s *Server) sizeNamer(ctx context.Context, app *shpyrdv1.App) func(process string, limit int64) string {
	none := func(string, int64) string { return "" }
	cat, err := s.catalog(ctx)
	if err != nil || cat == nil {
		return none
	}
	procs := app.EffectiveProcesses()
	return func(process string, limit int64) string {
		p, ok := procs[process]
		if !ok || limit == 0 {
			return ""
		}
		res, name, err := cat.Resolve(p.Size, p.Resources)
		if err != nil {
			return ""
		}
		if q, ok := res.Limits[corev1.ResourceMemory]; ok && q.Value() == limit {
			return name
		}
		return ""
	}
}

// ClusterOOMKillsResponse is the operator's list: every kill in the range,
// the platform's own components and the projects' processes alike, with
// the pods they happened to, which the operator sees anyway.
type ClusterOOMKillsResponse struct {
	Range string          `json:"range"`
	Kills []store.OOMKill `json:"kills"`
}

func (s *Server) clusterOOMKills(c *gin.Context) {
	rng := c.DefaultQuery("range", "24h")
	dur, ok := rangeOptions[rng]
	if !ok {
		abort(c, http.StatusBadRequest, errors.New("range must be one of 15m, 1h, 6h, 24h, 7d"))
		return
	}
	kills, err := s.store.QueryOOMKills(c.Request.Context(), store.OOMKillQuery{From: time.Now().Add(-dur)})
	if err != nil {
		abort(c, http.StatusBadGateway, s.errUnreadable("the out-of-memory kills", err))
		return
	}
	c.JSON(http.StatusOK, ClusterOOMKillsResponse{Range: rng, Kills: kills})
}
