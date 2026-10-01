package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/cliout"
	"github.com/shpyrd-io/shpyrd/pkg/kexec"
)

// Session store (RFC-0052): a JSON file at ~/.shpyrd/sessions.json holds the
// token (admin or user) per workspace URL, so project commands work without a
// kubeconfig. The operator's cluster commands (cluster init, …) keep the
// kubeconfig and live in shpyrd-ctl.

const sessionsFileName = "sessions.json"

type loginSessions struct {
	// Current is the workspace commands talk to when several sessions are
	// saved: the last `shpyrd login`, or what `shpyrd use` picked.
	Current  string                   `json:"current,omitempty"`
	Sessions map[string]*loginSession `json:"sessions"` // keyed by normalised workspace URL
}

// active is the session commands use: SHPYRD_URL when set (and signed in
// there), else Current, else the only session there is. Nil when none.
func (s *loginSessions) active() *loginSession {
	if env := os.Getenv("SHPYRD_URL"); env != "" {
		if norm, err := normaliseURL(env); err == nil {
			if sess, ok := s.Sessions[norm]; ok {
				return sess
			}
		}
	}
	if sess, ok := s.Sessions[s.Current]; ok && s.Current != "" {
		return sess
	}
	if len(s.Sessions) == 1 {
		for _, sess := range s.Sessions {
			return sess
		}
	}
	return nil
}

// activeURL is the URL of the active session, "" when none.
func (s *loginSessions) activeURL() string {
	if sess := s.active(); sess != nil {
		return sess.URL
	}
	return ""
}

