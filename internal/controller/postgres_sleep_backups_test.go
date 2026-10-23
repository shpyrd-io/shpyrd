package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// sleepBackupFixture is a dedicated database with backups and a sleep
// policy, idle long enough and with a fresh activity signal, so that the
// only thing between it and hibernation is its backup.
type sleepBackupFixture struct {
	t  *testing.T
	c  client.Client
	r  *PostgresReconciler
	pg *shpyrdv1.Postgres
}

func newSleepBackupFixture(t *testing.T, lastBackup *metav1.Time, extra ...client.Object) *sleepBackupFixture {
	t.Helper()
	storage := resource.MustParse("10Gi")
	pg := &shpyrdv1.Postgres{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-shop"},
		Spec:       shpyrdv1.PostgresSpec{Storage: &storage, Sleep: &shpyrdv1.PostgresSleepSpec{After: "30m"}, Backups: &shpyrdv1.PostgresBackups{}},
	}
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(CNPGClusterGVK)
	cluster.SetName("db")
	cluster.SetNamespace("app-shop")
	schedule := &unstructured.Unstructured{}
	schedule.SetGroupVersionKind(CNPGScheduledBackupGVK)
	schedule.SetName("db-scheduled")
	schedule.SetNamespace("app-shop")
	schedule.Object["spec"] = map[string]interface{}{"schedule": "0 0 2 * * *", "cluster": map[string]interface{}{"name": "db"}}
	gateway := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: pgGatewayName, Namespace: "shpyrd-system"}, Status: appsv1.DeploymentStatus{ReadyReplicas: 1}}
	rw := &corev1.Endpoints{ObjectMeta: metav1.ObjectMeta{Name: "db-rw", Namespace: "app-shop"}, Subsets: []corev1.EndpointSubset{{Addresses: []corev1.EndpointAddress{{IP: "10.0.0.9"}}}}}
	objs := append([]client.Object{pg, cluster, schedule, gateway, rw}, extra...)
	base, c := newTestReconciler(t, objs...)
	r := &PostgresReconciler{Client: c, Scheme: base.Scheme, Recorder: record.NewFakeRecorder(20), SystemNamespace: "shpyrd-system", SleepAllowed: always}
	f := &sleepBackupFixture{t: t, c: c, r: r, pg: pg}
	f.reconcile() // awake, wake port, idle window started
	twoHours := metav1.NewTime(time.Now().Add(-2 * time.Hour))
	fresh := metav1.Now()
	pg.Status.Sleep.LastActivityAt, pg.Status.Sleep.ActivityCheckedAt = &twoHours, &fresh
	pg.Status.LastBackup = lastBackup
	return f
}

func (f *sleepBackupFixture) reconcile() {
	f.t.Helper()
	if err := f.r.reconcilePostgresSleep(context.Background(), f.pg); err != nil {
		f.t.Fatalf("reconcile sleep: %v", err)
	}
}

func (f *sleepBackupFixture) backup(name string) *unstructured.Unstructured {
	f.t.Helper()
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(CNPGBackupGVK)
	if err := f.c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: name}, b); err != nil {
		return nil
	}
	return b
}

func (f *sleepBackupFixture) finish(name, phase string) {
	f.t.Helper()
	b := f.backup(name)
	if b == nil {
		f.t.Fatalf("no backup %s", name)
	}
	_ = unstructured.SetNestedField(b.Object, phase, "status", "phase")
	_ = unstructured.SetNestedField(b.Object, time.Now().UTC().Format(time.RFC3339), "status", "stoppedAt")
	if err := f.c.Update(context.Background(), b); err != nil {
		f.t.Fatal(err)
	}
}

func (f *sleepBackupFixture) scheduleSuspended() bool {
	f.t.Helper()
	s := &unstructured.Unstructured{}
	s.SetGroupVersionKind(CNPGScheduledBackupGVK)
	if err := f.c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "db-scheduled"}, s); err != nil {
		f.t.Fatal(err)
	}
	v, _, _ := unstructured.NestedBool(s.Object, "spec", "suspend")
	return v
}

