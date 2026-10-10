// Package storagemigrate moves a database's data from one storage class to
// another by switchover (the storage plan, step 1): a second instance is
// provisioned on the target class, the primary switches over to it, the old
// instance goes, and the old volume is kept for three days as the way back.
// The database stays available throughout, except for the seconds of the
// switchover. Every step reads the state before acting, so a run that was
// interrupted continues where it stopped.
package storagemigrate

import (
	"context"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
)

// LabelRetainedMigration marks the volume a migration left behind, with
// the database it belonged to; `cluster status` lists it and it is deleted
// by hand after the keep.
const LabelRetainedMigration = "shpyrd.io/retained-migration"

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
	SizeBefore int64             `json:"sizeBefore,omitempty"`
	SizeAfter  int64             `json:"sizeAfter,omitempty"`
	Rows       map[string][2]int `json:"rows,omitempty"`
	Verified   bool              `json:"verified"`
	Notes      []string          `json:"notes,omitempty"`
	Steps      []string          `json:"steps"`
}

type migration struct {
	c    client.Client
	opts Options
	res  *Result
	key  types.NamespacedName
}

// Database migrates one database. It returns the result with an error when
// a step could not complete; the state it reached stays, and a second run
// continues from it.
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
	err := m.run(ctx)
	return m.res, err
}

func (m *migration) say(format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	m.res.Steps = append(m.res.Steps, line)
	fmt.Fprintln(m.opts.Out, line)
}

func (m *migration) run(ctx context.Context) error {
	pg := &shpyrdv1.Postgres{}
	if err := m.c.Get(ctx, m.key, pg); err != nil {
		return fmt.Errorf("the database: %w", err)
	}
	cluster, err := m.cluster(ctx)
	if err != nil {
		return err
	}
	primary := primaryOf(cluster)
	if primary == "" {
		return errors.New("the database has no primary instance yet; migrate it when it runs")
	}
	primaryClaim, err := m.claim(ctx, primary)
	if err != nil {
		return err
	}
	m.res.From = ptr.Deref(primaryClaim.Spec.StorageClassName, "")

	// Already there: the primary runs on the target class, one instance,
	// nothing marked. Only the ending may be owed.
	if m.res.From == m.opts.TargetClass && pg.Annotations[controller.AnnotationStorageMigration] == "" && instancesOf(cluster) == 1 {
		m.say("%s already has its data on %s; nothing to move", m.res.Database, m.opts.TargetClass)
		m.res.Verified = true
		return m.finish(ctx, pg, primaryClaim)
	}

	if err := m.preflight(ctx, pg, cluster, primary); err != nil {
		return err
	}

	// 1. Mark the database and ask for a second instance.
	if pg.Annotations[controller.AnnotationStorageMigration] != m.opts.TargetClass || ptr.Deref(pg.Spec.Instances, 1) < 2 {
		before := pg.DeepCopy()
		if pg.Annotations == nil {
			pg.Annotations = map[string]string{}
		}
		pg.Annotations[controller.AnnotationStorageMigration] = m.opts.TargetClass
		if ptr.Deref(pg.Spec.Instances, 1) < 2 {
			pg.Spec.Instances = ptr.To[int32](2)
		}
		if err := m.c.Patch(ctx, pg, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return fmt.Errorf("mark the database for migration: %w", err)
		}
		m.say("%s: a second instance on %s requested", m.res.Database, m.opts.TargetClass)
	}

	// 2. The new instance, on the target class, in sync.
	var newInstance string
	if m.res.From == m.opts.TargetClass {
		// Resumed after the switchover: the primary is already the new one.
		newInstance = primary
	} else {
		newInstance, err = m.waitNewInstance(ctx, primary)
		if err != nil {
			return err
		}
		newClaim, err := m.claim(ctx, newInstance)
		if err != nil {
			return err
		}
		if got := ptr.Deref(newClaim.Spec.StorageClassName, ""); got != m.opts.TargetClass {
			return fmt.Errorf("the new instance %s got a volume on %q, not %s: the platform did not apply the migration class; the database keeps running on its two instances, fix the platform and run this again", newInstance, got, m.opts.TargetClass)
		}
		m.res.NewClaim = newClaim.Name
		m.say("%s: instance %s ready on %s (%s), in sync", m.res.Database, newInstance, m.opts.TargetClass, newClaim.Spec.Resources.Requests.Storage().String())

		// 3. The data before, the switchover, the data after.
		if err := m.measure(ctx, primary, true); err != nil {
			return err
		}
		if err := m.switchover(ctx, newInstance); err != nil {
			return err
		}
		m.say("%s: switched over to %s", m.res.Database, newInstance)
		if err := m.measure(ctx, newInstance, false); err != nil {
			return err
		}
		m.verify()
	}

	// 4. Keep the old volume, then let the old instance go.
	old := primary
	if old == newInstance {
		old = otherInstance(cluster, newInstance)
	}
	if old != "" {
		if err := m.retain(ctx, old); err != nil {
			return err
		}
	}
	if ptr.Deref(pg.Spec.Instances, 1) != 1 {
		if err := m.c.Get(ctx, m.key, pg); err != nil {
			return err
		}
		before := pg.DeepCopy()
		pg.Spec.Instances = ptr.To[int32](1)
		if err := m.c.Patch(ctx, pg, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return fmt.Errorf("back to one instance: %w", err)
		}
	}
	if err := m.waitInstances(ctx, 1, newInstance); err != nil {
		return err
	}
	m.say("%s: one instance again, %s on %s; the old volume %s is kept", m.res.Database, newInstance, m.opts.TargetClass, m.res.OldPV)

	newClaim, err := m.claim(ctx, newInstance)
	if err != nil {
		return err
	}
	if err := m.c.Get(ctx, m.key, pg); err != nil {
		return err
	}
	return m.finish(ctx, pg, newClaim)
}

