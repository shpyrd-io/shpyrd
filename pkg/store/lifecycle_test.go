package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The workspace lifecycle (#274), the same on every store: suspended for a
// reason, scheduled, activated back, deleted into a tombstone that keeps
// the names and the ledger, scrubbed so the names are free.
func TestWorkspaceLifecycle(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			exp := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
			w, err := s.CreateWorkspace(ctx, Workspace{Slug: "acme", Name: "Acme", Address: "acme.example.test", Origin: OriginAgent, CreatedBy: "agent:planner", ExpiresAt: &exp})
			if err != nil {
				t.Fatal(err)
			}
			if w.Origin != OriginAgent || w.CreatedBy != "agent:planner" || w.ExpiresAt == nil || !w.ExpiresAt.Equal(exp) {
				t.Errorf("created = origin %q by %q expires %v", w.Origin, w.CreatedBy, w.ExpiresAt)
			}
			for email, role := range map[string]string{"owner@acme.test": WorkspaceRoleOwner, "dev@acme.test": WorkspaceRoleMember} {
				if _, err := s.TouchIdentity(ctx, "acme", Identity{Email: email, Issuer: "https://idp.test", Subject: email, Connector: "company"}); err != nil {
					t.Fatal(err)
				}
				if _, err := s.PutMembership(ctx, "acme", email, role); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.PutWorkspaceHost(ctx, "acme", WorkspaceHost{Host: "www.acme.test", Kind: HostCustom}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.UpsertProject(ctx, Project{ID: newID(), WorkspaceID: w.ID, Slug: "shop"}); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Truncate(5 * time.Minute)
			qty := 42.0
			if err := s.WriteBuckets(ctx, []UsageBucket{{WorkspaceID: w.ID, Project: "shop", Component: "web", Metric: MetricCPUUsed, PeriodStart: now, PeriodEnd: now.Add(5 * time.Minute), Quantity: &qty, Unit: UnitCoreSeconds, Quality: QualityComplete, Revision: 1, Source: "test"}}); err != nil {
				t.Fatal(err)
			}

			// Claiming clears the expiry; a hold is a flag.
			if w, err = s.SetWorkspaceExpiry(ctx, "acme", nil); err != nil || w.ExpiresAt != nil {
				t.Errorf("claim: %v %v", w.ExpiresAt, err)
			}
			if w, err = s.SetWorkspaceProtected(ctx, "acme", true); err != nil || !w.Protected {
				t.Errorf("protect: %v", err)
			}

			// A deletion is scheduled on a suspended workspace only.
			if _, err := s.ScheduleWorkspaceDeletion(ctx, "acme", time.Now(), DeleteReasonOperator); !errors.Is(err, ErrConflict) {
				t.Errorf("schedule on an active workspace = %v, want ErrConflict", err)
			}
			if _, err := s.ScheduleWorkspaceDeletion(ctx, "nope", time.Now(), DeleteReasonOperator); !errors.Is(err, ErrNotFound) {
				t.Errorf("schedule on no workspace = %v, want ErrNotFound", err)
			}
			w, err = s.SuspendWorkspace(ctx, "acme", SuspendReasonDunning)
			if err != nil || w.Status != WorkspaceSuspended || w.SuspendedAt == nil || w.SuspendReason != SuspendReasonDunning {
				t.Fatalf("suspend = %+v %v", w, err)
			}
			first := *w.SuspendedAt
			// Suspending again keeps the time; an empty reason keeps the reason.
			w, err = s.SuspendWorkspace(ctx, "acme", "")
			if err != nil || !w.SuspendedAt.Equal(first) || w.SuspendReason != SuspendReasonDunning {
				t.Errorf("suspend twice = %v %q %v", w.SuspendedAt, w.SuspendReason, err)
			}
			// The old way of suspending records the operator.
			if w, err = s.SetWorkspaceStatus(ctx, "acme", WorkspaceSuspended); err != nil || w.SuspendReason != SuspendReasonOperator {
				t.Errorf("SetWorkspaceStatus(suspended) reason = %q %v", w.SuspendReason, err)
			}
			when := time.Now().Add(7 * 24 * time.Hour).UTC().Truncate(time.Second)
			w, err = s.ScheduleWorkspaceDeletion(ctx, "acme", when, DeleteReasonDunning)
			if err != nil || w.DeleteAfter == nil || !w.DeleteAfter.Equal(when) || w.DeleteReason != DeleteReasonDunning {
				t.Fatalf("schedule = %v %q %v", w.DeleteAfter, w.DeleteReason, err)
			}
			// Activating cancels everything.
			w, err = s.ActivateWorkspace(ctx, "acme")
			if err != nil || w.Status != WorkspaceActive || w.SuspendedAt != nil || w.SuspendReason != "" || w.DeleteAfter != nil || w.DeleteReason != "" {
				t.Fatalf("activate = %+v %v", w, err)
			}

			// Activity: the people seen and the time the caller brings, never
			// backwards.
			seen := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
			if w, err = s.RefreshWorkspaceActivity(ctx, "acme", seen); err != nil {
				t.Fatalf("activity: %v", err)
			}
			if w.LastActivityAt == nil || w.LastActivityAt.Before(seen) {
				t.Fatalf("activity = %v (seen %v)", w.LastActivityAt, seen)
			}
			latest := *w.LastActivityAt
			if w, err = s.RefreshWorkspaceActivity(ctx, "acme", seen.Add(-48*time.Hour)); err != nil || w.LastActivityAt.Before(latest) {
				t.Errorf("activity moved backwards: %v %v", w.LastActivityAt, err)
			}

			// The way down: suspended, scheduled, deleting, deleted.
			if _, err := s.SuspendWorkspace(ctx, "acme", SuspendReasonOperator); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ScheduleWorkspaceDeletion(ctx, "acme", time.Now(), DeleteReasonOperator); err != nil {
				t.Fatal(err)
			}
			w, err = s.BeginWorkspaceDeletion(ctx, "acme")
			if err != nil || w.Status != WorkspaceDeleting || w.Deletion == nil || w.Deletion.StartedAt.IsZero() {
				t.Fatalf("begin = %+v %v", w, err)
			}
			if _, err := s.BeginWorkspaceDeletion(ctx, "acme"); err != nil {
				t.Errorf("begin twice = %v", err)
			}
			// While deleting, nothing brings it back or changes its dates.
			if _, err := s.ActivateWorkspace(ctx, "acme"); !errors.Is(err, ErrConflict) {
				t.Errorf("activate while deleting = %v, want ErrConflict", err)
			}
			if _, err := s.SuspendWorkspace(ctx, "acme", SuspendReasonOperator); !errors.Is(err, ErrConflict) {
				t.Errorf("suspend while deleting = %v, want ErrConflict", err)
			}
			d := *w.Deletion
			step := d.Step("projects")
			step.Done, step.Detail = true, "2 namespaces gone"
			if w, err = s.SetWorkspaceDeletion(ctx, "acme", d); err != nil || len(w.Deletion.Steps) != 1 || !w.Deletion.Steps[0].Done {
				t.Errorf("record step = %+v %v", w.Deletion, err)
			}
			// A deleting workspace is still listed: the reaper needs it.
			if all, _ := s.ListWorkspaces(ctx); len(all) != 2 {
				t.Errorf("ListWorkspaces while deleting = %d, want 2", len(all))
			}

			w, err = s.TombstoneWorkspace(ctx, "acme")
			if err != nil || w.Status != WorkspaceDeleted || w.DeletedAt == nil || w.Deletion.FinishedAt == nil {
				t.Fatalf("tombstone = %+v %v", w, err)
			}
			if len(w.TombstoneOwners) != 1 || w.TombstoneOwners[0] != "owner@acme.test" {
				t.Errorf("tombstone owners = %v", w.TombstoneOwners)
			}
			if w.Slug != "acme" || w.Address != "acme.example.test" || w.CreationFingerprint != "" {
				t.Errorf("tombstone keeps slug and address, drops the fingerprint: %+v", w)
			}
			if _, err := s.TombstoneWorkspace(ctx, "acme"); err != nil {
				t.Errorf("tombstone twice = %v", err)
			}
			// The rows are gone, the ledger and the hosts stay.
			if ms, err := s.ListMemberships(ctx, "acme"); err != nil || len(ms) != 0 {
				t.Errorf("memberships after = %v %v", ms, err)
			}
			if ids, err := s.ListIdentities(ctx, "acme"); err != nil || len(ids) != 0 {
				t.Errorf("identities after = %v %v", ids, err)
			}
			if ts, err := s.ListTeams(ctx, "acme"); err != nil || len(ts) != 0 {
				t.Errorf("teams after = %v %v", ts, err)
			}
			if ps, err := s.ListProjects(ctx, "acme", false); err != nil || len(ps) != 0 {
				t.Errorf("live projects after = %v %v", ps, err)
			}
			if ps, err := s.ListProjects(ctx, "acme", true); err != nil || len(ps) != 1 || ps[0].DeletedAt == nil {
				t.Errorf("deleted projects after = %v %v", ps, err)
			}
			if got, err := s.QueryBuckets(ctx, w.ID, "", now.Add(-time.Minute), now.Add(10*time.Minute)); err != nil || len(got) != 1 {
				t.Errorf("usage after = %v %v (the ledger stays)", got, err)
			}
			if hs, err := s.ListWorkspaceHosts(ctx, "acme"); err != nil || len(hs) != 1 {
				t.Errorf("hosts after = %v %v (held until the scrub)", hs, err)
			}
			// Hidden from the list, found by name, in the tombstones.
			if all, _ := s.ListWorkspaces(ctx); len(all) != 1 {
				t.Errorf("ListWorkspaces after = %d, want the default only", len(all))
			}
			if got, err := s.Workspace(ctx, "acme"); err != nil || got.Status != WorkspaceDeleted {
				t.Errorf("Workspace(acme) after = %+v %v", got, err)
			}
			if dead, err := s.ListDeletedWorkspaces(ctx); err != nil || len(dead) != 1 || dead[0].Slug != "acme" {
				t.Errorf("tombstones = %v %v", dead, err)
			}
			// The names are taken.
			if _, err := s.CreateWorkspace(ctx, Workspace{Slug: "acme", Name: "Squatter"}); !errors.Is(err, ErrConflict) {
				t.Errorf("create with a held slug = %v, want ErrConflict", err)
			}
			if _, err := s.CreateWorkspace(ctx, Workspace{Slug: "other", Name: "Squatter", Address: "acme.example.test"}); !errors.Is(err, ErrConflict) {
				t.Errorf("create with a held address = %v, want ErrConflict", err)
			}
			if _, err := s.CreateWorkspace(ctx, Workspace{Slug: "other", Name: "Squatter", Address: "www.acme.test"}); !errors.Is(err, ErrConflict) {
				t.Errorf("create with a held custom domain = %v, want ErrConflict", err)
			}
			// Nothing of the lifecycle applies to a tombstone.
			if _, err := s.ActivateWorkspace(ctx, "acme"); !errors.Is(err, ErrConflict) {
				t.Errorf("activate a tombstone = %v", err)
			}
			if _, err := s.BeginWorkspaceDeletion(ctx, "acme"); !errors.Is(err, ErrConflict) {
				t.Errorf("begin on a tombstone = %v", err)
			}

			// The scrub frees the names.
			if _, err := s.ScrubWorkspace(ctx, DefaultWorkspace); !errors.Is(err, ErrConflict) {
				t.Errorf("scrub a live workspace = %v, want ErrConflict", err)
			}
			w, err = s.ScrubWorkspace(ctx, "acme")
			if err != nil || w.Slug != ScrubbedSlug(w.ID) || w.Address != "" || len(w.TombstoneOwners) != 0 {
				t.Fatalf("scrub = %+v %v", w, err)
			}
			if hs, _ := s.ListWorkspaceHosts(ctx, w.ID); len(hs) != 0 {
				t.Errorf("hosts after the scrub = %v", hs)
			}
			if _, err := s.Workspace(ctx, "acme"); !errors.Is(err, ErrNotFound) {
				t.Errorf("Workspace(acme) after the scrub = %v, want ErrNotFound", err)
			}
			if dead, _ := s.ListDeletedWorkspaces(ctx); len(dead) != 1 || dead[0].ID != w.ID {
				t.Errorf("tombstones after the scrub = %v", dead)
			}
			again, err := s.CreateWorkspace(ctx, Workspace{Slug: "acme", Name: "Acme again", Address: "acme.example.test"})
			if err != nil || again.ID == w.ID {
				t.Fatalf("create after the scrub = %+v %v", again, err)
			}
			if _, err := s.PutWorkspaceHost(ctx, "acme", WorkspaceHost{Host: "www.acme.test", Kind: HostCustom}); err != nil {
				t.Errorf("the custom domain after the scrub: %v", err)
			}
			// The ledger still names the old one by id.
			if got, err := s.QueryBuckets(ctx, w.ID, "", now.Add(-time.Minute), now.Add(10*time.Minute)); err != nil || len(got) != 1 {
				t.Errorf("usage of the scrubbed workspace = %v %v", got, err)
			}
		})
	}
}
