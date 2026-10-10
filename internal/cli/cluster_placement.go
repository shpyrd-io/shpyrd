package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// shpyrd-ctl cluster unpin <project-id>: clears the node pins a move left on
// a project (shpyrd.io/process-nodes on the app, shpyrd.io/project-node on
// its databases) so the scheduler places it by pool again. Refused while a
// pinned process or database still has its data on a node's disk.
func newClusterUnpinCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "unpin <project-id>",
		Short: "Clear the node pins a move left on a project; the scheduler places it by pool again",
		Long: strings.TrimSpace(`
A move pins a project's processes and databases to the node it moved them to.
With project disks on block storage the pins have no purpose: a block volume
follows its process anywhere in the pool. This clears them; each pinned
process and database restarts once. A process or database whose data is still
on a node's disk is unpinned too (the scheduler keeps it on that node through
its volume), which is what a storage migration asks for first. The project id
is the one the console's project archives page shows.`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			k, err := kube.Connect(kube.Options{Kubeconfig: g.kubeconfig, Context: g.kubeCtx})
			if err != nil {
				return err
			}
			raw, err := serverRequest(ctx, k, "DELETE", "api/cluster/project-archives/"+args[0]+"/placement/pins", nil, "")
			if err != nil {
				return err
			}
			var out struct {
				Processes []string `json:"processes"`
				Databases []string `json:"databases"`
				Note      string   `json:"note"`
			}
			if err := json.Unmarshal(raw, &out); err != nil {
				return fmt.Errorf("unexpected answer: %w", err)
			}
			if len(out.Processes) == 0 && len(out.Databases) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Nothing was pinned.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Unpinned processes %s and databases %s. %s\n", listOrNone(out.Processes), listOrNone(out.Databases), out.Note)
			return nil
		},
	}
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, ", ")
}
