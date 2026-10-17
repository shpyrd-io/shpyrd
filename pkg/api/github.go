package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/pages"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Connect GitHub (RFC-0054): a workspace admin sends the browser to
// GitHub with a signed state, to its authorize page (the App may be
// installed already) or its install page (a new account); GitHub sends it
// back to /api/github/installed, where the person's own OAuth token says
// which installations they own, and the one they chose is connected to the
// workspace the state names. Then anyone who makes projects lists its
// repositories and branches.

// GitHubView is the workspace's GitHub: whether the platform has an App,
// and the installations connected.
type GitHubView struct {
	Configured bool   `json:"configured"`
	AppURL     string `json:"appURL,omitempty"`
	// Note says why there is nothing to connect, when there is not.
	Note          string                   `json:"note,omitempty"`
	Installations []GitHubInstallationView `json:"installations"`
}

// GitHubInstallationView is one connection.
type GitHubInstallationView struct {
	ID          int64     `json:"id"`
	Account     string    `json:"account"`
	Type        string    `json:"type"`
	ConnectedBy string    `json:"connectedBy"`
	CreatedAt   time.Time `json:"createdAt"`
	Suspended   bool      `json:"suspended"`
	// ManageURL is the installation's settings on GitHub.
	ManageURL string `json:"manageURL"`
}

// GitHubConnectView is where to send the browser.
type GitHubConnectView struct {
	InstallURL string `json:"installURL"`
	// AuthorizeURL asks GitHub who the person is, then lists the
	// installations they own to connect: one that exists already.
	AuthorizeURL string `json:"authorizeURL"`
}

// GitHubRepositoryView is a repository a connection reaches.
type GitHubRepositoryView struct {
	Installation  int64  `json:"installation"`
	FullName      string `json:"fullName"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"defaultBranch"`
}

const (
	githubStateTTL = 15 * time.Minute
	githubReposTTL = 60 * time.Second
)

// githubState is what the install link carries through GitHub: the
// workspace, who started, until when, and a nonce that makes it single use.
type githubState struct {
	Workspace string `json:"ws"`
	Actor     string `json:"actor"`
	// Provider and Subject are who started, to the store: the finish
	// matches them, never the name (two identities may share an email).
	Provider string `json:"prov"`
	Subject  string `json:"sub"`
	Exp      int64  `json:"exp"`
	Nonce    string `json:"n"`
}

var (
	errGitHubState  = errors.New("the link is not one shpyrd made, or it expired")
	errGitHubTicket = errors.New("this connection was started by someone else, or has expired")
)

// githubTicketTTL is how long the browser has, from GitHub's callback, to
// reach the workspace's own address and finish.
const githubTicketTTL = 5 * time.Minute

// githubTicket is what the callback hands the workspace's finish page,
// once GitHub's checks passed: who started, what they connect, and who
// they are on GitHub.
type githubTicket struct {
	Workspace    string `json:"ws"`
	Actor        string `json:"actor"`
	Provider     string `json:"prov"`
	Subject      string `json:"sub"`
	Installation int64  `json:"installation"`
	Account      string `json:"account"`
	AccountType  string `json:"accountType"`
	Suspended    bool   `json:"suspended,omitempty"`
	GitHubLogin  string `json:"githubLogin"`
	Exp          int64  `json:"exp"`
	Nonce        string `json:"n"`
}

// githubTicketPrefix sets a ticket apart from a state: it starts the
// ticket and is under its tag, so neither passes as the other.
const githubTicketPrefix = "t."

func githubStateTag(key, body string) []byte {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(body))
	return mac.Sum(nil)
}

// githubSign is base64url JSON, a dot, and the HMAC-SHA256 of prefix and
// that body, keyed by the App's client secret; the prefix leads.
func githubSign(key, prefix string, v any) string {
	raw, _ := json.Marshal(v)
	body := base64.RawURLEncoding.EncodeToString(raw)
	return prefix + body + "." + base64.RawURLEncoding.EncodeToString(githubStateTag(key, prefix+body))
}

// githubRead checks the prefix and the tag, in constant time, and reads
// the body into v.
func githubRead(key, prefix, raw string, v any) bool {
	rest, ok := strings.CutPrefix(raw, prefix)
	if !ok || key == "" {
		return false
	}
	body, tag, ok := strings.Cut(rest, ".")
	if !ok {
		return false
	}
	got, err := base64.RawURLEncoding.DecodeString(tag)
	if err != nil || !hmac.Equal(got, githubStateTag(key, prefix+body)) {
		return false
	}
	js, err := base64.RawURLEncoding.DecodeString(body)
	return err == nil && json.Unmarshal(js, v) == nil
}

