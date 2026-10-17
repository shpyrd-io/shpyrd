// Package github is shpyrd's GitHub App (RFC-0054): the App's JWT,
// installation tokens scoped to one repository, the repositories and
// branches an installation grants, the OAuth exchange that says which
// installations a person can see, and the conversion of a registration
// manifest. Every call the server makes dials through gitauth's checks.
package github

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// The App's Secret in the system namespace: written by
// `shpyrd-ctl github app create`, or by the cloud for its one public App.
const (
	SecretName       = "shpyrd-github-app"
	KeyAppID         = "app-id"
	KeySlug          = "slug"
	KeyPrivateKey    = "private-key"
	KeyWebhookSecret = "webhook-secret"
	KeyClientID      = "client-id"
	KeyClientSecret  = "client-secret"
	KeyHTMLURL       = "html-url"

	// EnvAPIURL names GitHub's API; DefaultAPIURL when unset.
	EnvAPIURL     = "SHPYRD_GITHUB_API_URL"
	DefaultAPIURL = "https://api.github.com"
)

var (
	// ErrNotConfigured: the platform has no GitHub App, or its Secret
	// cannot be used.
	ErrNotConfigured = errors.New("GitHub is not set up on this platform")
	// ErrRepoNotGranted: the installation does not include the repository.
	ErrRepoNotGranted = errors.New("the GitHub App's installation does not include this repository")
	// ErrPermissionsNotGranted: the installation has not approved the
	// checks, statuses or Deployments permissions.
	ErrPermissionsNotGranted = errors.New("the GitHub App installation has not approved the checks, statuses or Deployments permissions: accept the App's updated permissions on GitHub")
	// ErrInstallationGone: GitHub no longer knows the installation, most
	// likely because the App was uninstalled.
	ErrInstallationGone = errors.New("the GitHub App's installation is gone: the App was uninstalled")
)

// App is the App's credentials, as its Secret holds them.
type App struct {
	ID            int64
	Slug          string
	PrivateKey    string
	WebhookSecret string
	ClientID      string
	ClientSecret  string
	HTMLURL       string
}

// AppFromSecret reads the App's Secret. A Secret that lacks a key is no
// App: the error wraps ErrNotConfigured and names the key.
func AppFromSecret(sec *corev1.Secret) (App, error) {
	get := func(k string) string { return strings.TrimSpace(string(sec.Data[k])) }
	id, err := strconv.ParseInt(get(KeyAppID), 10, 64)
	if err != nil || id <= 0 {
		return App{}, fmt.Errorf("%w: the Secret %s lacks %s", ErrNotConfigured, SecretName, KeyAppID)
	}
	app := App{ID: id, Slug: get(KeySlug), PrivateKey: string(sec.Data[KeyPrivateKey]), WebhookSecret: get(KeyWebhookSecret),
		ClientID: get(KeyClientID), ClientSecret: get(KeyClientSecret), HTMLURL: strings.TrimSuffix(get(KeyHTMLURL), "/")}
	if err := app.validate(); err != nil {
		return App{}, fmt.Errorf("%w: the Secret %s %v", ErrNotConfigured, SecretName, err)
	}
	return app, nil
}

func (a App) validate() error {
	if a.ID <= 0 {
		return errors.New("lacks " + KeyAppID)
	}
	for _, k := range []struct{ name, value string }{
		{KeySlug, a.Slug}, {KeyPrivateKey, a.PrivateKey}, {KeyWebhookSecret, a.WebhookSecret},
		{KeyClientID, a.ClientID}, {KeyClientSecret, a.ClientSecret}, {KeyHTMLURL, a.HTMLURL},
	} {
		if strings.TrimSpace(k.value) == "" {
			return errors.New("lacks " + k.name)
		}
	}
	return nil
}

// SecretData is the App as its Secret's data.
func (a App) SecretData() map[string][]byte {
	return map[string][]byte{
		KeyAppID: []byte(strconv.FormatInt(a.ID, 10)), KeySlug: []byte(a.Slug), KeyPrivateKey: []byte(a.PrivateKey),
		KeyWebhookSecret: []byte(a.WebhookSecret), KeyClientID: []byte(a.ClientID), KeyClientSecret: []byte(a.ClientSecret),
		KeyHTMLURL: []byte(a.HTMLURL),
	}
}

// WebURL is the GitHub the App lives on, from its html-url: where people
// sign in, and where the OAuth exchange goes.
func (a App) WebURL() string {
	u, err := url.Parse(a.HTMLURL)
	if err != nil || u.Host == "" {
		return "https://github.com"
	}
	return u.Scheme + "://" + u.Host
}

// InstallURL is GitHub's page that installs the App on an account.
func (a App) InstallURL() string { return a.HTMLURL + "/installations/new" }
