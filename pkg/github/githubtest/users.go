package githubtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// People on the fake GitHub (RFC-0054, GitHub access by person): the
// default person (Code gives UserToken; UserLogin, UserID) and those
// AddUser adds, the GitHub App user tokens their codes give, refreshed,
// expired and revoked as GitHub does, and what each may read.

const (
	// UserID is the default person's GitHub id.
	UserID      = 1001
	defaultUser = "default"
)

// User is a person on the fake GitHub. Installations are the ids, among
// SetUserInstallations' list, they see (nil: all of it); Repos are the
// full names they can read in those installations (nil: every one).
type User struct {
	ID            int64
	Login         string
	Installations []int64
	Repos         []string
}

// userGrant is a user access token: whose, and until when.
type userGrant struct {
	handle string
	exp    time.Time
	// forever: the App's user tokens did not expire when it was issued.
	forever bool
}

// AddUser makes a person whose OAuth code is code.
func (f *Server) AddUser(code string, u User) {
	f.mu.Lock()
	defer f.mu.Unlock()
	handle := strings.ToLower(u.Login)
	f.users[handle] = &u
	f.codes[code] = handle
}

// SetExpiringUserTokens says whether the App's user tokens expire, as
// GitHub's default is: off, an exchange gives no refresh token.
func (f *Server) SetExpiringUserTokens(on bool) { f.mu.Lock(); f.noExpiring = !on; f.mu.Unlock() }

// ExpireUserTokens makes every user access token issued so far expired.
func (f *Server) ExpireUserTokens() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for tok, g := range f.access {
		if g.forever {
			continue
		}
		g.exp = f.clock()
		f.access[tok] = g
	}
}

// ForgetRefreshTokens refuses every refresh token issued so far, as GitHub
// does after six months unused or once the person revoked the App.
func (f *Server) ForgetRefreshTokens() { f.mu.Lock(); f.refresh = map[string]string{}; f.mu.Unlock() }

// Revoked are the user tokens revoked through
// DELETE /applications/{client_id}/token, in order.
func (f *Server) Revoked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.revoked...)
}

// SetBranches are a repository's branches, in the order GitHub lists them.
func (f *Server) SetBranches(fullName string, names ...string) {
	f.mu.Lock()
	f.branches[strings.ToLower(fullName)] = names
	f.mu.Unlock()
}

// SetUserOrgRole sets the membership of org of the person with login (the
// default person's are SetOrgRole's); role "" is none.
func (f *Server) SetUserOrgRole(login, org, role, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	handle := strings.ToLower(login)
	if f.userRoles[handle] == nil {
		f.userRoles[handle] = map[string][2]string{}
	}
	if role == "" {
		delete(f.userRoles[handle], strings.ToLower(org))
		return
	}
	f.userRoles[handle][strings.ToLower(org)] = [2]string{role, state}
}

// ForbidRepo makes a repository answer 403 to the person with login, as
// for an organisation's SAML enforcement, whatever else they may read.
func (f *Server) ForbidRepo(login, fullName string) {
	f.mu.Lock()
	f.forbiddenRepos[strings.ToLower(login)+"|"+strings.ToLower(fullName)] = true
	f.mu.Unlock()
}

// ForbidInstallation makes the repositories of an installation answer 403
// to the person with login, as for an organisation's SAML enforcement.
func (f *Server) ForbidInstallation(login string, installation int64) {
	f.mu.Lock()
	f.forbiddenInstalls[strings.ToLower(login)+"|"+strconv.FormatInt(installation, 10)] = true
	f.mu.Unlock()
}

// SetPublicRepos are public repositories outside the installations:
// anyone reads them.
func (f *Server) SetPublicRepos(repos ...Repo) { f.mu.Lock(); f.public = repos; f.mu.Unlock() }

