// Command shpyrd-server runs the shpyrd API, the App controller and serves the
// dashboard from inside the cluster (or locally against a kubeconfig).
// Package server wires the platform server: the API, the controllers, the
// control-plane store, extensions. cmd/shpyrd-server is the open-source
// binary built on it; a binary that adds the cloud layer (RFC-0033 phase 8)
// imports this package and passes its own Options.
package server

import (
	"context"
	"net/url"
	"time"

	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/buildtrust"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ext/all"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/objectgateway"
	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
	"github.com/shpyrd-io/shpyrd/pkg/projectarchive"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
	"github.com/shpyrd-io/shpyrd/pkg/ui"
	"github.com/shpyrd-io/shpyrd/pkg/version"
)

// Options is what a binary built on the core chooses (RFC-0033's open-core
// boundary). The zero value is the open-source platform.
type Options struct {
	// InternalExposure supplies workspace networking defaults; nil permits self-hosted apps.
	InternalExposure api.InternalExposurePolicy
	// Tenancy builds the resolver mapping request hosts to workspaces. Nil:
	// the platform runs one workspace, which every host is, and the store
	// shows no other.
	Tenancy func(st store.Store, domain, dashboardURL string) tenancy.Resolver
	// Realms decides which login methods each workspace offers. Nil: all.
	Realms api.Realms
	// Capabilities names what this server offers beyond the core, for
	// GET /api/config ("workspaces", ...).
	Capabilities []string
	// Extensions are added to those enabled by SHPYRD_EXTENSIONS: their
	// controllers, routes and login providers are registered like any.
	Extensions []ext.Extension
	// Pages is what this binary writes into every page of the applications
	// (HTML before </head> and before </body>) and the origins those
	// additions may load scripts from, connect to and show images from.
	// The core adds nothing.
	Pages api.PageAdditions
}

