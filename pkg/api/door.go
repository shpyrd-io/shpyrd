package api

import (
	"context"
	"encoding/base32"
	"hash/fnv"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
)

// Two doors (RFC-0080): the console at its host, workspaces at theirs.
// This file holds what both need: the default workspace's slug, the
// console's URL, the request's own callback URL, and the identity
// provider's list of callbacks.

// defaultSlug is the operator's default workspace's slug (RFC-0078), read
// from the settings with a short cache.
func (s *Server) defaultSlug(ctx context.Context) string {
	s.defaultSlugMu.Lock()
	defer s.defaultSlugMu.Unlock()
	if s.defaultSlugVal != "" && time.Since(s.defaultSlugAt) < 10*time.Second {
		return s.defaultSlugVal
	}
	s.defaultSlugVal, s.defaultSlugAt = store.DefaultWorkspaceSlug(ctx, s.store), time.Now()
	return s.defaultSlugVal
}

// forgetDefaultSlug drops the cached default: the setting changed.
func (s *Server) forgetDefaultSlug() {
	s.defaultSlugMu.Lock()
	s.defaultSlugVal = ""
	s.defaultSlugMu.Unlock()
}

// workspaceOfApp is the workspace an App belongs to, from its
// authoritative label; Apps from before RFC-0033 carry none and are the
// default workspace's.
func (s *Server) workspaceOfApp(ctx context.Context, app interface{ GetLabels() map[string]string }) string {
	if ws := app.GetLabels()["shpyrd.io/workspace"]; ws != "" {
		return ws
	}
	return s.defaultSlug(ctx)
}

// consoleURL is where the console answers (SHPYRD_DASHBOARD_URL).
func (s *Server) consoleURL() string {
	return strings.TrimSuffix(s.opts.Public.DashboardURL, "/")
}

// requestScheme is the scheme the browser used: TLS here, or the front
// door's word for it.
func requestScheme(c *gin.Context) string {
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		return "https"
	}
	return "http"
}

// callbackURL is this request's OpenID Connect redirect URI: sign-in
// returns to the host it started from (RFC-0080), never to another door.
// In the shared layout a workspace's sign-in returns through the one
// callback every workspace shares (signInRelayURL), which hands the
// browser straight back to it: the identity provider lists that one
// address instead of one per workspace.
func (s *Server) callbackURL(c *gin.Context) string {
	if relay := s.signInRelayURL(); relay != "" {
		if t, err := s.door(c); err == nil && t.Workspace != nil && !t.Internal {
			return relay
		}
	}
	scheme := requestScheme(c)
	if scheme == "http" && strings.HasPrefix(s.opts.Public.DashboardURL, "https://") {
		// Behind the front door without X-Forwarded-Proto: the platform is
		// HTTPS everywhere the browser is concerned.
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host + "/api/auth/callback"
}

// signInRelayHost is the host of the shared callback: the workspaces
// domain itself (shpyrd.cloud), which no workspace has. "" outside the
// shared layout.
func (s *Server) signInRelayHost() string {
	if !s.opts.Layout.Shared() {
		return ""
	}
	return s.opts.Layout.WorkspacesDomain
}

// signInRelayURL is the shared callback, "" outside the shared layout.
func (s *Server) signInRelayURL() string {
	if h := s.signInRelayHost(); h != "" {
		return "https://" + s.withPort(h) + "/api/auth/callback"
	}
	return ""
}

// relaySignIn answers the shared callback: the identity provider's answer
// (code and state, or an error) goes on, unread, to the callback of the
// host the login started at, which the server remembered with the state;
// the address is never taken from the request. Everything else at that
// host is nobody's.
func (s *Server) relaySignIn() gin.HandlerFunc {
	return func(c *gin.Context) {
		relay := s.signInRelayHost()
		if relay == "" || tenancy.Host(c.Request.Host) != relay {
			c.Next()
			return
		}
		c.Abort()
		c.Header("Cache-Control", "no-store")
		if c.Request.URL.Path != "/api/auth/callback" || s.rp == nil {
			s.edgePage(c, http.StatusNotFound, "Nothing here", "There is nothing at this address.", nil)
			return
		}
		host := s.rp.pendingHost(c.Query("state"))
		if host == "" {
			s.edgePage(c, http.StatusBadRequest, "Sign-in expired", "Go back to your workspace and sign in again.", nil)
			return
		}
		c.Redirect(http.StatusFound, "https://"+host+"/api/auth/callback?"+c.Request.URL.RawQuery)
	}
}

// ---- the identity provider's callbacks --------------------------------------

// OAuth2ClientGVR is Dex's client resource (read live from its Kubernetes
// storage): the server owns the one client the platform uses and keeps
// its redirect URIs current (RFC-0080).
var OAuth2ClientGVR = schema.GroupVersionResource{Group: "dex.coreos.com", Version: "v1", Resource: "oauth2clients"}

// oidcClientID is the client the dashboard and workspaces use at Dex.
const oidcClientID = "shpyrd"

// oidcClientChanged asks for the redirect URIs to be reconciled soon, off
// the request's path.
func (s *Server) oidcClientChanged() {
	if s.kube == nil || s.kube.Dynamic == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.reconcileOIDCClient(ctx); err != nil {
			s.log.Warn("identity provider client: redirect URIs not updated", "error", err)
		}
	}()
}

