// Package storagemigrate moves a database's data from one storage class to
// another by switchover (the storage plan, step 1): a second instance is
// provisioned on the target class, the primary switches over to it, the old
// instance goes, and the old volume is kept for three days as the way back.
// The database stays available throughout, except for the seconds of the
// switchover. Every step reads the state before acting, so a run that was
// interrupted continues where it stopped, and nothing is removed before the
// data on the new instance has been checked.
package storagemigrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
)

// RetainedMigrationAnnotation carries, on the volume a migration left
// behind, the record the console's placement page lists and releases: the
// same one the project move writes.
const RetainedMigrationAnnotation = "shpyrd.io/retained-migration"

// RetainedDisk is that record.
type RetainedDisk struct {
	Name         string    `json:"name"`
	Namespace    string    `json:"namespace"`
	ProjectUID   types.UID `json:"projectUID"`
	Claim        string    `json:"claim"`
	StorageClass string    `json:"storageClass"`
	Capacity     string    `json:"capacity"`
	RetainedAt   time.Time `json:"retainedAt"`
}

// SQL runs a statement on a database instance and returns its output, one
// row per line, columns separated by tabs. nil skips the data checks.
type SQL func(ctx context.Context, namespace, pod, statement string) (string, error)

// Options is one database migration.
type Options struct {
	Namespace, Name string
	// TargetClass is the storage class the data moves to.
	TargetClass string
	// SnapshotClass takes a snapshot of the new volume at the end; "" none.
	SnapshotClass string
	// Back undoes a migration that stopped on two instances: the primary
	// goes back to the instance on the old class and the new one is removed.
	Back bool
	// Wait bounds each step (default 20 minutes); Poll the checks (3s).
	Wait, Poll time.Duration
	// SQL runs the data checks on the instances; nil skips them.
	SQL SQL
	// Out narrates one line per step.
	Out io.Writer
}

// Result is what the migration did and found.
type Result struct {
	Database   string            `json:"database"`
	From       string            `json:"from,omitempty"`
	To         string            `json:"to,omitempty"`
	OldPV      string            `json:"oldVolume,omitempty"`
	NewClaim   string            `json:"newClaim,omitempty"`
	Snapshot   string            `json:"snapshot,omitempty"`
	SizeOld    int64             `json:"sizeOld,omitempty"`
	SizeNew    int64             `json:"sizeNew,omitempty"`
	Rows       map[string][2]int `json:"rows,omitempty"`
	Verified   bool              `json:"verified"`
	Notes      []string          `json:"notes,omitempty"`
	Steps      []string          `json:"steps"`
	StoppedOn2 bool              `json:"stoppedOnTwoInstances,omitempty"`
}

// ErrDataCheck is returned when the data on the new instance does not match
// the old one; the database is left on its two instances.
var ErrDataCheck = errors.New("the data check failed; the database keeps both instances, nothing was removed: look at the numbers, then run again with --back to return to the old volume, or without it to accept")

type migration struct {
	c    client.Client
	opts Options
	res  *Result
	key  types.NamespacedName
}

