package install

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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// The control-plane database on CloudNativePG (storage plan, after step 7).
// With the postgres extension on the cluster the platform's own PostgreSQL
// is a CloudNativePG Cluster instead of the single StatefulSet: two
// instances with a synchronous replica (preferred: synchronous while the
// replica is there, asynchronous rather than blocked when it is not),
// failover in about thirty seconds, Barman backups to the gateway. A
// platform that already runs the StatefulSet moves its data once, at the
// server's turn of `cluster init`: the API pauses for the seconds the copy
// takes, the old claim stays behind at zero replicas until removed by
// hand. Without the extension, or with SHPYRD_DATABASE_URL, nothing of
// this applies.

const (
	// ControlPlaneCluster is the CloudNativePG Cluster; apps reach it at
	// <name>-rw.
	ControlPlaneCluster = "control-plane-pg"
	// ControlPlaneOwnerSecret is the basic-auth Secret the cluster's initdb
	// creates the shpyrd role from: the same password as the StatefulSet's,
	// so the move changes only the host.
	ControlPlaneOwnerSecret = "control-plane-pg-owner"
	// ControlPlaneBackupsBucket and ControlPlaneBackupsSecret are the
	// gateway bucket its backups go to and the credential the gateway hook
	// makes; ControlPlaneBackupStore the Barman ObjectStore naming them.
	ControlPlaneBackupsBucket = "platform-control-plane"
	ControlPlaneBackupsSecret = "gateway-platform-control-plane"
	ControlPlaneBackupStore   = "control-plane-pg-backups"
	// VarControlPlaneDBInstances is how many instances the cluster runs
	// (2 on the cloud profiles, 1 on the local one).
	VarControlPlaneDBInstances = "SHPYRD_CONTROL_PLANE_DB_INSTANCES"
	// controlPlaneMovedKey, in Secret control-plane-db, names the cluster
	// the data lives in once it left the StatefulSet (or never was there).
	controlPlaneMovedKey = "moved"
	// controlPlaneMoveJob copies the data, once.
	controlPlaneMoveJob = "control-plane-db-move"
	// controlPlaneStatefulSet is the single instance of before.
	controlPlaneStatefulSet = "control-plane-db"
	// barmanPluginName is the CloudNativePG Barman plugin (the controller
	// names it too; the installer cannot import the controller).
	barmanPluginName = "barman-cloud.cloudnative-pg.io"
)

// barmanSidecarResources is what the Barman sidecar asks for, as the
// controller gives the workspaces' databases.
var barmanSidecarResources = map[string]interface{}{
	"requests": map[string]interface{}{"cpu": "10m", "memory": "64Mi"},
	"limits":   map[string]interface{}{"cpu": "500m", "memory": "512Mi"},
}

// waitJobFn waits for a Job; tests replace it.
var waitJobFn = waitJob

// controlPlaneOnCNPG says the control plane runs on CloudNativePG: the
// postgres extension is on and no managed database is named.
func controlPlaneOnCNPG(vars map[string]string) bool {
	if vars[VarDatabaseURL] != "" {
		return false
	}
	for _, x := range strings.Split(vars[VarExtensions], ",") {
		if strings.TrimSpace(x) == "postgres" {
			return true
		}
	}
	return false
}

// controlPlaneInstances is the cluster's instance count, two by default.
func controlPlaneInstances(vars map[string]string) int64 {
	if n, err := strconv.Atoi(strings.TrimSpace(vars[VarControlPlaneDBInstances])); err == nil && n > 0 {
		return int64(n)
	}
	return 2
}

// controlPlaneURL is the control plane's connection string on the cluster.
func controlPlaneURL(password, namespace string) string {
	return "postgres://shpyrd:" + password + "@" + ControlPlaneCluster + "-rw." + namespace + ".svc:5432/shpyrd?sslmode=require"
}

// controlPlaneMoved says the data is on the cluster, by the Secret's word.
func controlPlaneMoved(data map[string][]byte) bool {
	return string(data[controlPlaneMovedKey]) == ControlPlaneCluster
}

