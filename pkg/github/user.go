package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// A person's GitHub App user token (RFC-0054, GitHub access by person):
// it reads what both the person and the App may, nothing more. shpyrd
// keeps the refresh token sealed in its database and the access token in
// memory; both rotate on every refresh.

// UserToken is GitHub's answer to an exchange or a refresh.
type UserToken struct {
	Access           string
	AccessExpiresAt  time.Time
	Refresh          string
	RefreshExpiresAt time.Time
}

// User is who a user token belongs to.
type User struct {
	ID    int64
	Login string
}

var (
	// ErrNoRefreshToken: the App's user tokens do not expire, so GitHub
	// gives no refresh token, and shpyrd cannot act for the person later.
	ErrNoRefreshToken = errors.New("GitHub gave no refresh token: the GitHub App's user tokens do not expire. An owner of the App turns on “Expire user authorization tokens” in the App's settings on GitHub (Optional features), then you link again")
	// ErrUserTokenRefused: GitHub no longer takes the refresh token: it
	// expired after six months unused, was used already, or the person
	// revoked the App's authorization.
	ErrUserTokenRefused = errors.New("GitHub no longer accepts this GitHub authorization: it expired or was revoked")
)

// ExchangeUserCode turns the code GitHub gave the browser into the
// person's tokens.
func (c *Client) ExchangeUserCode(ctx context.Context, code string) (UserToken, error) {
	return c.userToken(ctx, map[string]string{"code": code})
}

// RefreshUserToken trades a refresh token, once, for new tokens.
func (c *Client) RefreshUserToken(ctx context.Context, refresh string) (UserToken, error) {
	return c.userToken(ctx, map[string]string{"grant_type": "refresh_token", "refresh_token": refresh})
}

func (c *Client) userToken(ctx context.Context, grant map[string]string) (UserToken, error) {
	grant["client_id"], grant["client_secret"] = c.app.ClientID, c.app.ClientSecret
	raw, _ := json.Marshal(grant)
	req, err := http.NewRequestWithContext(ctx, "POST", c.web+"/login/oauth/access_token", bytes.NewReader(raw))
	if err != nil {
		return UserToken{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "shpyrd")
	var res struct {
		AccessToken      string `json:"access_token"`
		ExpiresIn        int64  `json:"expires_in"`
		RefreshToken     string `json:"refresh_token"`
		RefreshExpiresIn int64  `json:"refresh_token_expires_in"`
		Error            string `json:"error"`
		Description      string `json:"error_description"`
	}
	if err := c.send(req, &res); err != nil {
		return UserToken{}, err
	}
	switch {
	case res.Error == "bad_refresh_token":
		return UserToken{}, ErrUserTokenRefused
	case res.AccessToken == "":
		return UserToken{}, fmt.Errorf("GitHub did not accept the sign-in: %s", firstNonEmpty(res.Description, res.Error, "it gave no token"))
	}
	now := c.now()
	t := UserToken{Access: res.AccessToken, Refresh: res.RefreshToken}
	if res.ExpiresIn > 0 {
		t.AccessExpiresAt = now.Add(time.Duration(res.ExpiresIn) * time.Second)
	}
	if res.RefreshExpiresIn > 0 {
		t.RefreshExpiresAt = now.Add(time.Duration(res.RefreshExpiresIn) * time.Second)
	}
	return t, nil
}

// UserProfile is who the person behind userToken is (GET /user).
func (c *Client) UserProfile(ctx context.Context, userToken string) (User, error) {
	var res struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if err := c.call(ctx, "GET", c.api+"/user", userToken, nil, &res); err != nil {
		return User{}, err
	}
	if res.Login == "" {
		return User{}, errors.New("GitHub did not say who the person is")
	}
	return User{ID: res.ID, Login: res.Login}, nil
}

// UserRepositories are the repositories of an installation that the
// person behind userToken can read (GET
// /user/installations/{id}/repositories); a 404 APIError for an
// installation they cannot see.
func (c *Client) UserRepositories(ctx context.Context, userToken string, installation int64) ([]Repository, error) {
	return c.repositoryPages(ctx, fmt.Sprintf("%s/user/installations/%d/repositories", c.api, installation), userToken, installation)
}

// UserRepository is a repository as the person behind userToken reads it
// (GET /repos/{owner}/{repo}): a 404 or 403 APIError when they cannot.
func (c *Client) UserRepository(ctx context.Context, userToken, owner, repo string) (Repository, error) {
	return c.repository(ctx, userToken, owner, repo)
}

// RepositoryAsApp is a repository as the installation reads it, with a
// token for the whole installation: one it includes, or any public one. A
// 404 APIError for a private one it does not include.
func (c *Client) RepositoryAsApp(ctx context.Context, installation int64, owner, repo string) (Repository, error) {
	tok, err := c.InstallationToken(ctx, installation, "", ContentsRead())
	if err != nil {
		return Repository{}, err
	}
	r, err := c.repository(ctx, tok.Value, owner, repo)
	r.Installation = installation
	return r, err
}

func (c *Client) repository(ctx context.Context, token, owner, repo string) (Repository, error) {
	var res struct {
		FullName      string `json:"full_name"`
		Private       bool   `json:"private"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := c.call(ctx, "GET", fmt.Sprintf("%s/repos/%s/%s", c.api, url.PathEscape(owner), url.PathEscape(repo)), token, nil, &res); err != nil {
		return Repository{}, err
	}
	return Repository{FullName: res.FullName, Private: res.Private, DefaultBranch: res.DefaultBranch}, nil
}

// UserBranches are a repository's branches as the person reads them.
func (c *Client) UserBranches(ctx context.Context, userToken, owner, repo string) ([]string, error) {
	return c.branches(ctx, userToken, owner, repo)
}

// RevokeUserToken revokes a person's user token at GitHub (DELETE
// /applications/{client_id}/token, the App's client id and secret as Basic
// credentials). A token GitHub no longer knows (404) is no error.
func (c *Client) RevokeUserToken(ctx context.Context, access string) error {
	raw, _ := json.Marshal(map[string]string{"access_token": access})
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.api+"/applications/"+url.PathEscape(c.app.ClientID)+"/token", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.app.ClientID, c.app.ClientSecret)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "shpyrd")
	if err := c.send(req, nil); err != nil && !IsNotFound(err) {
		return err
	}
	return nil
}