// signGitHubState signs a state; no prefix.
func signGitHubState(key string, st githubState) string { return githubSign(key, "", st) }

// readGitHubState checks the tag in constant time, then the time.
func readGitHubState(key, raw string, now time.Time) (githubState, error) {
	var st githubState
	if strings.HasPrefix(raw, githubTicketPrefix) || !githubRead(key, "", raw, &st) || st.Workspace == "" || st.Nonce == "" || now.Unix() > st.Exp {
		return githubState{}, errGitHubState
	}
	return st, nil
}

func signGitHubTicket(key string, tk githubTicket) string {
	return githubSign(key, githubTicketPrefix, tk)
}

// readGitHubTicket checks the tag in constant time, then the time.
func readGitHubTicket(key, raw string, now time.Time) (githubTicket, error) {
	var tk githubTicket
	if !githubRead(key, githubTicketPrefix, raw, &tk) || tk.Workspace == "" || tk.Nonce == "" || tk.Installation <= 0 || now.Unix() > tk.Exp {
		return githubTicket{}, errGitHubTicket
	}
	return tk, nil
}

// githubWho is who acts: their name as the audit log gives it (the
// state's actor and ConnectedBy, for display), and their provider and
// subject, which identify them (what the finish matches).
func githubWho(c *gin.Context) (actor, provider, subject string) {
	if id, ok := ext.IdentityFrom(c); ok {
		return firstNonEmpty(actorName(id), "anonymous"), id.Provider, id.Subject
	}
	return "anonymous", "", ""
}

// nonceSet remembers the states used until they expire. The server runs
// one replica.
type nonceSet struct {
	mu   sync.Mutex
	used map[string]time.Time
}

// use says whether the nonce was unused, and marks it used.
func (n *nonceSet) use(nonce string, until, now time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.used == nil {
		n.used = map[string]time.Time{}
	}
	for k, t := range n.used {
		if now.After(t) {
			delete(n.used, k)
		}
	}
	if _, ok := n.used[nonce]; ok {
		return false
	}
	n.used[nonce] = until
	return true
}

// githubRepoCache keeps each workspace's listing a minute: the picker
// asks on every keystroke's render, GitHub counts every call.
type githubRepoCache struct {
	mu sync.Mutex
	by map[string]githubRepoEntry
}

type githubRepoEntry struct {
	at   time.Time
	list []GitHubRepositoryView
}

func (g *githubRepoCache) get(ws string, now time.Time) ([]GitHubRepositoryView, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.by[ws]
	return e.list, ok && now.Sub(e.at) < githubReposTTL
}

func (g *githubRepoCache) put(ws string, list []GitHubRepositoryView, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.by == nil {
		g.by = map[string]githubRepoEntry{}
	}
	g.by[ws] = githubRepoEntry{at: now, list: list}
}

// forgetGitHubRepos drops a workspace's listing: its connections changed.
func (s *Server) forgetGitHubRepos(ws string) {
	s.githubRepos.mu.Lock()
	delete(s.githubRepos.by, ws)
	s.githubRepos.mu.Unlock()
}

// githubChanged tells the controllers a workspace's connections changed.
func (s *Server) githubChanged(ws string) {
	if s.opts.GitHubChanged != nil {
		s.opts.GitHubChanged(ws)
	}
}

// githubMissing says there is no App, and who sets one up on self-hosted.
func (s *Server) githubMissing() string {
	msg := "GitHub is not set up on this platform."
	if !s.hasCapability("workspaces") {
		msg += " An operator sets it up with `shpyrd-ctl github app create`."
	}
	return msg
}

// githubClient is the App's client, or a 409 when the platform has none.
func (s *Server) githubClient(c *gin.Context) (*github.Client, bool) {
	cl, err := s.githubApp.Client(c.Request.Context())
	if errors.Is(err, github.ErrNotConfigured) {
		abort(c, http.StatusConflict, errors.New(s.githubMissing()))
		return nil, false
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return nil, false
	}
	return cl, true
}