// Main parses flags and the environment, runs the server and exits on
// error. It is the whole main() of cmd/shpyrd-server.
func Main(opts Options) {
	if len(os.Args) > 1 && os.Args[1] == "project-volume" {
		if err := projectarchive.VolumeMain(os.Args[2:], os.Stdin, os.Stdout, os.Stderr); err != nil {
			slog.Error("project volume operation failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "object-gateway" {
		if err := objectgateway.Main(); err != nil {
			slog.Error("object gateway failed", "error", err)
			os.Exit(1)
		}
		return
	}
	// `shpyrd-server ui-export <dir>`: write the applications built into
	// this binary to a directory, then exit. The server's init container
	// runs it, so the server reads its applications from a directory in
	// every install (SHPYRD_UI_DIR), and an image holding only the
	// applications can take the server's place there (SHPYRD_UI_IMAGE).
	if len(os.Args) > 2 && os.Args[1] == "ui-export" {
		if err := ui.Export(os.Args[2]); err != nil {
			slog.Error("ui-export failed", "dir", os.Args[2], "err", err.Error())
			os.Exit(1)
		}
		return
	}
	// `shpyrd-server backup`: one platform backup, then exit (RFC-0037).
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		if err := runBackup(logger); err != nil {
			logger.Error("backup failed", "err", err.Error())
			os.Exit(1)
		}
		return
	}
	// A private FlagSet: controller-runtime registers its own --kubeconfig
	// on flag.CommandLine at init time.
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	var (
		addr        = fs.String("addr", envOr("SHPYRD_ADDR", ":8080"), "listen address")
		metricsAddr = fs.String("metrics-addr", envOr("SHPYRD_METRICS_ADDR", ":8081"), "controller metrics address (0 to disable)")
		kubeconfig  = fs.String("kubeconfig", os.Getenv("KUBECONFIG"), "path to kubeconfig (defaults to in-cluster, then ~/.kube/config)")
		kubeCtx     = fs.String("context", "", "kubeconfig context")
		dataDir     = fs.String("data-dir", envOr("SHPYRD_DATA_DIR", "/data"), "directory for uploaded source archives")
		internalURL = fs.String("internal-url", os.Getenv("SHPYRD_INTERNAL_URL"), "cluster-internal URL of this server (for kpack blob sources)")
		noControl   = fs.Bool("no-controller", os.Getenv("SHPYRD_NO_CONTROLLER") != "", "serve the API only")
		leaderElect = fs.Bool("leader-elect", os.Getenv("SHPYRD_LEADER_ELECT") != "", "enable leader election for the controller")
		debug       = fs.Bool("debug", os.Getenv("SHPYRD_DEBUG") != "", "verbose logging")
	)
	_ = fs.Parse(os.Args[1:])

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)
	ctrl.SetLogger(logr.FromSlogHandler(logger.Handler()))

	if err := run(runOptions{
		addr: *addr, metricsAddr: *metricsAddr, kubeconfig: *kubeconfig, kubeCtx: *kubeCtx,
		dataDir: *dataDir, internalURL: *internalURL, controller: !*noControl, leaderElect: *leaderElect,
		opts: opts,
	}, logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

type runOptions struct {
	addr, metricsAddr, kubeconfig, kubeCtx string
	dataDir, internalURL                   string
	controller, leaderElect                bool
	opts                                   Options
}

func run(o runOptions, logger *slog.Logger) error {
	k, err := kube.Connect(kube.Options{Kubeconfig: o.kubeconfig, Context: o.kubeCtx})
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}
	logger.Info("connected", "host", k.Config.Host, "namespace", k.Namespace)

	// Build pods fetch source archives from the sources port, which the
	// server's NetworkPolicy opens to project namespaces; the API and the
	// edge stay with the front doors.
	internalURL := o.internalURL
	if internalURL == "" {
		internalURL = fmt.Sprintf("http://shpyrd-server.%s.svc:%d", k.Namespace, api.SourcesPort)
	}
	domain := envOr("SHPYRD_DOMAIN", "127.0.0.1.nip.io")
	httpsPort := envOr("SHPYRD_HTTPS_PORT", "443")
	grafana := "https://grafana." + domain
	if httpsPort != "443" {
		grafana += ":" + httpsPort
	}
	var prom *api.PromClient
	if u := envOr("SHPYRD_PROMETHEUS_URL", "http://monitoring-prometheus.monitoring.svc:9090"); u != "" && u != "off" {
		prom = api.NewPromClient(u)
	}
	dashboard := envOr("SHPYRD_DASHBOARD_URL", "https://shpyrd."+domain)
	if os.Getenv("SHPYRD_DASHBOARD_URL") == "" && httpsPort != "443" {
		dashboard += ":" + httpsPort
	}
	// Extensions enabled on this cluster (RFC-0002): recorded by the
	// installer in SHPYRD_EXTENSIONS.
	extensions, unknown := all.Enabled(os.Getenv("SHPYRD_EXTENSIONS"))
	for _, name := range unknown {
		logger.Warn("unknown extension in SHPYRD_EXTENSIONS, ignored", "extension", name)
	}
	extensions = append(extensions, o.opts.Extensions...)
	if len(extensions) > 0 {
		logger.Info("extensions enabled", "extensions", ext.Names(extensions))
	}
	// The in-cluster registry's garbage collector (RFC-0059): the API starts
	// it on demand, the controller manager runs its schedule on the leader.
	var registryGC *controller.RegistryGC
	if os.Getenv("SHPYRD_REGISTRY_IP") != "" && o.controller {
		registryGC = &controller.RegistryGC{
			Namespace: k.Namespace,
			Schedule:  envOr("SHPYRD_REGISTRY_GC", controller.DefaultRegistryGCSchedule),
			Image:     envOr("SHPYRD_REGISTRY_IMAGE", "docker.io/library/registry:3"),
			Logger:    logger,
		}
	}

	// The control-plane store (RFC-0033): teams, grants, people, the
	// workspace. Postgres from SHPYRD_DATABASE_URL (the control-plane-db
	// component or a managed database); memory only for development.
	// The operator's default workspace (RFC-0078, RFC-0080): slug and
	// address from the installer; the address is the platform domain in the
	// open-source layout, <slug>.<workspaces domain> when the console holds
	// the apex.
	st, err := openStore(logger, k, defaultWorkspaceFromEnv(domain, dashboard))
	if err != nil {
		return err
	}
	defer st.Close()
	// The open-source platform runs one workspace: the server and the
	// controllers see only it, whatever else the database holds.
	if o.opts.Tenancy == nil {
		st = store.OneWorkspace(st)
	}

	// The RBAC mirror runs in the controller manager; the API pokes it
	// after every team or grant write. The workspace reconciler publishes
	// front doors; the API pokes it when a workspace changes.
	memberships := &controller.MembershipReconciler{Store: st}
	workspaces := &controller.WorkspaceReconciler{Store: st}

	// The open-source platform resolves every host to its one workspace.
	var resolver tenancy.Resolver = &tenancy.Single{Store: st, ConsoleHost: hostOf(dashboard), DefaultSlug: defaultSlugOf(st)}
	if o.opts.Tenancy != nil {
		resolver = o.opts.Tenancy(st, domain, dashboard)
	}

	sources := &api.SourceStore{Dir: o.dataDir, BaseURL: internalURL, SigningKey: []byte(os.Getenv("SHPYRD_SOURCES_SIGNING_KEY"))}
	if bucket := os.Getenv(install.VarSourcesBucket); bucket != "" {
		sources.Bucket, err = objectstore.NewS3(os.Getenv(install.VarSourcesEndpoint), os.Getenv(install.VarSourcesRegion), bucket, "sources",
			os.Getenv("SHPYRD_SOURCES_AWS_ACCESS_KEY_ID"), os.Getenv("SHPYRD_SOURCES_AWS_SECRET_ACCESS_KEY"))
		if err != nil {
			return fmt.Errorf("source storage: %w", err)
		}
	}
	srv, err := api.New(k, api.Options{
		Addr:             o.addr,
		Store:            st,
		Tenancy:          resolver,
		Layout:           layoutFromEnv(),
		SignInHost:       hostOf(envOr("SHPYRD_AUTH_URL", "")),
		Realms:           o.opts.Realms,
		Capabilities:     o.opts.Capabilities,
		InternalExposure: o.opts.InternalExposure,
		MembershipChanged: func() {
			if o.controller {
				memberships.Notify()
			}
		},
		WorkspacesChanged: func() {
			if o.controller {
				workspaces.Notify()
			}
		},
		UI:             uiFiles(logger),
		Pages:          o.opts.Pages,
		Sources:        sources,
		SourcesAddr:    fmt.Sprintf(":%d", api.SourcesPort),
		TrustedProxies: trustedProxies(os.Getenv("SHPYRD_POD_CIDR")),
		Token:          strings.TrimSpace(os.Getenv("SHPYRD_ADMIN_TOKEN")),
		TokenDisabled:  strings.TrimSpace(os.Getenv("SHPYRD_ADMIN_TOKEN_DISABLED")) == "true",
		Prometheus:     prom,
		Extensions:     extensions,
		IngressService: envOr("SHPYRD_INGRESS_SERVICE", "ingress-nginx-controller.ingress-nginx.svc:443"),
		Public: api.PublicConfig{
			Version:      version.Version,
			Domain:       domain,
			HTTPSPort:    httpsPort,
			GrafanaURL:   grafana,
			DashboardURL: dashboard,
		},
		Logger:     logger,
		RegistryGC: gcOrNil(registryGC),
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return srv.Run(ctx) })

	// Build pods trust the platform CA through this webhook (RFC-0059).
	if addr := os.Getenv("SHPYRD_WEBHOOK_ADDR"); addr != "" {
		hook, err := buildtrust.New(buildtrust.Options{
			Addr:   addr,
			TLSDir: envOr("SHPYRD_WEBHOOK_TLS_DIR", "/etc/shpyrd/webhook-tls"),
			Bundle: envOr("SHPYRD_CA_BUNDLE", "shpyrd-ca-bundle"),
			Logger: logger,
		})
		if err != nil {
			return err
		}
		g.Go(func() error { return hook.Run(ctx) })
	}

	if o.controller {
		runControllers := func() error {
			mgr, err := newManager(k, o, memberships, workspaces, srv.Deps())
			if err != nil {
				return err
			}
			// Metering loop (RFC-0075) — runs on the leader alongside the
			// registry GC; needs the store and the Prometheus client.
			if prom != nil {
				metering := &controller.MeteringLoop{Store: memberships.Store, Prom: prom, Client: mgr.GetClient()}
				if err := mgr.Add(metering); err != nil {
					return fmt.Errorf("metering loop: %w", err)
				}
			}
			if registryGC != nil {
				registryGC.Client = mgr.GetClient()
				registryGC.Reader = mgr.GetAPIReader()
				if err := mgr.Add(registryGC); err != nil {
					return fmt.Errorf("registry garbage collector: %w", err)
				}
			}
			if os.Getenv(install.VarRegistryIP) != "" {
				// The in-cluster registry restarts when its certificate is renewed (RFC-0059).
				rotation := &controller.RegistryRotation{Client: mgr.GetClient(), Reader: mgr.GetAPIReader(), Namespace: k.Namespace, Secret: "registry-tls", Deploy: "registry"}
				if err := mgr.Add(rotation); err != nil {
					return fmt.Errorf("registry rotation: %w", err)
				}
			}
			logger.Info("starting controller manager")
			return mgr.Start(ctx)
		}
		// The API keeps answering whatever happens to the controllers: a
		// lost leader lease (a slow control plane, an upgrade) restarts the
		// manager here instead of the process, and another replica may hold
		// the lease meanwhile.
		g.Go(func() error {
			for {
				err := runControllers()
				if ctx.Err() != nil {
					return nil
				}
				logger.Error("controller manager stopped; restarting in 10s", "err", err)
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(10 * time.Second):
				}
			}
		})
	}
	return g.Wait()
}

