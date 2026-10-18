package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// deliverApp posts to the App's webhook, signed with secret, as GitHub does.
func deliverApp(t *testing.T, s *Server, event, delivery string, body []byte, secret string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/github/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", delivery)
	req.Header.Set("X-Hub-Signature-256", "sha256="+hmacHex(secret, body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func appPush(installation int64, fullName, ref string) []byte {
	return []byte(fmt.Sprintf(`{"ref":%q,"before":"0123456789abcdef0123456789abcdef01234567","after":%q,"installation":{"id":%d},"repository":{"full_name":%q},"head_commit":{"id":%q,"message":"fix login","author":{"name":"Alice","username":"alice"}}}`,
		ref, hookCommit, installation, fullName, hookCommit))
}

func installationEvent(action string, id int64) []byte {
	return []byte(fmt.Sprintf(`{"action":%q,"installation":{"id":%d,"account":{"login":"Acme","type":"Organization"}},"sender":{"login":"octo"}}`, action, id))
}

func onGitHub(name, url, rev, workspace string) *shpyrdv1.App {
	app := sampleApp(name, shpyrdv1.PhaseRunning)
	app.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: url, Revision: rev}}
	if workspace != "" {
		app.Labels = map[string]string{shpyrdv1.LabelWorkspace: workspace}
	}
	return app
}

func connectAcme(t *testing.T, s *Server, ws string) {
	t.Helper()
	if err := s.store.AddGitHubInstallation(context.Background(), ws, store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "ana"}); err != nil {
		t.Fatal(err)
	}
}

func pushOn(t *testing.T, cr client.Client, slug string) *gitauth.Push {
	t.Helper()
	p, err := gitauth.ReadPush(appNamed(t, cr, slug).Annotations[shpyrdv1.AnnotationGitPush])
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGitHubWebhookRoutesAPushToTheProjectsOnThatRepositoryWhateverTheCase(t *testing.T) {
	s, cr, _ := githubServer(t,
		onGitHub("shop", "https://github.com/acme/shop", "main", ""),
		onGitHub("shopsh", "git@github.com:ACME/Shop.git", "", ""),
		onGitHub("shopssh", "ssh://git@github.com/acme/shop.git", "main", ""),
		onGitHub("shopdev", "https://github.com/acme/shop", "dev", ""),
		onGitHub("site", "https://github.com/acme/site", "main", ""),
		onGitHub("theirs", "https://github.com/acme/shop", "main", "beta"),
	)
	connectAcme(t, s, store.DefaultWorkspace)
	rec := deliverApp(t, s, "push", "d-1", appPush(42, "Acme/Shop", "refs/heads/main"), githubtest.WebhookSecret)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"projects":["shop","shopsh","shopssh"]`) {
		t.Fatalf("push: %d %s", rec.Code, rec.Body.String())
	}
	for _, slug := range []string{"shop", "shopsh", "shopssh"} {
		if p := pushOn(t, cr, slug); p == nil || p.SHA != hookCommit || p.Delivery != "d-1" || p.Author != "alice" || p.Ref != "refs/heads/main" {
			t.Errorf("%s: %+v", slug, p)
		}
	}
	for _, slug := range []string{"shopdev", "site", "theirs"} {
		if p := pushOn(t, cr, slug); p != nil {
			t.Errorf("%s took a push not for it: %+v", slug, p)
		}
	}
	// GitHub delivers the same push again: nothing is written twice.
	before := appNamed(t, cr, "shop").Annotations[shpyrdv1.AnnotationGitPush]
	if rec := deliverApp(t, s, "push", "d-1", appPush(42, "Acme/Shop", "refs/heads/main"), githubtest.WebhookSecret); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"projects":[]`) {
		t.Errorf("again: %d %s", rec.Code, rec.Body.String())
	}
	if appNamed(t, cr, "shop").Annotations[shpyrdv1.AnnotationGitPush] != before {
		t.Error("a repeated delivery was applied again")
	}
}

