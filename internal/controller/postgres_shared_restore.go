package controller

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// Restoring a shared database (storage plan, step 3): a new shared
// database whose recovery names another and one of its copies gets that
// copy loaded once its own role and database exist, by a Job in the
// server's namespace running pg_restore as the new owner, reading the
// copy from the source server's bucket. The Database object records that
// it is done; the Postgres is Ready only then.
const annotationSharedRestored = "shpyrd.io/restored"

func sharedRestoreJob(hash string) string { return "restore-" + hash }

// reconcileSharedRestore loads the copy and reports whether it is in.
func (r *PostgresReconciler) reconcileSharedRestore(ctx context.Context, pg *shpyrdv1.Postgres, hash, server string) (bool, error) {
	db := &unstructured.Unstructured{}
	db.SetGroupVersionKind(CNPGDatabaseGVK)
	if err := r.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: sharedDBObject(hash)}, db); err != nil {
		return false, err
	}
	if db.GetAnnotations()[annotationSharedRestored] == "done" {
		return true, nil
	}
	rec := pg.Spec.Recovery
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: sharedRestoreJob(hash), Namespace: SharedPostgresNamespace}}
	err := r.Get(ctx, client.ObjectKeyFromObject(job), job)
	if apierrors.IsNotFound(err) {
		sourceServer, sourceDB, err := r.sharedRestoreSource(ctx, pg)
		if err != nil {
			return false, err
		}
		if rec.Backup == "" {
			return false, fmt.Errorf("recovery.backup names the copy of %s to restore", rec.From)
		}
		pull, err := r.ensureSharedPullSecret(ctx)
		if err != nil {
			return false, err
		}
		job = r.desiredSharedRestoreJob(pg, hash, server, sourceServer, sourceDB+"/"+rec.Backup+".dump", pull)
		if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
			return false, err
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return false, fmt.Errorf("restoring the copy %s of %s failed (%s); delete this database and try another copy", rec.Backup, rec.From, firstNonEmpty(cond.Message, cond.Reason))
		}
	}
	if job.Status.Succeeded < 1 {
		return false, nil
	}
	before := db.DeepCopy()
	ann := db.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	ann[annotationSharedRestored] = "done"
	db.SetAnnotations(ann)
	if err := r.Patch(ctx, db, client.MergeFrom(before)); err != nil {
		return false, err
	}
	r.Recorder.Eventf(pg, corev1.EventTypeNormal, "Restored", "loaded the copy %s of %s", rec.Backup, rec.From)
	return true, nil
}

// sharedRestoreSource is the server and database name the copies of
// recovery.from are under: the live database's, else those of the
// Database object kept behind its drop date after a deletion.
func (r *PostgresReconciler) sharedRestoreSource(ctx context.Context, pg *shpyrdv1.Postgres) (server, database string, err error) {
	from := pg.Spec.Recovery.From
	source := &shpyrdv1.Postgres{}
	err = r.Get(ctx, types.NamespacedName{Namespace: pg.Namespace, Name: from}, source)
	switch {
	case err == nil:
		if source.Spec.Type != shpyrdv1.PostgresTypeShared {
			return "", "", fmt.Errorf("%s is a dedicated database; its backups restore into a dedicated database", from)
		}
		if server = source.Annotations[AnnotationSharedServer]; server == "" {
			return "", "", &pendingError{msg: fmt.Sprintf("the database %s is not placed on a shared server yet", from)}
		}
		return server, sharedDatabaseName(tenantHash(source.UID)), nil
	case !apierrors.IsNotFound(err):
		return "", "", err
	}
	kept := &unstructured.UnstructuredList{}
	kept.SetGroupVersionKind(CNPGDatabaseGVK.GroupVersion().WithKind("DatabaseList"))
	if err := r.List(ctx, kept, client.InNamespace(SharedPostgresNamespace), client.MatchingLabels{sharedTenantNamespaceLabel: pg.Namespace, sharedTenantNameLabel: from}); err != nil {
		return "", "", err
	}
	if len(kept.Items) != 1 {
		return "", "", fmt.Errorf("no shared database %q in this project to restore from", from)
	}
	name, _, _ := unstructured.NestedString(kept.Items[0].Object, "spec", "name")
	return kept.Items[0].GetLabels()[sharedOfServerLabel], name, nil
}

// desiredSharedRestoreJob loads one copy: the platform's binary in the
// CloudNativePG image, connected as the new database's own role so what
// the dump creates is the role's, the copy read from the source server's
// bucket.
func (r *PostgresReconciler) desiredSharedRestoreJob(pg *shpyrdv1.Postgres, hash, server, sourceServer, key string, pull []corev1.LocalObjectReference) *batchv1.Job {
	env := func(name, k string) corev1.EnvVar {
		return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: sharedRoleObject(hash)}, Key: k}}}
	}
	labels := sharedTenantLabels(pg, server)
	locked := &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}
	tools := corev1.VolumeMount{Name: "tools", MountPath: sharedToolsDir}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: sharedRestoreJob(hash), Namespace: SharedPostgresNamespace, Labels: labels},
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
						Name:    "restore",
						Image:   "ghcr.io/cloudnative-pg/postgresql:" + defaultPostgresVersion,
						Command: []string{sharedToolsDir + "/shpyrd-server", "restore-dump"},
						Env: []corev1.EnvVar{
							{Name: "PGHOST", Value: server + "-rw"},
							{Name: "PGPORT", Value: fmt.Sprint(PostgresPort)},
							{Name: "PGDATABASE", Value: sharedDatabaseName(hash)},
							{Name: "PGSSLMODE", Value: "require"},
							env("PGUSER", "username"),
							env("PGPASSWORD", "password"),
							{Name: "SHPYRD_RESTORE_KEY", Value: key},
						},
						EnvFrom:         []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: SharedDumpsName(sourceServer)}}}},
						VolumeMounts:    []corev1.VolumeMount{tools},
						Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resourceMustParse("50m"), corev1.ResourceMemory: resourceMustParse("128Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resourceMustParse("512Mi")}},
						SecurityContext: locked,
					}},
					Volumes: []corev1.Volume{{Name: "tools", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: ptr.To(resourceMustParse("512Mi"))}}}},
				},
			},
		},
	}
}