// openStore connects to the control-plane database, migrates it, and
// imports the Team and ProjectMember objects of installs that predate it.
func openStore(logger *slog.Logger, k *kube.Client, def store.DefaultWorkspaceSpec) (store.Store, error) {
	url := strings.TrimSpace(os.Getenv("SHPYRD_DATABASE_URL"))
	if url == "" {
		if os.Getenv("SHPYRD_DEV_MEMORY_STORE") == "" {
			return nil, fmt.Errorf("SHPYRD_DATABASE_URL is not set: the control-plane database is required (the control-plane-db component provides it; SHPYRD_DEV_MEMORY_STORE=1 runs without one for development, losing teams on restart)")
		}
		logger.Warn("running with an in-memory control-plane store: teams and grants are lost on restart")
		return store.NewMemory(), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var st *store.Postgres
	var err error
	for attempt := 1; ; attempt++ {
		st, err = store.Open(ctx, url)
		if err == nil {
			break
		}
		if attempt >= 12 {
			return nil, err
		}
		logger.Info("waiting for the control-plane database", "attempt", attempt, "err", err.Error())
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(5 * time.Second):
		}
	}
	if err := st.Migrate(ctx, def); err != nil {
		st.Close()
		return nil, fmt.Errorf("control-plane database: %w", err)
	}
	if teams, grants, err := store.ImportCRDs(ctx, k.Dynamic, st, def.Slug); err != nil {
		logger.Warn("importing teams and members from Kubernetes objects failed; they stay where they are", "err", err.Error())
	} else if teams+grants > 0 {
		logger.Info("imported teams and members into the control-plane database", "teams", teams, "grants", grants)
	}
	return st, nil
}

