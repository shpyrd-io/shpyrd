package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const paezaoLinked = `{"login":"paezao","linkedAt":"2026-10-17T10:00:00Z"}`

// aPerson answers /api/me for a signed-in person (the admin token has none).
func aPerson(w http.ResponseWriter) { _, _ = w.Write([]byte(`{"email":"ana@acme.test"}`)) }

func notLinked(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"error":"no GitHub account is linked: shpyrd github link"}`))
}

func notLinkedPerson(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/me" {
		aPerson(w)
		return
	}
	notLinked(w)
}

func TestGitHubLinkOpensTheLinkPageAndWaitsForIt(t *testing.T) {
	opened := fastGitHubPolls(t)
	var reads atomic.Int32
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/me":
			aPerson(w)
		case "GET /api/me/github":
			if reads.Add(1) <= 3 {
				notLinked(w)
				return
			}
			_, _ = w.Write([]byte(paezaoLinked))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}, "github", "link")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(*opened) != 1 || !strings.HasSuffix((*opened)[0], "/api/me/github/link") || !strings.HasPrefix((*opened)[0], "http://127.0.0.1:") {
		t.Errorf("opened %v", *opened)
	}
	if !strings.Contains(out, "Continue with GitHub there; waiting for the link") || !strings.Contains(out, "Linked GitHub account paezao") {
		t.Errorf("output:\n%s", out)
	}
}

func TestGitHubLinkSaysWhenAnAccountIsLinkedAlready(t *testing.T) {
	opened := fastGitHubPolls(t)
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(paezaoLinked))
	}, "github", "link")
	if err != nil || !strings.Contains(out, "Your GitHub account is linked: paezao") || len(*opened) != 0 {
		t.Errorf("%v %v\n%s", err, *opened, out)
	}
}

func TestGitHubLinkGivesUpAfterTheWait(t *testing.T) {
	fastGitHubPolls(t)
	githubConnectWait = 30 * time.Millisecond
	_, err := runShpyrd(t, notLinkedPerson, "github", "link")
	if err == nil || !strings.Contains(err.Error(), "run shpyrd github link again") {
		t.Errorf("err = %v", err)
	}
}

func TestGitHubUnlink(t *testing.T) {
	var deleted atomic.Bool
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/me":
			aPerson(w)
		case "GET /api/me/github":
			_, _ = w.Write([]byte(paezaoLinked))
		case "DELETE /api/me/github":
			deleted.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}, "github", "unlink")
	if err != nil || !deleted.Load() || !strings.Contains(out, "Unlinked paezao") || !strings.Contains(out, "keep deploying") {
		t.Errorf("%v\n%s", err, out)
	}
	if _, err := runShpyrd(t, notLinkedPerson, "github", "unlink"); err == nil || !strings.Contains(err.Error(), "no GitHub account is linked") {
		t.Errorf("nothing linked: %v", err)
	}
}

func TestALinkRequiredAnswerSaysToRunGitHubLink(t *testing.T) {
	_, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"Link your GitHub account first: Continue with GitHub in New project, or run shpyrd github link","code":"github_link_required"}`))
	}, "github", "repos")
	if err == nil || err.Error() != "link your GitHub account first: run shpyrd github link" {
		t.Errorf("err = %v", err)
	}
	// Another 409 keeps the server's words.
	_, err = runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"GitHub is not set up on this platform."}`))
	}, "github", "repos")
	if err == nil || !strings.Contains(err.Error(), "GitHub is not set up") {
		t.Errorf("err = %v", err)
	}
}

func TestWhoAmIShowsTheLinkedGitHubAccount(t *testing.T) {
	for _, c := range []struct {
		name   string
		github func(w http.ResponseWriter)
		want   string
	}{
		{"linked", func(w http.ResponseWriter) { _, _ = w.Write([]byte(paezaoLinked)) }, "GitHub: paezao"},
		{"not linked", notLinked, "GitHub: not linked (shpyrd github link)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/me":
					_, _ = w.Write([]byte(`{"email":"ana@acme.test"}`))
				case "/api/me/github":
					c.github(w)
				default:
					t.Errorf("unexpected %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("KUBECONFIG", "")
			t.Setenv("SHPYRD_URL", srv.URL)
			t.Setenv("SHPYRD_TOKEN", "")
			norm, err := normaliseURL(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			s := &loginSessions{Current: norm, Sessions: map[string]*loginSession{norm: {URL: norm, Token: "shp_test", SavedAt: time.Now()}}}
			if err := s.save(); err != nil {
				t.Fatal(err)
			}
			root := New()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs([]string{"whoami"})
			if err := root.Execute(); err != nil {
				t.Fatalf("%v\n%s", err, out.String())
			}
			if !strings.Contains(out.String(), "ana@acme.test") || !strings.Contains(out.String(), c.want) {
				t.Errorf("output:\n%s", out.String())
			}
		})
	}
}

const adminRefusal = "The admin token is not a person: sign in as yourself (shpyrd login) to link a GitHub account."

func adminHandler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/me":
			_, _ = w.Write([]byte(`{"subject":"admin-token","provider":"token"}`))
		case "/api/me/github":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"no GitHub account is linked: the admin token is no person"}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}
}

func TestGitHubLinkAndUnlinkRefuseTheAdminToken(t *testing.T) {
	opened := fastGitHubPolls(t)
	githubConnectWait = time.Hour
	for _, sub := range []string{"link", "unlink"} {
		_, err := runShpyrd(t, adminHandler(t), "github", sub)
		if err == nil || err.Error() != adminRefusal {
			t.Errorf("%s: err = %v", sub, err)
		}
	}
	if len(*opened) != 0 {
		t.Errorf("opened %v", *opened)
	}
}

func TestAFailedWhoAmICallIsNotTakenForTheAdminToken(t *testing.T) {
	opened := fastGitHubPolls(t)
	githubConnectWait = time.Hour
	for _, sub := range []string{"link", "unlink"} {
		_, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/me":
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"the database is down"}`))
			case "/api/me/github":
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"no GitHub account is linked: shpyrd github link"}`))
			default:
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			}
		}, "github", sub)
		if err == nil || err.Error() == adminRefusal || !strings.Contains(err.Error(), "the database is down") {
			t.Errorf("%s: err = %v", sub, err)
		}
	}
	if len(*opened) != 0 {
		t.Errorf("opened %v", *opened)
	}
}

func whoAmIWith(t *testing.T, handler http.HandlerFunc, args ...string) string {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("SHPYRD_URL", srv.URL)
	t.Setenv("SHPYRD_TOKEN", "")
	norm, _ := normaliseURL(srv.URL)
	s := &loginSessions{Current: norm, Sessions: map[string]*loginSession{norm: {URL: norm, Token: "shp_test", SavedAt: time.Now()}}}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	root := New()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"whoami"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	return out.String()
}

func TestWhoAmIPrintsNoGitHubLineForTheAdminTokenOrAnUnknownAnswer(t *testing.T) {
	admin := whoAmIWith(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/me/github" {
			t.Errorf("the admin token has no GitHub to ask for")
		}
		w.WriteHeader(http.StatusNotFound)
	}, "--json")
	if !strings.Contains(admin, "admin token") || strings.Contains(admin, "github") {
		t.Errorf("admin json:\n%s", admin)
	}
	plain := whoAmIWith(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/me" {
			_, _ = w.Write([]byte(`{"email":"ana@acme.test"}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	if !strings.Contains(plain, "ana@acme.test") || strings.Contains(plain, "GitHub") {
		t.Errorf("unknown answer:\n%s", plain)
	}
}

func TestDeployWithoutALinkSaysToRunGitHubLink(t *testing.T) {
	_, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"Link your GitHub account first","code":"github_link_required"}`))
	}, "deploy", "--git", "https://github.com/acme/vault", "--ref", "main", "--project", "vault")
	if err == nil || !strings.Contains(err.Error(), "link your GitHub account first: run shpyrd github link") {
		t.Errorf("err = %v", err)
	}
}
