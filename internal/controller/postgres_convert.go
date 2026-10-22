package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/pgdump"
)

// Converting a database (storage plan, step 3): shpyrd pg convert <name>
// --to dedicated|shared moves a database between the platform's shared
// server and a cluster of its own, under its name, so the apps bound to
// it keep their binding. The API stamps the Postgres with the kind asked
// for; the controller walks the steps, each recorded on the object, so a
// restart carries on where it was:
//
//	to dedicated: the shared role is paused; the spec becomes a dedicated
//	one and the cluster comes up with the shared credentials as its own
//	(the same user and database names, so DATABASE_URL changes only its
//	host); a Job copies the rows into it and counts them; the credentials
//	turn to the cluster; the shared database stays, closed, behind a drop
//	date of seven days.
//
//	to shared: a role and a database are made on a shared server while the
//	cluster keeps serving; a Job pauses the cluster's owner, checks the
//	extensions and the size against what a shared database holds, copies
//	the rows over and counts them; the cluster goes, and the shared
//	credentials take its place (new user and database names: one config
//	release). The copy the Job took stays in the server's bucket as any
//	copy does, so the source's last state can be restored for seven days.
//
// A Job that fails puts the database back as it was and says why.
const (
	// AnnotationConvert is the kind the database is being converted to.
	AnnotationConvert = "shpyrd.io/convert"
	// AnnotationConvertOptions carries the conversion's ConvertOptions.
	AnnotationConvertOptions = "shpyrd.io/convert-options"
	// AnnotationConverted records a finished conversion.
	AnnotationConverted = "shpyrd.io/converted"
	// AnnotationConvertFailed says why the last conversion was not done;
	// the next one clears it.
	AnnotationConvertFailed = "shpyrd.io/convert-failed"
	annotationConvertStep   = "shpyrd.io/convert-step"
	// annotationConvertJob names this conversion's Job, decided before the
	// Job is made: a conversion back within a day must not read the
	// finished Job of the one before.
	annotationConvertJob = "shpyrd.io/convert-job"

	convertStepTarget = "target" // the new home is coming up
	convertStepLoad   = "load"   // the rows are being copied
	convertStepSwap   = "swap"   // the credentials turn over
)

// ConvertOptions are what the API records with the conversion: a
// dedicated database's disk and size, and the shared cap (the new one
// going to shared; the one to go back to, going to dedicated).
type ConvertOptions struct {
	Storage string `json:"storage,omitempty"`
	Size    string `json:"size,omitempty"`
	Cap     string `json:"cap,omitempty"`
}

func convertingTo(pg *shpyrdv1.Postgres) string { return pg.Annotations[AnnotationConvert] }
func converting(pg *shpyrdv1.Postgres) bool     { return convertingTo(pg) != "" }

func convertOptions(pg *shpyrdv1.Postgres) ConvertOptions {
	var o ConvertOptions
	_ = json.Unmarshal([]byte(pg.Annotations[AnnotationConvertOptions]), &o)
	return o
}

func sharedConvertSecret(hash string) string { return "convert-" + hash }

// convertJobName is this conversion's Job: recorded on the object once,
// before the Job exists; empty until then.
func convertJobName(pg *shpyrdv1.Postgres) string { return pg.Annotations[annotationConvertJob] }

// newConvertJobName names a conversion's Job after the tenant and the
// moment, so no two conversions of one database share a Job.
func newConvertJobName(hash string, now time.Time) string {
	return fmt.Sprintf("convert-%s-%s", hash, strconv.FormatInt(now.Unix(), 36))
}

// nameConvertJob records the Job's name and comes back to make it.
func (r *PostgresReconciler) nameConvertJob(ctx context.Context, pg *shpyrdv1.Postgres, hash string) (ctrl.Result, error) {
	name := newConvertJobName(hash, time.Now())
	return r.patchMeta(ctx, pg, func(pg *shpyrdv1.Postgres) { pg.Annotations[annotationConvertJob] = name })
}