// Database migrates one database (or, with Options.Back, undoes one that
// stopped on two instances). The result comes back with an error when a
// step could not complete; the state reached stays, a second run continues.
func Database(ctx context.Context, c client.Client, opts Options) (*Result, error) {
	if opts.Wait <= 0 {
		opts.Wait = 20 * time.Minute
	}
	if opts.Poll <= 0 {
		opts.Poll = 3 * time.Second
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.TargetClass == "" {
		return nil, errors.New("say which storage class the data moves to")
	}
	m := &migration{c: c, opts: opts, res: &Result{Database: opts.Namespace + "/" + opts.Name, To: opts.TargetClass, Rows: map[string][2]int{}}, key: types.NamespacedName{Namespace: opts.Namespace, Name: opts.Name}}
	var err error
	if opts.Back {
		err = m.back(ctx)
	} else {
		err = m.run(ctx)
	}
	return m.res, err
}

func (m *migration) say(format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	m.res.Steps = append(m.res.Steps, line)
	fmt.Fprintln(m.opts.Out, line)
}

func (m *migration) run(ctx context.Context) error {
	pg, err := m.postgres(ctx)
	if err != nil {
		return err
	}
	cluster, err := m.cluster(ctx)
	if err != nil {
		return err
	}
	if err := m.refusals(ctx, pg, cluster); err != nil {
		return err
	}
	// Already there: the primary's volume is on the target class, one
	// instance, no mark. Nothing to do, nothing touched.
	if primary := primaryOf(cluster); primary != "" && pg.Annotations[controller.AnnotationStorageMigration] == "" && ptr.Deref(pg.Spec.Instances, 1) == 1 {
		if claim, err := m.claim(ctx, primary); err == nil && ptr.Deref(claim.Spec.StorageClassName, "") == m.opts.TargetClass {
			m.res.From = m.opts.TargetClass
			m.res.Verified = true
			m.say("%s already has its data on %s; nothing to move", m.res.Database, m.opts.TargetClass)
			return nil
		}
	}

	// 1. The mark. The platform renders the Cluster on the target class at
	// its current size, keeps the database awake and its Service in place.
	// Nothing else changes until the platform has shown it knows the mark.
	if pg.Annotations[controller.AnnotationStorageMigration] != m.opts.TargetClass {
		if err := m.patchPostgres(ctx, func(pg *shpyrdv1.Postgres) {
			if pg.Annotations == nil {
				pg.Annotations = map[string]string{}
			}
			pg.Annotations[controller.AnnotationStorageMigration] = m.opts.TargetClass
		}); err != nil {
			return fmt.Errorf("mark the database for migration: %w", err)
		}
		m.say("%s: marked for migration to %s", m.res.Database, m.opts.TargetClass)
	}
	if err := m.untilWithin(ctx, 3*time.Minute, "the platform to apply the migration class", func() (bool, error) {
		cluster, err := m.cluster(ctx)
		if err != nil {
			return false, err
		}
		class, _, _ := unstructured.NestedString(cluster.Object, "spec", "storage", "storageClass")
		return class == m.opts.TargetClass, nil
	}); err != nil {
		_ = m.patchPostgres(ctx, func(pg *shpyrdv1.Postgres) { delete(pg.Annotations, controller.AnnotationStorageMigration) })
		return errors.New("the platform did not apply the migration class: it runs a version without the storage migration (core v0.9.85 or later); the mark was removed and nothing changed")
	}
	// A sleeping database wakes for the mark; the steps need a primary.
	if err := m.until(ctx, "the database to have a ready instance", func() (bool, error) {
		cluster, err := m.cluster(ctx)
		if err != nil {
			return false, err
		}
		ready, _, _ := unstructured.NestedInt64(cluster.Object, "status", "readyInstances")
		return ready >= 1 && primaryOf(cluster) != "", nil
	}); err != nil {
		return err
	}
	if cluster, err = m.cluster(ctx); err != nil {
		return err
	}
	primary := primaryOf(cluster)
	primaryClaim, err := m.claim(ctx, primary)
	if err != nil {
		return err
	}
	m.res.From = ptr.Deref(primaryClaim.Spec.StorageClassName, "")
	if pg, err = m.postgres(ctx); err != nil {
		return err
	}

	var newInstance, old string
	switch {
	case m.res.From != m.opts.TargetClass:
		// 2. A second instance, on the target class, in sync.
		if ptr.Deref(pg.Spec.Instances, 1) < 2 {
			if err := m.room(ctx, pg, cluster, primary); err != nil {
				return err
			}
			if err := m.patchPostgres(ctx, func(pg *shpyrdv1.Postgres) { pg.Spec.Instances = ptr.To[int32](2) }); err != nil {
				return fmt.Errorf("ask for a second instance: %w", err)
			}
			m.say("%s: a second instance on %s requested", m.res.Database, m.opts.TargetClass)
		}
		newInstance, err = m.waitNewInstance(ctx, primary)
		if err != nil {
			return err
		}
		newClaim, err := m.claim(ctx, newInstance)
		if err != nil {
			return err
		}
		if got := ptr.Deref(newClaim.Spec.StorageClassName, ""); got != m.opts.TargetClass {
			return fmt.Errorf("the new instance %s got a volume on %q, not %s; the database keeps running on its two instances: run again with --back to remove it", newInstance, got, m.opts.TargetClass)
		}
		m.res.NewClaim = newClaim.Name
		m.say("%s: instance %s ready on %s (%s), in sync", m.res.Database, newInstance, m.opts.TargetClass, newClaim.Spec.Resources.Requests.Storage().String())
		old = primary

		// 3. The switchover, then the data on both instances compared.
		if err := m.switchover(ctx, newInstance); err != nil {
			return err
		}
		m.say("%s: switched over to %s", m.res.Database, newInstance)
		if err := m.compare(ctx, old, newInstance); err != nil {
			return err
		}
		if !m.res.Verified {
			m.res.StoppedOn2 = true
			return ErrDataCheck
		}

	case instancesIn(cluster) >= 2 || ptr.Deref(pg.Spec.Instances, 1) >= 2:
		// Resumed after the switchover: the primary is the new instance.
		newInstance = primary
		old = otherInstance(cluster, primary)
		m.res.NewClaim = primaryClaim.Name
		if old != "" {
			if err := m.compare(ctx, old, newInstance); err != nil {
				return err
			}
			if !m.res.Verified {
				m.res.StoppedOn2 = true
				return ErrDataCheck
			}
		} else {
			m.res.Notes = append(m.res.Notes, "no data check: the old instance was already gone when this run started")
		}

	default:
		// On the target class with one instance and the mark still set: a
		// run interrupted after the scale-down. Only the ending is owed.
		m.res.Notes = append(m.res.Notes, "no data check: the old instance was removed in an earlier run")
		m.say("%s: already on %s with one instance; finishing", m.res.Database, m.opts.TargetClass)
		return m.finish(ctx, primaryClaim, false)
	}

	// 4. Keep the old volume, then let the old instance go.
	if old != "" {
		if err := m.retain(ctx, pg, old); err != nil {
			return err
		}
	}
	if err := m.patchPostgres(ctx, func(pg *shpyrdv1.Postgres) { pg.Spec.Instances = ptr.To[int32](1) }); err != nil {
		return fmt.Errorf("back to one instance: %w", err)
	}
	if err := m.waitInstances(ctx, 1, newInstance); err != nil {
		return err
	}
	m.say("%s: one instance again, %s on %s", m.res.Database, newInstance, m.opts.TargetClass)
	newClaim, err := m.claim(ctx, newInstance)
	if err != nil {
		return err
	}
	return m.finish(ctx, newClaim, true)
}

// back returns a database that stopped on two instances to its old volume:
// the primary switches back to the instance on the old class, the new one
// goes, the mark is cleared.
func (m *migration) back(ctx context.Context) error {
	pg, err := m.postgres(ctx)
	if err != nil {
		return err
	}
	cluster, err := m.cluster(ctx)
	if err != nil {
		return err
	}
	if pg.Annotations[controller.AnnotationStorageMigration] == "" {
		return errors.New("the database is not under migration; nothing to go back from")
	}
	names, _, _ := unstructured.NestedStringSlice(cluster.Object, "status", "instanceNames")
	var oldInstance string
	for _, n := range names {
		claim, err := m.claim(ctx, n)
		if err != nil {
			return err
		}
		if ptr.Deref(claim.Spec.StorageClassName, "") != m.opts.TargetClass {
			oldInstance = n
		}
	}
	if oldInstance == "" {
		return errors.New("no instance on the old storage class is left; there is nothing to go back to (the migration completed; the old volume, if kept, is the way back by hand)")
	}
	if primaryOf(cluster) != oldInstance {
		if err := m.switchover(ctx, oldInstance); err != nil {
			return err
		}
		m.say("%s: switched back to %s on the old volume", m.res.Database, oldInstance)
	}
	if err := m.patchPostgres(ctx, func(pg *shpyrdv1.Postgres) { pg.Spec.Instances = ptr.To[int32](1) }); err != nil {
		return err
	}
	if err := m.waitInstances(ctx, 1, oldInstance); err != nil {
		return err
	}
	if err := m.patchPostgres(ctx, func(pg *shpyrdv1.Postgres) { delete(pg.Annotations, controller.AnnotationStorageMigration) }); err != nil {
		return err
	}
	m.say("%s: back on the old volume with one instance; the migration mark is cleared", m.res.Database)
	m.res.Verified = true
	return nil
}

// finish clears the mark so the profile's rules apply to the cluster on
// its new class, and snapshots the new volume when the move happened in
// this run.
func (m *migration) finish(ctx context.Context, claim *corev1.PersistentVolumeClaim, snapshot bool) error {
	if err := m.patchPostgres(ctx, func(pg *shpyrdv1.Postgres) { delete(pg.Annotations, controller.AnnotationStorageMigration) }); err != nil {
		return fmt.Errorf("clear the migration mark: %w", err)
	}
	m.say("%s: migration mark cleared; the profile's size applies now", m.res.Database)
	if !snapshot || m.opts.SnapshotClass == "" || claim == nil {
		return nil
	}
	name := claim.Name + "-" + time.Now().UTC().Format("20060102-150405")
	snap := &unstructured.Unstructured{}
	snap.SetGroupVersionKind(controller.VolumeSnapshotGVK)
	snap.SetName(name)
	snap.SetNamespace(claim.Namespace)
	snap.SetLabels(map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", "shpyrd.io/postgres": m.opts.Name})
	_ = unstructured.SetNestedField(snap.Object, m.opts.SnapshotClass, "spec", "volumeSnapshotClassName")
	_ = unstructured.SetNestedField(snap.Object, claim.Name, "spec", "source", "persistentVolumeClaimName")
	if err := m.c.Create(ctx, snap); err != nil && !apierrors.IsAlreadyExists(err) {
		m.res.Notes = append(m.res.Notes, "no snapshot of the new volume: "+err.Error())
		return nil
	}
	m.res.Snapshot = name
	m.say("%s: snapshot %s of the new volume requested", m.res.Database, name)
	return nil
}

// refusals are what stops a migration before it touches anything.
func (m *migration) refusals(ctx context.Context, pg *shpyrdv1.Postgres, cluster *unstructured.Unstructured) error {
	if pg.Annotations[controller.AnnotationDataMove] != "" {
		return errors.New("the database is being moved between nodes; finish that first")
	}
	if pg.Spec.Sleep != nil && pg.Spec.Sleep.Suspended {
		return errors.New("the database is suspended; resume it first, the migration needs it running")
	}
	if node := pg.Annotations[shpyrdv1.AnnotationPlacement]; node != "" {
		return fmt.Errorf("the database is pinned to node %s by an earlier move; a migration must not change its pod while it runs, so clear the pin first (shpyrd-ctl cluster unpin <project-id>: one planned restart), then run this again", node)
	}
	if n := ptr.Deref(pg.Spec.Instances, 1); n > 1 && pg.Annotations[controller.AnnotationStorageMigration] == "" {
		return fmt.Errorf("the database runs %d instances; this moves single-instance databases (a replicated one changes class by its own switchover: add a replica on the new class, promote it, remove the old)", n)
	}
	sc := &storagev1.StorageClass{}
	if err := m.c.Get(ctx, types.NamespacedName{Name: m.opts.TargetClass}, sc); err != nil {
		return fmt.Errorf("the storage class %q: %w", m.opts.TargetClass, err)
	}
	return nil
}

// room checks that the second instance can be admitted: its namespace's
// quota and another node of the pool must take the primary pod's requests
// once more.
func (m *migration) room(ctx context.Context, pg *shpyrdv1.Postgres, cluster *unstructured.Unstructured, primary string) error {
	pod := &corev1.Pod{}
	if err := m.c.Get(ctx, types.NamespacedName{Namespace: m.opts.Namespace, Name: primary}, pod); err != nil {
		return fmt.Errorf("the pod of instance %s: %w", primary, err)
	}
	needCPU, needMem := podRequests(pod)
	quotas := &corev1.ResourceQuotaList{}
	if err := m.c.List(ctx, quotas, client.InNamespace(m.opts.Namespace)); err != nil {
		return err
	}
	for _, q := range quotas.Items {
		for _, pair := range []struct {
			res  corev1.ResourceName
			need int64
			what string
		}{{corev1.ResourceRequestsCPU, needCPU, "CPU"}, {corev1.ResourceRequestsMemory, needMem, "memory"}} {
			hard, ok := q.Status.Hard[pair.res]
			if !ok {
				continue
			}
			used := q.Status.Used[pair.res]
			left := hard.DeepCopy()
			left.Sub(used)
			if left.CmpInt64(pair.need) < 0 {
				return fmt.Errorf("the workspace's %s ceiling leaves no room for a second instance of %s (%s of %s used; the instance needs %s more); raise the ceiling for the evening, or migrate when the workspace uses less", pair.what, m.opts.Name, used.String(), hard.String(), quantityOf(pair.need, pair.res))
			}
		}
	}
	pool, _, _ := unstructured.NestedString(cluster.Object, "spec", "affinity", "nodeSelector", controller.PoolLabel)
	nodes := &corev1.NodeList{}
	if err := m.c.List(ctx, nodes); err != nil {
		return err
	}
	pods := &corev1.PodList{}
	if err := m.c.List(ctx, pods); err != nil {
		return err
	}
	for _, n := range nodes.Items {
		if n.Name == pod.Spec.NodeName || (pool != "" && n.Labels[controller.PoolLabel] != pool) || !nodeReady(&n) {
			continue
		}
		var usedCPU, usedMem int64
		for _, p := range pods.Items {
			if p.Spec.NodeName != n.Name || p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
				continue
			}
			c, mem := podRequests(&p)
			usedCPU += c
			usedMem += mem
		}
		if n.Status.Allocatable.Cpu().MilliValue()-usedCPU >= needCPU && n.Status.Allocatable.Memory().Value()-usedMem >= needMem {
			return nil
		}
	}
	where := "the cluster"
	if pool != "" {
		where = "the " + pool + " pool"
	}
	return fmt.Errorf("no other ready node in %s has room for the new instance (%s CPU, %s of memory); add a node to the pool first", where, quantityOf(needCPU, corev1.ResourceRequestsCPU), quantityOf(needMem, corev1.ResourceRequestsMemory))
}

func podRequests(pod *corev1.Pod) (cpu, mem int64) {
	for _, c := range pod.Spec.Containers {
		cpu += c.Resources.Requests.Cpu().MilliValue()
		mem += c.Resources.Requests.Memory().Value()
	}
	for _, c := range pod.Spec.InitContainers {
		if c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			cpu += c.Resources.Requests.Cpu().MilliValue()
			mem += c.Resources.Requests.Memory().Value()
		}
	}
	return cpu, mem
}

