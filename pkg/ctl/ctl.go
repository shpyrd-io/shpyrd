// Package ctl builds the shpyrd-ctl command tree for binaries built on the
// core: the core's own shpyrd-ctl, and the cloud's CLI, which is shpyrd-ctl
// with its commands added (another module cannot import internal/cli).
// The commands of the extensions given join those of the built-in ones:
// their operator commands, as shpyrd-ctl mounts its own.
package ctl

import (
	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/internal/cli"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// New returns the shpyrd-ctl root command with the extensions' commands.
func New(extra ...ext.Extension) *cobra.Command { return cli.NewCtl(extra...) }

// SetVersion sets what `version` prints.
func SetVersion(v string) { cli.Version = v }