func githubTarget(id int64, account string) string {
	return fmt.Sprintf("github installation %d (%s)", id, account)
}

// githubManageURL is the installation's settings page on GitHub.
func githubManageURL(web string, inst store.GitHubInstallation) string {
	if inst.AccountType == "Organization" {
		return fmt.Sprintf("%s/organizations/%s/settings/installations/%d", web, url.PathEscape(inst.AccountLogin), inst.ID)
	}
	return fmt.Sprintf("%s/settings/installations/%d", web, inst.ID)
}

// githubConnect is POST /api/workspace/github/connect.
func (s *Server) githubConnect(c *gin.Context) {
	cl, ok := s.githubClient(c)
	if !ok {
		return
	}
	app := cl.App()
	actor, provider, subject := githubWho(c)
	state, err := newGitHubState(app, githubState{Workspace: s.doorWorkspace(c), Actor: actor, Provider: provider, Subject: subject}, time.Now().Add(githubStateTTL))
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	// No redirect_uri: GitHub uses the App's first callback URL, the one
	// registered, where a fresh install lands too; the dashboard's URL now
	// may differ from the one the App was registered with.
	authorize := url.Values{"client_id": {app.ClientID}, "state": {state}}
	c.JSON(http.StatusOK, GitHubConnectView{
		InstallURL:   githubInstallURL(app, state),
		AuthorizeURL: app.WebURL() + "/login/oauth/authorize?" + authorize.Encode(),
	})
}

// newGitHubState signs a fresh state for who started: a new nonce, valid
// until exp.
func newGitHubState(app github.App, who githubState, exp time.Time) (string, error) {
	nonce, err := randomHex(16)
	if err != nil {
		return "", err
	}
	who.Exp, who.Nonce = exp.Unix(), nonce
	return signGitHubState(app.ClientSecret, who), nil
}

// githubInstallURL is GitHub's install page, carrying a state.
func githubInstallURL(app github.App, state string) string {
	return app.InstallURL() + "?state=" + url.QueryEscape(state)
}

// githubView is GET /api/workspace/github.
func (s *Server) githubView(c *gin.Context) {
	ctx := c.Request.Context()
	view := GitHubView{Installations: []GitHubInstallationView{}}
	web := "https://github.com"
	cl, err := s.githubApp.Client(ctx)
	switch {
	case errors.Is(err, github.ErrNotConfigured):
		view.Note = s.githubMissing()
	case err != nil:
		abort(c, http.StatusBadGateway, err)
		return
	default:
		view.Configured, view.AppURL, web = true, cl.App().HTMLURL, cl.App().WebURL()
	}
	list, err := s.store.GitHubInstallations(ctx, s.doorWorkspace(c))
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	for _, inst := range list {
		view.Installations = append(view.Installations, GitHubInstallationView{
			ID: inst.ID, Account: inst.AccountLogin, Type: inst.AccountType, ConnectedBy: inst.ConnectedBy,
			CreatedAt: inst.CreatedAt, Suspended: inst.SuspendedAt != nil, ManageURL: githubManageURL(web, inst),
		})
	}
	c.JSON(http.StatusOK, view)
}

// githubDisconnect is DELETE /api/workspace/github/installations/:id: the
// workspace forgets it; the App stays installed on GitHub.
func (s *Server) githubDisconnect(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		abort(c, http.StatusBadRequest, errors.New("give the installation's id, a number: shpyrd github list shows it"))
		return
	}
	ctx := c.Request.Context()
	ws := s.doorWorkspace(c)
	account := ""
	list, err := s.store.GitHubInstallations(ctx, ws)
	if err != nil {
		s.log.Warn("github: could not look up the installation's account", "installation", id, "err", err.Error())
	}
	for _, inst := range list {
		if inst.ID == id {
			account = inst.AccountLogin
		}
	}
	err = s.store.DeleteGitHubInstallation(ctx, ws, id)
	if errors.Is(err, store.ErrNotFound) {
		abort(c, http.StatusNotFound, errors.New("this workspace has no such GitHub connection"))
		return
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	s.forgetGitHubRepos(ws)
	s.githubChanged(ws)
	s.audit(c, "", "github.disconnect", githubTarget(id, account), "workspace "+ws)
	c.Status(http.StatusNoContent)
}

