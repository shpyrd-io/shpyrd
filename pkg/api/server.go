// Package api implements the shpyrd HTTP API and serves the embedded UI.
package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"helm.sh/helm/v3/pkg/action"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/shpyrd-io/shpyrd/pkg/authz"
	"github.com/shpyrd-io/shpyrd/pkg/edge"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/pages"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
)

// Options configures the server.
type Options struct {
	// Addr is the listen address, e.g. ":8080".
	Addr string
	// SourcesAddr is a second listener that serves only the uploaded
	// source archives (GET /api/sources/:name) to the build pods in
	// project namespaces, so the NetworkPolicy can keep everything else
	// (the edge, the API) to the front doors (RFC-0033). Empty: sources
	// are served on Addr only.
	SourcesAddr string
	// TrustedProxies are the CIDRs whose X-Forwarded-For is believed (the
	// ingress controllers' pod range): behind them every client would
	// otherwise be the ingress pod, and per-IP throttles one bucket.
	TrustedProxies []string
	// UI is the built single page application. Nil disables UI serving.
	UI fs.FS
	// Pages is what this binary writes into every page of the
	// applications, and the origins it allows them: nothing on the core.
	Pages PageAdditions
	// Sources stores uploaded application archives. Nil disables deploys
	// from local checkouts.
	Sources *SourceStore
	// Token protects the API. Empty disables authentication (development).
	Token string
	// TokenDisabled refuses the admin token: sign-in through accounts only
	// (`shpyrd cluster token --disable`). Authentication stays required.
	TokenDisabled bool
	// Apps is the client used for App resources; defaults to an uncached
	// controller-runtime client built from the kube client. Tests inject a
	// fake.
	Apps client.Client
	// Prometheus serves the metrics endpoints. Nil disables them.
	Prometheus *PromClient
	// Public is returned by GET /api/config for the dashboard.
	Public PublicConfig
	// Extensions are the enabled extensions (RFC-0002); their routes are
	// mounted and their login providers registered.
	Extensions []ext.Extension
	// Vars looks up install variables (SHPYRD_*) for extensions; defaults
	// to the environment.
	Vars func(name string) string
	// IngressService is the cluster-internal address of the ingress
	// controller, used to reach issuers published on the cluster domain.
	IngressService string
	// RegistryGC is the in-cluster registry's garbage collector (RFC-0059);
	// nil when this replica does not run the controller.
	RegistryGC RegistryGC
	// Store is the control-plane database (RFC-0033): teams, grants, the
	// people seen at sign-in, the workspace. Defaults to an in-memory store
	// (tests, development without a database).
	Store store.Store
	// MembershipChanged is called after every team or grant write so the
	// RBAC mirror runs at once; nil when this replica does not run it.
	MembershipChanged func()
	// Tenancy maps request hosts to workspaces (RFC-0033 phase 6). Defaults
	// to tenancy.Single: every host is the implicit workspace. The cloud
	// layer supplies a resolver that knows many.
	Tenancy tenancy.Resolver
	// Realms decides which login methods each workspace offers (RFC-0033's
	// RealmProvider). Defaults to every configured method for every
	// workspace.
	Realms Realms
	// WorkspacesChanged is called after a workspace is created or changed
	// so its front door is published at once; nil when this replica runs
	// no controllers.
	WorkspacesChanged func()
	// SignInHost is the host of the sign-in service (Dex), whose root the
	// Ingress sends here: the server answers it with the mark alone, so
	// the service's own index is never seen. Empty, or the console's own
	// host, leaves every host as it is.
	SignInHost string
	// Capabilities names what this server offers beyond the core
	// ("workspaces", "billing", ...), returned by GET /api/config so one
	// dashboard and one CLI adapt. The core adds nothing.
	Capabilities []string
	// Logger defaults to slog.Default().
	Logger *slog.Logger
}

// PublicConfig is what the dashboard needs before logging in.
type PublicConfig struct {
	Version      string `json:"version"`
	Domain       string `json:"domain"`
	HTTPSPort    string `json:"httpsPort"`
	GrafanaURL   string `json:"grafanaUrl"`
	DashboardURL string `json:"dashboardUrl,omitempty"`
	AuthRequired bool   `json:"authRequired"`
	Metrics      bool   `json:"metrics"`
	// Auth lists the sign-in options (RFC-0007).
	Auth AuthConfig `json:"auth"`
	// Extensions enabled on this cluster (RFC-0002).
	Extensions []string `json:"extensions"`
	// Capabilities beyond the core this server offers (RFC-0033): empty on
	// the open-source platform.
	Capabilities []string `json:"capabilities"`
	// Workspace is the one answering at this host: its slug and name, for
	// the sign-in page.
	Workspace *WorkspaceRef `json:"workspace,omitempty"`
	// DefaultWorkspaceID is the slug of the workspace the console resolves
	// to on sign-in (RFC-0078).
	DefaultWorkspaceID string `json:"defaultWorkspaceId,omitempty"`
	// ConsoleHost is the hostname the console dashboard answers at.
	ConsoleHost string `json:"consoleHost,omitempty"`
	// ConsoleURL is the console's URL, for the way back from an operator
	// workspace (RFC-0080).
	ConsoleURL string `json:"consoleUrl,omitempty"`
	// Door is which application answers at this host (RFC-0080):
	// "console" or "workspace".
	Door string `json:"door,omitempty"`
	// Volumes describes the profile's storage rules (RFC-0060).
	Volumes VolumesConfig `json:"volumes"`
}

// VolumesConfig is what the dashboard needs to know about volumes here.
type VolumesConfig struct {
	NodeLocal bool `json:"nodeLocal,omitempty"`
	// MinSize is the provider minimum requests are rounded up to ("" = none).
	MinSize string `json:"minSize,omitempty"`
	// Snapshots is true when the cluster can take volume snapshots.
	Snapshots bool `json:"snapshots"`
}

