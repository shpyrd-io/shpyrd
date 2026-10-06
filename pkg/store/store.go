// Package store is the platform's control-plane database (RFC-0033): the
// workspace, the people it has seen, teams and their grants on projects.
// Workloads stay Kubernetes objects; what is about people and tenancy lives
// here, in Postgres on a real install and in memory in tests.
package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/ids"
)

// DefaultWorkspace is the slug of the implicit workspace every open-source
// install has: names built from it carry no workspace part.
const DefaultWorkspace = "default"

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
)

// Workspace is the tenant. Every workspace answers at an Address of its
// own (RFC-0080): the dashboard and sign-in at <Address>, apps at
// <app>.<Address>. The default workspace, created at install, is one of
// them; in the open-source layout its address is the platform domain.
type Workspace struct {
	ID      string `json:"id"`
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Address string `json:"address,omitempty"`
	Status  string `json:"status"` // WorkspaceActive or WorkspaceSuspended
	// Owner distinguishes operator workspaces (COGS, never invoiced) from
	// customer workspaces (revenue). RFC-0078.
	Owner    string            `json:"owner,omitempty"` // "operator" | "customer"
	Settings WorkspaceSettings `json:"settings"`
	// Readiness is what the controller found the last time it looked at
	// the workspace's front door; nil before the first look. ReadyAt is
	// when every check first passed, nil until then: before that moment
	// a link to the workspace's address leads nowhere.
	Readiness *WorkspaceReadiness `json:"readiness,omitempty"`
	ReadyAt   *time.Time          `json:"readyAt,omitempty"`
	// OwnerInvitePending is the email of a first owner whose invitation
	// waits for the workspace to be ready; "" when none waits.
	OwnerInvitePending string    `json:"ownerInvitePending,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

// WorkspaceReadiness is the state of a workspace's door, as the controller
// last saw it: whether the address answers, and each thing it checked.
type WorkspaceReadiness struct {
	Ready     bool             `json:"ready"`
	CheckedAt time.Time        `json:"checkedAt"`
	Checks    []ReadinessCheck `json:"checks"`
}

// ReadinessCheck is one thing the controller checked about a workspace's
// door: the front door exists, the certificate is issued, the public name
// resolves, the door answers over HTTPS.
type ReadinessCheck struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	// Detail says what was seen when the check did not pass, or why it was
	// not made ("not checked: no DNS provider").
	Detail string `json:"detail,omitempty"`
}

// Workspace statuses.
const (
	WorkspaceActive    = "active"
	WorkspaceSuspended = "suspended" // answers nothing but the "suspended" page
)

// Workspace owner values (RFC-0078).
const (
	WorkspaceOwnerOperator = "operator"
	WorkspaceOwnerCustomer = "customer"
)

// OwnedByOperator reports whether the workspace is the platform operator's
// (RFC-0078): its costs are the operator's, it is never invoiced, and
// platform admins own it without a membership (RFC-0080).
func (w *Workspace) OwnedByOperator() bool { return w.Owner == WorkspaceOwnerOperator }

// Realms of a session (RFC-0080): the console (the operator's door, at the
// console host) and workspaces (at their addresses and hosts). A session
// answers only in the realm it was opened in.
const (
	RealmConsole   = "console"
	RealmWorkspace = "workspace"
)

// DefaultWorkspaceSpec is the workspace every install has, created by
// Migrate: the operator's, the default one (settings.default_workspace_id).
type DefaultWorkspaceSpec struct {
	Slug    string // "default" unless the operator chose a name
	Name    string // display name
	Address string // where its dashboard answers; apps one label under
}

// DefaultWorkspaceSlug is the slug of the default workspace: the setting
// when set (RFC-0078), else the constant every install started with.
func DefaultWorkspaceSlug(ctx context.Context, st Settings) string {
	if st != nil {
		if v, err := st.GetSetting(ctx, SettingDefaultWorkspaceID); err == nil && v != "" {
			return v
		}
	}
	return DefaultWorkspace
}

// Join policies: who becomes a person on first sign-in.
const (
	JoinOpen    = "open"    // anyone who can sign in
	JoinCompany = "company" // only through the method of a claimed domain
	JoinListed  = "listed"  // only people already named in a team or a grant
)

// WorkspaceSettings are the knobs of the workspace.
type WorkspaceSettings struct {
	// InternalExposure is an operator override; nil inherits the hosting policy.
	// Changed only through SetWorkspaceInternalExposure, never ordinary settings.
	InternalExposure *bool  `json:"internalExposure,omitempty"`
	JoinPolicy       string `json:"joinPolicy,omitempty"` // JoinOpen when empty
	// OwnMethodsOnly hides the platform's login methods from this
	// workspace's login page: only the methods the workspace configured
	// itself (its company SSO) are offered (RFC-0033). Never true for the
	// implicit workspace.
	OwnMethodsOnly bool `json:"ownMethodsOnly,omitempty"`
	// Limits are the workspace's ceilings (RFC-0033, RFC-0042): nil means
	// none, the open-source default. The API checks them before changing
	// anything; the controller backs them with a ResourceQuota per project
	// namespace.
	Limits *Limits `json:"limits,omitempty"`
	// Sleep is what the workspace's projects do by default when nobody
	// uses them (RFC-0075): nil means they never sleep unless they say so.
	Sleep *SleepDefaults `json:"sleep,omitempty"`
	// Branding is how the workspace looks to its people: the launcher and
	// the login page show its logo and use its colour (RFC-0033).
	Branding *Branding `json:"branding,omitempty"`
	// MCPName is what an assistant shows for the workspace's MCP server
	// (RFC-0032); "<name> on shpyrd" when empty.
	MCPName string `json:"mcpName,omitempty"`
}

// SleepDefaults are a workspace's default sleep: projects and databases
// without a policy of their own inherit them; an explicit "off" opts out.
// Durations are Go's ("10m"); empty means no default.
type SleepDefaults struct {
	// AppsAfter is the quiet period after which a web process sleeps;
	// AppsResuming how it wakes: "page" (a waiting page) or "wait" (the
	// request is held).
	AppsAfter    string `json:"appsAfter,omitempty"`
	AppsResuming string `json:"appsResuming,omitempty"`
	// DatabasesAfter is the idle period after which a database hibernates.
	DatabasesAfter string `json:"databasesAfter,omitempty"`
}

// Branding is a workspace's look.
type Branding struct {
	// Logo is the image, base64; LogoType its media type (image/png,
	// image/svg+xml, ...). At most 256 KB.
	Logo     string `json:"logo,omitempty"`
	LogoType string `json:"logoType,omitempty"`
	// Color is the accent colour, #rrggbb.
	Color string `json:"color,omitempty"`
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
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`
	OwnerEmail  string `json:"ownerEmail"`
	// Kind is what the token is: "" (TokenKindToken) for an API token with
	// roles of its own, within the owner's; TokenKindSession for the
	// credential a `shpyrd login` approved in the browser (RFC-0052), which
	// acts as the owner, with the owner's roles as they are now.
	Kind         string            `json:"kind,omitempty"`
	PlatformRole string            `json:"platformRole,omitempty"`
	ProjectRoles map[string]string `json:"projectRoles,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
	ExpiresAt    *time.Time        `json:"expiresAt,omitempty"`
	LastUsedAt   *time.Time        `json:"lastUsedAt,omitempty"`
}

// Kinds of API token (APIToken.Kind).
const (
	TokenKindToken   = "token"
	TokenKindSession = "session"
)

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

// OAuthClient is an application registered at the workspace's OAuth 2.1
// server (RFC-0032, RFC 7591 dynamic registration): an MCP client such as
// Claude. Public clients (no secret) prove themselves with PKCE.
type OAuthClient struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspaceId"`
	ClientID     string    `json:"clientId"`
	SecretHash   string    `json:"-"`
	Name         string    `json:"name"`
	RedirectURIs []string  `json:"redirectUris"`
	CreatedAt    time.Time `json:"createdAt"`
}