// sharedAllowedExtensions are what a shared database can hold: the
// extensions PostgreSQL 17 marks trusted, which the owner creates alone,
// and the ones the platform installs (platformExtensions).
var sharedAllowedExtensions = []string{
	"btree_gin", "btree_gist", "citext", "cube", "dict_int", "earthdistance", "fuzzystrmatch", "hstore", "intarray", "isn", "lo",
	"ltree", "pgcrypto", "pg_trgm", "seg", "tablefunc", "tcn", "tsm_system_rows", "tsm_system_time", "unaccent", "uuid-ossp",
	"vector",
}

// typeOf is the database's kind, dedicated unless said.
func typeOf(pg *shpyrdv1.Postgres) string {
	if pg.Spec.Type == shpyrdv1.PostgresTypeShared {
		return shpyrdv1.PostgresTypeShared
	}
	return shpyrdv1.PostgresTypeDedicated
}

// convertMessage is the status while a conversion runs.
func convertMessage(to, step string) string {
	words := map[string]string{
		"":                "preparing (step 1 of 3)",
		convertStepTarget: "waiting for the new cluster (step 1 of 3)",
		convertStepLoad:   "copying the data (step 2 of 3); connections are paused meanwhile",
		convertStepSwap:   "switching the credentials over (step 3 of 3)",
	}
	return fmt.Sprintf("Converting to a %s database: %s", to, words[step])
}

// reconcileConvert runs the conversion the database is stamped with.
func (r *PostgresReconciler) reconcileConvert(ctx context.Context, pg *shpyrdv1.Postgres) (ctrl.Result, error) {
	to, step := convertingTo(pg), pg.Annotations[annotationConvertStep]
	if to == typeOf(pg) && step == "" {
		return r.patchMeta(ctx, pg, func(pg *shpyrdv1.Postgres) {
			delete(pg.Annotations, AnnotationConvert)
			delete(pg.Annotations, AnnotationConvertOptions)
		})
	}
	var res ctrl.Result
	var err error
	switch to {
	case shpyrdv1.PostgresTypeDedicated:
		res, err = r.convertToDedicated(ctx, pg, step)
	case shpyrdv1.PostgresTypeShared:
		res, err = r.convertToShared(ctx, pg, step)
	default:
		return r.convertFailed(ctx, pg, fmt.Sprintf("%q is not a kind of database", to), nil)
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	// The conversion's words, over what the pass over the cluster or the
	// tenant wrote.
	pg.Status.Phase = shpyrdv1.ResourceProvisioning
	pg.Status.Message = convertMessage(to, step)
	setResourceCondition(&pg.Status, pg.Generation, metav1.ConditionFalse, "Converting", pg.Status.Message)
	if res.IsZero() {
		res = ctrl.Result{RequeueAfter: 10 * time.Second}
	}
	return res, nil
}

// patchMeta changes the object's metadata or spec (not its status) and
// asks to come back in a second: a timed requeue, not Requeue, which
// controller-runtime counts as a failure and backs off from exponentially.
func (r *PostgresReconciler) patchMeta(ctx context.Context, pg *shpyrdv1.Postgres, mutate func(*shpyrdv1.Postgres)) (ctrl.Result, error) {
	before := pg.DeepCopy()
	if pg.Annotations == nil {
		pg.Annotations = map[string]string{}
	}
	mutate(pg)
	return ctrl.Result{RequeueAfter: time.Second}, r.Patch(ctx, pg, client.MergeFrom(before))
}

func (r *PostgresReconciler) setConvertStep(ctx context.Context, pg *shpyrdv1.Postgres, step string) (ctrl.Result, error) {
	return r.patchMeta(ctx, pg, func(pg *shpyrdv1.Postgres) { pg.Annotations[annotationConvertStep] = step })
}

// convertFailed ends a conversion that was not done: the stamps go, the
// reason stays on the object until the next conversion, and undo puts
// what the steps made back.
func (r *PostgresReconciler) convertFailed(ctx context.Context, pg *shpyrdv1.Postgres, reason string, undo func(*shpyrdv1.Postgres)) (ctrl.Result, error) {
	r.Recorder.Event(pg, corev1.EventTypeWarning, "ConversionFailed", reason)
	return r.patchMeta(ctx, pg, func(pg *shpyrdv1.Postgres) {
		if undo != nil {
			undo(pg)
		}
		delete(pg.Annotations, AnnotationConvert)
		delete(pg.Annotations, AnnotationConvertOptions)
		delete(pg.Annotations, annotationConvertStep)
		delete(pg.Annotations, annotationConvertJob)
		pg.Annotations[AnnotationConvertFailed] = reason
	})
}

// convertDone ends a conversion that landed.
func (r *PostgresReconciler) convertDone(ctx context.Context, pg *shpyrdv1.Postgres, from, to string, spec func(*shpyrdv1.Postgres)) (ctrl.Result, error) {
	r.Recorder.Eventf(pg, corev1.EventTypeNormal, "Converted", "converted from a %s database to a %s one", from, to)
	return r.patchMeta(ctx, pg, func(pg *shpyrdv1.Postgres) {
		spec(pg)
		delete(pg.Annotations, AnnotationConvert)
		delete(pg.Annotations, AnnotationConvertOptions)
		delete(pg.Annotations, annotationConvertStep)
		delete(pg.Annotations, annotationConvertJob)
		delete(pg.Annotations, AnnotationConvertFailed)
		pg.Annotations[AnnotationConverted] = fmt.Sprintf("from %s to %s at %s", from, to, time.Now().UTC().Format(time.RFC3339))
	})
}

// convertJobState reads the conversion Job: done, failed with its reason,
// or neither yet. The Job's log carries the words of a refusal; the Job
// condition carries the reason Kubernetes saw.
func (r *PostgresReconciler) convertJobState(ctx context.Context, pg *shpyrdv1.Postgres) (done bool, failure string, err error) {
	job := &batchv1.Job{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: convertJobName(pg)}, job); err != nil {
		if apierrors.IsNotFound(err) {
			return false, "the copy's Job is gone before it finished", nil
		}
		return false, "", err
	}
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return false, r.convertJobWords(ctx, job, firstNonEmpty(cond.Message, cond.Reason)), nil
		}
	}
	return job.Status.Succeeded >= 1, "", nil
}