// A database with backups and no fresh copy is copied before it sleeps; the
// schedule is suspended while it sleeps and resumed at the wake, and a copy
// is taken after the wake when the last one is still old.
func TestPostgresSleepTakesABackupFirst(t *testing.T) {
	f := newSleepBackupFixture(t, nil)
	f.reconcile()
	st := f.pg.Status.Sleep
	if st.State != pgAwake || !strings.HasPrefix(st.Message, "awake: idle, taking a backup before sleeping") {
		t.Fatalf("expected to wait for the backup: %+v", st)
	}
	b := f.backup("db-before-sleep")
	if b == nil {
		t.Fatal("no before-sleep Backup requested")
	}
	if cl, _, _ := unstructured.NestedString(b.Object, "spec", "cluster", "name"); cl != "db" {
		t.Errorf("backup cluster = %q", cl)
	}
	if m, _, _ := unstructured.NestedString(b.Object, "spec", "method"); m != "plugin" {
		t.Errorf("backup method = %q", m)
	}
	if f.scheduleSuspended() {
		t.Error("the schedule must run while awake")
	}

	// Still running: still awake.
	f.reconcile()
	if st.State != pgAwake {
		t.Fatalf("a running backup must hold hibernation: %+v", st)
	}

	// Completed: sleeps; the schedule is suspended.
	f.finish("db-before-sleep", "completed")
	f.reconcile()
	if st.State != pgSleeping || strings.Contains(st.Message, noFreshBackupNote) {
		t.Fatalf("expected a clean sleep: %+v", st)
	}
	if !f.scheduleSuspended() {
		t.Error("the schedule must be suspended while asleep")
	}

	// Woken: the schedule resumes; with no fresh copy on record a backup
	// after the wake is taken.
	st.State = pgWaking
	f.reconcile()
	if st.State != pgAwake {
		t.Fatalf("expected awake: %+v", st)
	}
	if f.scheduleSuspended() {
		t.Error("the schedule must resume at the wake")
	}
	if f.backup("db-after-wake") == nil {
		t.Error("no after-wake Backup requested")
	}
}

// A fresh backup needs no copy: the database sleeps at once and nothing is
// requested; a wake with a fresh copy requests nothing either.
func TestPostgresSleepWithAFreshBackup(t *testing.T) {
	recent := metav1.NewTime(time.Now().Add(-time.Hour))
	f := newSleepBackupFixture(t, &recent)
	f.reconcile()
	if f.pg.Status.Sleep.State != pgSleeping {
		t.Fatalf("expected sleeping: %+v", f.pg.Status.Sleep)
	}
	if f.backup("db-before-sleep") != nil {
		t.Error("a fresh backup must not be copied again")
	}
	f.pg.Status.Sleep.State = pgWaking
	f.reconcile()
	if f.backup("db-after-wake") != nil {
		t.Error("no copy after a wake with a fresh backup")
	}
}

// Past the wait, or after a failed copy, the database sleeps anyway and the
// status and an event say that no fresh backup exists.
func TestPostgresSleepWithoutABackupWarns(t *testing.T) {
	stuck := &unstructured.Unstructured{}
	stuck.SetGroupVersionKind(CNPGBackupGVK)
	stuck.SetName("db-before-sleep")
	stuck.SetNamespace("app-shop")
	stuck.SetAnnotations(map[string]string{annotationBackupRequestedAt: time.Now().Add(-61 * time.Minute).UTC().Format(time.RFC3339)})
	stuck.Object["spec"] = map[string]interface{}{"cluster": map[string]interface{}{"name": "db"}}
	stuck.Object["status"] = map[string]interface{}{"phase": "running"}
	f := newSleepBackupFixture(t, nil, stuck)
	f.reconcile()
	st := f.pg.Status.Sleep
	if st.State != pgSleeping || !strings.Contains(st.Message, noFreshBackupNote+"the backup before sleeping did not finish") {
		t.Fatalf("expected to sleep with a warning: %+v", st)
	}
	if d := pgSleepDescription(f.pg); !strings.Contains(d, noFreshBackupNote) {
		t.Errorf("description hides the warning: %q", d)
	}
	select {
	case ev := <-f.r.Recorder.(*record.FakeRecorder).Events:
		if !strings.Contains(ev, "SleepWithoutBackup") {
			t.Errorf("event = %q", ev)
		}
	default:
		t.Error("no event for a sleep without a backup")
	}

	failed := &unstructured.Unstructured{}
	failed.SetGroupVersionKind(CNPGBackupGVK)
	failed.SetName("db-before-sleep")
	failed.SetNamespace("app-shop")
	failed.SetAnnotations(map[string]string{annotationBackupRequestedAt: time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)})
	failed.Object["spec"] = map[string]interface{}{"cluster": map[string]interface{}{"name": "db"}}
	failed.Object["status"] = map[string]interface{}{"phase": "failed"}
	g := newSleepBackupFixture(t, nil, failed)
	g.reconcile()
	if st := g.pg.Status.Sleep; st.State != pgSleeping || !strings.Contains(st.Message, "the backup before sleeping failed") {
		t.Fatalf("a failed copy must not hold hibernation for ever: %+v", st)
	}
}