// finish clears the marks (the migration annotation, the node pin) so the
// profile's rules apply to the cluster on its new class, and snapshots the
// new volume.
func (m *migration) finish(ctx context.Context, pg *shpyrdv1.Postgres, claim *corev1.PersistentVolumeClaim) error {
	if pg.Annotations[controller.AnnotationStorageMigration] != "" || pg.Annotations[shpyrdv1.AnnotationPlacement] != "" {
		before := pg.DeepCopy()
		delete(pg.Annotations, controller.AnnotationStorageMigration)
		delete(pg.Annotations, shpyrdv1.AnnotationPlacement)
		if err := m.c.Patch(ctx, pg, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return fmt.Errorf("clear the migration marks: %w", err)
		}
		m.say("%s: migration marks and node pin cleared; the profile's size applies now", m.res.Database)
	}
	if m.opts.SnapshotClass == "" || claim == nil {
		return nil
	}
	name := claim.Name + "-" + time.Now().UTC().Format("20060102-150405")
	snap := &unstructured.Unstructured{}
	snap.SetGroupVersionKind(controller.VolumeSnapshotGVK)
	snap.SetName(name)
	snap.SetNamespace(claim.Namespace)
	snap.SetLabels(map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", LabelRetainedMigration: m.opts.Name})
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

func (m *migration) preflight(ctx context.Context, pg *shpyrdv1.Postgres, cluster *unstructured.Unstructured, primary string) error {
	if pg.Annotations[controller.AnnotationDataMove] != "" {
		return errors.New("the database is being moved between nodes; finish that first")
	}
	if pg.Spec.Sleep != nil && pg.Spec.Sleep.Suspended {
		return errors.New("the database is suspended; resume it first, the migration needs it running")
	}
	sc := &storagev1.StorageClass{}
	if err := m.c.Get(ctx, types.NamespacedName{Name: m.opts.TargetClass}, sc); err != nil {
		return fmt.Errorf("the storage class %q: %w", m.opts.TargetClass, err)
	}
	ready, _, _ := unstructured.NestedInt64(cluster.Object, "status", "readyInstances")
	if ready < 1 {
		return errors.New("the database has no ready instance; migrate it when it is healthy")
	}
	// Room for the new instance: another node in the pool the cluster runs
	// in (the data pool, or any node), not the primary's.
	pool, _, _ := unstructured.NestedString(cluster.Object, "spec", "affinity", "nodeSelector", controller.PoolLabel)
	primaryNode, err := m.nodeOfPod(ctx, primary)
	if err != nil {
		return err
	}
	nodes := &corev1.NodeList{}
	if err := m.c.List(ctx, nodes); err != nil {
		return err
	}
	others := 0
	for _, n := range nodes.Items {
		if n.Name == primaryNode || (pool != "" && n.Labels[controller.PoolLabel] != pool) {
			continue
		}
		for _, cond := range n.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				others++
			}
		}
	}
	if others == 0 {
		where := "the cluster"
		if pool != "" {
			where = "the " + pool + " pool"
		}
		return fmt.Errorf("no other ready node in %s for the new instance; add a node first", where)
	}
	return nil
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

func (m *migration) nodeOfPod(ctx context.Context, instance string) (string, error) {
	pod := &corev1.Pod{}
	if err := m.c.Get(ctx, types.NamespacedName{Namespace: m.opts.Namespace, Name: instance}, pod); err != nil {
		return "", fmt.Errorf("the pod of instance %s: %w", instance, err)
	}
	return pod.Spec.NodeName, nil
}

// waitNewInstance waits for a second instance, ready, other than primary.
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
		other := otherInstance(cluster, primary)
		return other != "" && ready >= 2 && primaryOf(cluster) == primary, nil
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

// switchover asks CloudNativePG to make the new instance the primary, as
// its own promote does: the target primary in the status, with the phase.
func (m *migration) switchover(ctx context.Context, newInstance string) error {
	cluster, err := m.cluster(ctx)
	if err != nil {
		return err
	}
	if primaryOf(cluster) != newInstance {
		before := cluster.DeepCopy()
		_ = unstructured.SetNestedField(cluster.Object, newInstance, "status", "targetPrimary")
		_ = unstructured.SetNestedField(cluster.Object, "Switchover in progress", "status", "phase")
		_ = unstructured.SetNestedField(cluster.Object, "Switching over to "+newInstance+" (storage migration)", "status", "phaseReason")
		if err := m.c.Status().Patch(ctx, cluster, client.MergeFrom(before)); err != nil {
			return fmt.Errorf("ask for the switchover: %w", err)
		}
	}
	return m.until(ctx, "the switchover to "+newInstance, func() (bool, error) {
		cluster, err := m.cluster(ctx)
		if err != nil {
			return false, err
		}
		ready, _, _ := unstructured.NestedInt64(cluster.Object, "status", "readyInstances")
		return primaryOf(cluster) == newInstance && ready >= 2, nil
	})
}

// retain keeps the old instance's volume: reclaim policy Retain, labelled
// with the database, before CloudNativePG removes the claim.
func (m *migration) retain(ctx context.Context, instance string) error {
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
	if pv.Spec.PersistentVolumeReclaimPolicy == corev1.PersistentVolumeReclaimRetain && pv.Labels[LabelRetainedMigration] != "" {
		return nil
	}
	before := pv.DeepCopy()
	pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
	if pv.Labels == nil {
		pv.Labels = map[string]string{}
	}
	pv.Labels[LabelRetainedMigration] = m.opts.Name
	if err := m.c.Patch(ctx, pv, client.MergeFrom(before)); err != nil {
		return fmt.Errorf("retain the old volume: %w", err)
	}
	m.say("%s: old volume %s kept (Retain); release it after the keep", m.res.Database, pv.Name)
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
	deadline := time.Now().Add(m.opts.Wait)
	for {
		ok, err := done()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("waited %s for %s; the state so far stays, run this again to continue", m.opts.Wait.Round(time.Second), what)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.opts.Poll):
		}
	}
}