// convertJobWords is the Job's last word: the message its pod ended with,
// when the pod is still there, else the Job's reason.
func (r *PostgresReconciler) convertJobWords(ctx context.Context, job *batchv1.Job, fallback string) string {
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(job.Namespace), client.MatchingLabels{"job-name": job.Name}); err == nil {
		for _, pod := range pods.Items {
			for _, st := range pod.Status.ContainerStatuses {
				if st.Name == "convert" && st.State.Terminated != nil && st.State.Terminated.Message != "" {
					return strings.TrimSpace(st.State.Terminated.Message)
				}
			}
		}
	}
	return fallback
}

// convertEnd is one side of the copy: a server, a database, the Secret
// with the role's username and password.
type convertEnd struct{ host, database, secret string }

// desiredConvertJob copies one database into another: the platform's
// binary in the CloudNativePG image, the source as SOURCE_*, the target
// as PG*, the copy to the shared server's dumps bucket. One try: a second
// pass would find the target half full.
func (r *PostgresReconciler) desiredConvertJob(pg *shpyrdv1.Postgres, hash, server string, source, target convertEnd, key string, allowed []string, cap int64, fenceRole string, pull []corev1.LocalObjectReference) *batchv1.Job {
	from := func(name, secret, key string) corev1.EnvVar {
		return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret}, Key: key}}}
	}
	labels := mergeMaps(sharedTenantLabels(pg, server), map[string]string{sharedServerLabel: "convert"})
	locked := &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}
	tools := corev1.VolumeMount{Name: "tools", MountPath: sharedToolsDir}
	env := []corev1.EnvVar{
		{Name: "SOURCE_HOST", Value: source.host},
		{Name: "SOURCE_PORT", Value: fmt.Sprint(PostgresPort)},
		{Name: "SOURCE_DATABASE", Value: source.database},
		{Name: "SOURCE_SSLMODE", Value: "require"},
		from("SOURCE_USER", source.secret, "username"),
		from("SOURCE_PASSWORD", source.secret, "password"),
		{Name: "PGHOST", Value: target.host},
		{Name: "PGPORT", Value: fmt.Sprint(PostgresPort)},
		{Name: "PGDATABASE", Value: target.database},
		{Name: "PGSSLMODE", Value: "require"},
		from("PGUSER", target.secret, "username"),
		from("PGPASSWORD", target.secret, "password"),
		{Name: "SHPYRD_CONVERT_KEY", Value: key},
		{Name: "SHPYRD_CONVERT_EXTENSIONS", Value: strings.Join(allowed, ",")},
		{Name: "SHPYRD_CONVERT_FENCE_ROLE", Value: fenceRole},
		{Name: "TMPDIR", Value: sharedScratchDir},
	}
	if cap > 0 {
		env = append(env, corev1.EnvVar{Name: "SHPYRD_CONVERT_CAP", Value: fmt.Sprint(cap)})
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: convertJobName(pg), Namespace: SharedPostgresNamespace, Labels: labels},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To[int32](0),
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
						Name:                   "convert",
						Image:                  "ghcr.io/cloudnative-pg/postgresql:" + defaultPostgresVersion,
						Command:                []string{sharedToolsDir + "/shpyrd-server", "convert-db"},
						Env:                    env,
						EnvFrom:                []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: SharedDumpsName(server)}}}},
						VolumeMounts:           []corev1.VolumeMount{tools, scratch},
						TerminationMessagePath: "/dev/termination-log",
						Resources:              corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resourceMustParse("100m"), corev1.ResourceMemory: resourceMustParse("256Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resourceMustParse("1Gi")}},
						SecurityContext:        locked,
					}},
					Volumes: []corev1.Volume{
						{Name: "tools", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: ptr.To(resourceMustParse("512Mi"))}}},
						sharedScratchVolume(),
					},
				},
			},
		},
	}
}

