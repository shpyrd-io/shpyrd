package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
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
		if pgl := n.Postgres; pgl != nil {
			np, err := normalizePostgresLimits(*pgl)
			if err != nil {
				return out, err
			}
			n.Postgres = np
		}
		if os := n.ObjectStorage; os != nil {
			no, err := normalizeObjectStorageLimits(*os)
			if err != nil {
				return out, err
			}
			n.ObjectStorage = no
		}
		if n.AllowDisks != nil && *n.AllowDisks {
			n.AllowDisks = nil // allowed is the default; only the refusal is kept
		}
		if n.Projects != 0 || n.Instances != 0 || n.CPU != "" || n.Memory != "" || n.Storage != "" || n.AllowDisks != nil || n.ObjectStorage != nil || n.Postgres != nil {
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
			if sleepTurnsOn(sp) { // "off" is no default
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

// normalizePostgresLimits checks a plan's database rules; nil when they
// say nothing.
func normalizePostgresLimits(in store.PostgresLimits) (*store.PostgresLimits, error) {
	out := store.PostgresLimits{}
	for _, t := range in.Types {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if t != shpyrdv1.PostgresTypeShared && t != shpyrdv1.PostgresTypeDedicated {
			return nil, fmt.Errorf("limits: postgres type %q is not one of shared, dedicated", t)
		}
		if !slices.Contains(out.Types, t) {
			out.Types = append(out.Types, t)
		}
	}
	if d := strings.ToLower(strings.TrimSpace(in.DefaultType)); d != "" {
		if d != shpyrdv1.PostgresTypeShared && d != shpyrdv1.PostgresTypeDedicated {
			return nil, fmt.Errorf("limits: postgres defaultType %q is not one of shared, dedicated", d)
		}
		if len(out.Types) > 0 && !slices.Contains(out.Types, d) {
			return nil, fmt.Errorf("limits: postgres defaultType %q is not among the allowed types %v", d, out.Types)
		}
		out.DefaultType = d
	}
	if cap := strings.TrimSpace(in.SharedCap); cap != "" {
		v, err := resource.ParseQuantity(cap)
		if err != nil || v.Sign() <= 0 {
			return nil, fmt.Errorf("limits: postgres sharedCap %q is not a positive quantity (try 1Gi, 5Gi)", cap)
		}
		out.SharedCap = v.String()
	}
	if in.Databases < 0 || in.MaxInstances < 0 {
		return nil, errors.New("limits: postgres counts cannot be negative")
	}
	out.Databases, out.MaxInstances = in.Databases, in.MaxInstances
	if len(out.Types) == 0 && out.DefaultType == "" && out.SharedCap == "" && out.Databases == 0 && out.MaxInstances == 0 {
		return nil, nil
	}
	return &out, nil
}

// normalizeObjectStorageLimits checks a plan's bucket rules; nil when
// they say nothing.
func normalizeObjectStorageLimits(in store.ObjectStorageLimits) (*store.ObjectStorageLimits, error) {
	if in.Buckets < 0 {
		return nil, errors.New("limits: objectStorage buckets cannot be negative")
	}
	out := store.ObjectStorageLimits{Buckets: in.Buckets}
	for name, q := range map[string]*string{"bucketCap": &in.BucketCap, "total": &in.Total} {
		*q = strings.TrimSpace(*q)
		if *q == "" {
			continue
		}
		v, err := resource.ParseQuantity(*q)
		if err != nil || v.Sign() <= 0 {
			return nil, fmt.Errorf("limits: objectStorage %s %q is not a positive quantity (try 1Gi, 10Gi)", name, *q)
		}
		*q = v.String()
	}
	out.BucketCap, out.Total = in.BucketCap, in.Total
	if out.Buckets == 0 && out.BucketCap == "" && out.Total == "" {
		return nil, nil
	}
	return &out, nil
}

// NormalizeProjectCaps checks a workspace's per-project caps: quantities
// parsed, projects without a cap dropped; nil when none is left.
func NormalizeProjectCaps(in map[string]store.ProjectCap) (map[string]store.ProjectCap, error) {
	out := map[string]store.ProjectCap{}
	for slug, cap := range in {
		slug = strings.TrimSpace(slug)
		st := strings.TrimSpace(cap.Storage)
		if slug == "" || st == "" {
			continue
		}
		v, err := resource.ParseQuantity(st)
		if err != nil || v.Sign() <= 0 {
			return nil, fmt.Errorf("project %s: storage %q is not a positive quantity (try 10Gi)", slug, st)
		}
		out[slug] = store.ProjectCap{Storage: v.String()}
	}
	if len(out) == 0 {
		return nil, nil
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
		line := fmt.Sprintf("limits projects=%d instances=%d cpu=%s memory=%s storage=%s", l.Projects, l.Instances, l.CPU, l.Memory, l.Storage)
		if !l.AllowsDisks() {
			line += " disks=no"
		}
		if o := l.ObjectStorage; o != nil {
			line += fmt.Sprintf(" buckets=%d bucketCap=%s objectStorage=%s", o.Buckets, o.BucketCap, o.Total)
		}
		if pg := l.Postgres; pg != nil {
			line += fmt.Sprintf(" postgres=%s default=%s sharedCap=%s databases=%d maxInstances=%d", strings.Join(pg.Types, ","), pg.DefaultType, pg.SharedCap, pg.Databases, pg.MaxInstances)
		}
		parts = append(parts, line)
	}
	if sl == nil {
		parts = append(parts, "no sleep defaults")
	} else {
		parts = append(parts, fmt.Sprintf("sleep apps=%s/%s databases=%s", sl.AppsAfter, sl.AppsResuming, sl.DatabasesAfter))
	}
	return strings.Join(parts, "; ")
}
