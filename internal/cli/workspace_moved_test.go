package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceSessionMove(t *testing.T) {
	for _, tc := range []struct {
		name, target, env                     string
		status                                int
		insecure, existing, wrongToken, moved bool
	}{
		{name: "verified move", target: "https://new.example", status: 200, moved: true},
		{name: "unauthorized", target: "https://new.example", status: 401},
		{name: "server error", target: "https://new.example", status: 502},
		{name: "insecure TLS", target: "https://new.example", status: 200, insecure: true},
		{name: "plaintext target", target: "http://new.example", status: 200},
		{name: "userinfo target", target: "https://user@new.example", status: 200},
		{name: "path target", target: "https://new.example/evil", status: 200},
		{name: "query target", target: "https://new.example?evil", status: 200},
		{name: "fragment target", target: "https://new.example#evil", status: 200},
		{name: "existing destination", target: "https://new.example", status: 200, existing: true},
		{name: "different token", target: "https://new.example", status: 200, wrongToken: true},
		{name: "environment token", target: "https://new.example", status: 200, env: "SHPYRD_TOKEN"},
		{name: "environment URL", target: "https://new.example", status: 200, env: "SHPYRD_URL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("SHPYRD_TOKEN", "")
			t.Setenv("SHPYRD_URL", "")
			if tc.env != "" {
				t.Setenv(tc.env, "override")
			}
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(workspaceURLHeader, tc.target)
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			client := srv.Client()
			if tc.insecure {
				client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			}
			resp, err := client.Get(srv.URL + "/api/projects")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			expires := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
			s := &loginSessions{Current: srv.URL, Sessions: map[string]*loginSession{srv.URL: {URL: srv.URL, Token: "secret", WhoAmI: "me", ExpiresAt: expires}}}
			if tc.existing {
				s.Sessions[tc.target] = &loginSession{URL: tc.target, Token: "other"}
			}
			if err := s.save(); err != nil {
				t.Fatal(err)
			}
			token := "secret"
			if tc.wrongToken {
				token = "different"
			}
			var out bytes.Buffer
			migrateWorkspaceSession(srv.URL, token, resp, &out)
			got := loadSessions()
			if tc.moved {
				if got.Current != tc.target || got.Sessions[srv.URL] != nil || got.Sessions[tc.target].Token != "secret" || got.Sessions[tc.target].WhoAmI != "me" || !got.Sessions[tc.target].ExpiresAt.Equal(expires) || !strings.Contains(out.String(), "updated your saved session") {
					t.Fatalf("move: %+v, %s", got, out.String())
				}
			} else if got.Current != srv.URL || got.Sessions[srv.URL] == nil || out.Len() != 0 {
				t.Fatalf("unsafe move: %+v, %s", got, out.String())
			}
			if tc.existing && got.Sessions[tc.target].Token != "other" {
				t.Fatal("overwrote destination session")
			}
		})
	}
}

func TestWorkspaceMoveRejectsChangedOrigin(t *testing.T) {
	for _, origin := range []string{"http://old.example", "https://redirect.example"} {
		u, _ := url.Parse(origin)
		resp := &http.Response{Request: &http.Request{URL: u}, Header: http.Header{workspaceURLHeader: {"https://new.example"}}, TLS: &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{}}}}
		if got := workspaceMoveURL("https://old.example", resp); got != "" {
			t.Fatalf("accepted %s: %s", origin, got)
		}
	}
}

func TestSourceUploadRefusedDiscoversMoveWithoutCredentialsOrRetry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	posts, probes := 0, 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/sources":
			posts++
			if r.Header.Get("Authorization") != "Bearer secret" {
				t.Error("missing upload credential")
			}
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		case "/api/config":
			probes++
			if r.Header.Get("Authorization") != "" || r.Method != "GET" {
				t.Error("probe must be an unauthenticated GET")
			}
			w.Header().Set(workspaceURLHeader, "https://new.example")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	previous := http.DefaultClient
	http.DefaultClient = srv.Client()
	t.Cleanup(func() { http.DefaultClient = previous })
	_, err := serverRequestDirect(context.Background(), srv.URL, "secret", "POST", "api/sources", make([]byte, 1100000), "application/gzip")
	if err == nil || !strings.Contains(err.Error(), "1.10 MB at "+srv.URL) || !strings.Contains(err.Error(), "shpyrd login --url https://new.example") {
		t.Fatalf("error = %v", err)
	}
	if posts != 1 || probes != 1 {
		t.Fatalf("posts %d, probes %d", posts, probes)
	}
	if len(loadSessions().Sessions) != 0 {
		t.Fatal("unauthenticated hint saved a session")
	}
}
