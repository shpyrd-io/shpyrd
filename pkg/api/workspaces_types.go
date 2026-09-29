package api

import (
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Payloads of the console's workspace routes (RFC-0033 phase 8). The
// open-source platform has one workspace and serves none of these routes;
// a server that offers the "workspaces" capability mounts them at
// /api/workspaces, and the CLI (`shpyrd-ctl workspaces ...`) speaks them.
// The types live here so both sides agree.

// CreateWorkspaceRequest is POST /api/workspaces.
type CreateWorkspaceRequest struct {
	Slug string `json:"slug" binding:"required"`
	Name string `json:"name"`
	// Address is the host of the workspace's dashboard; its apps live one
	// label under it. Empty: <slug>.<the platform's workspaces domain>.
	Address string `json:"address,omitempty"`
	// Owner is the email of the first owner of the workspace: a workspace
	// is enforced from birth, so it must have one — unless it is the
	// operator's (OperatorOwned), which every platform admin owns.
	Owner string `json:"owner,omitempty"`
	// OperatorOwned marks one of the platform operator's own workspaces
	// (RFC-0078, RFC-0080): its costs are the operator's, it is never
	// invoiced, platform admins own it and see the way to the console.
	OperatorOwned bool `json:"operatorOwned,omitempty"`
	// Plan names the billing plan (RFC-0075) the workspace is metered
	// against from birth. Empty: none yet. Refused for the operator's own
	// workspaces, which are never invoiced.
	Plan string `json:"plan,omitempty"`
	// Limits sets the workspace's ceilings; nil means none.
	Limits *store.Limits `json:"limits,omitempty"`
}

// UpdateWorkspaceRequest is PATCH /api/workspaces/:slug; every field is
// optional.
type UpdateWorkspaceRequest struct {
	Name    *string       `json:"name,omitempty"`
	Address *string       `json:"address,omitempty"`
	Status  *string       `json:"status,omitempty"` // active or suspended
	Limits  *store.Limits `json:"limits,omitempty"`
	// ClearLimits removes the ceilings.
	ClearLimits bool `json:"clearLimits,omitempty"`
}

// WorkspaceSummary is one workspace as the console lists them.
type WorkspaceSummary struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Address string `json:"address,omitempty"`
	URL     string `json:"url"`
	Status  string `json:"status"`
	// Owner is "operator" or "customer" (RFC-0078).
	Owner string `json:"owner,omitempty"`
	// Plan is the billing plan the workspace is metered against (RFC-0075);
	// empty when none, always empty for the operator's own.
	Plan      string        `json:"plan,omitempty"`
	Limits    *store.Limits `json:"limits,omitempty"`
	Usage     *Usage        `json:"usage,omitempty"`
	Owners    []string      `json:"owners"`
	CreatedAt time.Time     `json:"createdAt"`
}
