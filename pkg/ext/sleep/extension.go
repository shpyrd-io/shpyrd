// Package sleep is the scale-to-zero extension (RFC-0075): it installs KEDA
// and the KEDA HTTP add-on, which the app controller uses to put a web
// process to sleep after a quiet period and wake it on the first request.
//
// The extension carries no controller, kinds or routes of its own: the sleep
// policy lives on the App (spec.processes.web.sleep), the API accepts it on
// POST /api/projects/:slug/processes, and the CLI is `shpyrd sleep`. What
// the extension adds is the installation — and the fact that the API refuses
// a policy until the add-on's CRDs exist.
package sleep

import (
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// Name of the extension.
const Name = "sleep"

type extension struct{}

// New returns the sleep extension.
func New() ext.Extension { return extension{} }

func (extension) Name() string { return Name }
func (extension) Description() string {
	return "Scale web processes to zero after a quiet period and wake them on the first request: KEDA and its HTTP add-on (shpyrd sleep)"
}

// Components: KEDA first (the add-on needs its CRDs), both in the keda
// namespace. RFC-0047 autoscaling will share the keda component.
func (extension) Components() []ext.ComponentRef {
	return []ext.ComponentRef{{Name: "keda", Runlevel: "rc3"}, {Name: "keda-http", Runlevel: "rc4"}}
}

func (extension) Register(ctrl.Manager, ext.Deps) error { return nil }
func (extension) Routes(ext.Router, ext.Deps) error     { return nil }
func (extension) Types() []ext.ResourceType             { return nil }
func (extension) CLI(ext.CLIGlobals) []*cobra.Command   { return nil }
