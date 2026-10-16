package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// runShpyrd runs the CLI against handler, signed in.
func runShpyrd(t *testing.T, handler http.HandlerFunc, args ...string) (string, error) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("SHPYRD_URL", srv.URL)
	t.Setenv("SHPYRD_TOKEN", "test-token")
	root := New()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestGitHooksShowPrintsTheWebhookAndHowToAddIt(t *testing.T) {
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/projects/shop/git-hooks":
			_, _ = w.Write([]byte(`{"url":"https://acme.shpyrd.app/api/hooks/git/shop/t0k3n","secret":"s3cr3t"}`))
		case "GET /api/projects/shop":
			_, _ = w.Write([]byte(`{"slug":"shop","spec":{"source":{"git":{"url":"https://github.com/acme/shop","revision":"main","autoDeploy":false}}},"status":{"phase":"Running","releases":[]}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}, "git-hooks", "show", "--project", "shop")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"https://acme.shpyrd.app/api/hooks/git/shop/t0k3n", "s3cr3t",
		"GitHub", "application/json", "Just the push event", "GitLab", "Secret token", "Gitea", "Push events",
		"Auto-deploy is off", "--auto-deploy",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestGitHooksShowSaysAutoDeployIsOn(t *testing.T) {
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/git-hooks") {
			_, _ = w.Write([]byte(`{"url":"https://acme.shpyrd.app/api/hooks/git/shop/t0k3n","secret":"s3cr3t"}`))
			return
		}
		_, _ = w.Write([]byte(`{"slug":"shop","spec":{"source":{"git":{"url":"https://github.com/acme/shop"}}},"status":{"phase":"Running","releases":[]}}`))
	}, "git-hooks", "show", "--project", "shop")
	if err != nil || !strings.Contains(out, "Auto-deploy is on: every push to main deploys") {
		t.Errorf("%v\n%s", err, out)
	}
}

func TestGitHooksRotate(t *testing.T) {
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/projects/shop/git-hooks/rotate" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"url":"https://acme.shpyrd.app/api/hooks/git/shop/n3w","secret":"n3ws3cr3t"}`))
	}, "git-hooks", "rotate", "--project", "shop")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/shop/n3w", "n3ws3cr3t", "refused"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestGitDeploySource(t *testing.T) {
	src, err := gitDeploySource("https://github.com/acme/shop", "", false, false)
	if err != nil || src.Revision != "main" || src.AutoDeploy != nil {
		t.Errorf("no flag leaves the project's setting: %+v %v", src, err)
	}
	if src, _ := gitDeploySource("https://github.com/acme/shop", "dev", true, false); src.Revision != "dev" || src.AutoDeploy == nil || !*src.AutoDeploy {
		t.Errorf("--auto-deploy: %+v", src)
	}
	if src, _ := gitDeploySource("https://github.com/acme/shop", "", false, true); src.AutoDeploy == nil || *src.AutoDeploy {
		t.Errorf("--no-auto-deploy: %+v", src)
	}
	if _, err := gitDeploySource("https://github.com/acme/shop", "", true, true); err == nil {
		t.Error("both flags contradict each other")
	}
}

func TestAutoDeployFlagsGoWithGit(t *testing.T) {
	_, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("nothing is sent: %s %s", r.Method, r.URL.Path)
	}, "deploy", "--project", "shop", "--no-auto-deploy")
	if err == nil || !strings.Contains(err.Error(), "go with --git") {
		t.Errorf("err = %v", err)
	}
}
