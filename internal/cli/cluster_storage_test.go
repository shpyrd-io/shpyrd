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
