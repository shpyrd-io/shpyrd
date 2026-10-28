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

func runSignedIn(t *testing.T, handler http.HandlerFunc, stdin string, args ...string) (string, error) {
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
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

const servingWorkspace = `{"slug":"acme","name":"Acme","url":"https://acme.shpyrd.test","joinPolicy":"open","owners":[],"mcpName":"Acme","mcpUrl":"https://acme.shpyrd.test/mcp","ui":{"ref":"oci://ghcr.io/acme/workspace-ui@sha256:abc","setBy":"ana@acme.test","status":{"state":"serving","name":"Acme","version":"2.3.1"}}}`

func TestWorkspaceUISetSendsTheReferenceAndSaysHowItIsDoing(t *testing.T) {
	var body map[string]string
	out, err := runSignedIn(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" || r.URL.Path != "/api/workspace" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(servingWorkspace))
	}, "", "workspace", "ui", "set", "oci://ghcr.io/acme/workspace-ui:2.3.1")
	if err != nil {
		t.Fatal(err)
	}
	if body["ui"] != "oci://ghcr.io/acme/workspace-ui:2.3.1" {
		t.Errorf("sent %q", body["ui"])
	}
	for _, want := range []string{"REFERENCE", "@sha256:abc", "STATUS", "serving", "PACKAGE", "Acme 2.3.1", "ana@acme.test"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestWorkspaceUIClearAndReloadCallTheirRoutes(t *testing.T) {
	var calls []string
	handler := func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(raw))
		_, _ = w.Write([]byte(`{"slug":"acme","name":"Acme","joinPolicy":"open","owners":[]}`))
	}
	out, err := runSignedIn(t, handler, "", "workspace", "ui", "clear")
	if err != nil || !strings.Contains(out, "platform's own") {
		t.Errorf("clear: %v %s", err, out)
	}
	if _, err := runSignedIn(t, handler, "", "workspace", "ui", "reload"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != `PATCH /api/workspace {"ui":""}` || calls[1] != "POST /api/workspace/ui/reload " {
		t.Errorf("calls = %q", calls)
	}
}

func TestWorkspaceUIShowsThePlatformsOwnWhenNothingIsChosen(t *testing.T) {
	out, err := runSignedIn(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"slug":"acme","name":"Acme","joinPolicy":"open","owners":[]}`))
	}, "", "workspace", "ui")
	if err != nil || !strings.Contains(out, "platform's own application") {
		t.Errorf("%v %s", err, out)
	}
}

func TestRegistryAuthSetReadsTheTokenFromStdinAndNeverPrintsIt(t *testing.T) {
	var body map[string]string
	out, err := runSignedIn(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/api/workspace/registry-auth/ghcr.io" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"host":"ghcr.io","username":"acme-bot","updatedAt":"2026-10-25T10:00:00Z"}`))
	}, "ghp_s3cret\n", "registry-auth", "set", "--host", "ghcr.io", "--username", "acme-bot", "--token", "-")
	if err != nil {
		t.Fatal(err)
	}
	if body["token"] != "ghp_s3cret" || body["username"] != "acme-bot" {
		t.Errorf("sent %v", body)
	}
	if strings.Contains(out, "s3cret") || !strings.Contains(out, "ghcr.io") {
		t.Errorf("output: %s", out)
	}
	if _, err := runSignedIn(t, func(w http.ResponseWriter, r *http.Request) {}, "", "registry-auth", "set", "--host", "ghcr.io"); err == nil {
		t.Error("set without a user and a token passed")
	}
}

func TestRegistryAuthListAndUnset(t *testing.T) {
	out, err := runSignedIn(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"host":"ghcr.io","username":"acme-bot","updatedBy":"ana@acme.test","updatedAt":"2026-10-25T10:00:00Z"}]`))
	}, "", "registry-auth", "list")
	if err != nil || !strings.Contains(out, "ghcr.io") || !strings.Contains(out, "acme-bot") || !strings.Contains(out, "ana@acme.test") {
		t.Errorf("list: %v %s", err, out)
	}
	var method, path string
	out, err = runSignedIn(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}, "", "registry-auth", "unset", "--host", "ghcr.io")
	if err != nil || method != "DELETE" || path != "/api/workspace/registry-auth/ghcr.io" || !strings.Contains(out, "removed") {
		t.Errorf("unset: %v %s %s %s", err, method, path, out)
	}
}
