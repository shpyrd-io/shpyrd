package ext

import "github.com/spf13/cobra"

// ProjectFlag adds project selection to an extension command. Resolve at
// execution time, so help does not read local config and explicit flags win.
func ProjectFlag(g CLIGlobals, cmd *cobra.Command, dst *string) {
	cmd.Flags().StringVar(dst, "project", "", "project name (default from shpyrd.yaml)")
	run := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		name, err := g.ResolveProject(*dst)
		if err != nil {
			return err
		}
		*dst = name
		return run(cmd, args)
	}
}