func quantityOf(v int64, res corev1.ResourceName) string {
	if res == corev1.ResourceRequestsCPU {
		return resource.NewMilliQuantity(v, resource.DecimalSI).String()
	}
	return resource.NewQuantity(v, resource.BinarySI).String()
}

func nodeReady(n *corev1.Node) bool {
	for _, cond := range n.Status.Conditions {
		if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func (m *migration) postgres(ctx context.Context) (*shpyrdv1.Postgres, error) {
	pg := &shpyrdv1.Postgres{}
	if err := m.c.Get(ctx, m.key, pg); err != nil {
		return nil, fmt.Errorf("the database: %w", err)
	}
	return pg, nil
}

// patchPostgres applies a change to the Postgres with a fresh read and a
// retry on conflict: the reconciler and the metering loop write it too.
func (m *migration) patchPostgres(ctx context.Context, change func(*shpyrdv1.Postgres)) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		pg, err := m.postgres(ctx)
		if err != nil {
			return err
		}
		before := pg.DeepCopy()
		change(pg)
		return m.c.Patch(ctx, pg, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	})
}

func (m *migration) cluster(ctx context.Context) (*unstructured.Unstructured, error) {
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(controller.CNPGClusterGVK)
	if err := m.c.Get(ctx, m.key, cluster); err != nil {
		return nil, fmt.Errorf("the database cluster: %w", err)
	}
	return cluster, nil
}

