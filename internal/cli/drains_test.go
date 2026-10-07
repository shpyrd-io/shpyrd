package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// #16: adding a drain through the API says so when the workspace has no
// logs agent to forward its lines, as it does through a kubeconfig.
func TestDrainsAddWithoutAgent(t *testing.T) {
	for _, tc := range []struct {
		extensions []string
		note       bool
	}{
		{[]string{"license", "costs"}, true},
		{[]string{"logs-agent", "license"}, false},
	} {
		var created bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "POST" && r.URL.Path == "/api/projects/shop/drains":
				created = true
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{}`))
			case r.Method == "GET" && r.URL.Path == "/api/config":
				_ = json.NewEncoder(w).Encode(map[string]any{"extensions": tc.extensions})
			default:
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				http.NotFound(w, r)
			}
		}))
		t.Setenv("HOME", t.TempDir())
		t.Setenv("KUBECONFIG", "")
		t.Setenv("SHPYRD_URL", srv.URL)
		t.Setenv("SHPYRD_TOKEN", "test-token")
		root := New()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{"drains", "add", "https://logs.example.com/ingest", "--project", "shop"})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		srv.Close()
		if !created {
			t.Fatal("the drain was not created")
		}
		if got := strings.Contains(out.String(), "shpyrd extensions enable logs-agent"); got != tc.note {
			t.Errorf("extensions %v: note=%v, want %v:\n%s", tc.extensions, got, tc.note, out.String())
		}
	}
}
