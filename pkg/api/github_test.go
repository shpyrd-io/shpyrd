package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	if _, err := s.store.AddGitHubInstallation(ctx, "beta", store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "bob"}); err != nil {
		t.Fatal(err)
	}
	// The generic dashboard: the redirect says nothing of which workspace
	// holds the installation.
	rec := do(t, s, "GET", "/api/github/installed?installation_id=42&setup_action=update", "", false)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != s.dashboardURLOf(nil) {
		t.Errorf("%d %q, want %q", rec.Code, rec.Header().Get("Location"), s.dashboardURLOf(nil))
	}
}

func TestGitHubConnectRefusesAnInstallationAnotherWorkspaceHas(t *testing.T) {
	s, _, _ := githubServer(t)
	ctx := context.Background()
	if _, err := s.store.CreateWorkspace(ctx, store.Workspace{Slug: "beta", Name: "Beta"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.AddGitHubInstallation(ctx, "beta", store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "bob@beta.test"}); err != nil {
		t.Fatal(err)
	}
	heard := githubAudits(t)
	rec := installed(t, s, "42", githubtest.Code, connectState(t, s))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Acme is connected to workspace beta: disconnect it there first.") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if got, _ := s.store.GitHubInstallation(ctx, 42); got == nil || got.Workspace != "beta" {
		t.Errorf("beta keeps it: %+v", got)
	}
	if len(*heard) != 0 {
		t.Errorf("audit %+v", *heard)
	}
	// Taken by another workspace between the callback and the finish: the
	// finish refuses too.
	if err := s.store.DeleteGitHubInstallation(ctx, "beta", 42); err != nil {
		t.Fatal(err)
	}
	finish := ticketOf(t, installed(t, s, "42", githubtest.Code, connectState(t, s)))
	if _, err := s.store.AddGitHubInstallation(ctx, "beta", store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "bob@beta.test"}); err != nil {
		t.Fatal(err)
	}
	if rec := do(t, s, "GET", finish, "", true); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "connected to workspace beta") {
		t.Errorf("finish: %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := s.store.GitHubInstallation(ctx, 42); got == nil || got.Workspace != "beta" {
		t.Errorf("beta keeps it: %+v", got)
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
	if got, _ := s.store.GitHubInstallation(context.Background(), 7); got == nil || got.AccountLogin != "Ana" || got.AccountType != "User" {
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
	person := func(email string) (string, string) {
		p, err := s.store.TouchIdentity(ctx, store.DefaultWorkspace, store.Identity{Email: email, Provider: "local"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.store.PutMembership(ctx, store.DefaultWorkspace, p.ID, store.WorkspaceRoleAdmin); err != nil {
			t.Fatal(err)
		}
		return signIn(t, s, ext.Identity{Subject: p.ID, Email: email, Provider: "local"})
	}
	anaSID, anaCSRF := person("ana@example.test")
	bobSID, _ := person("bob@example.test")
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
	if got, _ := s.store.GitHubInstallation(ctx, 42); got == nil || got.ConnectedBy != "ana@example.test" {
		t.Errorf("stored %+v", got)
	}
	refused("replayed", doCookie(t, s, "GET", finish, "", anaSID, ""))

	key := githubtest.ClientSecret
	tk := func(ws, actor string, exp time.Time, nonce string) string {
		return "/api/workspace/github/finish?ticket=" + url.QueryEscape(signGitHubTicket(key, githubTicket{
			Workspace: ws, Actor: actor, Installation: 42, Account: "Acme", AccountType: "Organization", GitHubLogin: "ana", Exp: exp.Unix(), Nonce: nonce}))
	}
	refused("expired", doCookie(t, s, "GET", tk("default", "ana@example.test", time.Now().Add(-time.Second), "t1"), "", anaSID, ""))
	refused("another workspace's", doCookie(t, s, "GET", tk("beta", "ana@example.test", time.Now().Add(time.Minute), "t2"), "", anaSID, ""))
	state := signGitHubState(key, githubState{Workspace: "default", Actor: "ana@example.test", Exp: time.Now().Add(time.Minute).Unix(), Nonce: "t3"})
	refused("a state as a ticket", doCookie(t, s, "GET", "/api/workspace/github/finish?ticket="+url.QueryEscape(state), "", anaSID, ""))
	if _, err := readGitHubTicket(key, state, time.Now()); err == nil {
		t.Error("a state reads as a ticket")
	}
	if _, err := readGitHubState(key, signGitHubTicket(key, githubTicket{Workspace: "default", Actor: "x", Exp: time.Now().Add(time.Minute).Unix(), Nonce: "t4"}), time.Now()); err == nil {
		t.Error("a ticket reads as a state")
	}
	// A fresh, valid one still works: the refusals spent nothing.
	if rec := doCookie(t, s, "GET", tk("default", "ana@example.test", time.Now().Add(time.Minute), "t5"), "", anaSID, ""); rec.Code != http.StatusFound {
		t.Errorf("valid: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubFinishSendsABrowserWithoutASessionToSignIn(t *testing.T) {
	s, _, _ := githubServer(t)
	finish := ticketOf(t, installed(t, s, "42", githubtest.Code, connectState(t, s)))
	rec := do(t, s, "GET", finish, "", false)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/?next="+url.QueryEscape(finish) {
		t.Errorf("%d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestGitHubRepositoriesAndBranchesForThePicker(t *testing.T) {
	s, _, fake := githubServer(t)
	ctx := context.Background()
	if _, err := s.store.AddGitHubInstallation(ctx, store.DefaultWorkspace, store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "ana"}); err != nil {
		t.Fatal(err)
	}
	rec := do(t, s, "GET", "/api/workspace/github/repositories", "", true)
	var repos []GitHubRepositoryView
	_ = json.Unmarshal(rec.Body.Bytes(), &repos)
	if rec.Code != http.StatusOK || len(repos) != 2 || repos[0].FullName != "Acme/shop" || !repos[0].Private || repos[0].DefaultBranch != "main" || repos[0].Installation != 42 || repos[1].FullName != "Acme/site" {
		t.Fatalf("repositories: %d %s", rec.Code, rec.Body.String())
	}
	// Kept a minute: a second listing does not ask GitHub again.
	calls := len(fake.Calls())
	do(t, s, "GET", "/api/workspace/github/repositories", "", true)
	if len(fake.Calls()) != calls {
		t.Errorf("listed again: %v", fake.Calls()[calls:])
	}
	rec = do(t, s, "GET", "/api/workspace/github/repositories/acme/shop/branches", "", true)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `["dev","main"]` {
		t.Errorf("branches: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "GET", "/api/workspace/github/repositories/acme/vault/branches", "", true); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "does not include vault") {
		t.Errorf("not granted: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "GET", "/api/workspace/github/repositories/other/shop/branches", "", true); rec.Code != http.StatusNotFound {
		t.Errorf("an owner no connection reaches: %d", rec.Code)
	}
	fake.SetDown(true)
	s.forgetGitHubRepos(store.DefaultWorkspace)
	if rec := do(t, s, "GET", "/api/workspace/github/repositories", "", true); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Acme") {
		t.Errorf("GitHub down: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubDisconnect(t *testing.T) {
	s, _, _ := githubServer(t)
	ctx := context.Background()
	if _, err := s.store.AddGitHubInstallation(ctx, store.DefaultWorkspace, store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "ana"}); err != nil {
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
}