func newManager(k *kube.Client, o runOptions, memberships *controller.MembershipReconciler, workspaces *controller.WorkspaceReconciler, apiDeps ext.Deps) (ctrl.Manager, error) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := shpyrdv1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	// Lease timings roomier than Kubernetes' defaults (15s/10s/2s): a
	// control plane that answers slowly for half a minute must not cost the
	// lease, since losing it stops every controller.
	lease, renew, retry := 60*time.Second, 40*time.Second, 5*time.Second
	mgr, err := ctrl.NewManager(k.Config, ctrl.Options{
		Scheme:                        scheme,
		Metrics:                       metricsserver.Options{BindAddress: o.metricsAddr},
		HealthProbeBindAddress:        "0",
		LeaderElection:                o.leaderElect,
		LeaderElectionID:              "shpyrd-server",
		LeaderElectionNamespace:       k.Namespace,
		LeaderElectionReleaseOnCancel: true,
		LeaseDuration:                 &lease,
		RenewDeadline:                 &renew,
		RetryPeriod:                   &retry,
		// A lost lease restarts the manager in this process (runControllers);
		// controller-runtime remembers controller names process-wide, so
		// without this every restart failed with "controller with name app
		// already exists" and no controller ran again until the pod did.
		Controller: ctrlconfig.Controller{SkipNameValidation: ptr.To(true)},
	})
	if err != nil {
		return nil, fmt.Errorf("controller manager: %w", err)
	}
	enabledExts, _ := all.Enabled(os.Getenv("SHPYRD_EXTENSIONS"))
	enabledExts = append(enabledExts, o.opts.Extensions...)
	var bindable []schema.GroupVersionKind
	for _, t := range all.BindableTypes(enabledExts) {
		bindable = append(bindable, schema.GroupVersionKind{Group: t.Group, Version: t.Version, Kind: t.Kind})
	}
	workspaceCache := &tenancy.Addresses{Store: memberships.Store, DefaultSlug: defaultSlugOf(memberships.Store), Layout: layoutFromEnv()}
	controller.DefaultWorkspace = func() string {
		if ws := workspaceCache.Workspace(""); ws != nil {
			return ws.Slug
		}
		return store.DefaultWorkspace
	}
	rec := &controller.AppReconciler{
		BindableTypes: bindable,
		Kube:          k.Kube,
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		Recorder:      mgr.GetEventRecorderFor("shpyrd"),
		Config: controller.Config{
			Domain:                os.Getenv("SHPYRD_DOMAIN"),
			WorkspaceDomain:       workspaceCache.Domain,
			WorkspaceAddress:      workspaceCache.Address,
			WorkspaceExtraDomains: workspaceCache.ExtraDomains,
			WorkspaceAppHosts:     workspaceCache.AppHosts,
			Layout:                workspaceCache.Layout,
			WorkspaceLimits:       workspaceCache.Limits,
			WorkspaceSleepDefault: workspaceCache.SleepDefault,
			SleepAllowed:          apiDeps.SleepAllowed,
			WorkspaceSuspended:    workspaceCache.Suspended,
			WorkspaceID:           workspaceCache.ID,
			Projects:              memberships.Store,
			DashboardURL:          envOr("SHPYRD_DASHBOARD_URL", ""),
			HTTPSPort:             os.Getenv("SHPYRD_HTTPS_PORT"),
			RegistryHost:          os.Getenv("SHPYRD_REGISTRY_HOST"),
			ClusterIssuer:         os.Getenv("SHPYRD_CLUSTER_ISSUER"),
			WorkspaceCertIssuer:   os.Getenv("SHPYRD_WORKSPACE_CERT_ISSUER"),
			IngressClass:          os.Getenv("SHPYRD_INGRESS_CLASS"),
			SystemNamespace:       k.Namespace,
			BuildKitImage:         os.Getenv("SHPYRD_BUILDKIT_IMAGE"),
			PodCIDR:               os.Getenv("SHPYRD_POD_CIDR"),
			RegistrySecret:        os.Getenv("SHPYRD_REGISTRY_SECRET"),
			RegistryInsecure:      registryInsecure(os.Getenv("SHPYRD_REGISTRY_INSECURE"), os.Getenv("SHPYRD_REGISTRY_HOST")),
			CABundle:              envOr("SHPYRD_CA_BUNDLE", "shpyrd-ca-bundle"),
			RegistryDeletes:       os.Getenv("SHPYRD_REGISTRY_IP") != "",
			BuildCacheRegistry:    os.Getenv("SHPYRD_BUILD_CACHE_REGISTRY"),
			AppsPool:              os.Getenv("SHPYRD_APPS_POOL"),
			PlatformPool:          os.Getenv("SHPYRD_PLATFORM_POOL"),
			WildcardTLS:           os.Getenv("SHPYRD_WILDCARD_TLS") == "true",
			IngressClassExternal:  envOr("SHPYRD_INGRESS_CLASS_EXTERNAL", "nginx"),
			IngressClassInternal:  envOr("SHPYRD_INGRESS_CLASS_INTERNAL", "nginx-internal"),
			InternalLBAddress:     internalLBAddress(k),
			ExternalLBAddress:     externalLBAddress(k),
			PublicChecks:          publishesNames(os.Getenv(install.VarDNSProvider), os.Getenv(install.VarWildcardTLS)),
		},
		// The external address may not exist yet at start (first install):
		// look it up when a custom domain needs it.
		LookupLB: func(context.Context) string { return externalLBAddress(k) },
	}
	if err := rec.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("app controller: %w", err)
	}
	drains := &controller.LogDrainReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Recorder: mgr.GetEventRecorderFor("shpyrd"), SystemNamespace: k.Namespace, Kube: k.Kube}
	if err := drains.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("log drain controller: %w", err)
	}
	if err := (&controller.LocalStorageProtection{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("local data protection: %w", err)
	}
	volumes := &controller.VolumeReconciler{
		Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Recorder: mgr.GetEventRecorderFor("shpyrd"),
		// Profile storage (RFC-0060).
		DefaultClass: install.ProjectStorageClass(os.Getenv), SharedClass: install.ProjectSharedStorageClass(os.Getenv), SnapshotClass: os.Getenv(install.VarSnapshotClass),
	}
	if err := volumes.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("volume controller: %w", err)
	}
	memberships.Client, memberships.Scheme = mgr.GetClient(), mgr.GetScheme()
	if err := memberships.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("membership controller: %w", err)
	}
	// Front doors of explicit workspaces (RFC-0033 phase 6); nothing to do
	// while there is one workspace.
	workspaces.Client, workspaces.Scheme, workspaces.Config, workspaces.LookupLB = mgr.GetClient(), mgr.GetScheme(), rec.Config, rec.LookupLB
	workspaces.Forget = workspaceCache.Forget
	workspaces.NamesChanged = rec.RequeueWorkspace
	if err := workspaces.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("workspace controller: %w", err)
	}
	if err := mgr.Add(workspaces); err != nil {
		return nil, fmt.Errorf("workspace controller: %w", err)
	}
	// Extensions' controllers get what their routes got from the API
	// server — the store, the mailer, the invitation hooks, the full
	// workspacesChanged — with the manager's cached client in place of the
	// API's: a loop that invites or mails runs in the same process as the
	// routes that do, whichever replica leads.
	deps := apiDeps
	deps.Kube, deps.Client, deps.SystemNamespace, deps.Vars = k, mgr.GetClient(), k.Namespace, os.Getenv
	if deps.Store == nil {
		deps.Store = memberships.Store
	}
	if deps.WorkspacesChanged == nil {
		deps.WorkspacesChanged = workspaces.Notify
	}
	for _, x := range enabledExts {
		if err := x.Register(mgr, deps); err != nil {
			return nil, fmt.Errorf("extension %s: %w", x.Name(), err)
		}
	}
	return mgr, nil
}

