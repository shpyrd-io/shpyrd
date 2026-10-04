//go:build !foss

// Package autosleep is the enterprise's auto sleep (ee/LICENSE): with a
// license in force, web processes and databases sleep by themselves after
// their quiet period, theirs or their workspace's default (RFC-0075).
// Without one nothing sleeps by itself: the core's controller asks this
// extension (ext.SleepGate), and the API refuses a sleep policy with 402.
// What sleep needs in the cluster, KEDA and its HTTP add-on, the core's
// sleep extension installs.
package autosleep

import (
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/shpyrd-io/shpyrd/ee/licensing"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// Name of the extension.
const Name = "auto-sleep"

type extension struct{}

// New returns the auto sleep extension.
func New() ext.Extension { return extension{} }

func (extension) Name() string { return Name }
func (extension) Description() string {
	return "Apps and databases sleep by themselves when nobody uses them"
}
func (extension) Components() []ext.ComponentRef        { return nil }
func (extension) Register(ctrl.Manager, ext.Deps) error { return nil }
func (extension) Routes(ext.Router, ext.Deps) error     { return nil }
func (extension) CLI(ext.CLIGlobals) []*cobra.Command   { return nil }
func (extension) Types() []ext.ResourceType             { return nil }

// SleepAllowed is ext.SleepGate: things sleep by themselves while the
// license is in force.
func (extension) SleepAllowed() bool { return licensing.Active() }

var _ ext.SleepGate = extension{}
