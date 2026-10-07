package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestMetricsCommands(t *testing.T) {
	const body = `{"range":"6h","step":360,"charts":[{"id":"cpu","title":"CPU","unit":"cores","series":[{"name":"web","points":[[1700000000,0.1],[1700000060,0.25]]}]},{"title":"Memory","series":[]},{"title":"Network","error":"query failed"}],"releases":[],"futureField":true}`
	var path, query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		if r.Method != "GET" {
			t.Errorf("method %s", r.Method)
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("SHPYRD_URL", srv.URL)
	t.Setenv("SHPYRD_TOKEN", "test-token")
	defer func(prev bool) { preferKubeconfig = prev }(preferKubeconfig)
	for _, tc := range []struct {
		args []string
		path string
	}{
		{[]string{"metrics", "--process", "web", "--mode", "total", "--by", "instance", "--agg", "avg", "--replaced"}, "/api/projects/shop/metrics"},
		{[]string{"pg", "metrics", "db"}, "/api/projects/shop/resources/postgres/db/metrics"},
		{[]string{"redis", "metrics", "cache"}, "/api/projects/shop/resources/redis/cache/metrics"},
	} {
		for _, format := range []string{"text", "json", "jq"} {
			root := New()
			var out, stderr bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&stderr)
			args := append(append([]string{}, tc.args...), "--project", "shop", "--range", "6h")
			if format == "json" {
				args = append(args, "--json")
			}
			if format == "jq" {
				args = append(args, "--jq", ".charts[0].series[0].points | length")
			}
			root.SetArgs(args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if path != tc.path || !strings.Contains(query, "range=6h") {
				t.Fatalf("request %s?%s", path, query)
			}
			if tc.args[0] == "metrics" && (!strings.Contains(query, "process=web") || !strings.Contains(query, "agg=avg") || !strings.Contains(query, "replaced=true")) {
				t.Fatal(query)
			}
			switch format {
			case "json":
				if stderr.Len() != 0 {
					t.Fatalf("JSON wrote to stderr: %s", stderr.String())
				}
				var v map[string]any
				if err := json.Unmarshal(out.Bytes(), &v); err != nil || v["futureField"] != true {
					t.Fatalf("JSON lost data: %s", out.String())
				}
				var expected map[string]any
				if err := json.Unmarshal([]byte(body), &expected); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(v, expected) {
					t.Fatalf("JSON changed the API series or metadata: %s", out.String())
				}
			case "jq":
				if out.String() != "2\n" {
					t.Fatal(out.String())
				}
			default:
				for _, want := range []string{"0.25", "cores", "2023-11-14T22:14:20Z", "No data", "query failed"} {
					if !strings.Contains(out.String(), want) {
						t.Fatalf("missing %s: %s", want, out.String())
					}
				}
			}
		}
	}
	for _, args := range [][]string{
		{"metrics", "--range", "2h"}, {"metrics", "--mode", "bad"}, {"metrics", "--by", "bad"}, {"metrics", "--agg", "sum"}, {"metrics", "--replaced"},
		{"pg", "metrics"}, {"redis", "metrics", "cache"}, {"pg", "metrics", "db", "--project", "shop", "--range", "2h"},
	} {
		path = ""
		root := New()
		root.SetArgs(args)
		if err := root.Execute(); err == nil {
			t.Fatalf("accepted %v", args)
		}
		if path != "" {
			t.Fatalf("invalid args sent request: %v", args)
		}
	}
}