// controlPlaneObjects renders the cluster and what goes with it: its
// network policy, and, with the object gateway, its backup store and
// schedule.
func controlPlaneObjects(vars map[string]string, namespace string) []*unstructured.Unstructured {
	get := func(k string) string { return vars[k] }
	instances := controlPlaneInstances(vars)
	gateway := vars[VarGatewayBucket] != ""
	size := firstNonEmptyString(vars[VarControlPlaneDBSize], "2Gi")
	storage := map[string]interface{}{"size": size}
	if class := DatabaseStorageClass(get); class != "" {
		storage["storageClass"] = class
	}
	spec := map[string]interface{}{
		"instances": instances,
		"imageName": "ghcr.io/cloudnative-pg/postgresql:17",
		"storage":   storage,
		"resources": map[string]interface{}{
			"requests": map[string]interface{}{"cpu": "100m", "memory": "256Mi"},
			"limits":   map[string]interface{}{"memory": "1Gi"},
		},
		"bootstrap": map[string]interface{}{"initdb": map[string]interface{}{
			"database": "shpyrd", "owner": "shpyrd", "secret": map[string]interface{}{"name": ControlPlaneOwnerSecret},
		}},
		"enableSuperuserAccess": false,
		"enablePDB":             instances > 1,
		// A graceful stop of the primary (a drain, a pod deletion) waits
		// for sessions to end before the failover: PostgreSQL's smart
		// shutdown. The server's pool holds idle sessions open, so the
		// default 180 s is three minutes without a control plane (seen on
		// dev); ten seconds, then the fast shutdown and the promotion.
		"smartShutdownTimeout": int64(10),
	}
	postgresql := map[string]interface{}{"parameters": map[string]interface{}{"max_connections": "100"}}
	if instances > 1 {
		// Synchronous while the replica is there; availability over
		// durability when it is not (a control plane that refuses every
		// write because its replica is down helps nobody).
		postgresql["synchronous"] = map[string]interface{}{"method": "any", "number": int64(1), "dataDurability": "preferred"}
	}
	spec["postgresql"] = postgresql
	affinity := map[string]interface{}{}
	// The platform pool, where the StatefulSet ran: the data pool is the
	// projects' databases' and runs close to its CPU.
	if pool := vars[VarPlatformPool]; pool != "" {
		affinity["nodeSelector"] = map[string]interface{}{PoolLabel: pool}
	}
	if instances > 1 {
		affinity["enablePodAntiAffinity"] = true
		affinity["topologyKey"] = corev1.LabelHostname
		affinity["podAntiAffinityType"] = "required"
	}
	if len(affinity) > 0 {
		spec["affinity"] = affinity
	}
	if gateway {
		spec["plugins"] = []interface{}{map[string]interface{}{
			"name": barmanPluginName, "isWALArchiver": true, "enabled": true,
			"parameters": map[string]interface{}{"barmanObjectName": ControlPlaneBackupStore},
		}}
	}
	cluster := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "postgresql.cnpg.io/v1", "kind": "Cluster",
		"metadata": map[string]interface{}{"name": ControlPlaneCluster, "namespace": namespace, "labels": map[string]interface{}{"app.kubernetes.io/name": ControlPlaneCluster, "app.kubernetes.io/part-of": "shpyrd"}},
		"spec":     spec,
	}}
	clients := map[string]interface{}{"podSelector": map[string]interface{}{"matchLabels": map[string]interface{}{"app.kubernetes.io/part-of": "shpyrd", "shpyrd.io/control-plane-client": "true"}}}
	peers := map[string]interface{}{"podSelector": map[string]interface{}{"matchLabels": map[string]interface{}{"cnpg.io/cluster": ControlPlaneCluster}}}
	operator := map[string]interface{}{"namespaceSelector": map[string]interface{}{"matchLabels": map[string]interface{}{"kubernetes.io/metadata.name": "cnpg-system"}}}
	monitoring := map[string]interface{}{"namespaceSelector": map[string]interface{}{"matchLabels": map[string]interface{}{"kubernetes.io/metadata.name": "monitoring"}}}
	policy := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy",
		"metadata": map[string]interface{}{"name": ControlPlaneCluster, "namespace": namespace},
		"spec": map[string]interface{}{
			"podSelector": map[string]interface{}{"matchLabels": map[string]interface{}{"cnpg.io/cluster": ControlPlaneCluster}},
			"policyTypes": []interface{}{"Ingress"},
			"ingress": []interface{}{
				// The server, its backup job and the move: the database port.
				map[string]interface{}{"from": []interface{}{clients, peers}, "ports": []interface{}{map[string]interface{}{"port": int64(5432)}}},
				// The operator's instance manager port and Prometheus's.
				map[string]interface{}{"from": []interface{}{operator, peers}, "ports": []interface{}{map[string]interface{}{"port": int64(8000)}}},
				map[string]interface{}{"from": []interface{}{monitoring}, "ports": []interface{}{map[string]interface{}{"port": int64(9187)}}},
			},
		},
	}}
	// The PodMonitor by hand: CloudNativePG 1.30 deprecates
	// spec.monitoring.enablePodMonitor.
	monitor := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "monitoring.coreos.com/v1", "kind": "PodMonitor",
		"metadata": map[string]interface{}{"name": ControlPlaneCluster, "namespace": namespace},
		"spec": map[string]interface{}{
			"selector":            map[string]interface{}{"matchLabels": map[string]interface{}{"cnpg.io/cluster": ControlPlaneCluster}},
			"podMetricsEndpoints": []interface{}{map[string]interface{}{"port": "metrics"}},
		},
	}}
	objs := []*unstructured.Unstructured{cluster, policy, monitor}
	if gateway {
		store := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "barmancloud.cnpg.io/v1", "kind": "ObjectStore",
			"metadata": map[string]interface{}{"name": ControlPlaneBackupStore, "namespace": namespace},
			"spec": map[string]interface{}{
				"retentionPolicy":              "14d",
				"instanceSidecarConfiguration": map[string]interface{}{"resources": barmanSidecarResources},
				"configuration": map[string]interface{}{
					"destinationPath": "s3://" + ControlPlaneBackupsBucket + "/",
					"endpointURL":     gatewayEndpoint(namespace),
					"s3Credentials": map[string]interface{}{
						"accessKeyId":     map[string]interface{}{"name": ControlPlaneBackupsSecret, "key": "AWS_ACCESS_KEY_ID"},
						"secretAccessKey": map[string]interface{}{"name": ControlPlaneBackupsSecret, "key": "AWS_SECRET_ACCESS_KEY"},
						"region":          map[string]interface{}{"name": ControlPlaneBackupsSecret, "key": "region"},
					},
					"wal":  map[string]interface{}{"compression": "gzip"},
					"data": map[string]interface{}{"compression": "gzip"},
				},
			},
		}}
		schedule := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "postgresql.cnpg.io/v1", "kind": "ScheduledBackup",
			"metadata": map[string]interface{}{"name": ControlPlaneCluster + "-scheduled", "namespace": namespace},
			"spec": map[string]interface{}{
				"schedule": "0 30 2 * * *", "immediate": true, "backupOwnerReference": "self",
				"cluster": map[string]interface{}{"name": ControlPlaneCluster},
				"method":  "plugin", "pluginConfiguration": map[string]interface{}{"name": barmanPluginName},
			},
		}}
		objs = append(objs, store, schedule)
	}
	return objs
}

