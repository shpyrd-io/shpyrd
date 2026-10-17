package cli

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGitSourceSettings(t *testing.T) {
	paths, wait, err := gitSourceSettings(nil, false, false, false)
	if err != nil || paths != nil || wait != nil {
		t.Errorf("no flag leaves the project's settings: %v %v %v", paths, wait, err)
	}
	paths, wait, err = gitSourceSettings([]string{"services/api/**", "!**/*.md"}, false, true, false)
	if err != nil || paths == nil || strings.Join(*paths, "|") != "services/api/**|!**/*.md" || wait == nil || !*wait {
		t.Errorf("set: %v %v %v", paths, wait, err)
	}
	paths, wait, err = gitSourceSettings(nil, true, false, true)
	if err != nil || paths == nil || len(*paths) != 0 || wait == nil || *wait {
		t.Errorf("cleared: %v %v %v", paths, wait, err)
	}
	if _, _, err := gitSourceSettings([]string{"a/**"}, true, false, false); err == nil {
		t.Error("--watch-path and --no-watch-paths contradict each other")
	}
	if _, _, err := gitSourceSettings(nil, false, true, true); err == nil {
		t.Error("--wait-for-ci and --no-wait-for-ci contradict each other")
	}
}

func TestDeploySendsWatchPathsAndWaitForCIAndSaysWhenTheyDoNotApply(t *testing.T) {
	var sent string
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/projects/shop":
			_, _ = w.Write([]byte(`{"slug":"shop","spec":{"source":{"git":{"url":"https://gitlab.example.test/acme/shop","revision":"main"}}},"status":{"phase":"Running","generation":1,"releases":[]}}`))
		case "POST /api/projects/shop/deploy":
			raw, _ := io.ReadAll(r.Body)
			sent = string(raw)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"slug":"shop","sourceNote":"Watch paths and wait for CI work through the GitHub App, which does not cover this repository: every push deploys.","spec":{},"status":{"phase":"Building","generation":2,"releases":[]}}`))
		default:
			http.NotFound(w, r)
		}
	}, "deploy", "--project", "shop", "--git", "https://gitlab.example.test/acme/shop", "--watch-path", "services/api/**", "--watch-path", "!**/*.md", "--wait-for-ci", "--no-wait")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(sent, `"watchPaths":["services/api/**","!**/*.md"]`) || !strings.Contains(sent, `"waitForChecks":true`) {
		t.Errorf("sent %s", sent)
	}
	if !strings.Contains(out, "which does not cover this repository") {
		t.Errorf("the note is said: %s", out)
	}
}

func TestWatchPathFlagsGoWithGit(t *testing.T) {
	_, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("nothing is sent: %s %s", r.Method, r.URL.Path)
	}, "deploy", "--project", "shop", "--wait-for-ci")
	if err == nil || !strings.Contains(err.Error(), "go with --git") {
		t.Errorf("err = %v", err)
	}
}

func TestProjectsInfoShowsAHeldAndANotDeployedCommit(t *testing.T) {
	since := time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339)
	for source, want := range map[string]string{
		`{"commit":"0123456789abcdef0123456789abcdef01234567","latest":"89abcdef0123456789abcdef0123456789abcdef","held":"89abcdef0123456789abcdef0123456789abcdef","heldSince":"` + since + `"}`:                                "Commit:     89abcde waits for CI (since 2m)",
		`{"commit":"0123456789abcdef0123456789abcdef01234567","latest":"89abcdef0123456789abcdef0123456789abcdef","notDeployed":"89abcdef0123456789abcdef0123456789abcdef","notDeployedReason":"CI failed: test"}`:               "Commit:     89abcde not deployed: CI failed: test",
		`{"commit":"0123456789abcdef0123456789abcdef01234567","latest":"89abcdef0123456789abcdef0123456789abcdef","notDeployed":"89abcdef0123456789abcdef0123456789abcdef","notDeployedReason":"No changes in the watch paths"}`: "Commit:     89abcde not deployed: no changes in the watch paths",
	} {
		out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method+" "+r.URL.Path != "GET /api/projects/shop" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"slug":"shop","spec":{"source":{"git":{"url":"https://github.com/acme/shop","revision":"main","waitForChecks":true}}},"status":{"phase":"Running","releases":[],"source":` + source + `}}`))
		}, "projects", "info", "shop")
		if err != nil || !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s (%v)", want, out, err)
		}
	}
}