type loginSession struct {
	URL       string    `json:"url"`
	Token     string    `json:"token"`
	SavedAt   time.Time `json:"savedAt"`
	WhoAmI    string    `json:"whoAmI,omitempty"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

func sessionsPath() string {
	dir, _ := os.UserHomeDir()
	return filepath.Join(dir, ".shpyrd", sessionsFileName)
}

func loadSessions() *loginSessions {
	s := &loginSessions{Sessions: map[string]*loginSession{}}
	raw, err := os.ReadFile(sessionsPath())
	if err == nil {
		_ = json.Unmarshal(raw, s)
		if s.Sessions == nil {
			s.Sessions = map[string]*loginSession{}
		}
	}
	return s
}

func (s *loginSessions) save() error {
	p := sessionsPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, raw, fs.FileMode(0o600))
}

// normaliseURL strips trailing slashes and ensures the scheme is present.
func normaliseURL(raw string) (string, error) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("not a valid URL: %q", raw)
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/"), nil
}

// SessionFor returns the saved token for the given workspace URL, or "" when
// none is stored.
func SessionFor(wsURL string) string {
	if wsURL == "" {
		return ""
	}
	norm, err := normaliseURL(wsURL)
	if err != nil {
		return ""
	}
	s := loadSessions()
	if sess, ok := s.Sessions[norm]; ok {
		return sess.Token
	}
	return ""
}

// DirectURL returns the workspace URL a saved session uses for direct HTTP.
// It exists so the API transport can dial the right address.
func DirectURL(wsURL string) string {
	if wsURL == "" {
		return ""
	}
	norm, _ := normaliseURL(wsURL)
	s := loadSessions()
	if sess, ok := s.Sessions[norm]; ok {
		return sess.URL
	}
	return ""
}

// newLoginCmd is `shpyrd login`.
func newLoginCmd(g *globalFlags) *cobra.Command {
	var (
		wsURL     string
		token     string
		noBrowser bool
		signup    bool
		signupURL string
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in to a workspace (no kubeconfig needed for project commands)",
		Long: `Sign in to a shpyrd workspace so project commands work without a kubeconfig:

  shpyrd login --url https://acme.shpyrd.app                      # approve in the browser
  shpyrd login --url https://acme.shpyrd.app --token shp_...      # a token from the dashboard or CI
  shpyrd login --url https://shpyrd.oci.shpyrd.io --token <admin token>

Without --token the browser opens the workspace's sign-in; approve the code
shown here and the CLI is signed in as you for 30 days (--no-browser prints
the link instead of opening it; it works over SSH). That creates a session
token, listed with your API tokens on the Workspace page and revoked there.

After login, every developer command uses the workspace API and your
identity: projects, deploy, logs, shell, run, scale, resize, releases,
rollback, redeploy, open, secrets, access, allow, exposure, volumes,
attach, detach, drains, domains, pg, redis, members, tokens. Operator
commands live in shpyrd-ctl and keep the kubeconfig.

The workspace you sign in to becomes the current one (shpyrd use lists
and switches; SHPYRD_URL overrides for one shell).

Tip: shpyrd cluster token --context <ctx> prints the admin token.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			out := g.progress(cmd)
			var expires time.Time
			if wsURL == "" && !signup {
				// Try to guess from --context if available.
				if g.kubeCtx != "" {
					return errors.New("--url is required: give the dashboard URL, e.g. https://shpyrd.oci.shpyrd.io")
				}
				// At a terminal, ask: an existing workspace, or a new
				// account with its first workspace on shpyrd cloud.
				choice, err := askLoginChoice(cmd.InOrStdin(), out)
				if err != nil {
					return err
				}
				switch choice {
				case "":
					return errors.New("--url is required: the workspace URL, e.g. https://acme.shpyrd.app (or --signup to create an account)")
				case loginChoiceSignup:
					signup = true
				default:
					wsURL = choice
				}
			}
			if signup {
				if token != "" || wsURL != "" {
					return errors.New("--signup creates an account and a workspace: it takes neither --url nor --token")
				}
				return signupLogin(ctx, cmd, g, signupURL, out, noBrowser)
			}
			norm, err := normaliseURL(wsURL)
			if err != nil {
				return err
			}
			if token, err = cliout.ValueOrFile(token); err != nil {
				return fmt.Errorf("--token: %w", err)
			}
			if token == "" {
				// Already signed in there, and the credential still works:
				// make it the current workspace. A dead one (expired,
				// revoked) is replaced by a fresh sign-in.
				if s := loadSessions(); s.Sessions[norm] != nil && verifyToken(ctx, norm, s.Sessions[norm].Token) == nil {
					s.Current = norm
					if err := s.save(); err != nil {
						return err
					}
					return g.print(cmd, map[string]any{"url": norm, "user": s.Sessions[norm].WhoAmI, "signedIn": true}, func(w io.Writer) {
						fmt.Fprintf(w, "Now using %s (%s)\n", norm, firstNonEmpty(s.Sessions[norm].WhoAmI, "signed in"))
					})
				}
				// The browser sign-in (RFC-0052): the workspace shows a
				// code here, the person approves it in the dashboard.
				approved, err := browserLogin(ctx, norm, out, noBrowser)
				if err != nil {
					return err
				}
				token = approved.Token
				expires = approved.ExpiresAt
			}
			// Verify the token works before saving.
			if err := verifyToken(ctx, norm, token); err != nil {
				return fmt.Errorf("login failed: %w", err)
			}
			whoAmI := whoAmI(ctx, norm, token)
			s := loadSessions()
			s.Sessions[norm] = &loginSession{URL: norm, Token: token, SavedAt: time.Now(), WhoAmI: whoAmI, ExpiresAt: expires}
			s.Current = norm
			if err := s.save(); err != nil {
				return fmt.Errorf("save session: %w", err)
			}
			return g.print(cmd, map[string]any{"url": norm, "user": whoAmI, "signedIn": true}, func(w io.Writer) {
				if whoAmI != "" {
					fmt.Fprintf(w, "Signed in to %s as %s\n", norm, whoAmI)
				} else {
					fmt.Fprintf(w, "Signed in to %s\n", norm)
				}
				fmt.Fprintln(w, "Project commands work without a kubeconfig now.")
			})
		},
	}
	cmd.Flags().StringVar(&wsURL, "url", os.Getenv("SHPYRD_URL"), "workspace URL (or SHPYRD_URL)")
	cmd.Flags().StringVar(&token, "token", os.Getenv("SHPYRD_TOKEN"), "API token (or SHPYRD_TOKEN), or @path to read it from a file: a personal token from `shpyrd tokens create` or the admin token from `shpyrd cluster token`; without it, the browser signs you in")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the sign-in link instead of opening the browser")
	cmd.Flags().BoolVar(&signup, "signup", false, "create an account and a first workspace on shpyrd cloud, in the browser; the CLI is signed in to it when it is ready")
	cmd.Flags().StringVar(&signupURL, "signup-url", firstNonEmpty(os.Getenv("SHPYRD_SIGNUP_URL"), defaultSignupURL), "where the signup lives (or SHPYRD_SIGNUP_URL)")
	return cmd
}

