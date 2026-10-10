package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/version"
)

// NewCtl builds the shpyrd-ctl command tree: cluster operations that need a
// kubeconfig (RFC-0052). Developers use `shpyrd` instead. The operator
// commands of the extensions given join those of the built-in ones (the
// ee extensions, where ee is built in).
func NewCtl(extra ...ext.Extension) *cobra.Command {
	g := &globalFlags{}
	root := &cobra.Command{
		Use:           "shpyrd-ctl",
		Short:         "Operate a shpyrd cluster (needs a kubeconfig)",
		Long:          "shpyrd-ctl manages the cluster lifecycle: bootstrap, upgrade, backup, restore, extensions, the registry and the admin token. Developers who only build and deploy apps use `shpyrd` instead.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&g.kubeconfig, "kubeconfig", os.Getenv("KUBECONFIG"), "path to the kubeconfig file")
	root.PersistentFlags().StringVar(&g.kubeCtx, "context", "", "kubeconfig context to use")
	root.PersistentFlags().BoolVarP(&g.verbose, "verbose", "v", false, "verbose output")
	addOutputFlags(root, g)
	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		// The operator's tool speaks to the cluster it is pointed at, never
		// through a saved `shpyrd login` session: those are a developer's
		// tokens for one workspace door, which the operator endpoints
		// refuse (and they go stale when a cluster is rebuilt).
		preferKubeconfig = true
	}
	root.CompletionOptions.HiddenDefaultCmd = true

	root.AddCommand(newClusterCmd(g))
	root.AddCommand(newStorageCmd(g))
	root.AddCommand(newExtensionsCmd(g))
	root.AddCommand(newSizesCmd(g))
	root.AddCommand(newGlobalsCmd(g))
	root.AddCommand(newConsoleUsersCmd(g))
	root.AddCommand(newCtlWorkspaceCmd(g))
	// The operator's extension commands (users, auth, object-storage);
	// project resources (pg, redis) are the developer's, in `shpyrd`.
	addExtensionCommands(root, g, ext.AudienceOperator, extra...)
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the CLI version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version.Version)
		},
	})
	return root
}
