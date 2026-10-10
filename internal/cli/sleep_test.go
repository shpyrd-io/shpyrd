package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// #135: `shpyrd sleep <project> --after off` and `--after default` are two
// different requests, and the command says which one it made: off keeps
// the app awake whatever the workspace's default, default hands the
// project back to it.
func TestSleepOffAndDefaultAreSaidApart(t *testing.T) {
	for _, tc := range []struct {
		after, sent, text string
	}{
		{"off", "off", "Sleep is off for shop: it stays awake, also when the workspace has a default."},
		{"default", "default", "Sleep for shop follows the workspace's default."},
		{"15m", "15m", "Sleep set: shop will scale to zero after 15m of inactivity (wait mode)."},
	} {
		var sent map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "POST" && r.URL.Path == "/api/projects/shop/processes":
				if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
					t.Error(err)
				}
				_, _ = w.Write([]byte(`{"slug":"shop"}`))
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
		root.SetArgs([]string{"sleep", "shop", "--after", tc.after})
		if err := root.Execute(); err != nil {
			t.Fatalf("--after %s: %v", tc.after, err)
		}
		srv.Close()
		web, _ := sent["processes"].(map[string]any)["web"].(map[string]any)
		sleep, _ := web["sleep"].(map[string]any)
		if sleep["after"] != tc.sent {
			t.Errorf("--after %s sent after=%v, want %q", tc.after, sleep["after"], tc.sent)
		}
		if !strings.Contains(out.String(), tc.text) {
			t.Errorf("--after %s printed:\n%s\nwant %q", tc.after, out.String(), tc.text)
		}
	}
}
