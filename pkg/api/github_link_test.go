package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tokenseal"
)

func TestTheServerMakesTheTokenKey(t *testing.T) {
	s, _, _ := githubServer(t)
	if s.githubBox == nil {
		t.Fatal("no token key")
	}
	sec, err := s.kube.Kube.CoreV1().Secrets("shpyrd-system").Get(context.Background(), tokenseal.SecretName, metav1.GetOptions{})
	if err != nil || len(sec.Data[tokenseal.KeyName]) != 32 {
		t.Fatalf("secret: err %v, key length %d, want 32", err, len(sec.Data[tokenseal.KeyName]))
	}
}

// signInPerson signs a person in, with a role in the default workspace
// (store.WorkspaceRoleMember or store.WorkspaceRoleAdmin), and returns
// their session, CSRF token and subject.
func signInPerson(t *testing.T, s *Server, email, role string) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	p, err := s.store.TouchIdentity(ctx, store.DefaultWorkspace, store.Identity{Email: email, Provider: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.PutMembership(ctx, store.DefaultWorkspace, p.ID, role); err != nil {
		t.Fatal(err)
	}
	s.authz.Invalidate()
	sid, csrf := signIn(t, s, ext.Identity{Subject: p.ID, Email: email, Provider: "local"})
	return sid, csrf, p.ID
}

// linkTicket starts a link as the person and plays GitHub's callback with
// code: the finish link the callback sends the browser to.
func linkTicket(t *testing.T, s *Server, sid, csrf, code string) string {
	t.Helper()
	rec := doCookie(t, s, "POST", "/api/me/github/link", `{"return":"/"}`, sid, csrf)
	var v GitHubLinkStartView
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &v) != nil {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	u, err := url.Parse(v.AuthorizeURL)
	if err != nil || u.Path != "/login/oauth/authorize" || u.Query().Get("client_id") != githubtest.ClientID || u.Query().Get("state") == "" {
		t.Fatalf("authorize URL %q", v.AuthorizeURL)
	}
	cb := do(t, s, "GET", "/api/github/installed?"+url.Values{"code": {code}, "state": {u.Query().Get("state")}}.Encode(), "", false)
	loc, err := url.Parse(cb.Header().Get("Location"))
	if cb.Code != http.StatusFound || err != nil || loc.Path != "/api/me/github/finish" || loc.Query().Get("ticket") == "" {
		t.Fatalf("callback: %d %q %s", cb.Code, cb.Header().Get("Location"), cb.Body.String())
	}
	// GitHub's code travels inside the ticket (signed, not encrypted: its
	// protection is that it is single use and spent only by the session
	// that started), never as a parameter of its own.
	if loc.Query().Get("code") != "" {
		t.Errorf("GitHub's code is a parameter of the finish link: %s", loc.RawQuery)
	}
	return loc.RequestURI()
}

// linkAs runs the whole link as the person.
func linkAs(t *testing.T, s *Server, sid, csrf, code string) *httptest.ResponseRecorder {
	t.Helper()
	return doCookie(t, s, "GET", linkTicket(t, s, sid, csrf, code), "", sid, "")
}

func doBearer(t *testing.T, s *Server, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// personToken is an API token the person makes, as an agent gets one.
func personToken(t *testing.T, s *Server, sid, csrf string) string {
	t.Helper()
	rec := doCookie(t, s, "POST", "/api/tokens", `{"name":"agent"}`, sid, csrf)
	var created TokenCreateView
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Token == "" {
		t.Fatalf("token: %d %s", rec.Code, rec.Body.String())
	}
	return created.Token
}

// exchanges counts the code exchanges and refreshes GitHub saw.
func exchanges(fake *githubtest.Server) int {
	n := 0
	for _, c := range fake.Calls() {
		if c == "POST /login/oauth/access_token" {
			n++
		}
	}
	return n
}

func TestLinkingGitHubStoresTheAccountSealed(t *testing.T) {
	s, _, fake := githubServer(t)
	heard := githubAudits(t)
	ctx := context.Background()
	sid, csrf, sub := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	finish := linkTicket(t, s, sid, csrf, githubtest.Code)
	if _, err := s.store.GitHubUserLink(ctx, "local", sub); !errors.Is(err, store.ErrNotFound) || exchanges(fake) != 0 {
		t.Fatalf("the callback alone links nothing: %v, %d exchanges", err, exchanges(fake))
	}
	rec := doCookie(t, s, "GET", finish, "", sid, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `postMessage({github:"linked"}`) || !strings.Contains(rec.Body.String(), githubtest.UserLogin) {
		t.Fatalf("finish: %d %s", rec.Code, rec.Body.String())
	}
	for _, secret := range []string{githubtest.UserToken, "ghr_", githubtest.ClientSecret} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("the page shows %q", secret)
		}
	}
	link, err := s.store.GitHubUserLink(ctx, "local", sub)
	if err != nil || link.GitHubLogin != githubtest.UserLogin || link.GitHubUserID != githubtest.UserID || strings.Contains(link.RefreshToken, "ghr_") {
		t.Fatalf("stored %+v %v", link, err)
	}
	if refresh, err := s.githubBox.Open(link.RefreshToken, githubLinkRow("local", sub)); err != nil || !strings.HasPrefix(refresh, "ghr_") {
		t.Errorf("the sealed token opens to %q %v", refresh, err)
	}
	if len(*heard) != 1 || (*heard)[0].Action != "github.link" || (*heard)[0].Target != "github user "+githubtest.UserLogin || (*heard)[0].Detail != "" {
		t.Errorf("audit %+v", *heard)
	}
	me := doCookie(t, s, "GET", "/api/me/github", "", sid, "")
	var v GitHubLinkView
	if me.Code != http.StatusOK || json.Unmarshal(me.Body.Bytes(), &v) != nil || v.Login != githubtest.UserLogin || v.LinkedAt.IsZero() {
		t.Errorf("me: %d %s", me.Code, me.Body.String())
	}
	if again := doCookie(t, s, "GET", finish, "", sid, ""); again.Code == http.StatusOK || !strings.Contains(again.Body.String(), "started by someone else, or has expired") {
		t.Errorf("replayed: %d %s", again.Code, again.Body.String())
	}
}