func TestGitHubWebhookBeforeTheCallbackIsAcceptedAndIgnored(t *testing.T) {
	s, cr, _ := githubServer(t, onGitHub("shop", "https://github.com/acme/shop", "main", ""))
	if rec := deliverApp(t, s, "installation", "d-0", installationEvent("created", 42), githubtest.WebhookSecret); rec.Code != http.StatusAccepted {
		t.Errorf("created: %d", rec.Code)
	}
	rec := deliverApp(t, s, "push", "d-1", appPush(42, "Acme/shop", "refs/heads/main"), githubtest.WebhookSecret)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "no workspace has connected") {
		t.Fatalf("early push: %d %s", rec.Code, rec.Body.String())
	}
	if p := pushOn(t, cr, "shop"); p != nil {
		t.Fatalf("an installation no workspace connected wrote %+v", p)
	}
	if _, err := s.store.GitHubInstallation(context.Background(), 42); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("installation.created connects nothing: %v", err)
	}
	connectAcme(t, s, store.DefaultWorkspace)
	if rec := deliverApp(t, s, "push", "d-2", appPush(42, "Acme/shop", "refs/heads/main"), githubtest.WebhookSecret); rec.Code != http.StatusAccepted {
		t.Fatalf("after connecting: %d", rec.Code)
	}
	if p := pushOn(t, cr, "shop"); p == nil || p.Delivery != "d-2" {
		t.Errorf("after connecting: %+v", p)
	}
}

func TestGitHubWebhookIgnoresAPushOfASuspendedInstallation(t *testing.T) {
	s, cr, _ := githubServer(t, onGitHub("shop", "https://github.com/acme/shop", "main", ""))
	connectAcme(t, s, store.DefaultWorkspace)
	if err := s.store.SetGitHubInstallationState(context.Background(), 42, true, false); err != nil {
		t.Fatal(err)
	}
	rec := deliverApp(t, s, "push", "d-1", appPush(42, "Acme/shop", "refs/heads/main"), githubtest.WebhookSecret)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "the installation is suspended") {
		t.Fatalf("suspended push: %d %s", rec.Code, rec.Body.String())
	}
	if p := pushOn(t, cr, "shop"); p != nil {
		t.Errorf("a suspended installation's push wrote %+v", p)
	}
}

func TestGitHubWebhookRecordsInstallationEvents(t *testing.T) {
	s, _, _ := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	heard := githubAudits(t)
	ctx := context.Background()
	if rec := deliverApp(t, s, "installation", "d-1", installationEvent("suspend", 42), githubtest.WebhookSecret); rec.Code != http.StatusAccepted {
		t.Fatalf("suspend: %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := s.store.GitHubInstallation(ctx, 42); got == nil || got.SuspendedAt == nil {
		t.Errorf("suspended: %+v", got)
	}
	deliverApp(t, s, "installation", "d-2", installationEvent("unsuspend", 42), githubtest.WebhookSecret)
	if got, _ := s.store.GitHubInstallation(ctx, 42); got == nil || got.SuspendedAt != nil {
		t.Errorf("unsuspended: %+v", got)
	}
	deliverApp(t, s, "installation", "d-3", installationEvent("deleted", 42), githubtest.WebhookSecret)
	if _, err := s.store.GitHubInstallation(ctx, 42); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("deleted: %v", err)
	}
	var actions []string
	for _, e := range *heard {
		actions = append(actions, e.Action)
		if e.Actor != "github:octo" || e.Detail != "workspace default" || e.Target != "github installation 42 (Acme)" {
			t.Errorf("entry %+v", e)
		}
	}
	if strings.Join(actions, ",") != "github.suspend,github.unsuspend,github.disconnect" {
		t.Errorf("audited %v", actions)
	}
	if rec := deliverApp(t, s, "installation", "d-4", installationEvent("deleted", 42), githubtest.WebhookSecret); rec.Code != http.StatusAccepted {
		t.Errorf("deleted again: %d", rec.Code)
	}
	if rec := deliverApp(t, s, "installation_repositories", "d-5", []byte(`{"action":"added"}`), githubtest.WebhookSecret); rec.Code != http.StatusAccepted {
		t.Errorf("repositories changed: %d", rec.Code)
	}
}

// listSelectors records the label selector of every App listing.
func listSelectors(s *Server) *[]string {
	var got []string
	s.apps = interceptor.NewClient(s.apps.(client.WithWatch), interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*shpyrdv1.AppList); ok {
				o := (&client.ListOptions{}).ApplyOptions(opts)
				sel := ""
				if o.LabelSelector != nil {
					sel = o.LabelSelector.String()
				}
				got = append(got, sel)
			}
			return c.List(ctx, list, opts...)
		},
	})
	return &got
}