// githubRepositories is GET /api/workspace/github/repositories: what the
// workspace's live connections reach, merged, sorted, kept a minute.
func (s *Server) githubRepositories(c *gin.Context) {
	cl, ok := s.githubClient(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	ws := s.doorWorkspace(c)
	now := time.Now()
	if list, ok := s.githubRepos.get(ws, now); ok {
		c.JSON(http.StatusOK, list)
		return
	}
	insts, err := s.store.GitHubInstallations(ctx, ws)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	out := []GitHubRepositoryView{}
	for _, inst := range insts {
		if inst.SuspendedAt != nil {
			continue
		}
		repos, err := cl.Repositories(ctx, inst.ID)
		if errors.Is(err, github.ErrInstallationGone) {
			// Uninstalled on GitHub, the webhook not yet heard: the other
			// connections still list.
			s.log.Warn("github: the installation is gone; listing the others", "installation", inst.ID, "account", inst.AccountLogin)
			continue
		}
		if err != nil {
			abort(c, http.StatusBadGateway, fmt.Errorf("GitHub did not list the repositories of %s: %w. Try again in a minute", inst.AccountLogin, err))
			return
		}
		for _, r := range repos {
			out = append(out, GitHubRepositoryView{Installation: r.Installation, FullName: r.FullName, Private: r.Private, DefaultBranch: r.DefaultBranch})
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].FullName) < strings.ToLower(out[j].FullName) })
	s.githubRepos.put(ws, out, now)
	c.JSON(http.StatusOK, out)
}

