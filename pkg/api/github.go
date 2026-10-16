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
// GitHub's install page with a signed state; GitHub sends it back to
// /api/github/installed, where the person's own OAuth token says which
// installations they can see, and the one they chose is connected to the
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
	Exp       int64  `json:"exp"`
	Nonce     string `json:"n"`
}

var errGitHubState = errors.New("the link is not one shpyrd made, or it expired")

func githubStateTag(key, body string) []byte {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(body))
	return mac.Sum(nil)
}

// signGitHubState is base64url JSON, a dot, and its HMAC-SHA256 keyed by
// the App's client secret.
func signGitHubState(key string, st githubState) string {
	raw, _ := json.Marshal(st)
	body := base64.RawURLEncoding.EncodeToString(raw)
	return body + "." + base64.RawURLEncoding.EncodeToString(githubStateTag(key, body))
}

// readGitHubState checks the tag in constant time, then the time.
func readGitHubState(key, raw string, now time.Time) (githubState, error) {
	body, tag, ok := strings.Cut(raw, ".")
	if !ok || key == "" {
		return githubState{}, errGitHubState
	}
	got, err := base64.RawURLEncoding.DecodeString(tag)
	if err != nil || !hmac.Equal(got, githubStateTag(key, body)) {
		return githubState{}, errGitHubState
	}
	js, err := base64.RawURLEncoding.DecodeString(body)
	var st githubState
	if err != nil || json.Unmarshal(js, &st) != nil || st.Workspace == "" || st.Nonce == "" || now.Unix() > st.Exp {
		return githubState{}, errGitHubState
	}
	return st, nil
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
	nonce, err := randomHex(16)
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	app := cl.App()
	// Who started: their email, or the subject (admin-token), as the
	// console users' AddedBy names them.
	actor := "anonymous"
	if id, ok := ext.IdentityFrom(c); ok {
		actor = firstNonEmpty(id.Email, id.Subject, actor)
	}
	st := githubState{Workspace: s.doorWorkspace(c), Actor: actor, Exp: time.Now().Add(githubStateTTL).Unix(), Nonce: nonce}
	c.JSON(http.StatusOK, GitHubConnectView{InstallURL: app.InstallURL() + "?state=" + url.QueryEscape(signGitHubState(app.ClientSecret, st))})
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
	if list, err := s.store.GitHubInstallations(ctx, ws); err == nil {
		for _, inst := range list {
			if inst.ID == id {
				account = inst.AccountLogin
			}
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
// browser after an install (public: no session comes back with it).
func (s *Server) githubInstalled(c *gin.Context) {
	ctx := c.Request.Context()
	cl, err := s.githubApp.Client(ctx)
	if err != nil {
		s.githubPage(c, http.StatusNotFound, "GitHub is not set up here", "This platform has no GitHub App, so nothing was connected.", "")
		return
	}
	id, err := strconv.ParseInt(c.Query("installation_id"), 10, 64)
	if err != nil || id <= 0 {
		s.githubPage(c, http.StatusBadRequest, "GitHub sent no installation", "Nothing was connected. Start again from the workspace's Git settings: Connect GitHub.", "")
		return
	}
	if c.Query("setup_action") == "update" {
		// The installation's repositories changed on GitHub: nothing to
		// connect, and no state comes from GitHub's own settings page.
		ws := ""
		if inst, err := s.store.GitHubInstallation(ctx, id); err == nil {
			ws = inst.Workspace
		}
		c.Redirect(http.StatusFound, s.gitSettingsURL(ctx, ws))
		return
	}
	now := time.Now()
	st, err := readGitHubState(cl.App().ClientSecret, c.Query("state"), now)
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
	mine, err := personsInstallations(ctx, cl, code)
	if err != nil {
		s.log.Warn("github: could not check the person's installations", "err", err.Error())
		s.githubPage(c, http.StatusBadGateway, "GitHub did not answer", "shpyrd could not ask GitHub which installations you can see. Try again from the workspace's Git settings in a minute.", back)
		return
	}
	var inst *github.Installation
	for i := range mine {
		if mine[i].ID == id {
			inst = &mine[i]
		}
	}
	if inst == nil {
		s.githubPage(c, http.StatusForbidden, "Not an installation you can see",
			fmt.Sprintf("GitHub says your account cannot see installation %d. Install the App on an account or organisation you manage, then connect it.", id), back)
		return
	}
	moved, err := s.store.AddGitHubInstallation(ctx, st.Workspace, store.GitHubInstallation{
		ID: id, AccountLogin: inst.Account, AccountType: inst.AccountType, ConnectedBy: st.Actor, SuspendedAt: inst.SuspendedAt,
	})
	if err != nil {
		s.log.Warn("github: could not record the installation", "installation", id, "err", err.Error())
		s.githubPage(c, http.StatusBadGateway, "Not connected", "shpyrd could not record the connection. Try again from the workspace's Git settings in a minute.", back)
		return
	}
	s.forgetGitHubRepos(st.Workspace)
	target := githubTarget(id, inst.Account)
	if moved != "" {
		s.forgetGitHubRepos(moved)
		s.auditActor(c, st.Actor, "github.move", target, "from workspace "+moved+" to workspace "+st.Workspace)
	}
	s.auditActor(c, st.Actor, "github.connect", target, "workspace "+st.Workspace)
	c.Redirect(http.StatusFound, back)
}

// personsInstallations are the App's installations the person behind the
// OAuth code can see. Their token only says who they are: it never leaves
// this function.
func personsInstallations(ctx context.Context, cl *github.Client, code string) ([]github.Installation, error) {
	token, err := cl.ExchangeCode(ctx, code)
	if err != nil {
		return nil, err
	}
	return cl.UserInstallations(ctx, token)
}

// gitSettingsURL is the workspace's Git settings page; the console's
// dashboard when the workspace is unknown.
func (s *Server) gitSettingsURL(ctx context.Context, slug string) string {
	var ws *store.Workspace
	if slug != "" {
		ws, _ = s.store.Workspace(ctx, slug)
	}
	return s.dashboardURLOf(ws) + "/workspace/git"
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

// githubHooksPerMinute is how many App webhook deliveries one address may
// make: every workspace's pushes come from GitHub's few hook addresses.
const githubHooksPerMinute = 3000