func TestGitHubWebhookListsOnlyTheInstallationsWorkspace(t *testing.T) {
	s, cr, _ := githubServer(t,
		onGitHub("ours", "https://github.com/acme/shop", "main", ""),
		onGitHub("theirs", "https://github.com/acme/shop", "main", "beta"),
	)
	if _, err := s.store.CreateWorkspace(context.Background(), store.Workspace{Slug: "beta", Name: "Beta", Address: "beta"}); err != nil {
		t.Fatal(err)
	}
	connectAcme(t, s, "beta")
	selectors := listSelectors(s)
	rec := deliverApp(t, s, "push", "d-1", appPush(42, "Acme/shop", "refs/heads/main"), githubtest.WebhookSecret)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"projects":["theirs"]`) {
		t.Fatalf("push: %d %s", rec.Code, rec.Body.String())
	}
	if fmt.Sprint(*selectors) != "["+shpyrdv1.LabelWorkspace+"=beta]" {
		t.Errorf("listed with %q", *selectors)
	}
	if appNamed(t, cr, "ours").Annotations[shpyrdv1.AnnotationGitPush] != "" {
		t.Error("another workspace's App got the push")
	}
}

func TestGitHubWebhookOfTheDefaultWorkspaceListsAppsWithoutALabel(t *testing.T) {
	s, _, _ := githubServer(t,
		onGitHub("ours", "https://github.com/acme/shop", "main", ""),
		onGitHub("theirs", "https://github.com/acme/shop", "main", "beta"),
	)
	connectAcme(t, s, store.DefaultWorkspace)
	selectors := listSelectors(s)
	rec := deliverApp(t, s, "push", "d-1", appPush(42, "Acme/shop", "refs/heads/main"), githubtest.WebhookSecret)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"projects":["ours"]`) {
		t.Fatalf("push: %d %s", rec.Code, rec.Body.String())
	}
	if len(*selectors) != 1 || (*selectors)[0] != "" {
		t.Errorf("listed with %q", *selectors)
	}
}

func TestGitHubChangesReachTheControllers(t *testing.T) {
	s, _, _ := githubServer(t)
	var changed []string
	s.opts.GitHubChanged = func(ws string) { changed = append(changed, ws) }
	if rec := connected(t, s, "42"); rec.Code != http.StatusFound {
		t.Fatalf("connect: %d", rec.Code)
	}
	for i, ev := range []string{"suspend", "unsuspend", "created", "deleted"} {
		deliverApp(t, s, "installation", fmt.Sprintf("d-%d", i), installationEvent(ev, 42), githubtest.WebhookSecret)
	}
	connectAcme(t, s, store.DefaultWorkspace)
	deliverApp(t, s, "installation_repositories", "d-r", installationEvent("added", 42), githubtest.WebhookSecret)
	deliverApp(t, s, "installation_repositories", "d-x", installationEvent("added", 99), githubtest.WebhookSecret)
	if rec := do(t, s, "DELETE", "/api/workspace/github/installations/42", "", true); rec.Code != http.StatusNoContent {
		t.Fatalf("disconnect: %d", rec.Code)
	}
	// connect; suspend, unsuspend, deleted (not created); repositories
	// added on 42 (not on 99, connected nowhere); disconnect.
	if len(changed) != 6 || strings.Join(changed, ",") != "default,default,default,default,default,default" {
		t.Errorf("changed %v", changed)
	}
}

func TestGitHubWebhookChecksTheSignatureAndAnswersAPing(t *testing.T) {
	s, _, _ := githubServer(t)
	body := []byte(`{"zen":"Design for failure."}`)
	if rec := deliverApp(t, s, "ping", "d-1", body, "not-the-secret"); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong secret: %d", rec.Code)
	}
	req := httptest.NewRequest("POST", "/api/github/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-Gitlab-Token", githubtest.WebhookSecret)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a GitLab token is no GitHub signature: %d", rec.Code)
	}
	if rec := deliverApp(t, s, "ping", "d-2", body, githubtest.WebhookSecret); rec.Code != http.StatusOK || rec.Body.String() != "pong" {
		t.Errorf("ping: %d %s", rec.Code, rec.Body.String())
	}
	if rec := deliverApp(t, s, "check_suite", "d-3", body, githubtest.WebhookSecret); rec.Code != http.StatusAccepted {
		t.Errorf("another event: %d", rec.Code)
	}
	big := bytes.Repeat([]byte("x"), githubHookBodyLimit+1)
	if rec := deliverApp(t, s, "push", "d-4", big, githubtest.WebhookSecret); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("too large: %d", rec.Code)
	}
}

