package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"k8s.io/apimachinery/pkg/api/resource"
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
				Groups []placementGroupAnswer `json:"groups"`
			}
			if err := json.Unmarshal(raw, &placement); err != nil {
				return fmt.Errorf("unexpected answer: %w", err)
			}
			g := volumeGroup(placement.Groups, volume)
			if g == nil {
				return fmt.Errorf("the project has no volume %q (groups are listed on its placement page)", volume)
			}
			if len(g.StorageClasses) == 1 && g.StorageClasses[0] == class {
				fmt.Fprintf(cmd.OutOrStdout(), "%s is already on %s; nothing to move.\n", volume, class)
				return nil
			}
			var node string
			if len(g.Nodes) > 0 {
				node = g.Nodes[0]
			}
			if node == "" {
				return errors.New("the volume is on no node (it has never been mounted); it needs a node for the copy, mount it first")
			}
			with := ""
			if len(g.Processes) > 0 {
				with = " (with " + strings.Join(g.Processes, ", ") + ", which mounts it)"
			}
			if !yes {
				fmt.Fprintf(cmd.OutOrStdout(), "This pauses project %s while %s%s is copied to %s on %s. Add --yes to go on.\n", project, volume, with, class, node)
				return nil
			}
			body, _ := json.Marshal(map[string]string{"group": g.ID, "node": node, "targetClass": class})
			request := func(method, path string, body []byte) ([]byte, error) {
				return serverRequest(ctx, k, method, "api/cluster/project-archives/"+project+path, body, "application/json")
			}
			out, err := request("POST", "/move", body)
			var warnings []string
			if err != nil {
				if !lostConnection(err) {
					return err
				}
				// The server keeps moving without us: wait for it to finish
				// and read the result from the placement page.
				fmt.Fprintf(cmd.OutOrStdout(), "the connection was lost while %s was being copied; the server goes on, waiting for it\n", volume)
				if warnings, err = waitForMove(ctx, request, volume, class, 5*time.Second); err != nil {
					return err
				}
			} else {
				var answer struct {
					Status   string   `json:"status"`
					Warnings []string `json:"warnings"`
				}
				_ = json.Unmarshal(out, &answer)
				warnings = answer.Warnings
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s moved to %s on %s; the project is back. The old disk is kept for three days (the placement page lists it).\n", volume, class, node)
			for _, w := range warnings {
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
	var noSnapshot, noChecks, back bool
	var wait time.Duration
	cmd := &cobra.Command{
		Use:   "database <namespace>/<name>",
		Short: "Move a database's data to the profile's storage class by switchover, keeping the old volume",
		Long: `Moves a database's data from the storage class it is on (a node's disk,
for the databases made before block storage) to the profile's database class,
without stopping it: a second instance is provisioned on the new class and
catches up, the primary switches over to it (seconds), the data on both
instances is compared (the size within 5 %, every table's row count), the
old instance goes and its volume is kept with the Retain policy for three
days as the way back; a snapshot of the new volume is taken at the end. A
database pinned to a node by an earlier move is refused: clear the project's
pins first (shpyrd-ctl cluster unpin), a planned restart of each pinned
process and database.

Each step reads the state before acting: an interrupted run continues where
it stopped when run again. When the data check fails the database keeps both
instances and nothing is removed; --back returns it to the old volume (the
new instance goes), --no-checks accepts the numbers and finishes.`,
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
			opts := storagemigrate.Options{Namespace: namespace, Name: name, TargetClass: class, Back: back, Wait: wait, Out: g.progress(cmd)}
			if class == install.DatabaseStorageClass(vars) {
				// On the profile's class the volume grows to the profile's
				// minimum once the mark is cleared; on another class it
				// keeps its size.
				if minSize := install.DatabaseVolumeMinSize(vars); minSize != "" {
					if q, err := resource.ParseQuantity(minSize); err == nil {
						opts.MinSize = q
					}
				}
			}
			if !noSnapshot {
				opts.SnapshotClass = vars(install.VarSnapshotClass)
			}
			if !noChecks && k.Config != nil {
				opts.SQL = func(ctx context.Context, namespace, pod, statement string) (string, error) {
					return kexec.Run(ctx, k, namespace, pod, "postgres", []string{"psql", "-U", "postgres", "-d", controller.PostgresDatabase, "-At", "-c", statement})
				}
			}
			res, err := storagemigrate.Database(ctx, c, opts)
			// The summary is for a run that reached an end: done, or stopped
			// on two instances. A refusal or a failed step has its error and
			// the steps already narrated.
			if res != nil && (err == nil || errors.Is(err, storagemigrate.ErrDataCheck)) {
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
	cmd.Flags().BoolVar(&back, "back", false, "return a database that stopped on two instances to its old volume")
	return cmd
}

// lostConnection reports a request that died on the way, not an answer
// from the server: the operation it asked for may be going on.
func lostConnection(err error) bool {
	if err == nil {
		return false
	}
	m := err.Error()
	for _, s := range []string{"connection lost", "connection reset", "broken pipe", "EOF", "TLS handshake timeout", "use of closed network connection", "context deadline exceeded", "no such host", "connection refused"} {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}

// waitForMove polls the project's archive status until no operation runs,
// then reads the placement page: the volume on the class is the move done;
// an operation that failed names its error. request is the server call for
// a path under the project's archives.
func waitForMove(ctx context.Context, request func(method, path string, body []byte) ([]byte, error), volume, class string, poll time.Duration) ([]string, error) {
	deadline := time.Now().Add(time.Hour)
	for {
		raw, err := request("GET", "", nil)
		if err == nil {
			var status struct {
				Kind     string   `json:"kind"`
				Phase    string   `json:"phase"`
				Error    string   `json:"error"`
				Active   bool     `json:"active"`
				Warnings []string `json:"warnings"`
			}
			if err := json.Unmarshal(raw, &status); err != nil {
				return nil, fmt.Errorf("unexpected answer: %w", err)
			}
			if status.Error != "" {
				return nil, fmt.Errorf("the move did not complete: %s (the project is back on its old disk)", status.Error)
			}
			if !status.Active && status.Kind == "" {
				raw, err := request("GET", "/placement", nil)
				if err != nil {
					return nil, err
				}
				var placement struct {
					Groups []placementGroupAnswer `json:"groups"`
				}
				if err := json.Unmarshal(raw, &placement); err != nil {
					return nil, fmt.Errorf("unexpected answer: %w", err)
				}
				g := volumeGroup(placement.Groups, volume)
				if g != nil && len(g.StorageClasses) == 1 && g.StorageClasses[0] == class {
					return status.Warnings, nil
				}
				return nil, fmt.Errorf("the move did not complete: %s is still on %v (the project is back on its old disk); run this again", volume, classesOf(g))
			}
		} else if !lostConnection(err) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, errors.New("waited an hour for the move to finish; look at the project's placement page")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(poll):
		}
	}
}

func classesOf(g *placementGroupAnswer) []string {
	if g == nil {
		return nil
	}
	return g.StorageClasses
}

// placementGroupAnswer is a group of the placement page: a process with the
// volumes it mounts, or a volume on its own.
type placementGroupAnswer struct {
	ID             string   `json:"id"`
	Processes      []string `json:"processes"`
	Volumes        []string `json:"volumes"`
	Nodes          []string `json:"nodes"`
	StorageClasses []string `json:"storageClasses"`
}

// volumeGroup finds the group a volume belongs to: its own when nothing
// mounts it, the mounting process's otherwise (the group's id is the
// process's then, so the id alone would miss it).
func volumeGroup(groups []placementGroupAnswer, volume string) *placementGroupAnswer {
	for i := range groups {
		g := &groups[i]
		if g.ID == "volume:"+volume {
			return g
		}
		for _, v := range g.Volumes {
			if v == volume {
				return g
			}
		}
	}
	return nil
}

func printMigration(w io.Writer, res *storagemigrate.Result) {
	if res.From == "" {
		// Refused or failed before the database's state was read: the
		// error says it all.
		for _, n := range res.Notes {
			fmt.Fprintln(w, "  "+n)
		}
		return
	}
	switch {
	case res.ReturnedTo != "":
		fmt.Fprintf(w, "%s: back on its old volume (%s) with one instance; the instance on %s removed, the migration mark cleared", res.Database, res.ReturnedTo, res.From)
	case res.StoppedOn2:
		fmt.Fprintf(w, "%s: stopped on two instances, the data check did not pass; nothing was removed", res.Database)
	case res.Verified:
		fmt.Fprintf(w, "%s: data on %s, verified", res.Database, res.To)
	default:
		fmt.Fprintf(w, "%s: data on %s, not checked", res.Database, res.To)
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
