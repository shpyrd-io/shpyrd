package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// ResourceShell is `shpyrd pg psql` over the API (RFC-0052): psql on the
// database's primary instance, with what follows the name as its
// arguments. The same pod and command the CLI execs into with a
// kubeconfig.
func (extension) ResourceShell(ctx context.Context, deps ext.Deps, kind, namespace, name string, args []string) (ext.ShellTarget, error) {
	if kind != "Postgres" {
		return ext.ShellTarget{}, fmt.Errorf("no shell into %s", kind)
	}
	pg := &shpyrdv1.Postgres{}
	if err := deps.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, pg); err != nil {
		if apierrors.IsNotFound(err) {
			return ext.ShellTarget{}, fmt.Errorf("database %q not found (see `shpyrd pg list`)", name)
		}
		return ext.ShellTarget{}, err
	}
	if pg.Spec.Type == shpyrdv1.PostgresTypeShared {
		// A shared database has no pod of its own: psql runs in a helper
		// pod of the project, connecting over the network with the
		// tenant's credentials; never inside the platform's server.
		pod, err := sharedPsqlHelper(ctx, deps.Client, pg, 2*time.Minute)
		if err != nil {
			return ext.ShellTarget{}, err
		}
		return ext.ShellTarget{Pod: pod, Container: "psql", Command: append([]string{"psql"}, args...)}, nil
	}
	pod, err := primaryPod(ctx, deps.Client, pg)
	if err != nil {
		return ext.ShellTarget{}, err
	}
	return ext.ShellTarget{Pod: pod, Container: "postgres", Command: psqlCommand(args)}, nil
}

// psqlHelperName is the helper pod psql for a shared database runs in.
func psqlHelperName(pg *shpyrdv1.Postgres) string { return "psql-" + pg.Name }

// sharedPsqlHelper is the helper pod, created when missing and waited for
// until it runs; it expires by itself after an hour.
func sharedPsqlHelper(ctx context.Context, c client.Client, pg *shpyrdv1.Postgres, wait time.Duration) (string, error) {
	if pg.Status.Phase != shpyrdv1.ResourceReady {
		return "", fmt.Errorf("database %s is %s (%s)", pg.Name, strings.ToLower(firstNonEmpty(pg.Status.Phase, "provisioning")), pg.Status.Message)
	}
	key := types.NamespacedName{Namespace: pg.Namespace, Name: psqlHelperName(pg)}
	pod := &corev1.Pod{}
	err := c.Get(ctx, key, pod)
	if apierrors.IsNotFound(err) || (err == nil && (pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed)) {
		if err == nil {
			_ = c.Delete(ctx, pod)
		}
		pod = psqlHelperPod(pg)
		if err := c.Create(ctx, pod); err != nil && !apierrors.IsAlreadyExists(err) {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	deadline := time.Now().Add(wait)
	for {
		if err := c.Get(ctx, key, pod); err != nil {
			return "", err
		}
		if pod.Status.Phase == corev1.PodRunning {
			return pod.Name, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("the psql helper for %s did not start in %s (%s)", pg.Name, wait, pod.Status.Phase)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// psqlHelperPod renders the helper: the PostgreSQL image, idle, with the
// connection in its environment from the database's own secret, so psql
// needs no arguments.
func psqlHelperPod(pg *shpyrdv1.Postgres) *corev1.Pod {
	from := func(name, key string) corev1.EnvVar {
		return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: pg.Name + "-app"}, Key: key}}}
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: psqlHelperName(pg), Namespace: pg.Namespace, Labels: map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", "shpyrd.io/postgres": pg.Name, "shpyrd.io/helper": "psql"}},
		Spec: corev1.PodSpec{
			RestartPolicy:         corev1.RestartPolicyNever,
			ActiveDeadlineSeconds: ptr.To[int64](3600),
			SecurityContext:       &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To[int64](26), RunAsGroup: ptr.To[int64](26), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
			Containers: []corev1.Container{{
				Name:    "psql",
				Image:   "ghcr.io/cloudnative-pg/postgresql:17",
				Command: []string{"sleep", "3600"},
				Env: []corev1.EnvVar{
					from("PGHOST", "host"), from("PGPORT", "port"), from("PGDATABASE", "dbname"),
					from("PGUSER", "username"), from("PGPASSWORD", "password"), from("PGSSLMODE", "sslmode"),
				},
				Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("32Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")}},
				SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
			}},
		},
	}
}

// primaryPod is the pod CloudNativePG runs the primary on.
func primaryPod(ctx context.Context, c client.Client, pg *shpyrdv1.Postgres) (string, error) {
	var pods corev1.PodList
	if err := c.List(ctx, &pods, client.InNamespace(pg.Namespace), client.MatchingLabels{"cnpg.io/cluster": pg.Name, "cnpg.io/instanceRole": "primary"}); err != nil {
		return "", err
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("database %s has no primary instance yet (%s)", pg.Name, firstNonEmpty(pg.Status.Message, pg.Status.Phase))
	}
	return pods.Items[0].Name, nil
}

// psqlCommand is psql on the app database, then the person's arguments.
func psqlCommand(args []string) []string {
	return append([]string{"psql", "-d", "app"}, args...)
}