// redirectURIs lists every host a sign-in may return to: the console, every
// workspace address, every verified custom host.
func (s *Server) redirectURIs(ctx context.Context) ([]string, error) {
	set := map[string]bool{}
	add := func(u string) {
		if u != "" {
			set[u] = true
		}
	}
	if u := s.consoleURL(); u != "" {
		add(u + "/api/auth/callback")
	}
	// The shared layout: every workspace returns through one callback.
	if relay := s.signInRelayURL(); relay != "" {
		add(relay)
		out := make([]string, 0, len(set))
		for u := range set {
			out = append(out, u)
		}
		sort.Strings(out)
		return out, nil
	}
	all, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	// A suspended workspace is signed in to only on the way to the gate
	// that receives it.
	_, gated := s.suspendedGate()
	for i := range all {
		ws := &all[i]
		if ws.Address == "" || (ws.Status == store.WorkspaceSuspended && !gated) {
			continue
		}
		add("https://" + s.withPort(ws.Address) + "/api/auth/callback")
		hosts, err := s.store.ListWorkspaceHosts(ctx, ws.Slug)
		if err != nil {
			continue
		}
		for j := range hosts {
			if hosts[j].Kind == store.HostCustom && tenancy.HostServes(&hosts[j]) {
				add("https://" + s.withPort(hosts[j].Host) + "/api/auth/callback")
			}
		}
	}
	out := make([]string, 0, len(set))
	for u := range set {
		out = append(out, u)
	}
	sort.Strings(out)
	return out, nil
}

// reconcileOIDCClient writes Dex's OAuth2Client for the platform: id,
// secret (from the installer's Secret) and the current redirect URIs. Dex
// creates the resource kind when it starts; until then this returns an
// error the caller retries.
func (s *Server) reconcileOIDCClient(ctx context.Context) error {
	if s.kube == nil || s.kube.Dynamic == nil || s.kube.Kube == nil {
		return nil
	}
	ns := s.kube.Namespace
	sec, err := s.kube.Kube.CoreV1().Secrets(ns).Get(ctx, install.OIDCClientSecretName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	secret := string(sec.Data["client-secret"])
	id := string(sec.Data["client-id"])
	if id == "" {
		id = oidcClientID
	}
	uris, err := s.redirectURIs(ctx)
	if err != nil {
		return err
	}
	list := make([]interface{}, 0, len(uris))
	for _, u := range uris {
		list = append(list, u)
	}
	res := s.kube.Dynamic.Resource(OAuth2ClientGVR).Namespace(ns)
	name := dexObjectName(id)
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "dex.coreos.com/v1",
		"kind":       "OAuth2Client",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": ns,
			"labels":    map[string]interface{}{"app.kubernetes.io/managed-by": "shpyrd"},
		},
		"id":           id,
		"name":         "shpyrd",
		"secret":       secret,
		"redirectURIs": list,
	}}
	// v0.9.52 named the object by the id, which Dex never read: remove it.
	if name != id {
		_ = res.Delete(ctx, id, metav1.DeleteOptions{})
	}
	existing, err := res.Get(ctx, name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		_, err = res.Create(ctx, obj, metav1.CreateOptions{})
		if err == nil {
			s.log.Info("identity provider client created", "redirectURIs", len(uris))
		}
		return err
	case err != nil:
		return err
	}
	cur, _, _ := unstructured.NestedStringSlice(existing.Object, "redirectURIs")
	curSecret, _, _ := unstructured.NestedString(existing.Object, "secret")
	if curSecret == secret && equalStrings(cur, uris) {
		return nil
	}
	obj.SetResourceVersion(existing.GetResourceVersion())
	if _, err := res.Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
		return err
	}
	s.log.Info("identity provider client updated", "redirectURIs", len(uris))
	return nil
}

// keepOIDCClient reconciles the client at start and retries while Dex is
// still creating its resource kinds, then stops: later changes come from
// workspacesChanged.
func (s *Server) keepOIDCClient(ctx context.Context) {
	if s.kube == nil || s.kube.Dynamic == nil {
		return
	}
	delay := 2 * time.Second
	for {
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := s.reconcileOIDCClient(rctx)
		cancel()
		if err == nil {
			return
		}
		s.log.Info("identity provider client: not yet, retrying", "in", delay.String(), "error", err.Error())
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if delay < time.Minute {
			delay *= 2
		}
	}
}

// dexObjectName is the object name Dex's Kubernetes storage gives an id
// (a client id, a lower-cased email): the FNV-64 offset basis appended to
// the id, base32 in Dex's alphabet, padding removed
// (dex/storage/kubernetes/client.go idToName). Dex looks the client up by
// this name; an object named by the id itself is invisible to it.
func dexObjectName(id string) string {
	sum := fnv.New64().Sum([]byte(id))
	return strings.TrimRight(dexNameEncoding.EncodeToString(sum), "=")
}

var dexNameEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567")

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
