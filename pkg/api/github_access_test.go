package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

func createBody(slug, url string) string {
	return fmt.Sprintf(`{"name":%q,"slug":%q,"git":{"url":%q,"revision":"main"}}`, slug, slug, url)
}

func linkedBy(t *testing.T, s *Server, slug string) string {
	t.Helper()
	app, err := s.findApp(context.Background(), store.DefaultWorkspace, slug)
	if err != nil {
		t.Fatalf("%s: %v", slug, err)
	}
	return app.Annotations[shpyrdv1.AnnotationGitHubLinkedBy]
}

func TestCreatingAProjectFromAPrivateRepositoryNeedsSomeoneWhoCanReadIt(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	fake.AddUser("bob-code", githubtest.User{ID: 1002, Login: "bob", Installations: []int64{42}, Repos: []string{"Acme/site"}})
	anaSID, anaCSRF, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	bobSID, bobCSRF, _ := signInPerson(t, s, "bob@example.test", store.WorkspaceRoleMember)
	if rec := doCookie(t, s, "POST", "/api/projects", createBody("shop", "https://github.com/acme/shop"), anaSID, anaCSRF); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), `"code":"github_link_required"`) {
		t.Fatalf("no link: %d %s", rec.Code, rec.Body.String())
	}
	linkAs(t, s, anaSID, anaCSRF, githubtest.Code)
	linkAs(t, s, bobSID, bobCSRF, "bob-code")
	if rec := doCookie(t, s, "POST", "/api/projects", createBody("shop", "https://github.com/acme/shop"), bobSID, bobCSRF); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), "You can't read acme/shop on GitHub") {
		t.Errorf("bob: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "POST", "/api/projects", createBody("vault", "https://github.com/acme/vault"), anaSID, anaCSRF); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "does not include acme/vault") {
		t.Errorf("a repository outside the installation: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "POST", "/api/projects", createBody("shop", "git@github.com:Acme/shop.git"), anaSID, anaCSRF); rec.Code != http.StatusCreated {
		t.Fatalf("ana: %d %s", rec.Code, rec.Body.String())
	}
	if got := linkedBy(t, s, "shop"); got != githubtest.UserLogin {
		t.Errorf("linked by %q", got)
	}
	// Not GitHub's to check: another host, and an owner the workspace has
	// no connection on (a deploy key or token reads it, as before).
	for slug, url := range map[string]string{"gl": "https://gitlab.com/acme/shop", "other": "https://github.com/other/private"} {
		if rec := doCookie(t, s, "POST", "/api/projects", createBody(slug, url), bobSID, bobCSRF); rec.Code != http.StatusCreated {
			t.Errorf("%s: %d %s", url, rec.Code, rec.Body.String())
		}
	}
}

func TestAPublicRepositoryNeedsNoLink(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	sid, csrf, _ := signInPerson(t, s, "cleo@example.test", store.WorkspaceRoleMember)
	if rec := doCookie(t, s, "POST", "/api/projects", createBody("site", "https://github.com/acme/site"), sid, csrf); rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if got := linkedBy(t, s, "site"); got != "" {
		t.Errorf("linked by %q", got)
	}
	for _, call := range fake.Calls() {
		if strings.HasPrefix(call, "GET /user") || call == "POST /login/oauth/access_token" {
			t.Errorf("asked GitHub as the person: %s", call)
		}
	}
}

func TestAnAPITokenCreatesWithItsMakersLink(t *testing.T) {
	s, _, _ := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	anaSID, anaCSRF, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	bobSID, bobCSRF, _ := signInPerson(t, s, "bob@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, anaSID, anaCSRF, githubtest.Code)
	if rec := doBearer(t, s, "POST", "/api/projects", createBody("shop", "https://github.com/acme/shop"), personToken(t, s, anaSID, anaCSRF)); rec.Code != http.StatusCreated {
		t.Fatalf("ana's agent: %d %s", rec.Code, rec.Body.String())
	}
	if got := linkedBy(t, s, "shop"); got != githubtest.UserLogin {
		t.Errorf("linked by %q", got)
	}
	if rec := doBearer(t, s, "POST", "/api/projects", createBody("shop2", "https://github.com/acme/shop"), personToken(t, s, bobSID, bobCSRF)); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "github_link_required") {
		t.Errorf("bob's agent, bob unlinked: %d %s", rec.Code, rec.Body.String())
	}
	// The admin token is the operator: not checked.
	if rec := do(t, s, "POST", "/api/projects", createBody("shop3", "https://github.com/acme/shop"), true); rec.Code != http.StatusCreated {
		t.Errorf("the admin token: %d %s", rec.Code, rec.Body.String())
	}
}

func TestADeployChecksOnlyAChangedRepository(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	fake.AddUser("bob-code", githubtest.User{ID: 1002, Login: "bob", Installations: []int64{42}, Repos: []string{"Acme/site"}})
	anaSID, anaCSRF, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleAdmin)
	bobSID, bobCSRF, _ := signInPerson(t, s, "bob@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, anaSID, anaCSRF, githubtest.Code)
	linkAs(t, s, bobSID, bobCSRF, "bob-code")
	if rec := doCookie(t, s, "POST", "/api/projects", createBody("site", "https://github.com/acme/site"), bobSID, bobCSRF); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	deploy := func(sid, csrf, url string) *httptest.ResponseRecorder {
		return doCookie(t, s, "POST", "/api/projects/site/deploy", fmt.Sprintf(`{"git":{"url":%q,"revision":"main"}}`, url), sid, csrf)
	}
	if rec := deploy(bobSID, bobCSRF, "https://github.com/acme/shop"); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "You can't read acme/shop on GitHub") {
		t.Errorf("bob moves it to shop: %d %s", rec.Code, rec.Body.String())
	}
	if rec := deploy(anaSID, anaCSRF, "https://github.com/acme/shop"); rec.Code != http.StatusAccepted {
		t.Fatalf("ana moves it to shop: %d %s", rec.Code, rec.Body.String())
	}
	if got := linkedBy(t, s, "site"); got != githubtest.UserLogin {
		t.Errorf("linked by %q", got)
	}
	// A redeploy of the same repository is not checked: bob cannot read
	// shop, and redeploys it.
	if rec := deploy(bobSID, bobCSRF, "https://github.com/ACME/shop.git"); rec.Code != http.StatusAccepted {
		t.Errorf("bob redeploys: %d %s", rec.Code, rec.Body.String())
	}
	if got := linkedBy(t, s, "site"); got != githubtest.UserLogin {
		t.Errorf("a redeploy changed who linked it: %q", got)
	}
	// Back to the public site, no link needed: the record goes.
	if rec := deploy(bobSID, bobCSRF, "https://github.com/acme/site"); rec.Code != http.StatusAccepted {
		t.Errorf("bob back to site: %d %s", rec.Code, rec.Body.String())
	}
	if got := linkedBy(t, s, "site"); got != "" {
		t.Errorf("linked by %q after a public repository", got)
	}
}

