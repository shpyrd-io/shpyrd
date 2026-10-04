package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"k8s.io/apimachinery/pkg/api/resource"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// A workspace's settings that bound what it uses: its ceilings (RFC-0042)
// and what its projects and databases do by default when nobody uses them
// (RFC-0075). The open-source console sets them for its one workspace; the
// cloud's admin API, through ApplyWorkspaceSettings, for each of its.

// WorkspaceSettings is a workspace's ceilings and sleep defaults; nil means
// none.
type WorkspaceSettings struct {
	Limits *store.Limits        `json:"limits"`
	Sleep  *store.SleepDefaults `json:"sleep"`
}

// NormalizeWorkspaceSettings checks settings and puts them in the form the
// store keeps: quantities parsed, durations as Go writes them, empty parts
// dropped.
func NormalizeWorkspaceSettings(in WorkspaceSettings) (WorkspaceSettings, error) {
	out := WorkspaceSettings{}
	if l := in.Limits; l != nil {
		if l.Projects < 0 || l.Instances < 0 {
			return out, errors.New("limits: counts cannot be negative")
		}
		n := *l
		for name, q := range map[string]*string{"cpu": &n.CPU, "memory": &n.Memory, "storage": &n.Storage} {
			*q = strings.TrimSpace(*q)
			if *q == "" {
				continue
			}
			v, err := resource.ParseQuantity(*q)
			if err != nil || v.Sign() <= 0 {
				return out, fmt.Errorf("limits: %s %q is not a positive quantity (try 2, 500m, 512Mi, 10Gi)", name, *q)
			}
			*q = v.String()
		}
		if n != (store.Limits{}) {
			out.Limits = &n
		}
	}
	if sl := in.Sleep; sl != nil {
		n := store.SleepDefaults{}
		if strings.TrimSpace(sl.AppsAfter) != "" {
			sp, err := validateSleep("web", &shpyrdv1.SleepSpec{After: sl.AppsAfter, Resuming: sl.AppsResuming})
			if err != nil {
				return out, fmt.Errorf("apps: %w", err)
			}
			if sp != nil {
				d, _ := time.ParseDuration(sp.After)
				n.AppsAfter, n.AppsResuming = shortDuration(d), sp.Resuming
			}
		}
		if after := strings.ToLower(strings.TrimSpace(sl.DatabasesAfter)); after != "" && after != "off" {
			d, err := time.ParseDuration(after)
			if err != nil || d < 5*time.Minute || d > 24*time.Hour {
				return out, errors.New("databases: sleep after must be a duration from 5m to 24h, or off")
			}
			n.DatabasesAfter = shortDuration(d)
		}
		if n != (store.SleepDefaults{}) {
			out.Sleep = &n
		}
	}
	return out, nil
}

// shortDuration is a duration as people write it: 10m, 1h, 1h30m.
func shortDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// ApplyWorkspaceSettings checks settings and makes them the workspace's,
// replacing its ceilings and sleep defaults (what is nil is none).
func ApplyWorkspaceSettings(ctx context.Context, st store.Store, slug string, in WorkspaceSettings) (*store.Workspace, error) {
	norm, err := NormalizeWorkspaceSettings(in)
	if err != nil {
		return nil, err
	}
	w, err := st.Workspace(ctx, slug)
	if err != nil {
		return nil, err
	}
	settings := w.Settings
	settings.Limits, settings.Sleep = norm.Limits, norm.Sleep
	return st.UpdateWorkspaceSettings(ctx, slug, settings)
}

// getWorkspaceSettings is GET /api/cluster/workspace-settings: the one
// workspace's ceilings and sleep defaults, and what it uses now.
func (s *Server) getWorkspaceSettings(c *gin.Context) {
	ctx := c.Request.Context()
	w, err := s.store.Workspace(ctx, store.DefaultWorkspaceSlug(ctx, s.store))
	if err != nil {
		storeErr(c, err, "workspace")
		return
	}
	c.JSON(http.StatusOK, gin.H{"workspace": w.Slug, "limits": w.Settings.Limits, "sleep": w.Settings.Sleep, "usage": s.usageOf(ctx, w.Slug)})
}

// putWorkspaceSettings is PUT /api/cluster/workspace-settings: the one
// workspace's ceilings and sleep defaults, replaced.
func (s *Server) putWorkspaceSettings(c *gin.Context) {
	var req WorkspaceSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	ctx := c.Request.Context()
	slug := store.DefaultWorkspaceSlug(ctx, s.store)
	w, err := ApplyWorkspaceSettings(ctx, s.store, slug, req)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			storeErr(c, err, "workspace")
			return
		}
		abort(c, http.StatusBadRequest, err)
		return
	}
	s.forgetTenants()
	s.audit(c, "", "workspace.settings", slug, settingsDetail(w.Settings.Limits, w.Settings.Sleep))
	c.JSON(http.StatusOK, gin.H{"workspace": w.Slug, "limits": w.Settings.Limits, "sleep": w.Settings.Sleep, "usage": s.usageOf(ctx, w.Slug)})
}

// settingsDetail is what the audit log says about new settings.
func settingsDetail(l *store.Limits, sl *store.SleepDefaults) string {
	parts := []string{}
	if l == nil {
		parts = append(parts, "no limits")
	} else {
		parts = append(parts, fmt.Sprintf("limits projects=%d instances=%d cpu=%s memory=%s storage=%s", l.Projects, l.Instances, l.CPU, l.Memory, l.Storage))
	}
	if sl == nil {
		parts = append(parts, "no sleep defaults")
	} else {
		parts = append(parts, fmt.Sprintf("sleep apps=%s/%s databases=%s", sl.AppsAfter, sl.AppsResuming, sl.DatabasesAfter))
	}
	return strings.Join(parts, "; ")
}