func (m *migration) claim(ctx context.Context, instance string) (*corev1.PersistentVolumeClaim, error) {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := m.c.Get(ctx, types.NamespacedName{Namespace: m.opts.Namespace, Name: instance}, pvc); err != nil {
		return nil, fmt.Errorf("the volume of instance %s: %w", instance, err)
	}
	return pvc, nil
}

// waitNewInstance waits for a second instance, ready, other than primary,
// with the primary unchanged.
func (m *migration) waitNewInstance(ctx context.Context, primary string) (string, error) {
	var last string
	err := m.until(ctx, "the second instance to be ready and in sync", func() (bool, error) {
		cluster, err := m.cluster(ctx)
		if err != nil {
			return false, err
		}
		if reason, _, _ := unstructured.NestedString(cluster.Object, "status", "phaseReason"); reason != "" {
			last = reason
		}
		ready, _, _ := unstructured.NestedInt64(cluster.Object, "status", "readyInstances")
		return otherInstance(cluster, primary) != "" && ready >= 2 && primaryOf(cluster) == primary, nil
	})
	if err != nil {
		if last != "" {
			err = fmt.Errorf("%w (the database says: %s)", err, last)
		}
		return "", err
	}
	cluster, err := m.cluster(ctx)
	if err != nil {
		return "", err
	}
	return otherInstance(cluster, primary), nil
}

