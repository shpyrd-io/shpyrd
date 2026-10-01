package postgres

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	pod, err := primaryPod(ctx, deps.Client, pg)
	if err != nil {
		return ext.ShellTarget{}, err
	}
	return ext.ShellTarget{Pod: pod, Container: "postgres", Command: psqlCommand(args)}, nil
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