// A copy from an earlier cycle is replaced, a scheduled backup already
// running is waited for rather than doubled.
func TestPostgresSleepBackupCycles(t *testing.T) {
	old := &unstructured.Unstructured{}
	old.SetGroupVersionKind(CNPGBackupGVK)
	old.SetName("db-before-sleep")
	old.SetNamespace("app-shop")
	old.SetAnnotations(map[string]string{annotationBackupRequestedAt: time.Now().Add(-3 * 24 * time.Hour).UTC().Format(time.RFC3339)})
	old.Object["spec"] = map[string]interface{}{"cluster": map[string]interface{}{"name": "db"}}
	old.Object["status"] = map[string]interface{}{"phase": "completed", "stoppedAt": time.Now().Add(-3 * 24 * time.Hour).UTC().Format(time.RFC3339)}
	f := newSleepBackupFixture(t, nil, old)
	f.reconcile()
	if f.pg.Status.Sleep.State != pgAwake {
		t.Fatalf("an old copy must not pass for this sleep's: %+v", f.pg.Status.Sleep)
	}
	if b := f.backup("db-before-sleep"); b == nil || backupRequestedAt(b).Before(time.Now().Add(-time.Minute)) {
		t.Errorf("the old copy must be replaced by a new request: %v", b)
	}

	scheduled := &unstructured.Unstructured{}
	scheduled.SetGroupVersionKind(CNPGBackupGVK)
	scheduled.SetName("db-scheduled-20261021020000")
	scheduled.SetNamespace("app-shop")
	scheduled.SetAnnotations(map[string]string{annotationBackupRequestedAt: time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339)})
	scheduled.Object["spec"] = map[string]interface{}{"cluster": map[string]interface{}{"name": "db"}}
	scheduled.Object["status"] = map[string]interface{}{"phase": "running"}
	g := newSleepBackupFixture(t, nil, scheduled)
	g.reconcile()
	if g.pg.Status.Sleep.State != pgAwake || g.backup("db-before-sleep") != nil {
		t.Fatalf("a running scheduled backup is waited for, not doubled: %+v %v", g.pg.Status.Sleep, g.backup("db-before-sleep"))
	}
}

func TestSchedulePeriod(t *testing.T) {
	for _, tc := range []struct {
		schedule string
		want     time.Duration
	}{
		{"", 24 * time.Hour},
		{"0 2 * * *", 24 * time.Hour},
		{"0 */6 * * *", 6 * time.Hour},
		{"*/30 * * * *", 30 * time.Minute},
		{"not a schedule", 24 * time.Hour},
	} {
		if got := schedulePeriod(&shpyrdv1.PostgresBackups{Schedule: tc.schedule}); got != tc.want {
			t.Errorf("schedulePeriod(%q) = %s, want %s", tc.schedule, got, tc.want)
		}
	}
}

// A dedicated database always says what sleep applies and where it comes
// from (#120), and its endpoint is the host apps get: the sleep-aware
// Service while a policy applies, CNPG's -rw otherwise.
func TestPostgresSleepReadBackAndEndpoint(t *testing.T) {
	ctx := context.Background()
	mk := func(name string, sleep *shpyrdv1.PostgresSleepSpec) *shpyrdv1.Postgres {
		storage := resource.MustParse("10Gi")
		return &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "p-free"}, Spec: shpyrdv1.PostgresSpec{Storage: &storage, Sleep: sleep}}
	}
	own, off, byDefault, none := mk("own", &shpyrdv1.PostgresSleepSpec{After: "30m"}), mk("off", &shpyrdv1.PostgresSleepSpec{After: "off"}), mk("bydefault", nil), mk("none", nil)
	none.Namespace = "p-paid"
	var objs []client.Object
	for _, pg := range []*shpyrdv1.Postgres{own, off, byDefault, none} {
		cluster := &unstructured.Unstructured{}
		cluster.SetGroupVersionKind(CNPGClusterGVK)
		cluster.SetName(pg.Name)
		cluster.SetNamespace(pg.Namespace)
		objs = append(objs, pg, cluster)
	}
	gateway := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: pgGatewayName, Namespace: "shpyrd-system"}, Status: appsv1.DeploymentStatus{ReadyReplicas: 1}}
	base, c := newTestReconciler(t, append(objs, gateway)...)
	r := &PostgresReconciler{Client: c, Scheme: base.Scheme, Recorder: record.NewFakeRecorder(20), SystemNamespace: "shpyrd-system", SleepAllowed: always,
		WorkspaceSleepDefault: func(_ context.Context, ns string) string {
			if ns == "p-free" {
				return "10m"
			}
			return ""
		}}
	for _, tc := range []struct {
		pg                  *shpyrdv1.Postgres
		source, after, host string
	}{
		{own, "project", "30m", "own"},
		{off, "off", "", "off-rw"},
		{byDefault, "workspace", "10m0s", "bydefault"},
		{none, "workspace", "", "none-rw"},
	} {
		if err := r.reconcilePostgresSleep(ctx, tc.pg); err != nil {
			t.Fatalf("%s: %v", tc.pg.Name, err)
		}
		st := tc.pg.Status.Sleep
		if st == nil || st.Source != tc.source || st.After != tc.after || st.State != pgAwake {
			t.Errorf("%s: read-back = %+v, want source %q after %q awake", tc.pg.Name, st, tc.source, tc.after)
		}
		if host := r.sleepHost(ctx, tc.pg); host != tc.host {
			t.Errorf("%s: host = %q, want %q", tc.pg.Name, host, tc.host)
		}
	}
}