// defaultSignupURL is shpyrd cloud's signup.
const defaultSignupURL = "https://signup.shpyrd.io"

// loginChoiceSignup is what askLoginChoice answers for a new account.
const loginChoiceSignup = "signup"

// askLoginChoice asks a person at a terminal what they want when they ran
// `shpyrd login` with no URL: the URL of a workspace they belong to, or a
// new account. Away from a terminal it answers "" and the caller explains
// the flags.
func askLoginChoice(in io.Reader, out io.Writer) (string, error) {
	if !kexec.StdinIsTerminal() {
		return "", nil
	}
	fmt.Fprintln(out, "No workspace given. What do you want to do?")
	fmt.Fprintln(out, "  1) Sign in to a workspace you belong to (its URL, like https://acme.shpyrd.app)")
	fmt.Fprintln(out, "  2) Create an account and your first workspace on shpyrd cloud")
	fmt.Fprint(out, "Choose 1 or 2: ")
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	switch strings.TrimSpace(line) {
	case "2":
		return loginChoiceSignup, nil
	case "1":
		fmt.Fprint(out, "Workspace URL: ")
		url, err := reader.ReadString('\n')
		if err != nil && url == "" {
			return "", err
		}
		if url = strings.TrimSpace(url); url != "" {
			return url, nil
		}
		return "", nil
	default:
		return "", errors.New("answer 1 or 2")
	}
}

// signupLogin creates an account and a workspace from the terminal: the
// signup gives the CLI a code, the browser opens the signup with it, the
// person proves their email and names the workspace there, and when the
// workspace's door answers the signup signs the CLI in to it. One signup,
// two entrances.
func signupLogin(ctx context.Context, cmd *cobra.Command, g *globalFlags, signupURL string, out io.Writer, noBrowser bool) error {
	base := strings.TrimSuffix(strings.TrimSpace(signupURL), "/")
	if base == "" {
		return errors.New("--signup-url is empty")
	}
	fmt.Fprintf(out, "Creating an account at %s.\n", base)
	approved, err := deviceFlow(ctx, base+"/api/signup/cli/device", base+"/api/signup/cli/device/token", out, noBrowser,
		"Finish the signup in the browser: your email, a code we send it, your workspace's name. The CLI waits for the workspace's door to answer.")
	if err != nil {
		return err
	}
	if approved.WorkspaceURL == "" {
		return errors.New("the signup approved the sign-in but named no workspace")
	}
	norm, err := normaliseURL(approved.WorkspaceURL)
	if err != nil {
		return err
	}
	if err := verifyToken(ctx, norm, approved.Token); err != nil {
		return fmt.Errorf("the new workspace refused the credential: %w", err)
	}
	s := loadSessions()
	s.Sessions[norm] = &loginSession{URL: norm, Token: approved.Token, SavedAt: time.Now(), WhoAmI: approved.Email, ExpiresAt: approved.ExpiresAt}
	s.Current = norm
	if err := s.save(); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return g.print(cmd, map[string]any{"url": norm, "user": approved.Email, "signedIn": true, "created": true}, func(w io.Writer) {
		fmt.Fprintf(w, "Your workspace %s is ready, and the CLI is signed in as %s.\n", norm, approved.Email)
		fmt.Fprintln(w, "Deploy your first project: `shpyrd deploy` in its folder. Your email has the way into the dashboard.")
	})
}