// githubBranches is GET /api/workspace/github/repositories/:owner/:repo/branches.
func (s *Server) githubBranches(c *gin.Context) {
	cl, ok := s.githubClient(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	owner, repo := c.Param("owner"), c.Param("repo")
	insts, err := s.store.GitHubInstallations(ctx, s.doorWorkspace(c))
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	for _, inst := range insts {
		if inst.SuspendedAt != nil || !strings.EqualFold(inst.AccountLogin, owner) {
			continue
		}
		names, err := cl.Branches(ctx, inst.ID, owner, repo)
		if errors.Is(err, github.ErrRepoNotGranted) {
			abort(c, http.StatusNotFound, fmt.Errorf("the GitHub connection on %s does not include %s: add it to the installation on GitHub", inst.AccountLogin, repo))
			return
		}
		if err != nil {
			abort(c, http.StatusBadGateway, fmt.Errorf("GitHub did not list the branches of %s/%s: %w", owner, repo, err))
			return
		}
		c.JSON(http.StatusOK, names)
		return
	}
	abort(c, http.StatusNotFound, fmt.Errorf("no GitHub connection of this workspace reaches %s", owner))
}

// githubInstalled is GET /api/github/installed, where GitHub sends the
// browser after an install (public: no session comes back with it). It
// makes every check GitHub can answer, then hands a ticket to the
// workspace's own address, where the session that started must finish.
func (s *Server) githubInstalled(c *gin.Context) {
	ctx := c.Request.Context()
	cl, err := s.githubApp.Client(ctx)
	if err != nil {
		s.githubPage(c, http.StatusNotFound, "GitHub is not set up here", "This platform has no GitHub App, so nothing was connected.", "")
		return
	}
	if c.Query("setup_action") == "request" {
		// A member asked an organisation's owners to install the App:
		// GitHub sends no installation until one approves.
		s.githubPage(c, http.StatusOK, "Waiting for an owner", "An owner of the organisation must approve the request on GitHub; then connect it from Workspace settings → Git.", "")
		return
	}
	if c.Query("installation_id") == "" && c.Query("code") != "" && c.Query("state") != "" {
		// Back from the authorize step: an installation that exists already.
		s.githubChoose(c, cl)
		return
	}
	id, err := strconv.ParseInt(c.Query("installation_id"), 10, 64)
	if err != nil || id <= 0 {
		s.githubPage(c, http.StatusBadRequest, "GitHub sent no installation", "Nothing was connected. Start again from the workspace's Git settings: Connect GitHub.", "")
		return
	}
	if c.Query("setup_action") == "update" {
		// The installation's repositories changed on GitHub: nothing to
		// connect, and no state comes from GitHub's own settings page. The
		// generic dashboard: which workspace has it is not said.
		c.Redirect(http.StatusFound, s.dashboardURLOf(nil))
		return
	}
	if c.Query("state") == "" {
		// Installed from GitHub's own page: no workspace asked for it.
		s.githubPage(c, http.StatusBadRequest, "Connect it from your workspace", "Connect GitHub from your workspace's settings (Workspace settings → Git) so shpyrd knows which workspace to connect it to.", "")
		return
	}
	now := time.Now()
	key := cl.App().ClientSecret
	st, err := readGitHubState(key, c.Query("state"), now)
	if err != nil {
		s.githubPage(c, http.StatusBadRequest, "This link has expired", "Nothing was connected. Start again from the workspace's Git settings: Connect GitHub.", "")
		return
	}
	back := s.gitSettingsURL(ctx, st.Workspace)
	if !s.githubNonces.use(st.Nonce, time.Unix(st.Exp, 0), now) {
		s.githubPage(c, http.StatusBadRequest, "This link was used already", "Each Connect GitHub link connects once. Start again from the workspace's Git settings.", back)
		return
	}
	code := c.Query("code")
	if code == "" {
		s.githubPage(c, http.StatusBadRequest, "GitHub did not say who you are", "Nothing was connected. Start again from the workspace's Git settings: Connect GitHub.", back)
		return
	}
	who, err := askGitHub(ctx, cl, code, id)
	if err != nil {
		s.log.Warn("github: could not check the person's installations", "err", err.Error())
		s.githubPage(c, http.StatusBadGateway, "GitHub did not answer", "shpyrd could not ask GitHub which installations you can see. Try again from the workspace's Git settings in a minute.", back)
		return
	}
	if who.inst == nil {
		s.githubPage(c, http.StatusForbidden, "Not an installation you can see",
			fmt.Sprintf("GitHub says your account cannot see installation %d. Install the App on an account or organisation you manage, then connect it.", id), back)
		return
	}
	if who.refused {
		s.githubPage(c, http.StatusForbidden, "GitHub refused",
			fmt.Sprintf("GitHub refused to say whether you own %s. If the organisation requires SAML single sign-on, authorize your GitHub session for it, then connect again.", who.inst.Account), back)
		return
	}
	if who.noMembers {
		s.githubPage(c, http.StatusConflict, "GitHub needs a new permission",
			fmt.Sprintf("This GitHub App can't check who owns %s yet: an owner of the App must accept its new Members permission on GitHub (Settings → GitHub Apps → shpyrd → Permissions).", who.inst.Account), back)
		return
	}
	if !who.owner {
		s.githubPage(c, http.StatusForbidden, "Not an owner", fmt.Sprintf("Only an owner of %s on GitHub can connect it.", who.inst.Account), back)
		return
	}
	if s.githubHeldElsewhere(c, id, who.inst.Account, st.Workspace, back) {
		return
	}
	nonce, err := randomHex(16)
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	tk := githubTicket{Workspace: st.Workspace, Actor: st.Actor, Provider: st.Provider, Subject: st.Subject, Installation: id, Account: who.inst.Account, AccountType: who.inst.AccountType,
		Suspended: who.inst.SuspendedAt != nil, GitHubLogin: who.login, Exp: now.Add(githubTicketTTL).Unix(), Nonce: nonce}
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, s.workspaceDashboardURL(ctx, st.Workspace)+"/api/workspace/github/finish?ticket="+url.QueryEscape(signGitHubTicket(key, tk)))
}

// githubHeldElsewhere refuses, with a page, an installation another
// workspace has: it is never moved silently.
func (s *Server) githubHeldElsewhere(c *gin.Context, id int64, account, ws, back string) bool {
	holder, ok := s.githubHolder(c, id, back)
	if !ok {
		return true
	}
	if holder != "" && holder != ws {
		s.githubHeldPage(c, account, holder, back)
		return true
	}
	return false
}

// githubHolder is the workspace that has the installation, "" for none;
// not ok, with a page, when the store cannot say.
func (s *Server) githubHolder(c *gin.Context, id int64, back string) (string, bool) {
	held, err := s.store.GitHubInstallation(c.Request.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && held == nil) {
		return "", true
	}
	if err != nil {
		s.log.Warn("github: could not look up the installation", "installation", id, "err", err.Error())
		s.githubPage(c, http.StatusBadGateway, "Not connected", "shpyrd could not check the connection. Try again from the workspace's Git settings in a minute.", back)
		return "", false
	}
	return held.Workspace, true
}

