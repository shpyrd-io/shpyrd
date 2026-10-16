package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func runGitAuth(t *testing.T, handler http.HandlerFunc, stdin string, args ...string) (string, error) {
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
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(append([]string{"git-auth"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestGitAuthSetReadsTheTokenFromStdin(t *testing.T) {
	var body map[string]string
	out, err := runGitAuth(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/api/workspace/git-auth/gitlab.example.com" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"host":"gitlab.example.com","username":"oauth2","updatedAt":"2026-10-16T10:00:00Z"}`))
	}, "glpat-s3cret\n", "set", "--host", "gitlab.example.com", "--token", "-")
	if err != nil {
		t.Fatal(err)
	}
	if body["token"] != "glpat-s3cret" {
		t.Errorf("token sent = %q", body["token"])
	}
	if strings.Contains(out, "s3cret") || !strings.Contains(out, "gitlab.example.com") {
		t.Errorf("output: %s", out)
	}
}

func TestGitAuthDeployKeyPrintsThePublicHalf(t *testing.T) {
	out, err := runGitAuth(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/projects/shop/git-auth/deploy-key" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"host":"github.com","publicKey":"ssh-ed25519 AAAAkey shpyrd shop","fingerprint":"SHA256:k","hostKeyFingerprint":"SHA256:h"}`))
	}, "", "deploy-key", "--project", "shop")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ssh-ed25519 AAAAkey shpyrd shop", "read-only", "SHA256:h"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestGitAuthShow(t *testing.T) {
	out, err := runGitAuth(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"url":"https://github.com/acme/shop","using":"token","token":{"host":"github.com","username":"x-access-token","updatedAt":"2026-10-16T10:00:00Z"}}`))
	}, "", "show", "--project", "shop")
	if err != nil || !strings.Contains(out, "token for github.com") {
		t.Errorf("%v: %s", err, out)
	}
}
