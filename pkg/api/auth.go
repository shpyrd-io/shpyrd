package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/authz"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/install"
)

// The server is an OpenID Connect relying party (RFC-0007): extensions
// register issuers (Dex for local accounts, Okta, ...), users sign in
// through the authorization code flow with PKCE and get a session cookie.
// The admin token keeps working for automation.

const (
	sessionCookie = "shpyrd_session"
	csrfCookie    = "shpyrd_csrf"
	csrfHeader    = "X-Shpyrd-CSRF"
	loginTTL      = 10 * time.Minute
)

// ProviderInfo is a login option shown by the dashboard.
type ProviderInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Kind picks the icon: "oidc", "github", "google".
	Kind string `json:"kind,omitempty"`
	// Realm is the door the method belongs to (RFC-0080): "console",
	// "platform" (the defaults workspaces offer) or "workspace".
	Realm string `json:"realm,omitempty"`
	// Workspace is the short id of the workspace that owns a "workspace"
	// method (RFC-0033, RFC-0076).
	Workspace string `json:"workspace,omitempty"`
}

// AuthConfig is the auth part of GET /api/config.
type AuthConfig struct {
	// Token says the admin token is accepted.
	Token bool `json:"token"`
	// Providers are the sign-in buttons, in registration order.
	Providers []ProviderInfo `json:"providers"`
	// Password is the provider behind the email/password form, if any
	// (RFC-0012); it is not repeated in Providers.
	Password *ProviderInfo `json:"password,omitempty"`
	// CompanyDomains says the workspace claimed email domains that route
	// to a method (RFC-0033): the login page asks for the email first and
	// sends the person straight to their company's sign-in.
	CompanyDomains bool `json:"companyDomains,omitempty"`
}

type oidcProvider struct {
	ext.OIDCProvider
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	client   *http.Client
	// endSession is the issuer's end_session_endpoint, "" when it has none
	// (Dex publishes none; corporate issuers usually do).
	endSession string
}

// idTokenClaims are the claims the dashboard reads from an id_token.
type idTokenClaims struct {
	Email             string   `json:"email"`
	Name              string   `json:"name"`
	PreferredUsername string   `json:"preferred_username"`
	Groups            []string `json:"groups"`
}

// pendingLogin is an authorization request waiting for its callback.
type pendingLogin struct {
	provider string
	nonce    string
	verifier string
	next     string
	// redirect is the redirect URI the authorization request named: the
	// host the login started at, where it completes (RFC-0080), or the
	// shared layout's one callback, which hands the browser on to host.
	redirect string
	// host is where the login started and completes, with its port.
	host    string
	created time.Time
}

// relyingParty holds providers, in-flight logins and sessions.
type relyingParty struct {
	mu        sync.Mutex
	providers map[string]*oidcProvider
	order     []string
	pending   map[string]pendingLogin
	sessions  *sessionStore
	baseURL   string // the console's URL; the redirect URI of each login is its own host's (RFC-0080)
	domain    string
	ingress   string // cluster-internal address reaching the ingress controller
	caPEM     []byte
	log       loggerish
	now       func() time.Time
}

type loggerish interface {
	Warn(msg string, args ...any)
	Info(msg string, args ...any)
}

func newRelyingParty(sessions *sessionStore, baseURL, domain, ingress string, log loggerish) *relyingParty {
	return &relyingParty{
		providers: map[string]*oidcProvider{}, pending: map[string]pendingLogin{}, sessions: sessions,
		baseURL: strings.TrimRight(baseURL, "/"), domain: domain, ingress: ingress, log: log, now: time.Now,
	}
}

// redirectURI is the console's callback, the client's registered default;
// each login names its own host's (RFC-0080).
func (rp *relyingParty) redirectURI() string { return rp.baseURL + "/api/auth/callback" }

// AddOIDC implements ext.AuthRegistry: discover the issuer and keep its
// endpoints. Discovery goes through httpClient so an issuer published on
// the cluster's own domain is reached inside the cluster.
func (rp *relyingParty) AddOIDC(ctx context.Context, p ext.OIDCProvider) error {
	if p.ID == "" || p.Issuer == "" || p.ClientID == "" {
		return errors.New("oidc provider needs id, issuer and client id")
	}
	client := rp.httpClient()
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, client), p.Issuer)
	if err != nil {
		return fmt.Errorf("discover %s: %w", p.Issuer, err)
	}
	scopes := append([]string{oidc.ScopeOpenID, "email", "profile"}, p.Scopes...)
	var discovery struct {
		EndSessionEndpoint string `json:"end_session_endpoint"`
	}
	_ = provider.Claims(&discovery)
	op := &oidcProvider{
		OIDCProvider: p,
		oauth: oauth2.Config{
			ClientID: p.ClientID, ClientSecret: p.ClientSecret, Endpoint: provider.Endpoint(),
			RedirectURL: rp.redirectURI(), Scopes: scopes,
		},
		verifier:   provider.Verifier(&oidc.Config{ClientID: p.ClientID}),
		client:     client,
		endSession: discovery.EndSessionEndpoint,
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()
	if _, dup := rp.providers[p.ID]; !dup {
		rp.order = append(rp.order, p.ID)
	}
	rp.providers[p.ID] = op
	rp.log.Info("login provider registered", "provider", p.ID, "issuer", p.Issuer)
	return nil
}

