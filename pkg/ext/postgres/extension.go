// Package postgres is the postgres extension (RFC-0009): PostgreSQL databases
// for projects, run by the CloudNativePG operator, attachable to apps as
// DATABASE_URL.
package postgres

import (
	"context"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Name of the extension.
const Name = "postgres"

type extension struct{}

// New returns the extension.
func New() ext.Extension { return extension{} }

func (extension) Name() string { return Name }
func (extension) Description() string {
	return "PostgreSQL databases for projects (CloudNativePG), attached to apps as DATABASE_URL (shpyrd pg create)"
}
func (extension) Components() []ext.ComponentRef {
	return []ext.ComponentRef{{Name: "cnpg", Runlevel: "rc2"}, {Name: "barman-cloud", Runlevel: "rc3"}, {Name: "pg-gateway", Runlevel: "rc4"}}
}

// Register runs the Postgres controller and makes the kind attachable.
func (extension) Register(mgr ctrl.Manager, deps ext.Deps) error {
	r := &controller.PostgresReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Recorder: mgr.GetEventRecorderFor("shpyrd"), SystemNamespace: deps.SystemNamespace, Storage: controller.StorageProfile{Class: install.ProjectStorageClass(deps.Var), MinSize: install.ProjectVolumeMinSize(deps.Var)}, DataPool: install.DataPool(deps.Var)}
	if deps.Store != nil {
		r.PlanSleepDefault = planSleepDefault(mgr.GetClient(), deps.Store)
	}
	if err := r.SetupWithManager(mgr); err != nil {
		return err
	}
	controller.RegisterBinder("Postgres", controller.PostgresBinder{})
	return nil
}

func (extension) Routes(ext.Router, ext.Deps) error { return nil }

func (extension) Types() []ext.ResourceType {
	return []ext.ResourceType{{Kind: "Postgres", Group: shpyrdv1.GroupVersion.Group, Version: shpyrdv1.GroupVersion.Version, Resource: "postgres", Bindable: true}}
}

// CLI returns `shpyrd pg`.
func (extension) CLI(g ext.CLIGlobals) []*cobra.Command { return []*cobra.Command{newPgCmd(g)} }

// planSleepDefault answers a project namespace's workspace plan default
// for databases (RFC-0075): the namespace carries its workspace's slug as
// a label; a namespace without one is the default workspace's. Asked once
// a minute per database with no policy of its own, so no cache.
func planSleepDefault(c client.Client, st store.Store) func(ctx context.Context, namespace string) string {
	return func(ctx context.Context, namespace string) string {
		ns := &corev1.Namespace{}
		if err := c.Get(ctx, client.ObjectKey{Name: namespace}, ns); err != nil {
			return ""
		}
		slug := ns.Labels[shpyrdv1.LabelWorkspace]
		if slug == "" {
			slug = store.DefaultWorkspaceSlug(ctx, st)
		}
		wp, err := st.WorkspacePlan(ctx, slug)
		if err != nil || wp == nil {
			return ""
		}
		p, err := st.GetPlan(ctx, wp.PlanName)
		if err != nil || p == nil {
			return ""
		}
		return p.PostgresSleepAfter
	}
}
