package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/projectarchive"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
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
	// Acme/vault is in another installation of the App, one ana sees on
	// GitHub and the workspace has not connected.
	fake.SetRepos(43, githubtest.Repo{FullName: "Acme/vault", Private: true, DefaultBranch: "main"})
	fake.SetUserInstallations(githubtest.Installation{ID: 42, Login: "Acme", Type: "Organization"}, githubtest.Installation{ID: 43, Login: "Acme", Type: "Organization"})
	anaSID, anaCSRF, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	bobSID, bobCSRF, _ := signInPerson(t, s, "bob@example.test", store.WorkspaceRoleMember)
	if rec := doCookie(t, s, "POST", "/api/projects", createBody("shop", "https://github.com/acme/shop"), anaSID, anaCSRF); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), `"code":"github_link_required"`) {
		t.Fatalf("no link: %d %s", rec.Code, rec.Body.String())
	}
	// Unlinked, nothing is said of what the installation includes.
	if rec := doCookie(t, s, "POST", "/api/projects", createBody("vault", "https://github.com/acme/vault"), anaSID, anaCSRF); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), `"code":"github_link_required"`) {
		t.Errorf("vault, no link: %d %s", rec.Code, rec.Body.String())
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
	if rec := doCookie(t, s, "POST", "/api/projects", createBody("vault", "https://github.com/acme/vault"), bobSID, bobCSRF); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), "You can't read acme/vault on GitHub") {
		t.Errorf("vault, bob: %d %s", rec.Code, rec.Body.String())
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
	// So do bob's agent and the operator.
	// An admin's token carries no workspace role (tokenCaller): it is made
	// a deployer of the project, by its grant key.
	shop, err := s.findApp(context.Background(), store.DefaultWorkspace, "shop")
	if err != nil {
		t.Fatal(err)
	}
	made := doCookie(t, s, "POST", "/api/tokens", fmt.Sprintf(`{"name":"agent","projectRoles":{%q:"developer"}}`, shop.Name), bobSID, bobCSRF)
	var agent TokenCreateView
	if err := json.Unmarshal(made.Body.Bytes(), &agent); err != nil || agent.Token == "" {
		t.Fatalf("token: %d %s", made.Code, made.Body.String())
	}
	if rec := doBearer(t, s, "POST", "/api/projects/shop/deploy", deploy, agent.Token); rec.Code != http.StatusAccepted {
		t.Errorf("bob's agent redeploys: %d %s", rec.Code, rec.Body.String())
	}
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

// A deploy whose project moved to another repository between the read
// and the write is refused: the check was made for the old one.
func TestADeployRacingARepositoryChangeIsRefused(t *testing.T) {
	s, _, _ := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	bobSID, bobCSRF, _ := signInPerson(t, s, "bob@example.test", store.WorkspaceRoleAdmin)
	if rec := do(t, s, "POST", "/api/projects", createBody("shop", "https://github.com/acme/shop"), true); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	cur, err := s.findApp(context.Background(), store.DefaultWorkspace, "shop")
	if err != nil {
		t.Fatal(err)
	}
	key := types.NamespacedName{Namespace: cur.Namespace, Name: cur.Name}
	armed := false
	s.apps = interceptor.NewClient(s.apps.(client.WithWatch), interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, k client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, isApp := obj.(*shpyrdv1.App); isApp && armed && k == key {
				// Someone moves the project meanwhile.
				armed = false
				moved := &shpyrdv1.App{}
				if err := c.Get(ctx, k, moved); err != nil {
					return err
				}
				moved.Spec.Source.Git.URL = "https://github.com/acme/elsewhere"
				if err := c.Update(ctx, moved); err != nil {
					return err
				}
			}
			return c.Get(ctx, k, obj, opts...)
		},
	})
	armed = true
	rec := doCookie(t, s, "POST", "/api/projects/shop/deploy", `{"git":{"url":"https://github.com/acme/shop","revision":"main"}}`, bobSID, bobCSRF)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "The project's repository changed while you deployed. Deploy again.") {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
	app, _ := s.findApp(context.Background(), store.DefaultWorkspace, "shop")
	if got := app.Spec.Source.Git.URL; got != "https://github.com/acme/elsewhere" {
		t.Errorf("the deploy wrote over the move: %s", got)
	}
}

