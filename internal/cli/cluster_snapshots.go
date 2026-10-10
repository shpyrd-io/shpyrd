package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/kexec"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// Interim disk snapshots: until the platform takes its nightly snapshots,
// an operator protects the block-volume claims of a storage class with
// provider snapshots (`shpyrd-ctl cluster snapshots take`), keeping a
// bounded number per disk. The snapshots are the ones RFC-0060 takes for a
// volume (the same class, the same objects), so the ones of a project's
// volume show up in `shpyrd volumes snapshots` and restore the same way.

// LabelInterimSnapshot marks a snapshot taken by `cluster snapshots take`;
// only those count towards --keep and are ever removed by it.
const LabelInterimSnapshot = "shpyrd.io/interim-snapshot"

// interimSnapshotOptions is what `cluster snapshots take` was asked to do.
type interimSnapshotOptions struct {
	// Class selects the claims: those whose storage class is this one.
	Class string
	// SnapshotClass is the cluster's VolumeSnapshotClass, from the install
	// record; "" means the cluster cannot take snapshots and the run is
	// refused.
	SnapshotClass string
	// NamespacePrefix, when set, selects the namespaces by name instead of
	// by the project label.
	NamespacePrefix string
	// System also takes the claims of the platform's own namespace.
	System bool
	// Keep is how many snapshots stay per disk, the new one included.
	Keep int
	// Wait bounds the wait for each snapshot to be ready; 0 does not wait.
	Wait time.Duration
	// Poll is the interval between readiness checks (default 3s).
	Poll time.Duration
	// Now names the snapshots (default time.Now).
	Now func() time.Time
	// Sync, when set, asks the instances mounting a claim to flush before
	// the snapshot is taken (best effort).
	Sync func(ctx context.Context, namespace, claim string)
}

// interimSnapshotResult is what happened to one disk.
type interimSnapshotResult struct {
	Namespace string `json:"namespace"`
	Claim     string `json:"claim"`
	Snapshot  string `json:"snapshot,omitempty"`
	// Status: ready, requested (not waited for), in-progress (the wait ran
	// out), failed, or skipped (no disk yet).
	Status  string   `json:"status"`
	Size    string   `json:"size,omitempty"`
	Message string   `json:"message,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// interimSnapshotView is one interim snapshot as listed.
type interimSnapshotView struct {
	Namespace string    `json:"namespace"`
	Claim     string    `json:"claim"`
	Name      string    `json:"name"`
	Size      string    `json:"size,omitempty"`
	Ready     bool      `json:"ready"`
	Message   string    `json:"message,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

var errNoSnapshotClass = errors.New("disk snapshots are not set up on this cluster: its profile names no snapshot class, so there is nowhere to take them (cloud profiles such as oci provide one; local clusters have none)")

func newClusterSnapshotsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshots",
		Short: "Protect the cluster's block disks with provider snapshots until the nightly ones exist",
		Long: `Interim protection for the block-volume disks of a storage class: a
provider snapshot of each disk, taken now, with a bounded number kept per
disk. Meant to run from a scheduler (one run a night) until the platform
takes its own nightly snapshots; the snapshots are the same objects a
project's "shpyrd volumes snapshot" takes, so the ones of a project's volume
are listed and restored there too.

The disks are the claims whose storage class is the one given, in project
namespaces (or the namespaces with --namespace-prefix) and, with --system,
in the platform's own namespace. Each snapshot is named after its disk and
the time it was taken. Snapshots are consistent at the block level; a
database's own backups are the real backup, the snapshot is the fallback.`,
	}
	cmd.AddCommand(newClusterSnapshotsTakeCmd(g), newClusterSnapshotsListCmd(g))
	return cmd
}