// externalLBAddress reads the public front door's address, what a custom
// domain's A record points at (RFC-0034): the static addresses the profile
// reserved (SHPYRD_LB_IP, comma-separated on AWS), else the ingress-nginx
// Service's.
func externalLBAddress(k *kube.Client) string {
	if fixed := os.Getenv(install.VarLBIP); fixed != "" {
		return fixed
	}
	svc, err := k.Kube.CoreV1().Services("ingress-nginx").Get(
		context.Background(), "ingress-nginx-controller", metav1.GetOptions{})
	if err != nil {
		return ""
	}
	for _, in := range svc.Status.LoadBalancer.Ingress {
		if in.IP != "" {
			return in.IP
		}
		if in.Hostname != "" {
			return in.Hostname
		}
	}
	return ""
}

// internalLBAddress reads the address (or hostname) of the internal
// ingress-nginx Service at startup; the controller uses it as the
// ExternalDNS target for Ingresses with exposure:internal so their records
// point at the private LB.
func internalLBAddress(k *kube.Client) string {
	svc, err := k.Kube.CoreV1().Services("ingress-nginx-internal").Get(
		context.Background(), "ingress-nginx-internal-controller", metav1.GetOptions{})
	if err != nil {
		return ""
	}
	for _, in := range svc.Status.LoadBalancer.Ingress {
		if in.IP != "" {
			return in.IP
		}
		if in.Hostname != "" {
			return in.Hostname // an NLB on AWS: ExternalDNS makes an alias of it
		}
	}
	return ""
}

