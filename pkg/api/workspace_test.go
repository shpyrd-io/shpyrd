package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

func adminJSON(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// Domain claims (RFC-0033 phase 3): a claim carries a TXT record to publish;
// verification looks it up; a verified claim with a connector routes its
// accounts to that connector at sign-in.
func TestDomainClaimsAndAdmission(t *testing.T) {
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}}
	s, _ := newTestServer(t, nil, []client.Object{app})
	s.authz.TTL = 1
	ctx := context.Background()
	records := map[string][]string{}
	s.lookupTXT = func(_ context.Context, name string) ([]string, error) { return records[name], nil }

	rec := adminJSON(t, s, "POST", "/api/workspace/domain-claims", `{"domain":"Acme.com","connector":"google"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"record":"_shpyrd-verify.acme.com"`) || !strings.Contains(rec.Body.String(), `"verified":false`) {
		t.Fatalf("claim = %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminJSON(t, s, "POST", "/api/workspace/domain-claims", `{"domain":"not a domain"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad domain = %d", rec.Code)
	}
	// Not published yet: 409 with the record to add.
	if rec := adminJSON(t, s, "POST", "/api/workspace/domain-claims/acme.com/verify", ""); rec.Code != http.StatusConflict {
		t.Errorf("verify without record = %d %s", rec.Code, rec.Body.String())
	}
	claims, _ := s.store.ListDomainClaims(ctx, store.DefaultWorkspace)
	records["_shpyrd-verify.acme.com"] = []string{"v=spf1 -all", "shpyrd-verify=" + claims[0].Token}
	if rec := adminJSON(t, s, "POST", "/api/workspace/domain-claims/acme.com/verify", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"verified":true`) {
		t.Fatalf("verify = %d %s", rec.Code, rec.Body.String())
	}

	// Admission: @acme.com must come through google; others are free (open policy).
	if err := s.admitSignIn(ctx, store.DefaultWorkspace, ext.Identity{Email: "joao@acme.com", Provider: "local"}); err == nil || !strings.Contains(err.Error(), "sign in with") {
		t.Errorf("claimed domain through another method: %v", err)
	}
	if err := s.admitSignIn(ctx, store.DefaultWorkspace, ext.Identity{Email: "joao@acme.com", Provider: "google"}); err != nil {
		t.Errorf("claimed domain through its connector: %v", err)
	}
	if err := s.admitSignIn(ctx, store.DefaultWorkspace, ext.Identity{Email: "guest@other.test", Provider: "local"}); err != nil {
		t.Errorf("open policy: %v", err)
	}
	if err := s.admitSignIn(ctx, store.DefaultWorkspace, ext.Identity{Subject: "admin-token", Provider: "token"}); err != nil {
		t.Errorf("token: %v", err)
	}

	// Company policy: only claimed domains join; known people keep signing in.
	if rec := adminJSON(t, s, "PATCH", "/api/workspace", `{"joinPolicy":"company"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"joinPolicy":"company"`) {
		t.Fatalf("policy = %d %s", rec.Code, rec.Body.String())
	}
	if err := s.admitSignIn(ctx, store.DefaultWorkspace, ext.Identity{Email: "guest@other.test", Provider: "local"}); err == nil {
		t.Error("company policy must refuse strangers")
	}
	if _, err := s.store.TouchIdentity(ctx, store.DefaultWorkspace, store.Identity{Email: "guest@other.test", Provider: "local"}); err != nil {
		t.Fatal(err)
	}
	if err := s.admitSignIn(ctx, store.DefaultWorkspace, ext.Identity{Email: "guest@other.test", Provider: "local"}); err != nil {
		t.Errorf("known person under company policy: %v", err)
	}
	if err := s.admitSignIn(ctx, store.DefaultWorkspace, ext.Identity{Email: "maria@acme.com", Provider: "google"}); err != nil {
		t.Errorf("company account: %v", err)
	}

	// Listed policy: named in a team or a grant first.
	if rec := adminJSON(t, s, "PATCH", "/api/workspace", `{"joinPolicy":"listed"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	if err := s.admitSignIn(ctx, store.DefaultWorkspace, ext.Identity{Email: "new@other.test", Provider: "local"}); err == nil {
		t.Error("listed policy must refuse the unlisted")
	}
	if _, _, err := s.store.PutTeam(ctx, store.DefaultWorkspace, store.Team{Name: "web", Members: []string{"new@other.test"}}); err != nil {
		t.Fatal(err)
	}
	s.authz.Invalidate()
	if err := s.admitSignIn(ctx, store.DefaultWorkspace, ext.Identity{Email: "new@other.test", Provider: "local"}); err != nil {
		t.Errorf("listed person: %v", err)
	}
	if rec := adminJSON(t, s, "PATCH", "/api/workspace", `{"joinPolicy":"whatever"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad policy = %d", rec.Code)
	}

	// Suspended people are refused at sign-in too.
	if _, err := s.store.SetIdentityStatus(ctx, store.DefaultWorkspace, "guest@other.test", store.StatusSuspended); err != nil {
		t.Fatal(err)
	}
	if err := s.admitSignIn(ctx, store.DefaultWorkspace, ext.Identity{Email: "guest@other.test", Provider: "local"}); err == nil || !strings.Contains(err.Error(), "suspended") {
		t.Errorf("suspended at sign-in: %v", err)
	}
	if rec := adminJSON(t, s, "DELETE", "/api/workspace/domain-claims/acme.com", ""); rec.Code != http.StatusNoContent {
		t.Errorf("unclaim = %d", rec.Code)
	}
}

// Branding (RFC-0033): a logo and a colour on the workspace, shown by the
// login page and the launcher; the logo is served on its own, cacheable.
func TestWorkspaceBranding(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	png := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nfake"))
	for body, want := range map[string]int{
		`{"color":"orange"}`:                    http.StatusBadRequest,
		`{"logo":"not a data url"}`:             http.StatusBadRequest,
		`{"logo":"data:text/html;base64,PGI+"}`: http.StatusBadRequest,
		`{"logo":"data:image/png;base64,` + base64.StdEncoding.EncodeToString(make([]byte, 300*1024)) + `"}`: http.StatusBadRequest,
	} {
		if rec := adminJSON(t, s, "PATCH", "/api/workspace", body); rec.Code != want {
			t.Errorf("%.60s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	rec := adminJSON(t, s, "PATCH", "/api/workspace", `{"logo":"`+png+`","color":"#FF4F00"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"color":"#ff4f00"`) || !strings.Contains(rec.Body.String(), `"logoUrl":"/api/workspace/logo?v=`) {
		t.Fatalf("branding: %d %s", rec.Code, rec.Body.String())
	}
	var view WorkspaceView
	_ = json.Unmarshal(rec.Body.Bytes(), &view)
	logo := do(t, s, "GET", view.Branding.LogoURL, "", false)
	if logo.Code != http.StatusOK || logo.Header().Get("Content-Type") != "image/png" || !strings.HasPrefix(logo.Body.String(), "\x89PNG") || !strings.Contains(logo.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("logo: %d %s %q", logo.Code, logo.Header().Get("Content-Type"), logo.Body.String()[:4])
	}
	// The public config carries it for the login page.
	if rec := do(t, s, "GET", "/api/config", "", false); !strings.Contains(rec.Body.String(), `"branding":{"logoUrl":"/api/workspace/logo?v=`) || !strings.Contains(rec.Body.String(), `"color":"#ff4f00"`) {
		t.Errorf("config: %s", rec.Body.String())
	}
	// Removing both clears the branding.
	rec = adminJSON(t, s, "PATCH", "/api/workspace", `{"logo":"","color":""}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"branding"`) {
		t.Errorf("cleared: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "GET", "/api/workspace/logo", "", false); rec.Code != http.StatusNotFound {
		t.Errorf("logo after clearing: %d", rec.Code)
	}
}
