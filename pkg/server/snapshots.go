package server

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/snapshots"
)

// runSnapshots is `shpyrd-server snapshots`: the nightly provider snapshots
// of the block disks (RFC-0060, the storage plan). Every claim of the
// profile's block class in the projects' namespaces and the platform's own
// gets a snapshot; the newest SHPYRD_SNAPSHOT_KEEP stay per disk. The
// platform-snapshots CronJob runs it where the profile names a snapshot
// class; `shpyrd-ctl cluster snapshots take` is the same by hand.
func runSnapshots(logger *slog.Logger) error {
	class, snapshotClass := os.Getenv(install.VarStorageClass), os.Getenv(install.VarSnapshotClass)
	if class == "" || snapshotClass == "" {
		return errors.New("SHPYRD_STORAGE_CLASS and SHPYRD_SNAPSHOT_CLASS name the disks to snapshot and the class to snapshot them with")
	}
	keep := 7
	if raw := os.Getenv("SHPYRD_SNAPSHOT_KEEP"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return errors.New("SHPYRD_SNAPSHOT_KEEP must be a number of snapshots to keep per disk, at least 1")
		}
		keep = n
	}
	k, err := kube.Connect(kube.Options{})
	if err != nil {
		return err
	}
	c, err := k.ControllerClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	opts := snapshots.Options{Class: class, SnapshotClass: snapshotClass, System: true, Keep: keep, Wait: 10 * time.Minute}
	if k.Config != nil {
		opts.Sync = func(ctx context.Context, namespace, claim string) { snapshots.Flush(ctx, k, namespace, claim) }
	}
	results, err := snapshots.Take(ctx, os.Stdout, c, opts)
	logger.Info("disk snapshots", "class", class, "disks", len(results), "keep", keep)
	return err
}
