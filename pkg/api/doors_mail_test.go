package api

import (
	"context"
	"encoding/json"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInvitationIsOneEmailWithAnIdPSignIn(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	s.authz.TTL = 1
	ctx := context.Background()
	owner, err := s.store.TouchIdentity(ctx, store.DefaultWorkspace, store.Identity{Email: "owner@example.test", Name: "Olive Owner", Provider: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.PutMembership(ctx, store.DefaultWorkspace, owner.ID, store.WorkspaceRoleOwner); err != nil {
		t.Fatal(err)
	}
	sid, csrf := signIn(t, s, ext.Identity{Subject: owner.ID, Email: owner.Email, Name: owner.Name, Provider: "local"})
	mail := &fakeMailer{configured: true}
	s.mailer = mail
	rec := doCookie(t, s, "POST", "/api/workspace/invitations", `{"email":"new@example.test","role":"member"}`, sid, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite: %d %s", rec.Code, rec.Body)
	}
	var res InviteResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(mail.sent) != 1 || !strings.Contains(mail.sent[0].Text, res.Link) || !strings.Contains(mail.sent[0].HTML, "Accept invitation") {
		t.Fatal("invitation must send exactly one message with the acceptance link")
	}
	if strings.Contains(mail.sent[0].HTML, "password") || strings.Contains(mail.sent[0].Text, "set-password") {
		t.Fatal("invitation tried to manage credentials")
	}
	if _, err := s.store.GetIdentity(ctx, store.DefaultWorkspace, "new@example.test"); err == nil {
		t.Fatal("inviting an email created an authenticated user")
	}
}

func TestRetiredAccountEndpointsNeverAuthenticate(t *testing.T) {
	s, _, _ := newTenantServer(t)
	for _, host := range []string{"shpyrd.example.test", "acme.shpyrd.test"} {
		for _, path := range []string{"/account/reset", "/account/set-password", "/api/auth/reset", "/api/auth/password", "/api/auth/signup-ticket?ticket=old&next=/"} {
			for _, method := range []string{"GET", "POST"} {
				rec := at(t, s, host, method, path, "")
				if rec.Code != http.StatusNotFound || cookieValue(rec, sessionCookie) != "" || rec.Header().Get("Location") != "" {
					t.Errorf("retired auth endpoint %s %s: %d", host, path, rec.Code)
				}
			}
		}
	}
}

func TestMeNamesTheWorkspaceByItsID(t *testing.T) {
	s, _, st := newTenantServer(t)
	ctx := context.Background()
	sess, err := principalFixtureSession(s, ctx, store.RealmWorkspace, "acme", ext.Identity{Email: "new@person.test", Provider: "local"}, "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "https://acme.shpyrd.test/api/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess.ID})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var body struct {
		WorkspaceID string `json:"workspaceId"`
		Subject     string `json:"subject"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	ws, _ := st.Workspace(ctx, "acme")
	if rec.Code != 200 || body.WorkspaceID != ws.ID || body.Subject != sess.Identity.Subject {
		t.Fatalf("me: %d %s", rec.Code, rec.Body)
	}
}