// --- to dedicated -----------------------------------------------------------

func (r *PostgresReconciler) convertToDedicated(ctx context.Context, pg *shpyrdv1.Postgres, step string) (ctrl.Result, error) {
	hash, server := sharedHash(pg), pg.Annotations[AnnotationSharedServer]
	if server == "" {
		return ctrl.Result{}, &pendingError{msg: "the database is not placed on a shared server yet"}
	}
	switch step {
	case "":
		// The source closes to its role; the spec becomes a dedicated one,
		// its cap remembered for a way back.
		if err := r.fenceSharedRole(ctx, pg, hash, server); err != nil {
			return ctrl.Result{}, err
		}
		opts := convertOptions(pg)
		if pg.Spec.Storage != nil {
			opts.Cap = pg.Spec.Storage.String()
		}
		storage := resource.MustParse(defaultPostgresStorage)
		if opts.Storage != "" {
			if q, err := resource.ParseQuantity(opts.Storage); err == nil && q.Sign() > 0 {
				storage = q
			}
		}
		return r.patchMeta(ctx, pg, func(pg *shpyrdv1.Postgres) {
			pg.Spec.Type = shpyrdv1.PostgresTypeDedicated
			pg.Spec.Storage = &storage
			pg.Spec.Size = opts.Size
			raw, _ := json.Marshal(opts)
			pg.Annotations[AnnotationConvertOptions] = string(raw)
			pg.Annotations[annotationConvertStep] = convertStepTarget
		})
	case convertStepTarget:
		// The cluster of its own, with the shared credentials as its own.
		if _, err := r.reconcileDedicated(ctx, pg); err != nil {
			return ctrl.Result{}, err
		}
		cluster := &unstructured.Unstructured{}
		cluster.SetGroupVersionKind(CNPGClusterGVK)
		if err := r.Get(ctx, types.NamespacedName{Namespace: pg.Namespace, Name: pg.Name}, cluster); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		if ready, _, _ := unstructured.NestedInt64(cluster.Object, "status", "readyInstances"); ready < 1 {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		// The platform's extensions, which the owner cannot create and the
		// copy's tables may need, before the copy comes.
		applied, err := r.ensureConvertedExtensions(ctx, pg, hash)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !applied {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		if convertJobName(pg) == "" {
			return r.nameConvertJob(ctx, pg, hash)
		}
		pull, err := r.ensureSharedPullSecret(ctx)
		if err != nil {
			return ctrl.Result{}, err
		}
		job := r.desiredConvertJob(pg, hash, server,
			convertEnd{host: server + "-rw", database: sharedDatabaseName(hash), secret: sharedDumpRoleSecret(server)},
			convertEnd{host: fmt.Sprintf("%s-rw.%s.svc.cluster.local", pg.Name, pg.Namespace), database: sharedDatabaseName(hash), secret: sharedRoleObject(hash)},
			pgdump.ConvertKey(sharedDatabaseName(hash), time.Now()), nil, 0, "", pull)
		if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
		return r.setConvertStep(ctx, pg, convertStepLoad)
	case convertStepLoad:
		done, failure, err := r.convertJobState(ctx, pg)
		if err != nil {
			return ctrl.Result{}, err
		}
		if failure != "" {
			// Back as it was: the cluster goes, the role opens, the spec is shared again.
			if err := r.deleteDedicatedCluster(ctx, pg); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.liftSharedFence(ctx, pg, hash); err != nil {
				return ctrl.Result{}, err
			}
			opts := convertOptions(pg)
			return r.convertFailed(ctx, pg, "The conversion to a dedicated database was not done: "+failure+". Your database is as it was.", func(pg *shpyrdv1.Postgres) {
				pg.Spec.Type = shpyrdv1.PostgresTypeShared
				pg.Spec.Size = ""
				if q, err := resource.ParseQuantity(opts.Cap); err == nil {
					pg.Spec.Storage = &q
				}
			})
		}
		if !done {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		return r.setConvertStep(ctx, pg, convertStepSwap)
	case convertStepSwap:
		// The credentials turn to the cluster: the project's Services go
		// and the Secret names the cluster's own; the shared database stays
		// closed behind its drop date.
		for _, name := range []string{pg.Name, sharedDirectService(pg)} {
			if err := r.Delete(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pg.Namespace}}); client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, err
			}
		}
		if err := r.retargetSharedAppSecret(ctx, pg, pg.Name+"-rw"); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.stampSharedDrop(ctx, pg, hash); err != nil {
			return ctrl.Result{}, err
		}
		return r.convertDone(ctx, pg, shpyrdv1.PostgresTypeShared, shpyrdv1.PostgresTypeDedicated, func(pg *shpyrdv1.Postgres) {
			controllerutil.RemoveFinalizer(pg, sharedFinalizer)
			delete(pg.Annotations, AnnotationSharedServer)
		})
	}
	return ctrl.Result{}, fmt.Errorf("conversion step %q is not one the controller knows", step)
}

