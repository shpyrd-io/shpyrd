package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/api"
)

func TestEnvAndSecretsUsePendingConfigAPI(t *testing.T) {
	var sent api.ConfigVarsUpdate
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/projects/shop" {
			_, _ = w.Write([]byte(`{"slug":"shop","status":{"digest":"registry/app@sha256:one"}}`))
			return
		}
		if r.URL.Path != "/api/projects/shop/secrets" {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method == "PUT" {
			if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
				t.Error(err)
			}
		}
		_, _ = w.Write([]byte(`{"vars":[{"name":"PASSWORD"}],"variables":[{"name":"APP_URL","value":"https://example.test"}],"pending":true}`))
	}))
	defer srv.Close()
	t.Setenv("SHPYRD_URL", srv.URL)
	t.Setenv("SHPYRD_TOKEN", "test-token")
	for _, tc := range []struct {
		args         []string
		want, absent string
	}{
		{[]string{"env", "set", "APP_URL=https://example.test", "--confirm-plain"}, "redeploy", "Restarting"},
		{[]string{"env", "list"}, "APP_URL=https://example.test", "PASSWORD"},
		{[]string{"secrets", "list"}, "PASSWORD", "https://example.test"},
		{[]string{"secrets", "list", "--json"}, "PASSWORD", "https://example.test"},
	} {
		root := New()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(append(tc.args, "--project", "shop"))
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), tc.want) || strings.Contains(out.String(), tc.absent) {
			t.Fatalf("%v: %s", tc.args, out.String())
		}
	}
	if sent.Kind != "variable" || sent.Set["APP_URL"] != "https://example.test" || !sent.ConfirmPlain {
		t.Fatalf("request = %+v", sent)
	}
}
