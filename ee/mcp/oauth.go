//go:build !foss

package mcp

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/authz"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/pages"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The workspace's OAuth 2.1 authorization server (RFC-0032, RFC-0033
// "Machines"): what lets an MCP client such as Claude act for a person.
// Discovery (RFC 8414, RFC 9728), dynamic client registration (RFC 7591),
// the authorization code flow with PKCE (S256, required), a consent page
// at the workspace host, access tokens that are the platform's own EdDSA
// JWTs (one hour) and refresh tokens that rotate (thirty days, revocable
// from the workspace page). Dex stays upstream for the sign-in: the
// consent page needs the dashboard session, nothing else.

const (
	oauthTokenType   = "shpyrd-oauth" // the JWT header's typ
	oauthAccessTTL   = time.Hour
	oauthRefreshTTL  = 30 * 24 * time.Hour
	oauthCodeTTL     = 5 * time.Minute
	oauthClientCap   = 1000 // registrations per workspace, against flooding
	oauthMaxRedirect = 8
)

// OAuth scopes. Reading covers what the MCP prototype does: list projects,
// status, logs and metrics. Writing (deploys, config) waits for a later
// slice; a token without it acts as a viewer whatever its owner is.
const (
	ScopeProjectsRead  = "projects:read"
	ScopeProjectsWrite = "projects:write"
)

var oauthScopes = []string{ScopeProjectsRead, ScopeProjectsWrite}

