package github

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

const (
	callTimeout = 20 * time.Second
	// maxBody caps what one answer of GitHub may be.
	maxBody  = 8 << 20
	perPage  = 100
	maxPages = 100
	// A token is kept until 10 minutes before it expires, or 50 minutes
	// after it was minted, whichever comes first: a build reading it
	// never gets one in its last 10 minutes.
	tokenMargin = 10 * time.Minute
	tokenLife   = 50 * time.Minute
)

// Options says where GitHub is and how to reach it.
type Options struct {
	// APIURL is GitHub's API, DefaultAPIURL when empty.
	APIURL string
	// WebURL is where the OAuth exchange goes; empty is the App's WebURL.
	WebURL string
	// HTTP makes the calls; nil is gitauth.SafeHTTPClient. Tests pass the
	// fake GitHub's own client.
	HTTP *http.Client
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Client speaks to GitHub as the App.
type Client struct {
	app  App
	key  *rsa.PrivateKey
	api  string
	web  string
	http *http.Client
	now  func() time.Time

	mu     sync.Mutex
	tokens map[tokenKey]Token
}

type tokenKey struct {
	installation int64
	repo         string
}

// Token is an installation token: for one repository, or for the whole
// installation when it was asked for none.
type Token struct {
	Value     string
	ExpiresAt time.Time
	MintedAt  time.Time
}

func (t Token) renewAt() time.Time {
	byExpiry, byAge := t.ExpiresAt.Add(-tokenMargin), t.MintedAt.Add(tokenLife)
	if byAge.Before(byExpiry) {
		return byAge
	}
	return byExpiry
}

// Repository is a repository an installation grants.
type Repository struct {
	Installation  int64
	FullName      string
	Private       bool
	DefaultBranch string
}

// Installation is an installation of the App a person can see.
type Installation struct {
	ID          int64
	Account     string
	AccountType string
	SuspendedAt *time.Time
}

// APIError is GitHub refusing a call: its status and its words.
type APIError struct {
	Status  int
	Message string
	// RetryAfter is how long GitHub asks to wait before the next call:
	// its Retry-After, or the reset of a spent rate limit. 0 when it
	// said neither.
	RetryAfter time.Duration
}

func (e *APIError) Error() string { return fmt.Sprintf("GitHub answered %d: %s", e.Status, e.Message) }

// New makes a client of the App. A private key that is not an RSA key in
// PEM is no App (ErrNotConfigured).
func New(app App, opts Options) (*Client, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(app.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("%w: the Secret %s's %s is not an RSA key in PEM", ErrNotConfigured, SecretName, KeyPrivateKey)
	}
	c := &Client{
		app: app, key: key,
		api:  strings.TrimSuffix(firstNonEmpty(opts.APIURL, DefaultAPIURL), "/"),
		web:  strings.TrimSuffix(firstNonEmpty(opts.WebURL, app.WebURL()), "/"),
		http: opts.HTTP, now: opts.Now, tokens: map[tokenKey]Token{},
	}
	if c.http == nil {
		c.http = gitauth.SafeHTTPClient(callTimeout)
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c, nil
}

// App is the App this client speaks as.
func (c *Client) App() App { return c.app }

// AppJWT is the App's own credential: RS256, issued a minute ago against
// clock drift, valid nine minutes (GitHub takes ten at most).
func (c *Client) AppJWT() (string, error) {
	now := c.now()
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
		Issuer:    strconv.FormatInt(c.app.ID, 10),
	}
	return jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(c.key)
}

// InstallationToken is a token of the installation that reads one
// repository (by its name, without the owner), or lists the whole
// installation when repo is "". It is kept and given again until it ages.
// A repository the installation does not include is ErrRepoNotGranted; an
// installation GitHub no longer knows is ErrInstallationGone.
func (c *Client) InstallationToken(ctx context.Context, installation int64, repo string) (Token, error) {
	key := tokenKey{installation, strings.ToLower(repo)}
	c.mu.Lock()
	if t, ok := c.tokens[key]; ok && c.now().Before(t.renewAt()) {
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()
	bearer, err := c.AppJWT()
	if err != nil {
		return Token{}, err
	}
	body := map[string]any{"permissions": map[string]string{"contents": "read"}}
	if repo != "" {
		body["repositories"] = []string{repo}
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	err = c.call(ctx, "POST", fmt.Sprintf("%s/app/installations/%d/access_tokens", c.api, installation), bearer, body, &out)
	// GitHub answers 404 when the installation is gone (the App was
	// uninstalled), and 422 when it does not include the repository.
	if apiErr, ok := err.(*APIError); ok {
		switch {
		case apiErr.Status == http.StatusNotFound:
			return Token{}, fmt.Errorf("%w: %w", ErrInstallationGone, apiErr)
		case apiErr.Status == http.StatusUnprocessableEntity && repo != "":
			return Token{}, ErrRepoNotGranted
		}
	}
	if err != nil {
		return Token{}, err
	}
	t := Token{Value: out.Token, ExpiresAt: out.ExpiresAt, MintedAt: c.now()}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, old := range c.tokens {
		if !t.MintedAt.Before(old.ExpiresAt) {
			delete(c.tokens, k)
		}
	}
	c.tokens[key] = t
	return t, nil
}

// Repositories pages through everything the installation grants.
func (c *Client) Repositories(ctx context.Context, installation int64) ([]Repository, error) {
	tok, err := c.InstallationToken(ctx, installation, "")
	if err != nil {
		return nil, err
	}
	out := []Repository{}
	for page := 1; page <= maxPages; page++ {
		var res struct {
			Total        int `json:"total_count"`
			Repositories []struct {
				FullName      string `json:"full_name"`
				Private       bool   `json:"private"`
				DefaultBranch string `json:"default_branch"`
			} `json:"repositories"`
		}
		if err := c.call(ctx, "GET", fmt.Sprintf("%s/installation/repositories?per_page=%d&page=%d", c.api, perPage, page), tok.Value, nil, &res); err != nil {
			return nil, err
		}
		for _, r := range res.Repositories {
			out = append(out, Repository{Installation: installation, FullName: r.FullName, Private: r.Private, DefaultBranch: r.DefaultBranch})
		}
		if len(res.Repositories) == 0 || len(out) >= res.Total {
			break
		}
	}
	return out, nil
}

// Branches are a repository's branch names, as GitHub orders them.
func (c *Client) Branches(ctx context.Context, installation int64, owner, repo string) ([]string, error) {
	tok, err := c.InstallationToken(ctx, installation, repo)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for page := 1; page <= 10; page++ {
		var res []struct {
			Name string `json:"name"`
		}
		endpoint := fmt.Sprintf("%s/repos/%s/%s/branches?per_page=%d&page=%d", c.api, url.PathEscape(owner), url.PathEscape(repo), perPage, page)
		if err := c.call(ctx, "GET", endpoint, tok.Value, nil, &res); err != nil {
			return nil, err
		}
		for _, b := range res {
			out = append(out, b.Name)
		}
		if len(res) < perPage {
			break
		}
	}
	return out, nil
}

// ExchangeCode turns the code GitHub gave the browser into the person's
// own token, which only says who they are: the caller drops it once it has
// asked.
func (c *Client) ExchangeCode(ctx context.Context, code string) (string, error) {
	raw, _ := json.Marshal(map[string]string{"client_id": c.app.ClientID, "client_secret": c.app.ClientSecret, "code": code})
	req, err := http.NewRequestWithContext(ctx, "POST", c.web+"/login/oauth/access_token", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "shpyrd")
	var res struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := c.send(req, &res); err != nil {
		return "", err
	}
	if res.AccessToken == "" {
		return "", fmt.Errorf("GitHub did not accept the sign-in: %s", firstNonEmpty(res.Description, res.Error, "it gave no token"))
	}
	return res.AccessToken, nil
}

// UserInstallations are the installations of the App the person behind
// userToken can see.
func (c *Client) UserInstallations(ctx context.Context, userToken string) ([]Installation, error) {
	out := []Installation{}
	for page := 1; page <= 10; page++ {
		var res struct {
			Total         int `json:"total_count"`
			Installations []struct {
				ID      int64 `json:"id"`
				Account struct {
					Login string `json:"login"`
					Type  string `json:"type"`
				} `json:"account"`
				SuspendedAt *time.Time `json:"suspended_at"`
			} `json:"installations"`
		}
		if err := c.call(ctx, "GET", fmt.Sprintf("%s/user/installations?per_page=%d&page=%d", c.api, perPage, page), userToken, nil, &res); err != nil {
			return nil, err
		}
		for _, i := range res.Installations {
			out = append(out, Installation{ID: i.ID, Account: i.Account.Login, AccountType: i.Account.Type, SuspendedAt: i.SuspendedAt})
		}
		if len(res.Installations) == 0 || len(out) >= res.Total {
			break
		}
	}
	return out, nil
}

// UserLogin is the login of the person behind userToken (GET /user).
func (c *Client) UserLogin(ctx context.Context, userToken string) (string, error) {
	var res struct {
		Login string `json:"login"`
	}
	if err := c.call(ctx, "GET", c.api+"/user", userToken, nil, &res); err != nil {
		return "", err
	}
	if res.Login == "" {
		return "", errors.New("GitHub did not say who the person is")
	}
	return res.Login, nil
}

// OrgMembership is the person's role (admin or member) and the state
// (active or pending) of their membership of org, through
// GET /user/memberships/orgs/{org}. Not a member is role "" and no error.
func (c *Client) OrgMembership(ctx context.Context, userToken, org string) (role, state string, err error) {
	var res struct {
		Role  string `json:"role"`
		State string `json:"state"`
	}
	err = c.call(ctx, "GET", c.api+"/user/memberships/orgs/"+url.PathEscape(org), userToken, nil, &res)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	return res.Role, res.State, nil
}

// Registration is a GitHub App GitHub created from a manifest.
type Registration struct {
	App   App
	Name  string
	Owner string
}

// ConvertManifest asks GitHub for the credentials of the App it created
// from a manifest (no authentication: the code is the credential, once).
// hc nil is a plain client with a timeout: this runs on the operator's
// machine.
func ConvertManifest(ctx context.Context, apiURL string, hc *http.Client, code string) (Registration, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	c := &Client{api: strings.TrimSuffix(firstNonEmpty(apiURL, DefaultAPIURL), "/"), http: hc, now: time.Now}
	var res struct {
		ID    int64  `json:"id"`
		Slug  string `json:"slug"`
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		HTMLURL       string `json:"html_url"`
		ClientID      string `json:"client_id"`
		ClientSecret  string `json:"client_secret"`
		WebhookSecret string `json:"webhook_secret"`
		PEM           string `json:"pem"`
	}
	if err := c.call(ctx, "POST", c.api+"/app-manifests/"+url.PathEscape(code)+"/conversions", "", nil, &res); err != nil {
		return Registration{}, err
	}
	app := App{ID: res.ID, Slug: res.Slug, PrivateKey: res.PEM, WebhookSecret: res.WebhookSecret,
		ClientID: res.ClientID, ClientSecret: res.ClientSecret, HTMLURL: strings.TrimSuffix(res.HTMLURL, "/")}
	if err := app.validate(); err != nil {
		return Registration{}, fmt.Errorf("GitHub's answer %v", err)
	}
	return Registration{App: app, Name: res.Name, Owner: res.Owner.Login}, nil
}

// call sends a JSON request to GitHub's API.
func (c *Client) call(ctx context.Context, method, endpoint, bearer string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "shpyrd")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return c.send(req, out)
}

// send makes the call and reads the answer; a status that is not 2xx is
// an APIError with GitHub's words.
func (c *Client) send(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		return &APIError{Status: resp.StatusCode, Message: firstNonEmpty(e.Message, http.StatusText(resp.StatusCode)), RetryAfter: c.retryAfter(resp.Header)}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// retryAfter reads Retry-After (seconds), else X-RateLimit-Reset when
// the quota is spent.
func (c *Client) retryAfter(h http.Header) time.Duration {
	if secs, err := strconv.Atoi(h.Get("Retry-After")); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if h.Get("X-RateLimit-Remaining") == "0" {
		if reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			if d := time.Unix(reset, 0).Sub(c.now()); d > 0 {
				return d
			}
		}
	}
	return 0
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
