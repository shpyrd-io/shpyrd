package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// A shared server's own backups (storage plan, step 3): Barman WAL
// archiving and a daily base backup of the whole server into a platform
// bucket, kept seven days, for whole-server recovery by an operator. A
// tenant's own restore is the nightly dump; this is for the day a server
// is lost with everyone on it. Rendered with the server when the platform
// offers object storage; a platform without it gets servers without them
// and a line in the log.
const (
	sharedServerBackupRetention = "7d"
	sharedServerBackupSchedule  = "0 2 * * *"
)

func sharedServerBackupsName(server string) string { return server + "-backups" }

// sharedServerBackupPlugin is the Cluster's plugins entry archiving into
// the server's ObjectStore.
func sharedServerBackupPlugin(server string) map[string]interface{} {
	return map[string]interface{}{
		"name":          BarmanPluginName,
		"isWALArchiver": true,
		"enabled":       true,
		"parameters":    map[string]interface{}{"barmanObjectName": sharedServerBackupsName(server)},
	}
}

// ensureSharedServerBackups makes the server's bucket, the plugin's
// ObjectStore and the daily schedule exist, and puts the plugin on a
// server rendered without it. It reports whether the server may carry the
// plugin now (the store exists), so a new server is rendered with it from
// the start when it can be.
func (r *PostgresReconciler) ensureSharedServerBackups(ctx context.Context, server string) (bool, error) {
	bucket := &shpyrdv1.ObjectBucket{ObjectMeta: metav1.ObjectMeta{Name: sharedServerBackupsName(server), Namespace: SharedPostgresNamespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, bucket, func() error {
		bucket.Labels = mergeMaps(bucket.Labels, sharedServerLabels(server, "backups"))
		bucket.Spec.SecretName = sharedServerBackupsName(server)
		bucket.Spec.DeletionPolicy = "Retain"
		return nil
	}); err != nil {
		if isNoKind(err) {
			log.FromContext(ctx).Info("no backups of the shared server: the platform offers no object storage", "server", server)
			return false, nil
		}
		return false, fmt.Errorf("server backups bucket: %w", err)
	}
	if bucket.Status.Phase != shpyrdv1.BucketReady {
		return false, nil // the next reconcile, once the bucket answers
	}
	store := &unstructured.Unstructured{}
	store.SetGroupVersionKind(BarmanObjectStoreGVK)
	store.SetName(sharedServerBackupsName(server))
	store.SetNamespace(SharedPostgresNamespace)
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, store, func() error {
		store.SetLabels(mergeMaps(store.GetLabels(), sharedServerLabels(server, "backups")))
		secret := bucket.CredentialSecretName()
		store.Object["spec"] = map[string]interface{}{
			"retentionPolicy":              sharedServerBackupRetention,
			"instanceSidecarConfiguration": map[string]interface{}{"resources": BarmanSidecarResources},
			"configuration": map[string]interface{}{
				"destinationPath": "s3://" + bucket.Status.Bucket + "/",
				"endpointURL":     bucket.Status.Endpoint,
				"s3Credentials": map[string]interface{}{
					"accessKeyId":     map[string]interface{}{"name": secret, "key": "accessKeyId"},
					"secretAccessKey": map[string]interface{}{"name": secret, "key": "secretAccessKey"},
					"region":          map[string]interface{}{"name": secret, "key": "AWS_REGION"},
				},
				"wal":  map[string]interface{}{"compression": "gzip"},
				"data": map[string]interface{}{"compression": "gzip"},
			},
		}
		return nil
	}); err != nil {
		if isNoKind(err) {
			log.FromContext(ctx).Info("no backups of the shared server: the Barman Cloud plugin is not installed", "server", server)
			return false, nil
		}
		return false, fmt.Errorf("server backups object store: %w", err)
	}
	schedule := &unstructured.Unstructured{}
	schedule.SetGroupVersionKind(CNPGScheduledBackupGVK)
	schedule.SetName(server + "-scheduled")
	schedule.SetNamespace(SharedPostgresNamespace)
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, schedule, func() error {
		schedule.SetLabels(mergeMaps(schedule.GetLabels(), sharedServerLabels(server, "backups")))
		schedule.Object["spec"] = map[string]interface{}{
			"schedule":             sharedServerBackupSchedule,
			"immediate":            true,
			"backupOwnerReference": "self",
			"cluster":              map[string]interface{}{"name": server},
			"method":               "plugin",
			"pluginConfiguration":  map[string]interface{}{"name": BarmanPluginName},
		}
		return nil
	}); err != nil {
		return false, fmt.Errorf("server backup schedule: %w", err)
	}
	return true, nil
}

// ensureSharedServerPlugin puts the backup plugin on a server rendered
// before its store existed, once the server is up. Put on a server whose
// instances are still coming, the plugin never takes: the instance manager
// loads the Cluster's plugins before it starts PostgreSQL, finds no sidecar
// for this one in a pod rendered without it, and retries for good, while
// the operator waits for the instance to become active before it would
// roll the pod (CI, 2026-10-28: a bucket answering a reconcile after the
// server's). Up, the server gets the sidecar through the operator's
// rolling restart.
func (r *PostgresReconciler) ensureSharedServerPlugin(ctx context.Context, server *unstructured.Unstructured) error {
	plugins, _, _ := unstructured.NestedSlice(server.Object, "spec", "plugins")
	for _, p := range plugins {
		if m, ok := p.(map[string]interface{}); ok && m["name"] == BarmanPluginName {
			return nil
		}
	}
	if !sharedServerUp(server) {
		return nil // the next reconcile, once the instances are up
	}
	before := server.DeepCopy()
	plugins = append(plugins, sharedServerBackupPlugin(server.GetName()))
	if err := unstructured.SetNestedSlice(server.Object, plugins, "spec", "plugins"); err != nil {
		return err
	}
	if err := r.Patch(ctx, server, client.MergeFrom(before)); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// sharedServerUp is whether every instance of a shared server is ready.
func sharedServerUp(server *unstructured.Unstructured) bool {
	ready, _, _ := unstructured.NestedInt64(server.Object, "status", "readyInstances")
	want, _, _ := unstructured.NestedInt64(server.Object, "spec", "instances")
	return ready >= 1 && ready >= want
}

var _ = types.NamespacedName{}
