package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/pages"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tokenseal"
)

// A person's GitHub account (RFC-0054, GitHub access by person): linked
// once through the GitHub App's user authorization, in a popup that keeps
// New project as it was. The picker then lists what the person can read,
// and a project is made from a repository only by someone who can read it.
// Builds, webhooks, checks and Deployments stay with the installation: a
// person who leaves or unlinks breaks nothing.

// GitHubLinkView is the person's linked GitHub account.
type GitHubLinkView struct {
	Login    string    `json:"login"`
	LinkedAt time.Time `json:"linkedAt"`
}

// GitHubLinkStartView is where the popup goes: GitHub's authorize page.
type GitHubLinkStartView struct {
	AuthorizeURL string `json:"authorizeURL"`
}

const (
	// githubLinkRequiredCode is the code of the 409 that asks the person to
	// link their GitHub account first.
	githubLinkRequiredCode = "github_link_required"
	// githubLinkTicketPrefix sets a link ticket apart from a state and a
	// Connect ticket.
	githubLinkTicketPrefix = "l."
	// githubUserTokenMargin: an access token is used until 5 minutes
	// before it expires.
	githubUserTokenMargin = 5 * time.Minute
	// tokenOwnerKey is where identifyWithToken leaves the token's maker.
	tokenOwnerKey = "shpyrd.tokenOwner"
	// sessionAuthKey is set by auth when the identity came from the
	// session cookie: a browser the person signed in to.
	sessionAuthKey = "shpyrd.sessionAuth"
)

var (
	errGitHubLinkRequired = errors.New("Link your GitHub account first: Continue with GitHub in New project, or run shpyrd github link")
	errGitHubNoPerson     = errors.New("the admin token is no person and has no GitHub account: sign in as yourself to link GitHub")
	// errGitHubLinkInBrowser: a token (an API token, an assistant's) may
	// read and remove the link, never make one: whoever holds a leaked
	// token would otherwise link their own GitHub account to the person.
	errGitHubLinkInBrowser = errors.New("Link GitHub from the dashboard, or with shpyrd github link, which opens a browser")
	// errGitHubLinkBusy: the link kept changing under a renewal (other
	// replicas renewing, or the person linking again): nothing is removed.
	errGitHubLinkBusy = errors.New("your GitHub authorization changed while shpyrd renewed it")
)

// githubLinkTicket is what the callback hands the workspace's finish page:
// who started the link, where they were, and GitHub's one-time code,
// exchanged only once the session that started presents it.
type githubLinkTicket struct {
	Workspace string `json:"ws"`
	Provider  string `json:"prov"`
	Subject   string `json:"sub"`
	Code      string `json:"code"`
	Return    string `json:"ret,omitempty"`
	Exp       int64  `json:"exp"`
	Nonce     string `json:"n"`
}

func signGitHubLinkTicket(key string, tk githubLinkTicket) string {
	return githubSign(key, githubLinkTicketPrefix, tk)
}

// readGitHubLinkTicket checks the tag in constant time, then the time.
func readGitHubLinkTicket(key, raw string, now time.Time) (githubLinkTicket, error) {
	var tk githubLinkTicket
	if !githubRead(key, githubLinkTicketPrefix, raw, &tk) || tk.Workspace == "" || tk.Subject == "" || tk.Code == "" || tk.Nonce == "" || now.Unix() > tk.Exp {
		return githubLinkTicket{}, errGitHubTicket
	}
	return tk, nil
}

// githubPersonOf is whose GitHub link a request uses: the person signed
// in, or, for an API token, the person who made it. Not ok for the admin
// token, a token the admin token made, the operator's kubeconfig, and a
// server without sign-in.
func githubPersonOf(c *gin.Context) (provider, subject string, ok bool) {
	id, signedIn := ext.IdentityFrom(c)
	if v, has := c.Get(tokenOwnerKey); has {
		if owner, isIdentity := v.(ext.Identity); isIdentity {
			id, signedIn = owner, true
		}
	}
	switch {
	case !signedIn || id.Subject == "" || id.Provider == "":
		return "", "", false
	case id.Provider == "token" || id.Provider == "api-token" || id.Provider == "kubeconfig" || id.Provider == "none":
		return "", "", false
	}
	return id.Provider, id.Subject, true
}