// switchover asks CloudNativePG to make an instance the primary, as its own
// promote does: the target primary in the status, with the phase. Done when
// that instance is the primary and ready; the former primary rejoins as a
// replica in its own time.
func (m *migration) switchover(ctx context.Context, target string) error {
	cluster, err := m.cluster(ctx)
	if err != nil {
		return err
	}
	if primaryOf(cluster) != target {
		before := cluster.DeepCopy()
		_ = unstructured.SetNestedField(cluster.Object, target, "status", "targetPrimary")
		_ = unstructured.SetNestedField(cluster.Object, "Switchover in progress", "status", "phase")
		_ = unstructured.SetNestedField(cluster.Object, "Switching over to "+target+" (storage migration)", "status", "phaseReason")
		if err := m.c.Status().Patch(ctx, cluster, client.MergeFrom(before)); err != nil {
			return fmt.Errorf("ask for the switchover: %w", err)
		}
	}
	return m.until(ctx, "the switchover to "+target, func() (bool, error) {
		cluster, err := m.cluster(ctx)
		if err != nil {
			return false, err
		}
		ready, _, _ := unstructured.NestedInt64(cluster.Object, "status", "readyInstances")
		return primaryOf(cluster) == target && ready >= 1, nil
	})
}

// retain keeps the old instance's volume: reclaim policy Retain and the
// record the console's placement page reads, before CloudNativePG removes
// the claim.
func (m *migration) retain(ctx context.Context, pg *shpyrdv1.Postgres, instance string) error {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := m.c.Get(ctx, types.NamespacedName{Namespace: m.opts.Namespace, Name: instance}, pvc); err != nil {
		if apierrors.IsNotFound(err) {
			return nil // already gone: a resumed run past this step
		}
		return err
	}
	if pvc.Spec.VolumeName == "" {
		return fmt.Errorf("the old instance %s has no bound volume to retain", instance)
	}
	pv := &corev1.PersistentVolume{}
	if err := m.c.Get(ctx, types.NamespacedName{Name: pvc.Spec.VolumeName}, pv); err != nil {
		return fmt.Errorf("the old volume: %w", err)
	}
	m.res.OldPV = pv.Name
	if pv.Spec.PersistentVolumeReclaimPolicy == corev1.PersistentVolumeReclaimRetain && pv.Annotations[RetainedMigrationAnnotation] != "" {
		return nil
	}
	var projectUID types.UID
	apps := &shpyrdv1.AppList{}
	if err := m.c.List(ctx, apps, client.InNamespace(m.opts.Namespace)); err == nil && len(apps.Items) > 0 {
		projectUID = apps.Items[0].UID
	}
	capacity := pv.Spec.Capacity[corev1.ResourceStorage]
	record, _ := json.Marshal(RetainedDisk{Name: pv.Name, Namespace: m.opts.Namespace, ProjectUID: projectUID, Claim: pvc.Name, StorageClass: pv.Spec.StorageClassName, Capacity: capacity.String(), RetainedAt: time.Now().UTC()})
	before := pv.DeepCopy()
	pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
	if pv.Annotations == nil {
		pv.Annotations = map[string]string{}
	}
	pv.Annotations[RetainedMigrationAnnotation] = string(record)
	if pv.Labels == nil {
		pv.Labels = map[string]string{}
	}
	pv.Labels[RetainedMigrationAnnotation] = m.opts.Name
	if err := m.c.Patch(ctx, pv, client.MergeFrom(before)); err != nil {
		return fmt.Errorf("retain the old volume: %w", err)
	}
	m.say("%s: old volume %s kept (Retain); the project's placement page lists it, release it after the keep", m.res.Database, pv.Name)
	return nil
}

