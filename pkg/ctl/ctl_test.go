package ctl

import (
	"testing"

	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

type extra struct{}

func (extra) Name() string                          { return "extra" }
func (extra) Description() string                   { return "" }
func (extra) Components() []ext.ComponentRef        { return nil }
func (extra) Register(ctrl.Manager, ext.Deps) error { return nil }
func (extra) Routes(ext.Router, ext.Deps) error     { return nil }
func (extra) Types() []ext.ResourceType             { return nil }
func (extra) CLI(ext.CLIGlobals) []*cobra.Command {
	return []*cobra.Command{ext.ForOperator(&cobra.Command{Use: "workspaces", Short: "the cloud's"})}
}

// A binary built on the core gets shpyrd-ctl with its own commands added.
func TestTheExtensionsCommandsJoinShpyrdCtl(t *testing.T) {
	root := New(extra{})
	for _, name := range []string{"cluster", "console-users", "workspace", "workspaces"} {
		if c, _, err := root.Find([]string{name}); err != nil || c.Name() != name {
			t.Errorf("%s: %v", name, err)
		}
	}
	if c, _, err := New().Find([]string{"workspaces"}); err == nil && c.Name() == "workspaces" {
		t.Error("the core's shpyrd-ctl has the cloud's workspaces")
	}
}
