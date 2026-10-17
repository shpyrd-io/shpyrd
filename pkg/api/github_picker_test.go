package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

func repoNames(list []GitHubRepositoryView) string {
	var out []string
	for _, r := range list {
		out = append(out, r.FullName)
	}
	return strings.Join(out, ",")
}

func TestThePickerListsOnlyWhatThePersonCanRead(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	fake.AddUser("bob-code", githubtest.User{ID: 1002, Login: "bob", Installations: []int64{42}, Repos: []string{"Acme/site"}})
	fake.AddUser("cleo-code", githubtest.User{ID: 1003, Login: "cleo", Installations: []int64{}})
	anaSID, anaCSRF, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	bobSID, bobCSRF, _ := signInPerson(t, s, "bob@example.test", store.WorkspaceRoleMember)
	cleoSID, cleoCSRF, _ := signInPerson(t, s, "cleo@example.test", store.WorkspaceRoleMember)
	for _, path := range []string{"/api/workspace/github/accounts", "/api/workspace/github/repositories?installation=42", "/api/workspace/github/repositories/acme/shop/branches"} {
		if rec := doCookie(t, s, "GET", path, "", anaSID, ""); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"github_link_required"`) {
			t.Errorf("not linked, %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	linkAs(t, s, anaSID, anaCSRF, githubtest.Code)
	linkAs(t, s, bobSID, bobCSRF, "bob-code")
	linkAs(t, s, cleoSID, cleoCSRF, "cleo-code")

	accounts := func(sid string) GitHubAccountsView {
		t.Helper()
		rec := doCookie(t, s, "GET", "/api/workspace/github/accounts", "", sid, "")
		var v GitHubAccountsView
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &v) != nil {
			t.Fatalf("accounts: %d %s", rec.Code, rec.Body.String())
		}
		return v
	}
	if a := accounts(anaSID); a.Connected != 1 || len(a.Accounts) != 1 || a.Accounts[0].Installation != 42 || a.Accounts[0].Account != "Acme" ||
		a.Accounts[0].Type != "Organization" || a.Accounts[0].ManageURL != fake.URL+"/organizations/Acme/settings/installations/42" {
		t.Errorf("ana's accounts %+v", a)
	}
	if a := accounts(cleoSID); a.Connected != 1 || len(a.Accounts) != 0 {
		t.Errorf("cleo sees an account: %+v", a)
	}

	repos := func(sid, query string) []GitHubRepositoryView {
		t.Helper()
		rec := doCookie(t, s, "GET", "/api/workspace/github/repositories"+query, "", sid, "")
		var v []GitHubRepositoryView
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &v) != nil {
			t.Fatalf("repositories%s: %d %s", query, rec.Code, rec.Body.String())
		}
		return v
	}
	if got := repoNames(repos(anaSID, "?installation=42")); got != "Acme/shop,Acme/site" {
		t.Errorf("ana: %s", got)
	}
	for _, q := range []string{"?installation=42", ""} {
		if got := repoNames(repos(bobSID, q)); got != "Acme/site" {
			t.Errorf("bob%s: %s", q, got)
		}
	}
	if got := repos(cleoSID, ""); len(got) != 0 {
		t.Errorf("cleo: %+v", got)
	}
	// Kept a minute, per person: bob asks again, GitHub is not asked.
	calls := len(fake.Calls())
	repos(bobSID, "?installation=42")
	if len(fake.Calls()) != calls {
		t.Errorf("listed again: %v", fake.Calls()[calls:])
	}

	branches := func(sid, path string) (int, string) {
		rec := doCookie(t, s, "GET", "/api/workspace/github/repositories/"+path+"/branches", "", sid, "")
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}
	if code, body := branches(anaSID, "acme/shop"); code != http.StatusOK || body != `["dev","main"]` {
		t.Errorf("ana's branches: %d %s", code, body)
	}
	if code, body := branches(bobSID, "acme/shop"); code != http.StatusNotFound || !strings.Contains(body, "You can't read acme/shop on GitHub") {
		t.Errorf("bob's branches of shop: %d %s", code, body)
	}
	if code, _ := branches(anaSID, "other/shop"); code != http.StatusNotFound {
		t.Errorf("an owner no connection reaches: %d", code)
	}
	if rec := doCookie(t, s, "GET", "/api/workspace/github/repositories?installation=7", "", anaSID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("another workspace's installation: %d", rec.Code)
	}
	if rec := doCookie(t, s, "GET", "/api/workspace/github/repositories?installation=abc", "", anaSID, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("not a number: %d", rec.Code)
	}
	fake.SetDown(true)
	s.forgetPersonRepos("local", "")
	s.forgetGitHubRepos(store.DefaultWorkspace)
	if rec := doCookie(t, s, "GET", "/api/workspace/github/repositories", "", anaSID, ""); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "GitHub did not answer. Try again in a minute") {
		t.Errorf("GitHub down: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAWorkspaceWithoutGitHubAsksNoLink(t *testing.T) {
	s, _, _ := githubServer(t)
	sid, _, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	rec := doCookie(t, s, "GET", "/api/workspace/github/accounts", "", sid, "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"accounts":[],"connected":0}` {
		t.Errorf("accounts: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "GET", "/api/workspace/github/repositories", "", sid, ""); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `[]` {
		t.Errorf("repositories: %d %s", rec.Code, rec.Body.String())
	}
}

func TestThePickerSkipsAnInstallationThatIsGone(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	if err := s.store.AddGitHubInstallation(t.Context(), store.DefaultWorkspace, store.GitHubInstallation{ID: 7, AccountLogin: "Gone", AccountType: "Organization", ConnectedBy: "ana"}); err != nil {
		t.Fatal(err)
	}
	fake.Uninstall(7)
	sid, csrf, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	rec := doCookie(t, s, "GET", "/api/workspace/github/repositories", "", sid, "")
	var v []GitHubRepositoryView
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &v) != nil || repoNames(v) != "Acme/shop,Acme/site" {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestThePickerAsksToLinkAgainWhenGitHubForgotTheLink(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	sid, csrf, sub := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	s.githubUsers.forget("local", sub)
	fake.ForgetRefreshTokens()
	if rec := doCookie(t, s, "GET", "/api/workspace/github/accounts", "", sid, ""); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "github_link_required") {
		t.Errorf("accounts: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "GET", "/api/me/github", "", sid, ""); rec.Code != http.StatusNotFound {
		t.Errorf("me: %d", rec.Code)
	}
}

func TestThePickerRefreshesAnExpiredToken(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	sid, csrf, sub := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	fake.ExpireUserTokens()
	s.githubUsers.forget("local", sub)
	if rec := doCookie(t, s, "GET", "/api/workspace/github/accounts", "", sid, ""); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if n := exchanges(fake); n != 2 {
		t.Errorf("%d exchanges: the code, then one refresh", n)
	}
}

func TestThePickerAnswersTheFirstPartOfAListGitHubCutShort(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	var repos []githubtest.Repo
	for i := range 10001 {
		repos = append(repos, githubtest.Repo{FullName: fmt.Sprintf("Acme/r%05d", i), DefaultBranch: "main"})
	}
	fake.SetRepos(42, repos...)
	var branches []string
	for i := range 1001 {
		branches = append(branches, fmt.Sprintf("b%04d", i))
	}
	fake.SetBranches("Acme/r00000", branches...)
	sid, csrf, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)

	for i := range 2 { // the second answer is the cached one: it says so too
		rec := doCookie(t, s, "GET", "/api/workspace/github/repositories?installation=42", "", sid, "")
		var v []GitHubRepositoryView
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &v) != nil || len(v) != 10000 || rec.Header().Get(githubTruncatedHeader) != "true" {
			t.Errorf("repositories, ask %d: %d, %d listed, truncated %q", i, rec.Code, len(v), rec.Header().Get(githubTruncatedHeader))
		}
	}
	rec := doCookie(t, s, "GET", "/api/workspace/github/repositories/acme/r00000/branches", "", sid, "")
	var names []string
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &names) != nil || len(names) != 1000 || rec.Header().Get(githubTruncatedHeader) != "true" {
		t.Errorf("branches: %d, %d listed, truncated %q", rec.Code, len(names), rec.Header().Get(githubTruncatedHeader))
	}
}