// Server is the shpyrd API server.
// WorkspaceRef names a workspace in public payloads.
type WorkspaceRef struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	// Address is where the workspace's dashboard answers; apps one label
	// under it (RFC-0080: every workspace has one).
	Address string `json:"address,omitempty"`
	// OwnedByOperator marks the platform operator's own workspaces
	// (RFC-0078): they show platform admins the way to the console.
	OwnedByOperator bool `json:"ownedByOperator,omitempty"`
	// Branding is the workspace's look, for the login page and the
	// launcher (RFC-0033): the logo's URL when one is set, the colour.
	Branding *BrandingView `json:"branding,omitempty"`
}

// BrandingView is a workspace's look as pages use it.
type BrandingView struct {
	LogoURL string `json:"logoUrl,omitempty"`
	Color   string `json:"color,omitempty"`
}

type Server struct {
	opts             Options
	log              *slog.Logger
	engine           *gin.Engine
	kube             *kube.Client
	apps             client.Client
	archiveDownloads *archiveDownloads
	gates            gateSet            // RFC-0083: gates declared by extensions
	linkProviders    []ext.LinkProvider // RFC-0083: sidebar links from extensions
	projectGates     sync.Map           // namespace -> *sync.RWMutex; excludes in-flight mutations from archive capture
	archiveActive    sync.Map           // namespace -> active request; the server runs one replica
	helm             *action.Configuration
	sources          *SourceStore
	prom             *PromClient
	rp               *relyingParty
	authz            *authz.Resolver
	store            store.Store
	tenancy          tenancy.Resolver
	realms           Realms
	// mailer sends invitations (RFC-0013); nil without the mail extension.
	mailer ext.Mailer
	// The edge (RFC-0033): signing keys, one-time codes, host index.
	edgeKeys  *edge.Keys
	edgeCodes *edge.Codes
	hostCache hostCache
	// lookupTXT resolves TXT records for domain claims; nil uses the system
	// resolver (tests inject one).
	lookupTXT func(ctx context.Context, name string) ([]string, error)
	// lookupCNAME resolves a CNAME for custom workspace domains; nil uses
	// the system resolver.
	lookupCNAME func(ctx context.Context, host string) (string, error)
	// sleepAvailable reports whether the cluster can put apps to sleep
	// (RFC-0075: the KEDA HTTP add-on's CRDs are installed); nil asks the
	// REST mapper (tests inject one).
	sleepAvailable func() bool
	// hosts caches workspaces' host records (custom domains, moved
	// addresses; RFC-0033 names).
	hosts hostsCache
	// tokenFailures throttles clients presenting wrong admin tokens.
	tokenFailures *rateLimiter
	// passwordFailures throttles wrong passwords per account (RFC-0012).
	passwordFailures *rateLimiter
	// resetRateLimit throttles POST /api/auth/reset per IP (RFC-0014).
	resetRateLimit *rateLimiter
	// defaultSlug's cache (RFC-0078): the operator's default workspace.
	defaultSlugMu  sync.Mutex
	defaultSlugVal string
	defaultSlugAt  time.Time
	// localAccounts is the authlocal user store (RFC-0014): set by the
	// auth-local extension; used for lockout, reset and invite.
	localAccounts ext.LocalAccountStore
	// regCache holds the registry catalog summary for a minute.
	regCache registryCache
	// The web terminal (RFC-0026): unredeemed tickets and the live shells.
	execTickets *ticketStore
	shells      *shellRegistry
	// The CLI's browser sign-in (RFC-0052): codes waiting for approval.
	cliDevices *cliDeviceStore
	// shellMints throttles ticket minting per actor (RFC-0026).
	shellMints *rateLimiter
	// The exec bridge (RFC-0026). The three function fields are the seam tests
	// replace: everything but the pod stream itself is then testable, down to
	// the candidate walk execRun sits under.
	execStream execStreamFunc
	// attachStream attaches to a pod's own process: a one-off command
	// (RFC-0052). A seam like execStream.
	attachStream attachStreamFunc
	probeShell   probeShellFunc
	execRun      execRunFunc
	shellIdle    time.Duration
	// runPoll is how often a one-off instance is looked at while it
	// starts or ends; short in tests.
	runPoll    time.Duration
	shellPing  time.Duration
	shellProbe time.Duration
}

