package cli

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// The cluster's GitHub App (RFC-0054). `shpyrd-ctl github app create`
// registers it on GitHub from a manifest, in one click in the browser, and
// keeps its credentials in the Secret shpyrd-system/shpyrd-github-app,
// which the server reads. The server has no registration endpoint.

var (
	// ctlKube connects to the cluster the kubeconfig names; tests replace it.
	ctlKube = func(g *globalFlags) (*kube.Client, error) {
		return kube.Connect(kube.Options{Kubeconfig: g.kubeconfig, Context: g.kubeCtx})
	}
	// browse opens a page in the person's browser; tests replace it.
	browse = openBrowser
	// manifestWait is how long the registration waits for GitHub.
	manifestWait = 10 * time.Minute
)

// githubNameLimit is the longest name GitHub takes for an App.
const githubNameLimit = 34

// appManifest is what GitHub creates an App from.
type appManifest struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	Description    string `json:"description"`
	HookAttributes struct {
		URL    string `json:"url"`
		Active bool   `json:"active"`
	} `json:"hook_attributes"`
	RedirectURL           string            `json:"redirect_url"`
	CallbackURLs          []string          `json:"callback_urls"`
	SetupURL              string            `json:"setup_url"`
	Public                bool              `json:"public"`
	RequestOAuthOnInstall bool              `json:"request_oauth_on_install"`
	SetupOnUpdate         bool              `json:"setup_on_update"`
	DefaultPermissions    map[string]string `json:"default_permissions"`
	DefaultEvents         []string          `json:"default_events"`
}

// newAppManifest is the cluster's App: its webhook and its install
// callback at the dashboard; every permission part 4 needs asked for now,
// so installations never approve again. With request_oauth_on_install
// GitHub sends the browser to callback_urls[0]; setup_url is the same.
func newAppManifest(dashboard, redirect, name string, public bool) appManifest {
	m := appManifest{
		Name:                  appName(dashboard, name),
		URL:                   dashboard,
		Description:           "Builds and deploys this account's repositories on shpyrd at " + dashboard + ".",
		RedirectURL:           redirect,
		SetupURL:              dashboard + "/api/github/installed",
		Public:                public,
		RequestOAuthOnInstall: true,
		SetupOnUpdate:         true,
		DefaultPermissions:    map[string]string{"contents": "read", "metadata": "read", "checks": "write", "deployments": "write", "statuses": "read"},
		DefaultEvents:         []string{"push", "installation_repositories", "check_suite", "status"},
	}
	m.HookAttributes.URL, m.HookAttributes.Active = dashboard+"/api/github/webhook", true
	m.CallbackURLs = []string{m.SetupURL}
	return m
}

// appName is --name, or shpyrd-<dashboard host> cut to GitHub's limit.
func appName(dashboard, name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	host := dashboard
	if u, err := url.Parse(dashboard); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	n := "shpyrd-" + host
	if len(n) > githubNameLimit {
		n = n[:githubNameLimit]
	}
	return strings.TrimRight(n, ".-")
}

func newCtlGitHubCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github",
		Short: "The cluster's GitHub App, which workspaces connect",
	}
	app := &cobra.Command{
		Use:   "app",
		Short: "Register, show or remove the cluster's GitHub App",
		Long: `Workspaces connect GitHub through one App per cluster. Register it once:

  shpyrd-ctl github app create [--org ORG] [--name NAME] [--public] [--url DASHBOARD_URL]

A browser opens on GitHub with the App filled in; one click creates it, and
its credentials go into the Secret shpyrd-system/shpyrd-github-app.

  shpyrd-ctl github app show      its slug, id and page on GitHub (never its keys)
  shpyrd-ctl github app delete    remove the Secret (then delete the App on GitHub)`,
	}
	var org, name, dashboard string
	var public bool
	create := &cobra.Command{
		Use:   "create",
		Short: "Register the cluster's GitHub App on GitHub, in one click",
		RunE: func(cmd *cobra.Command, args []string) error {
			return createGitHubApp(cmd, g, org, name, dashboard, public)
		},
	}
	create.Flags().StringVar(&org, "org", "", "register the App under this GitHub organisation (default: your own account)")
	create.Flags().StringVar(&name, "name", "", "the App's name on GitHub (default: shpyrd-<dashboard host>)")
	create.Flags().StringVar(&dashboard, "url", "", "the dashboard's public URL (default: the cluster's, from its install settings)")
	create.Flags().BoolVar(&public, "public", false, "let any account install the App, not only its owner")

	show := &cobra.Command{
		Use:   "show",
		Short: "Show the cluster's GitHub App: slug, id and page (never its keys)",
		RunE: func(cmd *cobra.Command, args []string) error {
			sec, err := githubAppSecret(g)
			if err != nil {
				return err
			}
			v := map[string]string{"slug": string(sec.Data[github.KeySlug]), "id": string(sec.Data[github.KeyAppID]), "url": string(sec.Data[github.KeyHTMLURL])}
			return g.print(cmd, v, func(w io.Writer) {
				fmt.Fprintf(w, "GitHub App %s (id %s)\n  %s\n  Install it: %s/installations/new\n", v["slug"], v["id"], v["url"], v["url"])
			})
		},
	}
	del := &cobra.Command{
		Use:   "delete",
		Short: "Remove the App's Secret; shpyrd stops using the App",
		RunE: func(cmd *cobra.Command, args []string) error {
			sec, err := githubAppSecret(g)
			if err != nil {
				return err
			}
			k, err := ctlKube(g)
			if err != nil {
				return err
			}
			if err := k.Kube.CoreV1().Secrets(install.DefaultSystemNamespace).Delete(signalContext(), github.SecretName, metav1.DeleteOptions{}); err != nil {
				return err
			}
			slug := string(sec.Data[github.KeySlug])
			return g.print(cmd, map[string]string{"deleted": slug}, func(w io.Writer) {
				fmt.Fprintf(w, "Removed the Secret %s/%s: shpyrd stops using %s within a minute; builds fall back to deploy keys and tokens.\n", install.DefaultSystemNamespace, github.SecretName, slug)
				fmt.Fprintf(w, "Delete the App on GitHub too: the owner's Settings, Developer settings, GitHub Apps, %s, Advanced, Delete GitHub App.\n", slug)
			})
		},
	}
	app.AddCommand(create, show, del)
	cmd.AddCommand(app)
	return cmd
}

