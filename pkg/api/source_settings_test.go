package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

func detailOf(t *testing.T, body []byte) AppDetail {
	t.Helper()
	var d AppDetail
	if err := json.Unmarshal(body, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestWatchPathsAndWaitForCIAreProjectSettings(t *testing.T) {
	s, cr, _ := githubServer(t, onGitHub("shop", "https://github.com/acme/shop", "main", ""))
	connectAcme(t, s, store.DefaultWorkspace)
	rec := do(t, s, "PATCH", "/api/projects/shop", `{"watchPaths":[" services/api/** ","","!**/*.md"],"waitForChecks":true}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	d := detailOf(t, rec.Body.Bytes())
	git := d.Spec.Source.Git
	if strings.Join(git.WatchPaths, "|") != "services/api/**|!**/*.md" || !git.WaitForChecks || !d.GitHubFeatures || d.SourceNote != "" {
		t.Errorf("view: %+v, features %v, note %q", git, d.GitHubFeatures, d.SourceNote)
	}
	if got := appNamed(t, cr, "shop").Spec.Source.Git; len(got.WatchPaths) != 2 || !got.WaitForChecks {
		t.Errorf("stored %+v", got)
	}
	if rec := do(t, s, "PATCH", "/api/projects/shop", `{"watchPaths":[]}`, true); rec.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body.String())
	}
	if got := appNamed(t, cr, "shop").Spec.Source.Git; got.WatchPaths != nil || !got.WaitForChecks {
		t.Errorf("[] removes the watch paths and leaves wait for CI: %+v", got)
	}
	if rec := do(t, s, "GET", "/api/projects/shop", "", true); !detailOf(t, rec.Body.Bytes()).GitHubFeatures {
		t.Errorf("the project's view says the App covers it: %s", rec.Body.String())
	}
}

func TestAWatchPathThatCannotBeReadIsRefused(t *testing.T) {
	s, cr, _ := githubServer(t, onGitHub("shop", "https://github.com/acme/shop", "main", ""))
	many := make([]string, 21)
	for i := range many {
		many[i] = fmt.Sprintf("%q", fmt.Sprintf("dir%d/**", i))
	}
	for body, words := range map[string]string{
		`{"watchPaths":["services/a**"]}`:                     "stands alone",
		`{"watchPaths":[` + strings.Join(many, ",") + `]}`:    "at most 20",
		`{"watchPaths":["` + strings.Repeat("a", 201) + `"]}`: "200 characters",
	} {
		if rec := do(t, s, "PATCH", "/api/projects/shop", body, true); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), words) {
			t.Errorf("%.40s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	if got := appNamed(t, cr, "shop").Spec.Source.Git; got.WatchPaths != nil {
		t.Errorf("nothing stored: %+v", got)
	}
}

func TestTheSettingsSaySoWhereTheGitHubAppDoesNotReach(t *testing.T) {
	site := sampleApp("site", shpyrdv1.PhaseRunning)
	site.Spec.Source = &shpyrdv1.Source{Blob: &shpyrdv1.BlobSource{URL: "http://shpyrd-server/api/sources/ab.tgz", SHA256: "ab"}}
	s, _ := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning), site})
	rec := do(t, s, "PATCH", "/api/projects/web1", `{"waitForChecks":true}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	if d := detailOf(t, rec.Body.Bytes()); d.GitHubFeatures || d.SourceNote != githubSourceNote {
		t.Errorf("features %v, note %q", d.GitHubFeatures, d.SourceNote)
	}
	if rec := do(t, s, "PATCH", "/api/projects/site", `{"waitForChecks":true}`, true); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "git repository") {
		t.Errorf("an archive has no branch: %d %s", rec.Code, rec.Body.String())
	}
}

func TestADeployKeepsTheProjectsWatchPathsUnlessItSays(t *testing.T) {
	app := onGitHub("shop", "https://github.com/acme/shop", "main", "")
	app.Spec.Source.Git.WatchPaths, app.Spec.Source.Git.WaitForChecks = []string{"services/api/**"}, true
	s, cr, _ := githubServer(t, app)
	// The git source's own fields are not read in a deploy: a bool cannot
	// say "keep".
	if rec := do(t, s, "POST", "/api/projects/shop/deploy", `{"git":{"url":"https://github.com/acme/shop","revision":"main","watchPaths":["x/**"]}}`, true); rec.Code != http.StatusAccepted {
		t.Fatalf("deploy: %d %s", rec.Code, rec.Body.String())
	}
	if got := appNamed(t, cr, "shop").Spec.Source.Git; strings.Join(got.WatchPaths, "|") != "services/api/**" || !got.WaitForChecks {
		t.Errorf("kept: %+v", got)
	}
	if rec := do(t, s, "POST", "/api/projects/shop/deploy", `{"git":{"url":"https://github.com/acme/shop","revision":"main"},"watchPaths":[],"waitForChecks":false}`, true); rec.Code != http.StatusAccepted {
		t.Fatalf("deploy: %d %s", rec.Code, rec.Body.String())
	}
	if got := appNamed(t, cr, "shop").Spec.Source.Git; got.WatchPaths != nil || got.WaitForChecks {
		t.Errorf("cleared: %+v", got)
	}
	if rec := do(t, s, "POST", "/api/projects/shop/deploy", `{"git":{"url":"https://github.com/acme/shop"},"watchPaths":["a**"]}`, true); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("a bad pattern: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "POST", "/api/projects/shop/deploy", `{"image":"ghcr.io/acme/shop:1","waitForChecks":true}`, true); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "go with git") {
		t.Errorf("with an image: %d %s", rec.Code, rec.Body.String())
	}
}

// Final review I3: a repository the connected installation does not
// include is not covered; GitHub's answer is kept, not asked on each view.
func TestTheSettingsSaySoForARepositoryTheInstallationDoesNotInclude(t *testing.T) {
	s, _, fake := githubServer(t, onGitHub("vault", "https://github.com/acme/vault", "main", ""))
	connectAcme(t, s, store.DefaultWorkspace)
	rec := do(t, s, "PATCH", "/api/projects/vault", `{"waitForChecks":true}`, true)
	if d := detailOf(t, rec.Body.Bytes()); rec.Code != http.StatusOK || d.GitHubFeatures || d.SourceNote != githubSourceNote {
		t.Fatalf("%d: features %v, note %q", rec.Code, d.GitHubFeatures, d.SourceNote)
	}
	minted := len(fake.Calls())
	if rec := do(t, s, "GET", "/api/projects/vault", "", true); detailOf(t, rec.Body.Bytes()).GitHubFeatures || len(fake.Calls()) != minted {
		t.Errorf("view: %s, calls %v", rec.Body.String(), fake.Calls())
	}
}