// ensureConvertedExtensions gives a cluster converted from a shared
// database the extensions every database has, by the Database object every
// dedicated cluster carries (kept: it reclaims nothing), and says whether
// the operator applied it.
func (r *PostgresReconciler) ensureConvertedExtensions(ctx context.Context, pg *shpyrdv1.Postgres, hash string) (bool, error) {
	return r.ensureDatabaseObject(ctx, pg, sharedDatabaseName(hash), sharedRoleName(hash))
}

// retargetSharedAppSecret rewrites the hosts in a shared database's
// credentials to a cluster's own Service.
func (r *PostgresReconciler) retargetSharedAppSecret(ctx context.Context, pg *shpyrdv1.Postgres, host string) error {
	sec := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: pg.Namespace, Name: PostgresSecretName(pg.Name)}, sec); err != nil {
		return err
	}
	before := sec.DeepCopy()
	for _, key := range []string{"host", "direct_host"} {
		if _, ok := sec.Data[key]; ok {
			sec.Data[key] = []byte(host)
		}
	}
	for key, from := range map[string]string{"uri": "@" + pg.Name + ":", "direct_uri": "@" + sharedDirectService(pg) + ":"} {
		if v, ok := sec.Data[key]; ok {
			sec.Data[key] = []byte(strings.Replace(string(v), from, "@"+host+":", 1))
		}
	}
	return r.Patch(ctx, sec, client.MergeFrom(before))
}

