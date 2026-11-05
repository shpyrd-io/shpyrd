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
	// Limits and Sleep are the workspace's ceilings and sleep defaults
	// (WorkspaceSettings); both nil: the defaults of the platform that
	// creates it, none for the operator's own.
	Limits *store.Limits        `json:"limits,omitempty"`
	Sleep  *store.SleepDefaults `json:"sleep,omitempty"`
	// Origin says what makes the workspace (store.OriginAgent, ...;
	// store.OriginOperator when empty) and CreatedBy who: an agent's name,
	// a token, an email. ExpiresIn makes it a workspace waiting to be
	// claimed (#274): a duration ("24h", "7d") after which, unclaimed, it
	// is suspended and deleted. A sign-in claims it, so does
	// POST /api/workspaces/:slug/claim.
	Origin    string `json:"origin,omitempty"`
	CreatedBy string `json:"createdBy,omitempty"`
	ExpiresIn string `json:"expiresIn,omitempty"`
}

// ProtectWorkspaceRequest is PUT /api/workspaces/:slug/protected: a hold
// no policy crosses (#274).
type ProtectWorkspaceRequest struct {
	Protected bool `json:"protected"`
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
	Name *string `json:"name,omitempty"`
	// Address moves the workspace: a label (<label>.<workspaces domain>)
	// or a host. The old address redirects for RedirectDays (30 when
	// unset), its apps' hosts too.
	Address      *string `json:"address,omitempty"`
	RedirectDays *int    `json:"redirectDays,omitempty"`
	Status       *string `json:"status,omitempty"` // active or suspended
}

// WorkspaceSummary is one workspace as the console lists them.
type WorkspaceSummary struct {
	Capabilities     WorkspaceCapabilities `json:"capabilities"`
	InternalExposure *bool                 `json:"internalExposure"`
	// ID is the workspace's id, the wsid of the JWTs it serves.
	ID      string `json:"id"`
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Address string `json:"address,omitempty"`
	URL     string `json:"url"`
	Status  string `json:"status"`
	// Owner is "operator" or "customer" (RFC-0078).
	Owner string `json:"owner,omitempty"`
	// Limits are the workspace's ceilings, nil for none; Sleep its default
	// sleep, nil for none.
	Limits *store.Limits        `json:"limits,omitempty"`
	Sleep  *store.SleepDefaults `json:"sleep,omitempty"`
	Usage  *Usage               `json:"usage,omitempty"`
	Owners []string             `json:"owners"`
	// Readiness is the controller's last look at the workspace's door
	// (nil before the first), ReadyAt when it first answered (nil until
	// then): the moment an invitation link to it can be followed.
	Readiness *store.WorkspaceReadiness `json:"readiness,omitempty"`
	ReadyAt   *time.Time                `json:"readyAt,omitempty"`
	CreatedAt time.Time                 `json:"createdAt"`
	// The lifecycle (#274): why and since when it is suspended, the date
	// it is deleted and why, when an unclaimed one expires, what made it,
	// whether policies leave it alone, when it was last used, and, on a
	// tombstone, when it was deleted and how the cascade went.
	SuspendedAt    *time.Time               `json:"suspendedAt,omitempty"`
	SuspendReason  string                   `json:"suspendReason,omitempty"`
	DeleteAfter    *time.Time               `json:"deleteAfter,omitempty"`
	DeleteReason   string                   `json:"deleteReason,omitempty"`
	ExpiresAt      *time.Time               `json:"expiresAt,omitempty"`
	Origin         string                   `json:"origin,omitempty"`
	CreatedBy      string                   `json:"createdBy,omitempty"`
	Protected      bool                     `json:"protected,omitempty"`
	LastActivityAt *time.Time               `json:"lastActivityAt,omitempty"`
	DeletedAt      *time.Time               `json:"deletedAt,omitempty"`
	Deletion       *store.WorkspaceDeletion `json:"deletion,omitempty"`
}
