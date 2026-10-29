package api

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/shpyrd-io/shpyrd/pkg/audit"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// githubServer is a test server on a platform with the fake GitHub's App.
func githubServer(t *testing.T, objs ...client.Object) (*Server, client.Client, *githubtest.Server) {
	t.Helper()
	fake := githubtest.NewServer(t)
	s, cr := newTestServer(t, nil, append([]client.Object{fake.Secret("shpyrd-system")}, objs...))
	s.githubApp = fake.Source(cr, "shpyrd-system")
	return s, cr, fake
}

// githubAudits collects what is audited from here on.
func githubAudits(t *testing.T) *[]audit.Entry {
	t.Helper()
	var heard []audit.Entry
	audit.Sink = func(_ audit.Ref, e audit.Entry) { heard = append(heard, e) }
	t.Cleanup(func() { audit.Sink = nil })
	return &heard
}

// connectState starts a connection as the admin token and returns the
// state GitHub carries back.
func connectState(t *testing.T, s *Server) string {
	t.Helper()
	rec := do(t, s, "POST", "/api/workspace/github/connect", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("connect: %d %s", rec.Code, rec.Body.String())
	}
	var v GitHubConnectView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	u, err := url.Parse(v.InstallURL)
	if err != nil || !strings.HasSuffix(u.Path, "/apps/"+githubtest.Slug+"/installations/new") || u.Query().Get("state") == "" {
		t.Fatalf("install URL %q", v.InstallURL)
	}
	return u.Query().Get("state")
}

func installed(t *testing.T, s *Server, id, code, state string) *httptest.ResponseRecorder {
	t.Helper()
	q := url.Values{"installation_id": {id}, "setup_action": {"install"}, "code": {code}, "state": {state}}
	return do(t, s, "GET", "/api/github/installed?"+q.Encode(), "", false)
}

// ticketOf is the finish link the callback sent the browser to, as a
// path on the workspace's host.
func ticketOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	u, err := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusFound || err != nil || u.Path != "/api/workspace/github/finish" || u.Query().Get("ticket") == "" {
		t.Fatalf("callback: %d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	return u.RequestURI()
}

// connected runs the whole flow as the admin token: connect, GitHub's
// callback, and the finish on the workspace's host.
func connected(t *testing.T, s *Server, id string) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, s, "GET", ticketOf(t, installed(t, s, id, githubtest.Code, connectState(t, s))), "", true)
}

