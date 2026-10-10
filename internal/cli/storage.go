package cli

import (
	"context"
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
	cmd.AddCommand(newStorageMigrateDatabaseCmd(g))
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