// New wires the routes.
func New(k *kube.Client, opts Options) (*Server, error) {
	if opts.Addr == "" {
		opts.Addr = ":8080"
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if os.Getenv("SHPYRD_DEBUG") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	helmCfg := new(action.Configuration)
	if err := helmCfg.Init(k.RESTClientGetterFor(""), "", os.Getenv("HELM_DRIVER"), func(format string, v ...interface{}) {
		opts.Logger.Debug(fmt.Sprintf(format, v...), "component", "helm")
	}); err != nil {
		return nil, fmt.Errorf("helm: %w", err)
	}
	if opts.Apps == nil {
		apps, err := k.ControllerClient()
		if err != nil {
			return nil, err
		}
		opts.Apps = apps
	}
	return newServer(k, opts, helmCfg)
}

// newServer wires everything except Helm initialisation (tests pass nil).
// NewWithoutHelm builds a server that cannot install Helm releases: for
// tests of layers built on the core, and tools that only need the API. It
// accepts a fake Kubernetes clientset.
func NewWithoutHelm(k *kube.Client, opts Options) (*Server, error) {
	return newServer(k, opts, nil)
}

func newServer(k *kube.Client, opts Options, helmCfg *action.Configuration) (*Server, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Vars == nil {
		opts.Vars = os.Getenv
	}
	opts.Public.AuthRequired = opts.Token != "" || opts.TokenDisabled
	opts.Public.Metrics = opts.Prometheus != nil
	opts.Public.Extensions = ext.Names(opts.Extensions)
	if opts.Public.Extensions == nil {
		opts.Public.Extensions = []string{}
	}
	if opts.Token == "" && !opts.TokenDisabled {
		opts.Logger.Warn("API authentication disabled: no admin token configured")
	}
	if opts.TokenDisabled {
		opts.Logger.Info("admin token disabled: sign-in through accounts only")
	}
	if opts.Store == nil {
		opts.Store = store.NewMemory()
	}
	if opts.Tenancy == nil {
		opts.Tenancy = &tenancy.Single{Store: opts.Store, ConsoleHost: consoleHostOf(opts.Public.DashboardURL), DefaultSlug: func(ctx context.Context) string { return store.DefaultWorkspaceSlug(ctx, opts.Store) }}
	}
	if opts.Realms == nil {
		opts.Realms = allMethods{}
	}
	if opts.Public.Capabilities == nil {
		opts.Public.Capabilities = append([]string{}, opts.Capabilities...)
	}
	s := &Server{opts: opts, log: opts.Logger, kube: k, apps: opts.Apps, helm: helmCfg, sources: opts.Sources, prom: opts.Prometheus, store: opts.Store, tenancy: opts.Tenancy, realms: opts.Realms}
	s.authz = &authz.Resolver{Store: opts.Store, DefaultSlug: func(ctx context.Context) string { return store.DefaultWorkspaceSlug(ctx, opts.Store) }}
	s.tokenFailures = newRateLimiter(20)
	s.passwordFailures = newRateLimiter(10)
	s.resetRateLimit = newRateLimiter(resetRequestsPerMinute)
	s.execTickets = newTicketStore(execTicketTTL)
	s.archiveDownloads = newArchiveDownloads()
	s.shells = newShellRegistry()
	s.cliDevices = newCLIDeviceStore()
	s.execStream = s.streamExec
	s.attachStream = s.streamAttach
	s.runPoll = time.Second
	s.probeShell = s.resolveShell
	s.execRun = s.runKexec
	s.shellIdle = shellIdleTimeout
	s.shellPing = shellPingInterval
	s.shellProbe = shellProbeTimeout
	s.shellMints = newRateLimiter(shellMintsPerMinute)
	s.engine = gin.New()
	s.engine.Use(gin.Recovery(), s.requestLogger(), securityHeaders(), s.redirectMoved())
	if err := s.engine.SetTrustedProxies(opts.TrustedProxies); err != nil {
		return nil, fmt.Errorf("trusted proxies: %w", err)
	}

	// Sessions and login providers (RFC-0007). Sessions are mirrored into a
	// Secret when a cluster is available.
	var kubeIface kubernetes.Interface
	systemNS := install.DefaultSystemNamespace
	if k != nil {
		kubeIface = k.Kube
		if k.Namespace != "" {
			systemNS = k.Namespace
		}
	}
	sessions := newSessionStore(opts.Store, kubeIface, systemNS, opts.Logger)
	sessions.load(context.Background())
	// The edge's signing key lives in the cluster; tests and clusterless
	// runs get a fresh one.
	if kubeIface != nil {
		keys, err := edge.LoadOrCreateKeys(context.Background(), kubeIface, systemNS)
		if err != nil {
			return nil, fmt.Errorf("edge keys: %w", err)
		}
		s.edgeKeys = keys
	} else {
		keys, err := edge.GenerateKeys()
		if err != nil {
			return nil, err
		}
		s.edgeKeys = keys
	}
	s.edgeCodes = edge.NewCodes(opts.Store)
	s.rp = newRelyingParty(sessions, opts.Public.DashboardURL, opts.Public.Domain, opts.IngressService, opts.Logger)
	s.rp.SetClusterCA(s.clusterCA(context.Background()))

	if err := s.routes(); err != nil {
		return nil, err
	}
	return s, nil
}

// Deps is what extensions get from the server: the controller manager
// hands the same to their Register, so a loop has what a route has.
func (s *Server) Deps() ext.Deps { return s.deps() }

// deps is what extensions get from the server.
func (s *Server) deps() ext.Deps {
	ns := install.DefaultSystemNamespace
	if s.kube != nil && s.kube.Namespace != "" {
		ns = s.kube.Namespace
	}
	// An extension that creates or changes a workspace needs everything
	// the server itself does after one: the front-door reconciler poked,
	// the host resolver's memory dropped, the identity provider's
	// callbacks refreshed (RFC-0080) — not the controller poke alone.
	d := ext.Deps{Kube: s.kube, Client: s.apps, SystemNamespace: ns, Vars: s.opts.Vars, Auth: s.rp, Store: s.store, WorkspacesChanged: s.workspacesChanged, Mail: s.mailer}
	d.GateAdmits = s.gateAdmitsHook
	if s.store != nil {
		d.Invite = s.inviteHook
		d.InviteBackground = s.inviteBackgroundHook
		d.SetSignupPassword = s.setSignupPasswordHook
		d.SignInTicket = s.signInTicketHook
	}
	return d
}

// routeGroups implements ext.Router.
type routeGroups struct {
	pub, api, wsAdmin, admin, platform gin.IRouter
	s                                  *Server
}

func (r routeGroups) Public() gin.IRouter   { return r.pub }
func (r routeGroups) Platform() gin.IRouter { return r.platform }
func (r routeGroups) SetLocalAccounts(la ext.LocalAccountStore) {
	if r.s != nil {
		r.s.localAccounts = la
	}
}
func (r routeGroups) Protected() gin.IRouter      { return r.api }
func (r routeGroups) WorkspaceAdmin() gin.IRouter { return r.wsAdmin }
func (r routeGroups) Admin() gin.IRouter          { return r.admin }

// Handler exposes the router, e.g. for tests.
func (s *Server) Handler() http.Handler { return s.engine }

// Run serves until ctx is cancelled, then shuts down gracefully.
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.opts.Addr,
		Handler:           s.engine,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// The edge's signing key rotates on schedule (RFC-0033); replicas
	// follow each other through the Secret.
	go s.edgeKeys.Run(ctx)
	go s.keepOIDCClient(ctx) // the identity provider's redirect URIs (RFC-0080)

	errCh := make(chan error, 2)
	go func() {
		s.log.Info("listening", "addr", s.opts.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	// The source archives on their own port for the build pods.
	var srcSrv *http.Server
	if s.opts.SourcesAddr != "" {
		srcSrv = &http.Server{Addr: s.opts.SourcesAddr, Handler: s.SourcesHandler(), ReadHeaderTimeout: 10 * time.Second}
		go func() {
			s.log.Info("serving source archives", "addr", s.opts.SourcesAddr)
			if err := srcSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
		}()
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s.log.Info("shutting down")
	if srcSrv != nil {
		_ = srcSrv.Shutdown(shutdownCtx)
	}
	return srv.Shutdown(shutdownCtx)
}

// SourcesHandler serves the uploaded source archives and nothing else: the
// build pods' view of the server.
func (s *Server) SourcesHandler() http.Handler {
	e := gin.New()
	e.Use(gin.Recovery(), s.requestLogger())
	e.GET("/api/sources/:name", s.serveSource)
	e.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return e
}

func (s *Server) routes() error {
	s.engine.Any("/_shpyrd/maintenance", projectMaintenanceResponse)
	// The edge (RFC-0033): what ingress-nginx and app hosts call.
	// /edge/auth arrives from ingress-nginx at the service name and names
	// its workspace in the query; the JWKS is the platform's, whatever the
	// host. Everything else is scoped to the workspace of its host.
	s.engine.GET("/edge/auth", s.edgeAuth)
	s.engine.GET("/.well-known/jwks.json", s.jwks)
	tenant := s.requireTenant()
	s.engine.GET(edgePathPrefix+"signin", tenant, s.edgeSignin)
	s.engine.GET(edgePathPrefix+"start", tenant, s.edgeStart)
	// A gate's host belongs to no workspace: its begin, callback and logout
	// are answered before the tenant middleware would refuse it (RFC-0083).
	// Only a gate's host begins; anywhere else there is nothing there.
	s.engine.GET(edgePathPrefix+"begin", s.onGateHost(s.gateBegin), s.notAGate)
	s.engine.GET(edgePathPrefix+"callback", s.onGateHost(s.gateCallback), tenant, s.edgeCallback)
	s.engine.GET(edgePathPrefix+"logout", s.onGateHost(s.gateLogout), tenant, s.edgeLogout)
	s.engine.GET(edgePathPrefix+"gate", tenant, s.gateOpen)

	s.engine.GET("/api/healthz", s.healthz)
	// Account self-service pages (RFC-0014): no session required.
	s.engine.GET("/account/reset", s.showResetPage)
	s.engine.POST("/account/reset", s.handleReset)
	s.engine.GET("/account/set-password", s.showSetPasswordPage)
	s.engine.POST("/account/set-password", s.handleSetPassword)
	pub := s.engine.Group("/api", tenant)
	pub.GET("/config", s.config)
	// Archives are content addressed (SHA-256) and fetched by build
	// instances, which cannot present the admin token.
	pub.GET("/sources/:name", s.serveSource)
	// The web terminal (RFC-0026) carries no header and may carry no cookie: a
	// browser cannot set headers on a WebSocket, and `shpyrd cluster dashboard`
	// signs in with a token in localStorage. So it skips s.auth() and the
	// one-time ticket is the whole credential; appShell takes the identity from
	// it and re-resolves the role itself.
	pub.GET("/projects/:slug/shell", s.appShell)
	// Sign-in (RFC-0007): the issuer redirects back to /api/auth/callback.
	pub.GET("/auth/providers", s.authProviders)
	login := newRateLimiter(30).middleware()
	pub.GET("/auth/login", login, s.authLogin)
	pub.GET("/auth/callback", login, s.authCallback)
	pub.GET("/auth/ticket", login, s.authTicket)
	pub.GET("/auth/signup-ticket", login, s.authSignupTicket) // a one-time sign-in from the signup
	pub.POST("/auth/password", login, s.authPassword)         // RFC-0012
	pub.POST("/auth/reset", s.requestReset)                   // RFC-0014: send reset link (public)
	pub.POST("/auth/token", login, s.authToken)               // the admin token as a session (RFC-0033)
	pub.GET("/invitations/:token", login, s.getInvitation)    // an invitation link, before signing in (RFC-0033)
	pub.GET("/auth/route", login, s.authRoute)                // the method a claimed email domain routes to
	pub.GET("/workspace/logo", s.workspaceLogo)               // the workspace's logo, for the login page too
	// The workspace's OAuth 2.1 server and MCP endpoint (RFC-0032). The
	// well-known documents sit at the root, with and without the resource
	// path (RFC 8414 §3.1, RFC 9728 §3.1); the endpoints are throttled
	// like sign-in.
	for _, p := range []string{"/.well-known/oauth-authorization-server", "/.well-known/oauth-authorization-server/mcp"} {
		s.engine.GET(p, tenant, s.oauthMetadata)
	}
	for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		s.engine.GET(p, tenant, s.protectedResourceMetadata)
	}
	// The CLI's browser sign-in (RFC-0052): a device code the CLI polls
	// and a page where the person approves it, with the dashboard session.
	pub.POST("/cli/device", login, s.cliDeviceStart)
	pub.POST("/cli/device/token", login, s.cliDevicePoll)
	s.engine.GET("/cli/activate", tenant, login, s.cliActivatePage)
	s.engine.POST("/cli/activate", tenant, login, s.cliActivateDecide)
	oauth := s.engine.Group("/oauth", tenant, login)
	oauth.POST("/register", s.oauthRegister)
	oauth.GET("/authorize", s.oauthAuthorize)
	oauth.POST("/authorize", s.oauthDecide)
	oauth.POST("/token", s.oauthToken)
	oauth.POST("/revoke", s.oauthRevoke)
	s.engine.POST("/mcp", tenant, s.mcpAuth(), s.mcpHandle)
	s.engine.GET("/mcp", tenant, s.mcpOther)
	s.engine.DELETE("/mcp", tenant, s.mcpOther)

	// Every protected route names the action it performs (RFC-0008); the
	// caller's roles decide.
	api := s.engine.Group("/api", tenant, s.auth())
	api.GET("/me", s.me)
	api.GET("/links", s.links)
	api.POST("/auth/logout", s.authLogout)
	// The cluster is the operator's (RFC-0033 phase 6): a workspace's
	// platform admin runs their workspace, not the machines under it. These
	// routes answer at the console (the implicit workspace) only.
	console := s.requireConsole()
	api.GET("/namespaces", console, s.require(authz.ClusterView), s.listNamespaces)
	api.GET("/helm/releases", console, s.require(authz.ClusterView), s.listHelmReleases)
	api.GET("/cluster", console, s.require(authz.ClusterView), s.clusterSummary)
	api.GET("/cluster/metrics", console, s.require(authz.ClusterView), s.clusterMetrics)
	api.GET("/cluster/registry", console, s.require(authz.ClusterAdmin), s.registryInfo) // RFC-0059
	api.POST("/cluster/registry/gc", console, s.require(authz.ClusterAdmin), s.registryGC)
	api.GET("/cluster/backups", console, s.require(authz.ClusterAdmin), s.listBackups) // RFC-0037
	api.POST("/cluster/backups", console, s.require(authz.ClusterAdmin), s.runBackup)
	api.GET("/sizes", s.getSizes) // any signed-in user: the size selector needs it
	api.PUT("/sizes", console, s.require(authz.ClusterAdmin), s.putSizes)
	api.PATCH("/cluster/settings", console, s.require(authz.ClusterAdmin), s.patchClusterSettings) // RFC-0078
	if !s.hasCapability("workspaces") {
		// The console lists the workspaces it hosts (RFC-0080); with the
		// workspaces capability the cloud layer serves the full routes.
		api.GET("/workspaces", console, s.require(authz.ClusterAdmin), s.listWorkspacesCore)
	}
	api.GET("/globals", console, s.require(authz.ClusterAdmin), s.getGlobals) // RFC-0016: the default workspace's, for the dashboard of before
	api.PUT("/globals", console, s.require(authz.ClusterAdmin), s.putGlobals)
	api.GET("/workspace/globals", s.require(authz.ClusterAdmin), s.getWorkspaceGlobals) // RFC-0016: the workspace's own
	api.PUT("/workspace/globals", s.require(authz.ClusterAdmin), s.putWorkspaceGlobals)
	api.GET("/workspace/drains", s.require(authz.ClusterAdmin), s.listWorkspaceDrains) // RFC-0023: the workspace's own
	api.POST("/workspace/drains", s.require(authz.ClusterAdmin), s.createWorkspaceDrain)
	api.DELETE("/workspace/drains/:name", s.require(authz.ClusterAdmin), s.deleteWorkspaceDrain)
	// Cluster log drains: every project's lines (RFC-0023).
	api.GET("/drains", console, s.require(authz.ClusterAdmin), s.listClusterDrains)
	api.POST("/drains", console, s.require(authz.ClusterAdmin), s.createClusterDrain)
	api.DELETE("/drains/:name", console, s.require(authz.ClusterAdmin), s.deleteClusterDrain)
	api.POST("/sources", s.uploadSource) // deploys check the project right when the App is updated
	// The workspace (RFC-0033): its name and the people it has seen.
	api.GET("/workspace", s.getWorkspace) // any signed-in user: the dashboard shows the name
	api.PATCH("/workspace", s.require(authz.ClusterAdmin), s.updateWorkspace)
	api.GET("/workspace/people", s.require(authz.ClusterAdmin), s.listPeople)
	api.DELETE("/workspace/people/:email", s.require(authz.ClusterAdmin), s.forgetPerson)
	api.PATCH("/workspace/people/:email", s.require(authz.ClusterAdmin), s.updatePerson)
	api.GET("/workspace/invitations", s.require(authz.ClusterAdmin), s.listInvitations)
	api.POST("/workspace/invitations", s.require(authz.ClusterAdmin), s.createInvitation)
	api.DELETE("/workspace/invitations/:id", s.require(authz.ClusterAdmin), s.deleteInvitation)
	api.POST("/invitations/:token/accept", s.acceptInvitation) // any signed-in person: the email must match

	// Billing (RFC-0075): usage and invoice preview (workspace admins).
	api.GET("/workspace/billing/current", s.billingAdmin, s.workspaceBillingCurrent)
	api.GET("/workspace/billing/invoices", s.billingAdmin, s.workspaceBillingInvoices)
	api.GET("/workspace/usage", s.require(authz.ClusterAdmin), s.workspaceUsage)
	api.GET("/projects/:slug/usage", s.require(authz.ProjectView), s.projectUsage)

	// Operator economics and plan management (cluster admins, console only).
	api.GET("/cluster/plans", console, s.require(authz.ClusterAdmin), s.listPlans)
	api.POST("/cluster/plans", console, s.require(authz.ClusterAdmin), s.createPlan)
	api.POST("/cluster/plans/:name/assign", console, s.require(authz.ClusterAdmin), s.assignPlan)
	api.GET("/cluster/plans/:name/versions", console, s.require(authz.ClusterAdmin), s.listPlanVersions)
	api.POST("/cluster/plans/:name/versions", console, s.require(authz.ClusterAdmin), s.addPlanVersion)
	api.GET("/cluster/economics", console, s.require(authz.ClusterAdmin), s.clusterEconomics)
	api.GET("/cluster/project-archives", console, s.require(authz.ClusterAdmin), s.clusterArchiveProjects)
	archives := api.Group("/cluster/project-archives/:projectID", console, s.require(authz.ClusterAdmin), s.clusterArchiveProject)
	archives.GET("", s.projectArchiveStatus)
	archives.GET("/placement", s.getProjectPlacement)
	archives.POST("/placement/measure", s.measureProjectPlacement)
	archives.POST("/move", s.moveProject)
	archives.DELETE("/retained-volumes/:volume", s.deleteRetainedMigrationDisk)
	archives.POST("/export", s.exportProjectArchive)
	archives.GET("/download", s.downloadProjectArchive)
	archives.POST("/restore", s.restoreProjectArchive)
	archives.POST("/recover", s.recoverProjectArchive)
	api.GET("/workspace/connections", s.listConnections) // the caller's connected assistants (RFC-0032)
	api.DELETE("/workspace/connections/:id", s.deleteConnection)
	api.GET("/workspace/domains", s.require(authz.ClusterAdmin), s.listWorkspaceDomains) // custom workspace domains (RFC-0033 names)
	api.POST("/workspace/domains", s.require(authz.ClusterAdmin), s.addWorkspaceDomain)
	api.POST("/workspace/domains/:host/verify", s.require(authz.ClusterAdmin), s.verifyWorkspaceDomain)
	api.PATCH("/workspace/domains/:host", s.require(authz.ClusterAdmin), s.updateWorkspaceDomain)
	api.DELETE("/workspace/domains/:host", s.require(authz.ClusterAdmin), s.deleteWorkspaceDomain)
	api.GET("/workspace/domain-claims", s.require(authz.ClusterAdmin), s.listDomainClaims)
	api.POST("/workspace/domain-claims", s.require(authz.ClusterAdmin), s.putDomainClaim)
	api.POST("/workspace/domain-claims/:domain/verify", s.require(authz.ClusterAdmin), s.verifyDomainClaim)
	api.DELETE("/workspace/domain-claims/:domain", s.require(authz.ClusterAdmin), s.deleteDomainClaim)
	api.GET("/workspace/grants", s.require(authz.ClusterAdmin), s.listAllMembers)
	api.GET("/workspace/export", s.require(authz.ClusterAdmin), s.exportWorkspace) // RFC-0037
	api.POST("/workspace/import", s.require(authz.ClusterAdmin), s.importWorkspace)
	api.GET("/teams", s.require(authz.ClusterAdmin), s.listTeams)
	api.POST("/teams", s.require(authz.ClusterAdmin), s.putTeam)
	api.PUT("/teams/:name", s.require(authz.ClusterAdmin), s.putTeam)
	api.DELETE("/teams/:name", s.require(authz.ClusterAdmin), s.deleteTeam)

	// Projects (RFC-0011): every path names the project by its slug; the
	// server derives the namespace (app-<slug>).
	api.GET("/projects", s.listApps) // filtered to visible projects
	api.POST("/projects", s.require(authz.ClusterCreate), s.createApp)
	api.GET("/projects/:slug", s.require(authz.ProjectView), s.getApp)
	api.GET("/project-archives/:slug", s.require(authz.ProjectDestroy), s.projectArchiveStatus)
	api.POST("/project-archives/:slug/export", s.require(authz.ProjectDestroy), s.exportProjectArchive)
	api.GET("/project-archives/:slug/download", s.require(authz.ProjectDestroy), s.downloadProjectArchive)
	api.POST("/project-archives/:slug/restore", s.require(authz.ProjectDestroy), s.restoreProjectArchive)
	api.POST("/project-archives/:slug/recover", s.require(authz.ProjectDestroy), s.recoverProjectArchive)
	api.PATCH("/projects/:slug", s.require(authz.ProjectConfig), s.updateApp)
	api.GET("/projects/:slug/icon", s.require(authz.ProjectView), s.projectIcon)
	api.PUT("/projects/:slug/icon", s.require(authz.ProjectConfig), s.putProjectIcon)
	api.DELETE("/projects/:slug/icon", s.require(authz.ProjectConfig), s.deleteProjectIcon)
	api.POST("/projects/:slug/rename", s.require(authz.ProjectConfig), s.renameApp)
	api.DELETE("/projects/:slug", s.require(authz.ProjectDestroy), s.deleteApp)
	api.POST("/projects/:slug/deploy", s.require(authz.ProjectDeploy), s.deployApp)
	api.GET("/projects/:slug/logs", s.require(authz.ProjectView), s.appLogs)
	api.GET("/projects/:slug/builds", s.require(authz.ProjectView), s.listBuilds)
	api.GET("/projects/:slug/builds/:build/logs", s.require(authz.ProjectView), s.buildLogs)
	api.GET("/projects/:slug/secrets", s.require(authz.ProjectView), s.appSecretKeys)
	api.PUT("/projects/:slug/secrets", s.require(authz.ProjectConfig), s.updateAppSecrets)
	api.GET("/projects/:slug/metrics", s.require(authz.ProjectView), s.appMetrics)
	api.POST("/projects/:slug/scale", s.require(authz.ProjectScale), s.scaleApp)
	api.POST("/projects/:slug/resize", s.require(authz.ProjectScale), s.resizeApp)
	api.POST("/projects/:slug/processes", s.require(authz.ProjectScale), s.applyProcesses)
	api.POST("/projects/:slug/rollback", s.require(authz.ProjectDeploy), s.rollbackApp)
	api.POST("/projects/:slug/redeploy", s.require(authz.ProjectDeploy), s.redeployApp)
	api.PUT("/projects/:slug/exposure", s.require(authz.ProjectDeploy), s.setExposure)
	api.PUT("/projects/:slug/access", s.require(authz.ProjectMembers), s.setAccess) // RFC-0033: who may open the app
	api.GET("/projects/:slug/allow", s.require(authz.ProjectView), s.listAllow)     // RFC-0033: who may reach from inside
	api.PUT("/projects/:slug/allow", s.require(authz.ProjectMembers), s.setAllow)
	api.POST("/projects/:slug/preview", s.require(authz.ProjectDeploy), s.previewApp) // "Open as"
	api.GET("/launcher", s.launcher)                                                  // the apps the caller may open
	api.GET("/tokens", s.listTokens)                                                  // RFC-0031: the caller's tokens
	api.POST("/tokens", s.createToken)
	api.DELETE("/tokens/:id", s.deleteToken)
	api.GET("/projects/:slug/domains", s.require(authz.ProjectView), s.listDomains) // RFC-0034
	api.POST("/projects/:slug/domains", s.require(authz.ProjectConfig), s.addDomain)
	api.DELETE("/projects/:slug/domains/:host", s.require(authz.ProjectConfig), s.removeDomain)
	api.GET("/projects/:slug/audit", s.require(authz.ProjectView), s.appAudit)
	api.GET("/projects/:slug/instances", s.require(authz.ProjectExec), s.listInstances) // RFC-0026
	// RFC-0026; the socket itself is on pub. The throttle comes first because it
	// is the cheaper check and because minting is what the socket costs: see
	// throttleShellMints.
	api.POST("/projects/:slug/shell/ticket", s.throttleShellMints(), s.require(authz.ProjectExec), s.mintShellTicket)
	// One-off commands over the API (RFC-0052): the instance is created
	// here and attached through the socket above.
	api.POST("/projects/:slug/run", s.throttleShellMints(), s.require(authz.ProjectExec), s.runApp)
	// A shell into a resource (`shpyrd pg psql`, `shpyrd redis cli`): the
	// extension that owns the kind names the pod and the command.
	api.POST("/projects/:slug/resources/:kind/:name/shell/ticket", s.throttleShellMints(), s.require(authz.ProjectExec), s.mintResourceShellTicket)
	// Project resources (RFC-0003/0006) live in the project namespace.
	api.GET("/projects/:slug/resources", s.require(authz.ProjectView), s.listProjectResources)
	api.POST("/projects/:slug/resources", s.require(authz.ProjectResource), s.createResource)
	api.DELETE("/projects/:slug/resources/:kind/:name", s.require(authz.ProjectResource), s.deleteResource)
	// Postgres sleep (RFC-0075): policy, suspend and resume.
	api.PATCH("/projects/:slug/resources/postgres/:name/sleep", s.require(authz.ProjectResource), s.patchPostgresSleep)
	api.POST("/projects/:slug/resources/postgres/:name/suspend", s.require(authz.ProjectResource), s.suspendPostgres)
	api.POST("/projects/:slug/resources/postgres/:name/resume", s.require(authz.ProjectResource), s.resumePostgres)
	api.POST("/projects/:slug/bindings", s.require(authz.ProjectResource), s.attachResource)
	api.DELETE("/projects/:slug/bindings/:kind/:name", s.require(authz.ProjectResource), s.detachResource)
	api.GET("/projects/:slug/volumes", s.require(authz.ProjectView), s.listVolumes)
	api.POST("/projects/:slug/volumes", s.require(authz.ProjectResource), s.createVolume)
	api.PUT("/projects/:slug/volumes/:name", s.require(authz.ProjectResource), s.resizeVolume)
	api.DELETE("/projects/:slug/volumes/:name", s.require(authz.ProjectResource), s.deleteVolume)
	api.GET("/projects/:slug/volumes/:name/snapshots", s.require(authz.ProjectView), s.listSnapshots)
	api.POST("/projects/:slug/volumes/:name/snapshots", s.require(authz.ProjectResource), s.createSnapshot)
	api.DELETE("/projects/:slug/volumes/:name/snapshots/:snap", s.require(authz.ProjectResource), s.deleteSnapshot)
	api.POST("/projects/:slug/volumes/:name/restore", s.require(authz.ProjectResource), s.restoreVolume)
	// Project log drains (RFC-0023).
	api.GET("/projects/:slug/drains", s.require(authz.ProjectView), s.listProjectDrains)
	api.POST("/projects/:slug/drains", s.require(authz.ProjectResource), s.createProjectDrain)
	api.DELETE("/projects/:slug/drains/:name", s.require(authz.ProjectResource), s.deleteProjectDrain)
	api.GET("/projects/:slug/members", s.require(authz.ProjectMembers), s.listMembers)
	api.POST("/projects/:slug/members", s.require(authz.ProjectMembers), s.addMember)
	api.DELETE("/projects/:slug/members/:name", s.require(authz.ProjectMembers), s.removeMember)

	// Extensions mount their routes and register login providers. The one
	// that sends mail (RFC-0013) is asked first, so the others find it in
	// their deps.
	for _, x := range s.opts.Extensions {
		if mp, ok := x.(ext.MailProvider); ok && s.mailer == nil {
			s.mailer = mp.Mailer(s.deps())
		}
	}
	deps := s.deps()
	for _, x := range s.opts.Extensions {
		// Extensions manage cluster-level things (accounts, connectors,
		// storage): the operator's, so the console's.
		if err := x.Routes(routeGroups{pub: pub, api: api, wsAdmin: api.Group("", s.require(authz.ClusterAdmin)), admin: api.Group("", console, s.require(authz.ClusterAdmin)), platform: s.engine.Group("/api"), s: s}, deps); err != nil {
			return fmt.Errorf("extension %s: %w", x.Name(), err)
		}
	}

	// Gates and links (RFC-0083), once every extension has its deps.
	if err := s.collectGates(deps); err != nil {
		return err
	}

	if s.opts.UI != nil {
		s.engine.NoRoute(s.serveUI())
	} else {
		s.engine.NoRoute(func(c *gin.Context) {
			if s.customError(c) {
				return
			}
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		})
	}
	return nil
}

// auth accepts the admin token as a Bearer token or in X-Shpyrd-Token (the
// API server's service proxy strips Authorization when the CLI uploads
// through it).
func (s *Server) auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.opts.Token == "" && !s.opts.TokenDisabled {
			c.Next()
			return
		}
		// Try a per-user API token first (shp_... prefix, RFC-0031); it
		// has its own identity and roles, so we skip the admin token check.
		if s.identifyWithToken(c) {
			c.Next()
			return
		}
		// Then one of our OAuth access tokens (RFC-0032): an assistant
		// acting for a person, within the token's scope.
		if s.identifyWithOAuth(c) {
			if !c.IsAborted() {
				c.Next()
			}
			return
		}
		tok := c.GetHeader("X-Shpyrd-Token")
		if h := c.GetHeader("Authorization"); tok == "" && strings.HasPrefix(strings.ToLower(h), "bearer ") {
			tok = strings.TrimSpace(h[7:])
		}
		if tok != "" {
			if s.opts.TokenDisabled {
				abort(c, http.StatusUnauthorized, errors.New("the admin token is disabled on this cluster; sign in with your account"))
				return
			}
			// Wrong tokens are throttled per client and audited; while a
			// client is throttled even the right token is refused, which
			// blunts brute force.
			ip := c.ClientIP()
			if s.tokenFailures.exhausted(ip) {
				c.Header("Retry-After", "60")
				abort(c, http.StatusTooManyRequests, errors.New("too many failed token attempts; try again in a minute"))
				return
			}
			if subtle.ConstantTimeCompare([]byte(tok), []byte(s.opts.Token)) != 1 {
				s.tokenFailures.allow(ip)
				s.log.Warn("invalid admin token", "remote", ip, "path", c.Request.URL.Path)
				s.auditAnonymous(c, "auth.token_failed", c.Request.URL.Path)
				c.Header("WWW-Authenticate", `Bearer realm="shpyrd"`)
				abort(c, http.StatusUnauthorized, errors.New("missing or invalid token"))
				return
			}
			ext.SetIdentity(c, ext.Identity{Subject: "admin-token", Name: "admin token", Provider: "token", Admin: true})
			c.Next()
			return
		}
		// A signed-in user (RFC-0007).
		ok, err := s.sessionAuth(c)
		if err != nil {
			abort(c, http.StatusForbidden, err)
			return
		}
		if !ok {
			c.Header("WWW-Authenticate", `Bearer realm="shpyrd"`)
			abort(c, http.StatusUnauthorized, errors.New("missing or invalid token"))
			return
		}
		c.Next()
	}
}

