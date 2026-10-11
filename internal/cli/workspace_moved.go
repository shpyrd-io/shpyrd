package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const workspaceURLHeader = "X-Shpyrd-Workspace-URL"

// A move is vouched for by the original server, over verified TLS. Never
// accept it from a redirect destination, plaintext HTTP, or an insecure client.
func workspaceMoveURL(old string, resp *http.Response) string {
	from, err := url.Parse(old)
	if err != nil || from.Scheme != "https" || resp == nil || resp.Request == nil || resp.Request.URL.Scheme != from.Scheme || !strings.EqualFold(resp.Request.URL.Host, from.Host) || resp.TLS == nil || len(resp.TLS.VerifiedChains) == 0 {
		return ""
	}
	u, err := url.Parse(resp.Header.Get(workspaceURLHeader))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func migrateWorkspaceSession(old, token string, resp *http.Response, out io.Writer) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || os.Getenv("SHPYRD_URL") != "" || os.Getenv("SHPYRD_TOKEN") != "" {
		return
	}
	next := workspaceMoveURL(old, resp)
	old = strings.TrimRight(old, "/")
	if next == "" || next == old {
		return
	}
	s := loadSessions()
	sess := s.Sessions[old]
	if sess == nil || sess.Token != token || s.Sessions[next] != nil {
		return
	}
	delete(s.Sessions, old)
	sess.URL = next
	s.Sessions[next] = sess
	if s.Current == old {
		s.Current = next
	}
	if err := s.save(); err != nil {
		fmt.Fprintf(out, "Workspace moved to %s; could not update saved session: %v\n", next, err)
		return
	}
	fmt.Fprintf(out, "Workspace moved to %s; updated your saved session.\n", next)
}

// An ingress can reject the body before the API can send its move header.
// Ask the same origin with no credentials or upload and never retry the POST.
func sourceUploadRefused(ctx context.Context, old string, size int, resp *http.Response) error {
	next := workspaceMoveURL(old, resp)
	if next == "" {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(old, "/")+"/api/config", nil)
		if err == nil {
			client := *http.DefaultClient
			client.Jar = nil
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			if answer, err := client.Do(req); err == nil {
				next = workspaceMoveURL(old, answer)
				answer.Body.Close()
			}
		}
	}
	msg := fmt.Sprintf("the server refused an upload of %.2f MB at %s/api/sources (HTTP 413)", float64(size)/1e6, strings.TrimRight(old, "/"))
	if next != "" && next != strings.TrimRight(old, "/") {
		return fmt.Errorf("%s; the workspace's address is %s: shpyrd login --url %s", msg, next, next)
	}
	return fmt.Errorf("%s; check the workspace's current address and upload limit; if it moved, run shpyrd login --url <current workspace URL>", msg)
}
