package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/api"
)

// #35: `shpyrd access` reads the mode and the URL where the project's detail
// carries them, spec.access and status.url.
func TestAccessShowsTheProjectsMode(t *testing.T) {
	const url = "https://docs.example.test"
	for _, tc := range []struct {
		access, text string
	}{
		{shpyrdv1.AccessPublic, "docs is public: anyone can open " + url},
		{shpyrdv1.AccessIdentified, "docs is public; signed-in visitors are identified to the app"},
		{shpyrdv1.AccessAuthenticated, "docs asks visitors to sign in; these roles open it:"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/projects/docs":
				_ = json.NewEncoder(w).Encode(api.AppDetail{Slug: "docs", Spec: api.AppDetailSpec{Access: tc.access}, Status: api.AppDetailStatus{URL: url}})
			case "/api/projects/docs/members":
				_, _ = w.Write([]byte(`[]`))
			default:
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				http.NotFound(w, r)
			}
		}))
		t.Setenv("HOME", t.TempDir())
		t.Setenv("KUBECONFIG", "")
		t.Setenv("SHPYRD_URL", srv.URL)
		t.Setenv("SHPYRD_TOKEN", "test-token")
		for _, asJSON := range []bool{false, true} {
			root := New()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			args := []string{"access", "--project", "docs"}
			if asJSON {
				args = append(args, "--json")
			}
			root.SetArgs(args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if asJSON {
				var v struct {
					Access string `json:"access"`
					URL    string `json:"url"`
				}
				if err := json.Unmarshal(out.Bytes(), &v); err != nil || v.Access != tc.access || v.URL != url {
					t.Errorf("%s --json = %s, want access %q and url %q", tc.access, out.String(), tc.access, url)
				}
			} else if !strings.Contains(out.String(), tc.text) {
				t.Errorf("%s: got\n%s\nwant %q", tc.access, out.String(), tc.text)
			}
		}
		srv.Close()
	}
}