// userOf is the person whose live user token the request carries.
func (f *Server) userOf(r *http.Request) (*User, bool) {
	g, ok := f.grantOf(r)
	if !ok {
		return nil, false
	}
	u, ok := f.users[g.handle]
	return u, ok
}

// grantOf is the live user access token the request carries.
func (f *Server) grantOf(r *http.Request) (userGrant, bool) {
	g, ok := f.access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	if !ok || (!g.forever && !f.clock().Before(g.exp)) {
		return userGrant{}, false
	}
	return g, true
}

// repoRefused answers 403 when the request's person is refused the repository.
func (f *Server) repoRefused(w http.ResponseWriter, r *http.Request, fullName string) bool {
	u, ok := f.userOf(r)
	if !ok || !f.forbiddenRepos[strings.ToLower(u.Login)+"|"+strings.ToLower(fullName)] {
		return false
	}
	reply(w, http.StatusForbidden, map[string]string{"message": "Resource protected by organization SAML enforcement."})
	return true
}

func (f *Server) installationsOf(u *User) []Installation {
	if u.Installations == nil {
		return f.people
	}
	var out []Installation
	for _, i := range f.people {
		if slices.Contains(u.Installations, i.ID) {
			out = append(out, i)
		}
	}
	return out
}

func (f *Server) sees(u *User, id int64) bool {
	return slices.ContainsFunc(f.installationsOf(u), func(i Installation) bool { return i.ID == id })
}

func readsRepo(u *User, fullName string) bool {
	return u.Repos == nil || slices.ContainsFunc(u.Repos, func(n string) bool { return strings.EqualFold(n, fullName) })
}

func repoNamed(repos []Repo, fullName string) (Repo, bool) {
	for _, r := range repos {
		if strings.EqualFold(r.FullName, fullName) {
			return r, true
		}
	}
	return Repo{}, false
}

// publicRepo is a public repository: one SetPublicRepos named, or one an
// installation has that is not private.
func (f *Server) publicRepo(fullName string) (Repo, bool) {
	if r, ok := repoNamed(f.public, fullName); ok {
		return r, true
	}
	for _, repos := range f.repos {
		if r, ok := repoNamed(repos, fullName); ok && !r.Private {
			return r, true
		}
	}
	return Repo{}, false
}

// covers says whether an installation token, asked for some repositories,
// reads this one.
func (g tokenGrant) covers(fullName string) bool {
	return len(g.repos) == 0 || slices.ContainsFunc(g.repos, func(n string) bool {
		_, name, _ := strings.Cut(fullName, "/")
		return strings.EqualFold(n, name)
	})
}

// readable is the repository as the request's token reads it: an
// installation token, one its installation has; a user token, one of an
// installation they see that they may read; either, a public one.
func (f *Server) readable(r *http.Request, fullName string) (Repo, bool) {
	if g, ok := f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]; ok {
		if repo, ok := repoNamed(f.repos[g.installation], fullName); ok && g.covers(fullName) {
			return repo, true
		}
		return f.publicRepo(fullName)
	}
	u, ok := f.userOf(r)
	if !ok {
		return Repo{}, false
	}
	for _, i := range f.installationsOf(u) {
		if repo, ok := repoNamed(f.repos[i.ID], fullName); ok && readsRepo(u, fullName) {
			return repo, true
		}
	}
	return f.publicRepo(fullName)
}