// gcOrNil keeps a nil *RegistryGC from becoming a non-nil interface.
func gcOrNil(g *controller.RegistryGC) api.RegistryGC {
	if g == nil {
		return nil
	}
	return g
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// registryInsecure follows SHPYRD_REGISTRY_INSECURE; every registry,
// including the in-cluster one (RFC-0059), speaks TLS unless it says so.
func registryInsecure(flag, host string) bool {
	_ = host
	return flag == "true"
}

// ByAddress is the host-based resolver over the platform's names: the
// building block of a multi-workspace binary. The console host is the
// console; the platform's other names (sign-in, Grafana) are nobody's.
func ByAddress(st store.Store, domain, dashboardURL string) tenancy.Resolver {
	reserved := []string{"auth." + domain, "grafana." + domain, "registry." + domain}
	if u, err := url.Parse(envOr("SHPYRD_AUTH_URL", "")); err == nil && u.Host != "" {
		reserved = append(reserved, u.Host)
	}
	return &tenancy.ByAddress{Store: st, Domain: domain, ConsoleHost: hostOf(dashboardURL), Reserved: reserved, DefaultSlug: defaultSlugOf(st), Layout: layoutFromEnv()}
}

// layoutFromEnv is where workspaces and their apps answer (RFC-0033 names):
// the shared layout when both domains are set (SHPYRD_WORKSPACES_DOMAIN,
// SHPYRD_APPS_DOMAIN), apps one label under their workspace otherwise.
func layoutFromEnv() tenancy.Layout {
	return tenancy.Layout{WorkspacesDomain: os.Getenv(install.VarWorkspacesDomain), AppsDomain: os.Getenv(install.VarAppsDomain)}
}

// hostOf is the host (with port) of a URL, or the string itself.
func hostOf(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		return u.Host
	}
	return rawURL
}

