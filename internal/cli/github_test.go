package cli

import (
	"net/http"
	"strconv"
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

func TestGitHubDisconnectPrefersTheIDAndRefusesAmbiguousNames(t *testing.T) {
	row := func(id int, account string) string {
		return `{"id":` + strconv.Itoa(id) + `,"account":"` + account + `","type":"User","connectedBy":"x","createdAt":"2026-10-01T00:00:00Z","suspended":false,"manageURL":"u"}`
	}
	var deleted atomic.Value
	deleted.Store("")
	handler := func(rows string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "DELETE" {
				deleted.Store(r.URL.Path)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_, _ = w.Write([]byte(`{"configured":true,"installations":[` + rows + `]}`))
		}
	}
	// An id beats a login that spells the same number.
	if _, err := runShpyrd(t, handler(row(5, "123")+","+row(123, "other")), "github", "disconnect", "123"); err != nil || deleted.Load() != "/api/workspace/github/installations/123" {
		t.Fatalf("id: %v %v", err, deleted.Load())
	}
	// Two rows of one account are refused, nothing deleted.
	deleted.Store("")
	_, err := runShpyrd(t, handler(row(1, "Acme")+","+row(2, "acme")), "github", "disconnect", "ACME")
	if err == nil || !strings.Contains(err.Error(), "matches several connections (ids 1, 2): give the id") || deleted.Load() != "" {
		t.Fatalf("ambiguous: %v %v", err, deleted.Load())
	}
	// One match goes.
	if _, err := runShpyrd(t, handler(row(1, "Acme")+","+row(2, "beta")), "github", "disconnect", "beta"); err != nil || deleted.Load() != "/api/workspace/github/installations/2" {
		t.Fatalf("one: %v %v", err, deleted.Load())
	}
}

func TestGitHubConnectRidesOutABlipButNotFour(t *testing.T) {
	fastGitHubPolls(t)
	var reads atomic.Int32
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		n := reads.Add(1)
		switch {
		case n == 1:
			_, _ = w.Write([]byte(`{"configured":true,"installations":[]}`))
		case n <= 4: // three failures in a row
			http.Error(w, "blip", http.StatusBadGateway)
		default:
			_, _ = w.Write([]byte(`{"configured":true,"installations":[` + acmeConnection + `]}`))
		}
	}, "github", "connect")
	if err != nil || !strings.Contains(out, "Connected Acme") {
		t.Fatalf("three blips: %v\n%s", err, out)
	}
	reads.Store(0)
	_, err = runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		if reads.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"configured":true,"installations":[]}`))
			return
		}
		http.Error(w, "down", http.StatusBadGateway)
	}, "github", "connect")
	if err == nil || reads.Load() != 5 {
		t.Fatalf("four blips: err=%v reads=%d", err, reads.Load())
	}
}