func TestAGitHubRefusalOfThePersonsTokenAsksToLinkAgain(t *testing.T) {
	s, _, _ := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	sid, csrf, sub := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	// GitHub stops taking the access token shpyrd holds (revoked on GitHub).
	cl, err := s.githubApp.Client(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	held, ok := s.githubUsers.entry(personKey("local", sub))
	if !ok {
		t.Fatal("no token held")
	}
	if err := cl.RevokeUserToken(t.Context(), held.access); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/workspace/github/accounts", "/api/workspace/github/repositories"} {
		if _, ok := s.githubUsers.entry(personKey("local", sub)); !ok {
			if rec := doCookie(t, s, "GET", "/api/workspace/github/accounts", "", sid, ""); rec.Code != http.StatusOK {
				t.Fatalf("renewal: %d", rec.Code)
			}
			if err := cl.RevokeUserToken(t.Context(), mustHeld(t, s, sub)); err != nil {
				t.Fatal(err)
			}
		}
		rec := doCookie(t, s, "GET", path, "", sid, "")
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"github_link_required"`) {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	// Dropped from memory, not unlinked: the next use renews.
	if rec := doCookie(t, s, "GET", "/api/workspace/github/accounts", "", sid, ""); rec.Code != http.StatusOK {
		t.Errorf("after the renewal: %d %s", rec.Code, rec.Body.String())
	}
}

func mustHeld(t *testing.T, s *Server, sub string) string {
	t.Helper()
	e, ok := s.githubUsers.entry(personKey("local", sub))
	if !ok {
		t.Fatal("no token held")
	}
	return e.access
}

func TestThePickerDoesNotListThroughTheAdminToken(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	calls := len(fake.Calls())
	for _, path := range []string{"/api/workspace/github/accounts", "/api/workspace/github/repositories", "/api/workspace/github/repositories/acme/shop/branches"} {
		if rec := do(t, s, "GET", path, "", true); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"github_link_required"`) {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	if len(fake.Calls()) != calls {
		t.Errorf("GitHub was asked: %v", fake.Calls()[calls:])
	}
}

func TestOneInstallationThatRefusesThePersonDoesNotFailTheList(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	if err := s.store.AddGitHubInstallation(t.Context(), store.DefaultWorkspace, store.GitHubInstallation{ID: 43, AccountLogin: "Beta", AccountType: "Organization", ConnectedBy: "ana"}); err != nil {
		t.Fatal(err)
	}
	fake.SetUserInstallations(githubtest.Installation{ID: 42, Login: "Acme", Type: "Organization"}, githubtest.Installation{ID: 43, Login: "Beta", Type: "Organization"})
	fake.SetRepos(43, githubtest.Repo{FullName: "Beta/app", DefaultBranch: "main"})
	fake.ForbidInstallation("ana", 43) // the default person's login
	sid, csrf, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	rec := doCookie(t, s, "GET", "/api/workspace/github/repositories", "", sid, "")
	var v []GitHubRepositoryView
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &v) != nil || repoNames(v) != "Acme/shop,Acme/site" {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestAGitHubFailureAnswersOneSentenceWithNothingOfGitHub(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	sid, csrf, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	fake.SetDown(true)
	for _, path := range []string{"/api/workspace/github/accounts", "/api/workspace/github/repositories", "/api/workspace/github/repositories/acme/shop/branches"} {
		rec := doCookie(t, s, "GET", path, "", sid, "")
		body := rec.Body.String()
		if rec.Code != http.StatusBadGateway || !strings.Contains(body, "GitHub did not answer. Try again in a minute") ||
			strings.Contains(body, "Service unavailable") || strings.Contains(body, "42") || strings.Contains(body, "/user/") {
			t.Errorf("%s: %d %s", path, rec.Code, body)
		}
	}
}

func TestTheListingsCacheDropsWhatExpired(t *testing.T) {
	var c githubRepoCache
	t0 := time.Now()
	c.put("a", nil, false, t0)
	c.put("b", nil, false, t0.Add(githubReposTTL/2))
	c.put("c", nil, false, t0.Add(githubReposTTL+time.Second))
	if _, has := c.by["a"]; has || len(c.by) != 2 {
		t.Errorf("kept %d entries, a among them: %v", len(c.by), has)
	}
	c.put("d", nil, false, t0.Add(3*githubReposTTL))
	if len(c.by) != 1 {
		t.Errorf("kept %d entries, want only d", len(c.by))
	}
}

func TestAnUnlinkedPersonCannotProbeWhichInstallationsExist(t *testing.T) {
	s, _, _ := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	sid, _, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	for _, q := range []string{"?installation=42", "?installation=9999"} {
		rec := doCookie(t, s, "GET", "/api/workspace/github/repositories"+q, "", sid, "")
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"github_link_required"`) {
			t.Errorf("%s: %d %s", q, rec.Code, rec.Body.String())
		}
	}
}

func TestAGitHubRefusalOfTheTokenOnBranchesAsksToLinkAgain(t *testing.T) {
	s, _, _ := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	sid, csrf, sub := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	cl, err := s.githubApp.Client(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := cl.RevokeUserToken(t.Context(), mustHeld(t, s, sub)); err != nil {
		t.Fatal(err)
	}
	rec := doCookie(t, s, "GET", "/api/workspace/github/repositories/acme/shop/branches", "", sid, "")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"github_link_required"`) {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
	if _, held := s.githubUsers.entry(personKey("local", sub)); held {
		t.Error("the refused token is still held")
	}
}