// stampSharedDrop leaves a converted database's shared side as a deletion
// does: the role closed, the Database kept behind a drop date for the
// sweep.
func (r *PostgresReconciler) stampSharedDrop(ctx context.Context, pg *shpyrdv1.Postgres, hash string) error {
	role := &unstructured.Unstructured{}
	role.SetGroupVersionKind(CNPGDatabaseRoleGVK)
	if err := r.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: sharedRoleObject(hash)}, role); err == nil {
		before := role.DeepCopy()
		_ = unstructured.SetNestedField(role.Object, false, "spec", "login")
		_ = unstructured.SetNestedField(role.Object, int64(0), "spec", "connectionLimit")
		if err := r.Patch(ctx, role, client.MergeFrom(before)); err != nil {
			return err
		}
	} else if client.IgnoreNotFound(err) != nil {
		return err
	}
	db := &unstructured.Unstructured{}
	db.SetGroupVersionKind(CNPGDatabaseGVK)
	if err := r.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: sharedDBObject(hash)}, db); err != nil {
		return client.IgnoreNotFound(err)
	}
	before := db.DeepCopy()
	ann := db.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	if ann[AnnotationDropAfter] == "" {
		ann[AnnotationDropAfter] = time.Now().UTC().Add(sharedKeep).Format(time.RFC3339)
	}
	db.SetAnnotations(ann)
	_ = unstructured.SetNestedField(db.Object, "retain", "spec", "databaseReclaimPolicy")
	return r.Patch(ctx, db, client.MergeFrom(before))
}

// deleteDedicatedCluster removes the database's own cluster and reports
// nothing when it is already gone.
func (r *PostgresReconciler) deleteDedicatedCluster(ctx context.Context, pg *shpyrdv1.Postgres) error {
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(CNPGClusterGVK)
	cluster.SetNamespace(pg.Namespace)
	cluster.SetName(pg.Name)
	return client.IgnoreNotFound(r.Delete(ctx, cluster))
}

// --- to shared --------------------------------------------------------------