// githubHeldText says an installation is another workspace's.
func githubHeldText(account, ws string) string {
	return fmt.Sprintf("%s is connected to workspace %s: disconnect it there first.", account, ws)
}

func (s *Server) githubHeldPage(c *gin.Context, account, ws, back string) {
	s.githubPage(c, http.StatusConflict, "Connected elsewhere", githubHeldText(account, ws), back)
}

// githubFinish is GET /api/workspace/github/finish, on the workspace's
// own address: the ticket GitHub's callback minted, presented by the
// session that started the connection, connects the installation.
func (s *Server) githubFinish(c *gin.Context) {
	ctx := c.Request.Context()
	ws := s.doorWorkspace(c)
	back := s.gitSettingsURL(ctx, ws)
	refuse := func(status int) {
		s.githubPage(c, status, "Not connected", "This connection was started by someone else, or has expired; start it again from Workspace settings → Git.", back)
	}
	cl, err := s.githubApp.Client(ctx)
	if err != nil {
		refuse(http.StatusNotFound)
		return
	}
	now := time.Now()
	tk, err := readGitHubTicket(cl.App().ClientSecret, c.Query("ticket"), now)
	if err != nil {
		refuse(http.StatusBadRequest)
		return
	}
	_, provider, subject := githubWho(c)
	if tk.Workspace != ws || tk.Provider != provider || tk.Subject != subject {
		refuse(http.StatusForbidden)
		return
	}
	// Spent only once the right person presents it.
	if !s.githubTickets.use(tk.Nonce, time.Unix(tk.Exp, 0), now) {
		refuse(http.StatusBadRequest)
		return
	}
	if s.githubHeldElsewhere(c, tk.Installation, tk.Account, ws, back) {
		return
	}
	inst := store.GitHubInstallation{ID: tk.Installation, AccountLogin: tk.Account, AccountType: tk.AccountType, ConnectedBy: tk.Actor}
	if tk.Suspended {
		inst.SuspendedAt = &now
	}
	var held *store.GitHubHeldError
	if err := s.store.AddGitHubInstallation(ctx, ws, inst); errors.As(err, &held) {
		// Another workspace's finish won the race since the check above.
		s.githubHeldPage(c, tk.Account, held.Workspace, back)
		return
	} else if err != nil {
		s.log.Warn("github: could not record the installation", "installation", tk.Installation, "err", err.Error())
		s.githubPage(c, http.StatusBadGateway, "Not connected", "shpyrd could not record the connection. Try again from the workspace's Git settings in a minute.", back)
		return
	}
	s.forgetGitHubRepos(ws)
	s.githubChanged(ws)
	s.audit(c, "", "github.connect", githubTarget(tk.Installation, tk.Account), "workspace "+ws+", by GitHub user "+tk.GitHubLogin)
	c.Redirect(http.StatusFound, back)
}

// githubSignInFirst asks a browser with no credential to sign in and open
// the link again: the finish page is reached by a redirect from GitHub,
// and the sign-in never sends anyone back to an /api/ path.
func (s *Server) githubSignInFirst(c *gin.Context) {
	if s.opts.Token == "" && !s.opts.TokenDisabled {
		c.Next() // no authentication on this server
		return
	}
	if c.GetHeader("Authorization") == "" && c.GetHeader("X-Shpyrd-Token") == "" {
		if ok, _ := s.sessionAuth(c); !ok {
			name := "this workspace"
			if ws, err := s.tenant(c); err == nil && ws != nil {
				name = firstNonEmpty(ws.Name, ws.Slug, name)
			}
			c.Header("Cache-Control", "no-store")
			c.Data(http.StatusUnauthorized, "text/html; charset=utf-8", []byte(pages.HTML(pages.Nothing, pages.Page{
				Title: "Sign in first",
				Text:  fmt.Sprintf("Sign in to %s, then open this link again.", name),
				Links: []pages.Link{{Label: "Sign in", URL: "/"}, {Label: "Open this link again", URL: c.Request.URL.RequestURI()}},
			})))
			c.Abort()
			return
		}
	}
	c.Next()
}

// githubPerson is what GitHub says of the person behind an OAuth code:
// their login, the installation they chose if they can see it, and
// whether they administer its account.
type githubPerson struct {
	login string
	inst  *github.Installation
	githubOwnership
}

