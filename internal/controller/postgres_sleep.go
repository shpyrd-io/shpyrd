package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// Postgres sleep (RFC-0075): CloudNativePG declarative hibernation with a
// shpyrd-owned Service and a TCP wake-proxy.
//
// The shpyrd Service "<name>" (distinct from CNPG's "<name>-rw") exists
// only for databases with a sleep policy. Its selector points at the CNPG
// primary while awake, and at the pg-gateway pods — on the database's wake
// port — while sleeping or waking. Hibernation itself only happens when the
// gateway is running: without it a sleeping database could not be woken.
//
// Status (v0.9.18): bindings still hand apps "<name>-rw", and no activity
// signal maintains LastActivityAt yet, so this stays behind the gateway
// gate and the CLI does not expose it. See RFC-0075 "Implementation status".

const (
	// pgWakePortBase is the start of the wake-port allocation range.
	pgWakePortBase = 15000
	// pgWakePortCap is the exclusive end of the range.
	pgWakePortCap = 16000
	// pgGatewayName is the pg-gateway Deployment (system namespace) and the
	// app.kubernetes.io/name label on its pods.
	pgGatewayName = "pg-gateway"
	// pgHibernationAnnotation is the CNPG declarative-hibernation annotation.
	pgHibernationAnnotation = "cnpg.io/hibernation"
)

// sleepAfterDuration parses the sleep after value; returns 0 when disabled.
func sleepAfterDuration(spec *shpyrdv1.PostgresSleepSpec) time.Duration {
	if spec == nil {
		return 0
	}
	return parseSleepDuration(spec.After)
}

// activityStaleAfter is how old the activity check may be before the
// reconciler refuses to hibernate: without a fresh signal (Prometheus down,
// metering loop not leader) a busy database must never be put to sleep.
const activityStaleAfter = 15 * time.Minute

// Sleep states of a database (status.sleep.state).
const (
	pgAwake     = "awake"
	pgSleeping  = "sleeping"
	pgWaking    = "waking"
	pgSuspended = "suspended"
)

// reconcilePostgresSleep manages the shpyrd-owned Service and the
// hibernation state machine for a Postgres resource (RFC-0075, section 5).
// It is called by the Postgres reconciler after the CNPG Cluster is
// reconciled; the reconciler requeues every minute while a policy exists.
//
//	awake ──(idle ≥ after, fresh signal, gateway ready)──▶ sleeping
//	sleeping ──(gateway sees a connection: status=waking)──▶ waking
//	waking ──(CNPG primary ready)──▶ awake
//	any ──(spec.sleep.suspended)──▶ suspended ──(resume)──▶ waking
func (r *PostgresReconciler) reconcilePostgresSleep(ctx context.Context, pg *shpyrdv1.Postgres) error {
	after := sleepAfterDuration(pg.Spec.Sleep)
	suspended := pg.Spec.Sleep != nil && pg.Spec.Sleep.Suspended

	// No policy, or an HA database (never sleeps): make sure it is awake
	// and carry no Service of ours.
	if (after == 0 && !suspended) || instances(pg) > 1 {
		if err := r.ensurePostgresAwake(ctx, pg); err != nil {
			return err
		}
		if pg.Status.Sleep != nil && pg.Status.Sleep.State != pgAwake && pg.Status.Sleep.State != "" {
			pg.Status.Sleep.State, pg.Status.Sleep.Message = pgAwake, ""
		}
		return r.deletePostgresService(ctx, pg)
	}

	if pg.Status.Sleep == nil {
		pg.Status.Sleep = &shpyrdv1.PostgresSleepStatus{}
	}
	sleep := pg.Status.Sleep
	if sleep.WakePort == nil {
		port, err := r.allocateWakePort(ctx, pg)
		if err != nil {
			return fmt.Errorf("allocate wake port: %w", err)
		}
		p := int32(port)
		sleep.WakePort = &p
	}
	if sleep.State == "" {
		sleep.State = pgAwake
	}

	// Explicit suspend wins over everything: down now, no wake on connect.
	if suspended {
		if sleep.State != pgSuspended {
			if err := r.setCNPGHibernation(ctx, pg, true); err != nil {
				return err
			}
			sleep.State = pgSuspended
			sleep.Message = "suspended by request; shpyrd pg resume brings it back"
		}
		return r.ensurePostgresService(ctx, pg)
	}

	switch sleep.State {
	case pgSuspended:
		// Resumed: leave suspension through the normal wake path.
		if err := r.setCNPGHibernation(ctx, pg, false); err != nil {
			return err
		}
		sleep.State, sleep.Message = pgWaking, "resuming"
		return r.ensurePostgresService(ctx, pg)

	case pgAwake:
		if err := r.ensurePostgresService(ctx, pg); err != nil {
			return err
		}
		return r.maybeHibernate(ctx, pg, after)

	case pgSleeping:
		// The gateway flips the state to waking when a connection arrives;
		// nothing to do but keep the Service pointing at the gateway.
		return r.ensurePostgresService(ctx, pg)

	case pgWaking:
		// The gateway (or a resume) asked for a wake: the annotation must be
		// off; when CNPG reports the primary ready the Service goes back to
		// it and a fresh idle window starts.
		if err := r.setCNPGHibernation(ctx, pg, false); err != nil {
			return err
		}
		if r.primaryReady(ctx, pg) {
			now := metav1.Now()
			sleep.State, sleep.Message = pgAwake, ""
			sleep.LastActivityAt = &now
			sleep.ActivityCheckedAt = &now
		}
		return r.ensurePostgresService(ctx, pg)
	}
	return nil
}

