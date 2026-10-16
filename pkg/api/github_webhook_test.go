package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

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