// githubOwnership is whether the person administers an installation's
// account, or why GitHub would not say.
type githubOwnership struct {
	owner bool
	// noMembers: the App cannot read org memberships, registered before
	// it asked members: read.
	noMembers bool
	// refused: GitHub refused to say, for another reason, most often the
	// organisation's SAML single sign-on.
	refused bool
}

// askGitHub asks GitHub, with the person's own token, who they are and
// whether they administer the installation's account: an owner of the
// org, or the user account itself. Their token never leaves this function.
func askGitHub(ctx context.Context, cl *github.Client, code string, id int64) (githubPerson, error) {
	var who githubPerson
	token, err := cl.ExchangeCode(ctx, code)
	if err != nil {
		return who, err
	}
	mine, err := cl.UserInstallations(ctx, token)
	if err != nil {
		return who, err
	}
	for i := range mine {
		if mine[i].ID == id {
			who.inst = &mine[i]
		}
	}
	if who.inst == nil {
		return who, nil
	}
	if who.login, err = cl.UserLogin(ctx, token); err != nil {
		return who, err
	}
	who.githubOwnership, err = githubOwns(ctx, cl, token, who.login, *who.inst)
	return who, err
}

// githubOwned is one of the person's installations and whether they own it.
type githubOwned struct {
	inst github.Installation
	githubOwnership
}

// askGitHubOwned asks GitHub, with the person's own token, who they are
// and which of the App's installations they can see they administer: the
// ones they own, and the ones GitHub would not say of. Their token never
// leaves this function.
func askGitHubOwned(ctx context.Context, cl *github.Client, code string) (string, []githubOwned, error) {
	token, err := cl.ExchangeCode(ctx, code)
	if err != nil {
		return "", nil, err
	}
	mine, err := cl.UserInstallations(ctx, token)
	if err != nil {
		return "", nil, err
	}
	login, err := cl.UserLogin(ctx, token)
	if err != nil {
		return "", nil, err
	}
	var out []githubOwned
	for _, inst := range mine {
		own, err := githubOwns(ctx, cl, token, login, inst)
		if err != nil {
			return "", nil, err
		}
		if own.owner || own.noMembers || own.refused {
			out = append(out, githubOwned{inst: inst, githubOwnership: own})
		}
	}
	return login, out, nil
}

// githubOwns asks whether login administers the installation's account:
// an owner of the org, or the user account itself.
func githubOwns(ctx context.Context, cl *github.Client, token, login string, inst github.Installation) (githubOwnership, error) {
	var own githubOwnership
	switch inst.AccountType {
	case "Organization":
		role, state, err := cl.OrgMembership(ctx, token, inst.Account)
		var apiErr *github.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden && !apiErr.RateLimited {
			// GitHub says which refusal it is only in the message.
			if strings.Contains(apiErr.Message, "Resource not accessible by integration") {
				own.noMembers = true
			} else {
				own.refused = true
			}
			return own, nil
		}
		if err != nil {
			return own, err
		}
		own.owner = role == "admin" && state == "active"
	case "User":
		own.owner = strings.EqualFold(login, inst.Account)
	}
	return own, nil
}