// maybeHibernate hibernates the database when the idle window has elapsed,
// the activity signal is fresh and the wake proxy is running to bring it
// back. Otherwise it records why not in the status message.
func (r *PostgresReconciler) maybeHibernate(ctx context.Context, pg *shpyrdv1.Postgres, after time.Duration) error {
	sleep := pg.Status.Sleep
	now := time.Now()
	if sleep.LastActivityAt == nil {
		t := metav1.NewTime(now)
		sleep.LastActivityAt = &t
		sleep.Message = "idle window started"
		return nil
	}
	if sleep.ActivityCheckedAt == nil || now.Sub(sleep.ActivityCheckedAt.Time) > activityStaleAfter {
		sleep.Message = "awake: waiting for a fresh activity signal (Prometheus/metering)"
		return nil
	}
	idle := now.Sub(sleep.LastActivityAt.Time)
	if idle < after {
		sleep.Message = fmt.Sprintf("awake: idle for %s of %s", idle.Truncate(time.Second), after)
		return nil
	}
	ready, err := r.gatewayReady(ctx)
	if err != nil {
		return err
	}
	if !ready {
		sleep.Message = "awake: idle, but the pg-gateway is not running (nothing could wake it)"
		return nil
	}
	// Hibernate via the CNPG annotation; the Service goes to the gateway.
	if err := r.setCNPGHibernation(ctx, pg, true); err != nil {
		return err
	}
	sleep.State = pgSleeping
	sleep.Message = fmt.Sprintf("sleeping since %s (idle %s)", now.UTC().Format(time.RFC3339), idle.Truncate(time.Second))
	log.FromContext(ctx).Info("database hibernated", "postgres", pg.Name, "idle", idle.Truncate(time.Second))
	return r.ensurePostgresService(ctx, pg)
}

// ensurePostgresAwake removes the hibernation annotation when the database
// is asleep and no policy asks for it (policy removed, HA enabled).
func (r *PostgresReconciler) ensurePostgresAwake(ctx context.Context, pg *shpyrdv1.Postgres) error {
	if pg.Status.Sleep == nil {
		return nil
	}
	switch pg.Status.Sleep.State {
	case pgSleeping, pgSuspended, pgWaking:
		return r.setCNPGHibernation(ctx, pg, false)
	}
	return nil
}

// primaryReady reports whether CNPG's "<name>-rw" Service has an endpoint:
// CNPG adds the primary only once it passes readiness, so this is "accepting
// connections", not just "pod running".
func (r *PostgresReconciler) primaryReady(ctx context.Context, pg *shpyrdv1.Postgres) bool {
	ep := &corev1.Endpoints{}
	if err := r.Get(ctx, types.NamespacedName{Name: pg.Name + "-rw", Namespace: pg.Namespace}, ep); err != nil {
		return false
	}
	for _, s := range ep.Subsets {
		if len(s.Addresses) > 0 {
			return true
		}
	}
	return false
}

// postgresIsHibernated: the Service points at the gateway in these states.
func (r *PostgresReconciler) postgresIsHibernated(_ context.Context, pg *shpyrdv1.Postgres) bool {
	if pg.Status.Sleep == nil {
		return false
	}
	return pg.Status.Sleep.State == pgSleeping || pg.Status.Sleep.State == pgWaking
}

