// Package autoscale is the autoscaling extension (RFC-0047): it installs
// what a web process needs to follow its load between a minimum and a
// maximum — KEDA, its HTTP add-on, which counts the requests in flight, and
// the Kubernetes Metrics Server, which measures CPU.
//
// The extension carries no controller, kinds or routes of its own: the
// bounds live on the App (spec.processes.web.autoscale), the API accepts
// them on POST /api/projects/:slug/processes, and the CLI is `shpyrd
// autoscale`. What the extension adds is the installation — and the fact
// that the API refuses bounds until KEDA's kinds exist.
//
// KEDA and the add-on are the sleep extension's too (RFC-0075): one
// ScaledObject per web process carries both, and the installer puts a
// component shared by two extensions in place once.
package autoscale

import (
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// Name of the extension.
const Name = "autoscale"

type extension struct{}

// New returns the autoscale extension.
func New() ext.Extension { return extension{} }

func (extension) Name() string { return Name }
func (extension) Description() string {
	return "Let a web process follow its load between a minimum and a maximum of instances: KEDA, its HTTP add-on and the Metrics Server (shpyrd autoscale)"
}

// Components: KEDA and the Metrics Server first (the add-on needs KEDA's
// CRDs), the add-on after.
func (extension) Components() []ext.ComponentRef {
	return []ext.ComponentRef{
		{Name: "keda", Runlevel: "rc3"},
		{Name: "metrics-server", Runlevel: "rc3"},
		{Name: "keda-http", Runlevel: "rc4"},
	}
}

func (extension) Register(ctrl.Manager, ext.Deps) error { return nil }
func (extension) Routes(ext.Router, ext.Deps) error     { return nil }
func (extension) Types() []ext.ResourceType             { return nil }
func (extension) CLI(ext.CLIGlobals) []*cobra.Command   { return nil }