// githubBrowserSession says the request's identity came from the session
// cookie, not from a token: a link starts and finishes only there.
func githubBrowserSession(c *gin.Context) bool {
	if _, byToken := c.Get(tokenOwnerKey); byToken {
		return false
	}
	return c.GetBool(sessionAuthKey)
}

// githubLinkRow binds a sealed refresh token to its link.
func githubLinkRow(provider, subject string) string {
	return tokenseal.Row("github-user-link", provider, subject)
}

// githubLinkStart is POST /api/me/github/link: GitHub's authorize URL, with
// a state bound to the person, for the dashboard's popup.
func (s *Server) githubLinkStart(c *gin.Context) {
	var req struct {
		Return string `json:"return"`
	}
	_ = c.ShouldBindJSON(&req)
	if to, ok := s.githubLinkURL(c, req.Return); ok {
		c.JSON(http.StatusOK, GitHubLinkStartView{AuthorizeURL: to})
	}
}

// githubLinkBrowser is GET /api/me/github/link: `shpyrd github link` opens
// it in the browser, signed in there, and it goes on to GitHub.
func (s *Server) githubLinkBrowser(c *gin.Context) {
	if to, ok := s.githubLinkURL(c, c.Query("return")); ok {
		c.Header("Cache-Control", "no-store")
		c.Redirect(http.StatusFound, to)
	}
}

func (s *Server) githubLinkURL(c *gin.Context, ret string) (string, bool) {
	provider, subject, ok := githubPersonOf(c)
	if !ok {
		abort(c, http.StatusBadRequest, errGitHubNoPerson)
		return "", false
	}
	if !githubBrowserSession(c) {
		abort(c, http.StatusForbidden, errGitHubLinkInBrowser)
		return "", false
	}
	cl, ok := s.githubClient(c)
	if !ok {
		return "", false
	}
	app := cl.App()
	actor, _, _ := githubWho(c)
	state, err := newGitHubState(app, githubState{Workspace: s.doorWorkspace(c), Actor: actor, Provider: provider, Subject: subject,
		Link: true, Return: safeNextOrEmpty(ret)}, time.Now().Add(githubStateTTL))
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return "", false
	}
	// No redirect_uri: GitHub returns to the App's registered callback,
	// /api/github/installed, which tells a link by its state.
	return app.WebURL() + "/login/oauth/authorize?" + url.Values{"client_id": {app.ClientID}, "state": {state}}.Encode(), true
}

// githubLinkCallback is GitHub's return from a link's authorize step, on
// /api/github/installed: the state is spent, and GitHub's code goes on in
// a ticket to the workspace's finish, where the session that started must
// present it.
func (s *Server) githubLinkCallback(c *gin.Context, cl *github.Client, st githubState) {
	now := time.Now()
	if !s.githubNonces.use(st.Nonce, time.Unix(st.Exp, 0), now) {
		s.githubPage(c, http.StatusBadRequest, "This link was used already", "Each Continue with GitHub is used once. Start again from New project, or run shpyrd github link.", "")
		return
	}
	code := c.Query("code")
	if code == "" {
		s.githubPage(c, http.StatusBadRequest, "GitHub did not say who you are", "Nothing was linked. Start again from New project, or run shpyrd github link.", "")
		return
	}
	nonce, err := randomHex(16)
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	tk := githubLinkTicket{Workspace: st.Workspace, Provider: st.Provider, Subject: st.Subject, Code: code, Return: st.Return,
		Exp: now.Add(githubTicketTTL).Unix(), Nonce: nonce}
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, s.workspaceDashboardURL(c.Request.Context(), st.Workspace)+"/api/me/github/finish?ticket="+url.QueryEscape(signGitHubLinkTicket(cl.App().ClientSecret, tk)))
}