func newClusterSnapshotsTakeCmd(g *globalFlags) *cobra.Command {
	opts := interimSnapshotOptions{}
	cmd := &cobra.Command{
		Use:   "take --class <storage-class> [--keep 7] [--namespace-prefix p-] [--system]",
		Short: "Take a snapshot of every block disk of a storage class and keep the newest ones",
		Long: `Takes a provider snapshot of every disk whose storage class is --class,
waits for each to be ready (bounded by --wait), and removes the oldest
snapshots this command took before, so --keep remain per disk (the new one
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
				opts.Sync = func(ctx context.Context, namespace, claim string) { flushClaim(ctx, k, namespace, claim) }
			}
			results, err := takeInterimSnapshots(ctx, g.progress(cmd), c, opts)
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
		Short:   "List the snapshots taken by this command, newest first",
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
			views, err := listInterimSnapshots(ctx, c)
			if err != nil {
				return err
			}
			return g.print(cmd, views, func(w io.Writer) {
				if len(views) == 0 {
					fmt.Fprintln(w, "No interim snapshots on this cluster.")
					return
				}
				printInterimSnapshots(w, views)
			})
		},
	}
}

// takeInterimSnapshots snapshots every selected disk, narrating one line
// per disk on out, and returns the results with an error when any
// snapshot failed or was not ready in time.
func takeInterimSnapshots(ctx context.Context, out io.Writer, c client.Client, opts interimSnapshotOptions) ([]interimSnapshotResult, error) {
	if opts.SnapshotClass == "" {
		return nil, errNoSnapshotClass
	}
	if opts.Class == "" {
		return nil, errors.New("say which storage class's disks to snapshot")
	}
	if opts.Keep < 1 {
		return nil, errors.New("keep at least one snapshot per disk")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Poll <= 0 {
		opts.Poll = 3 * time.Second
	}
	claims, err := interimClaims(ctx, c, opts)
	if err != nil {
		return nil, err
	}
	if len(claims) == 0 {
		fmt.Fprintf(out, "No disks of class %s in the selected namespaces; nothing to snapshot.\n", opts.Class)
		return []interimSnapshotResult{}, nil
	}
	results := make([]interimSnapshotResult, 0, len(claims))
	var ready, pending, failed, skipped int
	for i := range claims {
		res := takeInterimSnapshot(ctx, c, &claims[i], opts)
		results = append(results, res)
		switch res.Status {
		case "ready", "requested":
			if res.Status == "ready" {
				ready++
			} else {
				pending++
			}
		case "skipped":
			skipped++
		default:
			failed++
		}
		fmt.Fprintln(out, describeInterimResult(res, opts.Wait))
		if ctx.Err() != nil {
			return results, ctx.Err()
		}
	}
	var parts []string
	if ready > 0 {
		parts = append(parts, fmt.Sprintf("%d ready", ready))
	}
	if pending > 0 {
		parts = append(parts, fmt.Sprintf("%d requested", pending))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped (no disk yet)", skipped))
	}
	fmt.Fprintf(out, "%d disks of class %s: %s.\n", len(claims), opts.Class, strings.Join(parts, ", "))
	if failed > 0 {
		return results, fmt.Errorf("%d of %d snapshots did not complete", failed, len(claims))
	}
	return results, nil
}

// takeInterimSnapshot handles one disk: snapshot, wait, prune.
func takeInterimSnapshot(ctx context.Context, c client.Client, pvc *corev1.PersistentVolumeClaim, opts interimSnapshotOptions) interimSnapshotResult {
	res := interimSnapshotResult{Namespace: pvc.Namespace, Claim: pvc.Name}
	if pvc.Status.Phase != corev1.ClaimBound {
		res.Status, res.Message = "skipped", "no disk yet"
		return res
	}
	if opts.Sync != nil {
		opts.Sync(ctx, pvc.Namespace, pvc.Name)
	}
	snap := interimSnapshotObject(pvc, opts.SnapshotClass, interimSnapshotName(pvc.Name, opts.Now()))
	res.Snapshot = snap.GetName()
	if err := c.Create(ctx, snap); err != nil {
		res.Status = "failed"
		switch {
		case meta.IsNoMatchError(err):
			res.Message = "this cluster has no snapshot support installed"
		case apierrors.IsAlreadyExists(err):
			res.Message = "a snapshot of that name was taken a moment ago; try again"
		default:
			res.Message = err.Error()
		}
		return res
	}
	if opts.Wait <= 0 {
		res.Status = "requested"
	} else {
		res.Status, res.Size, res.Message = waitInterimSnapshot(ctx, c, snap, opts.Wait, opts.Poll)
	}
	if res.Status == "failed" {
		return res
	}
	removed, err := pruneInterimSnapshots(ctx, c, pvc.Namespace, pvc.Name, opts.Keep)
	res.Removed = removed
	if err != nil && res.Message == "" {
		res.Message = "older snapshots could not be removed: " + err.Error()
	}
	return res
}

// waitInterimSnapshot polls the snapshot until it is ready, fails, or the
// wait runs out ("in-progress").
func waitInterimSnapshot(ctx context.Context, c client.Client, snap *unstructured.Unstructured, wait, poll time.Duration) (status, size, message string) {
	deadline := time.Now().Add(wait)
	key := types.NamespacedName{Namespace: snap.GetNamespace(), Name: snap.GetName()}
	for {
		cur := &unstructured.Unstructured{}
		cur.SetGroupVersionKind(controller.VolumeSnapshotGVK)
		if err := c.Get(ctx, key, cur); err != nil {
			return "failed", "", "the snapshot disappeared while it was being taken: " + err.Error()
		}
		if ready, _, _ := unstructured.NestedBool(cur.Object, "status", "readyToUse"); ready {
			size, _, _ = unstructured.NestedString(cur.Object, "status", "restoreSize")
			return "ready", size, ""
		}
		if msg, _, _ := unstructured.NestedString(cur.Object, "status", "error", "message"); msg != "" {
			return "failed", "", msg
		}
		if time.Now().After(deadline) {
			return "in-progress", "", ""
		}
		if err := sleepCtx(ctx, poll); err != nil {
			return "in-progress", "", ""
		}
	}
}

// interimNamespaces is the set of namespaces whose disks are selected:
// project namespaces (by label, or by --namespace-prefix) and, with
// --system, the platform's own.
func interimNamespaces(ctx context.Context, c client.Client, opts interimSnapshotOptions) (map[string]bool, error) {
	list := &corev1.NamespaceList{}
	if err := c.List(ctx, list); err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	set := map[string]bool{}
	for _, ns := range list.Items {
		switch {
		case opts.NamespacePrefix != "":
			if strings.HasPrefix(ns.Name, opts.NamespacePrefix) {
				set[ns.Name] = true
			}
		case ns.Labels[shpyrdv1.LabelProject] != "":
			set[ns.Name] = true
		}
	}
	if opts.System {
		set[install.DefaultSystemNamespace] = true
	}
	return set, nil
}

// interimClaims lists the bound and unbound claims of the class in the
// selected namespaces, in namespace then name order.
func interimClaims(ctx context.Context, c client.Client, opts interimSnapshotOptions) ([]corev1.PersistentVolumeClaim, error) {
	namespaces, err := interimNamespaces(ctx, c, opts)
	if err != nil {
		return nil, err
	}
	list := &corev1.PersistentVolumeClaimList{}
	if err := c.List(ctx, list); err != nil {
		return nil, fmt.Errorf("list disks: %w", err)
	}
	var out []corev1.PersistentVolumeClaim
	for _, pvc := range list.Items {
		if !namespaces[pvc.Namespace] || pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != opts.Class {
			continue
		}
		out = append(out, pvc)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// interimSnapshotName is <claim>-<date>-<time>, within the 63 characters
// a name may have (the claim part is cut when it must be).
func interimSnapshotName(claim string, now time.Time) string {
	stamp := now.UTC().Format("20060102-150405")
	if max := 63 - len(stamp) - 1; len(claim) > max {
		claim = strings.TrimRight(claim[:max], "-")
	}
	return claim + "-" + stamp
}

// interimSnapshotObject is the VolumeSnapshot of a claim: labelled as an
// interim snapshot and, for a project volume's claim, with the volume it
// belongs to (so the project lists and restores it like its own).
func interimSnapshotObject(pvc *corev1.PersistentVolumeClaim, snapshotClass, name string) *unstructured.Unstructured {
	snap := &unstructured.Unstructured{}
	snap.SetGroupVersionKind(controller.VolumeSnapshotGVK)
	snap.SetName(name)
	snap.SetNamespace(pvc.Namespace)
	labels := map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", LabelInterimSnapshot: "true"}
	if vol := strings.TrimPrefix(pvc.Name, shpyrdv1.PVCPrefix); vol != pvc.Name && vol != "" {
		labels[shpyrdv1.LabelVolumeOf] = vol
	}
	snap.SetLabels(labels)
	_ = unstructured.SetNestedField(snap.Object, snapshotClass, "spec", "volumeSnapshotClassName")
	_ = unstructured.SetNestedField(snap.Object, pvc.Name, "spec", "source", "persistentVolumeClaimName")
	return snap
}

// pruneInterimSnapshots removes the interim snapshots of a claim beyond
// the newest keep, and returns the names removed, oldest first.
func pruneInterimSnapshots(ctx context.Context, c client.Client, namespace, claim string, keep int) ([]string, error) {
	snaps, err := interimSnapshotsOf(ctx, c, namespace)
	if err != nil {
		return nil, err
	}
	var mine []unstructured.Unstructured
	for _, s := range snaps {
		if src, _, _ := unstructured.NestedString(s.Object, "spec", "source", "persistentVolumeClaimName"); src == claim {
			mine = append(mine, s)
		}
	}
	sortInterimNewestFirst(mine)
	var removed []string
	for i := len(mine) - 1; i >= keep; i-- {
		if err := c.Delete(ctx, &mine[i]); client.IgnoreNotFound(err) != nil {
			return removed, err
		}
		removed = append(removed, mine[i].GetName())
	}
	return removed, nil
}

// interimSnapshotsOf lists the interim snapshots of a namespace ("" for
// every namespace).
func interimSnapshotsOf(ctx context.Context, c client.Client, namespace string) ([]unstructured.Unstructured, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(controller.VolumeSnapshotGVK.GroupVersion().WithKind("VolumeSnapshotList"))
	listOpts := []client.ListOption{client.MatchingLabels{LabelInterimSnapshot: "true"}}
	if namespace != "" {
		listOpts = append(listOpts, client.InNamespace(namespace))
	}
	if err := c.List(ctx, list, listOpts...); err != nil {
		if meta.IsNoMatchError(err) {
			return nil, errors.New("this cluster has no snapshot support installed")
		}
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	return list.Items, nil
}

// sortInterimNewestFirst orders by creation time, then by name (which
// carries the time the snapshot was taken) for ties.
func sortInterimNewestFirst(snaps []unstructured.Unstructured) {
	sort.Slice(snaps, func(i, j int) bool {
		ti, tj := snaps[i].GetCreationTimestamp(), snaps[j].GetCreationTimestamp()
		if !ti.Equal(&tj) {
			return ti.After(tj.Time)
		}
		return snaps[i].GetName() > snaps[j].GetName()
	})
}

func listInterimSnapshots(ctx context.Context, c client.Client) ([]interimSnapshotView, error) {
	snaps, err := interimSnapshotsOf(ctx, c, "")
	if err != nil {
		return nil, err
	}
	sortInterimNewestFirst(snaps)
	views := make([]interimSnapshotView, 0, len(snaps))
	for _, s := range snaps {
		v := interimSnapshotView{Namespace: s.GetNamespace(), Name: s.GetName(), CreatedAt: s.GetCreationTimestamp().Time}
		v.Claim, _, _ = unstructured.NestedString(s.Object, "spec", "source", "persistentVolumeClaimName")
		v.Ready, _, _ = unstructured.NestedBool(s.Object, "status", "readyToUse")
		v.Size, _, _ = unstructured.NestedString(s.Object, "status", "restoreSize")
		if msg, _, _ := unstructured.NestedString(s.Object, "status", "error", "message"); msg != "" {
			v.Message = msg
		} else if !v.Ready {
			v.Message = "being taken"
		}
		views = append(views, v)
	}
	return views, nil
}

func printInterimSnapshots(w io.Writer, views []interimSnapshotView) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tDISK\tSNAPSHOT\tSIZE\tSTATUS\tAGE")
	for _, v := range views {
		status := "ready"
		if !v.Ready {
			status = v.Message
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", v.Namespace, v.Claim, v.Name, firstNonEmpty(v.Size, "-"), status, age(metav1.NewTime(v.CreatedAt)))
	}
	_ = tw.Flush()
}

// describeInterimResult is the one line said about a disk.
func describeInterimResult(res interimSnapshotResult, wait time.Duration) string {
	disk := res.Namespace + "/" + res.Claim
	var line string
	switch res.Status {
	case "skipped":
		line = fmt.Sprintf("%s: no disk yet, skipped", disk)
	case "ready":
		line = fmt.Sprintf("%s: snapshot %s ready%s", disk, res.Snapshot, sizeSuffix(res.Size))
	case "requested":
		line = fmt.Sprintf("%s: snapshot %s requested; it finishes in the background", disk, res.Snapshot)
	case "in-progress":
		line = fmt.Sprintf("%s: snapshot %s still being taken after %s; it may yet finish, look again later", disk, res.Snapshot, wait.Round(time.Second))
	default:
		line = fmt.Sprintf("%s: snapshot %s failed: %s", disk, res.Snapshot, res.Message)
	}
	if n := len(res.Removed); n > 0 {
		line += fmt.Sprintf("; %d older removed", n)
	}
	if res.Message != "" && res.Status != "failed" && res.Status != "skipped" {
		line += "; " + res.Message
	}
	return line
}

// flushClaim runs `sync` in every running instance that mounts the claim,
// so what was written a moment ago is in the snapshot (best effort).
func flushClaim(ctx context.Context, k *kube.Client, namespace, claim string) {
	if k == nil || k.Kube == nil || k.Config == nil {
		return
	}
	pods, err := k.Kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	for _, p := range pods.Items {
		if p.Status.Phase != corev1.PodRunning {
			continue
		}
		for _, v := range p.Spec.Volumes {
			if v.PersistentVolumeClaim == nil || v.PersistentVolumeClaim.ClaimName != claim {
				continue
			}
			sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, _ = kexec.Run(sctx, k, namespace, p.Name, "", []string{"sync"})
			cancel()
			break
		}
	}
}
