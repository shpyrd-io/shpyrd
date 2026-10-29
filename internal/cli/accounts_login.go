package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultAccountsURL = "https://accounts.shpyrd.io"

// Installer progress is a versioned JSON-lines protocol on stderr. Stdout
// remains the single final --json result, and credentials never enter events.
type loginProgress struct {
	human      io.Writer
	events     io.Writer
	structured bool
}

func (p *loginProgress) Write(b []byte) (int, error) {
	if p.structured {
		return len(b), nil
	}
	return p.human.Write(b)
}
func (p *loginProgress) event(event, link, code, workspace string) {
	if p.structured {
		_ = json.NewEncoder(p.events).Encode(map[string]any{"version": 1, "event": event, "url": link, "code": code, "workspace": workspace})
	}
}
func loginEvent(out io.Writer, event, link, code, workspace string) {
	if p, ok := out.(*loginProgress); ok {
		p.event(event, link, code, workspace)
	}
}

// Only HTTPS or an explicitly local development server can receive handoff data.
func loginEndpoint(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("login endpoint must be an origin without credentials, path, query or fragment")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")) {
		return "", errors.New("login endpoint must use HTTPS (HTTP is allowed only for localhost)")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

type setupConnection struct{ base, secret, workspace string }

func setupPost(ctx context.Context, endpoint string, body []byte, v any) (deviceStatus, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return postJSONStatusClient(requestCtx, endpoint, body, v, client)
}
func accountsWorkspace(ctx context.Context, base string, out io.Writer, noBrowser bool) (*setupConnection, error) {
	base, err := loginEndpoint(base)
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	if len(host) > 100 {
		host = host[:100]
	}
	body, _ := json.Marshal(map[string]string{"name": host})
	var start struct {
		Secret     string `json:"secret"`
		BrowserURL string `json:"browserUrl"`
		ExpiresIn  int    `json:"expiresIn"`
		Interval   int    `json:"interval"`
	}
	status, err := setupPost(ctx, base+"/api/cli/setup/start", body, &start)
	if err != nil {
		return nil, fmt.Errorf("cannot start Accounts setup: %w", err)
	}
	if status.Error != "" {
		return nil, fmt.Errorf("cannot start Accounts setup: %s", status.Error)
	}
	link, err := url.Parse(start.BrowserURL)
	origin, _ := url.Parse(base)
	if err != nil || link.Scheme != origin.Scheme || link.Host != origin.Host || link.User != nil || link.Path != "/cli/setup" || len(start.Secret) != 64 {
		return nil, errors.New("Accounts returned an invalid setup response")
	}
	loginEvent(out, "browser", start.BrowserURL, "", "")
	fmt.Fprintf(out, "Open %s to connect your workspace. Sign in or create your account there.\n", start.BrowserURL)
	if !noBrowser {
		if err := openBrowser(start.BrowserURL); err != nil {
			fmt.Fprintln(out, "Could not open the browser; use the link above.")
		}
	}
	interval := time.Duration(firstPositive(start.Interval, 5)) * time.Second
	if interval > 30*time.Second {
		interval = 30 * time.Second
	}
	ttl := time.Duration(firstPositive(start.ExpiresIn, 3600)) * time.Second
	if ttl > time.Hour {
		ttl = time.Hour
	}
	ctx, cancel := context.WithTimeout(ctx, ttl)
	defer cancel()
	body, _ = json.Marshal(map[string]string{"secret": start.Secret})
	lastStatus := ""
	for {
		select {
		case <-ctx.Done():
			return nil, errors.New("Accounts setup cancelled or expired; run shpyrd login again")
		case <-time.After(interval):
		}
		var poll struct {
			Status    string `json:"status"`
			Workspace struct {
				URL  string `json:"url"`
				Name string `json:"name"`
			} `json:"workspace"`
		}
		st, err := setupPost(ctx, base+"/api/cli/setup/poll", body, &poll)
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			loginEvent(out, "retrying", "", "", "")
			continue
		}
		if st.Error == "slow_down" {
			interval += time.Second
			if interval > 30*time.Second {
				interval = 30 * time.Second
			}
			continue
		}
		if st.Error != "" {
			return nil, fmt.Errorf("Accounts setup: %s", st.Error)
		}
		if poll.Status != lastStatus {
			loginEvent(out, poll.Status, "", "", poll.Workspace.Name)
			fmt.Fprintf(out, "Workspace setup: %s\n", poll.Status)
			lastStatus = poll.Status
		}
		switch poll.Status {
		case "waiting", "provisioning":
			continue
		case "ready":
			workspace, err := loginEndpoint(poll.Workspace.URL)
			if err != nil {
				return nil, fmt.Errorf("invalid workspace from Accounts: %w", err)
			}
			return &setupConnection{base: base, secret: start.Secret, workspace: workspace}, nil
		case "failed", "pending", "suspended":
			return nil, fmt.Errorf("workspace is %s; open its setup page in Accounts to recover, then run shpyrd login again", poll.Status)
		default:
			return nil, fmt.Errorf("unexpected Accounts setup state %q; restart login", poll.Status)
		}
	}
}
func (s *setupConnection) complete(ctx context.Context, out io.Writer) {
	if s == nil {
		return
	}
	body, _ := json.Marshal(map[string]string{"secret": s.secret})
	var result map[string]any
	status, err := setupPost(ctx, s.base+"/api/cli/setup/complete", body, &result)
	if err != nil || status.Error != "" {
		loginEvent(out, "completion_pending", "", "", s.workspace)
		fmt.Fprintln(out, "Signed in locally; Accounts could not record completion. You can use the CLI.")
	}
}