func TestGitHubWebhookIsNotThereWithoutTheApp(t *testing.T) {
	s, cr := newTestServer(t, nil, nil)
	s.githubApp = githubtest.NewServer(t).Source(cr, "shpyrd-system")
	if rec := deliverApp(t, s, "ping", "d-1", []byte(`{}`), githubtest.WebhookSecret); rec.Code != http.StatusNotFound {
		t.Errorf("%d", rec.Code)
	}
}
func ciEvent(event string, installation int64, fullName, sha, action string) []byte {
	if event == "status" {
		return []byte(fmt.Sprintf(`{"sha":%q,"state":"success","context":"ci/test","installation":{"id":%d},"repository":{"full_name":%q}}`, sha, installation, fullName))
	}
	return []byte(fmt.Sprintf(`{"action":%q,"check_suite":{"head_sha":%q,"status":"completed","conclusion":"success"},"installation":{"id":%d},"repository":{"full_name":%q}}`,
		action, sha, installation, fullName))
}

// recordOn leaves the source watcher's record on app.
func recordOn(app *shpyrdv1.App, rec gitauth.SourceRecord) *shpyrdv1.App {
	raw, _ := json.Marshal(rec)
	if app.Annotations == nil {
		app.Annotations = map[string]string{}
	}
	app.Annotations[shpyrdv1.AnnotationGitSource] = string(raw)
	return app
}

func TestGitHubWebhookNudgesOnlyTheAppsWaitingOnThatCommit(t *testing.T) {
	const older = "0123456789abcdef0123456789abcdef01234567"
	held := gitauth.SourceRecord{Ref: "main", Latest: hookCommit, Held: hookCommit}
	failed := gitauth.SourceRecord{Ref: "main", Latest: hookCommit, NotDeployed: hookCommit, NotDeployedReason: gitauth.ReasonCIFailedPrefix + "test"}
	ignored := gitauth.SourceRecord{Ref: "main", Latest: hookCommit, NotDeployed: hookCommit, NotDeployedReason: gitauth.ReasonNoWatchedChanges}
	another := gitauth.SourceRecord{Ref: "main", Latest: older, Held: older}
	s, cr, _ := githubServer(t,
		recordOn(onGitHub("shop", "https://github.com/acme/shop", "main", ""), held),
		recordOn(onGitHub("shopci", "https://github.com/Acme/Shop", "main", ""), failed),
		recordOn(onGitHub("shopdocs", "https://github.com/acme/shop", "main", ""), ignored),
		recordOn(onGitHub("shopnext", "https://github.com/acme/shop", "main", ""), another),
		recordOn(onGitHub("site", "https://github.com/acme/site", "main", ""), held),
		recordOn(onGitHub("theirs", "https://github.com/acme/shop", "main", "beta"), held),
	)
	connectAcme(t, s, store.DefaultWorkspace)
	rec := deliverApp(t, s, "status", "d-5", ciEvent("status", 42, "Acme/shop", strings.ToUpper(hookCommit), ""), githubtest.WebhookSecret)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"projects":["shop","shopci"]`) {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	for _, slug := range []string{"shop", "shopci"} {
		if n := gitauth.ReadCINudge(appNamed(t, cr, slug).Annotations[shpyrdv1.AnnotationGitCI]); n == nil || n.SHA != hookCommit || n.Delivery != "d-5" {
			t.Errorf("%s: %+v", slug, n)
		}
	}
	for _, slug := range []string{"shopdocs", "shopnext", "site", "theirs"} {
		if v, ok := appNamed(t, cr, slug).Annotations[shpyrdv1.AnnotationGitCI]; ok {
			t.Errorf("%s was nudged: %s", slug, v)
		}
	}
}

func TestGitHubWebhookTakesACheckSuiteOnlyWhenItCompletes(t *testing.T) {
	s, cr, _ := githubServer(t, recordOn(onGitHub("shop", "https://github.com/acme/shop", "main", ""), gitauth.SourceRecord{Ref: "main", Latest: hookCommit, Held: hookCommit}))
	connectAcme(t, s, store.DefaultWorkspace)
	if rec := deliverApp(t, s, "check_suite", "d-6", ciEvent("check_suite", 42, "Acme/shop", hookCommit, "requested"), githubtest.WebhookSecret); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "ignored") {
		t.Errorf("requested: %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := appNamed(t, cr, "shop").Annotations[shpyrdv1.AnnotationGitCI]; ok {
		t.Fatal("a suite just requested nudges nothing")
	}
	if rec := deliverApp(t, s, "check_suite", "d-7", ciEvent("check_suite", 42, "Acme/shop", hookCommit, "completed"), githubtest.WebhookSecret); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"projects":["shop"]`) {
		t.Errorf("completed: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubWebhookRefusesACIEventWithABadSignature(t *testing.T) {
	s, cr, _ := githubServer(t, recordOn(onGitHub("shop", "https://github.com/acme/shop", "main", ""), gitauth.SourceRecord{Ref: "main", Latest: hookCommit, Held: hookCommit}))
	connectAcme(t, s, store.DefaultWorkspace)
	if rec := deliverApp(t, s, "status", "d-8", ciEvent("status", 42, "Acme/shop", hookCommit, ""), "not-the-secret"); rec.Code != http.StatusUnauthorized {
		t.Errorf("bad signature: %d", rec.Code)
	}
	if _, ok := appNamed(t, cr, "shop").Annotations[shpyrdv1.AnnotationGitCI]; ok {
		t.Error("nudged on a forged delivery")
	}
}

func TestGitHubWebhookIgnoresTheCIOfASuspendedOrUnknownInstallation(t *testing.T) {
	s, _, _ := githubServer(t, recordOn(onGitHub("shop", "https://github.com/acme/shop", "main", ""), gitauth.SourceRecord{Ref: "main", Latest: hookCommit, Held: hookCommit}))
	if rec := deliverApp(t, s, "status", "d-9", ciEvent("status", 42, "Acme/shop", hookCommit, ""), githubtest.WebhookSecret); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "no workspace") {
		t.Errorf("unknown: %d %s", rec.Code, rec.Body.String())
	}
	connectAcme(t, s, store.DefaultWorkspace)
	if err := s.store.SetGitHubInstallationState(context.Background(), 42, true, false); err != nil {
		t.Fatal(err)
	}
	if rec := deliverApp(t, s, "status", "d-10", ciEvent("status", 42, "Acme/shop", hookCommit, ""), githubtest.WebhookSecret); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "suspended") {
		t.Errorf("suspended: %d %s", rec.Code, rec.Body.String())
	}
}

