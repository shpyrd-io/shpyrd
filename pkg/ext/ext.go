// Package ext defines how optional platform capabilities plug into shpyrd
// (RFC-0002). An extension is a Go package compiled into the binaries and
// switched on per cluster; it contributes an installer component, resource
// types with reconcilers, API routes and CLI commands through the small
// interfaces below. The registry of built-in extensions lives in ext/all.
package ext

import (
	"context"
	"io"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Extension is one optional capability.
type Extension interface {
	// Name is the identifier used to enable it: "auth-local".
	Name() string
	Description() string
	// Components names the installer components the extension adds
	// (directories under deploy/components) with the runlevels they join,
	// in install order; empty when the extension has no cluster component.
	Components() []ComponentRef
	// Register adds reconcilers to the controller manager (may be a no-op).
	Register(mgr ctrl.Manager, deps Deps) error
	// Routes mounts API routes. Public routes need no authentication;
	// protected ones sit behind the server's authentication.
	Routes(r Router, deps Deps) error
	// CLI returns extra top-level commands.
	CLI(g CLIGlobals) []*cobra.Command
	// Types lists the resource kinds the extension owns; disabling refuses
	// while such resources exist.
	Types() []ResourceType
}

// ComponentRef points at an installer component by name.
type ComponentRef struct {
	Name     string
	Runlevel string
}

// ResourceType describes a resource kind for the dashboard and CLI.
type ResourceType struct {
	// Kind is the CRD kind ("Postgres"); Group/Version/Resource locate it.
	Kind     string
	Group    string
	Version  string
	Resource string
	// Bindable resources can be attached to an App (RFC-0003).
	Bindable bool
}

// ShellTarget is where a shell into a resource lands (RFC-0052): the pod
// and container, and the command to run there.
type ShellTarget struct {
	Pod       string
	Container string
	Command   []string
}

// Shellable is an extension whose resources take a shell: `shpyrd pg
// psql`, `shpyrd redis cli` over the web terminal's bridge (RFC-0026).
// ResourceShell names the pod, container and command for one resource of
// a kind; args are what the person wrote after the resource's name.
type Shellable interface {
	ResourceShell(ctx context.Context, deps Deps, kind, namespace, name string, args []string) (ShellTarget, error)
}

// Router gives extensions the route groups of the API: public (no
// session), protected (any signed-in identity), workspace-admin (the
// cluster.admin action at any workspace host: owners and admins of the
// request's workspace, RFC-0033) and admin (the same action at the console
// only: the operator's).
type Router interface {
	Public() gin.IRouter
	Protected() gin.IRouter
	WorkspaceAdmin() gin.IRouter
	Admin() gin.IRouter
}

// WorkspaceContextKey is where the API keeps the request's workspace
// (*store.Workspace) on the gin context once the host resolved it.
const WorkspaceContextKey = "shpyrd.workspace"

// WorkspaceFrom returns the slug of the request's workspace, "" when the
// request is not scoped to one (or the API has not resolved it).
// WorkspaceObjectFrom is the request's workspace, nil at the console
// (RFC-0080) or when unresolved.
func WorkspaceObjectFrom(c *gin.Context) *store.Workspace {
	if v, ok := c.Get(WorkspaceContextKey); ok {
		if ws, ok := v.(*store.Workspace); ok {
			return ws
		}
	}
	return nil
}

func WorkspaceFrom(c *gin.Context) string {
	if v, ok := c.Get(WorkspaceContextKey); ok {
		if ws, ok := v.(*store.Workspace); ok && ws != nil {
			return ws.Slug
		}
	}
	return ""
}

// Deps is what the server hands to extensions.
type Deps struct {
	Kube *kube.Client
	// Client is a controller-runtime client (cached when running with the
	// manager).
	Client client.Client
	// SystemNamespace holds shpyrd's own objects (shpyrd-system).
	SystemNamespace string
	// Vars are the install variables (SHPYRD_DOMAIN, SHPYRD_AUTH_URL, ...)
	// as the server sees them from its environment.
	Vars func(name string) string
	// Auth registers login providers with the server (RFC-0007).
	Auth AuthRegistry
	// Store is the control-plane database (RFC-0033): workspaces, people,
	// teams, grants. Nil when the extension runs without one (tests).
	Store store.Store
	// WorkspacesChanged asks the platform to publish workspaces now (front
	// doors, RFC-0033 phase 6) instead of at the next timer; nil when this
	// replica runs no controllers.
	WorkspacesChanged func()
	// Mail sends email on the platform's behalf (RFC-0013): the mail
	// extension provides it; nil when none is enabled.
	Mail Mailer
	// LocalAccounts is the authlocal user store (RFC-0014): the auth-local
	// extension provides it so the server can check lockout and set passwords
	// during reset/invite flows. Nil when auth-local is not enabled.
	LocalAccounts LocalAccountStore
	// Invite brings a person into a workspace with a role (RFC-0033): one
	// the workspace knows holds the role at once; anyone else gets an
	// invitation whose link, and the set-password link of a pending local
	// account (RFC-0014), stand on the workspace's own door and go out by
	// email when the mail extension is configured. Nil without a store.
	Invite func(c *gin.Context, workspace, email, role string) (*InviteOutcome, error)
}

// InviteOutcome is what Invite produced.
type InviteOutcome struct {
	// Applied says the person was known already and holds the role now;
	// there is no link.
	Applied bool `json:"applied"`
	// Link is the invitation link, shown once; empty when Applied.
	Link      string    `json:"link,omitempty"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
	// SetPasswordLink is where a person without a password chooses one
	// (RFC-0014; 24 hours); empty when they have one or auth-local is off.
	SetPasswordLink string `json:"setPasswordLink,omitempty"`
	// Emailed says the link went out by email; MailError says why not
	// (empty when mail is not configured: the inviter passes the link on).
	Emailed   bool   `json:"emailed"`
	MailError string `json:"mailError,omitempty"`
	// Error is set when the invitation itself could not be made; the role
	// the caller granted stands regardless.
	Error string `json:"error,omitempty"`
}

// LocalAccountStore is the interface the server uses from the auth-local
// extension (RFC-0014). The concrete type is *authlocal.Store.
type LocalAccountStore interface {
	IsLocked(ctx context.Context, email string) (bool, error)
	Lock(ctx context.Context, email string, until time.Time) error
	MarkVerified(ctx context.Context, email string) error
	ActivateFromInvite(ctx context.Context, email, password string) error
	SetPasswordAndVerify(ctx context.Context, email, password string) error
	CreatePending(ctx context.Context, email, name string) error
	// Status is active, pending or locked; "" when there is no account.
	Status(ctx context.Context, email string) (string, error)
}

// Account states a LocalAccountStore reports.
const (
	AccountActive  = "active"
	AccountPending = "pending"
	AccountLocked  = "locked"
)

// Var reads an install variable, "" when none is configured.
func (d Deps) Var(name string) string {
	if d.Vars == nil {
		return ""
	}
	return d.Vars(name)
}

// Mailer sends email (RFC-0013). Invitations and notifications use it;
// the mail extension implements it over SMTP.
type Mailer interface {
	// Send delivers one message; the error carries what the operator
	// needs (never the credentials).
	Send(ctx context.Context, m Message) error
	// Configured reports whether a sender is set up: the extension can be
	// enabled before the operator runs `shpyrd-ctl mail set`.
	Configured(ctx context.Context) bool
}

// Message is one email: text always, HTML when the sender has a template.
type Message struct {
	To      []string
	Subject string
	Text    string
	HTML    string
}

// MailProvider is implemented by the extension that supplies the Mailer;
// the server asks the enabled extensions for one before mounting routes.
type MailProvider interface {
	Mailer(deps Deps) Mailer
}

// AuthRegistry is implemented by the server's relying party.
type AuthRegistry interface {
	// AddOIDC registers an OpenID Connect provider users can sign in with.
	AddOIDC(ctx context.Context, p OIDCProvider) error
	// RemoveOIDC unregisters a provider; existing sessions stay.
	RemoveOIDC(id string)
}

// OIDCProvider configures one OpenID Connect issuer.
type OIDCProvider struct {
	// ID is used in URLs (/api/auth/login?provider=<id>).
	ID string
	// Label is shown on the login page ("Email and password").
	Label string
	// Issuer is the external issuer URL, as browsers see it.
	Issuer string
	// ClientID and ClientSecret identify the dashboard at the issuer.
	ClientID     string
	ClientSecret string
	// Scopes beyond openid, email and profile.
	Scopes []string
	// Password says the issuer accepts the OAuth2 password grant for this
	// client: the dashboard shows an email/password form for the provider
	// instead of a button and exchanges the credentials itself (RFC-0012).
	Password bool
	// Kind picks the button's icon: "oidc" (default), "github", "google".
	Kind string
	// ConnectorID preselects a Dex connector (connector_id in the
	// authorization request) so Dex's chooser is skipped (RFC-0058).
	ConnectorID string
	// Realm is the door the method belongs to (RFC-0080): RealmConsole for
	// the console's own login page, RealmPlatform for the defaults every
	// workspace offers unless it hides them, RealmWorkspace for one
	// workspace's own method. Empty means RealmPlatform.
	Realm string
	// Workspace is the short id (RFC-0076) of the workspace a
	// RealmWorkspace method belongs to.
	Workspace string
}

// Realms of a login method (RFC-0080).
const (
	RealmConsole   = "console"
	RealmPlatform  = "platform"
	RealmWorkspace = "workspace"
)

// CLIGlobals gives extension commands access to the CLI's connection flags.
// Audience of an extension's CLI command: which binary carries it
// (RFC-0052). Developers use `shpyrd` for what lives in a project; the
// operator uses `shpyrd-ctl` for the platform. Commands without the
// annotation are the developer's.
const (
	AnnotationAudience = "shpyrd.io/audience"
	AudienceDeveloper  = "developer"
	AudienceOperator   = "operator"
)

// ForOperator marks a command as the operator's (`shpyrd-ctl`).
func ForOperator(c *cobra.Command) *cobra.Command {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[AnnotationAudience] = AudienceOperator
	return c
}

// Audience reports who a command is for.
func Audience(c *cobra.Command) string {
	if c.Annotations[AnnotationAudience] == AudienceOperator {
		return AudienceOperator
	}
	return AudienceDeveloper
}

type CLIGlobals interface {
	Kubeconfig() string
	Context() string
	// API is the workspace API through whatever the CLI has: a login
	// session (no cluster at all) or the kubeconfig proxy. Extension
	// commands that speak the API work for tenants of a hosted platform;
	// those that need the cluster (exec into a database) do not.
	API() APIClient
}

// APIClient sends requests to the workspace API.
type APIClient interface {
	// Request performs one call; body may be nil. Errors carry the
	// server's message.
	Request(ctx context.Context, method, path string, body []byte, contentType string) ([]byte, error)
	// Session says the CLI is signed in through the API and has no cluster:
	// commands that exec into pods go through Exec then.
	Session() bool
	// Exec opens a shell into a resource of a project through the web
	// terminal's bridge (RFC-0026): the extension that owns the kind names
	// the pod and the command (Shellable); the local terminal is bridged
	// until it ends. Returns the remote exit code as a *kexec.ExitError.
	Exec(ctx context.Context, project, kind, name string, args []string, stderr io.Writer) error
}

// Names returns the names of extensions, in order.
func Names(list []Extension) []string {
	out := make([]string, 0, len(list))
	for _, e := range list {
		out = append(out, e.Name())
	}
	return out
}

// Find returns the extension with the name, or nil.
func Find(list []Extension, name string) Extension {
	for _, e := range list {
		if e.Name() == name {
			return e
		}
	}
	return nil
}

// Identity is a signed-in user as seen by the server (RFC-0007).
type Identity struct {
	// Subject is the provider's stable id; "admin-token" for the token.
	Subject string   `json:"subject"`
	Email   string   `json:"email,omitempty"`
	Name    string   `json:"name,omitempty"`
	Groups  []string `json:"groups,omitempty"`
	// Provider is the login provider id ("local", "okta"); "token" for the
	// admin token.
	Provider string `json:"provider"`
	// Admin is true for the admin token and, until roles arrive (RFC-0008),
	// for every signed-in user.
	Admin bool `json:"admin"`
}

// identityKey is where the auth middleware stores the Identity.
const identityKey = "shpyrd.identity"

// SetIdentity stores the request's identity on the context.
func SetIdentity(c *gin.Context, id Identity) { c.Set(identityKey, id) }

// IdentityFrom returns the request's identity, when authenticated.
func IdentityFrom(c *gin.Context) (Identity, bool) {
	v, ok := c.Get(identityKey)
	if !ok {
		return Identity{}, false
	}
	id, ok := v.(Identity)
	return id, ok
}