func (s *Server) config(c *gin.Context) {
	pub := s.opts.Public
	pub.Auth = s.authConfigFor(c)
	pub.Door = tenancy.RealmWorkspace
	if s.atConsole(c) {
		pub.Door = tenancy.RealmConsole // the console application, at the console host and for internal callers
	}
	pub.ConsoleURL = s.consoleURL()
	if t, err := s.door(c); err == nil && t.Workspace != nil {
		ws := t.Workspace
		pub.Workspace = &WorkspaceRef{Slug: ws.Slug, Name: ws.Name, Address: ws.Address, OwnedByOperator: ws.OwnedByOperator(), Branding: brandingView(ws)}
		pub.Domain = s.appsDomainOf(ws)
		pub.DashboardURL = s.dashboardURLOf(ws)
	}
	pub.Volumes = VolumesConfig{NodeLocal: install.ProjectStorageClass(s.vars) == controller.LocalStorageClass, MinSize: install.ProjectVolumeMinSize(s.vars), Snapshots: s.vars(install.VarSnapshotClass) != ""}
	if s.store != nil {
		if v, err := s.store.GetSetting(c.Request.Context(), store.SettingDefaultWorkspaceID); err == nil && v != "" {
			pub.DefaultWorkspaceID = v
		}
	}
	if ch := s.vars(install.VarDashboardURL); ch != "" {
		if u, err := url.Parse(ch); err == nil {
			pub.ConsoleHost = u.Host
		}
	}
	c.JSON(http.StatusOK, pub)
}

