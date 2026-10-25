package api

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/uipkg"
)

// A workspace may run an application of its own at its door (RFC-0084):
// the same kind of build the platform's own is, fetched by the reference
// in the workspace's settings and served by the same rules. The
// platform's own is what a workspace without a reference gets, and what
// answers whenever the chosen one cannot be served.

var (
	uiFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "shpyrd_workspace_ui_failures_total",
		Help: "Times a workspace's own application could not be loaded and the platform's own answered, by workspace.",
	}, []string{"workspace"})
	uiLoaded = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "shpyrd_workspace_ui_loaded",
		Help: "Workspace applications loaded on this replica.",
	})
	registerUIMetrics sync.Once
)

func init() {
	registerUIMetrics.Do(func() {
		ctrlmetrics.Registry.MustRegister(uiFailures, uiLoaded)
	})
}

// uiRetryAfter is how long a failed reference rests before the next
// attempt: a broken one costs one pull a minute, not one a request.
const uiRetryAfter = time.Minute

// UIStatus is what a workspace's application is doing on this replica.
type UIStatus struct {
	// State is "serving", "failed" (Error says why, At when), "pending"
	// (nobody asked yet), or "disabled" (the server does not accept this
	// source: Error says so).
	State string    `json:"state"`
	Error string    `json:"error,omitempty"`
	At    time.Time `json:"at,omitzero"`
	// Name and Version are what the package says about itself.
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type uiEntry struct {
	app      *application
	manifest *uipkg.Manifest
	err      error
	at       time.Time
}

// uiPackages holds the applications workspaces chose, each loaded once by
// its reference and kept, with what happened the last time it was asked
// for.
type uiPackages struct {
	sources uipkg.Sources
	fetcher *uipkg.Fetcher
	add     PageAdditions
	log     *slog.Logger

	mu      sync.Mutex
	entries map[string]*uiEntry
	// pulling keeps concurrent requests for one reference to one pull.
	pulling map[string]*sync.WaitGroup
}

func newUIPackages(sources uipkg.Sources, cacheDir string, add PageAdditions, log *slog.Logger) *uiPackages {
	return &uiPackages{
		sources: sources,
		fetcher: &uipkg.Fetcher{CacheDir: cacheDir, PlainHTTP: plainHTTPRegistry},
		add:     add, log: log,
		entries: map[string]*uiEntry{}, pulling: map[string]*sync.WaitGroup{},
	}
}

// plainHTTPRegistry says a registry is reached without TLS: one on this
// machine, while developing.
func plainHTTPRegistry(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || strings.HasSuffix(h, ".localhost")
}

// chosenUI is the cache, made on first use from the options.
func (s *Server) chosenUI() *uiPackages {
	s.uiOnce.Do(func() {
		sources, err := uipkg.ParseSources(s.opts.UISources)
		if err != nil {
			s.log.Warn("workspace applications off", "err", err.Error())
			sources = uipkg.Sources{Off: true}
		}
		s.uiChosen = newUIPackages(sources, s.opts.UICacheDir, s.opts.Pages, s.log)
		if !sources.Off && s.opts.UICacheDir != "" {
			s.uiChosen.fetcher.Prune(uipkg.Unused)
		}
	})
	return s.uiChosen
}

// get is the application a workspace chose, nil when it has none or it
// cannot be served (the caller falls back to the platform's own).
func (p *uiPackages) get(ctx context.Context, ws *store.Workspace, cred func(host string) uipkg.Credential) *application {
	if ws.Settings.UI == nil || ws.Settings.UI.Ref == "" || p.sources.Off {
		return nil
	}
	ref := ws.Settings.UI.Ref
	p.mu.Lock()
	e, ok := p.entries[ref]
	if ok && (e.app != nil || time.Since(e.at) < uiRetryAfter) {
		p.mu.Unlock()
		return e.app
	}
	wg, inFlight := p.pulling[ref]
	if inFlight {
		p.mu.Unlock()
		wg.Wait()
		p.mu.Lock()
		e = p.entries[ref]
		p.mu.Unlock()
		if e != nil {
			return e.app
		}
		return nil
	}
	wg = &sync.WaitGroup{}
	wg.Add(1)
	p.pulling[ref] = wg
	p.mu.Unlock()

	e = p.load(ctx, ws, ref, cred)

	p.mu.Lock()
	p.entries[ref] = e
	delete(p.pulling, ref)
	p.mu.Unlock()
	wg.Done()
	return e.app
}

func (p *uiPackages) load(ctx context.Context, ws *store.Workspace, ref string, cred func(host string) uipkg.Credential) *uiEntry {
	e := &uiEntry{at: time.Now()}
	r, err := uipkg.Parse(ref)
	if err == nil && !p.sources.Allows(r.Kind) {
		err = errors.New("this server does not serve " + r.Kind + ": references (SHPYRD_UI_SOURCES is " + p.sources.String() + ")")
	}
	var dir string
	if err == nil {
		var c uipkg.Credential
		if r.Kind == uipkg.KindOCI && cred != nil {
			c = cred(r.Host)
		}
		dir, e.manifest, err = p.fetcher.Fetch(ctx, r, c)
	}
	if err == nil {
		add := p.add
		if e.manifest != nil && len(e.manifest.Origins) > 0 {
			add.Origins = append(append([]string{}, add.Origins...), e.manifest.Origins...)
		}
		if e.app = loadApplication(os.DirFS(dir), ".", add, ""); e.app == nil {
			err = errors.New("the package has no index.html")
		}
	}
	if err != nil {
		e.err = err
		uiFailures.WithLabelValues(ws.Slug).Inc()
		p.log.Warn("workspace application not served; the platform's own answers", "workspace", ws.Slug, "ref", ref, "err", err.Error())
		return e
	}
	uiLoaded.Inc()
	p.log.Info("workspace application loaded", "workspace", ws.Slug, "ref", ref)
	return e
}

// forget drops what is known of a reference so the next request loads it
// again: the reference changed, or a development directory was rebuilt.
func (p *uiPackages) forget(ref string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.entries[ref]; ok && e.app != nil {
		uiLoaded.Dec()
	}
	delete(p.entries, ref)
}

// status is what a workspace's reference is doing here.
func (p *uiPackages) status(ws *store.Workspace) *UIStatus {
	if ws.Settings.UI == nil || ws.Settings.UI.Ref == "" {
		return nil
	}
	if p.sources.Off {
		return &UIStatus{State: "disabled", Error: "this server serves the platform's own application only (SHPYRD_UI_SOURCES is off)"}
	}
	if r, err := uipkg.Parse(ws.Settings.UI.Ref); err == nil && !p.sources.Allows(r.Kind) {
		return &UIStatus{State: "disabled", Error: "this server does not serve " + r.Kind + ": references (SHPYRD_UI_SOURCES is " + p.sources.String() + ")"}
	}
	p.mu.Lock()
	e := p.entries[ws.Settings.UI.Ref]
	p.mu.Unlock()
	switch {
	case e == nil:
		return &UIStatus{State: "pending"}
	case e.err != nil:
		return &UIStatus{State: "failed", Error: e.err.Error(), At: e.at}
	}
	st := &UIStatus{State: "serving", At: e.at}
	if e.manifest != nil {
		st.Name, st.Version = e.manifest.Name, e.manifest.Version
	}
	return st
}

// registryCredential is the workspace's credential for a registry host,
// empty when it holds none (an anonymous pull).
func (s *Server) registryCredential(ctx context.Context, ws string, host string) uipkg.Credential {
	if s.apps == nil {
		return uipkg.Credential{}
	}
	sec := &corev1.Secret{}
	if err := s.apps.Get(ctx, types.NamespacedName{Namespace: s.kube.Namespace, Name: uipkg.SecretName(ws, host)}, sec); err != nil {
		return uipkg.Credential{}
	}
	return uipkg.Credential{Username: string(sec.Data[uipkg.KeyUsername]), Token: string(sec.Data[uipkg.KeyToken])}
}

// credentialsOf is the lookup the cache uses for a workspace.
func (s *Server) credentialsOf(ctx context.Context, ws string) func(host string) uipkg.Credential {
	return func(host string) uipkg.Credential { return s.registryCredential(ctx, ws, host) }
}