// oauthClaims are the access token's claims.
type oauthClaims struct {
	Issuer    string `json:"iss"`
	Subject   string `json:"sub"`
	Audience  string `json:"aud"`
	Email     string `json:"email"`
	Name      string `json:"name,omitempty"`
	Workspace string `json:"ws"`
	Scope     string `json:"scope"`
	ClientID  string `json:"client_id"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

// oauthIssuer is the authorization server's identity: the workspace's
// dashboard URL. The MCP endpoint is the protected resource.
func (s *server) oauthIssuer(c *gin.Context) string {
	ws, _ := s.h.TenantOf(c)
	return s.h.DashboardURLOf(ws)
}

func (s *server) mcpResource(c *gin.Context) string { return s.oauthIssuer(c) + "/mcp" }

// oauthMetadata is GET /.well-known/oauth-authorization-server (RFC 8414).
func (s *server) oauthMetadata(c *gin.Context) {
	iss := s.oauthIssuer(c)
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, gin.H{
		"issuer":                                     iss,
		"authorization_endpoint":                     iss + "/oauth/authorize",
		"token_endpoint":                             iss + "/oauth/token",
		"registration_endpoint":                      iss + "/oauth/register",
		"revocation_endpoint":                        iss + "/oauth/revoke",
		"jwks_uri":                                   iss + "/.well-known/jwks.json",
		"response_types_supported":                   []string{"code"},
		"response_modes_supported":                   []string{"query"},
		"grant_types_supported":                      []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":           []string{"S256"},
		"token_endpoint_auth_methods_supported":      []string{"none", "client_secret_post", "client_secret_basic"},
		"revocation_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
		"scopes_supported":                           oauthScopes,
		"service_documentation":                      "https://shpyrd.io/docs/mcp",
	})
}

// protectedResourceMetadata is GET /.well-known/oauth-protected-resource
// (RFC 9728): the MCP endpoint names its authorization server.
func (s *server) protectedResourceMetadata(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, gin.H{
		"resource":                 s.mcpResource(c),
		"authorization_servers":    []string{s.oauthIssuer(c)},
		"scopes_supported":         oauthScopes,
		"bearer_methods_supported": []string{"header"},
		"resource_name":            s.mcpServerName(c),
	})
}

// registerRequest is RFC 7591's registration body, the fields we read.
type registerRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
}

// oauthRegister is POST /oauth/register: any MCP client registers itself.
// Redirect URIs are https, or http on the loopback (desktop clients).
func (s *server) oauthRegister(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		oauthError(c, http.StatusBadRequest, "invalid_client_metadata", "the registration body is not valid JSON")
		return
	}
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > oauthMaxRedirect {
		oauthError(c, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris: one to eight URIs")
		return
	}
	for _, r := range req.RedirectURIs {
		if err := validRedirectURI(r); err != nil {
			oauthError(c, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
			return
		}
	}
	for _, g := range req.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			oauthError(c, http.StatusBadRequest, "invalid_client_metadata", "grant_types: authorization_code and refresh_token only")
			return
		}
	}
	method := firstNonEmpty(req.TokenEndpointAuthMethod, "none")
	switch method {
	case "none", "client_secret_post", "client_secret_basic":
	default:
		oauthError(c, http.StatusBadRequest, "invalid_client_metadata", "token_endpoint_auth_method: none, client_secret_post or client_secret_basic")
		return
	}
	ws, err := s.h.TenantOf(c)
	if err != nil {
		oauthError(c, http.StatusNotFound, "invalid_request", "unknown workspace")
		return
	}
	clientID := "mcp_" + oauthRandom(12)
	secret, secretHash := "", ""
	if method != "none" {
		secret = oauthRandom(32)
		secretHash = hashToken(secret)
	}
	name := strings.TrimSpace(req.ClientName)
	if len(name) > 80 {
		name = name[:80]
	}
	client, err := s.h.Store().CreateOAuthClient(c.Request.Context(), ws.Slug, store.OAuthClient{ClientID: clientID, SecretHash: secretHash, Name: firstNonEmpty(name, "an MCP client"), RedirectURIs: req.RedirectURIs})
	if err != nil {
		oauthError(c, http.StatusBadGateway, "server_error", err.Error())
		return
	}
	s.h.AuditAnonymous(c, "oauth.client.register", client.Name+" ("+clientID+")")
	out := gin.H{
		"client_id":                  client.ClientID,
		"client_id_issued_at":        client.CreatedAt.Unix(),
		"client_name":                client.Name,
		"redirect_uris":              client.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": method,
		"scope":                      strings.Join(oauthScopes, " "),
	}
	if secret != "" {
		out["client_secret"] = secret
		out["client_secret_expires_at"] = 0
	}
	c.JSON(http.StatusCreated, out)
}

func validRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Fragment != "" {
		return fmt.Errorf("redirect URI %q is not a valid absolute URL", raw)
	}
	host := u.Hostname()
	loopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if loopback {
			return nil
		}
		return fmt.Errorf("redirect URI %q must use https (http is for localhost only)", raw)
	}
	// Custom schemes (desktop apps) are accepted per RFC 8252.
	if strings.Contains(u.Scheme, ".") {
		return nil
	}
	return fmt.Errorf("redirect URI %q must use https", raw)
}

func oauthRandom(n int) string {
	raw := make([]byte, n)
	_, _ = rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func oauthError(c *gin.Context, status int, code, desc string) {
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(status, gin.H{"error": code, "error_description": desc})
}

// authorizeParams are the authorization request's parameters, validated.
type authorizeParams struct {
	client        *store.OAuthClient
	redirectURI   string
	scope         []string
	state         string
	codeChallenge string
	resource      string
}

// authorizeParamsOf validates the request; errors that cannot be sent to
// the client (bad client, bad redirect) are shown to the person instead.
func (s *server) authorizeParamsOf(c *gin.Context, get func(string) string) (*authorizeParams, error) {
	clientID := get("client_id")
	if clientID == "" {
		return nil, errors.New("client_id is missing")
	}
	client, err := s.h.Store().OAuthClientByID(c.Request.Context(), clientID)
	if err != nil {
		return nil, errors.New("unknown client; the application must register first")
	}
	ws, err := s.h.TenantOf(c)
	if err != nil || ws.ID != client.WorkspaceID {
		return nil, errors.New("this client was registered at another workspace")
	}
	redirect := get("redirect_uri")
	if redirect == "" && len(client.RedirectURIs) == 1 {
		redirect = client.RedirectURIs[0]
	}
	registered := false
	for _, r := range client.RedirectURIs {
		if r == redirect {
			registered = true
		}
	}
	if !registered {
		return nil, errors.New("the redirect URI is not one the client registered")
	}
	p := &authorizeParams{client: client, redirectURI: redirect, state: get("state"), codeChallenge: get("code_challenge"), resource: get("resource")}
	for _, sc := range strings.Fields(get("scope")) {
		known := false
		for _, k := range oauthScopes {
			known = known || k == sc
		}
		if known {
			p.scope = append(p.scope, sc)
		}
	}
	if len(p.scope) == 0 {
		p.scope = []string{ScopeProjectsRead}
	}
	sort.Strings(p.scope)
	return p, nil
}

// redirectWithError sends the client an OAuth error at its redirect URI.
func redirectWithError(c *gin.Context, p *authorizeParams, code, desc string) {
	u, _ := url.Parse(p.redirectURI)
	q := u.Query()
	q.Set("error", code)
	q.Set("error_description", desc)
	if p.state != "" {
		q.Set("state", p.state)
	}
	u.RawQuery = q.Encode()
	c.Redirect(http.StatusFound, u.String())
}

// oauthAuthorize is GET /oauth/authorize: with a session, the consent
// page; without one, the sign-in page first, coming back here.
func (s *server) oauthAuthorize(c *gin.Context) {
	p, err := s.authorizeParamsOf(c, c.Query)
	if err != nil {
		s.h.EdgePage(c, http.StatusBadRequest, "This connection cannot be authorised", err.Error())
		return
	}
	if c.Query("response_type") != "code" {
		redirectWithError(c, p, "unsupported_response_type", "response_type must be code")
		return
	}
	if p.codeChallenge == "" || c.Query("code_challenge_method") != "S256" {
		redirectWithError(c, p, "invalid_request", "PKCE with S256 is required")
		return
	}
	ok, _ := s.h.SessionAuth(c)
	if !ok {
		c.Redirect(http.StatusFound, "/?next="+url.QueryEscape(c.Request.URL.RequestURI()))
		return
	}
	id, _ := ext.IdentityFrom(c)
	csrf, _, _ := s.h.Session(c)
	s.consentPage(c, p, id, csrf)
}

// consentPage asks the person to allow the client what it asked for.
// The name and the mark are the client's own say (it registered itself);
// the host its answer goes to is named beside them, and that it cannot
// make up.
func (s *server) consentPage(c *gin.Context, p *authorizeParams, id ext.Identity, csrf string) {
	ws, _ := s.h.TenantOf(c)
	scopeText := map[string]string{
		ScopeProjectsRead:  "See your projects: their status, logs and metrics",
		ScopeProjectsWrite: "Change your projects: deploy, scale, configure",
	}
	page := pages.ConsentPage{
		Title:     "Allow " + p.client.Name + "?",
		Client:    p.client.Name,
		Icon:      clientMark(p.client.Name),
		Initial:   initialOf(p.client.Name),
		Workspace: firstNonEmpty(ws.Name, "this workspace"),
		Account:   firstNonEmpty(id.Email, id.Name),
		Host:      firstNonEmpty(hostOf(p.redirectURI), originOf(p.redirectURI)),
	}
	for _, sc := range p.scope {
		page.Scopes = append(page.Scopes, pages.Scope{Text: firstNonEmpty(scopeText[sc], sc)})
	}
	for _, f := range [][2]string{
		{"client_id", p.client.ClientID}, {"redirect_uri", p.redirectURI}, {"scope", strings.Join(p.scope, " ")},
		{"state", p.state}, {"code_challenge", p.codeChallenge}, {"code_challenge_method", "S256"},
		{"response_type", "code"}, {"resource", p.resource}, {"csrf", csrf},
	} {
		page.Fields = append(page.Fields, pages.Field{Name: f[0], Value: f[1]})
	}
	c.Header("Cache-Control", "no-store")
	// Chrome and Safari hold the redirect that follows a form submission
	// to the page's form-action; the answer goes to the client's redirect
	// URI, so that origin is allowed here (and only here).
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self' "+originOf(p.redirectURI))
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(pages.HTML(pages.Consent, page)))
}

// clientMark is the mark the consent page has for a client, by the name
// it gave itself; "" for one it has none for.
func clientMark(name string) string {
	n := strings.ToLower(name)
	for _, m := range []struct {
		mark  string
		names []string
	}{
		{"claude", []string{"claude"}},
		{"openai", []string{"chatgpt", "openai", "codex"}},
		{"gemini", []string{"gemini"}},
		{"copilot", []string{"copilot"}},
		{"cursor", []string{"cursor"}},
		{"vscode", []string{"vs code", "vscode", "visual studio code"}},
		{"warp", []string{"warp"}},
	} {
		for _, word := range m.names {
			if strings.Contains(n, word) {
				return m.mark
			}
		}
	}
	return ""
}

// initialOf is the first letter of a name, as the mark of a client the
// page has none for.
func initialOf(name string) string {
	for _, r := range strings.TrimSpace(name) {
		return strings.ToUpper(string(r))
	}
	return "?"
}

// originOf is scheme://host[:port] of a URL, as a CSP source.
func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return ""
	}
	if u.Host == "" {
		return u.Scheme + ":" // a custom scheme (desktop clients)
	}
	return u.Scheme + "://" + u.Host
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Host
	}
	return raw
}

// oauthDecide is POST /oauth/authorize: the consent form's answer.
func (s *server) oauthDecide(c *gin.Context) {
	p, err := s.authorizeParamsOf(c, c.PostForm)
	if err != nil {
		s.h.EdgePage(c, http.StatusBadRequest, "This connection cannot be authorised", err.Error())
		return
	}
	// The form carries the session's CSRF token as a field (a plain HTML
	// form sets no header): checked here, the way sessionAuth checks the
	// header on API calls.
	csrf, identity, found := s.h.Session(c)
	if !found {
		q := c.Request.PostForm
		q.Del("csrf")
		q.Del("decision")
		c.Redirect(http.StatusFound, "/?next="+url.QueryEscape("/oauth/authorize?"+q.Encode()))
		return
	}
	if subtle.ConstantTimeCompare([]byte(csrf), []byte(c.PostForm("csrf"))) != 1 {
		s.h.EdgePage(c, http.StatusForbidden, "The form expired", "Open the connection again from your application.")
		return
	}
	ext.SetIdentity(c, identity)
	if c.PostForm("decision") != "allow" {
		s.h.Audit(c, "", "oauth.denied", p.client.Name, "")
		redirectWithError(c, p, "access_denied", "the person denied the request")
		return
	}
	if p.codeChallenge == "" {
		redirectWithError(c, p, "invalid_request", "PKCE with S256 is required")
		return
	}
	id, _ := ext.IdentityFrom(c)
	ws, _ := s.h.TenantOf(c)
	code := oauthRandom(32)
	if err := s.h.Store().PutOAuthCode(c.Request.Context(), store.OAuthCode{
		Hash: hashToken(code), WorkspaceID: ws.ID, ClientID: p.client.ClientID, Email: id.Email, Subject: id.Subject,
		Scope: strings.Join(p.scope, " "), RedirectURI: p.redirectURI, CodeChallenge: p.codeChallenge, Resource: p.resource,
		ExpiresAt: time.Now().Add(oauthCodeTTL),
	}); err != nil {
		redirectWithError(c, p, "server_error", err.Error())
		return
	}
	s.h.Audit(c, "", "oauth.allowed", p.client.Name, "scope "+strings.Join(p.scope, " "))
	u, _ := url.Parse(p.redirectURI)
	q := u.Query()
	q.Set("code", code)
	if p.state != "" {
		q.Set("state", p.state)
	}
	u.RawQuery = q.Encode()
	c.Redirect(http.StatusFound, u.String())
}

// clientAuth checks the client's credentials at the token endpoint: a
// public client sends none; a confidential one its secret, in the body or
// as HTTP basic.
func (s *server) clientAuth(c *gin.Context) (*store.OAuthClient, bool) {
	clientID, secret := c.PostForm("client_id"), c.PostForm("client_secret")
	if u, pw, ok := c.Request.BasicAuth(); ok {
		clientID, secret = u, pw
	}
	if clientID == "" {
		return nil, false
	}
	client, err := s.h.Store().OAuthClientByID(c.Request.Context(), clientID)
	if err != nil {
		return nil, false
	}
	if client.SecretHash != "" && subtle.ConstantTimeCompare([]byte(client.SecretHash), []byte(hashToken(secret))) != 1 {
		return nil, false
	}
	ws, err := s.h.TenantOf(c)
	if err != nil || ws.ID != client.WorkspaceID {
		return nil, false
	}
	return client, true
}

// oauthToken is POST /oauth/token: codes and refresh tokens become access
// tokens.
func (s *server) oauthToken(c *gin.Context) {
	client, ok := s.clientAuth(c)
	if !ok {
		c.Header("WWW-Authenticate", `Basic realm="shpyrd"`)
		oauthError(c, http.StatusUnauthorized, "invalid_client", "unknown client or wrong secret")
		return
	}
	ws, _ := s.h.TenantOf(c)
	ctx := c.Request.Context()
	switch c.PostForm("grant_type") {
	case "authorization_code":
		code, err := s.h.Store().TakeOAuthCode(ctx, hashToken(c.PostForm("code")))
		if err != nil || code.ClientID != client.ClientID || code.WorkspaceID != ws.ID {
			oauthError(c, http.StatusBadRequest, "invalid_grant", "the code is unknown, used or expired")
			return
		}
		if r := c.PostForm("redirect_uri"); r != "" && r != code.RedirectURI {
			oauthError(c, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match")
			return
		}
		verifier := c.PostForm("code_verifier")
		sum := sha256.Sum256([]byte(verifier))
		if verifier == "" || base64.RawURLEncoding.EncodeToString(sum[:]) != code.CodeChallenge {
			oauthError(c, http.StatusBadRequest, "invalid_grant", "the PKCE verifier does not match")
			return
		}
		s.issueTokens(c, ws, client, code.Email, code.Subject, code.Scope, nil)
	case "refresh_token":
		tok, err := s.h.Store().OAuthTokenByHash(ctx, hashToken(c.PostForm("refresh_token")))
		if err != nil || tok.ClientID != client.ClientID || tok.WorkspaceID != ws.ID {
			oauthError(c, http.StatusBadRequest, "invalid_grant", "the refresh token is unknown, revoked or expired")
			return
		}
		scope := tok.Scope
		if asked := strings.Fields(c.PostForm("scope")); len(asked) > 0 {
			// A narrower scope may be asked for, never a wider one.
			have := map[string]bool{}
			for _, sc := range strings.Fields(tok.Scope) {
				have[sc] = true
			}
			var keep []string
			for _, sc := range asked {
				if have[sc] {
					keep = append(keep, sc)
				}
			}
			if len(keep) == 0 {
				oauthError(c, http.StatusBadRequest, "invalid_scope", "the token does not hold that scope")
				return
			}
			scope = strings.Join(keep, " ")
		}
		s.issueTokens(c, ws, client, tok.Email, "", scope, tok)
	default:
		oauthError(c, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
	}
}

// issueTokens answers the token endpoint: a fresh access token, and a
// refresh token (new, or rotated from previous).
func (s *server) issueTokens(c *gin.Context, ws *store.Workspace, client *store.OAuthClient, email, subject, scope string, previous *store.OAuthToken) {
	ctx := c.Request.Context()
	// The person as the workspace knows them: suspended people get nothing.
	person, err := s.h.Store().GetIdentity(ctx, ws.Slug, email)
	if err != nil || person.Status == store.StatusSuspended {
		oauthError(c, http.StatusBadRequest, "invalid_grant", "the account is not available")
		return
	}
	now := time.Now()
	claims := oauthClaims{
		Issuer: s.oauthIssuer(c), Subject: person.ID, Audience: s.mcpResource(c), Email: person.Email, Name: person.Name,
		Workspace: ws.Slug, Scope: scope, ClientID: client.ClientID, IssuedAt: now.Unix(), ExpiresAt: now.Add(oauthAccessTTL).Unix(),
	}
	access, err := s.h.Sign(oauthTokenType, claims)
	if err != nil {
		oauthError(c, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	refresh := oauthRandom(32)
	if previous != nil {
		if err := s.h.Store().RotateOAuthToken(ctx, previous.ID, hashToken(refresh), now.Add(oauthRefreshTTL)); err != nil {
			oauthError(c, http.StatusBadGateway, "server_error", err.Error())
			return
		}
	} else {
		if _, err := s.h.Store().CreateOAuthToken(ctx, ws.Slug, store.OAuthToken{ClientID: client.ClientID, Email: person.Email, Scope: scope, Hash: hashToken(refresh), ExpiresAt: now.Add(oauthRefreshTTL)}); err != nil {
			oauthError(c, http.StatusBadGateway, "server_error", err.Error())
			return
		}
		s.h.AuditActor(c, person.Email, "oauth.token.issued", client.Name, "scope "+scope)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"access_token": access, "token_type": "Bearer", "expires_in": int(oauthAccessTTL.Seconds()),
		"refresh_token": refresh, "scope": scope,
	})
}

// oauthRevoke is POST /oauth/revoke (RFC 7009): a refresh token stops.
func (s *server) oauthRevoke(c *gin.Context) {
	client, ok := s.clientAuth(c)
	if !ok {
		oauthError(c, http.StatusUnauthorized, "invalid_client", "unknown client or wrong secret")
		return
	}
	ws, _ := s.h.TenantOf(c)
	if tok, err := s.h.Store().OAuthTokenByHash(c.Request.Context(), hashToken(c.PostForm("token"))); err == nil && tok.ClientID == client.ClientID {
		_ = s.h.Store().DeleteOAuthToken(c.Request.Context(), ws.Slug, tok.ID)
		s.h.AuditActor(c, tok.Email, "oauth.token.revoked", client.Name, "by the client")
	}
	c.Status(http.StatusOK) // always: RFC 7009 §2.2
}

// identifyWithOAuth accepts one of our access tokens as the request's
// identity: the person it was issued to, with their roles narrowed to the
// token's scope. It returns false for anything that is not such a token.
func (s *server) identifyWithOAuth(c *gin.Context) bool {
	tok := bearerOf(c)
	if tok == "" || api.IsAPIToken(tok) || strings.Count(tok, ".") != 2 || !strings.HasPrefix(tok, "eyJ") {
		return false
	}
	var claims oauthClaims
	if err := s.h.Verify(tok, oauthTokenType, &claims); err != nil {
		return false
	}
	if time.Now().Unix() > claims.ExpiresAt {
		c.Header("WWW-Authenticate", `Bearer realm="shpyrd", error="invalid_token", error_description="expired"`)
		abort(c, http.StatusUnauthorized, errors.New("the access token expired; refresh it"))
		return true
	}
	ws, err := s.h.TenantOf(c)
	if err != nil || ws.Slug != claims.Workspace {
		abort(c, http.StatusUnauthorized, errors.New("this token belongs to another workspace"))
		return true
	}
	ctx := c.Request.Context()
	person, err := s.h.Store().GetIdentity(ctx, ws.Slug, claims.Email)
	if err != nil || person.Status == store.StatusSuspended {
		abort(c, http.StatusUnauthorized, errors.New("the token's owner is not available"))
		return true
	}
	owner := ext.Identity{Subject: person.ID, Email: person.Email, Name: person.Name, Provider: person.Provider, Groups: person.Groups}
	roles, err := s.h.RolesIn(ctx, ws.Slug, owner)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return true
	}
	if !strings.Contains(" "+claims.Scope+" ", " "+ScopeProjectsWrite+" ") {
		roles = readOnlyRoles(roles)
	}
	roles.Workspace = "" // owner actions need a person, not a token
	ext.SetIdentity(c, ext.Identity{Subject: person.ID, Email: person.Email, Name: person.Name, Provider: "oauth"})
	s.h.SetRoles(c, roles)
	c.Set(ctxOAuthClient, claims.ClientID)
	return true
}

const ctxOAuthClient = "shpyrd.oauth.client"

// readOnlyRoles narrows roles to reading: platform admins become platform
// viewers, every project role above viewer becomes viewer.
func readOnlyRoles(r authz.Roles) authz.Roles {
	out := authz.Roles{Enforced: r.Enforced, Suspended: r.Suspended, Projects: map[string]string{}}
	if r.Platform != "" {
		out.Platform = shpyrdv1.RolePlatformViewer
	}
	for p, role := range r.Projects {
		if authz.RankRole(role) > authz.RankRole(shpyrdv1.RoleViewer) {
			role = shpyrdv1.RoleViewer
		}
		out.Projects[p] = role
	}
	return out
}

// ---- the person's connected applications ---------------------------------------

// ConnectionView is one client a person allowed (a refresh token).
type ConnectionView struct {
	ID         string     `json:"id"`
	Client     string     `json:"client"`
	ClientID   string     `json:"clientId"`
	Email      string     `json:"email"`
	Scope      string     `json:"scope"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

// listConnections is GET /api/workspace/connections: the caller's own
// connected applications (every person's for workspace admins).
func (s *server) listConnections(c *gin.Context) {
	roles, _ := s.h.RolesOf(c)
	id, _ := ext.IdentityFrom(c)
	email := id.Email
	if roles.Can(authz.ClusterAdmin, "") && c.Query("all") == "true" {
		email = ""
	}
	list, err := s.h.Store().ListOAuthTokens(c.Request.Context(), s.h.WorkspaceSlug(c), email)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	out := make([]ConnectionView, 0, len(list))
	for _, t := range list {
		out = append(out, ConnectionView{ID: t.ID, Client: firstNonEmpty(t.ClientName, t.ClientID), ClientID: t.ClientID, Email: t.Email, Scope: t.Scope, CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt, LastUsedAt: t.LastUsedAt})
	}
	c.JSON(http.StatusOK, out)
}

// deleteConnection is DELETE /api/workspace/connections/:id: the person
// (or an admin) revokes a client's refresh token; access tokens already
// issued stop within the hour.
func (s *server) deleteConnection(c *gin.Context) {
	roles, _ := s.h.RolesOf(c)
	id, _ := ext.IdentityFrom(c)
	list, err := s.h.Store().ListOAuthTokens(c.Request.Context(), s.h.WorkspaceSlug(c), "")
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	for _, t := range list {
		if t.ID == c.Param("id") {
			if !strings.EqualFold(t.Email, id.Email) && !roles.Can(authz.ClusterAdmin, "") {
				abort(c, http.StatusForbidden, errors.New("that connection is someone else's"))
				return
			}
			if err := s.h.Store().DeleteOAuthToken(c.Request.Context(), s.h.WorkspaceSlug(c), t.ID); err != nil {
				storeErr(c, err, "connection")
				return
			}
			s.h.Audit(c, "", "oauth.token.revoked", firstNonEmpty(t.ClientName, t.ClientID), "of "+t.Email)
			c.Status(http.StatusNoContent)
			return
		}
	}
	abort(c, http.StatusNotFound, errors.New("connection not found"))
}