func TestGitHubConnectStoresTheInstallationThePersonCanSee(t *testing.T) {
	s, _, fake := githubServer(t)
	heard := githubAudits(t)
	finish := ticketOf(t, installed(t, s, "42", githubtest.Code, connectState(t, s)))
	if list, _ := s.store.GitHubInstallations(context.Background(), store.DefaultWorkspace); len(list) != 0 || len(*heard) != 0 {
		t.Fatalf("the callback alone connects nothing: %+v %+v", list, *heard)
	}
	rec := do(t, s, "GET", finish, "", true)
	if rec.Code != http.StatusFound || !strings.HasSuffix(rec.Header().Get("Location"), "/workspace/git") {
		t.Fatalf("finish: %d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	list, _ := s.store.GitHubInstallations(context.Background(), store.DefaultWorkspace)
	if len(list) != 1 || list[0].ID != 42 || list[0].AccountLogin != "Acme" || list[0].AccountType != "Organization" || list[0].ConnectedBy != "admin token" {
		t.Fatalf("stored %+v", list)
	}
	if len(*heard) != 1 || (*heard)[0].Action != "github.connect" || (*heard)[0].Actor != "admin token" ||
		(*heard)[0].Target != "github installation 42 (Acme)" || (*heard)[0].Detail != "workspace default, by GitHub user "+githubtest.UserLogin {
		t.Errorf("audit %+v", *heard)
	}
	rec = do(t, s, "GET", "/api/workspace/github", "", true)
	var v GitHubView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	if !v.Configured || v.AppURL != fake.App().HTMLURL || len(v.Installations) != 1 || v.Installations[0].Account != "Acme" ||
		v.Installations[0].ManageURL != fake.URL+"/organizations/Acme/settings/installations/42" || v.Installations[0].Suspended {
		t.Errorf("view %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), githubtest.ClientSecret) || strings.Contains(rec.Body.String(), githubtest.UserToken) {
		t.Errorf("a credential in the answer: %s", rec.Body.String())
	}
}

func TestGitHubCallbackRefusesAnInstallationThePersonCannotSee(t *testing.T) {
	s, _, _ := githubServer(t)
	rec := installed(t, s, "99", githubtest.Code, connectState(t, s))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "cannot see installation 99") {
		t.Fatalf("forged id: %d %s", rec.Code, rec.Body.String())
	}
	if list, _ := s.store.GitHubInstallations(context.Background(), store.DefaultWorkspace); len(list) != 0 {
		t.Errorf("nothing is connected: %+v", list)
	}
	if rec := installed(t, s, "42", "stale-code", connectState(t, s)); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Try again") {
		t.Errorf("a code GitHub refuses: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubStateIsUsedOnceAndExpires(t *testing.T) {
	s, _, _ := githubServer(t)
	state := connectState(t, s)
	if rec := installed(t, s, "42", githubtest.Code, state); rec.Code != http.StatusFound {
		t.Fatalf("first: %d %s", rec.Code, rec.Body.String())
	}
	if rec := installed(t, s, "42", githubtest.Code, state); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "used") {
		t.Errorf("replayed: %d %s", rec.Code, rec.Body.String())
	}
	expired := signGitHubState(githubtest.ClientSecret, githubState{Workspace: "default", Actor: "x", Exp: time.Now().Add(-time.Minute).Unix(), Nonce: "n1"})
	otherKey := signGitHubState("not-the-client-secret", githubState{Workspace: "default", Actor: "x", Exp: time.Now().Add(time.Minute).Unix(), Nonce: "n2"})
	fresh := signGitHubState(githubtest.ClientSecret, githubState{Workspace: "default", Actor: "x", Exp: time.Now().Add(time.Minute).Unix(), Nonce: "n3"})
	body, tag, _ := strings.Cut(fresh, ".")
	tampered := body[:len(body)-2] + "AA." + tag
	for name, st := range map[string]string{"expired": expired, "another key": otherKey, "tampered": tampered, "none": "", "garbage": "x.y"} {
		if rec := installed(t, s, "42", githubtest.Code, st); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
}

func TestReadGitHubStateChecksTheTagAndTheTime(t *testing.T) {
	now := time.Now()
	st := githubState{Workspace: "acme", Actor: "ana@acme.test", Exp: now.Add(time.Minute).Unix(), Nonce: "n"}
	got, err := readGitHubState("k", signGitHubState("k", st), now)
	if err != nil || got != st {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := readGitHubState("k", signGitHubState("k", st), now.Add(2*time.Minute)); err == nil {
		t.Error("expired")
	}
	if _, err := readGitHubState("", signGitHubState("", st), now); err == nil {
		t.Error("no key, no state")
	}
}

func TestGitHubCallbackSaysToTryAgainWhenGitHubIsDown(t *testing.T) {
	s, _, fake := githubServer(t)
	state := connectState(t, s)
	fake.SetDown(true)
	rec := installed(t, s, "42", githubtest.Code, state)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Try again") || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubCallbackOfAnUpdateOnlyGoesBack(t *testing.T) {
	s, _, _ := githubServer(t)
	ctx := context.Background()
	if _, err := s.store.CreateWorkspace(ctx, store.Workspace{Slug: "beta", Name: "Beta", Address: "beta"}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.AddGitHubInstallation(ctx, "beta", store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "bob"}); err != nil {
		t.Fatal(err)
	}
	// The generic dashboard: the redirect says nothing of which workspace
	// holds the installation.
	rec := do(t, s, "GET", "/api/github/installed?installation_id=42&setup_action=update", "", false)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != s.dashboardURLOf(nil) {
		t.Errorf("%d %q, want %q", rec.Code, rec.Header().Get("Location"), s.dashboardURLOf(nil))
	}
}

// githubConnection is the workspace's connection of the installation; nil
// when it has none.
func githubConnection(t *testing.T, s *Server, id int64, ws string) *store.GitHubInstallation {
	t.Helper()
	conns, err := s.store.GitHubInstallationConnections(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	for i := range conns {
		if conns[i].Workspace == ws {
			return &conns[i]
		}
	}
	return nil
}

func TestGitHubConnectsAnInstallationAnotherWorkspaceHasToo(t *testing.T) {
	s, _, _ := githubServer(t)
	ctx := context.Background()
	if _, err := s.store.CreateWorkspace(ctx, store.Workspace{Slug: "beta", Name: "Beta"}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.AddGitHubInstallation(ctx, "beta", store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "bob@beta.test"}); err != nil {
		t.Fatal(err)
	}
	heard := githubAudits(t)
	if rec := connected(t, s, "42"); rec.Code != http.StatusFound {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if got := githubConnection(t, s, 42, store.DefaultWorkspace); got == nil || got.AccountLogin != "Acme" {
		t.Errorf("connected here: %+v", got)
	}
	if got := githubConnection(t, s, 42, "beta"); got == nil || got.ConnectedBy != "bob@beta.test" {
		t.Errorf("beta keeps it: %+v", got)
	}
	if len(*heard) != 1 || (*heard)[0].Action != "github.connect" {
		t.Errorf("audit %+v", *heard)
	}
}

func TestGitHubReconnectingToTheSameWorkspaceWorks(t *testing.T) {
	s, _, _ := githubServer(t)
	if rec := connected(t, s, "42"); rec.Code != http.StatusFound {
		t.Fatalf("first: %d %s", rec.Code, rec.Body.String())
	}
	if rec := connected(t, s, "42"); rec.Code != http.StatusFound {
		t.Fatalf("again: %d %s", rec.Code, rec.Body.String())
	}
	if list, _ := s.store.GitHubInstallations(context.Background(), store.DefaultWorkspace); len(list) != 1 {
		t.Errorf("%+v", list)
	}
}

func TestGitHubOnlyAnOwnerOfTheOrgConnectsIt(t *testing.T) {
	for _, tc := range []struct{ role, state string }{{"member", "active"}, {"admin", "pending"}, {"", ""}} {
		s, _, fake := githubServer(t)
		fake.SetOrgRole("Acme", tc.role, tc.state)
		rec := installed(t, s, "42", githubtest.Code, connectState(t, s))
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Only an owner of Acme on GitHub can connect it.") {
			t.Errorf("%s/%s: %d %s", tc.role, tc.state, rec.Code, rec.Body.String())
		}
		if list, _ := s.store.GitHubInstallations(context.Background(), store.DefaultWorkspace); len(list) != 0 {
			t.Errorf("%s: stored %+v", tc.role, list)
		}
	}
	s, _, _ := githubServer(t) // the fake's person owns Acme
	if rec := connected(t, s, "42"); rec.Code != http.StatusFound {
		t.Errorf("owner: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubOnlyThePersonConnectsTheirOwnAccount(t *testing.T) {
	s, _, fake := githubServer(t)
	fake.SetUserInstallations(githubtest.Installation{ID: 7, Login: "Ana", Type: "User"})
	if rec := connected(t, s, "7"); rec.Code != http.StatusFound {
		t.Fatalf("their own: %d %s", rec.Code, rec.Body.String())
	}
	if got := githubConnection(t, s, 7, store.DefaultWorkspace); got == nil || got.AccountLogin != "Ana" || got.AccountType != "User" {
		t.Errorf("stored %+v", got)
	}
	s, _, fake = githubServer(t)
	fake.SetUserInstallations(githubtest.Installation{ID: 7, Login: "ana", Type: "User"})
	fake.SetUserLogin("bob")
	if rec := installed(t, s, "7", githubtest.Code, connectState(t, s)); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Only an owner of ana on GitHub can connect it.") {
		t.Errorf("someone else's: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubFinishNeedsTheSessionThatStarted(t *testing.T) {
	s, _, _ := githubServer(t)
	s.authz.TTL = 1
	ctx := context.Background()
	// person signs in a workspace admin: its session, CSRF token and
	// subject.
	person := func(email string) (string, string, string) {
		p, err := s.store.TouchIdentity(ctx, store.DefaultWorkspace, store.Identity{Email: email, Provider: "local"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.store.PutMembership(ctx, store.DefaultWorkspace, p.ID, store.WorkspaceRoleAdmin); err != nil {
			t.Fatal(err)
		}
		sid, csrf := signIn(t, s, ext.Identity{Subject: p.ID, Email: email, Provider: "local"})
		return sid, csrf, p.ID
	}
	anaSID, anaCSRF, anaSub := person("ana@example.test")
	bobSID, _, _ := person("bob@example.test")
	start := func() string {
		rec := doCookie(t, s, "POST", "/api/workspace/github/connect", "", anaSID, anaCSRF)
		var v GitHubConnectView
		_ = json.Unmarshal(rec.Body.Bytes(), &v)
		u, _ := url.Parse(v.InstallURL)
		return ticketOf(t, installed(t, s, "42", githubtest.Code, u.Query().Get("state")))
	}
	refused := func(name string, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code == http.StatusFound || rec.Code == http.StatusOK || !strings.Contains(rec.Body.String(), "started by someone else, or has expired") {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	stored := func() int {
		list, _ := s.store.GitHubInstallations(ctx, store.DefaultWorkspace)
		return len(list)
	}

	finish := start()
	refused("another admin's session", doCookie(t, s, "GET", finish, "", bobSID, ""))
	refused("the admin token", do(t, s, "GET", finish, "", true))
	if stored() != 0 {
		t.Fatal("stored for someone else")
	}
	if rec := doCookie(t, s, "GET", finish, "", anaSID, ""); rec.Code != http.StatusFound || !strings.HasSuffix(rec.Header().Get("Location"), "/workspace/git") {
		t.Fatalf("ana finishes: %d %s", rec.Code, rec.Body.String())
	}
	if got := githubConnection(t, s, 42, store.DefaultWorkspace); got == nil || got.ConnectedBy != "ana@example.test" {
		t.Errorf("stored %+v", got)
	}
	refused("replayed", doCookie(t, s, "GET", finish, "", anaSID, ""))

	key := githubtest.ClientSecret
	tk := func(ws, provider, subject string, exp time.Time, nonce string) string {
		return "/api/workspace/github/finish?ticket=" + url.QueryEscape(signGitHubTicket(key, githubTicket{
			Workspace: ws, Actor: "ana@example.test", Provider: provider, Subject: subject,
			Installation: 42, Account: "Acme", AccountType: "Organization", GitHubLogin: "ana", Exp: exp.Unix(), Nonce: nonce}))
	}
	soon := time.Now().Add(time.Minute)
	refused("expired", doCookie(t, s, "GET", tk("default", "local", anaSub, time.Now().Add(-time.Second), "t1"), "", anaSID, ""))
	refused("another workspace's", doCookie(t, s, "GET", tk("beta", "local", anaSub, soon, "t2"), "", anaSID, ""))
	state := signGitHubState(key, githubState{Workspace: "default", Actor: "ana@example.test", Provider: "local", Subject: anaSub, Exp: soon.Unix(), Nonce: "t3"})
	refused("a state as a ticket", doCookie(t, s, "GET", "/api/workspace/github/finish?ticket="+url.QueryEscape(state), "", anaSID, ""))
	if _, err := readGitHubTicket(key, state, time.Now()); err == nil {
		t.Error("a state reads as a ticket")
	}
	if _, err := readGitHubState(key, signGitHubTicket(key, githubTicket{Workspace: "default", Actor: "x", Exp: soon.Unix(), Nonce: "t4"}), time.Now()); err == nil {
		t.Error("a ticket reads as a state")
	}
	// The same email is not the same person: another provider's ana, or
	// ana's API token, names her alike but is someone else to the store.
	// (signIn's fixture keys people by email: this one is made by hand,
	// as an SSO sign-in makes it, by its issuer and subject, through a
	// configured provider.)
	s.rp.mu.Lock()
	s.rp.providers["okta"] = &oidcProvider{OIDCProvider: ext.OIDCProvider{ID: "okta", Issuer: "https://okta.example.test"}}
	s.rp.order = append(s.rp.order, "okta")
	s.rp.mu.Unlock()
	sso, err := s.store.TouchIdentity(ctx, store.DefaultWorkspace, store.Identity{Email: "ana@example.test", Provider: "okta", Issuer: "https://okta.example.test", Subject: "00u-ana"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.PutMembership(ctx, store.DefaultWorkspace, sso.ID, store.WorkspaceRoleAdmin); err != nil {
		t.Fatal(err)
	}
	ssoSub := sso.ID
	if ssoSub == anaSub {
		t.Fatalf("the store made one person of two identities: %s", ssoSub)
	}
	ssoSess, err := s.rp.sessions.create(ctx, store.RealmWorkspace, store.DefaultWorkspace, ext.Identity{Subject: ssoSub, Issuer: "https://okta.example.test",
		ExternalSubject: "00u-ana", EmailVerified: true, Email: "ana@example.test", Provider: "okta"}, "")
	if err != nil {
		t.Fatal(err)
	}
	ssoSID := ssoSess.ID
	refused("ana through another provider", doCookie(t, s, "GET", tk("default", "local", anaSub, soon, "t6"), "", ssoSID, ""))
	refused("ana's API token's ticket in her browser", doCookie(t, s, "GET", tk("default", "api-token", "token:abc", soon, "t7"), "", anaSID, ""))
	// A fresh, valid one still works: the refusals spent nothing.
	if rec := doCookie(t, s, "GET", tk("default", "local", anaSub, soon, "t5"), "", anaSID, ""); rec.Code != http.StatusFound {
		t.Errorf("valid: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "GET", tk("default", "okta", ssoSub, soon, "t8"), "", ssoSID, ""); rec.Code != http.StatusFound {
		t.Errorf("the okta ana finishes her own: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubFinishAsksABrowserWithoutASessionToSignIn(t *testing.T) {
	s, _, _ := githubServer(t)
	finish := ticketOf(t, installed(t, s, "42", githubtest.Code, connectState(t, s)))
	// Not through the sign-in's next: it never goes back to an /api/ path.
	rec := do(t, s, "GET", finish, "", false)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") ||
		!strings.Contains(rec.Body.String(), "then open this link again.") || !strings.Contains(rec.Body.String(), `href="`+finish+`"`) {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
	if list, _ := s.store.GitHubInstallations(context.Background(), store.DefaultWorkspace); len(list) != 0 {
		t.Errorf("stored %+v", list)
	}
}

func TestGitHubAppWithoutTheMembersPermissionSaysSo(t *testing.T) {
	s, _, fake := githubServer(t)
	fake.SetNoMembersPermission(true)
	rec := installed(t, s, "42", githubtest.Code, connectState(t, s))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "check who owns Acme yet: an owner of the App must accept its new Members permission on GitHub") {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubMembershipRefusalsSayWhy(t *testing.T) {
	for name, tc := range map[string]struct {
		message string
		status  int
		says    string
	}{
		"rate limited":    {"API rate limit exceeded for user ID 1.", http.StatusBadGateway, "Try again from the workspace&#39;s Git settings in a minute."},
		"no members":      {"Resource not accessible by integration", http.StatusConflict, "an owner of the App must accept its new Members permission on GitHub"},
		"SAML or another": {"Resource protected by organization SAML enforcement. You must grant your OAuth token access to this organization.", http.StatusForbidden, "GitHub refused to say whether you own Acme. If the organisation requires SAML single sign-on, authorize your GitHub session for it, then connect again."},
	} {
		s, _, fake := githubServer(t)
		fake.SetMembershipRefusal(tc.message)
		rec := installed(t, s, "42", githubtest.Code, connectState(t, s))
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.says) {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
}

func TestGitHubCallbackOfAnInstallStartedOnGitHub(t *testing.T) {
	s, _, _ := githubServer(t)
	rec := do(t, s, "GET", "/api/github/installed?installation_id=42&setup_action=install&code="+githubtest.Code, "", false)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Connect GitHub from your workspace&#39;s settings (Workspace settings → Git) so shpyrd knows which workspace to connect it to.") {
		t.Errorf("no state: %d %s", rec.Code, rec.Body.String())
	}
	if list, _ := s.store.GitHubInstallations(context.Background(), store.DefaultWorkspace); len(list) != 0 {
		t.Errorf("stored %+v", list)
	}
}

func TestGitHubCallbackOfARequestedInstall(t *testing.T) {
	s, _, _ := githubServer(t)
	// GitHub sends no installation: there is none until an owner approves.
	rec := do(t, s, "GET", "/api/github/installed?setup_action=request&state="+url.QueryEscape(connectState(t, s)), "", false)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "An owner of the organisation must approve the request on GitHub; then connect it from Workspace settings → Git.") {
		t.Errorf("request: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubDisconnect(t *testing.T) {
	s, _, _ := githubServer(t)
	ctx := context.Background()
	if err := s.store.AddGitHubInstallation(ctx, store.DefaultWorkspace, store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "ana"}); err != nil {
		t.Fatal(err)
	}
	heard := githubAudits(t)
	if rec := do(t, s, "DELETE", "/api/workspace/github/installations/42", "", true); rec.Code != http.StatusNoContent {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if list, _ := s.store.GitHubInstallations(ctx, store.DefaultWorkspace); len(list) != 0 {
		t.Errorf("still there: %+v", list)
	}
	if len(*heard) != 1 || (*heard)[0].Action != "github.disconnect" || (*heard)[0].Actor != "admin token" || (*heard)[0].Detail != "workspace default" || (*heard)[0].Target != "github installation 42 (Acme)" {
		t.Errorf("audit %+v", *heard)
	}
	if rec := do(t, s, "DELETE", "/api/workspace/github/installations/42", "", true); rec.Code != http.StatusNotFound {
		t.Errorf("twice: %d", rec.Code)
	}
	if rec := do(t, s, "DELETE", "/api/workspace/github/installations/nope", "", true); rec.Code != http.StatusBadRequest {
		t.Errorf("not an id: %d", rec.Code)
	}
}

func TestGitHubRoutesWithoutTheApp(t *testing.T) {
	s, cr := newTestServer(t, nil, nil)
	s.githubApp = githubtest.NewServer(t).Source(cr, "shpyrd-system") // no Secret in the cluster
	rec := do(t, s, "POST", "/api/workspace/github/connect", "", true)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "GitHub is not set up on this platform") || !strings.Contains(rec.Body.String(), "shpyrd-ctl github app create") {
		t.Errorf("connect: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, s, "GET", "/api/workspace/github", "", true)
	var v GitHubView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	if rec.Code != http.StatusOK || v.Configured || !strings.Contains(v.Note, "shpyrd-ctl github app create") || v.Installations == nil {
		t.Errorf("view: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "GET", "/api/workspace/github/repositories", "", true); rec.Code != http.StatusConflict {
		t.Errorf("repositories: %d", rec.Code)
	}
	if rec := do(t, s, "GET", "/api/github/installed?installation_id=42&setup_action=install&code=c&state=s", "", false); rec.Code != http.StatusNotFound {
		t.Errorf("callback: %d", rec.Code)
	}
}

func TestGitHubRoutesNeedTheirRoles(t *testing.T) {
	s, _, _ := githubServer(t)
	s.authz.TTL = 1
	ctx := context.Background()
	person := func(email, role string) (string, string) {
		p, err := s.store.TouchIdentity(ctx, store.DefaultWorkspace, store.Identity{Email: email, Provider: "local"})
		if err != nil {
			t.Fatal(err)
		}
		if role != "" {
			if _, err := s.store.PutMembership(ctx, store.DefaultWorkspace, p.ID, role); err != nil {
				t.Fatal(err)
			}
		}
		return signIn(t, s, ext.Identity{Subject: p.ID, Email: email, Provider: "local"})
	}
	adminSID, adminCSRF := person("admin@example.test", store.WorkspaceRoleAdmin)
	memberSID, memberCSRF := person("member@example.test", store.WorkspaceRoleMember)
	strangerSID, _ := person("stranger@example.test", "")
	if rec := doCookie(t, s, "GET", "/api/workspace/github", "", adminSID, ""); rec.Code != http.StatusOK {
		t.Errorf("admin reads: %d", rec.Code)
	}
	if rec := doCookie(t, s, "POST", "/api/workspace/github/connect", "", adminSID, adminCSRF); rec.Code != http.StatusOK {
		t.Errorf("admin connects: %d", rec.Code)
	}
	if rec := doCookie(t, s, "GET", "/api/workspace/github", "", memberSID, ""); rec.Code != http.StatusForbidden {
		t.Errorf("member reads the connections: %d", rec.Code)
	}
	if rec := doCookie(t, s, "POST", "/api/workspace/github/connect", "", memberSID, memberCSRF); rec.Code != http.StatusForbidden {
		t.Errorf("member connects: %d", rec.Code)
	}
	if rec := doCookie(t, s, "GET", "/api/workspace/github/repositories", "", memberSID, ""); rec.Code != http.StatusOK {
		t.Errorf("a member, who makes projects, lists repositories: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "GET", "/api/workspace/github/repositories", "", strangerSID, ""); rec.Code != http.StatusForbidden {
		t.Errorf("stranger lists: %d", rec.Code)
	}
	if rec := doCookie(t, s, "GET", "/api/workspace/github/repositories/acme/shop/branches", "", strangerSID, ""); rec.Code != http.StatusForbidden {
		t.Errorf("stranger lists branches: %d", rec.Code)
	}
	if err := s.store.AddGitHubInstallation(ctx, store.DefaultWorkspace, store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "ana"}); err != nil {
		t.Fatal(err)
	}
	if rec := doCookie(t, s, "DELETE", "/api/workspace/github/installations/42", "", memberSID, memberCSRF); rec.Code != http.StatusForbidden {
		t.Errorf("member disconnects: %d", rec.Code)
	}
	if list, _ := s.store.GitHubInstallations(ctx, store.DefaultWorkspace); len(list) != 1 {
		t.Errorf("a member's disconnect went through: %+v", list)
	}
}

// chose is GitHub sending the browser back from the authorize step: a
// code and the state, no installation.
func chose(t *testing.T, s *Server, code, state string) *httptest.ResponseRecorder {
	t.Helper()
	q := url.Values{"code": {code}, "state": {state}}
	return do(t, s, "GET", "/api/github/installed?"+q.Encode(), "", false)
}

// pageLinks are a page's links, label to address.
func pageLinks(t *testing.T, body string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`<a href="([^"]*)"[^>]*>([^<]*)</a>`).FindAllStringSubmatch(body, -1) {
		out[html.UnescapeString(m[2])] = html.UnescapeString(m[1])
	}
	return out
}

func TestGitHubConnectAlsoGivesTheAuthorizeURL(t *testing.T) {
	s, _, fake := githubServer(t)
	rec := do(t, s, "POST", "/api/workspace/github/connect", "", true)
	var v GitHubConnectView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	install, _ := url.Parse(v.InstallURL)
	u, err := url.Parse(v.AuthorizeURL)
	if err != nil || u.Scheme+"://"+u.Host != fake.URL || u.Path != "/login/oauth/authorize" {
		t.Fatalf("authorize URL %q", v.AuthorizeURL)
	}
	q := u.Query()
	// No redirect_uri: GitHub uses the App's first callback URL, where the
	// install path lands too, whatever the dashboard's URL is now.
	if q.Get("client_id") != githubtest.ClientID || q.Get("state") == "" || q.Get("state") != install.Query().Get("state") ||
		q.Has("redirect_uri") {
		t.Errorf("authorize URL %q (install %q)", v.AuthorizeURL, v.InstallURL)
	}
}

func TestGitHubAuthorizeListsTheInstallationsThePersonOwns(t *testing.T) {
	s, _, fake := githubServer(t)
	ctx := context.Background()
	fake.SetUserInstallations(
		githubtest.Installation{ID: 42, Login: "Acme", Type: "Organization"},
		githubtest.Installation{ID: 7, Login: "ana", Type: "User"},
		githubtest.Installation{ID: 8, Login: "bob", Type: "User"},
		githubtest.Installation{ID: 9, Login: "Other", Type: "Organization"},
		githubtest.Installation{ID: 10, Login: "Beta", Type: "Organization"},
		githubtest.Installation{ID: 11, Login: "Mine", Type: "Organization"},
	)
	fake.SetOrgRole("Other", "member", "active")
	fake.SetOrgRole("Beta", "admin", "active")
	fake.SetOrgRole("Mine", "admin", "active")
	if _, err := s.store.CreateWorkspace(ctx, store.Workspace{Slug: "beta", Name: "Beta"}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.AddGitHubInstallation(ctx, "beta", store.GitHubInstallation{ID: 10, AccountLogin: "Beta", AccountType: "Organization", ConnectedBy: "bob@beta.test"}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.AddGitHubInstallation(ctx, store.DefaultWorkspace, store.GitHubInstallation{ID: 11, AccountLogin: "Mine", AccountType: "Organization", ConnectedBy: "ana"}); err != nil {
		t.Fatal(err)
	}
	heard := githubAudits(t)
	state := connectState(t, s)
	started, _ := readGitHubState(githubtest.ClientSecret, state, time.Now())
	rec := chose(t, s, githubtest.Code, state)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Connect an installation") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %s", rec.Code, body)
	}
	for _, says := range []string{"Mine (organisation): connected."} {
		if !strings.Contains(body, says) {
			t.Errorf("does not say %q: %s", says, body)
		}
	}
	for _, not := range []string{"bob", "Other", githubtest.UserToken, githubtest.ClientSecret} {
		if strings.Contains(body, not) {
			t.Errorf("says %q: %s", not, body)
		}
	}
	links := pageLinks(t, body)
	finishes := map[int64]string{}
	for label, href := range links {
		u, _ := url.Parse(href)
		if u.Path != "/api/workspace/github/finish" {
			continue
		}
		tk, err := readGitHubTicket(githubtest.ClientSecret, u.Query().Get("ticket"), time.Now())
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if tk.Workspace != store.DefaultWorkspace || tk.Actor != started.Actor || tk.Provider != started.Provider || tk.Subject != started.Subject ||
			tk.GitHubLogin != githubtest.UserLogin || tk.Exp > time.Now().Add(githubTicketTTL).Unix() {
			t.Errorf("%s: ticket %+v", label, tk)
		}
		finishes[tk.Installation] = u.RequestURI()
		if want := map[int64]string{42: "Connect Acme (organisation)", 7: "Connect ana (personal account)", 10: "Connect Beta (organisation)"}[tk.Installation]; label != want {
			t.Errorf("installation %d labelled %q", tk.Installation, label)
		}
	}
	// Beta's too: another workspace has it, and this one may as well.
	if len(finishes) != 3 || finishes[42] == "" || finishes[7] == "" || finishes[10] == "" {
		t.Fatalf("connectable %v; links %v", finishes, links)
	}
	install, err := url.Parse(links["Install the App on another account"])
	if err != nil || !strings.HasSuffix(install.Path, "/apps/"+githubtest.Slug+"/installations/new") || install.Query().Get("state") == state {
		t.Errorf("install link %v", links)
	} else if fresh, err := readGitHubState(githubtest.ClientSecret, install.Query().Get("state"), time.Now()); err != nil || fresh.Workspace != store.DefaultWorkspace || fresh.Subject != started.Subject ||
		fresh.Nonce == started.Nonce || fresh.Exp != started.Exp {
		// A new nonce, the original's time: a state is never chained past it.
		t.Errorf("install link's state %+v %v", fresh, err)
	}
	if list, _ := s.store.GitHubInstallations(ctx, store.DefaultWorkspace); len(list) != 1 || len(*heard) != 0 {
		t.Fatalf("the page alone connects nothing: %+v %+v", list, *heard)
	}
	// Each finishes on its own, once.
	for _, id := range []int64{42, 7, 10} {
		if rec := do(t, s, "GET", finishes[id], "", true); rec.Code != http.StatusFound || !strings.HasSuffix(rec.Header().Get("Location"), "/workspace/git") {
			t.Errorf("finish %d: %d %s", id, rec.Code, rec.Body.String())
		}
		if got := githubConnection(t, s, id, store.DefaultWorkspace); got == nil {
			t.Errorf("stored %d: %+v", id, got)
		}
	}
	if rec := do(t, s, "GET", finishes[42], "", true); rec.Code != http.StatusBadRequest {
		t.Errorf("a ticket spent twice: %d %s", rec.Code, rec.Body.String())
	}
	if got := githubConnection(t, s, 10, "beta"); got == nil {
		t.Errorf("beta keeps it: %+v", got)
	}
}

func TestGitHubAuthorizeStateIsUsedOnce(t *testing.T) {
	s, _, _ := githubServer(t)
	state := connectState(t, s)
	if rec := chose(t, s, githubtest.Code, state); rec.Code != http.StatusOK {
		t.Fatalf("first: %d %s", rec.Code, rec.Body.String())
	}
	if rec := chose(t, s, githubtest.Code, state); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "This link was used already") ||
		!strings.Contains(rec.Body.String(), "Connect GitHub again from Workspace settings → Git.") {
		t.Errorf("replay: %d %s", rec.Code, rec.Body.String())
	}
	if rec := chose(t, s, githubtest.Code, "forged"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "This link has expired") {
		t.Errorf("forged: %d %s", rec.Code, rec.Body.String())
	}
	if rec := chose(t, s, "stale-code", connectState(t, s)); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Try again") {
		t.Errorf("a code GitHub refuses: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubAuthorizeWithNothingOwnedSaysToInstallFirst(t *testing.T) {
	s, _, fake := githubServer(t)
	fake.SetUserInstallations(githubtest.Installation{ID: 8, Login: "bob", Type: "User"})
	rec := chose(t, s, githubtest.Code, connectState(t, s))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "You don&#39;t own an installation of this App yet: install it first") || strings.Contains(body, "bob") {
		t.Fatalf("%d %s", rec.Code, body)
	}
	links := pageLinks(t, body)
	install, err := url.Parse(links["Install the App on another account"])
	if err != nil || !strings.HasSuffix(install.Path, "/installations/new") || install.Query().Get("state") == "" {
		t.Errorf("links %v", links)
	}
	for _, href := range links {
		if strings.Contains(href, "/finish") {
			t.Errorf("a finish link: %v", links)
		}
	}
}

func TestGitHubAuthorizeSaysWhyAnOrgCannotBeChecked(t *testing.T) {
	s, _, fake := githubServer(t)
	fake.SetNoMembersPermission(true)
	rec := chose(t, s, githubtest.Code, connectState(t, s))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "an owner of the App must accept its new Members permission on GitHub") ||
		strings.Contains(rec.Body.String(), "/finish") {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubAuthorizeEscapesTheAccountName(t *testing.T) {
	s, _, fake := githubServer(t)
	name := `<b>x&"y</b>`
	fake.SetUserInstallations(githubtest.Installation{ID: 7, Login: name, Type: "User"})
	fake.SetUserLogin(name)
	rec := chose(t, s, githubtest.Code, connectState(t, s))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || strings.Contains(body, name) || !strings.Contains(body, "Connect &lt;b&gt;x&amp;&#34;y&lt;/b&gt; (personal account)") {
		t.Errorf("%d %s", rec.Code, body)
	}
}

func TestGitHubAuthorizeInstallLinkKeepsTheOriginalTime(t *testing.T) {
	s, _, _ := githubServer(t)
	started, _ := readGitHubState(githubtest.ClientSecret, connectState(t, s), time.Now())
	started.Exp, started.Nonce = time.Now().Add(time.Minute).Unix(), "short"
	rec := chose(t, s, githubtest.Code, signGitHubState(githubtest.ClientSecret, started))
	install, err := url.Parse(pageLinks(t, rec.Body.String())["Install the App on another account"])
	if rec.Code != http.StatusOK || err != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	fresh, err := readGitHubState(githubtest.ClientSecret, install.Query().Get("state"), time.Now())
	if err != nil || fresh.Exp != started.Exp || fresh.Nonce == started.Nonce {
		t.Errorf("the install link's state %+v %v: a state is never chained past %d", fresh, err, started.Exp)
	}
}