func githubAppSecret(g *globalFlags) (*corev1.Secret, error) {
	k, err := ctlKube(g)
	if err != nil {
		return nil, err
	}
	sec, err := k.Kube.CoreV1().Secrets(install.DefaultSystemNamespace).Get(signalContext(), github.SecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, errors.New("this cluster has no GitHub App: register one with shpyrd-ctl github app create")
	}
	return sec, err
}

var manifestPage = template.Must(template.New("manifest").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Register shpyrd's GitHub App</title></head>
<body style="font-family:system-ui,sans-serif;margin:3rem;max-width:36rem">
<p>Sending the App to GitHub. If nothing happens, continue by hand.</p>
<form id="manifest" method="post" action="{{.Action}}">
<input type="hidden" name="manifest" value="{{.Manifest}}">
<button type="submit">Continue to GitHub</button>
</form>
<script>document.getElementById("manifest").submit()</script>
</body></html>`))

var resultPage = template.Must(template.New("result").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>{{.Title}}</title></head>
<body style="font-family:system-ui,sans-serif;margin:3rem;max-width:36rem"><h1>{{.Title}}</h1><p>{{.Text}}</p></body></html>`))

func showResult(w http.ResponseWriter, status int, title, text string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = resultPage.Execute(w, map[string]string{"Title": title, "Text": text})
}