// exchange is POST /login/oauth/access_token: a code, or a refresh token
// taken once.
func (f *Server) exchange(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		Code         string `json:"code"`
		GrantType    string `json:"grant_type"`
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.ClientID != ClientID || in.ClientSecret != ClientSecret {
		reply(w, http.StatusOK, map[string]string{"error": "incorrect_client_credentials", "error_description": "The client_id and/or client_secret passed are incorrect."})
		return
	}
	var handle string
	if in.GrantType == "refresh_token" {
		h, ok := f.refresh[in.RefreshToken]
		if !ok {
			reply(w, http.StatusOK, map[string]string{"error": "bad_refresh_token", "error_description": "The refresh token passed is incorrect or expired."})
			return
		}
		delete(f.refresh, in.RefreshToken)
		handle = h
	} else {
		h, ok := f.codes[in.Code]
		if !ok {
			reply(w, http.StatusOK, map[string]string{"error": "bad_verification_code", "error_description": "The code passed is incorrect or expired."})
			return
		}
		handle = h
	}
	f.issued++
	access := fmt.Sprintf("ghu_%s_%d", handle, f.issued)
	if handle == defaultUser && in.GrantType == "" {
		access = UserToken
	}
	f.access[access] = userGrant{handle: handle, exp: f.clock().Add(8 * time.Hour), forever: f.noExpiring}
	out := map[string]any{"access_token": access, "token_type": "bearer"}
	if !f.noExpiring {
		refresh := fmt.Sprintf("ghr_%s_%d", handle, f.issued)
		f.refresh[refresh] = handle
		out["expires_in"], out["refresh_token"], out["refresh_token_expires_in"] = 28800, refresh, 15897600
	}
	reply(w, http.StatusOK, out)
}

func (f *Server) userInstallations(w http.ResponseWriter, r *http.Request) {
	u, ok := f.userOf(r)
	if !ok {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	mine := f.installationsOf(u)
	var out []map[string]any
	for _, i := range pageOf(mine, r) {
		var suspended any
		if i.Suspended {
			suspended = f.clock().UTC().Format(time.RFC3339)
		}
		out = append(out, map[string]any{"id": i.ID, "account": map[string]string{"login": i.Login, "type": i.Type}, "suspended_at": suspended})
	}
	reply(w, http.StatusOK, map[string]any{"total_count": len(mine), "installations": out})
}

// userInstallationRepos is GET /user/installations/{id}/repositories.
func (f *Server) userInstallationRepos(w http.ResponseWriter, r *http.Request, idText string) {
	u, ok := f.userOf(r)
	if !ok {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	id, _ := strconv.ParseInt(idText, 10, 64)
	if !f.sees(u, id) {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	if f.forbiddenInstalls[strings.ToLower(u.Login)+"|"+idText] {
		reply(w, http.StatusForbidden, map[string]string{"message": "Resource protected by organization SAML enforcement."})
		return
	}
	var mine []Repo
	for _, repo := range f.repos[id] {
		if readsRepo(u, repo.FullName) {
			mine = append(mine, repo)
		}
	}
	var out []map[string]any
	for _, repo := range pageOf(mine, r) {
		out = append(out, map[string]any{"full_name": repo.FullName, "private": repo.Private, "default_branch": repo.DefaultBranch})
	}
	reply(w, http.StatusOK, map[string]any{"total_count": len(mine), "repositories": out})
}

func (f *Server) user(w http.ResponseWriter, r *http.Request) {
	u, ok := f.userOf(r)
	if !ok {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	reply(w, http.StatusOK, map[string]any{"login": u.Login, "id": u.ID, "type": "User"})
}

// repoInfo is GET /repos/{owner}/{repo}.
func (f *Server) repoInfo(w http.ResponseWriter, r *http.Request, fullName string) {
	if f.repoRefused(w, r, fullName) {
		return
	}
	repo, ok := f.readable(r, fullName)
	if !ok {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	reply(w, http.StatusOK, map[string]any{"full_name": repo.FullName, "private": repo.Private, "default_branch": repo.DefaultBranch})
}

// revoke is DELETE /applications/{client_id}/token.
func (f *Server) revoke(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := r.BasicAuth()
	if !ok || id != ClientID || secret != ClientSecret {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	var in struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if _, ok := f.access[in.AccessToken]; !ok {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	delete(f.access, in.AccessToken)
	f.revoked = append(f.revoked, in.AccessToken)
	w.WriteHeader(http.StatusNoContent)
}
