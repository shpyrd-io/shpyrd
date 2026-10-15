package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/pgdump"
)

// Nightly dumps (storage plan, step 3). Every shared server has a dump
// role that is neither superuser nor a tenant: login, pg_read_all_data and
// BYPASSRLS, managed by CloudNativePG from the Cluster spec, with CONNECT
// granted per database by the tenant's grants Job. A CronJob in the
// server's namespace runs `shpyrd-server dumps` at night: pg_dump -Fc of
// every database the role may open, as that role over TCP, streamed into
// the server's own bucket, the newest seven of each database kept. The
// run needs pg_dump and the platform's object store client, so it is the
// CloudNativePG image with the platform's static binary copied in by an
// init container; the pod holds no Kubernetes credential.
const (
	sharedDumpRole  = "shpyrd_dump"
	sharedDumpsKeep = 7
	// sharedDumpsHour is the UTC hour the dumps start; the servers are
	// spread over it by their number.
	sharedDumpsHour = 3
	sharedToolsDir  = "/tools"
)

func sharedDumpRoleSecret(server string) string { return server + "-dump" }

// SharedDumpsName names a server's dumps bucket, its credential Secret and
// its CronJob.
func SharedDumpsName(server string) string { return server + "-dumps" }

// SharedDatabaseName is a shared database's name on its server.
func SharedDatabaseName(pg *shpyrdv1.Postgres) string { return sharedDatabaseName(tenantHash(pg.UID)) }

// The environment a Job cut from the CronJob adds for one database now.
const (
	envDumpsDatabase = "SHPYRD_DUMPS_DATABASE"
	envDumpsKind     = "SHPYRD_DUMPS_KIND"
	envDumpsAt       = "SHPYRD_DUMPS_AT"
	sharedDumpPart   = "dump"
)

// SharedDumpJob is a dump of one shared database now: a Job cut from its
// server's CronJob, told the database, that it was asked for, and the
// moment, which names the copy so whoever asked can follow it by name.
func SharedDumpJob(pg *shpyrdv1.Postgres, cj *batchv1.CronJob, at time.Time) *batchv1.Job {
	hash, server := tenantHash(pg.UID), pg.Annotations[AnnotationSharedServer]
	labels := sharedTenantLabels(pg, server)
	labels[sharedServerLabel] = sharedDumpPart
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("dump-%s-%s", hash, at.UTC().Format("20060102150405")),
			Namespace:       SharedPostgresNamespace,
			Labels:          labels,
			Annotations:     map[string]string{"cronjob.kubernetes.io/instantiate": "manual"},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "CronJob", Name: cj.Name, UID: cj.UID}},
		},
		Spec: *cj.Spec.JobTemplate.Spec.DeepCopy(),
	}
	job.Spec.BackoffLimit = ptr.To[int32](1)
	job.Spec.Template.Labels = mergeMaps(job.Spec.Template.Labels, labels)
	for i, c := range job.Spec.Template.Spec.Containers {
		if c.Name == sharedDumpPart {
			job.Spec.Template.Spec.Containers[i].Env = append(c.Env,
				corev1.EnvVar{Name: envDumpsDatabase, Value: sharedDatabaseName(hash)},
				corev1.EnvVar{Name: envDumpsKind, Value: pgdump.KindNow},
				corev1.EnvVar{Name: envDumpsAt, Value: at.UTC().Format(time.RFC3339)})
		}
	}
	return job
}

// SharedDumpJobsSelector selects the dump Jobs asked for a shared database.
func SharedDumpJobsSelector(pg *shpyrdv1.Postgres) string {
	return sharedTenantLabel + "=" + string(pg.UID) + "," + sharedServerLabel + "=" + sharedDumpPart
}

// SharedDumpAt is the moment a dump Job's copy is named by.
func SharedDumpAt(job *batchv1.Job) (time.Time, bool) {
	for _, c := range job.Spec.Template.Spec.Containers {
		if c.Name != sharedDumpPart {
			continue
		}
		for _, e := range c.Env {
			if e.Name == envDumpsAt {
				at, err := time.Parse(time.RFC3339, e.Value)
				return at, err == nil
			}
		}
	}
	return time.Time{}, false
}

// sharedServerLabels mark an object of a server's: its name and which
// part it is.
func sharedServerLabels(server, part string) map[string]string {
	return map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", sharedServerLabel: part, sharedOfServerLabel: server}
}

