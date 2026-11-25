package v1alpha1

import "k8s.io/apimachinery/pkg/runtime/schema"

// Roles (RFC-0008). Project roles are granted per project; platform roles
// apply to the whole cluster.
const (
	// RoleReader opens the app read-only (the edge refuses requests that
	// change things and tells the app "reader"), so an app needs no
	// permission code of its own to have viewers (RFC-0033).
	RoleReader = "reader"
	// RoleUser opens the app (the edge, RFC-0033) and nothing in the
	// builder dashboard beyond the launcher.
	RoleUser      = "user"
	RoleViewer    = "viewer"
	RoleDeveloper = "developer"
	RoleAdmin     = "admin"

	RolePlatformAdmin  = "platform-admin"
	RolePlatformViewer = "platform-viewer"
)

// Teams and project members were Kubernetes objects (Team, ProjectMember)
// before RFC-0033 moved them to the control-plane database. The types are
// gone; what remains is what the move needs on clusters that still hold
// them (#249): the server imports each object and marks it, and the
// installer removes the definitions once every object is marked.
var (
	LegacyTeams          = schema.GroupVersionResource{Group: GroupVersion.Group, Version: GroupVersion.Version, Resource: "teams"}
	LegacyProjectMembers = schema.GroupVersionResource{Group: GroupVersion.Group, Version: GroupVersion.Version, Resource: "projectmembers"}
)

// AnnotationMigrated marks a legacy Team or ProjectMember the server has
// dealt with: a Unix time when it was imported or the database already had
// it, "skipped: <why>" when it could not be (a grant to a team that is gone).
// The installer removes the definitions only when every object carries it.
const AnnotationMigrated = "shpyrd.io/migrated"
