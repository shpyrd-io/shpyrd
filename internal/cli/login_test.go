package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBrowserLoginPollsUntilApproved(t *testing.T) {
	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/cli/device":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["name"] == "" {
				t.Error("the CLI names the machine")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code": "dev-1", "user_code": "BCDF-GHJK",
				"verification_uri":          serverURL(r) + "/cli/activate",
				"verification_uri_complete": serverURL(r) + "/cli/activate?code=BCDF-GHJK",
				"expires_in":                600, "interval": 1,
			})
		case "/api/cli/device/token":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["device_code"] != "dev-1" {
				t.Errorf("polled with %v", req)
			}
			switch polls.Add(1) {
			case 1:
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
			case 2:
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "slow_down"})
			default:
				_ = json.NewEncoder(w).Encode(map[string]any{"token": "shp_abc", "email": "ana@example.test", "expiresAt": time.Now().Add(time.Hour)})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := browserLogin(ctx, srv.URL, &out, true)
	if err != nil {
		t.Fatalf("browserLogin: %v\n%s", err, out.String())
	}
	if got.Token != "shp_abc" || got.Email != "ana@example.test" || got.ExpiresAt.IsZero() {
		t.Errorf("approval = %+v", got)
	}
	if !strings.Contains(out.String(), "Your code: BCDF-GHJK") || !strings.Contains(out.String(), "/cli/activate?code=BCDF-GHJK") || !strings.Contains(out.String(), "approved.") {
		t.Errorf("output:\n%s", out.String())
	}
	if polls.Load() < 3 {
		t.Errorf("polled %d times; pending and slow_down keep polling", polls.Load())
	}
}

func TestBrowserLoginReportsARefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/cli/device":
			_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "dev-1", "user_code": "BCDF-GHJK", "verification_uri": "http://x/cli/activate", "expires_in": 600, "interval": 1})
		case "/api/cli/device/token":
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "access_denied"})
		}
	}))
	defer srv.Close()
	var out bytes.Buffer
	if _, err := browserLogin(context.Background(), srv.URL, &out, true); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("err = %v", err)
	}
	// A workspace that has no browser sign-in says so, pointing at --token.
	old := httptest.NewServer(http.NotFoundHandler())
	defer old.Close()
	if _, err := browserLogin(context.Background(), old.URL, &out, true); err == nil || !strings.Contains(err.Error(), "--token") {
		t.Fatalf("old server: err = %v", err)
	}
}

func serverURL(r *http.Request) string { return "http://" + r.Host }

// `shpyrd login --signup` is the device flow against the signup rather than
// a workspace: the approval names the workspace the signup made, the CLI
// checks the credential there and keeps it as the current session.
func TestSignupLoginSignsInToTheNewWorkspace(t *testing.T) {
	var polls atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/signup/cli/device":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code": "dev-7", "user_code": "KLMN-PQRS",
				"verification_uri":          serverURL(r) + "/",
				"verification_uri_complete": serverURL(r) + "/?cli=KLMN-PQRS",
				"expires_in":                600, "interval": 1,
			})
		case "/api/signup/cli/device/token":
			if polls.Add(1) == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "shp_new", "email": "new@person.test", "expiresAt": time.Now().Add(time.Hour), "workspaceUrl": srv.URL})
		case "/api/me":
			// The new workspace: the credential works there.
			if r.Header.Get("Authorization") != "Bearer shp_new" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"email": "new@person.test"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())

	var out bytes.Buffer
	cmd := newLoginCmd(&globalFlags{})
	cmd.SetOut(&out)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := signupLogin(ctx, cmd, &globalFlags{}, srv.URL+"/", &out, true); err != nil {
		t.Fatalf("signupLogin: %v\n%s", err, out.String())
	}
	text := out.String()
	if !strings.Contains(text, "Your code: KLMN-PQRS") || !strings.Contains(text, "/?cli=KLMN-PQRS") || !strings.Contains(text, "is ready, and the CLI is signed in as new@person.test") {
		t.Errorf("output:\n%s", text)
	}
	s := loadSessions()
	norm, _ := normaliseURL(srv.URL)
	if s.Current != norm || s.Sessions[norm] == nil || s.Sessions[norm].Token != "shp_new" || s.Sessions[norm].WhoAmI != "new@person.test" {
		t.Errorf("sessions after signup = %+v (current %q)", s.Sessions, s.Current)
	}
}

// Away from a terminal, `shpyrd login` with no URL explains the two flags
// instead of asking.
func TestLoginWithoutURLExplainsAwayFromATerminal(t *testing.T) {
	cmd := newLoginCmd(&globalFlags{})
	cmd.SetArgs([]string{})
	cmd.SetIn(strings.NewReader(""))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--url is required") || !strings.Contains(err.Error(), "--signup") {
		t.Errorf("err = %v", err)
	}
}
