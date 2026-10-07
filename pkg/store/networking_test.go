package store

import (
	"k8s.io/utils/ptr"
	"testing"
)

func TestNetworkingSurvivesStaleOrdinarySettingsWrites(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			s := open(t)
			ctx := t.Context()
			stale, err := s.Workspace(ctx, DefaultWorkspace)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.SetWorkspaceInternalExposure(ctx, DefaultWorkspace, ptr.To(true)); err != nil {
				t.Fatal(err)
			}
			stale.Settings.JoinPolicy = JoinListed
			// Even an explicit attempt through ordinary settings cannot revoke networking.
			stale.Settings.InternalExposure = ptr.To(false)
			w, err := s.UpdateWorkspaceSettings(ctx, DefaultWorkspace, stale.Settings)
			if err != nil {
				t.Fatal(err)
			}
			if w.Settings.InternalExposure == nil || !*w.Settings.InternalExposure || w.Settings.JoinPolicy != JoinListed {
				t.Fatalf("settings: %+v", w.Settings)
			}
			w, err = s.SetWorkspaceInternalExposure(ctx, DefaultWorkspace, nil)
			if err != nil {
				t.Fatal(err)
			}
			if w.Settings.InternalExposure != nil || w.Settings.JoinPolicy != JoinListed {
				t.Fatalf("reset: %+v", w.Settings)
			}
			// A stale write also cannot restore a removed override.
			stale.Settings.InternalExposure = ptr.To(true)
			w, err = s.UpdateWorkspaceSettings(ctx, DefaultWorkspace, stale.Settings)
			if err != nil || w.Settings.InternalExposure != nil {
				t.Fatalf("stale override restored: %+v %v", w, err)
			}
		})
	}
}
