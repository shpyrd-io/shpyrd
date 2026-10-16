package cli

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const acmeConnection = `{"id":42,"account":"Acme","type":"Organization","connectedBy":"ana@acme.test","createdAt":"2026-10-16T12:00:00Z","suspended":false,"manageURL":"https://github.com/organizations/Acme/settings/installations/42"}`

func fastGitHubPolls(t *testing.T) *[]string {
	t.Helper()
	opened := []string{}
	prevEvery, prevWait, prevBrowse := githubPollEvery, githubConnectWait, browse
	githubPollEvery, githubConnectWait = 5*time.Millisecond, time.Second
	browse = func(u string) error { opened = append(opened, u); return nil }
	t.Cleanup(func() { githubPollEvery, githubConnectWait, browse = prevEvery, prevWait, prevBrowse })
	return &opened
}

func TestGitHubConnectOpensTheGitPageAndWaitsForTheNewConnection(t *testing.T) {
	opened := fastGitHubPolls(t)
	var reads atomic.Int32
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/workspace/github":
			list := `[{"id":7,"account":"old","type":"User","connectedBy":"x","createdAt":"2026-10-01T00:00:00Z","suspended":false,"manageURL":"u"}]`
			if reads.Add(1) > 3 {
				list = `[{"id":7,"account":"old","type":"User","connectedBy":"x","createdAt":"2026-10-01T00:00:00Z","suspended":false,"manageURL":"u"},` + acmeConnection + `]`
			}
			_, _ = w.Write([]byte(`{"configured":true,"appURL":"https://github.com/apps/shpyrd-test","installations":` + list + `}`))
		default:
			t.Errorf("unexpected %s %s: the CLI never starts the connect itself", r.Method, r.URL.Path)
		}
	}, "github", "connect")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(*opened) != 1 || !strings.HasSuffix((*opened)[0], "/workspace/git") || !strings.HasPrefix((*opened)[0], "http://127.0.0.1:") {
		t.Errorf("opened %v", *opened)
	}
	if !strings.Contains(out, (*opened)[0]) || !strings.Contains(out, "Connect GitHub there; waiting for the connection") {
		t.Errorf("output lacks the page URL or the waiting line:\n%s", out)
	}
	if !strings.Contains(out, "Connected Acme (organisation), installation 42") || strings.Contains(out, "Connected old") {
		t.Errorf("output:\n%s", out)
	}
}

func TestGitHubConnectSaysWhenGitHubIsNotSetUp(t *testing.T) {
	fastGitHubPolls(t)
	_, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"configured":false,"note":"GitHub is not set up on this platform. An operator sets it up with ` + "`shpyrd-ctl github app create`" + `.","installations":[]}`))
	}, "github", "connect")
	if err == nil || !strings.Contains(err.Error(), "shpyrd-ctl github app create") {
		t.Errorf("err = %v", err)
	}
}

func TestGitHubConnectGivesUpAfterTheWait(t *testing.T) {
	fastGitHubPolls(t)
	githubConnectWait = 30 * time.Millisecond
	_, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"configured":true,"installations":[]}`))
	}, "github", "connect")
	if err == nil || !strings.Contains(err.Error(), "run shpyrd github connect again") {
		t.Errorf("err = %v", err)
	}
}

func TestGitHubListAndDisconnectByAccount(t *testing.T) {
	var deleted atomic.Value // the handler runs on the test server's goroutines
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			_, _ = w.Write([]byte(`{"configured":true,"installations":[` + acmeConnection + `]}`))
		case "DELETE":
			deleted.Store(r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		}
	}
	out, err := runShpyrd(t, handler, "github", "list")
	if err != nil || !strings.Contains(out, "Acme") || !strings.Contains(out, "organisation") || !strings.Contains(out, "ana@acme.test") || !strings.Contains(out, "42") {
		t.Fatalf("list: %v\n%s", err, out)
	}
	out, err = runShpyrd(t, handler, "github", "disconnect", "acme")
	if err != nil || deleted.Load() != "/api/workspace/github/installations/42" || !strings.Contains(out, "Disconnected Acme") || !strings.Contains(out, "settings/installations/42") {
		t.Fatalf("disconnect: %v %v\n%s", err, deleted.Load(), out)
	}
	if _, err := runShpyrd(t, handler, "github", "disconnect", "nobody"); err == nil || !strings.Contains(err.Error(), "shpyrd github list") {
		t.Errorf("unknown: %v", err)
	}
}

func TestGitHubReposFilters(t *testing.T) {
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/workspace/github/repositories" {
			t.Errorf("unexpected %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"installation":42,"fullName":"Acme/shop","private":true,"defaultBranch":"main"},{"installation":42,"fullName":"Acme/site","private":false,"defaultBranch":"trunk"}]`))
	}, "github", "repos", "--filter", "SHOP")
	if err != nil || !strings.Contains(out, "Acme/shop") || strings.Contains(out, "Acme/site") || !strings.Contains(out, "private") {
		t.Errorf("%v\n%s", err, out)
	}
}

func TestGitHooksShowSaysTheGitHubAppNeedsNothing(t *testing.T) {
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/projects/shop/git-auth":
			_, _ = w.Write([]byte(`{"url":"https://github.com/acme/shop","using":"github-app","githubAccount":"Acme"}`))
		case "/api/projects/shop":
			_, _ = w.Write([]byte(`{"slug":"shop","spec":{"source":{"git":{"url":"https://github.com/acme/shop"}}},"status":{"phase":"Running","releases":[]}}`))
		default:
			t.Errorf("unexpected %s %s: an App project needs no webhook of its own", r.Method, r.URL.Path)
		}
	}, "git-hooks", "show", "--project", "shop")
	if err != nil || !strings.Contains(out, "Pushes arrive through the GitHub App (Acme); nothing to set up.") || strings.Contains(out, "Payload URL") {
		t.Errorf("%v\n%s", err, out)
	}
}
