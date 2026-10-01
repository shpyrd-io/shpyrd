package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/authz"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// activate posts the approval form as the browser does: a session cookie
// and the fields, no CSRF header.
func activate(t *testing.T, s *Server, sid string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	for k, v := range fields {
		form.Set(k, v)
	}
	req := httptest.NewRequest("POST", "/cli/activate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func startDevice(t *testing.T, s *Server) (device, user string) {
	t.Helper()
	rec := do(t, s, "POST", "/api/cli/device", `{"name":"laptop"}`, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	var start struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
		URI        string `json:"verification_uri_complete"`
		Interval   int    `json:"interval"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &start)
	if start.DeviceCode == "" || len(start.UserCode) != 9 || start.UserCode[4] != '-' || start.Interval != cliDeviceInterval {
		t.Fatalf("start = %+v", start)
	}
	if !strings.Contains(start.URI, "/cli/activate?code="+start.UserCode) {
		t.Errorf("verification uri = %q", start.URI)
	}
	return start.DeviceCode, start.UserCode
}

func pollDevice(t *testing.T, s *Server, device string) (int, map[string]any) {
	t.Helper()
	rec := do(t, s, "POST", "/api/cli/device/token", `{"device_code":"`+device+`"}`, false)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestCLIBrowserSignIn(t *testing.T) {
	shop := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}}
	s, _ := newTestServer(t, nil, []client.Object{shop})
	s.authz.TTL = 1
	ctx := t.Context()
	if _, err := s.store.AddGrant(ctx, store.DefaultWorkspace, store.Grant{Project: "shop", Role: shpyrdv1.RoleDeveloper, User: "dev@example.test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.TouchIdentity(ctx, store.DefaultWorkspace, store.Identity{Email: "dev@example.test", Provider: "local"}); err != nil {
		t.Fatal(err)
	}
	s.authz.Invalidate()
	// A clock the test moves, so polls are never told to slow down by accident.
	now := time.Now()
	s.cliDevices.now = func() time.Time { return now }

	device, user := startDevice(t, s)

	// Nobody has approved yet.
	if code, body := pollDevice(t, s, device); code != http.StatusBadRequest || body["error"] != "authorization_pending" {
		t.Fatalf("pending poll = %d %v", code, body)
	}
	// Polling again at once is told to slow down.
	if code, body := pollDevice(t, s, device); code != http.StatusBadRequest || body["error"] != "slow_down" {
		t.Fatalf("fast poll = %d %v", code, body)
	}

	// The page needs a dashboard session: without one, the sign-in first.
	rec := do(t, s, "GET", "/cli/activate?code="+user, "", false)
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "next=%2Fcli%2Factivate%3Fcode%3D") {
		t.Fatalf("no session: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	sid, csrf := signIn(t, s, ext.Identity{Subject: "u1", Email: "dev@example.test", Provider: "local"})
	req := httptest.NewRequest("GET", "/cli/activate?code="+strings.ToLower(user), nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `value="`+user+`"`) || !strings.Contains(rec.Body.String(), "dev@example.test") {
		t.Fatalf("page: %d %s", rec.Code, rec.Body.String())
	}

	// A wrong code is sent back to the form; a wrong CSRF token is refused.
	if rec := activate(t, s, sid, map[string]string{"csrf": csrf, "code": "AAAA-BBBB", "decision": "allow"}); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "not waiting for approval") {
		t.Fatalf("wrong code: %d %s", rec.Code, rec.Body.String())
	}
	if rec := activate(t, s, sid, map[string]string{"csrf": "nope", "code": user, "decision": "allow"}); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong csrf: %d", rec.Code)
	}
	if rec := activate(t, s, "", map[string]string{"csrf": csrf, "code": user, "decision": "allow"}); rec.Code != http.StatusFound {
		t.Fatalf("no session on post: %d", rec.Code)
	}

	// Approved, typed with a space and in lower case.
	rec = activate(t, s, sid, map[string]string{"csrf": csrf, "code": strings.ToLower(strings.Replace(user, "-", " ", 1)), "decision": "allow"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "signed in") {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body.String())
	}
	now = now.Add(cliDeviceInterval * time.Second)
	code, body := pollDevice(t, s, device)
	if code != http.StatusOK || body["email"] != "dev@example.test" {
		t.Fatalf("approved poll = %d %v", code, body)
	}
	token, _ := body["token"].(string)
	if !IsAPIToken(token) {
		t.Fatalf("token = %q", token)
	}
	// Consumed: the code cannot be polled again.
	now = now.Add(cliDeviceInterval * time.Second)
	if code, body := pollDevice(t, s, device); code != http.StatusBadRequest || body["error"] != "expired_token" {
		t.Errorf("second poll = %d %v", code, body)
	}

	// The token is the person: their identity, their roles.
	req = httptest.NewRequest("GET", "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me = %d %s", rec.Code, rec.Body.String())
	}
	var me struct {
		ext.Identity
		Roles authz.Roles `json:"roles"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me.Email != "dev@example.test" || me.Provider != "local" || me.Roles.Projects["shop"] != shpyrdv1.RoleDeveloper {
		t.Errorf("me = %+v", me)
	}
	// And it can do what the person can: mint a token (an API token could not).
	req = httptest.NewRequest("POST", "/api/tokens", strings.NewReader(`{"name":"ci","projectRoles":{"shop":"viewer"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("a sign-in mints tokens like the person: %d %s", rec.Code, rec.Body.String())
	}
	// A grant made after the sign-in applies to it.
	if _, err := s.store.AddGrant(ctx, store.DefaultWorkspace, store.Grant{Project: "shop", Role: shpyrdv1.RoleAdmin, User: "dev@example.test"}); err != nil {
		t.Fatal(err)
	}
	s.authz.Invalidate()
	req = httptest.NewRequest("GET", "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me.Roles.Projects["shop"] != shpyrdv1.RoleAdmin {
		t.Errorf("roles follow the person: %+v", me.Roles)
	}

	// Listed as a sign-in, with an expiry; signing in again from the same
	// machine replaces it rather than adding one.
	tokens, _ := s.store.ListTokens(ctx, store.DefaultWorkspace, "dev@example.test")
	var sessions []store.APIToken
	for _, tk := range tokens {
		if tk.Kind == store.TokenKindSession {
			sessions = append(sessions, tk)
		}
	}
	if len(sessions) != 1 || sessions[0].Name != "CLI on laptop" || sessions[0].ExpiresAt == nil {
		t.Fatalf("sessions = %+v", sessions)
	}
	device2, user2 := startDevice(t, s)
	if rec := activate(t, s, sid, map[string]string{"csrf": csrf, "code": user2, "decision": "allow"}); rec.Code != http.StatusOK {
		t.Fatalf("approve again: %d", rec.Code)
	}
	now = now.Add(cliDeviceInterval * time.Second)
	if code, _ := pollDevice(t, s, device2); code != http.StatusOK {
		t.Fatalf("second approval poll = %d", code)
	}
	tokens, _ = s.store.ListTokens(ctx, store.DefaultWorkspace, "dev@example.test")
	sessions = sessions[:0]
	for _, tk := range tokens {
		if tk.Kind == store.TokenKindSession {
			sessions = append(sessions, tk)
		}
	}
	if len(sessions) != 1 {
		t.Errorf("one sign-in per machine: %+v", sessions)
	}
	// The first token is gone with it.
	req = httptest.NewRequest("GET", "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("the replaced sign-in still works: %d", rec.Code)
	}

	// Refused in the browser.
	device3, user3 := startDevice(t, s)
	if rec := activate(t, s, sid, map[string]string{"csrf": csrf, "code": user3, "decision": "deny"}); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "refused") {
		t.Fatalf("deny: %d %s", rec.Code, rec.Body.String())
	}
	now = now.Add(cliDeviceInterval * time.Second)
	if code, body := pollDevice(t, s, device3); code != http.StatusBadRequest || body["error"] != "access_denied" {
		t.Errorf("denied poll = %d %v", code, body)
	}

	// A code nobody approves expires.
	device4, _ := startDevice(t, s)
	now = now.Add(cliDeviceTTL + time.Second)
	if code, body := pollDevice(t, s, device4); code != http.StatusBadRequest || body["error"] != "expired_token" {
		t.Errorf("expired poll = %d %v", code, body)
	}
}

func TestNormaliseUserCode(t *testing.T) {
	for in, want := range map[string]string{
		"BCDF-GHJK":  "BCDF-GHJK",
		"bcdfghjk":   "BCDF-GHJK",
		" bcdf ghjk": "BCDF-GHJK",
		"BCDF":       "",
		"":           "",
	} {
		if got := normaliseUserCode(in); got != want {
			t.Errorf("normaliseUserCode(%q) = %q, want %q", in, got, want)
		}
	}
	for i := 0; i < 50; i++ {
		c := newUserCode()
		if normaliseUserCode(c) != c || strings.ContainsAny(c, "AEIOU01") {
			t.Fatalf("user code %q", c)
		}
	}
}
