package authz

import (
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

func team(name string, members, groups []string, platform string) store.Team {
	return store.Team{Name: name, Members: members, Groups: groups, PlatformRole: platform}
}

func member(project, role, user, teamName string) store.Grant {
	return store.Grant{Project: project, Role: role, User: user, Team: teamName}
}

func TestBootstrapAndToken(t *testing.T) {
	empty := &Snapshot{}
	r := empty.RolesFor(ext.Identity{Email: "ada@example.test", Provider: "local"})
	if r.Enforced || r.Platform != shpyrdv1.RolePlatformAdmin || !r.Can(ClusterAdmin, "") || !r.Can(ProjectDestroy, "x") {
		t.Errorf("without memberships everyone is a platform admin: %+v", r)
	}
	enforced := &Snapshot{Teams: []store.Team{team("ops", []string{"ops@example.test"}, nil, shpyrdv1.RolePlatformAdmin)}}
	tok := enforced.RolesFor(ext.Identity{Subject: "admin-token", Provider: "token"})
	if tok.Platform != shpyrdv1.RolePlatformAdmin || !tok.Enforced {
		t.Errorf("token must stay platform admin: %+v", tok)
	}
	nobody := enforced.RolesFor(ext.Identity{Email: "new@example.test", Provider: "local"})
	if nobody.Platform != "" || nobody.Can(ProjectView, "hello") || nobody.Can(ClusterView, "") {
		t.Errorf("unknown users get nothing once enforced: %+v", nobody)
	}
}

func TestRolesResolution(t *testing.T) {
	snap := &Snapshot{
		Teams: []store.Team{
			team("ops", []string{"Ops@Example.test"}, nil, shpyrdv1.RolePlatformAdmin),
			team("auditors", nil, []string{"security"}, shpyrdv1.RolePlatformViewer),
			team("web", []string{"dev@example.test"}, []string{"engineering"}, ""),
		},
		Grants: []store.Grant{
			member("shop", shpyrdv1.RoleDeveloper, "", "web"),
			member("shop", shpyrdv1.RoleViewer, "dev@example.test", ""), // weaker grant does not downgrade
			member("blog", shpyrdv1.RoleAdmin, "dev@example.test", ""),
			member("shop", shpyrdv1.RoleViewer, "guest@example.test", ""),
		},
	}
	dev := snap.RolesFor(ext.Identity{Email: "dev@example.test", Provider: "local"})
	if dev.Platform != "" || dev.Projects["shop"] != shpyrdv1.RoleDeveloper || dev.Projects["blog"] != shpyrdv1.RoleAdmin {
		t.Errorf("dev = %+v", dev)
	}
	if !dev.Can(ProjectDeploy, "shop") || dev.Can(ProjectDestroy, "shop") || !dev.Can(ProjectDestroy, "blog") || dev.Can(ClusterView, "") || dev.Can(ProjectView, "other") {
		t.Errorf("dev permissions wrong: %+v", dev)
	}
	if dev.ProjectRole("shop") != shpyrdv1.RoleDeveloper || dev.ProjectRole("other") != "" {
		t.Errorf("project roles: %s %s", dev.ProjectRole("shop"), dev.ProjectRole("other"))
	}

	byGroup := snap.RolesFor(ext.Identity{Email: "someone@example.test", Groups: []string{"engineering"}, Provider: "okta"})
	if byGroup.Projects["shop"] != shpyrdv1.RoleDeveloper {
		t.Errorf("group membership should grant the team's role: %+v", byGroup)
	}

	ops := snap.RolesFor(ext.Identity{Email: "ops@example.test", Provider: "local"})
	if ops.Platform != shpyrdv1.RolePlatformAdmin || !ops.Can(ProjectDestroy, "anything") || !ops.Can(ClusterAdmin, "") || ops.ProjectRole("shop") != shpyrdv1.RoleAdmin {
		t.Errorf("ops = %+v", ops)
	}
	auditor := snap.RolesFor(ext.Identity{Email: "sec@example.test", Groups: []string{"security"}, Provider: "okta"})
	if auditor.Platform != shpyrdv1.RolePlatformViewer || !auditor.Can(ProjectView, "shop") || auditor.Can(ProjectDeploy, "shop") || !auditor.Can(ClusterView, "") || auditor.Can(ClusterAdmin, "") {
		t.Errorf("auditor = %+v", auditor)
	}
	if auditor.ProjectRole("shop") != shpyrdv1.RoleViewer {
		t.Errorf("platform viewer is a viewer everywhere, got %q", auditor.ProjectRole("shop"))
	}
	guest := snap.RolesFor(ext.Identity{Email: "guest@example.test", Provider: "local"})
	if !guest.Can(ProjectView, "shop") || guest.Can(ProjectConfig, "shop") || guest.Can(ProjectExec, "shop") {
		t.Errorf("viewer = %+v", guest)
	}
	if ProjectFromNamespace("app-shop") != "shop" {
		t.Error("namespace mapping")
	}
}

func TestEveryoneAndSuspended(t *testing.T) {
	snap := &Snapshot{
		Teams:     []store.Team{{Name: store.TeamEveryone, Everyone: true}, team("ops", []string{"ops@example.test"}, nil, shpyrdv1.RolePlatformAdmin)},
		Grants:    []store.Grant{member("intranet", shpyrdv1.RoleUser, "", store.TeamEveryone)},
		Suspended: map[string]bool{"gone@example.test": true},
	}
	anyone := snap.RolesFor(ext.Identity{Email: "someone@example.test", Provider: "google"})
	if !anyone.Can(ProjectOpen, "intranet") || anyone.Can(ProjectView, "intranet") || anyone.ProjectRole("intranet") != shpyrdv1.RoleUser {
		t.Errorf("everyone grant: %+v", anyone)
	}
	if got := snap.TeamNames(ext.Identity{Email: "someone@example.test"}); len(got) != 1 || got[0] != store.TeamEveryone {
		t.Errorf("teams = %v", got)
	}
	gone := snap.RolesFor(ext.Identity{Email: "gone@example.test", Provider: "google"})
	if !gone.Suspended || gone.Can(ProjectOpen, "intranet") || gone.Platform != "" {
		t.Errorf("suspended = %+v", gone)
	}
	// The token is never suspended, and not a person for the everyone team.
	tok := snap.RolesFor(ext.Identity{Subject: "admin-token", Provider: "token"})
	if tok.Suspended || tok.Platform != shpyrdv1.RolePlatformAdmin {
		t.Errorf("token = %+v", tok)
	}
	if got := snap.TeamNames(ext.Identity{Subject: "admin-token", Provider: "token"}); len(got) != 0 {
		t.Errorf("token teams = %v", got)
	}
}

func TestWorkspaceRoles(t *testing.T) {
	membership := func(email, role string) store.Membership { return store.Membership{Email: email, Role: role} }
	person := func(email string) ext.Identity { return ext.Identity{Email: email, Provider: "local"} }

	// A workspace role alone ends bootstrap.
	snap := &Snapshot{Memberships: []store.Membership{membership("owner@example.test", store.WorkspaceRoleOwner)}}
	if !snap.Enforced() {
		t.Fatal("a membership must end bootstrap mode")
	}
	owner := snap.RolesFor(person("Owner@example.test"))
	if owner.Workspace != store.WorkspaceRoleOwner || owner.Platform != shpyrdv1.RolePlatformAdmin || !owner.Can(WorkspaceOwner, "") || !owner.Can(ClusterAdmin, "") || !owner.Can(ProjectDestroy, "any") {
		t.Errorf("owner: %+v", owner)
	}
	nobody := snap.RolesFor(person("new@example.test"))
	if nobody.Workspace != "" || nobody.Can(ClusterCreate, "") || nobody.Can(WorkspaceOwner, "") {
		t.Errorf("a person without a role has nothing: %+v", nobody)
	}

	snap.Memberships = append(snap.Memberships,
		membership("admin@example.test", store.WorkspaceRoleAdmin),
		membership("dev@example.test", store.WorkspaceRoleMember),
		membership("legacy@example.test", store.WorkspaceRoleMember),
	)
	snap.Teams = []store.Team{team("ops", []string{"legacy@example.test", "teamadmin@example.test"}, nil, shpyrdv1.RolePlatformAdmin)}
	snap.Grants = []store.Grant{member("blog", shpyrdv1.RoleAdmin, "dev@example.test", "")}

	admin := snap.RolesFor(person("admin@example.test"))
	if admin.Workspace != store.WorkspaceRoleAdmin || admin.Platform != shpyrdv1.RolePlatformAdmin || admin.Can(WorkspaceOwner, "") || !admin.Can(ClusterAdmin, "") {
		t.Errorf("admin administers but does not own: %+v", admin)
	}
	dev := snap.RolesFor(person("dev@example.test"))
	if dev.Workspace != store.WorkspaceRoleMember || dev.Platform != "" || !dev.Can(ClusterCreate, "") || dev.Can(ClusterAdmin, "") || dev.Can(ClusterView, "") ||
		!dev.Can(ProjectDestroy, "blog") || dev.Can(ProjectView, "other") || dev.ProjectRole("blog") != shpyrdv1.RoleAdmin {
		t.Errorf("member creates projects and keeps their grants: %+v", dev)
	}
	// A membership decides: the team's platform role no longer applies.
	legacy := snap.RolesFor(person("legacy@example.test"))
	if legacy.Platform != "" || legacy.Workspace != store.WorkspaceRoleMember || legacy.Can(ClusterAdmin, "") {
		t.Errorf("membership must override the team's platform role: %+v", legacy)
	}
	// Without one, the team's platform role still counts.
	viaTeam := snap.RolesFor(person("teamadmin@example.test"))
	if viaTeam.Platform != shpyrdv1.RolePlatformAdmin || viaTeam.Workspace != "" || viaTeam.Can(WorkspaceOwner, "") {
		t.Errorf("team platform role without membership: %+v", viaTeam)
	}
	// The operator holds the owner's actions.
	op := snap.RolesFor(ext.Identity{Subject: "admin-token", Provider: "token"})
	if !op.Can(WorkspaceOwner, "") || op.Workspace != store.WorkspaceRoleOwner {
		t.Errorf("operator: %+v", op)
	}
	// Suspension beats everything.
	snap.Suspended = map[string]bool{"owner@example.test": true}
	if r := snap.RolesFor(person("owner@example.test")); !r.Suspended || r.Can(WorkspaceOwner, "") || r.Can(ProjectView, "blog") {
		t.Errorf("suspended owner: %+v", r)
	}
	if got := snap.Owners(); len(got) != 1 || got[0] != "owner@example.test" {
		t.Errorf("owners = %v", got)
	}
	if snap.WorkspaceRole(" ADMIN@example.test ") != store.WorkspaceRoleAdmin || snap.WorkspaceRole("") != "" {
		t.Error("WorkspaceRole must normalise the email")
	}
}