func TestSameRepositoryIgnoresFormAndCase(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"https://github.com/acme/shop", "git@github.com:ACME/Shop.git", true},
		{"https://github.com/acme/shop", "ssh://git@github.com/acme/shop.git", true},
		{"https://github.com/acme/shop", "https://github.com/acme/site", false},
		{"https://github.com/acme/shop", "https://gitlab.com/acme/shop", false},
	} {
		if got := sameRepository(c.a, c.b); got != c.same {
			t.Errorf("%s ~ %s: %v", c.a, c.b, got)
		}
	}
}

// The project's maker leaving does not stop its deploys (P13): a redeploy
// of the same repository is not checked, whoever makes it, and the
// watcher's pushes never pass through the check.
func TestTheMakerLeavingDoesNotStopDeploys(t *testing.T) {
	s, _, _ := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	anaSID, anaCSRF, ana := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	bobSID, bobCSRF, _ := signInPerson(t, s, "bob@example.test", store.WorkspaceRoleAdmin)
	linkAs(t, s, anaSID, anaCSRF, githubtest.Code)
	if rec := doCookie(t, s, "POST", "/api/projects", createBody("shop", "https://github.com/acme/shop"), anaSID, anaCSRF); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	// Ana unlinks, then leaves the workspace.
	if rec := doCookie(t, s, "DELETE", "/api/me/github", "", anaSID, anaCSRF); rec.Code != http.StatusNoContent {
		t.Fatalf("unlink: %d %s", rec.Code, rec.Body.String())
	}
	if err := s.store.DeleteMembership(context.Background(), store.DefaultWorkspace, ana); err != nil {
		t.Fatal(err)
	}
	s.authz.Invalidate()
	deploy := `{"git":{"url":"https://github.com/acme/shop","revision":"main"}}`
	// Bob, a workspace admin with no link, redeploys it.
	if rec := doCookie(t, s, "POST", "/api/projects/shop/deploy", deploy, bobSID, bobCSRF); rec.Code != http.StatusAccepted {
		t.Errorf("bob redeploys: %d %s", rec.Code, rec.Body.String())
	}
	// So does the operator.
	if rec := do(t, s, "POST", "/api/projects/shop/deploy", deploy, true); rec.Code != http.StatusAccepted {
		t.Errorf("the admin token redeploys: %d %s", rec.Code, rec.Body.String())
	}
	if got := linkedBy(t, s, "shop"); got != githubtest.UserLogin {
		t.Errorf("linked by %q after ana left", got)
	}
}

// A GitHub that does not answer is one sentence: its own words stay in
// the log.
func TestTheCheckAnswersAGitHubFailureInOneSentence(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	sid, csrf, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	fake.SetRateLimited(time.Minute)
	rec := doCookie(t, s, "POST", "/api/projects", createBody("shop", "https://github.com/acme/shop"), sid, csrf)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "GitHub did not answer. Try again in a minute") ||
		strings.Contains(rec.Body.String(), "GitHub answered") {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}