// A deploy that leaves GitHub drops the record of who linked it.
func TestABlobDeployDropsTheLinkRecord(t *testing.T) {
	s, _, _ := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	sid, csrf, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	if rec := doCookie(t, s, "POST", "/api/projects", createBody("shop", "https://github.com/acme/shop"), sid, csrf); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	// An image deploy keeps the repository as the source: the record stays.
	if rec := doCookie(t, s, "POST", "/api/projects/shop/deploy", `{"image":"registry.example.test/shop:1"}`, sid, csrf); rec.Code != http.StatusAccepted {
		t.Fatalf("image: %d %s", rec.Code, rec.Body.String())
	}
	if got := linkedBy(t, s, "shop"); got != githubtest.UserLogin {
		t.Errorf("after an image deploy: %q", got)
	}
	if rec := doCookie(t, s, "POST", "/api/projects/shop/deploy", `{"blob":{"url":"https://files.example.test/shop.tgz"}}`, sid, csrf); rec.Code != http.StatusAccepted {
		t.Fatalf("blob: %d %s", rec.Code, rec.Body.String())
	}
	if got := linkedBy(t, s, "shop"); got != "" {
		t.Errorf("after a blob deploy: %q", got)
	}
}

// A refresh GitHub refuses with its own words leaves them in the log: the
// picker answers one sentence.
func TestARefusedRefreshDoesNotReachThePickersBody(t *testing.T) {
	s, cr, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	sid, csrf, sub := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	// The access token is gone from memory, and the App's client secret
	// changed: GitHub refuses the renewal, with a description.
	s.githubUsers.forget("local", sub)
	sec := &corev1.Secret{}
	if err := cr.Get(context.Background(), types.NamespacedName{Namespace: "shpyrd-system", Name: github.SecretName}, sec); err != nil {
		t.Fatal(err)
	}
	sec.Data[github.KeyClientSecret] = []byte("rotated")
	if err := cr.Update(context.Background(), sec); err != nil {
		t.Fatal(err)
	}
	s.githubApp = fake.Source(cr, "shpyrd-system")
	for _, path := range []string{"/api/workspace/github/accounts", "/api/workspace/github/repositories", "/api/workspace/github/repositories/acme/shop/branches"} {
		rec := doCookie(t, s, "GET", path, "", sid, csrf)
		if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "GitHub did not answer. Try again in a minute") ||
			strings.Contains(rec.Body.String(), "client_secret") || strings.Contains(rec.Body.String(), "refused to renew") {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
}

// gitArchive is an uploaded archive whose project is built from url: no
// source carried, so the restore keeps the repository.
func gitArchive(t *testing.T, url string) string {
	t.Helper()
	ctx := context.Background()
	b, err := projectarchive.New(t.TempDir(), projectarchive.DefaultLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	m := projectArchiveMetadata{Name: "crafted", Spec: shpyrdv1.AppSpec{Source: &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: url, Revision: "main"}}}, Config: map[string][]byte{}}
	raw, _ := json.Marshal(m)
	if err := b.Add(ctx, "project.json", bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := b.Write(ctx, &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// Restoring an archive that names a private repository is the same check
// as a deploy to it.
func TestRestoringAnArchiveOfAPrivateRepositoryNeedsSomeoneWhoCanReadIt(t *testing.T) {
	site := onGitHub("site", "https://github.com/acme/site", "main", "")
	site.Status.Image, site.Status.URL = "", ""
	s, _, _ := githubServer(t, site)
	connectAcme(t, s, store.DefaultWorkspace)
	sid, csrf, _ := signInPerson(t, s, "cleo@example.test", store.WorkspaceRoleAdmin)
	archive := gitArchive(t, "https://github.com/acme/shop")
	restore := func() *httptest.ResponseRecorder {
		return doCookie(t, s, "POST", "/api/project-archives/site/restore", archive, sid, csrf)
	}
	if rec := restore(); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "github_link_required") {
		t.Fatalf("cleo, unlinked: %d %s", rec.Code, rec.Body.String())
	}
	app, _ := s.findApp(context.Background(), store.DefaultWorkspace, "site")
	if got := app.Spec.Source.Git.URL; got != "https://github.com/acme/site" {
		t.Errorf("the refused restore moved site to %s", got)
	}
	linkAs(t, s, sid, csrf, githubtest.Code)
	if rec := restore(); rec.Code != http.StatusOK {
		t.Fatalf("cleo, linked: %d %s", rec.Code, rec.Body.String())
	}
	if got := linkedBy(t, s, "site"); got != githubtest.UserLogin {
		t.Errorf("linked by %q after the restore", got)
	}
}
func TestTheProjectSaysHowItLearnsOfPushes(t *testing.T) {
	s, _, _ := githubServer(t,
		onGitHub("shop", "https://github.com/acme/shop", "main", ""),
		recordOn(onGitHub("blog", "https://gitlab.com/acme/blog", "main", ""), gitauth.SourceRecord{Ref: "main", ProjectHook: true}),
		recordOn(onGitHub("docs", "https://gitlab.com/acme/docs", "main", ""), gitauth.SourceRecord{Ref: "main", Watch: "github-app", LastWebhookAt: &time.Time{}}),
		// Delivered before the mode was stored: its own webhook (RFC-0054).
		recordOn(onGitHub("old", "https://gitlab.com/acme/old", "main", ""), gitauth.SourceRecord{Ref: "main", LastWebhookAt: &time.Time{}}),
	)
	connectAcme(t, s, store.DefaultWorkspace)
	for slug, want := range map[string]string{"shop": `"sourceWatch":{"mode":"github-app"}`, "blog": `"sourceWatch":{"mode":"webhook"}`, "docs": `"sourceWatch":{"mode":"poll","pollEvery":300}`, "old": `"sourceWatch":{"mode":"webhook"}`} {
		rec := do(t, s, "GET", "/api/projects/"+slug, "", true)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: %d %s", slug, rec.Code, rec.Body.String())
		}
	}
}

// asMCPAgent adds an identifier like the MCP extension's OAuth one
// (ee/mcp): the request is the person, Provider "oauth", with their roles.
func asMCPAgent(t *testing.T, s *Server) func(email, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	s.identifiers = append(s.identifiers, func(c *gin.Context) bool {
		email := c.GetHeader("X-Test-Agent")
		if email == "" {
			return false
		}
		person, err := s.store.GetIdentity(c.Request.Context(), store.DefaultWorkspace, email)
		if err != nil {
			t.Fatal(err)
		}
		owner := ext.Identity{Subject: person.ID, Email: person.Email, Provider: person.Provider}
		ws, _ := s.store.Workspace(c.Request.Context(), store.DefaultWorkspace)
		roles, err := s.rolesInWorkspace(c.Request.Context(), ws, owner)
		if err != nil {
			t.Fatal(err)
		}
		ext.SetIdentity(c, ext.Identity{Subject: person.ID, Email: person.Email, Provider: "oauth"})
		c.Set(rolesKey, roles)
		return true
	})
	return func(email, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("X-Test-Agent", email)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
}

// An agent through the MCP extension's OAuth is the person, under the
// provider "oauth": its GitHub link is the one stored under the person's
// own sign-in.
func TestAnMCPAgentUsesThePersonsLink(t *testing.T) {
	s, _, _ := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	anaSID, anaCSRF, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	signInPerson(t, s, "bob@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, anaSID, anaCSRF, githubtest.Code)
	agent := asMCPAgent(t, s)
	if rec := agent("ana@example.test", "GET", "/api/workspace/github/accounts", ""); rec.Code != http.StatusOK {
		t.Errorf("ana's agent, the picker: %d %s", rec.Code, rec.Body.String())
	}
	if rec := agent("ana@example.test", "POST", "/api/projects", createBody("shop", "https://github.com/acme/shop")); rec.Code != http.StatusCreated {
		t.Errorf("ana's agent, create: %d %s", rec.Code, rec.Body.String())
	}
	if got := linkedBy(t, s, "shop"); got != githubtest.UserLogin {
		t.Errorf("linked by %q", got)
	}
	for _, call := range [][3]string{{"GET", "/api/workspace/github/accounts", ""}, {"POST", "/api/projects", createBody("shop2", "https://github.com/acme/shop")}} {
		if rec := agent("bob@example.test", call[0], call[1], call[2]); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"github_link_required"`) {
			t.Errorf("bob's agent, %s %s: %d %s", call[0], call[1], rec.Code, rec.Body.String())
		}
	}
	// A link starts and finishes in a browser only.
	if rec := agent("ana@example.test", "POST", "/api/me/github/link", ""); rec.Code != http.StatusForbidden {
		t.Errorf("an agent starts a link: %d %s", rec.Code, rec.Body.String())
	}
}