// measure reads the database size and the row count of every user table on
// an instance; before the switchover on the old primary, after it on the new.
func (m *migration) measure(ctx context.Context, instance string, before bool) error {
	if m.opts.SQL == nil {
		return nil
	}
	size, err := m.opts.SQL(ctx, m.opts.Namespace, instance, "SELECT pg_database_size(current_database())")
	if err != nil {
		return fmt.Errorf("measure %s: %w", instance, err)
	}
	bytes, _ := strconv.ParseInt(strings.TrimSpace(size), 10, 64)
	tables, err := m.opts.SQL(ctx, m.opts.Namespace, instance, "SELECT quote_ident(schemaname)||'.'||quote_ident(relname) FROM pg_stat_user_tables ORDER BY 1")
	if err != nil {
		return fmt.Errorf("list tables on %s: %w", instance, err)
	}
	for _, table := range strings.Split(strings.TrimSpace(tables), "\n") {
		if table == "" {
			continue
		}
		out, err := m.opts.SQL(ctx, m.opts.Namespace, instance, "SELECT count(*) FROM "+table)
		if err != nil {
			return fmt.Errorf("count %s on %s: %w", table, instance, err)
		}
		n, _ := strconv.Atoi(strings.TrimSpace(out))
		counts := m.res.Rows[table]
		if before {
			counts[0] = n
		} else {
			counts[1] = n
		}
		m.res.Rows[table] = counts
	}
	if before {
		m.res.SizeBefore = bytes
	} else {
		m.res.SizeAfter = bytes
	}
	return nil
}

