package api

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Workspace limits (RFC-0033 phase 6, RFC-0042): ceilings on what a
// workspace's projects may use together: projects, instances, CPU and
// memory of their instance sizes, storage. The API checks them before it
// changes anything, so people read "this would run 5 instances; the
// workspace allows 4" rather than finding pods stuck Pending; the
// controller backs the check with a ResourceQuota per project namespace.
// A workspace has none unless they are set: on the console of the
// open-source platform, by the cloud's admin API for each workspace.

// Usage is what a workspace uses, in the terms of its limits.
type Usage = ext.Usage

type usage struct {
	projects, instances  int
	cpu, memory, storage resource.Quantity
}

func (u usage) view() Usage {
	return Usage{Projects: u.projects, Instances: u.instances, CPU: u.cpu.String(), Memory: u.memory.String(), Storage: u.storage.String()}
}

// addApp adds an App's desired instances and their sizes: the CPU of a
// size is its limit (a shared size requests only a share of it), its
// memory the request. A project with nothing to run yet (no image, no
// source) is a project and nothing more.
func (u *usage) addApp(app *shpyrdv1.App, cat *sizes.Catalog) {
	u.projects++
	if app.Spec.Image == "" && !app.HasSource() {
		return
	}
	procs := app.EffectiveProcesses()
	if len(procs) == 0 {
		procs = map[string]shpyrdv1.Process{"web": {}}
	}
	for name, p := range procs {
		if name == "release" {
			continue // runs once per release as a Job (RFC-0066), not an instance
		}
		n := int32(1)
		if p.Replicas != nil {
			n = *p.Replicas
		}
		if n <= 0 {
			continue
		}
		u.instances += int(n)
		res, _, err := cat.Resolve(p.Size, p.Resources)
		if err != nil {
			continue // an unknown size is refused elsewhere
		}
		cpu, ok := res.Limits[corev1.ResourceCPU]
		if !ok {
			cpu, ok = res.Requests[corev1.ResourceCPU]
		}
		if ok {
			for i := int32(0); i < n; i++ {
				u.cpu.Add(cpu)
			}
		}
		if mem, ok := res.Requests[corev1.ResourceMemory]; ok {
			for i := int32(0); i < n; i++ {
				u.memory.Add(mem)
			}
		}
	}
}

// addSized adds n instances of resources sized res: CPU at the limit,
// memory at the request, as addApp counts a process.
func (u *usage) addSized(res corev1.ResourceRequirements, n int32) {
	for i := int32(0); i < n; i++ {
		if cpu, ok := res.Limits[corev1.ResourceCPU]; ok {
			u.cpu.Add(cpu)
		}
		if mem, ok := res.Requests[corev1.ResourceMemory]; ok {
			u.memory.Add(mem)
		}
	}
}

// subSized takes back what addSized added.
func (u *usage) subSized(res corev1.ResourceRequirements, n int32) {
	for i := int32(0); i < n; i++ {
		if cpu, ok := res.Limits[corev1.ResourceCPU]; ok {
			u.cpu.Sub(cpu)
		}
		if mem, ok := res.Requests[corev1.ResourceMemory]; ok {
			u.memory.Sub(mem)
		}
	}
}

// storeResources is what a database or store of a kind and a size takes,
// with how many instances of it run; ok is false for a size not in its
// list (a database from before #57 the controller has not moved yet).
func storeResources(cat *sizes.Catalog, kind, size string, instances *int32) (corev1.ResourceRequirements, int32, bool) {
	what := strings.ToLower(kind)
	list := cat.For(what)
	if list == nil {
		return corev1.ResourceRequirements{}, 0, false
	}
	s, err := list.Pick(what, size)
	if err != nil {
		return corev1.ResourceRequirements{}, 0, false
	}
	n := int32(1)
	if instances != nil && *instances > 0 {
		n = *instances
	}
	return s.Resources(), n, true
}

// limitsOf returns the ceilings the workspace is held to; nil when it has
// none.
func (s *Server) limitsOf(ctx context.Context, ws string) *store.Limits {
	w, err := s.store.Workspace(ctx, ws)
	if err != nil {
		return nil
	}
	return w.Settings.Limits
}

// workspaceUsage sums a workspace's projects. When replace is not nil it
// stands in for the stored App of the same name (the state after a
// change); when add is not nil it is counted as a new project.
func (s *Server) workspaceQuotaUsage(ctx context.Context, ws string, cat *sizes.Catalog, replace *shpyrdv1.App) (usage, error) {
	var u usage
	var apps shpyrdv1.AppList
	if err := s.apps.List(ctx, &apps); err != nil {
		return u, err
	}
	seen := false
	namespaces := map[string]bool{} // the workspace's projects
	for i := range apps.Items {
		a := &apps.Items[i]
		if workspaceOf(a) != ws {
			continue
		}
		namespaces[a.Namespace] = true
		if replace != nil && a.Namespace == replace.Namespace && a.Name == replace.Name {
			u.addApp(replace, cat)
			seen = true
			continue
		}
		u.addApp(a, cat)
	}
	if replace != nil && !seen {
		u.addApp(replace, cat)
	}
	// Databases and stores, by their sizes (#57); a CRD missing is an
	// extension off, nothing of that kind.
	var pgs shpyrdv1.PostgresList
	if err := s.apps.List(ctx, &pgs); err == nil {
		for _, pg := range pgs.Items {
			if res, n, ok := storeResources(cat, "Postgres", pg.Spec.Size, pg.Spec.Instances); ok && namespaces[pg.Namespace] {
				u.addSized(res, n)
			}
		}
	}
	var rds shpyrdv1.RedisList
	if err := s.apps.List(ctx, &rds); err == nil {
		for _, rd := range rds.Items {
			if res, n, ok := storeResources(cat, "Redis", rd.Spec.Size, nil); ok && namespaces[rd.Namespace] {
				u.addSized(res, n)
			}
		}
	}
	var vols shpyrdv1.VolumeList
	if err := s.apps.List(ctx, &vols); err == nil {
		for i := range vols.Items {
			v := &vols.Items[i]
			if volumeWorkspace(v) != ws {
				continue
			}
			u.storage.Add(v.Spec.Size)
		}
	}
	return u, nil
}