// defaultSlugOf answers the default workspace's slug from the settings
// (RFC-0078), for resolvers and caches.
func defaultSlugOf(st store.Store) tenancy.DefaultSlugFunc {
	return func(ctx context.Context) string { return store.DefaultWorkspaceSlug(ctx, st) }
}

// trustedProxies is the pod range the ingress controllers live in: with the
// server's NetworkPolicy only they reach the API port from inside the
// cluster, so their X-Forwarded-For is the client. No range: nobody is
// trusted and every client is the ingress pod.
func trustedProxies(podCIDR string) []string {
	if strings.TrimSpace(podCIDR) == "" {
		return nil
	}
	return []string{strings.TrimSpace(podCIDR)}
}

// defaultWorkspaceFromEnv is the operator's default workspace as the
// installer described it (RFC-0078, RFC-0080): slug and address. Without
// an address, the platform domain when the console is not at the apex
// (apps stay at <project>.<domain>), else <slug>.<workspaces domain>.
func defaultWorkspaceFromEnv(domain, dashboard string) store.DefaultWorkspaceSpec {
	def := store.DefaultWorkspaceSpec{
		Slug:    envOr("SHPYRD_DEFAULT_WORKSPACE", store.DefaultWorkspace),
		Name:    envOr("SHPYRD_DEFAULT_WORKSPACE_NAME", domain),
		Address: os.Getenv("SHPYRD_DEFAULT_WORKSPACE_ADDRESS"),
	}
	if def.Address == "" {
		if u, err := url.Parse(dashboard); err == nil && u.Hostname() == domain {
			def.Address = def.Slug + "." + envOr("SHPYRD_WORKSPACES_DOMAIN", domain)
		} else {
			def.Address = domain
		}
	}
	return def
}

// publishesNames says the install writes its names to public DNS, so what
// it publishes can be checked from outside (RFC-0061): a DNS provider
// other than none. The server is not handed the provider on every
// install, but it is handed SHPYRD_WILDCARD_TLS, which the installer sets
// to true exactly when a provider exists (pkg/install/vars.go), so either
// says so.
func publishesNames(provider, wildcardTLS string) bool {
	return (provider != "" && provider != "none") || wildcardTLS == "true"
}