// ensurePostgresService creates or updates the shpyrd-owned "<name>" Service
// (distinct from CNPG's "<name>-rw") that apps reference as DATABASE_HOST.
//
//	awake      ClusterIP, selector = the CNPG primary.
//	sleeping/  ExternalName → pgwake-<port>.<system>.svc, a Service in the
//	waking     system namespace that fronts the gateway pods on this
//	           database's wake port (a selector cannot cross namespaces).
//	suspended  ClusterIP with a selector nothing matches: refused at once.
//
// Hibernation happens with no clients connected, so the switch to the
// ExternalName never races a live connection; on wake both paths serve —
// the gateway proxies straight through once the primary is up.
func (r *PostgresReconciler) ensurePostgresService(ctx context.Context, pg *shpyrdv1.Postgres) error {
	if r.postgresIsHibernated(ctx, pg) && pg.Status.Sleep.WakePort != nil {
		if err := r.ensureGatewayService(ctx, pg); err != nil {
			return err
		}
	}
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: pg.Name, Namespace: pg.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Labels = mergeMaps(svc.Labels, map[string]string{
			shpyrdv1.LabelManagedBy: "shpyrd",
			"shpyrd.io/postgres":    pg.Name,
		})
		port := corev1.ServicePort{Name: "postgres", Port: PostgresPort, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(PostgresPort)}
		switch {
		case r.postgresIsHibernated(ctx, pg) && pg.Status.Sleep.WakePort != nil:
			svc.Spec.Type = corev1.ServiceTypeExternalName
			svc.Spec.ExternalName = gatewayServiceName(*pg.Status.Sleep.WakePort) + "." + r.SystemNamespace + ".svc.cluster.local"
			svc.Spec.Selector = nil
			svc.Spec.ClusterIP, svc.Spec.ClusterIPs = "", nil
		case pg.Status.Sleep != nil && pg.Status.Sleep.State == pgSuspended:
			svc.Spec.Type = corev1.ServiceTypeClusterIP
			svc.Spec.ExternalName = ""
			svc.Spec.Selector = map[string]string{"shpyrd.io/suspended-postgres": pg.Name}
		default:
			svc.Spec.Type = corev1.ServiceTypeClusterIP
			svc.Spec.ExternalName = ""
			// CNPG labels its pods cnpg.io/cluster and cnpg.io/instanceRole.
			svc.Spec.Selector = map[string]string{"cnpg.io/cluster": pg.Name, "cnpg.io/instanceRole": "primary"}
		}
		svc.Spec.Ports = []corev1.ServicePort{port}
		return controllerutil.SetControllerReference(pg, svc, r.Scheme)
	})
	return err
}

// gatewayServiceName is the per-database Service in the system namespace
// that fronts the gateway pods on one wake port. Ports are unique across
// databases, so the port names the Service.
func gatewayServiceName(port int32) string { return fmt.Sprintf("pgwake-%d", port) }

// ensureGatewayService creates the system-namespace Service for a sleeping
// database: 5432 in, the database's wake port on the gateway pods out. It
// is labelled with the database it serves and deleted with the policy; an
// owner reference cannot cross namespaces.
func (r *PostgresReconciler) ensureGatewayService(ctx context.Context, pg *shpyrdv1.Postgres) error {
	port := *pg.Status.Sleep.WakePort
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: gatewayServiceName(port), Namespace: r.SystemNamespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Labels = mergeMaps(svc.Labels, map[string]string{
			shpyrdv1.LabelManagedBy:      "shpyrd",
			"shpyrd.io/postgres-wake":    pg.Namespace + "." + pg.Name,
			"shpyrd.io/postgres-wake-ns": pg.Namespace,
		})
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.Selector = map[string]string{"app.kubernetes.io/name": pgGatewayName}
		svc.Spec.Ports = []corev1.ServicePort{{Name: "postgres", Port: PostgresPort, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(port)}}
		return nil
	})
	return err
}

