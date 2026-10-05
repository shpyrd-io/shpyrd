package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// fakeAccounts is a local-account store (RFC-0014) that remembers what
// was asked of it; accounts in status have one, the rest have none.
type fakeAccounts struct {
	pending []string
	status  map[string]string
}

func (f *fakeAccounts) IsLocked(context.Context, string) (bool, error)           { return false, nil }
func (f *fakeAccounts) Lock(context.Context, string, time.Time) error            { return nil }
func (f *fakeAccounts) MarkVerified(context.Context, string) error               { return nil }
func (f *fakeAccounts) ActivateFromInvite(context.Context, string, string) error { return nil }
func (f *fakeAccounts) SetPasswordAndVerify(context.Context, string, string) error {
	return nil
}
func (f *fakeAccounts) CreatePending(_ context.Context, email, _ string) error {
	f.pending = append(f.pending, email)
	if f.status == nil {
		f.status = map[string]string{}
	}
	f.status[email] = ext.AccountPending
	return nil
}
func (f *fakeAccounts) Status(_ context.Context, email string) (string, error) {
	return f.status[email], nil
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

// An invited person receives one email. Without a password, its call to
// action is to choose one (RFC-0014), the invitation link (RFC-0033)
// being the other way in; with a password, it is the invitation. Both
// links stand on the workspace's own door.
func TestInvitationIsOneEmailWithBothWaysIn(t *testing.T) {
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
	var res InviteResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	door := s.dashboardURLOf(nil)
	if !strings.HasPrefix(res.Link, door+"/invite/") || !strings.HasPrefix(res.SetPasswordLink, door+"/account/set-password?token=") {
		t.Fatalf("links off the workspace's door %s: %+v", door, res)
	}
	if len(mail.sent) != 1 {
		t.Fatalf("one email, got %d: %+v", len(mail.sent), mail.sent)
	}
	m := mail.sent[0]
	if m.To[0] != "new@example.test" || !strings.Contains(m.Text, res.SetPasswordLink) || !strings.Contains(m.Text, res.Link) || !strings.Contains(m.HTML, "Choose a password") || !strings.Contains(m.Subject, "Olive Owner invited you to") {
		t.Errorf("mail = %+v", m)
	}

	// Someone with a password already is not given a new one: the one
	// email is the invitation, and their account is left alone.
	accounts.status["known@example.test"] = ext.AccountActive
	rec = doCookie(t, s, "POST", "/api/workspace/invitations", `{"email":"known@example.test","role":"member"}`, sid, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite known: %d %s", rec.Code, rec.Body.String())
	}
	var known InviteResult // fresh: json leaves absent fields as they were
	_ = json.Unmarshal(rec.Body.Bytes(), &known)
	if known.SetPasswordLink != "" || len(accounts.pending) != 1 || len(mail.sent) != 2 || !strings.Contains(mail.sent[1].HTML, "Accept invitation") || strings.Contains(mail.sent[1].Text, "set-password") {
		t.Errorf("known person: %+v mails=%d pending=%v", known, len(mail.sent), accounts.pending)
	}
}

// The password a person chose at signup is set on the local sign-in at
// once, with no set-password step: a new person is created and activated;
// one who has a password keeps it; without local sign-in there is nothing
// to set.
func TestSignupPasswordActivatesTheAccount(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	accounts := &fakeAccounts{status: map[string]string{"has@person.test": ext.AccountActive}}
	s.localAccounts = accounts
	ctx := context.Background()
	if err := s.setSignupPasswordHook(ctx, " New@Person.test ", "new", "correct horse"); err != nil {
		t.Fatalf("new person: %v", err)
	}
	if len(accounts.pending) != 1 || accounts.pending[0] != "new@person.test" {
		t.Errorf("the account is created first: %v", accounts.pending)
	}
	if err := s.setSignupPasswordHook(ctx, "has@person.test", "has", "another one"); !errors.Is(err, ext.ErrAccountHasPassword) {
		t.Errorf("a person with a password: %v", err)
	}
	s.localAccounts = nil
	if err := s.setSignupPasswordHook(ctx, "x@person.test", "x", "correct horse"); err == nil {
		t.Error("without local sign-in the password cannot be set")
	}
}

// A sign-in ticket opens a session at the door it was minted for, once:
// the signup hands it to the person it just made a workspace for, so no
// login form follows. Used again, or at another door, it is refused.
func TestSignInTicketOpensTheDoorOnce(t *testing.T) {
	s, _, _ := newTenantServer(t)
	ctx := context.Background()
	link, err := s.signInTicketHook(ctx, "acme", "New@Person.test")
	if err != nil || !strings.Contains(link, "https://acme.shpyrd.test/api/auth/signup-ticket?code=") {
		t.Fatalf("ticket = %q %v", link, err)
	}
	redeem := func(host string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", link, nil)
		req.Host = host
		req.Header.Set("X-Forwarded-Proto", "https")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	// At another door: refused, and the code is spent (taken once).
	rec := redeem("example.test")
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "another+workspace") {
		t.Fatalf("another door: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	// A fresh ticket at its own door: a session, and the person recorded.
	link, _ = s.signInTicketHook(ctx, "acme", "new@person.test")
	rec = redeem("acme.shpyrd.test")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("own door: %d %s %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Error("no session cookie set")
	}
	if person, err := s.store.GetIdentity(ctx, "acme", "new@person.test"); err != nil || person == nil {
		t.Errorf("the person is recorded by the sign-in: %+v %v", person, err)
	}
	// Once only.
	rec = redeem("acme.shpyrd.test")
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "login_error=") {
		t.Fatalf("second use: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

// The rest of a sign-in ticket's link goes on past the redirect, which no
// page sees: the device id the signup adds and Google's linker reach the
// launcher, where the analytics read them, as they came; the code stays
// behind. Refused, the link still carries them on to the login page,
// whose reason the link cannot replace.
func TestSignInTicketCarriesTheRestOfTheLink(t *testing.T) {
	s, _, _ := newTenantServer(t)
	ctx := context.Background()
	const rest = "&ampDeviceId=dev-1&_gl=1*x9k2*_ga*MTIzLjQ1Ng.."
	redeem := func(link string) (int, string) {
		req := httptest.NewRequest("GET", link, nil)
		req.Host = "acme.shpyrd.test"
		req.Header.Set("X-Forwarded-Proto", "https")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code, rec.Header().Get("Location")
	}
	link, err := s.signInTicketHook(ctx, "acme", "new@person.test")
	if err != nil {
		t.Fatal(err)
	}
	code, at := redeem(link + rest)
	if code != http.StatusFound || at != "/?ampDeviceId=dev-1&_gl=1*x9k2*_ga*MTIzLjQ1Ng.." {
		t.Fatalf("own door: %d %s", code, at)
	}
	code, at = redeem(link + rest + "&login_error=forged")
	if code != http.StatusFound || !strings.HasPrefix(at, "/?login_error=this+sign-in+link") || !strings.HasSuffix(at, rest) || strings.Contains(at, "code=") || strings.Contains(at, "forged") {
		t.Fatalf("second use: %d %s", code, at)
	}
}
