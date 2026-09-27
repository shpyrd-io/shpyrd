// Package store is the platform's control-plane database (RFC-0033): the
// workspace, the people it has seen, teams and their grants on projects.
// Workloads stay Kubernetes objects; what is about people and tenancy lives
// here, in Postgres on a real install and in memory in tests.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// DefaultWorkspace is the slug of the implicit workspace every open-source
// install has: names built from it carry no workspace part.
const DefaultWorkspace = "default"

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
)

// Workspace is the tenant. The OSS has exactly one, the implicit
// workspace, which answers at the platform domain. Explicit workspaces
// (RFC-0033 phase 6) answer at an Address of their own: the dashboard and
// sign-in at <Address>, apps at <app>.<Address>.
type Workspace struct {
	ID        string            `json:"id"`
	Slug      string            `json:"slug"`
	Name      string            `json:"name"`
	Address   string            `json:"address,omitempty"`
	Status    string            `json:"status"` // WorkspaceActive or WorkspaceSuspended
	Settings  WorkspaceSettings `json:"settings"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

// Workspace statuses.
const (
	WorkspaceActive    = "active"
	WorkspaceSuspended = "suspended" // answers nothing but the "suspended" page
)

// Implicit reports whether this is the one workspace every install has.
func (w *Workspace) Implicit() bool { return w.Slug == DefaultWorkspace }

// Join policies: who becomes a person on first sign-in.
const (
	JoinOpen    = "open"    // anyone who can sign in
	JoinCompany = "company" // only through the method of a claimed domain
	JoinListed  = "listed"  // only people already named in a team or a grant
)

// WorkspaceSettings are the knobs of the workspace.
type WorkspaceSettings struct {
	JoinPolicy string `json:"joinPolicy,omitempty"` // JoinOpen when empty
	// OwnMethodsOnly hides the platform's login methods from this
	// workspace's login page: only the methods the workspace configured
	// itself (its company SSO) are offered (RFC-0033). Never true for the
	// implicit workspace.
	OwnMethodsOnly bool `json:"ownMethodsOnly,omitempty"`
	// Limits is the workspace's plan (RFC-0033, RFC-0042): ceilings the
	// API checks before changing anything and the controller backs with a
	// ResourceQuota per project namespace. Nil means no ceiling, the
	// open-source default.
	Limits *Limits `json:"limits,omitempty"`
}

// Limits are the ceilings of a workspace plan. Zero values mean no ceiling
// on that axis. Quantities use Kubernetes notation ("4", "8Gi", "50Gi").
type Limits struct {
	Projects  int    `json:"projects,omitempty"`
	Instances int    `json:"instances,omitempty"`
	CPU       string `json:"cpu,omitempty"`
	Memory    string `json:"memory,omitempty"`
	Storage   string `json:"storage,omitempty"`
}

// APIToken is a scoped, named credential (RFC-0031): shp_<id>_<random>.
// The random part is shown once and not stored; Hash is SHA-256(random).
type APIToken struct {
	ID           string            `json:"id"`
	WorkspaceID  string            `json:"workspaceId"`
	Name         string            `json:"name"`
	OwnerEmail   string            `json:"ownerEmail"`
	PlatformRole string            `json:"platformRole,omitempty"`
	ProjectRoles map[string]string `json:"projectRoles,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
	ExpiresAt    *time.Time        `json:"expiresAt,omitempty"`
	LastUsedAt   *time.Time        `json:"lastUsedAt,omitempty"`
}

// Tokens is the token-management part of the Store.
type Tokens interface {
	// CreateToken stores a new token; caller provides the Hash of the random part.
	CreateToken(ctx context.Context, ws string, t APIToken, hash string) (*APIToken, error)
	// LookupToken finds a token by its hash; updates LastUsedAt at most once a
	// minute (best-effort); nil when not found, expired or revoked.
	LookupToken(ctx context.Context, hash string) (*APIToken, error)
	ListTokens(ctx context.Context, ws, ownerEmail string) ([]APIToken, error)
	DeleteToken(ctx context.Context, ws, id string) error
}