func (r *PostgresReconciler) convertToShared(ctx context.Context, pg *shpyrdv1.Postgres, step string) (ctrl.Result, error) {
	hash := sharedHash(pg)
	switch step {
	case "":
		// A shared side of this database kept behind a drop date (it was
		// converted to dedicated less than seven days ago) keeps its hash:
		// the new tenant takes another, or it would be rendered over it.
		if kept, err := r.sharedSideKept(ctx, hash); err != nil {
			return ctrl.Result{}, err
		} else if kept {
			next := tenantHash(types.UID(string(pg.UID) + "/" + strconv.FormatInt(time.Now().Unix(), 36)))
			return r.patchMeta(ctx, pg, func(pg *shpyrdv1.Postgres) { pg.Annotations[AnnotationSharedTenant] = next })
		}
		// The cluster opens superuser access for the operation while the
		// tenant comes up on a shared server.
		if _, err := r.reconcileDedicated(ctx, pg); err != nil {
			return ctrl.Result{}, err
		}
		tenant, err := r.ensureSharedTenant(ctx, pg)
		if err != nil {
			return ctrl.Result{}, err
		}
		if tenant.message != "" {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		server := tenant.server.GetName()
		superuser := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: pg.Namespace, Name: pg.Name + "-superuser"}, superuser); err != nil {
			if apierrors.IsNotFound(err) {
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			return ctrl.Result{}, err
		}
		app := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: pg.Namespace, Name: PostgresSecretName(pg.Name)}, app); err != nil {
			return ctrl.Result{}, err
		}
		// The superuser's credentials, where the Job can read them.
		copied := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: sharedConvertSecret(hash), Namespace: SharedPostgresNamespace}}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, copied, func() error {
			copied.Labels = mergeMaps(copied.Labels, mergeMaps(sharedTenantLabels(pg, server), map[string]string{sharedServerLabel: "convert"}))
			copied.Data = map[string][]byte{"username": superuser.Data["username"], "password": superuser.Data["password"]}
			return nil
		}); err != nil {
			return ctrl.Result{}, err
		}
		if convertJobName(pg) == "" {
			return r.nameConvertJob(ctx, pg, hash)
		}
		pull, err := r.ensureSharedPullSecret(ctx)
		if err != nil {
			return ctrl.Result{}, err
		}
		var cap int64
		if q, err := resource.ParseQuantity(convertOptions(pg).Cap); err == nil {
			cap = q.Value()
		}
		job := r.desiredConvertJob(pg, hash, server,
			convertEnd{host: fmt.Sprintf("%s-rw.%s.svc.cluster.local", pg.Name, pg.Namespace), database: firstNonEmpty(string(app.Data["dbname"]), PostgresDatabase), secret: sharedConvertSecret(hash)},
			convertEnd{host: server + "-rw", database: sharedDatabaseName(hash), secret: sharedRoleObject(hash)},
			pgdump.ConvertKey(sharedDatabaseName(hash), time.Now()), sharedAllowedExtensions, cap, firstNonEmpty(string(app.Data["username"]), PostgresUser), pull)
		if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
		return r.setConvertStep(ctx, pg, convertStepLoad)
	case convertStepLoad:
		done, failure, err := r.convertJobState(ctx, pg)
		if err != nil {
			return ctrl.Result{}, err
		}
		if failure != "" {
			// Back as it was: the tenant goes (the Job lifted the owner's
			// pause itself), the cluster closes superuser access again.
			if err := r.dropSharedTenant(ctx, pg, hash); err != nil {
				return ctrl.Result{}, err
			}
			return r.convertFailed(ctx, pg, "The conversion to a shared database was not done: "+failure+". Your database is as it was.", func(pg *shpyrdv1.Postgres) {
				controllerutil.RemoveFinalizer(pg, sharedFinalizer)
				delete(pg.Annotations, AnnotationSharedServer)
				delete(pg.Annotations, AnnotationSharedTenant)
			})
		}
		if !done {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		return r.setConvertStep(ctx, pg, convertStepSwap)
	case convertStepSwap:
		// The cluster goes, its credentials and backups with it; once they
		// are gone the spec is a shared one and the shared credentials are
		// rendered in their place.
		cluster := &unstructured.Unstructured{}
		cluster.SetGroupVersionKind(CNPGClusterGVK)
		err := r.Get(ctx, types.NamespacedName{Namespace: pg.Namespace, Name: pg.Name}, cluster)
		if err == nil {
			if err := r.Delete(ctx, cluster); client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		app := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: pg.Namespace, Name: PostgresSecretName(pg.Name)}, app); err == nil {
			if err := r.Delete(ctx, app); client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		} else if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		if err := r.dropDedicatedBackups(ctx, pg); err != nil {
			return ctrl.Result{}, err
		}
		// The extensions object a conversion from shared had put on the
		// cluster, if this database came that way; it reclaims nothing.
		ext := &unstructured.Unstructured{}
		ext.SetGroupVersionKind(CNPGDatabaseGVK)
		ext.SetNamespace(pg.Namespace)
		ext.SetName(pg.Name + "-db")
		if err := r.Delete(ctx, ext); err != nil && !apierrors.IsNotFound(err) && !isNoKind(err) {
			return ctrl.Result{}, err
		}
		if err := r.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: sharedConvertSecret(hash), Namespace: SharedPostgresNamespace}}); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		opts := convertOptions(pg)
		return r.convertDone(ctx, pg, shpyrdv1.PostgresTypeDedicated, shpyrdv1.PostgresTypeShared, func(pg *shpyrdv1.Postgres) {
			pg.Spec.Type = shpyrdv1.PostgresTypeShared
			pg.Spec.Instances, pg.Spec.Size, pg.Spec.Version, pg.Spec.Sleep, pg.Spec.Recovery, pg.Spec.Backups = nil, "", "", nil, nil, nil
			pg.Spec.Storage = nil
			if q, err := resource.ParseQuantity(opts.Cap); err == nil && q.Sign() > 0 {
				pg.Spec.Storage = &q
			}
		})
	}
	return ctrl.Result{}, fmt.Errorf("conversion step %q is not one the controller knows", step)
}