// deletePostgresService removes the shpyrd-owned "<name>" Service when the
// database has no sleep policy. Only a Service we labelled is touched.
func (r *PostgresReconciler) deletePostgresService(ctx context.Context, pg *shpyrdv1.Postgres) error {
	svc := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Name: pg.Name, Namespace: pg.Namespace}, svc); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if svc.Labels[shpyrdv1.LabelManagedBy] != "shpyrd" || svc.Labels["shpyrd.io/postgres"] != pg.Name {
		return nil
	}
	if err := r.Delete(ctx, svc); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if pg.Status.Sleep != nil && pg.Status.Sleep.WakePort != nil {
		gw := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: gatewayServiceName(*pg.Status.Sleep.WakePort), Namespace: r.SystemNamespace}}
		if err := r.Delete(ctx, gw); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// gatewayReady reports whether the pg-gateway Deployment in the system
// namespace has a ready replica. Hibernation waits for it.
func (r *PostgresReconciler) gatewayReady(ctx context.Context) (bool, error) {
	if r.SystemNamespace == "" {
		return false, nil
	}
	var dep appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: pgGatewayName, Namespace: r.SystemNamespace}, &dep); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return dep.Status.ReadyReplicas > 0, nil
}

// setCNPGHibernation sets or removes the cnpg.io/hibernation annotation on
// the CNPG Cluster object (CloudNativePG declarative hibernation).
func (r *PostgresReconciler) setCNPGHibernation(ctx context.Context, pg *shpyrdv1.Postgres, on bool) error {
	cluster, err := r.getCNPGCluster(ctx, pg)
	if err != nil || cluster == nil {
		return err
	}
	ann := cluster.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	if on {
		ann[pgHibernationAnnotation] = "on"
	} else {
		delete(ann, pgHibernationAnnotation)
	}
	cluster.SetAnnotations(ann)
	return r.Client.Update(ctx, cluster)
}

// getCNPGCluster fetches the CNPG Cluster object as unstructured.
func (r *PostgresReconciler) getCNPGCluster(ctx context.Context, pg *shpyrdv1.Postgres) (*unstructured.Unstructured, error) {
	u := makeUnstructured(CNPGClusterGVK, pg.Name, pg.Namespace)
	if err := r.Client.Get(ctx, types.NamespacedName{Name: pg.Name, Namespace: pg.Namespace}, u); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return u, nil
}

// allocateWakePort finds an unused port in the range [pgWakePortBase, pgWakePortCap).
func (r *PostgresReconciler) allocateWakePort(ctx context.Context, pg *shpyrdv1.Postgres) (int, error) {
	// List all Postgres resources in all namespaces to find taken ports.
	var list shpyrdv1.PostgresList
	if err := r.Client.List(ctx, &list); err != nil {
		return 0, err
	}
	taken := map[int]bool{}
	for _, p := range list.Items {
		if p.Status.Sleep != nil && p.Status.Sleep.WakePort != nil {
			taken[int(*p.Status.Sleep.WakePort)] = true
		}
	}
	for port := pgWakePortBase; port < pgWakePortCap; port++ {
		if !taken[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("all wake ports in range %d-%d are allocated", pgWakePortBase, pgWakePortCap-1)
}

// WakeDatabase wakes a sleeping database — called by the pg-gateway when a
// connection arrives on the wake port.
func (r *PostgresReconciler) WakeDatabase(ctx context.Context, namespace, name string) error {
	pg := &shpyrdv1.Postgres{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pg); err != nil {
		return err
	}
	if pg.Status.Sleep == nil || pg.Status.Sleep.State != "sleeping" {
		return nil // already awake
	}
	return r.ensurePostgresAwake(ctx, pg)
}

// UpdateSleepActivity records user activity, preventing premature hibernation.
// Called from the metering loop or any signal source.
func (r *PostgresReconciler) UpdateSleepActivity(ctx context.Context, namespace, name string) error {
	pg := &shpyrdv1.Postgres{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pg); err != nil {
		return err
	}
	if pg.Status.Sleep == nil {
		pg.Status.Sleep = &shpyrdv1.PostgresSleepStatus{}
	}
	now := metav1.Now()
	pg.Status.Sleep.LastActivityAt = &now
	return r.Client.Status().Update(ctx, pg)
}

// pgSleepDescription returns a human-readable description of the sleep state.
func pgSleepDescription(pg *shpyrdv1.Postgres) string {
	if pg.Status.Sleep == nil {
		return ""
	}
	switch pg.Status.Sleep.State {
	case pgSleeping:
		return "sleeping (volumes kept, data safe; wakes on the first connection)"
	case pgWaking:
		return "waking up — connections will succeed shortly"
	case pgSuspended:
		return "suspended — resume with shpyrd pg resume"
	}
	return ""
}