// deviceStart is what the workspace answers when a browser sign-in
// begins (RFC 8628 §3.2).
type deviceStart struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// deviceApproval is the credential the approval minted. WorkspaceURL is
// set by the signup, which makes the workspace the credential is for.
type deviceApproval struct {
	Token        string    `json:"token"`
	Email        string    `json:"email"`
	ExpiresAt    time.Time `json:"expiresAt"`
	WorkspaceURL string    `json:"workspaceUrl,omitempty"`
}

// browserLogin runs the browser sign-in against a workspace: asks it for
// a code, shows the person where to approve it, and polls until the
// approval has turned into a token (or the code expires, or Ctrl-C).
func browserLogin(ctx context.Context, wsURL string, out io.Writer, noBrowser bool) (*deviceApproval, error) {
	approved, err := deviceFlow(ctx, wsURL+"/api/cli/device", wsURL+"/api/cli/device/token", out, noBrowser, "")
	if err != nil && strings.Contains(err.Error(), "cannot start") {
		return nil, fmt.Errorf("%w\n(pass --token to sign in with a token instead)", err)
	}
	return approved, err
}

// deviceFlow is the device flow (RFC 8628) against any pair of endpoints:
// a workspace's own sign-in, or the signup's. `note` is said once the
// browser opened, when there is more to do there than approve.
func deviceFlow(ctx context.Context, startURL, pollURL string, out io.Writer, noBrowser bool, note string) (*deviceApproval, error) {
	host, _ := os.Hostname()
	body, _ := json.Marshal(map[string]string{"name": host})
	var start deviceStart
	if err := postJSON(ctx, startURL, body, &start); err != nil {
		return nil, fmt.Errorf("cannot start the browser sign-in at %s: %w", startURL, err)
	}
	if start.DeviceCode == "" || start.UserCode == "" {
		return nil, fmt.Errorf("%s answered without a sign-in code; is it a shpyrd server?", startURL)
	}
	link := firstNonEmpty(start.VerificationURIComplete, start.VerificationURI)
	fmt.Fprintf(out, "Your code: %s\n", start.UserCode)
	opened := false
	if !noBrowser {
		opened = openBrowser(link) == nil
	}
	if opened {
		fmt.Fprintf(out, "Approve it in the browser (if nothing opened: %s)\n", link)
	} else {
		fmt.Fprintf(out, "Open %s and approve it.\n", link)
	}
	if note != "" {
		fmt.Fprintln(out, note)
	}
	fmt.Fprint(out, "Waiting for the approval... ")
	interval := time.Duration(firstPositive(start.Interval, 5)) * time.Second
	deadline := time.Now().Add(time.Duration(firstPositive(start.ExpiresIn, 600)) * time.Second)
	poll, _ := json.Marshal(map[string]string{"device_code": start.DeviceCode})
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(out)
			return nil, errors.New("sign-in cancelled")
		case <-time.After(interval):
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(out)
			return nil, errors.New("the code expired before it was approved; run `shpyrd login` again")
		}
		var approval deviceApproval
		status, err := postJSONStatus(ctx, pollURL, poll, &approval)
		if err != nil {
			return nil, fmt.Errorf("cannot reach %s: %w", pollURL, err)
		}
		switch status.Error {
		case "":
			if approval.Token == "" {
				fmt.Fprintln(out)
				return nil, errors.New("the workspace approved the sign-in but sent no token")
			}
			fmt.Fprintln(out, "approved.")
			return &approval, nil
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		case "access_denied":
			fmt.Fprintln(out)
			return nil, errors.New("the sign-in was refused in the browser")
		case "expired_token":
			fmt.Fprintln(out)
			return nil, errors.New("the code expired before it was approved; run `shpyrd login` again")
		default:
			fmt.Fprintln(out)
			return nil, fmt.Errorf("sign-in failed: %s", firstNonEmpty(status.Description, status.Error))
		}
	}
}

// deviceStatus is the error half of a poll (RFC 8628 §3.5).
type deviceStatus struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

