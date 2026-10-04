//go:build !foss

// Package costs is the enterprise Costs (ee/LICENSE): what the cluster
// uses and costs, line by line, per workspace, project, process and
// resource, and the drains that send those lines to another system.
// Three kinds of lines: usage (what the metering loop measured), estimated
// (OpenCost's allocation at list prices) and real (the provider's bill,
// from OCI). The lines carry the OCID of the node or volume they ran on, so
// whoever receives them reconciles the estimate with the bill.
package costs

import (
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/shpyrd-io/shpyrd/ee/licensing"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ext/opencost"
	"github.com/shpyrd-io/shpyrd/pkg/install"
)

// Name of the extension, and the capability the console shows Costs by.
const Name = "costs"

type extension struct{}

// New returns the costs extension.
func New() ext.Extension { return extension{} }

func (extension) Name() string { return Name }
func (extension) Description() string {
	return "Costs and cost drains: what the cluster uses and costs, sent elsewhere"
}
func (extension) Components() []ext.ComponentRef { return nil }
func (extension) Types() []ext.ResourceType      { return nil }

// Register runs the collector and the drains on the leader.
func (extension) Register(mgr ctrl.Manager, deps ext.Deps) error {
	if deps.Store == nil {
		return nil
	}
	c := &Collector{Store: deps.Store, Namespace: deps.SystemNamespace, OpenCostURL: opencost.AllocationURL}
	s := &Sender{Store: deps.Store, Namespace: deps.SystemNamespace, Cluster: firstNonEmpty(deps.Var(install.VarCluster), deps.Var(install.VarDomain))}
	if deps.Kube != nil {
		c.Kube, s.Kube = deps.Kube.Kube, deps.Kube.Kube
	}
	if err := mgr.Add(c); err != nil {
		return err
	}
	return mgr.Add(s)
}

// Routes mounts the console's routes, behind the license.
func (extension) Routes(r ext.Router, deps ext.Deps) error {
	if deps.Store == nil {
		return nil
	}
	h := &handlers{store: deps.Store, namespace: deps.SystemNamespace}
	if deps.Kube != nil {
		h.kube = deps.Kube.Kube
	}
	g := r.Admin().Group("/cluster", licensing.Require())
	g.GET("/costs", h.summary)
	g.GET("/costs/oci", h.ociStatus)
	g.PUT("/costs/oci", h.putOCI)
	g.DELETE("/costs/oci", h.deleteOCI)
	g.GET("/cost-drains", h.listDrains)
	g.POST("/cost-drains", h.createDrain)
	g.DELETE("/cost-drains/:name", h.deleteDrain)
	return nil
}

func (extension) CLI(g ext.CLIGlobals) []*cobra.Command {
	return []*cobra.Command{ext.ForOperator(newCostsCmd(g))}
}
