package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// consoleAs sends a request to the console host with a console session of
// email, or with the admin token when email is "".
func consoleAs(t *testing.T, s *Server, email, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "https://shpyrd.example.test"+path, strings.NewReader(body))
	req.Host = "shpyrd.example.test"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if email == "" {
		req.Header.Set("Authorization", "Bearer "+testToken)
	} else {
		sess, err := s.rp.sessions.create(context.Background(), store.RealmConsole, "", ext.Identity{Email: email, Provider: "local"}, "")
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess.ID})
		req.Header.Set(csrfHeader, sess.CSRF)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// The console has its own users: a workspace's owner is nobody there until
// put on the list, and then the console is theirs.
func TestTheConsoleHasItsOwnUsers(t *testing.T) {
	s, _, st := newTenantServer(t)
	ctx := context.Background()
	// Roles exist in the default workspace, so the cluster is past bootstrap.
	if _, err := st.PutMembership(ctx, store.DefaultWorkspace, "owner@example.test", store.WorkspaceRoleOwner); err != nil {
		t.Fatal(err)
	}
	s.authz.Invalidate()

	if rec := consoleAs(t, s, "owner@example.test", "GET", "/api/cluster/console-users", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("a workspace owner, not a console user = %d %s", rec.Code, rec.Body)
	}
	// The operator adds her with the admin token (the kubeconfig does the same).
	if rec := consoleAs(t, s, "", "POST", "/api/cluster/console-users", `{"email":"Owner@Example.test"}`); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d %s", rec.Code, rec.Body)
	}
	rec := consoleAs(t, s, "owner@example.test", "GET", "/api/cluster/console-users", "")
	var list []ConsoleUserView
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list) != 1 || list[0].Email != "owner@example.test" || list[0].AddedBy != "admin-token" {
		t.Fatalf("list as a console user = %d %s", rec.Code, rec.Body)
	}
	// She may not take herself off; the operator may.
	if rec := consoleAs(t, s, "owner@example.test", "DELETE", "/api/cluster/console-users/owner@example.test", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("remove herself = %d %s", rec.Code, rec.Body)
	}
	if rec := consoleAs(t, s, "", "DELETE", "/api/cluster/console-users/owner@example.test", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("remove = %d %s", rec.Code, rec.Body)
	}
	if rec := consoleAs(t, s, "owner@example.test", "GET", "/api/cluster/console-users", ""); rec.Code != http.StatusForbidden {
		t.Errorf("after removal = %d", rec.Code)
	}
	if rec := consoleAs(t, s, "", "DELETE", "/api/cluster/console-users/owner@example.test", ""); rec.Code != http.StatusNotFound {
		t.Errorf("remove twice = %d", rec.Code)
	}
	if rec := consoleAs(t, s, "", "POST", "/api/cluster/console-users", `{"email":"not an email"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad email = %d", rec.Code)
	}
}

// A fresh cluster is in bootstrap mode, as before: whoever signs in at the
// console is its admin, and the first to add someone is put on the list too,
// so the change does not lock them out.
func TestTheFirstConsoleUserEndsBootstrapWithoutLockingOut(t *testing.T) {
	s, _, _ := newTenantServer(t)
	if rec := consoleAs(t, s, "first@example.test", "GET", "/api/cluster/console-users", ""); rec.Code != http.StatusOK {
		t.Fatalf("bootstrap = %d %s", rec.Code, rec.Body)
	}
	if rec := consoleAs(t, s, "first@example.test", "POST", "/api/cluster/console-users", `{"email":"second@example.test"}`); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d %s", rec.Code, rec.Body)
	}
	s.authz.Invalidate()
	for _, email := range []string{"first@example.test", "second@example.test"} {
		if rec := consoleAs(t, s, email, "GET", "/api/cluster/console-users", ""); rec.Code != http.StatusOK {
			t.Errorf("%s after bootstrap = %d", email, rec.Code)
		}
	}
	if rec := consoleAs(t, s, "third@example.test", "GET", "/api/cluster/console-users", ""); rec.Code != http.StatusForbidden {
		t.Errorf("someone else after bootstrap = %d", rec.Code)
	}
}

// The default workspace is no longer a setting.
func TestTheDefaultWorkspaceIsNoLongerASetting(t *testing.T) {
	s, _, _ := newTenantServer(t)
	if rec := consoleAs(t, s, "", "PATCH", "/api/cluster/settings", `{"defaultWorkspaceId":"acme"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("set the default workspace = %d %s", rec.Code, rec.Body)
	}
}