// githubChoose is the callback back from the authorize step, with no
// installation: the App may be installed already, and GitHub then never
// calls back from its install page. It checks the state, asks GitHub
// which installations the person owns, and lists them, each available one
// with its own ticket to the workspace's finish.
func (s *Server) githubChoose(c *gin.Context, cl *github.Client) {
	ctx := c.Request.Context()
	now := time.Now()
	app := cl.App()
	st, err := readGitHubState(app.ClientSecret, c.Query("state"), now)
	if err != nil {
		s.githubPage(c, http.StatusBadRequest, "This link has expired", "Nothing was connected. Start again from the workspace's Git settings: Connect GitHub.", "")
		return
	}
	back := s.gitSettingsURL(ctx, st.Workspace)
	if !s.githubNonces.use(st.Nonce, time.Unix(st.Exp, 0), now) {
		s.githubPage(c, http.StatusBadRequest, "This link was used already", "Each Connect GitHub link connects once. Connect GitHub again from Workspace settings → Git.", back)
		return
	}
	login, mine, err := askGitHubOwned(ctx, cl, c.Query("code"))
	if err != nil {
		s.log.Warn("github: could not list the person's installations", "err", err.Error())
		s.githubPage(c, http.StatusBadGateway, "GitHub did not answer", "shpyrd could not ask GitHub which installations you own. Try again from the workspace's Git settings in a minute.", back)
		return
	}
	// A new nonce, the original's time: the install link's state is valid
	// here too, and is never chained past the first one's expiry.
	state, err := newGitHubState(app, githubState{Workspace: st.Workspace, Actor: st.Actor, Provider: st.Provider, Subject: st.Subject}, time.Unix(st.Exp, 0))
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	install := pages.Link{Label: "Install the App on another account", URL: githubInstallURL(app, state)}
	if len(mine) == 0 {
		s.githubLinksPage(c, "Connect an installation", "You don't own an installation of this App yet: install it first.", []pages.Link{install})
		return
	}
	finish := s.workspaceDashboardURL(ctx, st.Workspace) + "/api/workspace/github/finish?ticket="
	var links []pages.Link
	says := []string{"The installations of the App you own on GitHub. Connect one to this workspace."}
	for _, o := range mine {
		name := fmt.Sprintf("%s (%s)", o.inst.Account, githubKind(o.inst.AccountType))
		if o.noMembers {
			says = append(says, name+": shpyrd can't check who owns it yet: an owner of the App must accept its new Members permission on GitHub.")
			continue
		}
		if o.refused {
			says = append(says, name+": GitHub refused to say whether you own it. If the organisation requires SAML single sign-on, authorize your GitHub session for it, then connect again.")
			continue
		}
		holder, ok := s.githubHolder(c, o.inst.ID, back)
		if !ok {
			return
		}
		switch holder {
		case st.Workspace:
			says = append(says, name+": connected.")
		default:
			says = append(says, githubHeldText(o.inst.Account, holder))
		case "":
			nonce, err := randomHex(16)
			if err != nil {
				abort(c, http.StatusInternalServerError, err)
				return
			}
			tk := githubTicket{Workspace: st.Workspace, Actor: st.Actor, Provider: st.Provider, Subject: st.Subject, Installation: o.inst.ID, Account: o.inst.Account,
				AccountType: o.inst.AccountType, Suspended: o.inst.SuspendedAt != nil, GitHubLogin: login, Exp: now.Add(githubTicketTTL).Unix(), Nonce: nonce}
			links = append(links, pages.Link{Label: "Connect " + name, URL: finish + url.QueryEscape(signGitHubTicket(app.ClientSecret, tk))})
		}
	}
	links = append(links, install, pages.Link{Label: "Back to the Git settings", URL: back})
	s.githubLinksPage(c, "Connect an installation", strings.Join(says, " "), links)
}

// githubKind is an account's type, in words.
func githubKind(accountType string) string {
	if accountType == "Organization" {
		return "organisation"
	}
	return "personal account"
}

// gitSettingsURL is the workspace's Git settings page; the console's
// dashboard when the workspace is unknown.
func (s *Server) gitSettingsURL(ctx context.Context, slug string) string {
	return s.workspaceDashboardURL(ctx, slug) + "/workspace/git"
}

// workspaceDashboardURL is the dashboard of a workspace by slug; the
// generic one when it is unknown.
func (s *Server) workspaceDashboardURL(ctx context.Context, slug string) string {
	var ws *store.Workspace
	if slug != "" {
		ws, _ = s.store.Workspace(ctx, slug)
	}
	return s.dashboardURLOf(ws)
}

// githubPage is what the browser GitHub sent back sees when nothing was
// connected: a plain page, the lock when it is refused.
func (s *Server) githubPage(c *gin.Context, status int, title, text, back string) {
	kind := pages.Nothing
	if status == http.StatusForbidden {
		kind = pages.NoAccess
	}
	var links []pages.Link
	if back != "" {
		links = []pages.Link{{Label: "Back to the Git settings", URL: back}}
	}
	c.Header("Cache-Control", "no-store")
	c.Data(status, "text/html; charset=utf-8", []byte(pages.HTML(kind, pages.Page{Title: title, Text: text, Links: links})))
}

// githubLinksPage is a page of choices: 200, never cached (its links
// carry tickets).
func (s *Server) githubLinksPage(c *gin.Context, title, text string, links []pages.Link) {
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(pages.HTML(pages.Nothing, pages.Page{Title: title, Text: text, Links: links})))
}

// githubHooksPerMinute is how many App webhook deliveries one address may
// make: every workspace's pushes come from GitHub's few hook addresses.
const githubHooksPerMinute = 3000
