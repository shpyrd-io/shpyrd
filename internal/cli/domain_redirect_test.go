package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDomainRedirectCommands(t *testing.T) {
	for _, tc := range []struct {
		args               []string
		path, body, output string
	}{
		{[]string{"domains", "redirect", "shop.example.com", "--project", "shop"}, "/api/projects/shop/domains", `{"redirectTo":"shop.example.com"}`, "Redirect to:"},
		{[]string{"domains", "redirect", "off", "--project", "shop"}, "/api/projects/shop/domains", `{"redirectTo":""}`, "Project hostname:"},
		{[]string{"workspace", "domains", "redirect", "acme.example.com"}, "/api/workspace/domains/acme.example.com", `{"redirectApps":true}`, "app addresses redirect"},
		{[]string{"workspace", "domains", "serve", "acme.example.com"}, "/api/workspace/domains/acme.example.com", `{"redirectApps":false}`, "acme.example.com"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			called := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "PATCH" || r.URL.Path != tc.path {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				var got, want map[string]interface{}
				_ = json.NewDecoder(r.Body).Decode(&got)
				_ = json.Unmarshal([]byte(tc.body), &want)
				gb, _ := json.Marshal(got)
				wb, _ := json.Marshal(want)
				if string(gb) != string(wb) {
					t.Errorf("body=%s want=%s", gb, wb)
				}
				called = true
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(tc.path, "workspace") {
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"host": "acme.example.com", "primary": true, "verified": true, "redirectApps": got["redirectApps"]})
				} else {
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"target": "shop.example.test", "redirectTo": got["redirectTo"], "domains": []interface{}{}})
				}
			}))
			defer srv.Close()
			t.Setenv("SHPYRD_URL", srv.URL)
			t.Setenv("SHPYRD_TOKEN", "test-token")
			root := New()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs(tc.args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if !called || !strings.Contains(out.String(), tc.output) {
				t.Fatalf("output=%s called=%v", out.String(), called)
			}
		})
	}
}