// createGitHubApp serves the manifest on 127.0.0.1, opens the browser on
// it, and waits for GitHub to send the browser back with a code, which it
// turns into the App's credentials and writes into the Secret.
func createGitHubApp(cmd *cobra.Command, g *globalFlags, org, name, dashboard string, public bool) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(name)); n > githubNameLimit {
		return fmt.Errorf("--name is %d characters: GitHub takes at most %d characters for an App's name", n, githubNameLimit)
	}
	ctx := signalContext()
	out := g.progress(cmd)
	k, err := ctlKube(g)
	if err != nil {
		return err
	}
	ns := install.DefaultSystemNamespace
	if sec, err := k.Kube.CoreV1().Secrets(ns).Get(ctx, github.SecretName, metav1.GetOptions{}); err == nil {
		return fmt.Errorf("this cluster already has a GitHub App (%s): shpyrd-ctl github app delete first, to register another", sec.Data[github.KeySlug])
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	if dashboard == "" {
		info, err := install.ReadInstallInfo(ctx, k, "")
		if err != nil {
			return fmt.Errorf("could not read the cluster's install settings (%v): give the dashboard's public URL with --url", err)
		}
		// BaseURL with no domain is https://shpyrd., which looks like a
		// URL: fall back to it only when the cluster names its domain.
		dashboard = info.Vars[install.VarDashboardURL]
		if dashboard == "" && info.Vars[install.VarDomain] != "" {
			dashboard = install.BaseURL(info.Vars)("shpyrd")
		}
		if dashboard == "" {
			return errors.New("this cluster's install settings name no dashboard URL: give it with --url")
		}
	}
	dashboard = strings.TrimSuffix(dashboard, "/")
	if u, err := url.Parse(dashboard); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%q is not a URL: give the dashboard's, such as https://shpyrd.example.com", dashboard)
	}
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	state := base64.RawURLEncoding.EncodeToString(nonce)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	base := "http://" + ln.Addr().String()
	manifest := newAppManifest(dashboard, base+"/created", name, public)
	raw, _ := json.Marshal(manifest)
	target := "https://github.com/settings/apps/new"
	if org != "" {
		target = "https://github.com/organizations/" + url.PathEscape(org) + "/settings/apps/new"
	}
	apiURL := firstNonEmpty(os.Getenv(github.EnvAPIURL), github.DefaultAPIURL)

	type result struct {
		reg github.Registration
		err error
	}
	done := make(chan result, 1)
	var taken atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = manifestPage.Execute(w, map[string]string{"Action": target + "?state=" + state, "Manifest": string(raw)})
	})
	mux.HandleFunc("GET /created", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(state)) != 1 {
			showResult(w, http.StatusBadRequest, "Not this registration", "This page was not opened by this run of shpyrd-ctl. Start again with shpyrd-ctl github app create.")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			showResult(w, http.StatusBadRequest, "GitHub sent no code", "Start again with shpyrd-ctl github app create.")
			return
		}
		// The first good callback takes the code; another, even while
		// the first is still at work, is told so at once.
		if !taken.CompareAndSwap(false, true) {
			showResult(w, http.StatusConflict, "Already registering", "This registration is being finished or is done. You can close this tab.")
			return
		}
		func() {
			// GitHub has created the App by now: keep its credentials even
			// if the tab closes and the request is cancelled.
			keep := context.WithoutCancel(r.Context())
			reg, err := github.ConvertManifest(keep, apiURL, nil, code)
			if err == nil {
				sec := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: github.SecretName, Namespace: ns, Labels: map[string]string{"app.kubernetes.io/managed-by": "shpyrd"}},
					Type:       corev1.SecretTypeOpaque,
					Data:       reg.App.SecretData(),
				}
				_, err = k.Kube.CoreV1().Secrets(ns).Create(keep, sec, metav1.CreateOptions{})
			}
			if err != nil {
				showResult(w, http.StatusBadGateway, "Not registered", "GitHub created the App, but shpyrd-ctl could not keep its credentials: "+err.Error()+". Delete the App on GitHub and run shpyrd-ctl github app create again.")
			} else {
				showResult(w, http.StatusOK, "Registered "+reg.Name, "The App is ready. You can close this tab; the terminal says what is next.")
			}
			done <- result{reg, err}
		}()
	})
	// Only the page this run opened, by the address it opened: another
	// Host is a name rebound to 127.0.0.1 by some other site.
	host := ln.Addr().String()
	onlyHere := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host {
			showResult(w, http.StatusForbidden, "Not this address", "Open "+base+"/ itself.")
			return
		}
		mux.ServeHTTP(w, r)
	})
	srv := &http.Server{Handler: onlyHere, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()

	fmt.Fprintf(out, "Registering %s on GitHub. Opening the browser (if nothing opens: %s/)\n", manifest.Name, base)
	if err := browse(base + "/"); err != nil {
		fmt.Fprintf(out, "Open %s/ in a browser on this machine.\n", base)
	}
	fmt.Fprint(out, "Waiting for GitHub... ")
	select {
	case <-ctx.Done():
		fmt.Fprintln(out)
		return errors.New("cancelled: nothing was registered")
	case <-time.After(manifestWait):
		fmt.Fprintln(out)
		return fmt.Errorf("GitHub did not come back within %s: run shpyrd-ctl github app create again", waitText(manifestWait))
	case r := <-done:
		fmt.Fprintln(out)
		if r.err != nil {
			return fmt.Errorf("the App was created on GitHub but not kept: %w; delete it there and run shpyrd-ctl github app create again", r.err)
		}
		app := r.reg.App
		return g.print(cmd, map[string]any{"slug": app.Slug, "id": app.ID, "url": app.HTMLURL, "install": app.InstallURL()}, func(w io.Writer) {
			fmt.Fprintf(w, "Registered %s: %s\n", r.reg.Name, app.HTMLURL)
			fmt.Fprintf(w, "Its credentials are in the Secret %s/%s.\n", ns, github.SecretName)
			fmt.Fprintf(w, "Install it on an account or organisation: %s\n", app.InstallURL())
			fmt.Fprintln(w, "Workspaces connect it from the dashboard (Workspace settings, Git) or with: shpyrd github connect")
		})
	}
}

// waitText says how long the registration waits, in words.
func waitText(wait time.Duration) string {
	switch {
	case wait == time.Minute:
		return "1 minute"
	case wait > 0 && wait%time.Minute == 0:
		return fmt.Sprintf("%d minutes", wait/time.Minute)
	}
	return wait.String()
}