// SetClusterCA trusts the cluster's own certificate authority (issuers
// published through the cluster ingress carry its certificates).
func (rp *relyingParty) SetClusterCA(pem []byte) { rp.caPEM = pem }

// httpClient reaches issuers under the cluster domain through the ingress
// controller service: from inside the cluster the public hostname would
// otherwise resolve to the developer's machine (127.0.0.1.nip.io) or hairpin
// through a load balancer.
func (rp *relyingParty) httpClient() *http.Client {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if len(rp.caPEM) > 0 {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		pool.AppendCertsFromPEM(rp.caPEM)
		tlsCfg.RootCAs = pool
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		TLSClientConfig: tlsCfg,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err == nil && rp.ingress != "" && rp.domain != "" && strings.HasSuffix(host, "."+rp.domain) {
				return dialer.DialContext(ctx, network, rp.ingress)
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
	return &http.Client{Transport: transport, Timeout: 20 * time.Second}
}

// providerList returns the sign-in buttons: every provider except the one
// behind the password form.
// RemoveOIDC forgets a provider (a connector removed from the Workspace
// page); sessions opened through it stay until they expire.
func (rp *relyingParty) RemoveOIDC(id string) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	delete(rp.providers, id)
	kept := rp.order[:0]
	for _, o := range rp.order {
		if o != id {
			kept = append(kept, o)
		}
	}
	rp.order = kept
}

func (rp *relyingParty) providerList() []ProviderInfo {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	out := make([]ProviderInfo, 0, len(rp.order))
	for _, id := range rp.order {
		if p := rp.providers[id]; !p.Password && p.available() {
			out = append(out, ProviderInfo{ID: id, Label: p.Label, Kind: firstNonEmpty(p.Kind, "oidc"), Realm: firstNonEmpty(p.Realm, ext.RealmPlatform), Workspace: p.Workspace})
		}
	}
	return out
}

// passwordProvider is the provider that accepts the password grant, if any.
func (rp *relyingParty) passwordProvider() *oidcProvider {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	for _, id := range rp.order {
		if p := rp.providers[id]; p.Password {
			return p
		}
	}
	return nil
}

// available says whether the provider may be used now (ext.OIDCProvider).
func (p *oidcProvider) available() bool { return p.Available == nil || p.Available() }

// provider looks a registered provider up by id.
func (rp *relyingParty) provider(id string) *oidcProvider {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.providers[id]
}

// errBadCredentials is the password grant's "wrong email or password".
var errBadCredentials = errors.New("wrong email or password")

// lockoutThreshold is the number of failed password attempts before the
// account is locked for lockoutDuration (RFC-0014).
const (
	lockoutThreshold = 10
	lockoutWindow    = 15 * time.Minute
	lockoutDuration  = 15 * time.Minute
)

// password signs a user in with the OAuth2 password grant (RFC-0012): the
// credentials go to the issuer's token endpoint server to server, and the
// id_token that comes back is verified like the code flow's. The request is
// built by hand because oauth2.Config cannot carry the nonce Dex accepts.
func (rp *relyingParty) password(ctx context.Context, email, password string) (ext.Identity, string, error) {
	p := rp.passwordProvider()
	if p == nil {
		return ext.Identity{}, "", errors.New("password sign-in is not enabled")
	}
	nonce, err := randomToken(24)
	if err != nil {
		return ext.Identity{}, "", err
	}
	form := url.Values{
		"grant_type": {"password"}, "username": {email}, "password": {password},
		"scope": {strings.Join(p.oauth.Scopes, " ")}, "nonce": {nonce},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.oauth.Endpoint.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return ext.Identity{}, "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(url.QueryEscape(p.ClientID), url.QueryEscape(p.ClientSecret))
	resp, err := p.client.Do(req)
	if err != nil {
		return ext.Identity{}, "", fmt.Errorf("token endpoint: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tok struct {
		IDToken          string `json:"id_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &tok)
	switch {
	case resp.StatusCode == http.StatusUnauthorized || tok.Error == "access_denied" || tok.Error == "invalid_grant":
		return ext.Identity{}, "", errBadCredentials
	case resp.StatusCode/100 != 2:
		return ext.Identity{}, "", fmt.Errorf("token endpoint: %s (%s)", resp.Status, firstNonEmpty(tok.ErrorDescription, tok.Error))
	case tok.IDToken == "":
		return ext.Identity{}, "", errors.New("issuer returned no id_token")
	}
	id, err := rp.identity(ctx, p, tok.IDToken, nonce)
	if err != nil {
		return ext.Identity{}, "", err
	}
	return id, tok.IDToken, nil
}

// identity verifies a raw id_token from provider p and reads the user out
// of it; wantNonce must match the token's nonce.
func (rp *relyingParty) identity(ctx context.Context, p *oidcProvider, raw, wantNonce string) (ext.Identity, error) {
	idt, err := p.verifier.Verify(oidc.ClientContext(ctx, p.client), raw)
	if err != nil {
		return ext.Identity{}, fmt.Errorf("verify id_token: %w", err)
	}
	if idt.Nonce != wantNonce {
		return ext.Identity{}, errors.New("id_token nonce mismatch")
	}
	var claims idTokenClaims
	if err := idt.Claims(&claims); err != nil {
		return ext.Identity{}, fmt.Errorf("read claims: %w", err)
	}
	return ext.Identity{
		Subject: idt.Subject, Email: strings.ToLower(claims.Email), Name: firstNonEmpty(claims.Name, claims.PreferredUsername, claims.Email),
		Groups: claims.Groups, Provider: p.ID, Admin: true,
	}, nil
}

// endSessionURL asks the issuer to end its own session too, when it can.
func (rp *relyingParty) endSessionURL(sess *session) string {
	p := rp.provider(sess.Identity.Provider)
	if p == nil || p.endSession == "" || sess.IDToken == "" {
		return ""
	}
	q := url.Values{"id_token_hint": {sess.IDToken}}
	if rp.baseURL != "" {
		q.Set("post_logout_redirect_uri", rp.baseURL+"/")
	}
	sep := "?"
	if strings.Contains(p.endSession, "?") {
		sep = "&"
	}
	return p.endSession + sep + q.Encode()
}

// begin starts the authorization code flow and returns the issuer URL.
// redirect is the callback the issuer returns to ("" keeps the console's);
// host is where the login started, where it completes.
func (rp *relyingParty) begin(providerID, next, redirect, host string) (string, error) {
	rp.mu.Lock()
	p, ok := rp.providers[providerID]
	if !ok && providerID == "" && len(rp.order) == 1 {
		p, ok = rp.providers[rp.order[0]], true
		providerID = rp.order[0]
	}
	rp.mu.Unlock()
	if !ok || !p.available() {
		return "", fmt.Errorf("unknown login provider %q", providerID)
	}
	state, err := randomToken(24)
	if err != nil {
		return "", err
	}
	nonce, err := randomToken(24)
	if err != nil {
		return "", err
	}
	verifier := oauth2.GenerateVerifier()
	rp.mu.Lock()
	now := rp.now()
	for k, v := range rp.pending {
		if now.Sub(v.created) > loginTTL {
			delete(rp.pending, k)
		}
	}
	rp.pending[state] = pendingLogin{provider: providerID, nonce: nonce, verifier: verifier, next: safeNext(next), redirect: redirect, host: strings.ToLower(host), created: now}
	rp.mu.Unlock()
	opts := []oauth2.AuthCodeOption{oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)}
	if redirect != "" {
		opts = append(opts, oauth2.SetAuthURLParam("redirect_uri", redirect))
	}
	if p.ConnectorID != "" {
		// Dex: go straight to this connector instead of its chooser.
		opts = append(opts, oauth2.SetAuthURLParam("connector_id", p.ConnectorID))
	}
	return p.oauth.AuthCodeURL(state, opts...), nil
}

// pendingHost is the host a login in flight started at, without taking
// the login: "" for a state nobody issued or that expired.
func (rp *relyingParty) pendingHost(state string) string {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	pl, ok := rp.pending[state]
	if !ok || rp.now().Sub(pl.created) > loginTTL {
		return ""
	}
	return pl.host
}

// complete exchanges the callback code for an identity, the raw id_token
// and the pending login it answers (destination, workspace).
func (rp *relyingParty) complete(ctx context.Context, state, code string) (ext.Identity, string, pendingLogin, error) {
	rp.mu.Lock()
	pl, ok := rp.pending[state]
	delete(rp.pending, state)
	p := rp.providers[pl.provider]
	rp.mu.Unlock()
	if !ok || p == nil || rp.now().Sub(pl.created) > loginTTL {
		return ext.Identity{}, "", pl, errors.New("login expired or unknown; start again")
	}
	exchange := []oauth2.AuthCodeOption{oauth2.VerifierOption(pl.verifier)}
	if pl.redirect != "" {
		exchange = append(exchange, oauth2.SetAuthURLParam("redirect_uri", pl.redirect))
	}
	tok, err := p.oauth.Exchange(oidc.ClientContext(ctx, p.client), code, exchange...)
	if err != nil {
		return ext.Identity{}, "", pl, fmt.Errorf("token exchange: %w", err)
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return ext.Identity{}, "", pl, errors.New("issuer returned no id_token")
	}
	id, err := rp.identity(ctx, p, raw, pl.nonce)
	if err != nil {
		return ext.Identity{}, "", pl, err
	}
	return id, raw, pl, nil
}

// safeNext only allows same-origin paths as post-login destinations.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/api/") {
		return "/"
	}
	if u, err := url.Parse(next); err != nil || u.Host != "" || u.Scheme != "" {
		return "/"
	}
	return next
}

// ---- server integration ------------------------------------------------------

// authProviders is the public list of login options.
func (s *Server) authProviders(c *gin.Context) {
	c.JSON(http.StatusOK, s.authConfigFor(c))
}

func (s *Server) authConfig() AuthConfig {
	cfg := AuthConfig{Token: s.opts.Token != "" && !s.opts.TokenDisabled, Providers: []ProviderInfo{}}
	if s.rp != nil {
		cfg.Providers = s.rp.providerList()
		if p := s.rp.passwordProvider(); p != nil {
			cfg.Password = &ProviderInfo{ID: p.ID, Label: p.Label, Realm: firstNonEmpty(p.Realm, ext.RealmPlatform), Workspace: p.Workspace}
		}
	}
	return cfg
}

// PasswordLoginRequest is the email/password form (RFC-0012).
type PasswordLoginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
	Next     string `json:"next"`
}

// tokenLoginRequest is POST /api/auth/token.
type tokenLoginRequest struct {
	Token string `json:"token" binding:"required"`
	Next  string `json:"next"`
}

// authToken turns the admin token into a browser session, so the token's
// holder gets the same cookie-based identity as accounts: the edge
// (RFC-0033) and every page work the same way. Throttled per address like
// the header form of the token.
func (s *Server) authToken(c *gin.Context) {
	var req tokenLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	if s.opts.Token == "" || s.opts.TokenDisabled {
		abort(c, http.StatusUnauthorized, errors.New("the admin token is disabled on this platform"))
		return
	}
	if s.tokenFailures.exhausted(c.ClientIP()) {
		abort(c, http.StatusTooManyRequests, errors.New("too many attempts; wait a minute"))
		return
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(req.Token)), []byte(s.opts.Token)) != 1 {
		s.tokenFailures.allow(c.ClientIP()) // a failure spends a token
		s.auditAnonymous(c, "auth.login_failed", "admin token")
		abort(c, http.StatusUnauthorized, errors.New("that is not the admin token"))
		return
	}
	if s.rp == nil {
		abort(c, http.StatusNotImplemented, errors.New("sessions are not available: no login provider is configured"))
		return
	}
	if _, ok := s.openSession(c, ext.Identity{Subject: "admin-token", Provider: "token", Admin: true}, "", "token"); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"next": firstNonEmpty(safeNextOrEmpty(req.Next), "/")})
}

// authPassword signs in with email and password through the issuer's
// password grant and opens a session. Wrong credentials are 401 with a
// message the page shows in place; failures per account are throttled and
// every attempt is audited by email.
func (s *Server) authPassword(c *gin.Context) {
	var req PasswordLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, errors.New("email and password are required"))
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if s.rp == nil || s.rp.passwordProvider() == nil {
		abort(c, http.StatusNotFound, errors.New("password sign-in is not enabled"))
		return
	}
	// The password grant needs no browser round trip, so it works at any
	// door directly, when that door offers it (RFC-0080).
	if !s.offersAt(c, s.rp.passwordProvider().ID) {
		abort(c, http.StatusNotFound, errors.New("password sign-in is not offered here"))
		return
	}
	// RFC-0014 lockout: check durable lock first, then in-memory rate limit.
	if s.localAccounts != nil {
		if locked, _ := s.localAccounts.IsLocked(c.Request.Context(), email); locked {
			c.Header("Retry-After", "900")
			abort(c, http.StatusTooManyRequests, errors.New("account is locked due to repeated failed attempts; try again in 15 minutes or use 'Forgot password'"))
			return
		}
	}
	if s.passwordFailures.exhausted(email) {
		c.Header("Retry-After", "60")
		abort(c, http.StatusTooManyRequests, errors.New("too many failed attempts for this account; try again in a minute"))
		return
	}
	id, idToken, err := s.rp.password(c.Request.Context(), email, req.Password)
	if err != nil {
		s.log.Warn("password sign-in failed", "email", email, "error", err, "remote", c.ClientIP())
		if errors.Is(err, errBadCredentials) {
			// Count the failure; lock the account after the threshold.
			if !s.passwordFailures.allow(email) && s.localAccounts != nil {
				until := time.Now().Add(lockoutDuration)
				if lerr := s.localAccounts.Lock(c.Request.Context(), email, until); lerr == nil {
					s.auditFailure(c, "auth.locked", email, "account locked after repeated failures")
				}
			}
			s.auditFailure(c, "auth.login_failed", email, "wrong password")
			abort(c, http.StatusUnauthorized, err)
			return
		}
		s.auditFailure(c, "auth.login_failed", email, err.Error())
		abort(c, http.StatusBadGateway, errors.New("the sign-in service is not reachable; try again"))
		return
	}
	// Successful sign-in: clear in-memory failure count.
	if s.localAccounts != nil {
		_ = s.localAccounts.Lock(c.Request.Context(), email, time.Time{}) // unlock
	}
	if err := s.admitAt(c, id); err != nil {
		s.auditFailure(c, "auth.refused", id.Email, err.Error())
		abort(c, http.StatusForbidden, err)
		return
	}
	next, ok := s.openSession(c, id, idToken, "password")
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"next": firstNonEmpty(safeNextOrEmpty(req.Next), next)})
}

// safeNextOrEmpty is safeNext without the "/" default.
func safeNextOrEmpty(next string) string {
	if next == "" {
		return ""
	}
	return safeNext(next)
}

// openSession creates the session for a verified identity, sets the
// cookies and audits the sign-in. The id_token is kept only when the
// issuer can end sessions (RFC-0012). It returns "/" as destination.
func (s *Server) openSession(c *gin.Context, id ext.Identity, idToken, how string) (string, bool) {
	if p := s.rp.provider(id.Provider); p == nil || p.endSession == "" {
		idToken = ""
	}
	// The person's id in this workspace is the subject apps see, whatever
	// login method brought them (RFC-0033: the JWT's sub is the identity).
	if person := s.recordSignIn(c, id); person != nil && person.ID != "" {
		id.Subject = person.ID
	}
	// An invitation for this address is accepted by signing in (RFC-0033).
	s.acceptPendingInvitation(c, id)
	sess, err := s.rp.sessions.create(c.Request.Context(), s.realmAt(c), s.workspace(c), id, idToken)
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return "", false
	}
	s.setSessionCookies(c, sess)
	s.log.Info("user signed in", "email", id.Email, "provider", id.Provider, "how", how)
	ext.SetIdentity(c, id)
	s.audit(c, "", "auth.login", firstNonEmpty(id.Email, id.Name, id.Subject), "provider "+id.Provider+" ("+how+")")
	return "/", true
}

// authLogin redirects the browser to the issuer, for a method this door
// offers, naming this host's callback: the sign-in completes where it
// started (RFC-0080). No door signs in on another's behalf.
func (s *Server) authLogin(c *gin.Context) {
	if s.rp == nil {
		abort(c, http.StatusNotFound, errors.New("no login provider configured"))
		return
	}
	provider, next := c.Query("provider"), c.Query("next")
	if _, err := s.door(c); err != nil {
		abort(c, http.StatusNotFound, err)
		return
	}
	if !s.offersAt(c, provider) {
		abort(c, http.StatusBadRequest, fmt.Errorf("login method %q is not offered here", provider))
		return
	}
	u, err := s.rp.begin(provider, next, s.callbackURL(c), c.Request.Host)
	if err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	c.Redirect(http.StatusFound, u)
}

// authCallback finishes the flow and sets the session cookies.
func (s *Server) authCallback(c *gin.Context) {
	if s.rp == nil {
		abort(c, http.StatusNotFound, errors.New("no login provider configured"))
		return
	}
	if e := c.Query("error"); e != "" {
		s.loginFailed(c, fmt.Errorf("%s: %s", e, c.Query("error_description")))
		return
	}
	if host := s.rp.pendingHost(c.Query("state")); host != "" && host != strings.ToLower(c.Request.Host) {
		// A login completes where it started; the code is not taken here.
		s.loginFailed(c, errors.New("this sign-in started at another address; start again"))
		return
	}
	id, idToken, pl, err := s.rp.complete(c.Request.Context(), c.Query("state"), c.Query("code"))
	if err != nil {
		s.log.Warn("login failed", "error", err, "remote", c.ClientIP())
		s.auditAnonymous(c, "auth.login_failed", err.Error())
		s.loginFailed(c, err)
		return
	}
	if p := s.rp.provider(id.Provider); p == nil || !s.offersAt(c, p.ID) {
		// The method is not this door's: a callback replayed at another
		// host mints nothing.
		s.auditFailure(c, "auth.refused", id.Email, "login method not offered at "+c.Request.Host)
		s.loginFailed(c, errors.New("this sign-in method is not offered here"))
		return
	}
	if err := s.admitAt(c, id); err != nil {
		s.auditFailure(c, "auth.refused", id.Email, err.Error())
		s.loginFailed(c, err)
		return
	}
	if _, ok := s.openSession(c, id, idToken, "redirect"); !ok {
		return
	}
	c.Redirect(http.StatusFound, pl.next)
}

// authTicket signs a browser in with a one-time login ticket minted by the
// CLI (`shpyrd cluster dashboard`): the ticket Secret in the system
// namespace holds the code's hash, an expiry and the local user, so the
// admin token never reaches the browser and the session is attributable.
func (s *Server) authTicket(c *gin.Context) {
	code := c.Query("code")
	if code == "" || s.kube == nil || s.kube.Kube == nil {
		abort(c, http.StatusNotFound, errors.New("no such login ticket"))
		return
	}
	actor, err := RedeemLoginTicket(c.Request.Context(), s.kube.Kube, s.deps().SystemNamespace, code)
	if err != nil {
		s.log.Warn("login ticket refused", "error", err, "remote", c.ClientIP())
		s.auditAnonymous(c, "auth.ticket_failed", err.Error())
		s.loginFailed(c, err)
		return
	}
	id := ext.Identity{Subject: "kubeconfig:" + actor, Name: actor, Provider: "kubeconfig", Admin: true}
	sess, err := s.rp.sessions.create(c.Request.Context(), s.realmAt(c), s.workspace(c), id, "")
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	s.setSessionCookies(c, sess)
	ext.SetIdentity(c, id)
	s.audit(c, "", "auth.login", actor, "one-time ticket from the CLI")
	c.Redirect(http.StatusFound, safeNext(c.Query("next")))
}

// A sign-in ticket (ext.Deps.SignInTicket): the signup made a person a
// workspace and proved their email; rather than a login form at the new
// door, the page sends them through a one-time code that the door turns
// into a session. The code lives in the store's one-time codes, bound to
// the door's host, for ten minutes, and is taken on first use.

const signupTicketTTL = 10 * time.Minute

// signupTicketClaims is what a sign-in ticket carries.
type signupTicketClaims struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

func (s *Server) signInTicketHook(ctx context.Context, wsSlug, email string) (string, error) {
	if s.store == nil {
		return "", errors.New("no control-plane store")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if !validEmail(email) {
		return "", errors.New("that is not an email address")
	}
	ws, err := s.store.Workspace(ctx, wsSlug)
	if err != nil {
		return "", err
	}
	if ws.Address == "" {
		return "", errors.New("the workspace has no address yet")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	claims, _ := json.Marshal(signupTicketClaims{Email: email, Name: strings.SplitN(email, "@", 2)[0]})
	if err := s.store.PutCode(ctx, store.Code{Code: code, Host: strings.ToLower(ws.Address), Claims: claims, ExpiresAt: time.Now().Add(signupTicketTTL)}); err != nil {
		return "", err
	}
	return s.dashboardURLOf(ws) + "/api/auth/signup-ticket?code=" + url.QueryEscape(code), nil
}

// authSignupTicket is GET /api/auth/signup-ticket?code=…: the door takes
// the code once, admits the person as any sign-in would and opens their
// session here, then sends them to the launcher. Whatever else the link
// carries goes on with them, refused or not (carryQuery).
func (s *Server) authSignupTicket(c *gin.Context) {
	redirect := func(to string) { c.Redirect(http.StatusFound, carryQuery(c, to, "code")) }
	fail := func(err error) { redirect(loginErrorURL(err)) }
	code := c.Query("code")
	if code == "" || s.store == nil || s.rp == nil {
		fail(errors.New("no such sign-in link"))
		return
	}
	ticket, err := s.store.TakeCode(c.Request.Context(), code)
	if err != nil || ticket == nil || time.Now().After(ticket.ExpiresAt) {
		s.auditAnonymous(c, "auth.signup_ticket_failed", "unknown or expired")
		fail(errors.New("this sign-in link was used already or expired; sign in with your email and password"))
		return
	}
	if ws := s.workspace(c); ticket.Host != tenancy.Host(c.Request.Host) && (ws == "" || ticket.Host != s.workspaceAddress(c)) {
		s.auditAnonymous(c, "auth.signup_ticket_failed", "another door")
		fail(errors.New("this sign-in link is for another workspace"))
		return
	}
	var claims signupTicketClaims
	if json.Unmarshal(ticket.Claims, &claims) != nil || claims.Email == "" {
		fail(errors.New("this sign-in link is not valid"))
		return
	}
	id := ext.Identity{Subject: claims.Email, Email: claims.Email, Name: firstNonEmpty(claims.Name, claims.Email), Provider: "signup"}
	if err := s.admitAt(c, id); err != nil {
		s.auditFailure(c, "auth.refused", id.Email, err.Error())
		fail(err)
		return
	}
	next, ok := s.openSession(c, id, "", "signup ticket")
	if !ok {
		return
	}
	redirect(next)
}

// carryQuery is where a link that ends in a redirect sends the browser,
// with the rest of the link's query. No page sees the link itself, only
// the one it redirects to, and that page is the first to load the pages'
// additions (the analytics): what they read on arrival rides on the
// query (the device id the signup hands on, Google's linker, a
// campaign's tags), so it must reach that page. The core names none of
// them: every parameter goes on except the ones dropped (the link's own,
// such as a ticket's code) and the ones the destination sets itself. The
// parameters go as they came, not encoded again, since the scripts read
// some of them byte for byte.
func carryQuery(c *gin.Context, to string, drop ...string) string {
	u, err := url.Parse(to)
	if err != nil {
		return to
	}
	own := u.Query()
	var rest []string
	for _, pair := range strings.Split(c.Request.URL.RawQuery, "&") {
		raw, _, _ := strings.Cut(pair, "=")
		key, err := url.QueryUnescape(raw)
		if pair == "" || err != nil || slices.Contains(drop, key) || own.Has(key) {
			continue
		}
		rest = append(rest, pair)
	}
	if len(rest) == 0 {
		return to
	}
	if u.RawQuery != "" {
		rest = append([]string{u.RawQuery}, rest...)
	}
	u.RawQuery = strings.Join(rest, "&")
	return u.String()
}

// workspaceAddress is the address of the request's workspace, "" at the
// console or when unresolved.
func (s *Server) workspaceAddress(c *gin.Context) string {
	if ws := ext.WorkspaceObjectFrom(c); ws != nil {
		return strings.ToLower(ws.Address)
	}
	return ""
}

// loginFailed sends the browser back to the login page with the reason.
func (s *Server) loginFailed(c *gin.Context, err error) {
	c.Redirect(http.StatusFound, loginErrorURL(err))
}

// loginErrorURL is the login page with the reason.
func loginErrorURL(err error) string {
	return "/?login_error=" + url.QueryEscape(err.Error())
}

// secureCookies says the cookies of this request are Secure: the browser
// reached us over HTTPS (directly or through the front door), or the
// platform is HTTPS by configuration.
func (s *Server) secureCookies(c *gin.Context) bool {
	return requestScheme(c) == "https" || s.platformHTTPS()
}

// platformHTTPS says the platform is HTTPS by configuration: what the edge
// cookie's name (__Host-) must decide once, not per request.
func (s *Server) platformHTTPS() bool {
	return strings.HasPrefix(s.opts.Public.DashboardURL, "https://")
}

func (s *Server) setSessionCookies(c *gin.Context, sess *session) {
	secure := s.secureCookies(c)
	http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: sess.ID, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionAbsolute.Seconds())})
	// Readable by the dashboard, echoed in X-Shpyrd-CSRF on mutations.
	http.SetCookie(c.Writer, &http.Cookie{Name: csrfCookie, Value: sess.CSRF, Path: "/", Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionAbsolute.Seconds())})
}

func (s *Server) clearSessionCookies(c *gin.Context) {
	for _, name := range []string{sessionCookie, csrfCookie} {
		http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: name == sessionCookie, Secure: s.secureCookies(c), SameSite: http.SameSiteLaxMode})
	}
}

// authLogout ends the session (cookie authenticated, CSRF checked by auth())
// and tells the page where to go next: the issuer's end-session endpoint
// when it has one, so the user is signed out there too, else the root.
func (s *Server) authLogout(c *gin.Context) {
	redirect := "/"
	if s.rp != nil {
		if sid, err := c.Cookie(sessionCookie); err == nil && sid != "" {
			if sess, ok := s.rp.sessions.getIn(sid, s.realmAt(c), s.workspaceID(c)); ok {
				if u := s.rp.endSessionURL(sess); u != "" {
					redirect = u
				}
			}
			s.rp.sessions.delete(c.Request.Context(), sid)
		}
	}
	s.clearSessionCookies(c)
	c.JSON(http.StatusOK, gin.H{"redirect": redirect})
}

// Me is the caller's identity with the roles the dashboard hides actions by.
type Me struct {
	ext.Identity
	// Person is the stable id the workspace knows the person by (the
	// store's identity id): what the pages report as who acted, and what
	// the audit trail carries as Subject. Empty for the admin token.
	Person string `json:"person,omitempty"`
	// WorkspaceID is the id of the workspace the door belongs to, the
	// store's UUID: what the pages report as the workspace, as the
	// server's events and the billing app name it, since a workspace's
	// address changes and its id does not. Empty at the console.
	WorkspaceID string      `json:"workspaceId,omitempty"`
	Roles       authz.Roles `json:"roles"`
	// Console says the person is a platform admin by the console's roles
	// (RFC-0080): a workspace owned by the operator shows them the way to
	// the console.
	Console bool `json:"console,omitempty"`
}

// me returns the caller's identity and roles.
func (s *Server) me(c *gin.Context) {
	id, ok := ext.IdentityFrom(c)
	if !ok {
		id = ext.Identity{Subject: "anonymous", Provider: "none", Admin: true}
	}
	roles, err := s.rolesOf(c)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	id.Admin = roles.Platform == shpyrdv1.RolePlatformAdmin
	// Console: a platform admin by the console's roles. At the console that
	// is the admin flag; at an operator workspace the same rule gave the
	// person the owner role (rolesAt); elsewhere the console's enforced
	// roles decide.
	console := id.Admin && (s.atConsole(c) || s.ownedByOperator(c))
	if !console && s.authz != nil {
		console = s.authz.ConsoleAdmin(c.Request.Context(), id)
	}
	var workspaceID string
	if ws, err := s.tenant(c); err == nil {
		workspaceID = ws.ID
	}
	c.JSON(http.StatusOK, Me{Identity: id, Person: s.subjectOf(c, id), WorkspaceID: workspaceID, Roles: roles, Console: console})
}

// securityHeaders hardens every response (RFC-0008). The dashboard is a
// same-origin SPA: scripts and styles come from this server only.
func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		if !strings.HasPrefix(c.Request.URL.Path, "/api/") {
			h.Set("Content-Security-Policy", basePolicy)
		}
		c.Next()
	}
}

// sessionAuth tries the session cookie: on success the identity is set and
// mutations must carry the CSRF header.
func (s *Server) sessionAuth(c *gin.Context) (bool, error) {
	if s.rp == nil {
		return false, nil
	}
	sid, err := c.Cookie(sessionCookie)
	if err != nil || sid == "" {
		return false, nil
	}
	sess, ok := s.rp.sessions.getIn(sid, s.realmAt(c), s.workspaceID(c))
	if !ok {
		s.clearSessionCookies(c)
		return false, nil
	}
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		if subtle.ConstantTimeCompare([]byte(c.GetHeader(csrfHeader)), []byte(sess.CSRF)) != 1 {
			return false, errors.New("missing or invalid CSRF token")
		}
	}
	ext.SetIdentity(c, sess.Identity)
	return true, nil
}

// clusterCA reads the certificate authority the cluster issuer signs with:
// the bundle trust-manager distributes to every namespace, or the root CA
// Secret itself.
func (s *Server) clusterCA(ctx context.Context) []byte {
	if s.kube == nil || s.kube.Kube == nil {
		return nil
	}
	if cm, err := s.kube.Kube.CoreV1().ConfigMaps(s.kube.Namespace).Get(ctx, install.CABundleName, metav1.GetOptions{}); err == nil {
		if pem := cm.Data[install.CABundleKey]; pem != "" {
			return []byte(pem)
		}
	}
	for _, ns := range []string{"cert-manager", s.kube.Namespace} {
		if sec, err := s.kube.Kube.CoreV1().Secrets(ns).Get(ctx, install.LocalCASecretName, metav1.GetOptions{}); err == nil {
			if pem := sec.Data["ca.crt"]; len(pem) > 0 {
				return pem
			}
			return sec.Data["tls.crt"]
		}
	}
	return nil
}

// rateLimiter is a per-client token bucket for the login endpoints: it
// slows down credential stuffing and callback replay without a store.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens per second
	burst   float64
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(perMinute int) *rateLimiter {
	return &rateLimiter{buckets: map[string]*bucket{}, rate: float64(perMinute) / 60, burst: float64(perMinute), now: time.Now}
}

// allow takes a token for key, reporting whether one was available.
func (r *rateLimiter) allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	b, ok := r.buckets[key]
	if !ok {
		if len(r.buckets) > 10000 {
			r.buckets = map[string]*bucket{} // crude but bounded
		}
		b = &bucket{tokens: r.burst, last: now}
		r.buckets[key] = b
	}
	b.tokens = min(r.burst, b.tokens+now.Sub(b.last).Seconds()*r.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// exhausted reports whether key has no tokens left, without taking one.
func (r *rateLimiter) exhausted(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.buckets[key]
	if !ok {
		return false
	}
	return b.tokens+r.now().Sub(b.last).Seconds()*r.rate < 1
}

func (r *rateLimiter) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !r.allow(c.ClientIP()) {
			c.Header("Retry-After", "60")
			abort(c, http.StatusTooManyRequests, errors.New("too many sign-in attempts; try again in a minute"))
			return
		}
		c.Next()
	}
}

// admitAt runs the admission of the request's door: a workspace's join
// policy, claimed domains and suspensions at a workspace host; at the
// console, suspension of the operator's people only — the console's
// methods decide who reaches it, its roles what they may do (RFC-0080).
func (s *Server) admitAt(c *gin.Context, id ext.Identity) error {
	t, err := s.door(c)
	if err != nil {
		return err
	}
	if t.Workspace == nil {
		return s.admitConsole(c.Request.Context(), id)
	}
	return s.admitSignIn(c.Request.Context(), t.Workspace.Slug, id)
}

// ownedByOperator says the request's workspace is one of the operator's.
func (s *Server) ownedByOperator(c *gin.Context) bool {
	ws, err := s.tenant(c)
	return err == nil && ws.OwnedByOperator()
}