// The two applications (RFC-0080), as the UI build lays them out.
func (s *Server) requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		if strings.HasPrefix(c.Request.URL.Path, "/api/") && c.Request.URL.Path != "/api/healthz" {
			s.log.Info("request",
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"status", c.Writer.Status(),
				"duration", time.Since(start).Round(time.Millisecond).String(),
			)
		}
	}
}

func abort(c *gin.Context, status int, err error) {
	c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
}

// atSignInHost says the request came to the sign-in service's host: the
// root of it, which its Ingress sends here to be answered with the mark
// alone. The console's own host is never it.
func (s *Server) atSignInHost(c *gin.Context) bool {
	host := hostOnly(s.opts.SignInHost)
	return host != "" && host != hostOnly(consoleHostOf(s.opts.Public.DashboardURL)) && hostOnly(c.Request.Host) == host
}

// placeholderHTML is what a binary built without the applications
// answers with.
var placeholderHTML = pages.HTML(pages.Nothing, pages.Page{
	Title: "The applications are not here.",
	Text:  "They were not built into this binary. Run `make ui` and build it again, or use the API at /api/healthz.",
})

// consoleHostOf is the host (with port) of the console's URL.
func consoleHostOf(dashboardURL string) string {
	if u, err := url.Parse(dashboardURL); err == nil && u.Host != "" {
		return u.Host
	}
	return dashboardURL
}
