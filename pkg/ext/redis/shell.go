package redis

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// ResourceShell is `shpyrd redis cli` over the API (RFC-0052): the
// engine's CLI on the store's pod, authenticated from the pod's own
// environment, with what follows the name as its arguments. The same pod
// and command the CLI execs into with a kubeconfig.
func (extension) ResourceShell(ctx context.Context, deps ext.Deps, kind, namespace, name string, args []string) (ext.ShellTarget, error) {
	if kind != "Redis" {
		return ext.ShellTarget{}, fmt.Errorf("no shell into %s", kind)
	}
	rd := &shpyrdv1.Redis{}
	if err := deps.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, rd); err != nil {
		if apierrors.IsNotFound(err) {
			return ext.ShellTarget{}, fmt.Errorf("store %q not found (see `shpyrd redis list`)", name)
		}
		return ext.ShellTarget{}, err
	}
	return ext.ShellTarget{Pod: rd.Name + "-0", Container: "redis", Command: cliCommand(rd, args)}, nil
}

// cliCommand is the engine's CLI (valkey-cli or redis-cli) with the
// store's password from the pod's environment, then the arguments.
func cliCommand(rd *shpyrdv1.Redis, args []string) []string {
	engine := firstNonEmpty(rd.Spec.Engine, "valkey")
	extra := ""
	if len(args) > 0 {
		extra = " " + shellJoin(args)
	}
	return []string{"sh", "-c", fmt.Sprintf(`exec %s-cli -a "$REDIS_PASSWORD" --no-auth-warning%s`, engine, extra)}
}