// postJSON sends a JSON body and decodes a 2xx answer into v; any other
// status is an error with the server's message.
func postJSON(ctx context.Context, url string, body []byte, v any) error {
	status, err := postJSONStatus(ctx, url, body, v)
	if err != nil {
		return err
	}
	if status.Error != "" {
		return errors.New(firstNonEmpty(status.Description, status.Error))
	}
	return nil
}

// postJSONStatus is postJSON that hands a 4xx answer back as a status
// instead of an error: the poll's "not yet" answers are 400s.
func postJSONStatus(ctx context.Context, url string, body []byte, v any) (deviceStatus, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return deviceStatus{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return deviceStatus{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return deviceStatus{}, json.Unmarshal(raw, v)
	}
	var st deviceStatus
	if json.Unmarshal(raw, &st) != nil || st.Error == "" {
		st.Error = resp.Status
		st.Description = strings.TrimSpace(truncate(string(raw), 200))
	}
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusNotFound {
		return deviceStatus{}, fmt.Errorf("%s: %s", resp.Status, firstNonEmpty(st.Description, st.Error))
	}
	return st, nil
}

func firstPositive(vals ...int) int {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

// verifyToken checks the token is accepted by the workspace: /api/me answers
// 401 for an unknown, expired or revoked token and 200 for a live one.
func verifyToken(ctx context.Context, wsURL, token string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", wsURL+"/api/me", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", wsURL, err)
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("the workspace rejected this token (expired, revoked or mistyped)")
	case resp.StatusCode >= 500:
		return fmt.Errorf("server error %d", resp.StatusCode)
	}
	return nil
}

func whoAmI(ctx context.Context, wsURL, token string) string {
	req, err := http.NewRequestWithContext(ctx, "GET", wsURL+"/api/me", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	defer resp.Body.Close()
	var me struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		return ""
	}
	return me.Email
}

// newLogoutCmd is `shpyrd logout`.
func newLogoutCmd(g *globalFlags) *cobra.Command {
	var wsURL string
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove saved credentials for a workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			s := loadSessions()
			if wsURL == "" {
				wsURL = s.activeURL()
			}
			if wsURL == "" {
				return errors.New("--url is required (no current workspace)")
			}
			norm, err := normaliseURL(wsURL)
			if err != nil {
				return err
			}
			if _, ok := s.Sessions[norm]; !ok {
				return fmt.Errorf("not signed in to %s", norm)
			}
			delete(s.Sessions, norm)
			if s.Current == norm {
				s.Current = ""
			}
			if err := s.save(); err != nil {
				return err
			}
			return g.print(cmd, map[string]any{"url": norm, "signedIn": false}, func(w io.Writer) {
				fmt.Fprintf(w, "Signed out of %s\n", norm)
			})
		},
	}
	cmd.Flags().StringVar(&wsURL, "url", os.Getenv("SHPYRD_URL"), "workspace URL (or SHPYRD_URL)")
	return cmd
}

// newWhoAmICmd is `shpyrd whoami`.
func newWhoAmICmd(g *globalFlags) *cobra.Command {
	var wsURL string
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the current user of a workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			s := loadSessions()
			if wsURL == "" {
				wsURL = s.activeURL()
			}
			if wsURL == "" {
				return errors.New("not signed in: run `shpyrd login --url <workspace URL>`")
			}
			norm, err := normaliseURL(wsURL)
			if err != nil {
				return err
			}
			sess, ok := s.Sessions[norm]
			if !ok {
				return fmt.Errorf("not signed in to %s; run `shpyrd login --url %s`", norm, norm)
			}
			if err := verifyToken(ctx, norm, sess.Token); err != nil {
				return fmt.Errorf("%s: %w; run `shpyrd login --url %s` again", norm, err, norm)
			}
			me := whoAmI(ctx, norm, sess.Token)
			if me == "" {
				// The admin token has no person behind it.
				me = "admin token"
			}
			return g.print(cmd, map[string]string{"url": norm, "user": me}, func(w io.Writer) {
				fmt.Fprintln(w, me)
			})
		},
	}
	cmd.Flags().StringVar(&wsURL, "url", os.Getenv("SHPYRD_URL"), "workspace URL (or SHPYRD_URL)")
	return cmd
}