// DomainClaim says the workspace owns an email domain: once verified (a DNS
// TXT record carrying Token), accounts of that domain must sign in through
// Connector (when set) and count as the company's people.
type DomainClaim struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspaceId"`
	Domain      string     `json:"domain"`
	Token       string     `json:"token"`
	Connector   string     `json:"connector,omitempty"`
	VerifiedAt  *time.Time `json:"verifiedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// Identity is a person the platform has seen sign in: recorded at every
// sign-in so the workspace has a list of its people before RFC-0033's
// invitations and provisioning arrive.
type Identity struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	Realm       string    `json:"realm"` // workspace, console, operator
	Email       string    `json:"email"`
	Name        string    `json:"name,omitempty"`
	Provider    string    `json:"provider,omitempty"` // last login method
	Groups      []string  `json:"groups,omitempty"`   // last groups claim
	Status      string    `json:"status"`             // active, suspended
	FirstSeenAt time.Time `json:"firstSeenAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
}

// TeamEveryone is the built-in team every person who signed in belongs to:
// grant it a role and the whole company has it. It has no listed members,
// cannot be edited or deleted, and carries no platform role.
const TeamEveryone = "everyone"

// Team groups people by email or by identity-provider group; a team may
// carry a platform role (RFC-0008) until workspace roles replace it.
type Team struct {
	ID           string   `json:"id"`
	WorkspaceID  string   `json:"workspaceId"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Members      []string `json:"members"` // emails, lower case
	Groups       []string `json:"groups"`
	PlatformRole string   `json:"platformRole,omitempty"`
	// Everyone marks the built-in team (TeamEveryone).
	Everyone  bool      `json:"everyone,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Identity statuses.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

var ErrBuiltIn = errors.New("built-in")

// Workspace roles (RFC-0033): what a person is in the workspace, apart
// from what grants give them on projects. Owners and admins administer
// the workspace (people, teams, settings, every project); only owners name
// owners. Members may create projects (and administer the ones they
// create). A person without a membership has what grants give them.
const (
	WorkspaceRoleOwner  = "owner"
	WorkspaceRoleAdmin  = "admin"
	WorkspaceRoleMember = "member"
)

// ValidWorkspaceRole reports whether role is one of the workspace roles.
func ValidWorkspaceRole(role string) bool {
	switch role {
	case WorkspaceRoleOwner, WorkspaceRoleAdmin, WorkspaceRoleMember:
		return true
	}
	return false
}

// Membership is a person's workspace role, by email: it exists before the
// person has signed in (an invitation accepted, an owner named at
// creation) and stays when their sign-in record is forgotten.
type Membership struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	Email       string    `json:"email"` // lower case
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Invitation asks a person, by email, to join the workspace with a role
// (and, optionally, a team). The link carries a random token shown once;
// TokenHash is its SHA-256. Signing in with the invited email accepts it:
// the membership is written and the invitation goes.
type Invitation struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	Email       string    `json:"email"`
	Role        string    `json:"role"`
	Team        string    `json:"team,omitempty"` // team name
	InvitedBy   string    `json:"invitedBy,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// Expired reports whether the invitation can no longer be accepted.
func (i *Invitation) Expired(now time.Time) bool { return !now.Before(i.ExpiresAt) }

// WorkspaceHost is a host of a workspace besides its address (RFC-0033
// names): a custom domain the company owns (kind "custom", CNAME mode:
// the host and *.host point at the address; verified through a TXT
// record or the CNAME itself; primary when the dashboard and app URLs
// use it), or a previous address (kind "moved", redirecting to the
// current one until ExpiresAt).
type WorkspaceHost struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspaceId"`
	Host        string     `json:"host"`
	Kind        string     `json:"kind"`
	Primary     bool       `json:"primary,omitempty"`
	Token       string     `json:"token,omitempty"`
	VerifiedAt  *time.Time `json:"verifiedAt,omitempty"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// Kinds of WorkspaceHost.
const (
	HostCustom = "custom"
	HostMoved  = "moved"
)

// Hosts is the workspace-hosts part of the Store.
type Hosts interface {
	ListWorkspaceHosts(ctx context.Context, ws string) ([]WorkspaceHost, error)
	// PutWorkspaceHost creates a host (with a fresh token for a custom
	// one) or updates its Primary, VerifiedAt and ExpiresAt; ErrConflict
	// when another workspace has the host or an address equal to it.
	// Setting Primary clears it on the workspace's other hosts.
	PutWorkspaceHost(ctx context.Context, ws string, h WorkspaceHost) (*WorkspaceHost, error)
	DeleteWorkspaceHost(ctx context.Context, ws, host string) error
	// WorkspaceByHost finds the workspace owning a host record (exact
	// match, case-insensitive), returning the record too; ErrNotFound
	// otherwise.
	WorkspaceByHost(ctx context.Context, host string) (*Workspace, *WorkspaceHost, error)
	// UpdateWorkspaceAddress moves the workspace to a new address;
	// ErrConflict when a workspace has that address or a host equal to it.
	UpdateWorkspaceAddress(ctx context.Context, slug, address string) (*Workspace, error)
}

// Memberships is the workspace-role part of the Store.
type Memberships interface {
	ListMemberships(ctx context.Context, ws string) ([]Membership, error)
	// PutMembership sets a person's workspace role, creating the
	// membership when there is none; role must be a workspace role.
	PutMembership(ctx context.Context, ws, email, role string) (*Membership, error)
	// DeleteMembership removes a person's workspace role; ErrNotFound when
	// they had none.
	DeleteMembership(ctx context.Context, ws, email string) error

	// ListInvitations lists the workspace's invitations, expired ones
	// included (the caller says so), oldest first.
	ListInvitations(ctx context.Context, ws string) ([]Invitation, error)
	// CreateInvitation stores an invitation with the hash of its token,
	// replacing a pending one for the same email (a re-invite is a new
	// link); ErrNotFound when the team named does not exist.
	CreateInvitation(ctx context.Context, ws string, inv Invitation, tokenHash string) (*Invitation, error)
	// InvitationByToken finds an invitation by its token's hash, expired or
	// not; ErrNotFound otherwise.
	InvitationByToken(ctx context.Context, tokenHash string) (*Invitation, error)
	DeleteInvitation(ctx context.Context, ws, id string) error
}

// Grant gives a role on a project to a user (email) or to a team; exactly
// one of the two is set.
type Grant struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	Project     string    `json:"project"`
	Role        string    `json:"role"`
	User        string    `json:"user,omitempty"`
	Team        string    `json:"team,omitempty"` // team name
	CreatedAt   time.Time `json:"createdAt"`
}

// Store is what the server and the controllers need. Every method is
// scoped to a workspace by slug; the OSS passes DefaultWorkspace.
type Store interface {
	// Migrate brings the schema to the current version and ensures the
	// implicit workspace exists.
	Migrate(ctx context.Context, defaultName string) error
	Close()

	Workspace(ctx context.Context, slug string) (*Workspace, error)
	// WorkspaceByAddress finds the workspace answering at a host (exact
	// match on Address, case-insensitive); ErrNotFound otherwise.
	WorkspaceByAddress(ctx context.Context, address string) (*Workspace, error)
	ListWorkspaces(ctx context.Context) ([]Workspace, error)
	// CreateWorkspace adds an explicit workspace with its built-in team;
	// ErrConflict when the slug or the address is taken. The OSS server
	// never calls it: only the cloud layer creates workspaces.
	CreateWorkspace(ctx context.Context, w Workspace) (*Workspace, error)
	UpdateWorkspace(ctx context.Context, slug, name string) (*Workspace, error)
	UpdateWorkspaceSettings(ctx context.Context, slug string, settings WorkspaceSettings) (*Workspace, error)
	// SetWorkspaceStatus suspends or reactivates a workspace.
	SetWorkspaceStatus(ctx context.Context, slug, status string) (*Workspace, error)

	// Domain claims: PutDomainClaim creates one (with a fresh token) or
	// updates its connector; MarkDomainVerified records the DNS check.
	ListDomainClaims(ctx context.Context, ws string) ([]DomainClaim, error)
	PutDomainClaim(ctx context.Context, ws, domain, connector string) (*DomainClaim, error)
	MarkDomainVerified(ctx context.Context, ws, domain string, at time.Time) (*DomainClaim, error)
	DeleteDomainClaim(ctx context.Context, ws, domain string) error

	// TouchIdentity records a sign-in: creates the person on first sight,
	// updates name, provider, groups and the time otherwise.
	TouchIdentity(ctx context.Context, ws string, id Identity) (*Identity, error)
	ListIdentities(ctx context.Context, ws string) ([]Identity, error)
	// GetIdentity finds a person by email (case-insensitive); ErrNotFound
	// when the workspace has never seen them.
	GetIdentity(ctx context.Context, ws, email string) (*Identity, error)
	// SetIdentityStatus suspends or reactivates a person.
	SetIdentityStatus(ctx context.Context, ws, email, status string) (*Identity, error)
	DeleteIdentity(ctx context.Context, ws, email string) error

	ListTeams(ctx context.Context, ws string) ([]Team, error)
	GetTeam(ctx context.Context, ws, name string) (*Team, error)
	// PutTeam creates or replaces a team by name; created reports which.
	// The built-in team cannot be written (ErrBuiltIn).
	PutTeam(ctx context.Context, ws string, t Team) (team *Team, created bool, err error)
	// DeleteTeam removes the team and every grant given to it; not the
	// built-in one (ErrBuiltIn).
	DeleteTeam(ctx context.Context, ws, name string) error

	ListGrants(ctx context.Context, ws string) ([]Grant, error)
	ListProjectGrants(ctx context.Context, ws, project string) ([]Grant, error)
	// AddGrant fails with ErrConflict when the same grant exists and with
	// ErrNotFound when the team does not.
	AddGrant(ctx context.Context, ws string, g Grant) (*Grant, error)
	DeleteGrant(ctx context.Context, ws, id string) error
	// DeleteProjectGrants removes every grant of a project (project destroyed).
	DeleteProjectGrants(ctx context.Context, ws, project string) error

	Sessions
	Tokens
	Memberships
	Hosts

	// Export and Import move the whole workspace's people and tenancy
	// (platform backups, RFC-0037).
	Export(ctx context.Context, ws string) (*Dump, error)
	Import(ctx context.Context, ws string, d *Dump, overwrite bool) (*ImportResult, error)
}

// Session is a signed-in browser (RFC-0007), kept here so restarts and
// replicas share it. Identity is the server's identity type as JSON.
type Session struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspaceId"`
	Identity    json.RawMessage `json:"identity"`
	CSRF        string          `json:"csrf"`
	IDToken     string          `json:"idToken,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
	LastSeenAt  time.Time       `json:"lastSeenAt"`
}

// Code is a one-time code carrying a session to an app host (the edge).
type Code struct {
	Code      string          `json:"code"`
	Host      string          `json:"host"`
	Claims    json.RawMessage `json:"claims"`
	ExpiresAt time.Time       `json:"expiresAt"`
}

// Sessions is the part of the store the sign-in machinery uses.
type Sessions interface {
	PutSession(ctx context.Context, ws string, s Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	// TouchSession moves last_seen_at forward.
	TouchSession(ctx context.Context, id string, at time.Time) error
	DeleteSession(ctx context.Context, id string) error
	// PurgeSessions removes sessions created before createdBefore or not
	// seen since seenBefore; it returns how many went.
	PurgeSessions(ctx context.Context, createdBefore, seenBefore time.Time) (int, error)
	CountSessions(ctx context.Context, ws string) (int, error)

	PutCode(ctx context.Context, c Code) error
	// TakeCode returns the code once and removes it (expired ones too, as
	// ErrNotFound).
	TakeCode(ctx context.Context, code string) (*Code, error)
}

// Dump is a workspace's content as the backup carries it. Invitations are
// not in it: their tokens live in the emails sent, and they expire.
type Dump struct {
	Version     int             `json:"version"`
	Workspace   Workspace       `json:"workspace"`
	Identities  []Identity      `json:"identities"`
	Teams       []Team          `json:"teams"`
	Grants      []Grant         `json:"grants"`
	Domains     []DomainClaim   `json:"domains,omitempty"`
	Memberships []Membership    `json:"memberships,omitempty"`
	Hosts       []WorkspaceHost `json:"hosts,omitempty"`
}

// ImportResult counts what Import did.
type ImportResult struct {
	Teams, Grants, Identities, Memberships, Hosts int
	Skipped                                       int
}

// DumpVersion is the format of Dump: 2 added memberships (v0.9.13), 3 the
// workspace's hosts (v0.9.16); a server that knows only an older version
// refuses a newer dump instead of dropping what it does not know.
const DumpVersion = 3
