package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Workspace limits (RFC-0033 phase 6, RFC-0042; storage plan step 4):
// ceilings on what a workspace's projects may use together: projects,
// instances, the CPU and memory of their instance sizes, and three kinds
// of storage: block storage (disks, a dedicated database's disk per
// instance, a durable store's disk, in provisioned capacity), buckets,
// and databases. The API judges every change before it makes it, so
// people read "This would bring the workspace to 150 GB of 100 GB"
// rather than finding a claim the namespace quota never admits; the
// controller backs the check with a ResourceQuota per project namespace.
//
// One computation (workspaceUsage) feeds the judgement, the dashboard's
// bars and the console; one judgement (admit) runs for every create and
// resize, under the workspace's lock in the control-plane database, so
// two changes at once are judged one after the other and the second sees
// what the first added. A ceiling refuses what would be added, never what
// exists: a workspace above a ceiling that shrank keeps everything and may
// still act on the other axes, and may still shrink.
//
// A workspace has no ceilings unless they are set: on the console of the
// open-source platform, by the cloud's admin API for each workspace.

// Usage is what a workspace uses, in the terms of its limits.
type Usage = ext.Usage

// usage is what a workspace holds, counted the way its ceilings count.
type usage struct {
	projects, instances  int
	cpu, memory, storage resource.Quantity
	buckets              int
	objectStorage        resource.Quantity // the buckets' caps together
	databases, shared    int
	perProject           map[string]resource.Quantity // block storage by project slug
}

