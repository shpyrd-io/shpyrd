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

// #120: `shpyrd sleep <project>` with no flag reads the policy back: what
// applies, where it comes from, and the state.
func TestSleepReadsThePolicyBack(t *testing.T) {
	for _, tc := range []struct {
		sleep string
		text  []string
	}{
		{`{"state":"awake","after":"15m","resuming":"page","source":"project"}`, []string{"shop sleeps after 15m of inactivity (page mode), by a policy of its own."}},
		{`{"state":"sleeping","after":"15m","resuming":"wait","source":"workspace"}`, []string{"shop follows the workspace's default: it sleeps after 15m of inactivity (wait mode).", "Asleep now; the first request wakes it."}},
		{`{"state":"awake","source":"workspace"}`, []string{"shop follows the workspace's default, which sets no sleep: it stays awake."}},
		{`{"state":"awake","source":"off"}`, []string{"Sleep is off for shop: it stays awake, also when the workspace has a default."}},
		{`{"state":"unavailable","after":"15m","source":"project","message":"sleep needs the sleep extension"}`, []string{"Not sleeping now: sleep needs the sleep extension"}},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && r.URL.Path == "/api/projects/shop" {
				_, _ = w.Write([]byte(`{"slug":"shop","processes":{"web":{"desired":1,"ready":1,"sleep":` + tc.sleep + `}}}`))
				return
			}
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}))
		t.Setenv("HOME", t.TempDir())
		t.Setenv("KUBECONFIG", "")
		t.Setenv("SHPYRD_URL", srv.URL)
		t.Setenv("SHPYRD_TOKEN", "test-token")
		root := New()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{"sleep", "shop"})
		if err := root.Execute(); err != nil {
			t.Fatalf("%s: %v", tc.sleep, err)
		}
		srv.Close()
		for _, want := range tc.text {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%s printed:\n%s\nwant %q", tc.sleep, out.String(), want)
			}
		}
		if !strings.Contains(out.String(), "Change it: shpyrd sleep shop --after 15m|off|default") {
			t.Errorf("no hint on how to change it:\n%s", out.String())
		}
	}
}
