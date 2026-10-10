package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/snapshots"
)

// shpyrd-ctl cluster snapshots: the provider snapshots of a storage class's
// disks, taken by hand (the platform-snapshots job takes them nightly on the
// cloud profiles) and listed.
func newClusterSnapshotsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshots",
		Short: "Provider snapshots of the block disks of a storage class: take them now, list them",
		Long: `The platform takes a snapshot of every block disk nightly where its profile
names a snapshot class (platform-snapshots). These commands take one now and
list the ones taken, by the job or by hand.`,
	}
	cmd.AddCommand(newClusterSnapshotsTakeCmd(g), newClusterSnapshotsListCmd(g))
	return cmd
}

func newClusterSnapshotsTakeCmd(g *globalFlags) *cobra.Command {
	opts := snapshots.Options{}
	cmd := &cobra.Command{
		Use:   "take --class <storage-class> [--keep 7] [--namespace-prefix p-] [--system]",
		Short: "Take a snapshot of every block disk of a storage class and keep the newest ones",
		Long: `Takes a provider snapshot of every disk whose storage class is --class,
waits for each to be ready (bounded by --wait), and removes the oldest
snapshots taken this way before, so --keep remain per disk (the new one
counts). Disks that have no provider disk yet are skipped and said so.

One line per disk says what happened; the command ends with a summary and
fails when any snapshot failed or was still being taken when the wait ran
out. It refuses to run on a cluster whose profile has no snapshot class.`,
		Example: `  shpyrd-ctl cluster snapshots take --class oci-bv --keep 7
  shpyrd-ctl cluster snapshots take --class oci-bv --system --namespace-prefix p-`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			k, err := kube.Connect(kube.Options{Kubeconfig: g.kubeconfig, Context: g.kubeCtx})
			if err != nil {
				return err
			}
			info, err := install.ReadInstallInfo(ctx, k, "")
			if err != nil {
				return errors.New("this cluster does not run the platform yet, so there is no record of a snapshot class to take snapshots with")
			}
			opts.SnapshotClass = info.Vars[install.VarSnapshotClass]
			c, err := k.ControllerClient()
			if err != nil {
				return err
			}
			if k.Config != nil {
				opts.Sync = func(ctx context.Context, namespace, claim string) { snapshots.Flush(ctx, k, namespace, claim) }
			}
			results, err := snapshots.Take(ctx, g.progress(cmd), c, opts)
			if results != nil {
				if perr := g.print(cmd, results, silent); perr != nil {
					return perr
				}
			}
			return err
		},
	}
	cmd.Flags().StringVar(&opts.Class, "class", "", "storage class of the disks to snapshot (required)")
	cmd.Flags().IntVar(&opts.Keep, "keep", 7, "snapshots to keep per disk, the new one included")
	cmd.Flags().StringVar(&opts.NamespacePrefix, "namespace-prefix", "", "select the namespaces by this name prefix instead of the project label")
	cmd.Flags().BoolVar(&opts.System, "system", false, "also snapshot the disks of the platform's own namespace")
	cmd.Flags().DurationVar(&opts.Wait, "wait", 3*time.Minute, "how long to wait for each snapshot to be ready (0: do not wait)")
	_ = cmd.MarkFlagRequired("class")
	return cmd
}

func newClusterSnapshotsListCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the snapshots taken this way, newest first",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			k, err := kube.Connect(kube.Options{Kubeconfig: g.kubeconfig, Context: g.kubeCtx})
			if err != nil {
				return err
			}
			c, err := k.ControllerClient()
			if err != nil {
				return err
			}
			views, err := snapshots.List(ctx, c)
			if err != nil {
				return err
			}
			return g.print(cmd, views, func(w io.Writer) {
				if len(views) == 0 {
					fmt.Fprintln(w, "No snapshots of this kind on this cluster.")
					return
				}
				snapshots.Print(w, views)
			})
		},
	}
}
