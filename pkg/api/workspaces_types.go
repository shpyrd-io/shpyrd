package api

import (
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Payloads of the console's workspace routes (RFC-0033 phase 8). The
// open-source platform has one implicit workspace and serves none of these
// routes; a server that offers the "workspaces" capability mounts them at
// /api/workspaces, and the CLI (`shpyrd-ctl workspaces ...`) speaks them.
// The types live here so both sides agree.

// CreateWorkspaceRequest is POST /api/workspaces.
type CreateWorkspaceRequest struct {
	Slug string `json:"slug" binding:"required"`
	Name string `json:"name"`
	// Address is the host of the workspace's dashboard; its apps live one
	// label under it. Empty: <slug>.<the platform's workspaces domain>.
	Address string `json:"address,omitempty"`
	// Owner is the email of the first platform admin of the workspace: an
	// explicit workspace is enforced from birth, so it must have one.
	Owner string `json:"owner" binding:"required"`
	// Plan sets the workspace's ceilings; nil means none.
	Plan *store.Limits `json:"plan,omitempty"`
}

// UpdateWorkspaceRequest is PATCH /api/workspaces/:slug; every field is
// optional.
type UpdateWorkspaceRequest struct {
	Name    *string       `json:"name,omitempty"`
	Address *string       `json:"address,omitempty"`
	Status  *string       `json:"status,omitempty"` // active or suspended
	Plan    *store.Limits `json:"plan,omitempty"`
	// ClearPlan removes the ceilings.
	ClearPlan bool `json:"clearPlan,omitempty"`
}

// WorkspaceSummary is one workspace as the console lists them.
type WorkspaceSummary struct {
	Slug      string        `json:"slug"`
	Name      string        `json:"name"`
	Address   string        `json:"address,omitempty"`
	URL       string        `json:"url"`
	Status    string        `json:"status"`
	Implicit  bool          `json:"implicit"`
	Plan      *store.Limits `json:"plan,omitempty"`
	Usage     *Usage        `json:"usage,omitempty"`
	Owners    []string      `json:"owners"`
	CreatedAt time.Time     `json:"createdAt"`
}