func (u usage) view() Usage {
	return Usage{
		Projects: u.projects, Instances: u.instances,
		CPU: u.cpu.String(), Memory: u.memory.String(), Storage: u.storage.String(),
		Buckets: u.buckets, ObjectStorage: u.objectStorage.String(),
		Databases: u.databases, SharedDatabases: u.shared,
	}
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

// addStorage adds block storage, to the workspace and to the project it
// belongs to.
func (u *usage) addStorage(project string, q resource.Quantity) {
	u.storage.Add(q)
	if project == "" {
		return
	}
	if u.perProject == nil {
		u.perProject = map[string]resource.Quantity{}
	}
	p := u.perProject[project]
	p.Add(q)
	u.perProject[project] = p
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
	l, _ := s.ceilingsOf(ctx, ws)
	return l
}

// ceilingsOf returns the workspace's ceilings and its projects' own caps.
func (s *Server) ceilingsOf(ctx context.Context, ws string) (*store.Limits, map[string]store.ProjectCap) {
	w, err := s.store.Workspace(ctx, ws)
	if err != nil {
		return nil, nil
	}
	return w.Settings.Limits, w.Settings.ProjectCaps
}

// workspaceUsage sums what a workspace holds. It returns the usage as it
// is (before) and as it would be with replace standing in for the stored
// App of the same name, or counted as a new project (after); both are the
// same when replace is nil. One listing feeds both.
func (s *Server) workspaceUsage(ctx context.Context, ws string, cat *sizes.Catalog, replace *shpyrdv1.App) (before, after usage, err error) {
	var apps shpyrdv1.AppList
	if err := s.apps.List(ctx, &apps); err != nil {
		return before, after, err
	}
	seen := false
	namespaces := map[string]string{} // the workspace's projects: namespace -> slug
	for i := range apps.Items {
		a := &apps.Items[i]
		if workspaceOf(a) != ws {
			continue
		}
		namespaces[a.Namespace] = a.Spec.Slug
		before.addApp(a, cat)
		if replace != nil && a.Namespace == replace.Namespace && a.Name == replace.Name {
			after.addApp(replace, cat)
			seen = true
			continue
		}
		after.addApp(a, cat)
	}
	if replace != nil && !seen {
		after.addApp(replace, cat)
	}
	both := func(f func(u *usage)) { f(&before); f(&after) }
	// Databases and stores, by their sizes (#57); a CRD missing is an
	// extension off, nothing of that kind.
	var pgs shpyrdv1.PostgresList
	if err := s.apps.List(ctx, &pgs); err == nil {
		for i := range pgs.Items {
			pg := &pgs.Items[i]
			slug, ours := namespaces[pg.Namespace]
			if !ours || !pg.DeletionTimestamp.IsZero() {
				continue
			}
			if p := pg.Labels[shpyrdv1.LabelProject]; p != "" {
				slug = p
			}
			if pg.Spec.Type == shpyrdv1.PostgresTypeShared {
				// A database and a role on the platform's server: no
				// instance of its own, no disk; its cap is its own.
				both(func(u *usage) { u.databases++; u.shared++ })
				continue
			}
			res, n, ok := storeResources(cat, "Postgres", pg.Spec.Size, pg.Spec.Instances)
			// Its data volume counts against the storage ceiling as the
			// namespace quota counts it, once per instance: the disk it
			// has, else the disk it gets (the provider minimum rounds the
			// request up).
			disk := existingOrRounded(pg.Status.Storage, s.databaseDiskSize(pg.Spec.Storage))
			if !ok {
				n = 1
				if pg.Spec.Instances != nil && *pg.Spec.Instances > 0 {
					n = *pg.Spec.Instances
				}
			}
			both(func(u *usage) {
				u.databases++
				if ok {
					u.addSized(res, n)
				}
				for i := int32(0); i < n; i++ {
					u.addStorage(slug, disk)
				}
			})
		}
	}
	var rds shpyrdv1.RedisList
	if err := s.apps.List(ctx, &rds); err == nil {
		for i := range rds.Items {
			rd := &rds.Items[i]
			slug, ours := namespaces[rd.Namespace]
			if !ours || !rd.DeletionTimestamp.IsZero() {
				continue
			}
			if p := rd.Labels[shpyrdv1.LabelProject]; p != "" {
				slug = p
			}
			res, n, ok := storeResources(cat, "Redis", rd.Spec.Size, nil)
			disk := existingOrRounded(rd.Status.Storage, s.redisDiskSize(rd.Spec.Storage))
			both(func(u *usage) {
				if ok {
					u.addSized(res, n)
				}
				if rd.Spec.Persistent {
					u.addStorage(slug, disk)
				}
			})
		}
	}
	var vols shpyrdv1.VolumeList
	if err := s.apps.List(ctx, &vols); err == nil {
		for i := range vols.Items {
			v := &vols.Items[i]
			if volumeWorkspace(v) != ws || !v.DeletionTimestamp.IsZero() {
				continue
			}
			slug := v.Labels[shpyrdv1.LabelProject]
			if slug == "" {
				slug = namespaces[v.Namespace]
			}
			size := v.Spec.Size
			both(func(u *usage) { u.addStorage(slug, size) })
		}
	}
	// Buckets of the projects' own, with their caps: the one a database's
	// backups go to belongs to the platform and is not counted.
	var buckets shpyrdv1.ObjectBucketList
	if err := s.apps.List(ctx, &buckets); err == nil {
		for i := range buckets.Items {
			b := &buckets.Items[i]
			if _, ours := namespaces[b.Namespace]; !ours || !b.DeletionTimestamp.IsZero() || b.PlatformBucket() {
				continue
			}
			both(func(u *usage) {
				u.buckets++
				if b.Spec.MaxBytes != nil {
					u.objectStorage.Add(*b.Spec.MaxBytes)
				}
			})
		}
	}
	return before, after, nil
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

// errNoDisks is what a plan without disks says to a new one.
const errNoDisks = "Disks are not included in the free workspace; use a bucket for files, or upgrade."

// A refusal is a change the workspace's ceilings say no to, in the words
// the person reads.
type refusal struct{ msg string }

func (e *refusal) Error() string { return e.msg }

// Refused returns the refusal in err, if it is one.
func refused(err error) (*refusal, bool) {
	var r *refusal
	if errors.As(err, &r) {
		return r, true
	}
	return nil, false
}

// A change is what a request would add to a workspace, in the terms its
// ceilings count. Zero fields change nothing on that axis.
type change struct {
	// project is the slug of the project the change happens in, for its
	// own cap.
	project string
	// newProject counts one more project; app is an App as it would be
	// stored, standing in for its stored self (or new, with newProject).
	newProject bool
	app        *shpyrdv1.App
	// add and sub are instances of a database or store: what it would
	// take, what it takes now (a resize).
	add, sub   corev1.ResourceRequirements
	addN, subN int32
	// storage is block storage added: a new disk, or the growth of one.
	storage resource.Quantity
	// newDisk says the storage is a disk that does not exist yet.
	newDisk bool
	// database counts one more database; instances is how many a
	// dedicated one would run.
	database  bool
	instances int32
	// buckets counts new buckets; bucketCap is the largest cap among
	// them, objectStorage their caps together.
	buckets                  int
	bucketCap, objectStorage resource.Quantity
}

// admit judges a change against the workspace's ceilings and, admitted,
// runs write while still holding the workspace's lock, so what it writes
// is what the next judgement sees. A refusal is returned as a *refusal; a
// usage that cannot be read refuses too (nothing is admitted blind).
// write may be nil for a judgement alone.
func (s *Server) admit(ctx context.Context, ws string, ch change, write func(context.Context) error) error {
	return s.store.LockWorkspace(ctx, ws, func(ctx context.Context) error {
		limits, caps := s.ceilingsOf(ctx, ws)
		if limits != nil || len(caps) > 0 {
			cat, err := s.catalog(ctx)
			if err != nil {
				return err
			}
			before, after, err := s.workspaceUsage(ctx, ws, cat, ch.app)
			if err != nil {
				return fmt.Errorf("the workspace's usage could not be read, so nothing was changed: %w", err)
			}
			if ch.newProject && ch.app == nil {
				after.projects++
			}
			after.subSized(ch.sub, ch.subN)
			after.addSized(ch.add, ch.addN)
			if ch.storage.Sign() != 0 {
				after.addStorage(ch.project, ch.storage)
			}
			if ch.database {
				after.databases++
			}
			after.buckets += ch.buckets
			if ch.objectStorage.Sign() != 0 {
				after.objectStorage.Add(ch.objectStorage)
			}
			if msg := overCeiling(limits, caps, ch, before, after); msg != "" {
				return &refusal{msg}
			}
		}
		if write == nil {
			return nil
		}
		return write(ctx)
	})
}

// overCeiling is the first ceiling the change breaks, in words, or "". A
// ceiling is broken by what the change adds (after above the ceiling and
// above before), never by what the workspace already holds.
func overCeiling(l *store.Limits, caps map[string]store.ProjectCap, ch change, before, after usage) string {
	if l != nil {
		// What the plan does not include comes before how much of it is
		// left: a person learns the shape of their plan first.
		if ch.newDisk && !l.AllowsDisks() {
			return errNoDisks
		}
		if pg := l.Postgres; pg != nil && pg.MaxInstances > 0 && ch.instances > int32(pg.MaxInstances) {
			return fmt.Sprintf("A dedicated database here runs up to %d instances.", pg.MaxInstances)
		}
		if o := l.ObjectStorage; o != nil && ch.buckets > 0 {
			if cap, err := resource.ParseQuantity(o.BucketCap); err == nil && o.BucketCap != "" && ch.bucketCap.Cmp(cap) > 0 {
				return fmt.Sprintf("A bucket holds up to %s.", bytesWords(cap))
			}
		}
		if l.Projects > 0 && after.projects > l.Projects && after.projects > before.projects {
			return fmt.Sprintf("This would bring the workspace to %d of %d projects.", after.projects, l.Projects)
		}
		if l.Instances > 0 && after.instances > l.Instances && after.instances > before.instances {
			return fmt.Sprintf("This would bring the workspace to %d of %d instances.", after.instances, l.Instances)
		}
		if q, err := resource.ParseQuantity(l.CPU); err == nil && l.CPU != "" && after.cpu.Cmp(q) > 0 && after.cpu.Cmp(before.cpu) > 0 {
			return fmt.Sprintf("This would bring the workspace to %s of %s cores.", coreWords(after.cpu), coreWords(q))
		}
		if q, err := resource.ParseQuantity(l.Memory); err == nil && l.Memory != "" && after.memory.Cmp(q) > 0 && after.memory.Cmp(before.memory) > 0 {
			return fmt.Sprintf("This would bring the workspace to %s of %s of memory.", bytesWords(after.memory), bytesWords(q))
		}
		if q, err := resource.ParseQuantity(l.Storage); err == nil && l.Storage != "" && after.storage.Cmp(q) > 0 && after.storage.Cmp(before.storage) > 0 {
			return fmt.Sprintf("This would bring the workspace to %s of %s.", bytesWords(after.storage), bytesWords(q))
		}
		if pg := l.Postgres; pg != nil {
			if pg.Databases > 0 && after.databases > pg.Databases && after.databases > before.databases {
				return fmt.Sprintf("This would bring the workspace to %d of %d databases.", after.databases, pg.Databases)
			}
		}
		if o := l.ObjectStorage; o != nil {
			if o.Buckets > 0 && after.buckets > o.Buckets && after.buckets > before.buckets {
				return fmt.Sprintf("This would bring the workspace to %d of %d buckets.", after.buckets, o.Buckets)
			}
			if q, err := resource.ParseQuantity(o.Total); err == nil && o.Total != "" && after.objectStorage.Cmp(q) > 0 && after.objectStorage.Cmp(before.objectStorage) > 0 {
				return fmt.Sprintf("This would bring the workspace to %s of %s of object storage.", bytesWords(after.objectStorage), bytesWords(q))
			}
		}
	}
	if cap, has := caps[ch.project]; has && ch.project != "" {
		if q, err := resource.ParseQuantity(cap.Storage); err == nil && cap.Storage != "" {
			was, now := before.perProject[ch.project], after.perProject[ch.project]
			if now.Cmp(q) > 0 && now.Cmp(was) > 0 {
				return fmt.Sprintf("This would bring the project to %s of %s.", bytesWords(now), bytesWords(q))
			}
		}
	}
	return ""
}

// bytesWords is a quantity of bytes in words (5Gi: 5 GB, 1536Mi: 1.5 GB,
// 512Mi: 512 MB).
func bytesWords(q resource.Quantity) string {
	return humanQuantity(q.String())
}

// coreWords is a quantity of CPU in words (2, 0.5, 4.5).
func coreWords(q resource.Quantity) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", q.AsApproximateFloat64()), "0"), ".")
}

// usageOf reports a workspace's current usage for the dashboard.
func (s *Server) usageOf(ctx context.Context, ws string) *Usage {
	cat, err := s.catalog(ctx)
	if err != nil {
		return nil
	}
	u, _, err := s.workspaceUsage(ctx, ws, cat, nil)
	if err != nil {
		return nil
	}
	v := u.view()
	return &v
}

// ProjectUsage is what one project holds against the ceiling that
// applies to it: its own cap when it has one, else the workspace's.
type ProjectUsage struct {
	Storage string `json:"storage"`
	// Ceiling is the storage ceiling the project is held to, "" when
	// none; Own says it is the project's own cap.
	Ceiling string `json:"ceiling,omitempty"`
	Own     bool   `json:"own,omitempty"`
}

// projectUsageOf reports a project's block storage against what applies.
func (s *Server) projectUsageOf(ctx context.Context, ws, slug string) *ProjectUsage {
	cat, err := s.catalog(ctx)
	if err != nil {
		return nil
	}
	u, _, err := s.workspaceUsage(ctx, ws, cat, nil)
	if err != nil {
		return nil
	}
	held := u.perProject[slug]
	out := &ProjectUsage{Storage: held.String()}
	limits, caps := s.ceilingsOf(ctx, ws)
	if cap, has := caps[slug]; has && cap.Storage != "" {
		out.Ceiling, out.Own = cap.Storage, true
	} else if limits != nil && limits.Storage != "" {
		out.Ceiling = limits.Storage
	}
	return out
}

// databaseDiskSize is the disk a database gets for a request: the request
// (the controller's 5Gi default when none), rounded to the provider minimum
// of the database class.
func (s *Server) databaseDiskSize(requested *resource.Quantity) resource.Quantity {
	size := resource.MustParse("5Gi")
	if requested != nil {
		size = *requested
	}
	return roundToMinimum(size, install.DatabaseVolumeMinSize(s.vars))
}

// redisDiskSize is the same for a persistent store (1Gi by default).
func (s *Server) redisDiskSize(requested *resource.Quantity) resource.Quantity {
	size := resource.MustParse("1Gi")
	if requested != nil {
		size = *requested
	}
	return roundToMinimum(size, install.DatabaseVolumeMinSize(s.vars))
}

func roundToMinimum(size resource.Quantity, minimum string) resource.Quantity {
	if minimum == "" {
		return size
	}
	if q, err := resource.ParseQuantity(minimum); err == nil && size.Cmp(q) < 0 {
		return q
	}
	return size
}

// existingOrRounded prefers what the status reports the disk is.
func existingOrRounded(status string, rounded resource.Quantity) resource.Quantity {
	if q, err := resource.ParseQuantity(status); err == nil && status != "" {
		return q
	}
	return rounded
}

var _ client.Object = &shpyrdv1.ObjectBucket{}
