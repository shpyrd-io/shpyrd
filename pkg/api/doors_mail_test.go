package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// fakeAccounts is a local-account store (RFC-0014) that remembers what
// was asked of it and has every account.
type fakeAccounts struct{ pending []string }

func (f *fakeAccounts) IsLocked(context.Context, string) (bool, error)           { return false, nil }
func (f *fakeAccounts) Lock(context.Context, string, time.Time) error            { return nil }
func (f *fakeAccounts) MarkVerified(context.Context, string) error               { return nil }
func (f *fakeAccounts) ActivateFromInvite(context.Context, string, string) error { return nil }
func (f *fakeAccounts) SetPasswordAndVerify(context.Context, string, string) error {
	return nil
}
func (f *fakeAccounts) CreatePending(_ context.Context, email, _ string) error {
	f.pending = append(f.pending, email)
	return nil
}

// The platform has several doors (RFC-0080). A person who asks for a
// password reset at acme.shpyrd.app is sent a link on acme.shpyrd.app,
// not on some "dashboard" host that may not answer; the links were once
// built on https://shpyrd.<domain>, which two doors did away with.
func TestAccountLinksStandOnTheDoorTheyWereAskedAt(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	mail := &fakeMailer{configured: true}
	s.mailer = mail
	s.localAccounts = &fakeAccounts{}

	req := httptest.NewRequest("POST", "https://acme.shpyrd.app/api/auth/reset", strings.NewReader(`{"email":"ana@acme.test"}`))
	req.Host = "acme.shpyrd.app"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body.String())
	}
	if len(mail.sent) != 1 || !strings.Contains(mail.sent[0].Text, "https://acme.shpyrd.app/account/reset?token=") {
		t.Fatalf("reset link must stand on the door asked at: %+v", mail.sent)
	}
}

// An invitation into a workspace carries two links, both on the
// workspace's own door: the invitation (RFC-0033) and, with auth-local,
// the set-password link of the pending account (RFC-0014).
func TestInvitationLinksStandOnTheWorkspaceDoor(t *testing.T) {
	issuer := newFakeIssuer(t)
	s, _ := newTestServer(t, nil, nil)
	s.authz.TTL = 1
	ctx := context.Background()
	if err := s.rp.AddOIDC(ctx, ext.OIDCProvider{ID: "local", Label: "Email and password", Issuer: issuer.srv.URL, ClientID: "shpyrd", ClientSecret: "sekret", Password: true}); err != nil {
		t.Fatal(err)
	}
	mail := &fakeMailer{configured: true}
	s.mailer = mail
	accounts := &fakeAccounts{}
	s.localAccounts = accounts
	if rec := do(t, s, "PATCH", "/api/workspace/people/owner@example.test", `{"role":"owner"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("owner: %d %s", rec.Code, rec.Body.String())
	}
	owner := ext.Identity{Subject: "o", Email: "owner@example.test", Name: "Olive Owner", Provider: "local"}
	sid, csrf := signIn(t, s, owner)

	rec := doCookie(t, s, "POST", "/api/workspace/invitations", `{"email":"new@example.test","role":"member"}`, sid, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite: %d %s", rec.Code, rec.Body.String())
	}
	if len(accounts.pending) != 1 || accounts.pending[0] != "new@example.test" {
		t.Fatalf("pending account = %v", accounts.pending)
	}
	if len(mail.sent) != 2 {
		t.Fatalf("want the invitation and the set-password mail, got %d: %+v", len(mail.sent), mail.sent)
	}
	door := s.dashboardURLOf(nil)
	for _, m := range mail.sent {
		if m.To[0] != "new@example.test" {
			t.Errorf("mail to %v", m.To)
		}
		if !strings.Contains(m.Text, door+"/invite/") && !strings.Contains(m.Text, door+"/account/set-password?token=") {
			t.Errorf("a link off the workspace's door %s: %q", door, m.Text)
		}
	}
	if !strings.Contains(mail.sent[1].Subject, "Set your password for") {
		t.Errorf("set-password subject: %q", mail.sent[1].Subject)
	}
}