// sharedDumpManagedRole is the dump role's entry in spec.managed.roles.
func sharedDumpManagedRole(server string) map[string]interface{} {
	return map[string]interface{}{
		"name":            sharedDumpRole,
		"ensure":          "present",
		"login":           true,
		"bypassrls":       true,
		"inRoles":         []interface{}{"pg_read_all_data"},
		"connectionLimit": int64(3),
		"passwordSecret":  map[string]interface{}{"name": sharedDumpRoleSecret(server)},
	}
}

// ensureSharedDumps is everything a server needs for its nightly dumps.
func (r *PostgresReconciler) ensureSharedDumps(ctx context.Context, server *unstructured.Unstructured) error {
	name := server.GetName()
	if err := r.ensureSharedDumpRoleSecret(ctx, name); err != nil {
		return err
	}
	if err := r.ensureSharedDumpRole(ctx, server); err != nil {
		return err
	}
	if err := r.ensureSharedDumpsBucket(ctx, name); err != nil {
		return err
	}
	if r.ServerImage == "" {
		log.FromContext(ctx).Info("no nightly dumps: the server image is not known (SHPYRD_SERVER_IMAGE)", "server", name)
		return nil
	}
	pull, err := r.ensureSharedPullSecret(ctx)
	if err != nil {
		return err
	}
	return r.ensureSharedDumpsCronJob(ctx, name, pull)
}

// ensureSharedDumpRoleSecret is the dump role's password, generated once:
// the basic-auth Secret CloudNativePG reads and the dumps pod uses.
func (r *PostgresReconciler) ensureSharedDumpRoleSecret(ctx context.Context, server string) error {
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: sharedDumpRoleSecret(server), Namespace: SharedPostgresNamespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sec, func() error {
		sec.Labels = mergeMaps(sec.Labels, sharedServerLabels(server, "dumps"))
		sec.Type = corev1.SecretTypeBasicAuth
		if sec.Data == nil {
			sec.Data = map[string][]byte{}
		}
		sec.Data["username"] = []byte(sharedDumpRole)
		if len(sec.Data["password"]) == 0 {
			sec.Data["password"] = []byte(randomPassword())
		}
		return nil
	})
	return err
}

// ensureSharedDumpRole puts the dump role on a server rendered before it
// had one.
func (r *PostgresReconciler) ensureSharedDumpRole(ctx context.Context, server *unstructured.Unstructured) error {
	roles, _, _ := unstructured.NestedSlice(server.Object, "spec", "managed", "roles")
	for _, role := range roles {
		if m, ok := role.(map[string]interface{}); ok && m["name"] == sharedDumpRole {
			return nil
		}
	}
	before := server.DeepCopy()
	roles = append(roles, sharedDumpManagedRole(server.GetName()))
	if err := unstructured.SetNestedSlice(server.Object, roles, "spec", "managed", "roles"); err != nil {
		return err
	}
	return r.Patch(ctx, server, client.MergeFrom(before))
}

// ensureSharedDumpsBucket is the server's bucket, its credential Secret
// named after it. Retained on deletion: the dumps outlive a server.
func (r *PostgresReconciler) ensureSharedDumpsBucket(ctx context.Context, server string) error {
	b := &shpyrdv1.ObjectBucket{ObjectMeta: metav1.ObjectMeta{Name: SharedDumpsName(server), Namespace: SharedPostgresNamespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, b, func() error {
		b.Labels = mergeMaps(b.Labels, sharedServerLabels(server, "dumps"))
		b.Spec.SecretName = SharedDumpsName(server)
		b.Spec.DeletionPolicy = "Retain"
		return nil
	})
	return err
}

// ensureSharedPullSecret mirrors the platform's image pull Secret into the
// server's namespace when the platform has one (on the cloud a private
// registry holds the server image), and names it for the pod.
func (r *PostgresReconciler) ensureSharedPullSecret(ctx context.Context) ([]corev1.LocalObjectReference, error) {
	src := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Namespace: r.SystemNamespace, Name: install.ImagePullSecretName}, src)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	mirror := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: install.ImagePullSecretName, Namespace: SharedPostgresNamespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, mirror, func() error {
		mirror.Labels = mergeMaps(mirror.Labels, map[string]string{shpyrdv1.LabelManagedBy: "shpyrd"})
		mirror.Type = src.Type
		mirror.Data = src.Data
		return nil
	}); err != nil {
		return nil, fmt.Errorf("mirror the image pull secret: %w", err)
	}
	return []corev1.LocalObjectReference{{Name: install.ImagePullSecretName}}, nil
}