// The sweep redelivers what GitHub could not deliver; a delivery shpyrd
// took already is not left on the App again.
func TestARedeliveredPushIsNotAppliedTwice(t *testing.T) {
	s, cr, _ := githubServer(t, onGitHub("shop", "https://github.com/acme/shop", "main", ""))
	connectAcme(t, s, store.DefaultWorkspace)
	if rec := deliverApp(t, s, "push", "d-1", appPush(42, "Acme/shop", "refs/heads/main"), githubtest.WebhookSecret); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"shop"`) {
		t.Fatalf("first: %d %s", rec.Code, rec.Body.String())
	}
	before := appNamed(t, cr, "shop")
	rec := deliverApp(t, s, "push", "d-1", appPush(42, "Acme/shop", "refs/heads/main"), githubtest.WebhookSecret)
	if rec.Code != http.StatusAccepted || strings.Contains(rec.Body.String(), `"shop"`) {
		t.Errorf("redelivered while waiting: %d %s", rec.Code, rec.Body.String())
	}
	if after := appNamed(t, cr, "shop"); after.ResourceVersion != before.ResourceVersion {
		t.Error("the redelivery wrote the App again")
	}
	// Once the watcher took it, its record remembers the delivery.
	app := appNamed(t, cr, "shop")
	delete(app.Annotations, shpyrdv1.AnnotationGitPush)
	recordOn(app, gitauth.SourceRecord{Ref: "main", Latest: hookCommit, Delivery: "d-1"})
	if err := cr.Update(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if rec := deliverApp(t, s, "push", "d-1", appPush(42, "Acme/shop", "refs/heads/main"), githubtest.WebhookSecret); strings.Contains(rec.Body.String(), `"shop"`) {
		t.Errorf("redelivered after the watcher took it: %s", rec.Body.String())
	}
	if _, ok := appNamed(t, cr, "shop").Annotations[shpyrdv1.AnnotationGitPush]; ok {
		t.Error("left on the App again")
	}
}

func TestAPushThroughTheAppSaysSo(t *testing.T) {
	s, cr, _ := githubServer(t, onGitHub("shop", "https://github.com/acme/shop", "main", ""))
	connectAcme(t, s, store.DefaultWorkspace)
	deliverApp(t, s, "push", "d-1", appPush(42, "Acme/shop", "refs/heads/main"), githubtest.WebhookSecret)
	if p := pushOn(t, cr, "shop"); p == nil || !p.App {
		t.Errorf("push %+v", p)
	}
}