// OAuthCode is an authorization code waiting to be exchanged: one use,
// minutes of life, bound to the client, the redirect URI and the PKCE
// challenge.
type OAuthCode struct {
	Hash          string    `json:"-"`
	WorkspaceID   string    `json:"workspaceId"`
	ClientID      string    `json:"clientId"`
	Email         string    `json:"email"`
	Subject       string    `json:"subject"`
	Scope         string    `json:"scope"`
	RedirectURI   string    `json:"redirectUri"`
	CodeChallenge string    `json:"codeChallenge"`
	Resource      string    `json:"resource,omitempty"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

// OAuthToken is a refresh token a person granted a client: what the
// person's settings list and revoke. Only its hash is stored.
type OAuthToken struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspaceId"`
	ClientID    string     `json:"clientId"`
	ClientName  string     `json:"clientName,omitempty"`
	Email       string     `json:"email"`
	Scope       string     `json:"scope"`
	Hash        string     `json:"-"`
	CreatedAt   time.Time  `json:"createdAt"`
	ExpiresAt   time.Time  `json:"expiresAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
}

// OAuth is the OAuth 2.1 server's part of the Store (RFC-0032).
type OAuth interface {
	CreateOAuthClient(ctx context.Context, ws string, c OAuthClient) (*OAuthClient, error)
	// OAuthClientByID finds a client of any workspace by its client_id.
	OAuthClientByID(ctx context.Context, clientID string) (*OAuthClient, error)
	// PutOAuthCode stores a code by its hash; TakeOAuthCode returns and
	// removes it (expired ones too, as ErrNotFound).
	PutOAuthCode(ctx context.Context, code OAuthCode) error
	TakeOAuthCode(ctx context.Context, hash string) (*OAuthCode, error)
	// CreateOAuthToken stores a refresh token by its hash.
	CreateOAuthToken(ctx context.Context, ws string, t OAuthToken) (*OAuthToken, error)
	// OAuthTokenByHash finds a live refresh token; ErrNotFound when it is
	// unknown or expired.
	OAuthTokenByHash(ctx context.Context, hash string) (*OAuthToken, error)
	// RotateOAuthToken replaces a token's hash (refresh token rotation)
	// and records the use.
	RotateOAuthToken(ctx context.Context, id, newHash string, expiresAt time.Time) error
	ListOAuthTokens(ctx context.Context, ws, email string) ([]OAuthToken, error)
	DeleteOAuthToken(ctx context.Context, ws, id string) error
}

// ---- Usage (RFC-0075) ------------------------------------------------------

// MergeLimits applies the ceilings patch gives (its non-zero fields) over
// base; a nil base is no ceiling at all.
func MergeLimits(base, patch *Limits) *Limits {
	out := Limits{}
	if base != nil {
		out = *base
	}
	if patch == nil {
		return &out
	}
	if patch.Projects > 0 {
		out.Projects = patch.Projects
	}
	if patch.Instances > 0 {
		out.Instances = patch.Instances
	}
	if patch.CPU != "" {
		out.CPU = patch.CPU
	}
	if patch.Memory != "" {
		out.Memory = patch.Memory
	}
	if patch.Storage != "" {
		out.Storage = patch.Storage
	}
	return &out
}

// UsageBucket is one 5-minute (or hourly) usage record for a project
// component. Quantity is nil when Quality is missing.
type UsageBucket struct {
	WorkspaceID string            `json:"workspaceId"`
	Project     string            `json:"project"`
	Component   string            `json:"component"`
	Metric      string            `json:"metric"`
	PeriodStart time.Time         `json:"periodStart"`
	PeriodEnd   time.Time         `json:"periodEnd"`
	Quantity    *float64          `json:"quantity"` // nil when missing
	Unit        string            `json:"unit"`
	Quality     string            `json:"quality"` // complete | partial | missing
	Revision    int               `json:"revision"`
	Source      string            `json:"source"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// Usage metrics.
const (
	MetricCPUUsed     = "cpu_used"     // core-seconds
	MetricCPUReserved = "cpu_reserved" // core-seconds
	MetricMemoryUsed  = "memory_used"  // GiB-seconds of working set (metered until the switch to reserved)
	// MetricMemoryReserved is the memory an awake instance reserves,
	// request equals limit, so it is also what the instance takes from the
	// node: the memory the customer is billed for (RFC-0075).
	MetricMemoryReserved = "memory_reserved"  // GiB-seconds
	MetricStorage        = "storage"          // GiB-seconds (provisioned)
	MetricEgressHTTP     = "egress_http"      // bytes
	MetricInstanceSec    = "instance_seconds" // process-seconds
	MetricStateSec       = "state_seconds"    // seconds in each state

	UnitCoreSeconds = "core_seconds"
	UnitGiBSeconds  = "gib_seconds"
	UnitBytes       = "bytes"
	UnitSeconds     = "seconds"

	QualityComplete = "complete"
	QualityPartial  = "partial"
	QualityMissing  = "missing"
)

// SleepEvent records a sleep or wake of a process or database.
type SleepEvent struct {
	ID              string    `json:"id"`
	WorkspaceID     string    `json:"workspaceId"`
	Project         string    `json:"project"`
	Component       string    `json:"component"`
	Event           string    `json:"event"` // sleep | wake
	At              time.Time `json:"at"`
	DurationSeconds *int      `json:"durationSeconds,omitempty"`
	Reason          string    `json:"reason,omitempty"`
}

// Project is the store's mirror of a project (RFC-0076): its stable ID, the
// workspace it belongs to, and the current slug and name — kept by the
// controller from the App custom resource. Deleted projects stay, marked,
// so an invoice can still name them. The ledger keys on Short().
type Project struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspaceId"`
	Slug        string     `json:"slug"`
	Name        string     `json:"name"`
	Namespace   string     `json:"namespace"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	DeletedAt   *time.Time `json:"deletedAt,omitempty"`
}

// Short is the project's ID as the ledger, namespaces and labels carry it:
// 25 lowercase base36 characters (RFC-0059, RFC-0076).
func (p Project) Short() string { return ids.Short(p.ID) }

// Projects is the project registry part of the Store (RFC-0076).
type Projects interface {
	// UpsertProject records a project's identity, slug, name and namespace
	// (insert or update by ID); a deleted project is revived.
	UpsertProject(ctx context.Context, p Project) (*Project, error)
	// DeleteProject marks a project deleted; its rows stay for invoices.
	DeleteProject(ctx context.Context, id string) error
	// ListProjects returns the projects of a workspace (slug or ID), oldest
	// first; deleted ones only when withDeleted is set.
	ListProjects(ctx context.Context, ws string, withDeleted bool) ([]Project, error)
	// ProjectBySlug finds the live project with that slug in a workspace.
	ProjectBySlug(ctx context.Context, ws, slug string) (*Project, error)
	// RekeyProject rewrites ledger rows keyed by a legacy project slug to the
	// project's ID (the one-time migration of RFC-0076); rows that would
	// collide with rows already keyed by the ID are dropped in favour of the
	// latter. Returns the number of rows moved.
	RekeyProject(ctx context.Context, ws, slug, id string) (int, error)
	// RekeyGrantsToIDs rewrites grants.project and api_tokens.project_roles
	// keys from project slugs to short base36 IDs (RFC-0076, v0.9.47);
	// called once from Migrate. Rows whose slug has no live project entry
	// are left unchanged. Returns the number of rows updated.
	RekeyGrantsToIDs(ctx context.Context) (int, error)
	// RenameProjectSlug changes the slug of a live project and updates every
	// grant that referenced it. Used by the rename API (RFC-0076 part B).
	RenameProjectSlug(ctx context.Context, ws, oldSlug, newSlug string) error
	// ProjectIcon is the image a project sent for its card on the
	// launcher, by the project's ID, and its media type; ErrNotFound when
	// it sent none.
	ProjectIcon(ctx context.Context, id string) (data []byte, typ string, err error)
	// SetProjectIcon records the image of a project; no data removes it.
	SetProjectIcon(ctx context.Context, id string, data []byte, typ string) error
}

// Usage is the metering part of the Store (RFC-0075): what projects use,
// in quantities, and when they sleep.
type Usage interface {
	// Usage ledger.
	// WriteBuckets writes or revises usage buckets; existing (workspace, project,
	// component, metric, period_start, revision) rows are skipped (idempotent).
	WriteBuckets(ctx context.Context, buckets []UsageBucket) error
	// QueryBuckets returns buckets for a workspace and optional project, from
	// the five-minute table when the range is recent or the hourly table otherwise.
	QueryBuckets(ctx context.Context, ws, project string, from, to time.Time) ([]UsageBucket, error)
	// RollupHourly folds five-minute buckets whose hour closed before the given
	// time into usage_hourly (sum of quantities; quality is the worst of the
	// hour's buckets) and deletes the folded five-minute rows. Idempotent: an
	// hour already rolled up is left alone. Returns hours rolled up.
	RollupHourly(ctx context.Context, before time.Time) (int, error)

	// Sleep events.
	WriteSleepEvent(ctx context.Context, e SleepEvent) error
	QuerySleepEvents(ctx context.Context, ws, project string, from, to time.Time) ([]SleepEvent, error)
}

// Settings is the cluster-wide key/value store (RFC-0078).
type Settings interface {
	// GetSetting returns the value for key, "" when not set.
	GetSetting(ctx context.Context, key string) (string, error)
	// SetSetting stores a key/value pair (upsert).
	SetSetting(ctx context.Context, key, value string) error
}

// Well-known setting keys.
const (
	// SettingDefaultWorkspaceID is the slug of the workspace the console
	// host resolves to (RFC-0078).
	SettingDefaultWorkspaceID = "default_workspace_id"
)

// ConsoleUser is someone who may open the operator's console: an email,
// independent of every workspace. All of them are its admins for now.
type ConsoleUser struct {
	Email   string    `json:"email"`
	AddedAt time.Time `json:"addedAt"`
	AddedBy string    `json:"addedBy,omitempty"`
}

// normEmail is an email as the store keeps it.
func normEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// ConsoleUsers is the console's own list of people.
type ConsoleUsers interface {
	// ListConsoleUsers lists them by email.
	ListConsoleUsers(ctx context.Context) ([]ConsoleUser, error)
	// AddConsoleUser puts an email on the list (lowercased); adding one
	// already there changes nothing.
	AddConsoleUser(ctx context.Context, email, by string) (*ConsoleUser, error)
	// RemoveConsoleUser takes an email off; ErrNotFound when it was not on.
	RemoveConsoleUser(ctx context.Context, email string) error
	// IsConsoleUser says whether an email is on the list.
	IsConsoleUser(ctx context.Context, email string) (bool, error)
}

// ---- Costs (ee/costs) ------------------------------------------------------

// Kinds and sources of cost lines.
const (
	CostUsage     = "usage"     // quantities the platform measured (metering)
	CostEstimated = "estimated" // OpenCost's allocation at list prices
	CostReal      = "real"      // the provider's bill (oci)
)

// CostLine is one line of what the cluster used or cost: for a window, a
// workspace, project and process where it applies, a metric, a resource
// (the OCID of a node or a volume) and the provider's tags.
type CostLine struct {
	ID           string            `json:"id"`
	Kind         string            `json:"kind"`
	Source       string            `json:"source"`
	Start        time.Time         `json:"start"`
	End          time.Time         `json:"end"`
	Workspace    string            `json:"workspace,omitempty"`
	Project      string            `json:"project,omitempty"`
	Process      string            `json:"process,omitempty"`
	Metric       string            `json:"metric,omitempty"`
	Quantity     *float64          `json:"quantity,omitempty"`
	Unit         string            `json:"unit,omitempty"`
	Cost         *float64          `json:"cost,omitempty"`
	Currency     string            `json:"currency,omitempty"`
	Resource     string            `json:"resource,omitempty"`
	ResourceType string            `json:"resourceType,omitempty"`
	Service      string            `json:"service,omitempty"`
	SKU          string            `json:"sku,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
	ChangedAt    time.Time         `json:"-"`
}