// sharedSideKept says whether a Database object of this hash is kept
// behind a drop date: the shared side of a database converted to
// dedicated, waiting for the sweep.
func (r *PostgresReconciler) sharedSideKept(ctx context.Context, hash string) (bool, error) {
	db := &unstructured.Unstructured{}
	db.SetGroupVersionKind(CNPGDatabaseGVK)
	if err := r.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: sharedDBObject(hash)}, db); err != nil {
		return false, client.IgnoreNotFound(err)
	}
	return db.GetAnnotations()[AnnotationDropAfter] != "", nil
}

// dropSharedTenant removes a tenant made for a conversion that was not
// done: its database (dropped, nothing of the source is in it), its role,
// their Secret and its copy count.
func (r *PostgresReconciler) dropSharedTenant(ctx context.Context, pg *shpyrdv1.Postgres, hash string) error {
	db := &unstructured.Unstructured{}
	db.SetGroupVersionKind(CNPGDatabaseGVK)
	if err := r.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: sharedDBObject(hash)}, db); err == nil {
		before := db.DeepCopy()
		_ = unstructured.SetNestedField(db.Object, "delete", "spec", "databaseReclaimPolicy")
		if err := r.Patch(ctx, db, client.MergeFrom(before)); err != nil {
			return err
		}
		if err := r.Delete(ctx, db); client.IgnoreNotFound(err) != nil {
			return err
		}
	} else if client.IgnoreNotFound(err) != nil {
		return err
	}
	role := &unstructured.Unstructured{}
	role.SetGroupVersionKind(CNPGDatabaseRoleGVK)
	role.SetNamespace(SharedPostgresNamespace)
	role.SetName(sharedRoleObject(hash))
	if err := r.Delete(ctx, role); client.IgnoreNotFound(err) != nil {
		return err
	}
	for _, name := range []string{sharedRoleObject(hash), sharedConvertSecret(hash)} {
		if err := r.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: SharedPostgresNamespace}}); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	if server := pg.Annotations[AnnotationSharedServer]; server != "" {
		keep := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: sharedDumpsKeepMap(server), Namespace: SharedPostgresNamespace}}
		patch := []byte(fmt.Sprintf(`{"data":{%q:null}}`, sharedDatabaseName(hash)))
		if err := r.Patch(ctx, keep, client.RawPatch(types.MergePatchType, patch)); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// dropDedicatedBackups removes what a dedicated database's backups left
// once the database is shared: its schedule, its store and its bucket.
func (r *PostgresReconciler) dropDedicatedBackups(ctx context.Context, pg *shpyrdv1.Postgres) error {
	schedule := &unstructured.Unstructured{}
	schedule.SetGroupVersionKind(CNPGScheduledBackupGVK)
	schedule.SetNamespace(pg.Namespace)
	schedule.SetName(pg.Name + "-scheduled")
	store := &unstructured.Unstructured{}
	store.SetGroupVersionKind(BarmanObjectStoreGVK)
	store.SetNamespace(pg.Namespace)
	store.SetName(BackupsBucketName(pg.Name))
	for _, obj := range []client.Object{schedule, store, &shpyrdv1.ObjectBucket{ObjectMeta: metav1.ObjectMeta{Name: BackupsBucketName(pg.Name), Namespace: pg.Namespace}}} {
		if err := r.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) && !isNoKind(err) {
			return err
		}
	}
	return nil
}