func TestALinkStartedBySomeoneElseIsRefused(t *testing.T) {
	s, _, _ := githubServer(t)
	ctx := context.Background()
	anaSID, anaCSRF, anaSub := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	bobSID, _, bobSub := signInPerson(t, s, "bob@example.test", store.WorkspaceRoleMember)
	refused := func(name string, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code == http.StatusOK || !strings.Contains(rec.Body.String(), "started by someone else, or has expired") {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	finish := linkTicket(t, s, anaSID, anaCSRF, githubtest.Code)
	refused("bob's session", doCookie(t, s, "GET", finish, "", bobSID, ""))
	refused("the admin token", do(t, s, "GET", finish, "", true))
	for _, sub := range []string{anaSub, bobSub} {
		if _, err := s.store.GitHubUserLink(ctx, "local", sub); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("linked %s: %v", sub, err)
		}
	}
	key := githubtest.ClientSecret
	ticket := func(sub string, exp time.Time, nonce string) string {
		return "/api/me/github/finish?ticket=" + url.QueryEscape(signGitHubLinkTicket(key, githubLinkTicket{
			Workspace: "default", Provider: "local", Subject: sub, Code: githubtest.Code, Return: "/", Exp: exp.Unix(), Nonce: nonce}))
	}
	refused("expired", doCookie(t, s, "GET", ticket(anaSub, time.Now().Add(-time.Second), "l1"), "", anaSID, ""))
	refused("signed with another key", doCookie(t, s, "GET", "/api/me/github/finish?ticket="+url.QueryEscape(signGitHubLinkTicket("not-the-key",
		githubLinkTicket{Workspace: "default", Provider: "local", Subject: anaSub, Code: githubtest.Code, Exp: time.Now().Add(time.Minute).Unix(), Nonce: "l2"})), "", anaSID, ""))
	connectTicket := signGitHubTicket(key, githubTicket{Workspace: "default", Provider: "local", Subject: anaSub, Installation: 42, Exp: time.Now().Add(time.Minute).Unix(), Nonce: "l3"})
	refused("a Connect ticket", doCookie(t, s, "GET", "/api/me/github/finish?ticket="+url.QueryEscape(connectTicket), "", anaSID, ""))
	if _, err := readGitHubState(key, signGitHubLinkTicket(key, githubLinkTicket{Workspace: "default", Subject: "x", Code: "c", Exp: time.Now().Add(time.Minute).Unix(), Nonce: "l4"}), time.Now()); err == nil {
		t.Error("a link ticket reads as a state")
	}
	// The refusals spent nothing: ana finishes her own.
	if rec := doCookie(t, s, "GET", finish, "", anaSID, ""); rec.Code != http.StatusOK {
		t.Errorf("ana: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := s.store.GitHubUserLink(ctx, "local", bobSub); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("bob is linked to ana's GitHub: %v", err)
	}
}

func TestALinkStateIsUsedOnce(t *testing.T) {
	s, _, _ := githubServer(t)
	sid, csrf, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	rec := doCookie(t, s, "POST", "/api/me/github/link", `{}`, sid, csrf)
	var v GitHubLinkStartView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	u, _ := url.Parse(v.AuthorizeURL)
	q := "/api/github/installed?" + url.Values{"code": {githubtest.Code}, "state": {u.Query().Get("state")}}.Encode()
	if first := do(t, s, "GET", q, "", false); first.Code != http.StatusFound {
		t.Fatalf("first: %d %s", first.Code, first.Body.String())
	}
	if again := do(t, s, "GET", q, "", false); again.Code != http.StatusBadRequest || !strings.Contains(again.Body.String(), "used already") {
		t.Errorf("replayed: %d %s", again.Code, again.Body.String())
	}
}

func TestAnAPITokenUsesItsMakersGitHubLink(t *testing.T) {
	s, _, _ := githubServer(t)
	sid, csrf, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	if rec := linkAs(t, s, sid, csrf, githubtest.Code); rec.Code != http.StatusOK {
		t.Fatalf("link: %d %s", rec.Code, rec.Body.String())
	}
	token := personToken(t, s, sid, csrf)
	rec := doBearer(t, s, "GET", "/api/me/github", "", token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"login":"`+githubtest.UserLogin+`"`) {
		t.Errorf("the token's GitHub: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "GET", "/api/me/github", "", true); rec.Code != http.StatusNotFound {
		t.Errorf("the admin token has a GitHub: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "POST", "/api/me/github/link", `{}`, true); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "sign in as yourself") {
		t.Errorf("the admin token links: %d %s", rec.Code, rec.Body.String())
	}
}

func TestUnlinkRevokesTheTokenAndForgetsTheLink(t *testing.T) {
	s, _, fake := githubServer(t)
	ctx := context.Background()
	sid, csrf, sub := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	heard := githubAudits(t)
	if rec := doCookie(t, s, "DELETE", "/api/me/github", "", sid, csrf); rec.Code != http.StatusNoContent {
		t.Fatalf("unlink: %d %s", rec.Code, rec.Body.String())
	}
	if got := fake.Revoked(); len(got) != 1 || got[0] != githubtest.UserToken {
		t.Errorf("revoked %v", got)
	}
	if _, err := s.store.GitHubUserLink(ctx, "local", sub); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("still linked: %v", err)
	}
	if len(*heard) != 1 || (*heard)[0].Action != "github.unlink" || (*heard)[0].Target != "github user "+githubtest.UserLogin {
		t.Errorf("audit %+v", *heard)
	}
	if rec := doCookie(t, s, "GET", "/api/me/github", "", sid, ""); rec.Code != http.StatusNotFound {
		t.Errorf("me after unlink: %d", rec.Code)
	}
	if rec := doCookie(t, s, "DELETE", "/api/me/github", "", sid, csrf); rec.Code != http.StatusNotFound {
		t.Errorf("unlinked twice: %d", rec.Code)
	}
	// GitHub down: the link goes all the same.
	linkAs(t, s, sid, csrf, githubtest.Code)
	fake.SetDown(true)
	if rec := doCookie(t, s, "DELETE", "/api/me/github", "", sid, csrf); rec.Code != http.StatusNoContent {
		t.Errorf("unlink with GitHub down: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := s.store.GitHubUserLink(ctx, "local", sub); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("still linked: %v", err)
	}
}

func TestAPersonsTokenIsRefreshedOnceAndTheNewRefreshTokenStored(t *testing.T) {
	s, _, fake := githubServer(t)
	ctx := context.Background()
	sid, csrf, sub := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	before, _ := s.store.GitHubUserLink(ctx, "local", sub)
	cl, err := s.githubApp.Client(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tok, err := s.githubAccessToken(ctx, cl, before); err != nil || tok != githubtest.UserToken || exchanges(fake) != 1 {
		t.Fatalf("kept from the link: %q %v, %d exchanges", tok, err, exchanges(fake))
	}
	// After a restart nothing is kept in memory: the refresh token is used,
	// once, and the new one stored.
	s.githubUsers.forget("local", sub)
	tok, err := s.githubAccessToken(ctx, cl, before)
	if err != nil || !strings.HasPrefix(tok, "ghu_") || exchanges(fake) != 2 {
		t.Fatalf("refreshed: %q %v, %d exchanges", tok, err, exchanges(fake))
	}
	after, _ := s.store.GitHubUserLink(ctx, "local", sub)
	if after.RefreshToken == before.RefreshToken {
		t.Error("the rotated refresh token was not stored")
	}
	if again, _ := s.githubAccessToken(ctx, cl, before); again != tok || exchanges(fake) != 2 {
		t.Errorf("refreshed twice: %q, %d exchanges", again, exchanges(fake))
	}
	// GitHub no longer takes it: the link goes, and the person is asked to
	// link again.
	s.githubUsers.forget("local", sub)
	fake.ForgetRefreshTokens()
	if _, err := s.githubAccessToken(ctx, cl, after); !errors.Is(err, errGitHubLinkRequired) {
		t.Errorf("refused refresh: %v", err)
	}
	if _, err := s.store.GitHubUserLink(ctx, "local", sub); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a dead link stays: %v", err)
	}
}

func TestALinkWithoutExpiringTokensSaysWhatToTurnOn(t *testing.T) {
	s, _, fake := githubServer(t)
	fake.SetExpiringUserTokens(false)
	sid, csrf, sub := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	rec := linkAs(t, s, sid, csrf, githubtest.Code)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Expire user authorization tokens") {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
	if _, err := s.store.GitHubUserLink(context.Background(), "local", sub); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("stored: %v", err)
	}
	if got := fake.Revoked(); len(got) != 1 {
		t.Errorf("the token GitHub gave is not revoked: %v", got)
	}
}

func TestTheCLIsLinkPageGoesToGitHub(t *testing.T) {
	s, _, _ := githubServer(t)
	sid, _, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	rec := doCookie(t, s, "GET", "/api/me/github/link", "", sid, "")
	u, err := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusFound || err != nil || u.Path != "/login/oauth/authorize" || u.Query().Get("state") == "" {
		t.Errorf("%d %q", rec.Code, rec.Header().Get("Location"))
	}
	// A browser not signed in is asked to sign in first.
	if rec := do(t, s, "GET", "/api/me/github/link", "", false); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "then open this link again") {
		t.Errorf("not signed in: %d %s", rec.Code, rec.Body.String())
	}
}

// The refresh race across replicas: each replica keeps its own access
// tokens in memory, and they share the store. racingLinks is one
// replica's view of the store, with a hook that runs once, at a chosen
// moment, as the other replica acting in between.
type racingLinks struct {
	githubLinkStore
	afterRead     func()
	beforeReplace func()
	beforeDelete  func()
	// failReplace: how many of the next replace writes fail.
	failReplace int
	// deletes counts the conditional deletes tried.
	deletes int
}

func once(f *func()) {
	if *f != nil {
		run := *f
		*f = nil
		run()
	}
}

func (r *racingLinks) GitHubUserLink(ctx context.Context, provider, subject string) (*store.GitHubUserLink, error) {
	l, err := r.githubLinkStore.GitHubUserLink(ctx, provider, subject)
	once(&r.afterRead)
	return l, err
}

func (r *racingLinks) ReplaceGitHubUserLinkToken(ctx context.Context, provider, subject, oldSealed string, l store.GitHubUserLink) (bool, error) {
	once(&r.beforeReplace)
	if r.failReplace > 0 {
		r.failReplace--
		return false, errors.New("the database is away")
	}
	return r.githubLinkStore.ReplaceGitHubUserLinkToken(ctx, provider, subject, oldSealed, l)
}

func (r *racingLinks) DeleteGitHubUserLinkIf(ctx context.Context, provider, subject, sealed string) (bool, error) {
	once(&r.beforeDelete)
	r.deletes++
	return r.githubLinkStore.DeleteGitHubUserLinkIf(ctx, provider, subject, sealed)
}

// replicas links ana through the server and returns what two other
// replicas share: the store, the token key, GitHub; each replica has a
// log, checked for tokens when the test ends.
type replicaSetup struct {
	s    *Server
	fake *githubtest.Server
	cl   *github.Client
	sub  string
	logs *bytes.Buffer
}

func replicas(t *testing.T) replicaSetup {
	t.Helper()
	s, _, fake := githubServer(t)
	sid, csrf, sub := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	if rec := linkAs(t, s, sid, csrf, githubtest.Code); rec.Code != http.StatusOK {
		t.Fatalf("link: %d %s", rec.Code, rec.Body.String())
	}
	cl, err := s.githubApp.Client(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	t.Cleanup(func() {
		for _, secret := range []string{"ghu_", "ghr_", githubtest.UserToken, githubtest.ClientSecret} {
			if strings.Contains(logs.String(), secret) {
				t.Errorf("the log shows %q: %s", secret, logs.String())
			}
		}
	})
	return replicaSetup{s: s, fake: fake, cl: cl, sub: sub, logs: logs}
}

func (r replicaSetup) replica(st githubLinkStore) *githubUserTokens {
	return newGitHubUserTokens(st, r.s.githubBox, slog.New(slog.NewTextHandler(r.logs, nil)))
}

func (r replicaSetup) stored(t *testing.T) *store.GitHubUserLink {
	t.Helper()
	l, err := r.s.store.GitHubUserLink(context.Background(), "local", r.sub)
	if err != nil {
		t.Fatalf("the link: %v", err)
	}
	return l
}

// relink links ana again behind the replicas' backs: a new refresh token
// GitHub takes, sealed and stored.
func (r replicaSetup) relink(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	tok, err := r.cl.ExchangeUserCode(ctx, githubtest.Code)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := r.s.githubBox.Seal(tok.Refresh, githubLinkRow("local", r.sub))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.s.store.PutGitHubUserLink(ctx, store.GitHubUserLink{Provider: "local", Subject: r.sub, GitHubUserID: githubtest.UserID,
		GitHubLogin: githubtest.UserLogin, RefreshToken: sealed, RefreshExpiresAt: tok.RefreshExpiresAt}); err != nil {
		t.Fatal(err)
	}
	return sealed
}

func TestAReplicaRefusedAfterAnotherRenewedUsesTheRenewedToken(t *testing.T) {
	r := replicas(t)
	ctx := context.Background()
	original := r.stored(t)
	a := r.replica(r.s.store)
	var fromA string
	race := &racingLinks{githubLinkStore: r.s.store}
	// B reads the row, then A renews it before B trades the token B read.
	race.afterRead = func() {
		tok, err := a.access(ctx, r.cl, original)
		if err != nil {
			t.Fatalf("a: %v", err)
		}
		fromA = tok
	}
	b := r.replica(race)
	tok, err := b.access(ctx, r.cl, original)
	if err != nil || !strings.HasPrefix(tok, "ghu_") || tok == fromA {
		t.Fatalf("b: %q %v (a has %q)", tok, err, fromA)
	}
	after := r.stored(t)
	if after.RefreshToken == original.RefreshToken {
		t.Error("b's renewed token was not stored")
	}
	if race.deletes != 0 {
		t.Errorf("b tried to remove a link another replica renewed: %d deletes", race.deletes)
	}
	// One code, A's refresh, B's refused one and B's second.
	if n := exchanges(r.fake); n != 4 {
		t.Errorf("%d exchanges", n)
	}
	// What B stored is live: a third replica renews with it.
	if _, err := r.replica(r.s.store).access(ctx, r.cl, after); err != nil {
		t.Errorf("the stored token: %v", err)
	}
}

func TestARefusedRefreshRemovesTheLinkOnlyWhileTheRefusedTokenIsStored(t *testing.T) {
	r := replicas(t)
	ctx := context.Background()
	original := r.stored(t)
	r.fake.ForgetRefreshTokens()
	// Ana links again between B's refusal and B's delete: the new link stays.
	race := &racingLinks{githubLinkStore: r.s.store}
	var relinked string
	race.beforeDelete = func() { relinked = r.relink(t) }
	b := r.replica(race)
	tok, err := b.access(ctx, r.cl, original)
	if err != nil || !strings.HasPrefix(tok, "ghu_") {
		t.Fatalf("b after the new link: %q %v", tok, err)
	}
	if l := r.stored(t); l.RefreshToken == relinked || l.RefreshToken == original.RefreshToken {
		t.Error("b did not store its renewed token over the new link")
	}
	// Nobody renewed it: the link goes, and the person is asked to link.
	r.fake.ForgetRefreshTokens()
	c := r.replica(r.s.store)
	if _, err := c.access(ctx, r.cl, original); !errors.Is(err, errGitHubLinkRequired) {
		t.Errorf("refused: %v", err)
	}
	if _, err := r.s.store.GitHubUserLink(ctx, "local", r.sub); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a dead link stays: %v", err)
	}
}

func TestAReplicaWhoseWriteLosesUsesTheStoredToken(t *testing.T) {
	r := replicas(t)
	ctx := context.Background()
	original := r.stored(t)
	race := &racingLinks{githubLinkStore: r.s.store}
	var relinked string
	race.beforeReplace = func() { relinked = r.relink(t) }
	b := r.replica(race)
	tok, err := b.access(ctx, r.cl, original)
	if err != nil || !strings.HasPrefix(tok, "ghu_") {
		t.Fatalf("b: %q %v", tok, err)
	}
	after := r.stored(t)
	if after.RefreshToken == relinked || after.RefreshToken == original.RefreshToken {
		t.Fatal("b did not renew with the stored token")
	}
	if got := r.fake.Revoked(); len(got) != 1 || got[0] == tok || !strings.HasPrefix(got[0], "ghu_") {
		t.Errorf("the token b dropped is not revoked: %v (b uses %q)", got, tok)
	}
	if _, err := r.replica(r.s.store).access(ctx, r.cl, after); err != nil {
		t.Errorf("the stored token: %v", err)
	}
}

func TestARenewedTokenIsKeptWhenItsWriteFailsAndWrittenNextTime(t *testing.T) {
	r := replicas(t)
	ctx := context.Background()
	original := r.stored(t)
	race := &racingLinks{githubLinkStore: r.s.store, failReplace: 1}
	b := r.replica(race)
	tok, err := b.access(ctx, r.cl, original)
	if err != nil || !strings.HasPrefix(tok, "ghu_") {
		t.Fatalf("b: %q %v", tok, err)
	}
	if r.stored(t).RefreshToken != original.RefreshToken {
		t.Fatal("a failed write changed the row")
	}
	again, err := b.access(ctx, r.cl, original)
	if err != nil || again != tok || exchanges(r.fake) != 2 {
		t.Fatalf("next use: %q %v, %d exchanges", again, err, exchanges(r.fake))
	}
	written := r.stored(t)
	if written.RefreshToken == original.RefreshToken {
		t.Fatal("the renewed token was not written on the next use")
	}
	if _, err := r.replica(r.s.store).access(ctx, r.cl, written); err != nil {
		t.Errorf("the written token: %v", err)
	}

	// The store stays away until the access token expires: the renewed
	// refresh token, kept in memory, is the one traded.
	before := r.stored(t)
	race.failReplace = 2
	c := r.replica(race)
	first, err := c.access(ctx, r.cl, before)
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return time.Now().Add(9 * time.Hour) }
	second, err := c.access(ctx, r.cl, before)
	if err != nil || second == first || !strings.HasPrefix(second, "ghu_") {
		t.Fatalf("after expiry: %q %v", second, err)
	}
	if l := r.stored(t); l.RefreshToken == before.RefreshToken {
		t.Error("nothing written once the store came back")
	} else if _, err := r.replica(r.s.store).access(ctx, r.cl, l); err != nil {
		t.Errorf("the written token: %v", err)
	}
}

func TestATransientErrorRemovesNothing(t *testing.T) {
	r := replicas(t)
	ctx := context.Background()
	original := r.stored(t)
	r.fake.SetDown(true)
	if _, err := r.replica(r.s.store).access(ctx, r.cl, original); err == nil || errors.Is(err, errGitHubLinkRequired) {
		t.Errorf("GitHub down: %v", err)
	}
	if l := r.stored(t); l.RefreshToken != original.RefreshToken {
		t.Error("the link changed")
	}
	r.fake.SetDown(false)
	if _, err := r.replica(r.s.store).access(ctx, r.cl, original); err != nil {
		t.Errorf("GitHub back: %v", err)
	}
}

func TestALinkThatNoLongerOpensIsRemovedAndAsksForALinkAgain(t *testing.T) {
	s, _, _ := githubServer(t)
	ctx := context.Background()
	_, _, sub := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	other, err := tokenseal.Generate()
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := other.Seal("ghr_sealed_under_the_old_key", githubLinkRow("local", sub))
	if err := s.store.PutGitHubUserLink(ctx, store.GitHubUserLink{Provider: "local", Subject: sub, GitHubUserID: githubtest.UserID,
		GitHubLogin: githubtest.UserLogin, RefreshToken: sealed}); err != nil {
		t.Fatal(err)
	}
	cl, err := s.githubApp.Client(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("GET", "/api/workspace/github/repositories", nil)
	ext.SetIdentity(c, ext.Identity{Subject: sub, Email: "ana@example.test", Provider: "local"})
	_, _, err = s.githubPersonToken(c, cl)
	if !errors.Is(err, errGitHubLinkRequired) {
		t.Fatalf("err = %v", err)
	}
	githubLinkAnswer(c, err)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"github_link_required"`) {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
	if _, err := s.store.GitHubUserLink(ctx, "local", sub); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the unreadable link stays: %v", err)
	}
}

func TestAConnectStateDoesNotLinkAndALinkStateDoesNotConnect(t *testing.T) {
	s, _, _ := githubServer(t)
	ctx := context.Background()
	sid, csrf, sub := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	// A link state with an installation: still a link, never a connection.
	rec := doCookie(t, s, "POST", "/api/me/github/link", `{}`, sid, csrf)
	var v GitHubLinkStartView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	u, _ := url.Parse(v.AuthorizeURL)
	cb := do(t, s, "GET", "/api/github/installed?"+url.Values{"code": {githubtest.Code}, "state": {u.Query().Get("state")}, "installation_id": {"42"}}.Encode(), "", false)
	if loc := cb.Header().Get("Location"); cb.Code != http.StatusFound || !strings.Contains(loc, "/api/me/github/finish?") {
		t.Errorf("link state: %d %q", cb.Code, loc)
	}
	if list, _ := s.store.GitHubInstallations(ctx, store.DefaultWorkspace); len(list) != 0 {
		t.Errorf("connected %+v", list)
	}
	// A Connect state is no link.
	state := connectState(t, s)
	cb = do(t, s, "GET", "/api/github/installed?"+url.Values{"code": {githubtest.Code}, "state": {state}}.Encode(), "", false)
	if strings.Contains(cb.Header().Get("Location"), "/api/me/github/finish") {
		t.Errorf("a Connect state linked: %q", cb.Header().Get("Location"))
	}
	if _, err := s.store.GitHubUserLink(ctx, "local", sub); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("linked: %v", err)
	}
}

// tokenIdentifier is an extension's identifier (as the MCP's OAuth tokens
// are): a request with X-Test-Person acts as that person.
func tokenIdentifier(s *Server, sub, email string) {
	s.identifiers = append(s.identifiers, func(c *gin.Context) bool {
		if c.GetHeader("X-Test-Person") != sub {
			return false
		}
		ext.SetIdentity(c, ext.Identity{Subject: sub, Email: email, Provider: "local"})
		return true
	})
}

func TestALinkStartsAndFinishesOnlyInABrowserSession(t *testing.T) {
	s, _, _ := githubServer(t)
	ctx := context.Background()
	sid, csrf, sub := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	token := personToken(t, s, sid, csrf)
	tokenIdentifier(s, sub, "ana@example.test")
	const says = "Link GitHub from the dashboard, or with shpyrd github link, which opens a browser"
	refused := func(name string, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), says) {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	viaExtension := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer mcp-token")
		req.Header.Set("X-Test-Person", sub)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	refused("the token's start", doBearer(t, s, "POST", "/api/me/github/link", `{}`, token))
	refused("the token's browser start", doBearer(t, s, "GET", "/api/me/github/link", "", token))
	refused("the extension's start", viaExtension("POST", "/api/me/github/link", `{}`))
	refused("the extension's browser start", viaExtension("GET", "/api/me/github/link", ""))
	// A valid ticket for ana herself, presented with her token: refused,
	// and not spent.
	finish := linkTicket(t, s, sid, csrf, githubtest.Code)
	refused("the token's finish", doBearer(t, s, "GET", finish, "", token))
	refused("the extension's finish", viaExtension("GET", finish, ""))
	if _, err := s.store.GitHubUserLink(ctx, "local", sub); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("linked by token: %v", err)
	}
	// Reading and unlinking by token stay allowed.
	if rec := doCookie(t, s, "GET", finish, "", sid, ""); rec.Code != http.StatusOK {
		t.Fatalf("ana's session: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doBearer(t, s, "GET", "/api/me/github", "", token); rec.Code != http.StatusOK {
		t.Errorf("read by token: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doBearer(t, s, "DELETE", "/api/me/github", "", token); rec.Code != http.StatusNoContent {
		t.Errorf("unlink by token: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAReplicaDoesNotUseItsTokenForALinkMadeAgain(t *testing.T) {
	r := replicas(t)
	ctx := context.Background()
	b := r.replica(r.s.store)
	old, err := b.access(ctx, r.cl, r.stored(t))
	if err != nil {
		t.Fatal(err)
	}
	// Ana unlinks and links the same GitHub account again elsewhere.
	sid, csrf, _ := signInPerson(t, r.s, "ana@example.test", store.WorkspaceRoleMember)
	if rec := doCookie(t, r.s, "DELETE", "/api/me/github", "", sid, csrf); rec.Code != http.StatusNoContent {
		t.Fatalf("unlink: %d %s", rec.Code, rec.Body.String())
	}
	if rec := linkAs(t, r.s, sid, csrf, githubtest.Code); rec.Code != http.StatusOK {
		t.Fatalf("link: %d %s", rec.Code, rec.Body.String())
	}
	again, err := b.access(ctx, r.cl, r.stored(t))
	if err != nil || again == old {
		t.Errorf("b after the new link: %q %v (before %q)", again, err, old)
	}
}

func TestAPendingRenewalThatLostItsRowIsRevoked(t *testing.T) {
	r := replicas(t)
	ctx := context.Background()
	race := &racingLinks{githubLinkStore: r.s.store, failReplace: 1}
	b := r.replica(race)
	kept, err := b.access(ctx, r.cl, r.stored(t))
	if err != nil {
		t.Fatal(err)
	}
	r.relink(t)
	tok, err := b.access(ctx, r.cl, r.stored(t))
	if err != nil || tok == kept {
		t.Fatalf("b: %q %v", tok, err)
	}
	if got := r.fake.Revoked(); len(got) != 1 || got[0] != kept {
		t.Errorf("revoked %v, want %q", got, kept)
	}
}

// failingLinks is a store whose link writes fail.
type failingLinks struct{ store.Store }

func (failingLinks) PutGitHubUserLink(context.Context, store.GitHubUserLink) error {
	return errors.New("the database is away")
}

func TestAFinishThatFailsRevokesTheTokenGitHubGave(t *testing.T) {
	s, _, fake := githubServer(t)
	sid, csrf, _ := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	s.store = failingLinks{s.store}
	if rec := linkAs(t, s, sid, csrf, githubtest.Code); rec.Code != http.StatusBadGateway {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if got := fake.Revoked(); len(got) != 1 || got[0] != githubtest.UserToken {
		t.Errorf("revoked %v", got)
	}
}

func TestALinkMadeAgainRevokesTheEarlierToken(t *testing.T) {
	s, _, fake := githubServer(t)
	ctx := context.Background()
	fake.AddUser("bea-code", githubtest.User{ID: 2002, Login: "bea"})
	sid, csrf, sub := signInPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	if rec := linkAs(t, s, sid, csrf, "bea-code"); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if got := fake.Revoked(); len(got) != 1 || got[0] != githubtest.UserToken {
		t.Errorf("revoked %v", got)
	}
	if l, err := s.store.GitHubUserLink(ctx, "local", sub); err != nil || l.GitHubLogin != "bea" {
		t.Errorf("link %+v %v", l, err)
	}
}
