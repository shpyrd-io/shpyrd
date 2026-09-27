package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The OAuth 2.1 server and the MCP endpoint (RFC-0032): an MCP client
// registers itself, the person consents, PKCE codes become tokens, tools
// answer within the person's roles and the token's scope, refresh tokens
// rotate, and connections are listed and revoked.
func TestOAuthAndMCP(t *testing.T) {
	shop := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}, Status: shpyrdv1.AppStatus{Phase: shpyrdv1.PhaseRunning, URL: "https://shop.example.test"}}
	secret := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "vault", Namespace: "app-vault"}}
	s, _ := newTestServer(t, nil, []client.Object{shop, secret})
	s.authz.TTL = 1
	ctx := t.Context()
	_ = ctx
	form := func(path string, v url.Values, cookie, csrf string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}

	// Discovery.
	rec := do(t, s, "GET", "/.well-known/oauth-authorization-server", "", false)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"registration_endpoint":"https://shpyrd.example.test/oauth/register"`) || !strings.Contains(rec.Body.String(), `"code_challenge_methods_supported":["S256"]`) {
		t.Fatalf("metadata: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "GET", "/.well-known/oauth-protected-resource/mcp", "", false); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"resource":"https://shpyrd.example.test/mcp"`) {
		t.Errorf("resource metadata: %d %s", rec.Code, rec.Body.String())
	}
	// An unauthenticated MCP call is told where to authorize.
	rec = do(t, s, "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, false)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Header().Get("WWW-Authenticate"), `resource_metadata="https://shpyrd.example.test/.well-known/oauth-protected-resource"`) {
		t.Fatalf("mcp anonymous: %d %q", rec.Code, rec.Header().Get("WWW-Authenticate"))
	}

	// Registration: bad redirect URIs are refused; a public client is made.
	if rec := do(t, s, "POST", "/oauth/register", `{"client_name":"Evil","redirect_uris":["http://evil.example/cb"]}`, false); rec.Code != http.StatusBadRequest {
		t.Errorf("http redirect: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, s, "POST", "/oauth/register", `{"client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback","http://localhost:6274/callback"],"token_endpoint_auth_method":"none","grant_types":["authorization_code","refresh_token"]}`, false)
	if rec.Code != http.StatusCreated || strings.Contains(rec.Body.String(), "client_secret") {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &reg)

	// The person: a developer on shop, nothing on vault.
	ada := ext.Identity{Subject: "u1", Email: "ada@example.test", Name: "Ada", Provider: "local"}
	if _, err := s.store.TouchIdentity(ctx, store.DefaultWorkspace, store.Identity{Email: ada.Email, Name: ada.Name, Provider: "local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.AddGrant(ctx, store.DefaultWorkspace, store.Grant{Project: "shop", Role: shpyrdv1.RoleDeveloper, User: ada.Email}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.PutMembership(ctx, store.DefaultWorkspace, "owner@example.test", store.WorkspaceRoleOwner); err != nil {
		t.Fatal(err)
	}
	sid, csrf := signIn(t, s, ada)

	// PKCE.
	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	authz := url.Values{"response_type": {"code"}, "client_id": {reg.ClientID}, "redirect_uri": {"https://claude.ai/api/mcp/auth_callback"}, "state": {"xyz"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "scope": {"projects:read"}}
	// Signed out: to the sign-in page, and back.
	rec = do(t, s, "GET", "/oauth/authorize?"+authz.Encode(), "", false)
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/?next=%2Foauth%2Fauthorize") {
		t.Fatalf("authorize signed out: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	// Signed in: the consent page names the client and the scope.
	rec = doCookie(t, s, "GET", "/oauth/authorize?"+authz.Encode(), "", sid, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Allow Claude") || !strings.Contains(rec.Body.String(), "See your projects") || !strings.Contains(rec.Body.String(), `name="csrf" value="`+csrf+`"`) {
		t.Fatalf("consent page: %d %s", rec.Code, rec.Body.String()[:min(300, len(rec.Body.String()))])
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "form-action 'self' https://claude.ai") {
		t.Errorf("consent page must allow the redirect in form-action: %q", csp)
	}
	// Without PKCE the client is told at its redirect URI.
	noPKCE := url.Values{}
	for k, v := range authz {
		noPKCE[k] = v
	}
	noPKCE.Del("code_challenge")
	if rec := doCookie(t, s, "GET", "/oauth/authorize?"+noPKCE.Encode(), "", sid, ""); rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "error=invalid_request") {
		t.Errorf("no pkce: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	// Deny.
	decision := url.Values{}
	for k, v := range authz {
		decision[k] = v
	}
	decision.Set("csrf", csrf)
	decision.Set("decision", "deny")
	if rec := form("/oauth/authorize", decision, sid, csrf); rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "error=access_denied") || !strings.Contains(rec.Header().Get("Location"), "state=xyz") {
		t.Errorf("deny: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	// Wrong CSRF.
	decision.Set("decision", "allow")
	decision.Set("csrf", "nope")
	if rec := form("/oauth/authorize", decision, sid, csrf); rec.Code != http.StatusForbidden {
		t.Errorf("bad csrf: %d", rec.Code)
	}
	// Allow: a code at the redirect URI.
	decision.Set("csrf", csrf)
	rec = form("/oauth/authorize", decision, sid, csrf)
	if rec.Code != http.StatusFound {
		t.Fatalf("allow: %d %s", rec.Code, rec.Body.String())
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	code := loc.Query().Get("code")
	if loc.Host != "claude.ai" || code == "" || loc.Query().Get("state") != "xyz" {
		t.Fatalf("redirect: %s", loc)
	}
	// Exchange: wrong verifier, then right.
	if rec := form("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {reg.ClientID}, "code": {code}, "code_verifier": {"wrong"}, "redirect_uri": {"https://claude.ai/api/mcp/auth_callback"}}, "", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("wrong verifier: %d %s", rec.Code, rec.Body.String())
	}
	// The code was consumed by the failed attempt: get another.
	rec = form("/oauth/authorize", decision, sid, csrf)
	loc, _ = url.Parse(rec.Header().Get("Location"))
	code = loc.Query().Get("code")
	rec = form("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {reg.ClientID}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {"https://claude.ai/api/mcp/auth_callback"}}, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("token: %d %s", rec.Code, rec.Body.String())
	}
	var tokens struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		Type    string `json:"token_type"`
		Scope   string `json:"scope"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &tokens)
	if tokens.Type != "Bearer" || tokens.Access == "" || tokens.Refresh == "" || tokens.Scope != "projects:read" {
		t.Fatalf("tokens: %+v", tokens)
	}
	if rec := form("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {reg.ClientID}, "code": {code}, "code_verifier": {verifier}}, "", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("code reuse: %d", rec.Code)
	}

	// The access token works on the API as Ada, read-only: a viewer on
	// shop (she is a developer), nothing on vault; no deploys.
	bearer := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer "+tokens.Access)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := bearer("GET", "/api/me", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"provider":"oauth"`) || !strings.Contains(rec.Body.String(), `"shop":"viewer"`) {
		t.Errorf("me with access token: %d %s", rec.Code, rec.Body.String())
	}
	if rec := bearer("GET", "/api/projects/vault", ""); rec.Code != http.StatusForbidden {
		t.Errorf("vault: %d", rec.Code)
	}
	if rec := bearer("POST", "/api/projects/shop/deploy", `{"image":"x"}`); rec.Code != http.StatusForbidden {
		t.Errorf("deploy with a read token: %d %s", rec.Code, rec.Body.String())
	}
	// MCP: initialize, tools, calls.
	mcp := func(body string) *httptest.ResponseRecorder { return bearer("POST", "/mcp", body) }
	rec = mcp(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"claude","version":"1"}}}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"protocolVersion":"2025-03-26"`) || !strings.Contains(rec.Body.String(), `"name":"shpyrd on shpyrd"`) {
		t.Fatalf("initialize: %d %s", rec.Code, rec.Body.String())
	}
	if rec := mcp(`{"jsonrpc":"2.0","method":"notifications/initialized"}`); rec.Code != http.StatusAccepted {
		t.Errorf("initialized notification: %d %s", rec.Code, rec.Body.String())
	}
	rec = mcp(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"get_logs"`) || !strings.Contains(rec.Body.String(), `"readOnlyHint":true`) {
		t.Fatalf("tools/list: %d %s", rec.Code, rec.Body.String())
	}
	rec = mcp(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_projects","arguments":{}}}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "shop") || strings.Contains(rec.Body.String(), "vault") || !strings.Contains(rec.Body.String(), `"isError":false`) {
		t.Fatalf("list_projects: %d %s", rec.Code, rec.Body.String())
	}
	rec = mcp(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_project","arguments":{"project":"Shop"}}}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `\"phase\": \"Running\"`) {
		t.Errorf("get_project: %d %s", rec.Code, rec.Body.String())
	}
	rec = mcp(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_project","arguments":{"project":"vault"}}}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"isError":true`) || !strings.Contains(rec.Body.String(), "no project") {
		t.Errorf("get_project on a hidden project: %d %s", rec.Code, rec.Body.String())
	}
	rec = mcp(`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_metrics","arguments":{"project":"shop","range":"1h"}}}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"isError":`) {
		t.Errorf("get_metrics: %d %s", rec.Code, rec.Body.String())
	}
	if rec := mcp(`{"jsonrpc":"2.0","id":7,"method":"nope"}`); !strings.Contains(rec.Body.String(), `"code":-32601`) {
		t.Errorf("unknown method: %s", rec.Body.String())
	}
	if rec := do(t, s, "GET", "/mcp", "", false); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /mcp: %d", rec.Code)
	}

	// Connections: Ada sees hers and revokes; refresh rotates; a revoked
	// refresh token is refused.
	rec = doCookie(t, s, "GET", "/api/workspace/connections", "", sid, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"client":"Claude"`) {
		t.Fatalf("connections: %d %s", rec.Code, rec.Body.String())
	}
	rec = form("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {reg.ClientID}, "refresh_token": {tokens.Refresh}}, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body.String())
	}
	var refreshed struct {
		Refresh string `json:"refresh_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &refreshed)
	if refreshed.Refresh == "" || refreshed.Refresh == tokens.Refresh {
		t.Errorf("refresh token did not rotate")
	}
	if rec := form("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {reg.ClientID}, "refresh_token": {tokens.Refresh}}, "", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("old refresh token: %d", rec.Code)
	}
	var conns []ConnectionView
	rec = doCookie(t, s, "GET", "/api/workspace/connections", "", sid, "")
	_ = json.Unmarshal(rec.Body.Bytes(), &conns)
	if len(conns) != 1 {
		t.Fatalf("connections = %+v", conns)
	}
	if rec := doCookie(t, s, "DELETE", "/api/workspace/connections/"+conns[0].ID, "", sid, csrf); rec.Code != http.StatusNoContent {
		t.Errorf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	if rec := form("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {reg.ClientID}, "refresh_token": {refreshed.Refresh}}, "", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("revoked refresh token: %d", rec.Code)
	}
	// The MCP name is a workspace setting, shown at initialize.
	if rec := do(t, s, "PATCH", "/api/workspace", `{"mcpName":"Acme Ops"}`, true); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"mcpName":"Acme Ops"`) {
		t.Fatalf("mcp name: %d %s", rec.Code, rec.Body.String())
	}
	rec = mcp(`{"jsonrpc":"2.0","id":8,"method":"initialize","params":{}}`)
	if !regexp.MustCompile(`"name":"Acme Ops"`).MatchString(rec.Body.String()) {
		t.Errorf("renamed server: %s", rec.Body.String())
	}
}