// controlPlaneWaits are the component's declared waits on CloudNativePG,
// after the primary's: the StatefulSet's while the data is still on it.
// The cluster's Ready condition is not among them: it waits for every
// replica, and a replica still joining (minutes on a fresh block volume)
// holds nothing the install needs.
func controlPlaneWaits(namespace string, moved bool) []WaitSpec {
	if moved {
		return nil
	}
	return []WaitSpec{{StatefulSet: namespace + "/" + controlPlaneStatefulSet}}
}

// waitControlPlanePrimary waits for the cluster to have a ready instance.
func (e *Engine) waitControlPlanePrimary(ctx context.Context, namespace string) error {
	return poll(ctx, "cluster "+namespace+"/"+ControlPlaneCluster+" primary", func(ctx context.Context) (bool, error) {
		return e.controlPlaneClusterReady(ctx, namespace), nil
	})
}

// controlPlaneHookCNPG is the credentials hook on CloudNativePG: the owner
// Secret for initdb, the connection string (the StatefulSet's until the
// move, the cluster's after), and the move itself at the server's turn.
func (e *Engine) controlPlaneHookCNPG(ctx context.Context, c *Component, existing *corev1.Secret, found bool) error {
	password := ""
	if found {
		password = string(existing.Data["password"])
	}
	if password == "" {
		password = randomHex(24)
	}
	if err := e.applySecret(ctx, c.Namespace, ControlPlaneOwnerSecret, string(corev1.SecretTypeBasicAuth), map[string]string{"username": "shpyrd", "password": password}); err != nil {
		return err
	}
	newURL := controlPlaneURL(password, c.Namespace)
	onCluster := map[string]string{"password": password, "url": newURL, controlPlaneMovedKey: ControlPlaneCluster}
	if found && controlPlaneMoved(existing.Data) {
		if string(existing.Data["url"]) != newURL {
			return e.applyOpaqueSecret(ctx, c.Namespace, ControlPlaneDBSecretName, onCluster)
		}
		e.rep.Step(c.Name, "control-plane database: the CloudNativePG cluster "+ControlPlaneCluster)
		return nil
	}
	old, err := e.kube.Kube.AppsV1().StatefulSets(c.Namespace).Get(ctx, controlPlaneStatefulSet, metav1.GetOptions{})
	hasOld := err == nil && old.Status.ReadyReplicas > 0
	if !found || !hasOld {
		// A fresh platform, or one whose single instance never held data:
		// the cluster from the start.
		if err := e.applyOpaqueSecret(ctx, c.Namespace, ControlPlaneDBSecretName, onCluster); err != nil {
			return err
		}
		e.rep.Step(c.Name, "control-plane database on CloudNativePG: cluster "+ControlPlaneCluster+" (Secret "+ControlPlaneDBSecretName+")")
		return nil
	}
	if c.Name != ServerComponent {
		e.rep.Step(c.Name, "control-plane database: on its own disk until the move at the server's turn")
		return nil
	}
	if !e.controlPlaneClusterReady(ctx, c.Namespace) {
		e.rep.Step(c.Name, "control-plane database: the cluster "+ControlPlaneCluster+" is not ready yet; the move waits for the next cluster init")
		return nil
	}
	oldURL := string(existing.Data["url"])
	e.rep.Step(c.Name, "moving the control-plane database to "+ControlPlaneCluster+" (the API pauses meanwhile)")
	if err := e.moveControlPlane(ctx, c.Namespace, oldURL, newURL); err != nil {
		return fmt.Errorf("move the control-plane database: %w", err)
	}
	if err := e.applyOpaqueSecret(ctx, c.Namespace, ControlPlaneDBSecretName, onCluster); err != nil {
		return err
	}
	if err := scaleStatefulSet(ctx, e.kube, c.Namespace, controlPlaneStatefulSet, 0); err != nil {
		return err
	}
	e.rep.Step(c.Name, "control-plane database moved to "+ControlPlaneCluster+"; the old instance is stopped, its claim data-"+controlPlaneStatefulSet+"-0 kept (remove it after a week)")
	return nil
}