func (m *migration) waitInstances(ctx context.Context, n int64, primary string) error {
	return m.until(ctx, fmt.Sprintf("the cluster to run %d instance(s)", n), func() (bool, error) {
		cluster, err := m.cluster(ctx)
		if err != nil {
			return false, err
		}
		ready, _, _ := unstructured.NestedInt64(cluster.Object, "status", "readyInstances")
		names, _, _ := unstructured.NestedStringSlice(cluster.Object, "status", "instanceNames")
		return int64(len(names)) == n && ready == n && primaryOf(cluster) == primary, nil
	})
}

func (m *migration) until(ctx context.Context, what string, done func() (bool, error)) error {
	return m.untilWithin(ctx, m.opts.Wait, what, done)
}

func (m *migration) untilWithin(ctx context.Context, wait time.Duration, what string, done func() (bool, error)) error {
	deadline := time.Now().Add(wait)
	for {
		ok, err := done()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("waited %s for %s; the state so far stays, run this again to continue (or with --back to return to the old volume)", wait.Round(time.Second), what)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.opts.Poll):
		}
	}
}

// compare reads the database size and every table's row count on the old
// instance (now a replica, at the same point in the stream) and on the new
// primary, right after the switchover, and judges: the size within 5 %,
// every table within a handful of rows (what replication lag can hold).
func (m *migration) compare(ctx context.Context, old, newInstance string) error {
	if m.opts.SQL == nil {
		m.res.Notes = append(m.res.Notes, "data checks skipped (no way to run SQL on the instances)")
		m.res.Verified = true
		return nil
	}
	sizeOld, rowsOld, err := m.measure(ctx, old)
	if err != nil {
		return err
	}
	sizeNew, rowsNew, err := m.measure(ctx, newInstance)
	if err != nil {
		return err
	}
	m.res.SizeOld, m.res.SizeNew = sizeOld, sizeNew
	ok := true
	if sizeOld > 0 {
		diff := float64(sizeNew-sizeOld) / float64(sizeOld)
		if diff < -0.05 || diff > 0.05 {
			ok = false
			m.res.Notes = append(m.res.Notes, fmt.Sprintf("database size %d on the old instance, %d on the new (%.1f%%)", sizeOld, sizeNew, diff*100))
		}
	}
	tables := make([]string, 0, len(rowsOld))
	for t := range rowsOld {
		tables = append(tables, t)
	}
	sort.Strings(tables)
	for _, t := range tables {
		o, n := rowsOld[t], rowsNew[t]
		m.res.Rows[t] = [2]int{o, n}
		tolerance := o / 200 // 0.5 %
		if tolerance < 5 {
			tolerance = 5
		}
		if n < o-tolerance || n > o+tolerance {
			ok = false
			m.res.Notes = append(m.res.Notes, fmt.Sprintf("%s: %d rows on the old instance, %d on the new", t, o, n))
		}
	}
	for t := range rowsNew {
		if _, seen := rowsOld[t]; !seen {
			ok = false
			m.res.Notes = append(m.res.Notes, fmt.Sprintf("%s: only on the new instance", t))
		}
	}
	m.res.Verified = ok
	if ok {
		m.say("%s: data verified on the new instance (%d tables, %d bytes)", m.res.Database, len(tables), sizeNew)
	} else {
		m.say("%s: DATA CHECK FAILED: %s", m.res.Database, strings.Join(m.res.Notes, "; "))
	}
	return nil
}

