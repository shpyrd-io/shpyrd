package controller

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// The Postgres sleep state machine (RFC-0075, section 5), driven directly
// through reconcilePostgresSleep with a fake client: a ready gateway, a CNPG
// Cluster object to carry the hibernation annotation, and the "-rw"
// Endpoints standing in for CNPG's readiness.
func TestPostgresSleepStateMachine(t *testing.T) {
	storage := resource.MustParse("10Gi")
	pg := &shpyrdv1.Postgres{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-shop"},
		Spec:       shpyrdv1.PostgresSpec{Storage: &storage, Sleep: &shpyrdv1.PostgresSleepSpec{After: "30m"}},
	}
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(CNPGClusterGVK)
	cluster.SetName("db")
	cluster.SetNamespace("app-shop")
	gateway := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: pgGatewayName, Namespace: "shpyrd-system"},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 2},
	}
	rw := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{Name: "db-rw", Namespace: "app-shop"},
		Subsets:    []corev1.EndpointSubset{{Addresses: []corev1.EndpointAddress{{IP: "10.0.0.9"}}}},
	}
	base, c := newTestReconciler(t, pg, cluster, gateway, rw)
	r := &PostgresReconciler{Client: c, Scheme: base.Scheme, Recorder: record.NewFakeRecorder(20), SystemNamespace: "shpyrd-system"}
	ctx := context.Background()

	reconcile := func() {
		t.Helper()
		if err := r.reconcilePostgresSleep(ctx, pg); err != nil {
			t.Fatalf("reconcile sleep: %v", err)
		}
	}
	hibernated := func() bool {
		t.Helper()
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(CNPGClusterGVK)
		if err := c.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: "db"}, u); err != nil {
			t.Fatal(err)
		}
		return u.GetAnnotations()[pgHibernationAnnotation] == "on"
	}
	service := func() *corev1.Service {
		t.Helper()
		svc := &corev1.Service{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: "db"}, svc); err != nil {
			t.Fatal(err)
		}
		return svc
	}

	// 1. First pass: awake, wake port allocated, Service to the primary,
	// idle window started; no signal yet so no hibernation.
	reconcile()
	st := pg.Status.Sleep
	if st == nil || st.State != pgAwake || st.WakePort == nil || *st.WakePort < pgWakePortBase {
		t.Fatalf("after first pass: %+v", st)
	}
	if sel := service().Spec.Selector; sel["cnpg.io/instanceRole"] != "primary" {
		t.Errorf("awake selector = %v", sel)
	}

	// 2. Idle long enough but the activity signal is stale: stays awake.
	old := metav1.NewTime(time.Now().Add(-2 * time.Hour))
	st.LastActivityAt = &old
	st.ActivityCheckedAt = nil
	reconcile()
	if st.State != pgAwake || hibernated() {
		t.Fatalf("stale signal must not hibernate: %+v hibernated=%v", st, hibernated())
	}

	// 3. Fresh signal, idle ≥ after, gateway ready: hibernates; Service to
	// the gateway on the wake port.
	fresh := metav1.Now()
	st.ActivityCheckedAt = &fresh
	reconcile()
	if st.State != pgSleeping || !hibernated() {
		t.Fatalf("expected sleeping+hibernated: %+v hibernated=%v", st, hibernated())
	}
	svc := service()
	wantExt := gatewayServiceName(*st.WakePort) + ".shpyrd-system.svc.cluster.local"
	if svc.Spec.Type != corev1.ServiceTypeExternalName || svc.Spec.ExternalName != wantExt || svc.Spec.Selector != nil {
		t.Errorf("sleeping Service = type %s externalName %q selector %v", svc.Spec.Type, svc.Spec.ExternalName, svc.Spec.Selector)
	}
	gw := &corev1.Service{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: gatewayServiceName(*st.WakePort)}, gw); err != nil {
		t.Fatalf("gateway Service: %v", err)
	}
	if gw.Spec.Selector["app.kubernetes.io/name"] != pgGatewayName || gw.Spec.Ports[0].TargetPort.IntValue() != int(*st.WakePort) || gw.Spec.Ports[0].Port != PostgresPort {
		t.Errorf("gateway Service = selector %v ports %v", gw.Spec.Selector, gw.Spec.Ports)
	}

	// 4. The gateway saw a connection: state waking (it also removes the
	// annotation itself; the reconciler does too, as a backstop). Primary
	// ready → awake, fresh idle window, Service back to the primary.
	st.State = pgWaking
	reconcile()
	if hibernated() {
		t.Errorf("waking must clear the hibernation annotation")
	}
	if st.State != pgAwake || st.LastActivityAt == nil || time.Since(st.LastActivityAt.Time) > time.Minute {
		t.Fatalf("expected awake with a fresh idle window: %+v", st)
	}
	if s := service(); s.Spec.Type != corev1.ServiceTypeClusterIP || s.Spec.Selector["cnpg.io/instanceRole"] != "primary" || s.Spec.ExternalName != "" {
		t.Errorf("awake-again Service = type %s selector %v externalName %q", s.Spec.Type, s.Spec.Selector, s.Spec.ExternalName)
	}

	// 5. Waking with the primary not ready yet stays waking.
	st.State = pgWaking
	if err := c.Delete(ctx, rw); err != nil {
		t.Fatal(err)
	}
	reconcile()
	if st.State != pgWaking {
		t.Errorf("primary not ready: state = %s", st.State)
	}
	if err := c.Create(ctx, &corev1.Endpoints{ObjectMeta: metav1.ObjectMeta{Name: "db-rw", Namespace: "app-shop"},
		Subsets: []corev1.EndpointSubset{{Addresses: []corev1.EndpointAddress{{IP: "10.0.0.9"}}}}}); err != nil {
		t.Fatal(err)
	}
	reconcile()
	if st.State != pgAwake {
		t.Errorf("primary ready: state = %s", st.State)
	}

	// 6. Suspend: hibernated at once, Service points at nothing; resume
	// goes through waking.
	pg.Spec.Sleep.Suspended = true
	reconcile()
	if st.State != pgSuspended || !hibernated() {
		t.Fatalf("suspend: %+v hibernated=%v", st, hibernated())
	}
	if sel := service().Spec.Selector; sel["shpyrd.io/suspended-postgres"] != "db" {
		t.Errorf("suspended selector = %v", sel)
	}
	pg.Spec.Sleep.Suspended = false
	reconcile()
	if st.State != pgWaking || hibernated() {
		t.Fatalf("resume: %+v hibernated=%v", st, hibernated())
	}
	reconcile()
	if st.State != pgAwake {
		t.Errorf("resume completes: state = %s", st.State)
	}

	// 7. Policy removed: awake, both Services of ours gone.
	pg.Spec.Sleep = nil
	reconcile()
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: "db"}, &corev1.Service{}); err == nil {
		t.Errorf("Service should be deleted when the policy is removed")
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: gatewayServiceName(*st.WakePort)}, &corev1.Service{}); err == nil {
		t.Errorf("gateway Service should be deleted when the policy is removed")
	}
}