// volumeWorkspace is the workspace a Volume belongs to: its label
// (authoritative, set at creation), else the implicit workspace, the only
// one that existed before labels.
func volumeWorkspace(v *shpyrdv1.Volume) string {
	if ws := v.Labels[shpyrdv1.LabelWorkspace]; ws != "" {
		return ws
	}
	return store.DefaultWorkspace
}

// Axes of the limits a check may look at: a storage change is judged on
// storage, a scale on instances and requests, so a workspace already over
// on one axis (limits that shrank) can still act on the others.
type axis int

const (
	axisProjects axis = iota
	axisCompute       // instances, CPU, memory
	axisStorage
)

// exceeds explains the first ceiling a usage breaks on the given axes, or "".
func exceeds(l *store.Limits, u usage, axes ...axis) string {
	if l == nil {
		return ""
	}
	for _, a := range axes {
		switch a {
		case axisProjects:
			if l.Projects > 0 && u.projects > l.Projects {
				return fmt.Sprintf("this would make %d projects; the workspace allows %d", u.projects, l.Projects)
			}
		case axisCompute:
			if l.Instances > 0 && u.instances > l.Instances {
				return fmt.Sprintf("this would run %d instances; the workspace allows %d", u.instances, l.Instances)
			}
			if q, err := resource.ParseQuantity(l.CPU); err == nil && l.CPU != "" && u.cpu.Cmp(q) > 0 {
				return fmt.Sprintf("this would request %s CPU; the workspace allows %s", u.cpu.String(), q.String())
			}
			if q, err := resource.ParseQuantity(l.Memory); err == nil && l.Memory != "" && u.memory.Cmp(q) > 0 {
				return fmt.Sprintf("this would request %s of memory; the workspace allows %s", u.memory.String(), q.String())
			}
		case axisStorage:
			if q, err := resource.ParseQuantity(l.Storage); err == nil && l.Storage != "" && u.storage.Cmp(q) > 0 {
				return fmt.Sprintf("this would claim %s of storage; the workspace allows %s", u.storage.String(), q.String())
			}
		}
	}
	return ""
}

// checkLimits refuses a change to an App that would break the workspace's
// limits; after is the App as it would be stored.
func (s *Server) checkLimits(ctx context.Context, ws string, after *shpyrdv1.App) error {
	limits := s.limitsOf(ctx, ws)
	if limits == nil {
		return nil
	}
	cat, err := s.catalog(ctx)
	if err != nil {
		return err
	}
	u, err := s.workspaceQuotaUsage(ctx, ws, cat, after)
	if err != nil {
		return err
	}
	if msg := exceeds(limits, u, axisCompute); msg != "" {
		return fmt.Errorf("workspace limit: %s", msg)
	}
	return nil
}

// checkStorageLimit refuses claiming more storage than the workspace allows;
// extra is the size being added (or the growth of a resize).
func (s *Server) checkStorageLimit(ctx context.Context, ws string, extra resource.Quantity) error {
	limits := s.limitsOf(ctx, ws)
	if limits == nil || limits.Storage == "" {
		return nil
	}
	cat, err := s.catalog(ctx)
	if err != nil {
		return err
	}
	u, err := s.workspaceQuotaUsage(ctx, ws, cat, nil)
	if err != nil {
		return err
	}
	u.storage.Add(extra)
	if msg := exceeds(limits, u, axisStorage); msg != "" {
		return fmt.Errorf("workspace limit: %s", msg)
	}
	return nil
}

// checkStoreLimits refuses a database or store that would take the
// workspace past its CPU or memory: add is what it takes, sub what it
// took before (a resize).
func (s *Server) checkStoreLimits(ctx context.Context, ws string, add, sub corev1.ResourceRequirements, addN, subN int32) error {
	limits := s.limitsOf(ctx, ws)
	if limits == nil {
		return nil
	}
	cat, err := s.catalog(ctx)
	if err != nil {
		return err
	}
	u, err := s.workspaceQuotaUsage(ctx, ws, cat, nil)
	if err != nil {
		return err
	}
	u.subSized(sub, subN)
	u.addSized(add, addN)
	if msg := exceeds(limits, u, axisCompute); msg != "" {
		return fmt.Errorf("workspace limit: %s", msg)
	}
	return nil
}

// usageOf reports a workspace's current usage for the dashboard.
func (s *Server) usageOf(ctx context.Context, ws string) *Usage {
	cat, err := s.catalog(ctx)
	if err != nil {
		return nil
	}
	u, err := s.workspaceQuotaUsage(ctx, ws, cat, nil)
	if err != nil {
		return nil
	}
	v := u.view()
	return &v
}