// verify compares what was measured: the size within 5 %, and no table
// with fewer rows after than before (rows written during the switchover
// window only add).
func (m *migration) verify() {
	if m.opts.SQL == nil {
		m.res.Notes = append(m.res.Notes, "data checks skipped (no way to run SQL on the instances)")
		return
	}
	ok := true
	if m.res.SizeBefore > 0 {
		diff := float64(m.res.SizeAfter-m.res.SizeBefore) / float64(m.res.SizeBefore)
		if diff < -0.05 || diff > 0.05 {
			ok = false
			m.res.Notes = append(m.res.Notes, fmt.Sprintf("database size %d before, %d after (%.1f%%)", m.res.SizeBefore, m.res.SizeAfter, diff*100))
		}
	}
	tables := make([]string, 0, len(m.res.Rows))
	for t := range m.res.Rows {
		tables = append(tables, t)
	}
	sort.Strings(tables)
	for _, t := range tables {
		c := m.res.Rows[t]
		if c[1] < c[0] {
			ok = false
			m.res.Notes = append(m.res.Notes, fmt.Sprintf("%s: %d rows before, %d after", t, c[0], c[1]))
		}
	}
	m.res.Verified = ok
	if ok {
		m.say("%s: data verified on the new instance (%d tables, %d bytes)", m.res.Database, len(tables), m.res.SizeAfter)
	} else {
		m.say("%s: DATA CHECK FAILED: %s", m.res.Database, strings.Join(m.res.Notes, "; "))
	}
}

func primaryOf(cluster *unstructured.Unstructured) string {
	p, _, _ := unstructured.NestedString(cluster.Object, "status", "currentPrimary")
	return p
}

func instancesOf(cluster *unstructured.Unstructured) int64 {
	n, _, _ := unstructured.NestedInt64(cluster.Object, "spec", "instances")
	return n
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

// Retained lists the volumes migrations left behind, by database.
func Retained(ctx context.Context, c client.Client) ([]corev1.PersistentVolume, error) {
	list := &corev1.PersistentVolumeList{}
	if err := c.List(ctx, list, client.HasLabels{LabelRetainedMigration}); err != nil {
		return nil, err
	}
	out := list.Items
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

var _ = metav1.Now
