package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/api"
)

// shpyrd-ctl cluster orphans: what a workspace deletion or a project
// destroy left behind (#274), as a report. Nothing is removed.

func newClusterOrphansCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "orphans",
		Short: "List what a deleted workspace or a destroyed project left behind",
		Long: `List the leftovers a cascade did not reach: project namespaces whose
workspace is gone or whose project row is deleted, image repositories no
project claims, front doors of workspaces that are gone. The report names
them; it removes nothing. A deleting workspace's reaper retries its own
steps; the rest is the operator's.

  shpyrd-ctl cluster orphans
  shpyrd-ctl cluster orphans --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			raw, err := serverRequest(signalContext(), ac.k, "GET", "api/cluster/orphans", nil, "")
			if err != nil {
				return err
			}
			var rep api.OrphanReport
			if err := json.Unmarshal(raw, &rep); err != nil {
				return err
			}
			return g.print(cmd, rep, func(w io.Writer) { printOrphans(w, &rep) })
		},
	}
}

func printOrphans(w io.Writer, rep *api.OrphanReport) {
	if rep.Clean() {
		fmt.Fprintln(w, "Nothing left behind: every project namespace, image repository and front door belongs to a live workspace.")
		return
	}
	if len(rep.Namespaces) > 0 {
		fmt.Fprintf(w, "Project namespaces with nothing live behind them (%d):\n", len(rep.Namespaces))
		for _, ns := range rep.Namespaces {
			fmt.Fprintf(w, "  %s  workspace %s, project %s: %s\n", ns.Name, ns.Workspace, ns.Project, ns.Why)
		}
	}
	if len(rep.Repositories) > 0 {
		fmt.Fprintf(w, "Image repositories no project claims (%d):\n", len(rep.Repositories))
		for _, r := range rep.Repositories {
			fmt.Fprintf(w, "  %s\n", r)
		}
		fmt.Fprintln(w, "  (shpyrd-ctl cluster registry gc reclaims their blobs once their tags are deleted)")
	}
	if len(rep.FrontDoors) > 0 {
		fmt.Fprintf(w, "Front doors of workspaces that are gone (%d):\n", len(rep.FrontDoors))
		for _, d := range rep.FrontDoors {
			fmt.Fprintf(w, "  %s\n", d)
		}
	}
	if rep.RegistryError != "" {
		fmt.Fprintf(w, "The registry could not be read: %s\n", rep.RegistryError)
	}
}
