package cli

import (
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/install"
)

func TestPlatformBackupsUseGatewayWithoutExplicitBackupTarget(t *testing.T) {
	prof := &install.Profile{Runlevels: []install.Runlevel{{Name: "rc4", Components: []string{"platform-backup"}}}}
	for _, tt := range []struct {
		name string
		vars map[string]string
		skip bool
	}{
		{"no target", nil, true},
		{"explicit target", map[string]string{install.VarBackupTarget: "s3://backups/platform"}, false},
		{"gateway", map[string]string{install.VarGatewayBucket: "shared-objects"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := contains(conditionalComponents(tt.vars, prof), "platform-backup"); got != tt.skip {
				t.Fatalf("platform-backup skipped=%v, want %v", got, tt.skip)
			}
		})
	}
}

// The nightly disk snapshots need a snapshot class: without one (the local
// profile) the job is left out, with one it is installed.
func TestDiskSnapshotsNeedASnapshotClass(t *testing.T) {
	prof := &install.Profile{Runlevels: []install.Runlevel{{Name: "rc4", Components: []string{"platform-snapshots"}}}}
	if !contains(conditionalComponents(map[string]string{}, prof), "platform-snapshots") {
		t.Error("without a snapshot class the job must be skipped")
	}
	if contains(conditionalComponents(map[string]string{install.VarSnapshotClass: "oci-bv-backup"}, prof), "platform-snapshots") {
		t.Error("with a snapshot class the job must be installed")
	}
}