// Key is what a line is about: two lines with the same key are the same
// line, read again.
func (l CostLine) Key() string {
	return strings.Join([]string{l.Kind, l.Source, l.Start.UTC().Format(time.RFC3339), l.End.UTC().Format(time.RFC3339), l.Workspace, l.Project, l.Process, l.Metric, l.Resource, l.Service, l.SKU}, "|")
}

// CostQuery selects lines whose window starts in [From, To); empty fields
// do not filter.
type CostQuery struct {
	From, To  time.Time
	Kind      string
	Workspace string
}

// CostDrain sends cost lines to a URL (ee/costs). Header values live in a
// Secret; the store keeps their names.
type CostDrain struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	URL            string     `json:"url"`
	Headers        []string   `json:"headers,omitempty"`
	CursorAt       *time.Time `json:"cursorAt,omitempty"`
	CursorID       string     `json:"-"`
	LastDeliveryAt *time.Time `json:"lastDeliveryAt,omitempty"`
	Sent           int64      `json:"sent"`
	Errors         int64      `json:"errors"`
	Message        string     `json:"message,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

// CostDelivery is what one attempt of a drain did: lines sent and the
// cursor reached, or an error.
type CostDelivery struct {
	At       time.Time
	Sent     int
	CursorAt *time.Time
	CursorID string
	Err      string
}

// Costs is the cost part of the Store (ee/costs).
type Costs interface {
	// UpsertCostLines writes lines by key (ID is set from it); a line whose
	// numbers or tags did not change keeps its changed_at. Returns how many
	// changed.
	UpsertCostLines(ctx context.Context, lines []CostLine) (int, error)
	QueryCostLines(ctx context.Context, q CostQuery) ([]CostLine, error)
	// CostLinesChangedSince lists lines changed after the cursor (at, then
	// id), oldest first, at most limit.
	CostLinesChangedSince(ctx context.Context, at time.Time, id string, limit int) ([]CostLine, error)

	ListCostDrains(ctx context.Context) ([]CostDrain, error)
	// CreateCostDrain adds a drain; ErrConflict when the name is taken.
	CreateCostDrain(ctx context.Context, d CostDrain) (*CostDrain, error)
	// DeleteCostDrain removes one by name; ErrNotFound when none.
	DeleteCostDrain(ctx context.Context, name string) error
	// RecordCostDelivery records an attempt: lines sent, cursor moved, or
	// an error.
	RecordCostDelivery(ctx context.Context, id string, d CostDelivery) error
}

// CostLineID is the stable id of a line: a hash of its key.
func CostLineID(l CostLine) string {
	sum := sha256.Sum256([]byte(l.Key()))
	return "cl_" + hex.EncodeToString(sum[:12])
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
	Migrate(ctx context.Context, def DefaultWorkspaceSpec) error
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
	SetWorkspaceInternalExposure(ctx context.Context, slug string, enabled *bool) (*Workspace, error)
	// SetWorkspaceStatus suspends or reactivates a workspace.
	SetWorkspaceStatus(ctx context.Context, slug, status string) (*Workspace, error)
	// SetWorkspaceReadiness records what the controller found at the
	// workspace's door; ReadyAt is set the first time it is ready and kept.
	SetWorkspaceReadiness(ctx context.Context, slug string, r WorkspaceReadiness) (*Workspace, error)
	// SetWorkspaceOwnerInvitePending keeps (or, with "", clears) the email
	// of a first owner whose invitation waits for readiness.
	SetWorkspaceOwnerInvitePending(ctx context.Context, slug, email string) (*Workspace, error)

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
	OAuth
	Usage
	Projects
	Settings
	ConsoleUsers
	Costs

	// Export and Import move the whole workspace's people and tenancy
	// (platform backups, RFC-0037).
	Export(ctx context.Context, ws string) (*Dump, error)
	Import(ctx context.Context, ws string, d *Dump, overwrite bool) (*ImportResult, error)
}

// Session is a signed-in browser (RFC-0007), kept here so restarts and
// replicas share it. Identity is the server's identity type as JSON.
type Session struct {
	ID string `json:"id"`
	// Realm is where the session was opened (RealmConsole or
	// RealmWorkspace); WorkspaceID is set for workspace sessions only.
	Realm       string          `json:"realm"`
	WorkspaceID string          `json:"workspaceId,omitempty"`
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
	// PutSession stores a session in a workspace (slug), or in the console
	// realm when ws is "" (RFC-0080).
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
	// PeekCode returns a code without consuming it; ErrNotFound when it is
	// unknown or expired. A gate's pass is read on every request (RFC-0083).
	PeekCode(ctx context.Context, code string) (*Code, error)
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