// githubLinkFinish is GET /api/me/github/finish, on the workspace's own
// address: the ticket, presented by the session that started the link,
// is exchanged for the person's tokens, and the link is stored.
func (s *Server) githubLinkFinish(c *gin.Context) {
	ctx := c.Request.Context()
	refuse := func(status int) {
		s.githubPage(c, status, "Not linked", "This link was started by someone else, or has expired: start it again from New project, or run shpyrd github link.", "")
	}
	cl, err := s.githubApp.Client(ctx)
	if err != nil {
		refuse(http.StatusNotFound)
		return
	}
	now := time.Now()
	tk, err := readGitHubLinkTicket(cl.App().ClientSecret, c.Query("ticket"), now)
	if err != nil {
		refuse(http.StatusBadRequest)
		return
	}
	provider, subject, ok := githubPersonOf(c)
	if !ok || tk.Workspace != s.doorWorkspace(c) || tk.Provider != provider || tk.Subject != subject {
		refuse(http.StatusForbidden)
		return
	}
	if !githubBrowserSession(c) {
		s.githubPage(c, http.StatusForbidden, "Not linked", errGitHubLinkInBrowser.Error()+".", "")
		return
	}
	// Spent only once the right person presents it.
	if !s.githubTickets.use(tk.Nonce, time.Unix(tk.Exp, 0), now) {
		refuse(http.StatusBadRequest)
		return
	}
	tok, err := s.githubLinkExchange(ctx, cl, tk.Code)
	if errors.Is(err, github.ErrNoRefreshToken) {
		s.githubPage(c, http.StatusConflict, "GitHub needs a setting", err.Error()+".", "")
		return
	}
	if err != nil {
		s.log.Warn("github: could not exchange a link's code", "err", err.Error())
		s.githubPage(c, http.StatusBadGateway, "GitHub did not answer", "shpyrd could not finish linking your GitHub account. Start again in a minute.", "")
		return
	}
	// From here, a token GitHub gave that is not kept is revoked.
	who, err := cl.UserProfile(ctx, tok.Access)
	if err != nil {
		s.githubUsers.revoke(ctx, cl, tok.Access)
		s.log.Warn("github: could not ask GitHub who linked", "err", err.Error())
		s.githubPage(c, http.StatusBadGateway, "GitHub did not answer", "shpyrd could not finish linking your GitHub account. Start again in a minute.", "")
		return
	}
	sealed, err := s.githubBox.Seal(tok.Refresh, githubLinkRow(provider, subject))
	if err != nil {
		s.githubUsers.revoke(ctx, cl, tok.Access)
		abort(c, http.StatusInternalServerError, err)
		return
	}
	// A link made again replaces the earlier one, whose token is revoked
	// once the new one is stored.
	earlier := ""
	if prev, err := s.store.GitHubUserLink(ctx, provider, subject); err == nil {
		if access, err := s.githubAccessToken(ctx, cl, prev); err == nil {
			earlier = access
		}
	}
	link := store.GitHubUserLink{Provider: provider, Subject: subject, GitHubUserID: who.ID, GitHubLogin: who.Login,
		RefreshToken: sealed, RefreshExpiresAt: tok.RefreshExpiresAt}
	if err := s.store.PutGitHubUserLink(ctx, link); err != nil {
		s.githubUsers.revoke(ctx, cl, tok.Access)
		s.log.Warn("github: could not record a link", "err", err.Error())
		s.githubPage(c, http.StatusBadGateway, "Not linked", "shpyrd could not record the link. Start again in a minute.", "")
		return
	}
	if earlier != "" && earlier != tok.Access {
		s.githubUsers.revoke(ctx, cl, earlier)
	}
	// Kept for the link as stored: its LinkedAt is the store's.
	s.githubUsers.forget(provider, subject)
	if stored, err := s.store.GitHubUserLink(ctx, provider, subject); err == nil && stored.RefreshToken == sealed {
		s.githubUsers.keep(stored, tok)
	}
	s.audit(c, "", "github.link", "github user "+who.Login, "")
	s.githubLinkedPage(c, who.Login, firstNonEmpty(tk.Return, "/"))
}