// TokenView is a token as shown in the CLI (mirrors api.TokenView).
type TokenView struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Kind         string            `json:"kind,omitempty"`
	PlatformRole string            `json:"platformRole,omitempty"`
	ProjectRoles map[string]string `json:"projectRoles,omitempty"`
	ExpiresAt    *time.Time        `json:"expiresAt,omitempty"`
	LastUsedAt   *time.Time        `json:"lastUsedAt,omitempty"`
}

// TokenCreateView is the create response.
type TokenCreateView struct {
	TokenView
	Token string `json:"token"`
}

// newTokensCmd is `shpyrd tokens`.
func newTokensCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tokens",
		Short: "Personal API tokens (scoped credentials for CI and integrations)",
		Long: `API tokens let scripts and CI pipelines authenticate without the admin token.
They carry only the roles you give them and expire when you say.

  shpyrd tokens create ci --platform-role platform-viewer --expires 90d
  shpyrd tokens create ci --project shop --role developer --expires 30d
  shpyrd tokens list
  shpyrd tokens revoke <id>
  
The token value is shown once. Store it in SHPYRD_TOKEN for the CLI, or
pass it to shpyrd login --token.`,
	}

	var wsURL string

	// helper: serverRequest via the login session
	call := func(ctx context.Context, method, path string, body []byte) ([]byte, error) {
		sessions := loadSessions()
		url := os.Getenv("SHPYRD_URL")
		tok := os.Getenv("SHPYRD_TOKEN")
		if sess := sessions.active(); sess != nil {
			if url == "" {
				url = sess.URL
			}
			if tok == "" {
				tok = sess.Token
			}
		}
		if wsURL != "" {
			url = wsURL
		}
		if url == "" {
			return nil, errors.New("--url is required or run `shpyrd login`")
		}
		if tok == "" {
			return nil, errors.New("not signed in: run `shpyrd login --url " + url + " --token <token>`")
		}
		return serverRequestDirect(ctx, url, tok, method, path, body, "application/json")
	}

	var (
		name         string
		platformRole string
		projectRole  string
		project      string
		expiresIn    string
	)
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a personal API token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			body := map[string]interface{}{"name": args[0]}
			if platformRole != "" {
				body["platformRole"] = platformRole
			}
			if project != "" && projectRole != "" {
				body["projectRoles"] = map[string]string{project: projectRole}
			}
			if expiresIn != "" {
				body["expiresIn"] = expiresIn
			}
			raw, _ := json.Marshal(body)
			resp, err := call(ctx, "POST", "api/tokens", raw)
			if err != nil {
				return err
			}
			var tok TokenCreateView
			if err := json.Unmarshal(resp, &tok); err != nil {
				return fmt.Errorf("unexpected response: %s", truncate(string(resp), 200))
			}
			return g.print(cmd, tok, func(out io.Writer) {
				fmt.Fprintln(out, tok.Token)
				fmt.Fprintf(out, "Token %q created (id: %s). The value above is shown once; store it safely.\n", tok.Name, tok.ID)
				if tok.ExpiresAt != nil {
					fmt.Fprintf(out, "Expires: %s\n", tok.ExpiresAt.Local().Format("2006-01-02"))
				}
			})
		},
	}
	createCmd.Flags().StringVar(&platformRole, "platform-role", "", "platform-viewer or platform-admin")
	createCmd.Flags().StringVar(&project, "project", "", "project slug for a project-scoped role")
	createCmd.Flags().StringVar(&projectRole, "role", "", "reader, user, viewer, developer or admin (with --project)")
	createCmd.Flags().StringVar(&expiresIn, "expires", "90d", "expiry: 30d, 90d, 365d, etc.")
	_ = name

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List your API tokens",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			resp, err := call(ctx, "GET", "api/tokens", nil)
			if err != nil {
				return err
			}
			var tokens []TokenView
			if err := json.Unmarshal(resp, &tokens); err != nil {
				return fmt.Errorf("unexpected response: %s", truncate(string(resp), 200))
			}
			if tokens == nil {
				tokens = []TokenView{}
			}
			return g.print(cmd, tokens, func(w io.Writer) {
				if len(tokens) == 0 {
					fmt.Fprintln(w, "No tokens. Create one with `shpyrd tokens create <name>`.")
					return
				}
				tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "NAME\tID\tROLE\tEXPIRES\tLAST USED")
				for _, t := range tokens {
					role := t.PlatformRole
					for p, r := range t.ProjectRoles {
						role = p + "=" + r
						break
					}
					if role == "" {
						role = "project-scoped"
					}
					if t.Kind == "session" {
						role = "session (your roles)"
					}
					expires := "never"
					if t.ExpiresAt != nil {
						expires = t.ExpiresAt.Local().Format("2006-01-02")
					}
					used := "-"
					if t.LastUsedAt != nil {
						used = ago(*t.LastUsedAt)
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", t.Name, t.ID, role, expires, used)
				}
				_ = tw.Flush()
			})
		},
	}

	revokeCmd := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke an API token immediately",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			if _, err := call(ctx, "DELETE", "api/tokens/"+args[0], nil); err != nil {
				return err
			}
			return g.print(cmd, map[string]any{"id": args[0], "revoked": true}, func(w io.Writer) {
				fmt.Fprintf(w, "Token %s revoked.\n", args[0])
			})
		},
	}

	for _, c := range []*cobra.Command{createCmd, listCmd, revokeCmd} {
		c.Flags().StringVar(&wsURL, "url", os.Getenv("SHPYRD_URL"), "workspace URL (or SHPYRD_URL)")
	}
	cmd.AddCommand(createCmd, listCmd, revokeCmd)
	return cmd
}

