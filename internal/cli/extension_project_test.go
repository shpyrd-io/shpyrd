package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestExtensionProjectSelection(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		// Stop at the first request, including commands that would otherwise
		// poll or mutate resources. These tests only exercise project selection.
		http.Error(w, `{"error":"selection test"}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("SHPYRD_URL", srv.URL)
	t.Setenv("SHPYRD_TOKEN", "test-token")
	defer func(prev bool) { preferKubeconfig = prev }(preferKubeconfig)

	commands := [][]string{
		{"pg", "metrics", "db"}, {"redis", "metrics", "cache"},
		{"pg", "list"}, {"redis", "list"},
		{"pg", "info", "db"}, {"redis", "info", "cache"},
		{"pg", "resize", "db", "shared-m"}, {"redis", "resize", "cache", "shared-m"},
		{"pg", "backups", "list", "db"},
		{"pg", "sleep", "db", "--after", "30m"},
		{"pg", "suspend", "db"}, {"pg", "resume", "db"},
	}
	for _, tc := range []struct {
		name, config, flag, project, err string
	}{
		{name: "config", config: "project: shop\n", project: "shop"},
		{name: "legacy alias", config: "app: legacy\n", project: "legacy"},
		{name: "project over alias", config: "project: shop\napp: legacy\n", project: "shop"},
		{name: "explicit override", config: "project: shop\n", flag: "other", project: "other"},
		{name: "explicit without config", flag: "other", project: "other"},
		{name: "explicit ignores malformed config", config: "project: [\n", flag: "other", project: "other"},
		{name: "missing config", err: "no project selected"},
		{name: "empty config", config: "{}\n", err: "no project selected"},
		{name: "malformed config", config: "project: [\n", err: "shpyrd.yaml:"},
		{name: "invalid config project", config: "project: Invalid!\n", err: "invalid project"},
		{name: "invalid explicit project", config: "project: shop\n", flag: "Invalid!", err: "invalid project"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if tc.config != "" {
				if err := os.WriteFile("shpyrd.yaml", []byte(tc.config), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, command := range commands {
				path = ""
				args := append([]string{}, command...)
				if tc.flag != "" {
					args = append(args, "--project", tc.flag)
				}
				root := New()
				root.SetOut(io.Discard)
				root.SetErr(io.Discard)
				root.SetArgs(args)
				err := root.Execute()
				if tc.err != "" {
					if err == nil || !strings.Contains(err.Error(), tc.err) || path != "" {
						t.Fatalf("%v: error %v, request %q; want %q without a request", args, err, path, tc.err)
					}
				} else if !strings.HasPrefix(path, "/api/projects/"+tc.project+"/resources") {
					t.Fatalf("%v: request %q, error %v; want project %s", args, path, err, tc.project)
				}
			}
		})
	}
}