// sharedDumpsSchedule spreads the servers over the dumps hour: pg-shared-1
// at :07, pg-shared-2 at :14, and so on.
func sharedDumpsSchedule(server string) string {
	n, _ := strconv.Atoi(strings.TrimPrefix(server, sharedServerPrefix))
	return fmt.Sprintf("%d %d * * *", (n*7)%60, sharedDumpsHour)
}

func (r *PostgresReconciler) ensureSharedDumpsCronJob(ctx context.Context, server string, pull []corev1.LocalObjectReference) error {
	desired := r.desiredSharedDumpsCronJob(server, pull)
	cj := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cj, func() error {
		cj.Labels = mergeMaps(cj.Labels, desired.Labels)
		cj.Spec = desired.Spec
		return nil
	})
	return err
}

// desiredSharedDumpsCronJob is the nightly run: the platform's binary
// copied into the CloudNativePG image, connecting as the dump role, the
// bucket's credential from its Secret.
func (r *PostgresReconciler) desiredSharedDumpsCronJob(server string, pull []corev1.LocalObjectReference) *batchv1.CronJob {
	env := func(name, key string) corev1.EnvVar {
		return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: sharedDumpRoleSecret(server)}, Key: key}}}
	}
	labels := sharedServerLabels(server, "dumps")
	locked := &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}
	tools := corev1.VolumeMount{Name: "tools", MountPath: sharedToolsDir}
	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: SharedDumpsName(server), Namespace: SharedPostgresNamespace, Labels: labels},
		Spec: batchv1.CronJobSpec{
			Schedule:                   sharedDumpsSchedule(server),
			TimeZone:                   ptr.To("Etc/UTC"),
			ConcurrencyPolicy:          batchv1.ForbidConcurrent,
			SuccessfulJobsHistoryLimit: ptr.To[int32](3),
			FailedJobsHistoryLimit:     ptr.To[int32](3),
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: batchv1.JobSpec{
					BackoffLimit:            ptr.To[int32](2),
					ActiveDeadlineSeconds:   ptr.To[int64](6 * 3600),
					TTLSecondsAfterFinished: ptr.To[int32](86400),
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec: corev1.PodSpec{
							RestartPolicy:                corev1.RestartPolicyNever,
							AutomountServiceAccountToken: ptr.To(false),
							ImagePullSecrets:             pull,
							SecurityContext:              &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To[int64](26), RunAsGroup: ptr.To[int64](26), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
							InitContainers: []corev1.Container{{
								Name:            "tools",
								Image:           r.ServerImage,
								Args:            []string{"copy-self", sharedToolsDir},
								VolumeMounts:    []corev1.VolumeMount{tools},
								Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resourceMustParse("10m"), corev1.ResourceMemory: resourceMustParse("32Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resourceMustParse("128Mi")}},
								SecurityContext: locked,
							}},
							Containers: []corev1.Container{{
								Name:    sharedDumpPart,
								Image:   "ghcr.io/cloudnative-pg/postgresql:" + defaultPostgresVersion,
								Command: []string{sharedToolsDir + "/shpyrd-server", "dumps"},
								Env: []corev1.EnvVar{
									{Name: "PGHOST", Value: server + "-rw"},
									{Name: "PGPORT", Value: fmt.Sprint(PostgresPort)},
									{Name: "PGDATABASE", Value: "postgres"},
									{Name: "PGSSLMODE", Value: "require"},
									env("PGUSER", "username"),
									env("PGPASSWORD", "password"),
									{Name: "SHPYRD_DUMPS_KEEP", Value: fmt.Sprint(sharedDumpsKeep)},
								},
								EnvFrom:         []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: SharedDumpsName(server)}}}},
								VolumeMounts:    []corev1.VolumeMount{tools},
								Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resourceMustParse("50m"), corev1.ResourceMemory: resourceMustParse("128Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resourceMustParse("512Mi")}},
								SecurityContext: locked,
							}},
							Volumes: []corev1.Volume{{Name: "tools", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: ptr.To(resourceMustParse("512Mi"))}}}},
						},
					},
				},
			},
		},
	}
}
