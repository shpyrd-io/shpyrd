package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
)

// A customer's agent has a workspace session and no kubeconfig: it still
// sees the catalog it must choose a size from, through the API (issue #63).
func TestSizesListThroughTheAPI(t *testing.T) {
	cat := sizes.Defaults()
	served := api.SizesResponse{Default: cat.Default, Sizes: cat.Sorted(), DatabaseMinMemory: sizes.DBMinMemory, DatabaseDefaultMemory: sizes.DBDefaultMemory}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sizes" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, `{"error":"sign in"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(served)
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("SHPYRD_URL", srv.URL)
	t.Setenv("SHPYRD_TOKEN", "test-token")
	defer func(prev bool) { preferKubeconfig = prev }(preferKubeconfig)

	run := func(args ...string) string {
		var out, errOut bytes.Buffer
		root := New()
		root.SetOut(&out)
		root.SetErr(&errOut)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, errOut.String())
		}
		return out.String()
	}

	// `sizes list`, and `sizes` alone, print the table.
	for _, args := range [][]string{{"sizes", "list"}, {"sizes"}} {
		text := run(args...)
		if !strings.HasPrefix(text, "NAME") || !strings.Contains(text, cat.Default+" (default)") {
			t.Errorf("%v:\n%s", args, text)
		}
	}

	// --json is the catalog as the API gives it.
	var got api.SizesResponse
	if err := json.Unmarshal([]byte(run("sizes", "list", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if got.Default != served.Default || len(got.Sizes) != len(served.Sizes) || got.DatabaseMinMemory != sizes.DBMinMemory {
		t.Errorf("--json = %+v, want %+v", got, served)
	}
}
