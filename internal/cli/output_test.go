package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// --json prints one JSON document on stdout and nothing else; --jq filters
// it; without either, the command prints its text. The workspace API is a
// fake reached through SHPYRD_URL and SHPYRD_TOKEN, as CI does.
func TestJSONAndJQOutput(t *testing.T) {
	var received map[string]string // what the last PUT of secrets carried
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/projects":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"slug":"shop","displayName":"Shop","phase":"Running","release":3,"url":"https://shop.example.com","createdAt":"2026-09-01T10:00:00Z"},{"slug":"api","displayName":"API","phase":"Running","release":1,"url":"https://api.example.com","createdAt":"2026-09-02T10:00:00Z"}]`))
		case "/api/projects/shop/secrets":
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodPut {
				// Echo the names that were set, sorted, as the server would.
				var upd struct {
					Set map[string]string `json:"set"`
				}
				_ = json.NewDecoder(r.Body).Decode(&upd)
				received = upd.Set
				names := make([]string, 0, len(upd.Set))
				for k := range upd.Set {
					names = append(names, k)
				}
				sort.Strings(names)
				vars := make([]map[string]string, 0, len(names))
				for _, n := range names {
					vars = append(vars, map[string]string{"name": n})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"vars": vars})
				return
			}
			_, _ = w.Write([]byte(`{"vars":[{"name":"DATABASE_URL","updatedAt":"2026-09-01T10:00:00Z"}]}`))
		case "/api/projects/shop":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"slug":"shop","spec":{},"status":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "") // the root flag defaults to it; set, it would name a cluster
	t.Setenv("SHPYRD_URL", srv.URL)
	t.Setenv("SHPYRD_TOKEN", "test-token")
	defer func(prev bool) { preferKubeconfig = prev }(preferKubeconfig)

	run := func(args ...string) (string, string) {
		var out, errOut bytes.Buffer
		root := New()
		root.SetOut(&out)
		root.SetErr(&errOut)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, errOut.String())
		}
		return out.String(), errOut.String()
	}

	text, _ := run("projects", "list")
	if !strings.HasPrefix(text, "PROJECT") || !strings.Contains(text, "shop") {
		t.Fatalf("text form:\n%s", text)
	}

	stdout, stderr := run("projects", "list", "--json")
	var items []map[string]any
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("--json is not a JSON document: %v\n%s", err, stdout)
	}
	if len(items) != 2 || items[0]["slug"] != "api" || items[1]["slug"] != "shop" {
		t.Fatalf("--json items: %v", items)
	}
	if stderr != "" {
		t.Fatalf("--json wrote to stderr: %q", stderr)
	}

	stdout, _ = run("projects", "list", "--jq", ".[].slug")
	if stdout != "api\nshop\n" {
		t.Fatalf("--jq strings print raw: %q", stdout)
	}

	stdout, _ = run("secrets", "list", "--project", "shop", "--jq", "{names: [.vars[].name]}")
	if stdout != "{\"names\":[\"DATABASE_URL\"]}\n" {
		t.Fatalf("--jq objects print as compact JSON: %q", stdout)
	}

	// --from-file: a dotenv file plus an argument that overrides one of
	// its lines; the fake echoes the values it was sent.
	env := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(env, []byte("A=from-file\nB=\"from file\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, _ = run("secrets", "set", "B=arg", "--from-file", env, "--project", "shop", "--jq", ".vars[].name")
	if stdout != "A\nB\n" {
		t.Fatalf("--from-file names: %q", stdout)
	}
	if received["A"] != "from-file" || received["B"] != "arg" {
		t.Fatalf("--from-file merged with arguments: the server received %v", received)
	}

	var out bytes.Buffer
	root := New()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"projects", "list", "--jq", ".["})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "--jq") {
		t.Fatalf("a bad expression must fail with a --jq error, got %v", err)
	}
}
