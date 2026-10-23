package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAccountsLoginBothEntryFlowsAndExistingSessions(t *testing.T) {
	for _, mode := range []string{"signup-first", "cli-first", "signup-flag", "reuse", "expired-session"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("SHPYRD_URL", "")
			t.Setenv("SHPYRD_TOKEN", "")
			t.Setenv("SHPYRD_SIGNUP_URL", "")
			polls, devices, completed := 0, 0, false
			secret := strings.Repeat("a", 64)
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
				switch r.URL.Path {
				case "/api/cli/setup/start":
					reply(map[string]any{"secret": secret, "browserUrl": srv.URL + "/cli/setup?code=test", "expiresIn": 30, "interval": 1})
				case "/api/cli/setup/poll":
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["secret"] != secret {
						t.Error("missing private polling credential")
					}
					polls++
					if mode == "cli-first" && polls == 1 {
						reply(map[string]string{"status": "waiting"})
						return
					}
					if polls == 1 {
						reply(map[string]string{"status": "provisioning"})
						return
					}
					reply(map[string]any{"status": "ready", "workspace": map[string]string{"url": srv.URL, "name": "Acme"}})
				case "/api/cli/setup/complete":
					completed = true
					reply(map[string]string{"status": "completed"})
				case "/api/cli/device":
					devices++
					reply(map[string]any{"device_code": "device", "user_code": "ABCD-EFGH", "verification_uri_complete": srv.URL + "/cli/activate?code=ABCD-EFGH", "expires_in": 30, "interval": 1})
				case "/api/cli/device/token":
					reply(map[string]any{"token": "new-token", "email": "a@example.test", "expiresAt": time.Now().Add(time.Hour)})
				case "/api/me":
					if r.Header.Get("Authorization") == "Bearer expired" {
						w.WriteHeader(401)
						return
					}
					reply(map[string]string{"email": "a@example.test"})
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			store := &loginSessions{Current: "https://unrelated.example", Sessions: map[string]*loginSession{"https://unrelated.example": {URL: "https://unrelated.example", Token: "other-token"}}}
			if mode == "reuse" || mode == "expired-session" {
				token := "valid"
				if mode == "expired-session" {
					token = "expired"
				}
				store.Sessions[srv.URL] = &loginSession{URL: srv.URL, Token: token}
			}
			if err := store.save(); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			cmd := newLoginCmd(&globalFlags{})
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			args := []string{"--accounts-url", srv.URL, "--no-browser", "--progress-json"}
			if mode == "signup-flag" {
				args = append(args, "--signup")
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if !completed || loadSessions().Current != srv.URL {
				t.Fatal("did not complete the handoff and select the resolved workspace")
			}
			if mode == "reuse" && devices != 0 {
				t.Fatal("valid session should be reused")
			}
			if mode != "reuse" && devices != 1 {
				t.Fatal("wrong or expired sessions must not skip authorization")
			}
			if !strings.Contains(stderr.String(), `"event":"browser"`) {
				t.Fatal("missing structured browser event")
			}
			if strings.Contains(stderr.String(), secret) || strings.Contains(stderr.String(), "new-token") {
				t.Fatal("credential leaked in progress")
			}
			raw, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".shpyrd", "sessions.json"))
			if err != nil || !bytes.Contains(raw, []byte("other-token")) {
				t.Fatal("unrelated session was lost")
			}
		})
	}
}

func TestAccountsCancellationAndUntrustedResponses(t *testing.T) {
	for _, tc := range []string{"cancel", "cross-origin", "bad-workspace", "expired", "failed"} {
		t.Run(tc, func(t *testing.T) {
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "start") {
					link := srv.URL + "/cli/setup?code=test"
					if tc == "cross-origin" {
						link = "https://evil.example/cli/setup"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"secret": strings.Repeat("a", 64), "browserUrl": link, "interval": 1})
					return
				}
				if tc == "expired" {
					w.WriteHeader(410)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "expired"})
					return
				}
				state := "ready"
				if tc == "failed" {
					state = "failed"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"status": state, "workspace": map[string]string{"url": "http://evil.example"}})
			}))
			defer srv.Close()
			ctx := context.Background()
			if tc == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, err := accountsWorkspace(ctx, srv.URL, &bytes.Buffer{}, true); err == nil {
				t.Fatal("must reject or cancel")
			}
		})
	}
	for _, u := range []string{"https://user:password@example.com", "http://example.com", "file:///tmp/test", "https://example.com/path", "https://example.com?token=x"} {
		if _, err := loginEndpoint(u); err == nil {
			t.Fatalf("accepted %s", u)
		}
	}
}