// githubLinkExchange trades a link's code for the person's tokens;
// github.ErrNoRefreshToken when the App's user tokens do not expire, the
// token GitHub gave then revoked: shpyrd could not act for the person
// later, and keeps nothing.
func (s *Server) githubLinkExchange(ctx context.Context, cl *github.Client, code string) (github.UserToken, error) {
	tok, err := cl.ExchangeUserCode(ctx, code)
	if err != nil {
		return github.UserToken{}, err
	}
	if tok.Refresh == "" {
		if err := cl.RevokeUserToken(ctx, tok.Access); err != nil {
			s.log.Warn("github: could not revoke a token that is not kept", "err", err.Error())
		}
		return github.UserToken{}, github.ErrNoRefreshToken
	}
	return tok, nil
}

// githubLinkedScript tells the dashboard under the popup, and closes it.
// Opened by the CLI, there is no opener: the page stays and says so.
const githubLinkedScript = `if(window.opener){try{window.opener.postMessage({github:"linked"},location.origin)}catch(e){}window.close()}`

// githubLinkedPage is the end of a link.
func (s *Server) githubLinkedPage(c *gin.Context, login, back string) {
	html := pages.HTML(pages.Nothing, pages.Page{
		Title: "GitHub linked",
		Text:  fmt.Sprintf("Your GitHub account %s is linked. You can close this window.", login),
		Links: []pages.Link{{Label: "Back to shpyrd", URL: back}},
	})
	if i := strings.LastIndex(html, "</body>"); i >= 0 {
		html = html[:i] + "<script>" + githubLinkedScript + "</script>" + html[i:]
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

// githubMe is GET /api/me/github.
func (s *Server) githubMe(c *gin.Context) {
	provider, subject, ok := githubPersonOf(c)
	if !ok {
		abort(c, http.StatusNotFound, errors.New("no GitHub account is linked: the admin token is no person"))
		return
	}
	link, err := s.store.GitHubUserLink(c.Request.Context(), provider, subject)
	if errors.Is(err, store.ErrNotFound) {
		abort(c, http.StatusNotFound, errors.New("no GitHub account is linked: shpyrd github link"))
		return
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, GitHubLinkView{Login: link.GitHubLogin, LinkedAt: link.LinkedAt})
}

// githubUnlink is DELETE /api/me/github: the token is revoked at GitHub
// and the link removed. The person's projects keep deploying.
func (s *Server) githubUnlink(c *gin.Context) {
	provider, subject, ok := githubPersonOf(c)
	if !ok {
		abort(c, http.StatusBadRequest, errGitHubNoPerson)
		return
	}
	ctx := c.Request.Context()
	link, err := s.store.GitHubUserLink(ctx, provider, subject)
	if errors.Is(err, store.ErrNotFound) {
		abort(c, http.StatusNotFound, errors.New("no GitHub account is linked"))
		return
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	if cl, err := s.githubApp.Client(ctx); err == nil {
		access, err := s.githubAccessToken(ctx, cl, link)
		if err == nil {
			err = cl.RevokeUserToken(ctx, access)
		}
		if err != nil && !errors.Is(err, errGitHubLinkRequired) {
			s.log.Warn("github: could not revoke the person's GitHub token; the link is removed all the same", "login", link.GitHubLogin, "err", err.Error())
		}
	}
	if err := s.store.DeleteGitHubUserLink(ctx, provider, subject); err != nil && !errors.Is(err, store.ErrNotFound) {
		abort(c, http.StatusBadGateway, err)
		return
	}
	s.githubUsers.forget(provider, subject)
	s.audit(c, "", "github.unlink", "github user "+link.GitHubLogin, "")
	c.Status(http.StatusNoContent)
}

// githubLinkStore is what the people's tokens need of the store.
type githubLinkStore interface {
	GitHubUserLink(ctx context.Context, provider, subject string) (*store.GitHubUserLink, error)
	ReplaceGitHubUserLinkToken(ctx context.Context, provider, subject, oldSealed string, l store.GitHubUserLink) (bool, error)
	DeleteGitHubUserLinkIf(ctx context.Context, provider, subject, sealed string) (bool, error)
}

// githubUserTokens keeps each person's access token in memory, never
// stored, and one lock per person: GitHub takes each refresh token once.
// Each replica has its own; they share the store, whose conditional
// writes settle a race between replicas.
type githubUserTokens struct {
	store githubLinkStore
	box   *tokenseal.Box
	log   *slog.Logger
	now   func() time.Time

	mu    sync.Mutex
	by    map[string]githubUserEntry
	locks map[string]*sync.Mutex
}

// githubUserEntry is one person's access token in memory, for the GitHub
// account it belongs to.
type githubUserEntry struct {
	// userID and linkedAt name the link: linkedAt stays across renewals
	// and changes when the person unlinks and links again.
	userID   int64
	linkedAt time.Time
	access   string
	expires  time.Time
	// pending is a renewal the store has not taken yet: written on the
	// next use, so the rotated refresh token is never lost.
	pending *githubPendingWrite
}

// githubPendingWrite is the row as a renewal left it, and the sealed token
// the store still has.
type githubPendingWrite struct {
	old  string
	link store.GitHubUserLink
}

func newGitHubUserTokens(st githubLinkStore, box *tokenseal.Box, log *slog.Logger) *githubUserTokens {
	return &githubUserTokens{store: st, box: box, log: log, now: time.Now}
}

func personKey(provider, subject string) string { return provider + "\x00" + subject }

// usable says the entry's access token is the link's, for 5 minutes more.
func (e githubUserEntry) usable(link *store.GitHubUserLink, now time.Time) bool {
	return e.access != "" && e.userID == link.GitHubUserID && e.linkedAt.Equal(link.LinkedAt) && now.Add(githubUserTokenMargin).Before(e.expires)
}

// entryFor is a renewal's entry, for the link it renewed.
func entryFor(link *store.GitHubUserLink, t github.UserToken) githubUserEntry {
	return githubUserEntry{userID: link.GitHubUserID, linkedAt: link.LinkedAt, access: t.Access, expires: t.AccessExpiresAt}
}

func (g *githubUserTokens) entry(key string) (githubUserEntry, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.by[key]
	return e, ok
}

func (g *githubUserTokens) put(key string, e githubUserEntry) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.by == nil {
		g.by = map[string]githubUserEntry{}
	}
	g.by[key] = e
}

// keep holds a freshly linked person's access token, for the link stored.
func (g *githubUserTokens) keep(link *store.GitHubUserLink, t github.UserToken) {
	g.put(personKey(link.Provider, link.Subject), entryFor(link, t))
}

// revoke revokes, best effort, an access token shpyrd drops: one never
// kept stays usable at GitHub until it expires otherwise.
func (g *githubUserTokens) revoke(ctx context.Context, cl *github.Client, access string) {
	if err := cl.RevokeUserToken(ctx, access); err != nil {
		g.log.Warn("github: could not revoke a person's GitHub token shpyrd no longer keeps", "err", err.Error())
	}
}

// forget drops the person's access token from memory, unrevoked: for a
// token GitHub refused (401), or a link removed. The next use renews.
func (g *githubUserTokens) forget(provider, subject string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.by, personKey(provider, subject))
}

func (g *githubUserTokens) lock(key string) *sync.Mutex {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.locks == nil {
		g.locks = map[string]*sync.Mutex{}
	}
	if g.locks[key] == nil {
		g.locks[key] = &sync.Mutex{}
	}
	return g.locks[key]
}

// access is the person's access token: the one in memory while it has 5
// minutes left, else a renewal with the refresh token, whose successor is
// sealed and stored. errGitHubLinkRequired when there is no link, when
// GitHub no longer takes the stored token, or when it no longer opens (the
// key changed): the link is then removed. Any other error removes nothing.
func (g *githubUserTokens) access(ctx context.Context, cl *github.Client, link *store.GitHubUserLink) (string, error) {
	key := personKey(link.Provider, link.Subject)
	if e, ok := g.entry(key); ok && e.pending == nil && e.usable(link, g.now()) {
		return e.access, nil
	}
	mu := g.lock(key)
	mu.Lock()
	defer mu.Unlock()
	e, has := g.entry(key)
	if has && e.pending != nil {
		done, err := g.store.ReplaceGitHubUserLinkToken(ctx, link.Provider, link.Subject, e.pending.old, e.pending.link)
		switch {
		case err != nil:
			g.log.Warn("github: could not record a person's renewed GitHub authorization; it is kept in memory and written on the next use", "login", e.pending.link.GitHubLogin, "err", err.Error())
		case done:
			e.pending = nil
			g.put(key, e)
		default:
			// The row changed meanwhile (unlinked, or linked again): this
			// renewal is no longer the link's.
			g.forget(link.Provider, link.Subject)
			g.revoke(ctx, cl, e.access)
			e, has = githubUserEntry{}, false
		}
	}
	if has && e.usable(link, g.now()) {
		return e.access, nil
	}
	return g.renew(ctx, cl, link.Provider, link.Subject, e.pending)
}

// renew trades a refresh token, under the person's lock: the stored one,
// or a renewal's the store has not taken yet (pending).
func (g *githubUserTokens) renew(ctx context.Context, cl *github.Client, provider, subject string, pending *githubPendingWrite) (string, error) {
	key := personKey(provider, subject)
	row := githubLinkRow(provider, subject)
	// cur is the row as stored; use is the sealed token to trade.
	var cur *store.GitHubUserLink
	var use string
	read := func() error {
		l, err := g.store.GitHubUserLink(ctx, provider, subject)
		if errors.Is(err, store.ErrNotFound) {
			g.forget(provider, subject)
			return errGitHubLinkRequired
		}
		if err != nil {
			return err
		}
		cur, use = l, l.RefreshToken
		return nil
	}
	if pending != nil {
		stored := pending.link
		stored.RefreshToken = pending.old
		cur, use = &stored, pending.link.RefreshToken
	} else if err := read(); err != nil {
		return "", err
	}
	for retried := false; ; retried = true {
		refresh, err := g.box.Open(use, row)
		if err != nil {
			g.log.Warn("github: a stored GitHub link does not open with the token key; it is removed and the person links again", "login", cur.GitHubLogin)
			g.forget(provider, subject)
			if _, err := g.store.DeleteGitHubUserLinkIf(ctx, provider, subject, use); err != nil {
				g.log.Warn("github: could not remove a GitHub link that does not open", "login", cur.GitHubLogin, "err", err.Error())
			}
			return "", errGitHubLinkRequired
		}
		tok, err := cl.RefreshUserToken(ctx, refresh)
		if errors.Is(err, github.ErrUserTokenRefused) {
			g.forget(provider, subject)
			refused := use
			if err := read(); err != nil {
				return "", err
			}
			if use != refused {
				// Another replica renewed it meanwhile: its token is the live one.
				if retried {
					return "", errGitHubLinkBusy
				}
				continue
			}
			gone, err := g.store.DeleteGitHubUserLinkIf(ctx, provider, subject, refused)
			if err != nil {
				return "", err
			}
			if !gone {
				// Renewed, or linked again, between the read and the delete.
				if retried {
					return "", errGitHubLinkBusy
				}
				if err := read(); err != nil {
					return "", err
				}
				continue
			}
			g.log.Info("github: GitHub no longer accepts a person's authorization; the link is removed and the person links again", "login", cur.GitHubLogin)
			return "", errGitHubLinkRequired
		}
		if err != nil {
			return "", err
		}
		if tok.Refresh == "" {
			// The App's user tokens stopped expiring: shpyrd cannot renew
			// again. The link goes; linking again says what to turn on.
			if err := cl.RevokeUserToken(ctx, tok.Access); err != nil {
				g.log.Warn("github: could not revoke a token that is not kept", "err", err.Error())
			}
			g.forget(provider, subject)
			if _, err := g.store.DeleteGitHubUserLinkIf(ctx, provider, subject, cur.RefreshToken); err != nil {
				return "", err
			}
			g.log.Warn("github: GitHub gave no refresh token on a renewal; the link is removed", "login", cur.GitHubLogin)
			return "", errGitHubLinkRequired
		}
		sealed, err := g.box.Seal(tok.Refresh, row)
		if err != nil {
			return "", err
		}
		next := *cur
		next.RefreshToken, next.RefreshExpiresAt = sealed, tok.RefreshExpiresAt
		// In memory first: if the write fails, the renewed refresh token is
		// still here, and written on the next use.
		pend := entryFor(cur, tok)
		pend.pending = &githubPendingWrite{old: cur.RefreshToken, link: next}
		g.put(key, pend)
		done, err := g.store.ReplaceGitHubUserLinkToken(ctx, provider, subject, cur.RefreshToken, next)
		if err != nil {
			g.log.Warn("github: could not record a person's renewed GitHub authorization; it is kept in memory and written on the next use", "login", cur.GitHubLogin, "err", err.Error())
			return tok.Access, nil
		}
		if done {
			g.put(key, entryFor(cur, tok))
			return tok.Access, nil
		}
		// Another replica's write won (or the person linked again): the
		// stored token is the link's, and this one is dropped.
		g.forget(provider, subject)
		g.revoke(ctx, cl, tok.Access)
		if retried {
			return "", errGitHubLinkBusy
		}
		if err := read(); err != nil {
			return "", err
		}
	}
}

// githubAccessToken is the person's access token (githubUserTokens.access).
func (s *Server) githubAccessToken(ctx context.Context, cl *github.Client, link *store.GitHubUserLink) (string, error) {
	return s.githubUsers.access(ctx, cl, link)
}

// githubPersonToken is the GitHub token of whoever the request acts as,
// and their login; errGitHubLinkRequired when they have no link (or are no
// person).
func (s *Server) githubPersonToken(c *gin.Context, cl *github.Client) (string, string, error) {
	provider, subject, ok := githubPersonOf(c)
	if !ok {
		return "", "", errGitHubLinkRequired
	}
	link, err := s.store.GitHubUserLink(c.Request.Context(), provider, subject)
	if errors.Is(err, store.ErrNotFound) {
		return "", "", errGitHubLinkRequired
	}
	if err != nil {
		return "", "", err
	}
	tok, err := s.githubAccessToken(c.Request.Context(), cl, link)
	return tok, link.GitHubLogin, err
}

// githubLinkAnswer answers a request that needed the person's GitHub: 409
// github_link_required, or 502.
func githubLinkAnswer(c *gin.Context, err error) {
	if errors.Is(err, errGitHubLinkRequired) {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": err.Error(), "code": githubLinkRequiredCode})
		return
	}
	abort(c, http.StatusBadGateway, fmt.Errorf("shpyrd could not reach GitHub with your account: %w. Try again in a minute", err))
}