// Without the gateway running, an idle database with a fresh signal stays
// awake and says why.
func TestPostgresSleepNeedsGateway(t *testing.T) {
	storage := resource.MustParse("10Gi")
	pg := &shpyrdv1.Postgres{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-shop"},
		Spec:       shpyrdv1.PostgresSpec{Storage: &storage, Sleep: &shpyrdv1.PostgresSleepSpec{After: "5m"}},
	}
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(CNPGClusterGVK)
	cluster.SetName("db")
	cluster.SetNamespace("app-shop")
	base, c := newTestReconciler(t, pg, cluster)
	r := &PostgresReconciler{Client: c, Scheme: base.Scheme, Recorder: record.NewFakeRecorder(20), SystemNamespace: "shpyrd-system"}
	ctx := context.Background()
	if err := r.reconcilePostgresSleep(ctx, pg); err != nil {
		t.Fatal(err)
	}
	old := metav1.NewTime(time.Now().Add(-time.Hour))
	now := metav1.Now()
	pg.Status.Sleep.LastActivityAt, pg.Status.Sleep.ActivityCheckedAt = &old, &now
	if err := r.reconcilePostgresSleep(ctx, pg); err != nil {
		t.Fatal(err)
	}
	if pg.Status.Sleep.State != pgAwake || pg.Status.Sleep.Message == "" {
		t.Fatalf("no gateway: %+v", pg.Status.Sleep)
	}
}

// HA databases never sleep and get no Service of ours.
func TestPostgresSleepHANeverSleeps(t *testing.T) {
	storage := resource.MustParse("10Gi")
	pg := &shpyrdv1.Postgres{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-shop"},
		Spec:       shpyrdv1.PostgresSpec{Storage: &storage, Instances: ptr.To[int32](3), Sleep: &shpyrdv1.PostgresSleepSpec{After: "5m"}},
	}
	base, c := newTestReconciler(t, pg)
	r := &PostgresReconciler{Client: c, Scheme: base.Scheme, Recorder: record.NewFakeRecorder(20), SystemNamespace: "shpyrd-system"}
	if err := r.reconcilePostgresSleep(context.Background(), pg); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "db"}, &corev1.Service{}); err == nil {
		t.Errorf("HA database must not get the sleep Service")
	} else if client.IgnoreNotFound(err) != nil {
		t.Fatal(err)
	}
}
