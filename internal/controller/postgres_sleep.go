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

// reconcilePostgresSleep manages the shpyrd-owned Service and the
// hibernation state for a Postgres resource. It is called by the Postgres
// reconciler after the CNPG Cluster is reconciled.
func (r *PostgresReconciler) reconcilePostgresSleep(ctx context.Context, pg *shpyrdv1.Postgres) error {
	after := sleepAfterDuration(pg.Spec.Sleep)

	// No policy, or an HA database (never sleeps): make sure it is awake
	// and carry no Service of ours.
	if after == 0 || instances(pg) > 1 {
		if err := r.ensurePostgresAwake(ctx, pg); err != nil {
			return err
		}
		return r.deletePostgresService(ctx, pg)
	}

	if err := r.ensurePostgresService(ctx, pg); err != nil {
		return err
	}

	// Assign a wake port if not yet done.
	if pg.Status.Sleep == nil {
		pg.Status.Sleep = &shpyrdv1.PostgresSleepStatus{}
	}
	if pg.Status.Sleep.WakePort == nil {
		port, err := r.allocateWakePort(ctx, pg)
		if err != nil {
			return fmt.Errorf("allocate wake port: %w", err)
		}
		p := int32(port)
		pg.Status.Sleep.WakePort = &p
	}

	// Evaluate whether to hibernate.
	if pg.Status.Sleep.State == "" {
		pg.Status.Sleep.State = "awake"
	}
	switch pg.Status.Sleep.State {
	case "awake":
		ready, err := r.gatewayReady(ctx)
		if err != nil {
			return err
		}
		if !ready {
			log.FromContext(ctx).V(1).Info("sleep policy set but pg-gateway is not running; staying awake", "postgres", pg.Name)
			return nil
		}
		return r.maybeHibernate(ctx, pg, after)
	case "sleeping", "waking":
		// The gateway is responsible for waking on connect; we just watch.
		return r.syncServiceSelector(ctx, pg)
	case "suspended":
		return nil
	}
	return nil
}

// ensurePostgresService creates or updates the shpyrd-owned "<name>" Service
// (distinct from CNPG's "<name>-rw") that apps reference as DATABASE_HOST.
// Its selector switches between the CNPG primary pod and the gateway pod.
func (r *PostgresReconciler) ensurePostgresService(ctx context.Context, pg *shpyrdv1.Postgres) error {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: pg.Name, Namespace: pg.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Labels = mergeMaps(svc.Labels, map[string]string{
			shpyrdv1.LabelManagedBy: "shpyrd",
			"shpyrd.io/postgres":    pg.Name,
		})
		port := corev1.ServicePort{Name: "postgres", Port: PostgresPort, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(PostgresPort)}
		// Awake: the CNPG primary (CNPG labels its pods cnpg.io/cluster and
		// cnpg.io/instanceRole). Sleeping or waking: the gateway pods, on
		// this database's wake port.
		svc.Spec.Selector = map[string]string{"cnpg.io/cluster": pg.Name, "cnpg.io/instanceRole": "primary"}
		if r.postgresIsHibernated(ctx, pg) {
			if wp := pg.Status.Sleep.WakePort; wp != nil {
				svc.Spec.Selector = map[string]string{"app.kubernetes.io/name": pgGatewayName}
				port.TargetPort = intstr.FromInt32(*wp)
			}
		}
		svc.Spec.Ports = []corev1.ServicePort{port}
		return controllerutil.SetControllerReference(pg, svc, r.Scheme)
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

// syncServiceSelector flips the Service selector to match the sleep state.
func (r *PostgresReconciler) syncServiceSelector(ctx context.Context, pg *shpyrdv1.Postgres) error {
	return r.ensurePostgresService(ctx, pg)
}

// postgresIsHibernated checks the CNPG annotation on the Cluster object.
func (r *PostgresReconciler) postgresIsHibernated(ctx context.Context, pg *shpyrdv1.Postgres) bool {
	if pg.Status.Sleep == nil {
		return false
	}
	return pg.Status.Sleep.State == "sleeping" || pg.Status.Sleep.State == "waking"
}

// maybeHibernate hibernates the database when the idle window has elapsed.
func (r *PostgresReconciler) maybeHibernate(ctx context.Context, pg *shpyrdv1.Postgres, after time.Duration) error {
	sleep := pg.Status.Sleep
	if sleep == nil {
		return nil
	}
	if sleep.LastActivityAt == nil {
		now := metav1.Now()
		sleep.LastActivityAt = &now
		return nil
	}
	if time.Since(sleep.LastActivityAt.Time) < after {
		return nil // still within the idle window
	}
	// Hibernate via the CNPG annotation.
	if err := r.setCNPGHibernation(ctx, pg, true); err != nil {
		return err
	}
	sleep.State = "sleeping"
	if err := r.syncServiceSelector(ctx, pg); err != nil {
		return err
	}
	return nil
}

// ensurePostgresAwake removes the hibernation annotation if set.
func (r *PostgresReconciler) ensurePostgresAwake(ctx context.Context, pg *shpyrdv1.Postgres) error {
	if pg.Status.Sleep != nil && pg.Status.Sleep.State == "sleeping" {
		if err := r.setCNPGHibernation(ctx, pg, false); err != nil {
			return err
		}
		pg.Status.Sleep.State = "waking"
		return r.syncServiceSelector(ctx, pg)
	}
	return nil
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
	case "sleeping":
		return "sleeping (volumes kept, data safe)"
	case "waking":
		return "waking up — connections will succeed shortly"
	case "suspended":
		return "suspended — resume with shpyrd pg resume"
	}
	return ""
}
