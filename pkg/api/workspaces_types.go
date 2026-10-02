package api

import (
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
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
	// Limits gives the workspace ceilings of its own, an exception to its
	// plan's (#51); nil: it follows its plan's, or has none without one.
	Limits *store.Limits `json:"limits,omitempty"`
}

// CreatedWorkspace is what POST /api/workspaces answers: the workspace,
// and what became of its first owner's invitation (RFC-0033, RFC-0014):
// emailed, or a link to pass on, shown once.
type CreatedWorkspace struct {
	WorkspaceSummary
	OwnerInvitation *ext.InviteOutcome `json:"ownerInvitation,omitempty"`
	// OwnerInvitationPending says the invitation was not sent yet: it goes
	// out by email once the workspace's door answers, so the link in it
	// leads somewhere. (When mail is not configured the link is made at
	// once and returned above, for the operator to pass on.)
	OwnerInvitationPending bool `json:"ownerInvitationPending,omitempty"`
}

// UpdateWorkspaceRequest is PATCH /api/workspaces/:slug; every field is
// optional.
type UpdateWorkspaceRequest struct {
	Name    *string `json:"name,omitempty"`
	Address *string `json:"address,omitempty"`
	Status  *string `json:"status,omitempty"` // active or suspended
	// Limits sets ceilings of the workspace's own over those in force
	// (those not given keep their value), an exception to its plan's.
	Limits *store.Limits `json:"limits,omitempty"`
	// ClearLimits removes the workspace's own ceilings: it follows its
	// plan's again (none without a plan).
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
	Plan string `json:"plan,omitempty"`
	// Limits are the ceilings in force: the workspace's own when
	// LimitsOverride, else its plan's (#51); nil for none.
	Limits         *store.Limits `json:"limits,omitempty"`
	LimitsOverride bool          `json:"limitsOverride,omitempty"`
	Usage          *Usage        `json:"usage,omitempty"`
	Owners         []string      `json:"owners"`
	// Readiness is the controller's last look at the workspace's door
	// (nil before the first), ReadyAt when it first answered (nil until
	// then): the moment an invitation link to it can be followed.
	Readiness *store.WorkspaceReadiness `json:"readiness,omitempty"`
	ReadyAt   *time.Time                `json:"readyAt,omitempty"`
	CreatedAt time.Time                 `json:"createdAt"`
}