// newUseCmd is `shpyrd use <url>`: pick which signed-in workspace commands
// talk to. `shpyrd use` alone lists them.
func newUseCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "use [workspace URL]",
		Short: "Choose the signed-in workspace commands talk to (no argument: list them)",
		Long: `The CLI keeps one saved login per workspace. Commands talk to the current
one; SHPYRD_URL overrides it for one shell, --context bypasses it for a cluster.

  shpyrd use                              # list the workspaces you are signed in to
  shpyrd use https://acme.shpyrd.app      # switch`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s := loadSessions()
			if len(args) == 0 {
				active := s.activeURL()
				urls := make([]string, 0, len(s.Sessions))
				for u := range s.Sessions {
					urls = append(urls, u)
				}
				sort.Strings(urls)
				// Never the tokens: only where and as whom.
				type entry struct {
					URL     string `json:"url"`
					User    string `json:"user,omitempty"`
					Current bool   `json:"current"`
				}
				list := make([]entry, 0, len(urls))
				for _, u := range urls {
					list = append(list, entry{URL: u, User: s.Sessions[u].WhoAmI, Current: u == active})
				}
				return g.print(cmd, list, func(out io.Writer) {
					if len(list) == 0 {
						fmt.Fprintln(out, "Not signed in anywhere: shpyrd login --url <workspace URL>")
						return
					}
					for _, e := range list {
						mark := "  "
						if e.Current {
							mark = "* "
						}
						fmt.Fprintf(out, "%s%s  %s\n", mark, e.URL, e.User)
					}
				})
			}
			norm, err := normaliseURL(args[0])
			if err != nil {
				return err
			}
			if _, ok := s.Sessions[norm]; !ok {
				return fmt.Errorf("not signed in to %s; run `shpyrd login --url %s`", norm, norm)
			}
			s.Current = norm
			if err := s.save(); err != nil {
				return err
			}
			return g.print(cmd, map[string]any{"url": norm, "user": s.Sessions[norm].WhoAmI, "current": true}, func(w io.Writer) {
				fmt.Fprintf(w, "Now using %s (%s)\n", norm, firstNonEmpty(s.Sessions[norm].WhoAmI, "signed in"))
			})
		},
	}
	return cmd
}
