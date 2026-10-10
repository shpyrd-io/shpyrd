package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/kexec"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/storagemigrate"
)

// shpyrd-ctl storage: the operator's moves of project data between storage
// classes (the storage plan, step 1).
func newStorageCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "storage",
		Short: "Move project data between storage classes",
	}
	cmd.AddCommand(newStorageMigrateCmd(g))
	return cmd
}

func newStorageMigrateCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate a database's or a volume's data to the profile's storage class",
	}
	cmd.AddCommand(newStorageMigrateDatabaseCmd(g), newStorageMigrateVolumeCmd(g))
	return cmd
}

// A volume's data is copied by the project move (RFC-0081) onto the
// profile's class: the project pauses for the copy, the claim keeps its
// name, the old disk is kept for three days.
func newStorageMigrateVolumeCmd(g *globalFlags) *cobra.Command {
	var class string
	var yes bool
	cmd := &cobra.Command{
		Use:   "volume <project-id> <volume>",
		Short: "Move a volume's data to the profile's storage class; the project pauses for the copy",
		Long: `Copies a volume from the storage class it is on (a node's disk, for the
volumes made before block storage) to the profile's class, through the project
move: the whole project pauses (HTTP answers 503 with Retry-After), the data is
copied and verified by fingerprint, the claim keeps its name on the new disk,
the old disk is kept with the Retain policy for three days as the way back,
and the project resumes. The volume keeps its mount paths and files; its size
becomes the provider minimum where there is one.

The project id is the one the console's project archives page shows. Say
--yes to pause the project without being asked.`,
		Example: `  shpyrd-ctl storage migrate volume 0yo6bvyy7oe89k5wrkwuva9iw uploads --yes`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, volume := args[0], args[1]
			ctx := signalContext()
			k, err := kube.Connect(kube.Options{Kubeconfig: g.kubeconfig, Context: g.kubeCtx})
			if err != nil {
				return err
			}
			info, err := install.ReadInstallInfo(ctx, k, "")
			if err != nil {
				return errors.New("this cluster does not run the platform yet; there is no profile to read the storage class from")
			}
			if class == "" {
				class = install.ProjectStorageClass(func(key string) string { return info.Vars[key] })
			}
			if class == "" {
				return errors.New("the profile names no storage class for disks; say the target with --class")
			}
			raw, err := serverRequest(ctx, k, "GET", "api/cluster/project-archives/"+project+"/placement", nil, "")
			if err != nil {
				return err
			}
			var placement struct {
				Groups []struct {
					ID             string   `json:"id"`
					Nodes          []string `json:"nodes"`
					StorageClasses []string `json:"storageClasses"`
				} `json:"groups"`
			}
			if err := json.Unmarshal(raw, &placement); err != nil {
				return fmt.Errorf("unexpected answer: %w", err)
			}
			group := "volume:" + volume
			var node string
			found := false
			for _, g := range placement.Groups {
				if g.ID != group {
					continue
				}
				found = true
				if len(g.Nodes) > 0 {
					node = g.Nodes[0]
				}
				if len(g.StorageClasses) == 1 && g.StorageClasses[0] == class {
					fmt.Fprintf(cmd.OutOrStdout(), "%s is already on %s; nothing to move.\n", volume, class)
					return nil
				}
			}
			if !found {
				return fmt.Errorf("the project has no volume %q (groups are listed on its placement page)", volume)
			}
			if node == "" {
				return errors.New("the volume is on no node (it has never been mounted); it needs a node for the copy, mount it first")
			}
			if !yes {
				fmt.Fprintf(cmd.OutOrStdout(), "This pauses project %s while %s is copied to %s on %s. Add --yes to go on.\n", project, volume, class, node)
				return nil
			}
			body, _ := json.Marshal(map[string]string{"group": group, "node": node, "targetClass": class})
			out, err := serverRequest(ctx, k, "POST", "api/cluster/project-archives/"+project+"/move", body, "application/json")
			if err != nil {
				return err
			}
			var answer struct {
				Status   string   `json:"status"`
				Warnings []string `json:"warnings"`
			}
			_ = json.Unmarshal(out, &answer)
			fmt.Fprintf(cmd.OutOrStdout(), "%s moved to %s on %s; the project is back. The old disk is kept for three days (the placement page lists it).\n", volume, class, node)
			for _, w := range answer.Warnings {
				fmt.Fprintln(cmd.OutOrStdout(), "  "+w)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&class, "class", "", "the storage class to move to (default: the profile's class for disks)")
	cmd.Flags().BoolVar(&yes, "yes", false, "pause the project and move without asking")
	return cmd
}

func newStorageMigrateDatabaseCmd(g *globalFlags) *cobra.Command {
	var class string
	var noSnapshot, noChecks bool
	var wait time.Duration
	cmd := &cobra.Command{
		Use:   "database <namespace>/<name>",
		Short: "Move a database's data to the profile's storage class by switchover, keeping the old volume",
		Long: `Moves a database's data from the storage class it is on (a node's disk,
for the databases made before block storage) to the profile's database class,
without stopping it: a second instance is provisioned on the new class and
catches up, the primary switches over to it (seconds), the old instance goes
and its volume is kept with the Retain policy for three days as the way
back. The database size and every table's row count are read before and
after and compared; a snapshot of the new volume is taken at the end.

Each step reads the state before acting: an interrupted run continues where
it stopped when run again. The project keeps working throughout.`,
		Example: `  shpyrd-ctl storage migrate database p-2f4vrjuxtxvq9bj8qlsjuz4gj/db
  shpyrd-ctl storage migrate database p-2f4vrjuxtxvq9bj8qlsjuz4gj/db --class oci-bv --wait 30m`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			namespace, name, ok := strings.Cut(args[0], "/")
			if !ok || namespace == "" || name == "" {
				return errors.New("name the database as <namespace>/<name>")
			}
			ctx := signalContext()
			k, err := kube.Connect(kube.Options{Kubeconfig: g.kubeconfig, Context: g.kubeCtx})
			if err != nil {
				return err
			}
			info, err := install.ReadInstallInfo(ctx, k, "")
			if err != nil {
				return errors.New("this cluster does not run the platform yet; there is no profile to read the storage class from")
			}
			vars := func(key string) string { return info.Vars[key] }
			if class == "" {
				class = install.DatabaseStorageClass(vars)
			}
			if class == "" {
				return errors.New("the profile names no database storage class; say the target with --class")
			}
			c, err := k.ControllerClient()
			if err != nil {
				return err
			}
			opts := storagemigrate.Options{Namespace: namespace, Name: name, TargetClass: class, Wait: wait, Out: g.progress(cmd)}
			if !noSnapshot {
				opts.SnapshotClass = vars(install.VarSnapshotClass)
			}
			if !noChecks && k.Config != nil {
				opts.SQL = func(ctx context.Context, namespace, pod, statement string) (string, error) {
					return kexec.Run(ctx, k, namespace, pod, "postgres", []string{"psql", "-U", "postgres", "-d", controller.PostgresDatabase, "-At", "-c", statement})
				}
			}
			res, err := storagemigrate.Database(ctx, c, opts)
			if res != nil {
				if perr := g.print(cmd, res, func(w io.Writer) { printMigration(w, res) }); perr != nil {
					return perr
				}
			}
			return err
		},
	}
	cmd.Flags().StringVar(&class, "class", "", "the storage class to move to (default: the profile's database class)")
	cmd.Flags().BoolVar(&noSnapshot, "no-snapshot", false, "do not snapshot the new volume at the end")
	cmd.Flags().BoolVar(&noChecks, "no-checks", false, "skip the row-count and size checks")
	cmd.Flags().DurationVar(&wait, "wait", 20*time.Minute, "how long to wait for each step")
	return cmd
}

func printMigration(w io.Writer, res *storagemigrate.Result) {
	if res.Verified {
		fmt.Fprintf(w, "%s: data on %s, verified", res.Database, res.To)
	} else {
		fmt.Fprintf(w, "%s: data on %s, NOT verified", res.Database, res.To)
	}
	if res.OldPV != "" {
		fmt.Fprintf(w, "; old volume %s kept", res.OldPV)
	}
	if res.Snapshot != "" {
		fmt.Fprintf(w, "; snapshot %s", res.Snapshot)
	}
	fmt.Fprintln(w, ".")
	for _, n := range res.Notes {
		fmt.Fprintln(w, "  "+n)
	}
}