// measure reads the size of the application database and the row count of
// every user table on one instance.
func (m *migration) measure(ctx context.Context, instance string) (int64, map[string]int, error) {
	size, err := m.opts.SQL(ctx, m.opts.Namespace, instance, "SELECT pg_database_size(current_database())")
	if err != nil {
		return 0, nil, fmt.Errorf("measure %s: %w", instance, err)
	}
	bytes, err := strconv.ParseInt(strings.TrimSpace(size), 10, 64)
	if err != nil {
		return 0, nil, fmt.Errorf("measure %s: unexpected size %q", instance, strings.TrimSpace(size))
	}
	tables, err := m.opts.SQL(ctx, m.opts.Namespace, instance, "SELECT quote_ident(schemaname)||'.'||quote_ident(relname) FROM pg_stat_user_tables ORDER BY 1")
	if err != nil {
		return 0, nil, fmt.Errorf("list tables on %s: %w", instance, err)
	}
	rows := map[string]int{}
	for _, table := range strings.Split(strings.TrimSpace(tables), "\n") {
		if table == "" {
			continue
		}
		out, err := m.opts.SQL(ctx, m.opts.Namespace, instance, "SELECT count(*) FROM "+table)
		if err != nil {
			return 0, nil, fmt.Errorf("count %s on %s: %w", table, instance, err)
		}
		n, err := strconv.Atoi(strings.TrimSpace(out))
		if err != nil {
			return 0, nil, fmt.Errorf("count %s on %s: unexpected %q", table, instance, strings.TrimSpace(out))
		}
		rows[table] = n
	}
	return bytes, rows, nil
}

func primaryOf(cluster *unstructured.Unstructured) string {
	p, _, _ := unstructured.NestedString(cluster.Object, "status", "currentPrimary")
	return p
}

func instancesIn(cluster *unstructured.Unstructured) int {
	names, _, _ := unstructured.NestedStringSlice(cluster.Object, "status", "instanceNames")
	return len(names)
}

func otherInstance(cluster *unstructured.Unstructured, primary string) string {
	names, _, _ := unstructured.NestedStringSlice(cluster.Object, "status", "instanceNames")
	for _, n := range names {
		if n != primary {
			return n
		}
	}
	return ""
}