// controlPlaneClusterReady reads the cluster's Ready condition.
func (e *Engine) controlPlaneClusterReady(ctx context.Context, namespace string) bool {
	if e.kube.Dynamic == nil {
		return false
	}
	gvr := schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"}
	obj, err := e.kube.Dynamic.Resource(gvr).Namespace(namespace).Get(ctx, ControlPlaneCluster, metav1.GetOptions{})
	if err != nil {
		return false
	}
	ready, _, _ := unstructured.NestedInt64(obj.Object, "status", "readyInstances")
	return ready >= 1
}

// moveControlPlane copies the StatefulSet's database into the cluster with
// a Job (pg_dump, pg_restore), the server stopped meanwhile so nothing is
// written between the copy and the switch; the server component applies
// the server again right after, reading the new connection string.
func (e *Engine) moveControlPlane(ctx context.Context, namespace, oldURL, newURL string) error {
	k := e.kube
	if err := scaleDeployment(ctx, k, namespace, "shpyrd-server", 0); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err := poll(ctx, "server stopped", func(ctx context.Context) (bool, error) {
		pods, err := k.Kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=shpyrd-server"})
		if err != nil {
			return false, err
		}
		return len(pods.Items) == 0, nil
	}); err != nil {
		return err
	}
	if err := e.applyOpaqueSecret(ctx, namespace, controlPlaneMoveJob, map[string]string{"old": oldURL, "new": newURL}); err != nil {
		return err
	}
	jobs := k.Kube.BatchV1().Jobs(namespace)
	if err := jobs.Delete(ctx, controlPlaneMoveJob, metav1.DeleteOptions{PropagationPolicy: ptr.To(metav1.DeletePropagationBackground)}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	_ = poll(ctx, "previous move job gone", func(ctx context.Context) (bool, error) {
		_, err := jobs.Get(ctx, controlPlaneMoveJob, metav1.GetOptions{})
		return apierrors.IsNotFound(err), nil
	})
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: controlPlaneMoveJob, Namespace: namespace, Labels: map[string]string{"app.kubernetes.io/part-of": "shpyrd", "app.kubernetes.io/name": controlPlaneMoveJob}},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To[int32](0),
			ActiveDeadlineSeconds:   ptr.To[int64](1200),
			TTLSecondsAfterFinished: ptr.To[int32](86400),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app.kubernetes.io/part-of": "shpyrd", "shpyrd.io/control-plane-client": "true", "app.kubernetes.io/name": controlPlaneMoveJob}},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					EnableServiceLinks: ptr.To(false),
					SecurityContext:    &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To[int64](70), RunAsGroup: ptr.To[int64](70), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
					Containers: []corev1.Container{{
						Name:    "move",
						Image:   "docker.io/library/postgres:17-alpine",
						Command: []string{"sh", "-ec", `pg_dump --format=custom --no-owner --no-privileges "$OLD_URL" > /tmp/control-plane.dump && pg_restore --no-owner --no-privileges --role=shpyrd --exit-on-error --dbname "$NEW_URL" /tmp/control-plane.dump && echo moved`},
						Env: []corev1.EnvVar{
							{Name: "OLD_URL", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: controlPlaneMoveJob}, Key: "old"}}},
							{Name: "NEW_URL", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: controlPlaneMoveJob}, Key: "new"}}},
						},
						SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
						VolumeMounts:    []corev1.VolumeMount{{Name: "tmp", MountPath: "/tmp"}},
					}},
					Volumes: []corev1.Volume{{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
				},
			},
		},
	}
	if _, err := jobs.Create(ctx, job, metav1.CreateOptions{}); err != nil {
		return err
	}
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	if err := waitJobFn(waitCtx, k, namespace, controlPlaneMoveJob); err != nil {
		return err
	}
	_ = k.Kube.CoreV1().Secrets(namespace).Delete(ctx, controlPlaneMoveJob, metav1.DeleteOptions{})
	return nil
}

func scaleDeployment(ctx context.Context, k *kube.Client, namespace, name string, replicas int32) error {
	d, err := k.Kube.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	d.Spec.Replicas = ptr.To(replicas)
	_, err = k.Kube.AppsV1().Deployments(namespace).Update(ctx, d, metav1.UpdateOptions{})
	return err
}

func scaleStatefulSet(ctx context.Context, k *kube.Client, namespace, name string, replicas int32) error {
	s, err := k.Kube.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	s.Spec.Replicas = ptr.To(replicas)
	_, err = k.Kube.AppsV1().StatefulSets(namespace).Update(ctx, s, metav1.UpdateOptions{})
	return err
}
