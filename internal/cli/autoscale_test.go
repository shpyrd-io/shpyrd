package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// RFC-0047: `shpyrd autoscale <project> --min 2 --max 10` sends the bounds
// on the processes endpoint, with the CPU and concurrency targets only
// when given; --off clears them; and the command says what it did.
func TestAutoscaleSendsBoundsAndSaysWhatTheyMean(t *testing.T) {
	for _, tc := range []struct {
		args []string
		sent map[string]any
		text string
	}{
		{[]string{"--min", "2", "--max", "10"}, map[string]any{"min": 2.0, "max": 10.0}, "shop follows its load: web runs between 2 and 10 instances, another when an instance passes 70% of its CPU or 20 requests in flight."},
		{[]string{"--min", "1", "--max", "4", "--cpu", "50", "--concurrency", "8"}, map[string]any{"min": 1.0, "max": 4.0, "cpu": 50.0, "concurrency": 8.0}, "web runs between 1 and 4 instances, another when an instance passes 50% of its CPU or 8 requests in flight."},
		{[]string{"--off"}, map[string]any{"off": true}, "Autoscaling is off for shop: web keeps a fixed count (shpyrd scale web=N)."},
	} {
		var sent map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" && r.URL.Path == "/api/projects/shop/processes" {
				if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
					t.Error(err)
				}
				_, _ = w.Write([]byte(`{"slug":"shop"}`))
				return
			}
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}))
		t.Setenv("HOME", t.TempDir())
		t.Setenv("KUBECONFIG", "")
		t.Setenv("SHPYRD_URL", srv.URL)
		t.Setenv("SHPYRD_TOKEN", "test-token")
		root := New()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(append([]string{"autoscale", "shop"}, tc.args...))
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		srv.Close()
		web, _ := sent["processes"].(map[string]any)["web"].(map[string]any)
		got, _ := web["autoscale"].(map[string]any)
		if len(got) != len(tc.sent) {
			t.Errorf("%v sent %v, want %v", tc.args, got, tc.sent)
		}
		for k, v := range tc.sent {
			if got[k] != v {
				t.Errorf("%v sent %s=%v, want %v", tc.args, k, got[k], v)
			}
		}
		if !strings.Contains(out.String(), tc.text) {
			t.Errorf("%v printed:\n%s\nwant %q", tc.args, out.String(), tc.text)
		}
	}
}

// Half the bounds, or bounds with --off, are refused before any request.
func TestAutoscaleNeedsBothBounds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("SHPYRD_URL", "http://127.0.0.1:1")
	t.Setenv("SHPYRD_TOKEN", "test-token")
	for _, args := range [][]string{{"--min", "2"}, {"--max", "4"}, {"--off", "--min", "1", "--max", "2"}} {
		root := New()
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetArgs(append([]string{"autoscale", "shop"}, args...))
		if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "--") {
			t.Errorf("%v: err = %v, want a refusal naming the flags", args, err)
		}
	}
}

// `shpyrd autoscale <project>` with no flag reads the bounds back: what
// applies, whether the cluster follows it, and how to change it.
func TestAutoscaleReadsTheBoundsBack(t *testing.T) {
	for _, tc := range []struct {
		spec, status string
		text         []string
	}{
		{`{}`, ``, []string{"shop keeps a fixed count of web instances (shpyrd scale web=N).", "shpyrd autoscale shop --min 2 --max 10"}},
		{`{"autoscale":{"min":2,"max":10}}`, `{"min":2,"max":10,"cpu":70,"concurrency":20,"state":"active"}`, []string{"shop follows its load: web runs between 2 and 10 instances, another when an instance passes 70% of its CPU or 20 requests in flight.", "Change it: shpyrd autoscale shop --min N --max M"}},
		{`{"autoscale":{"min":2,"max":10,"cpu":50}}`, `{"min":2,"max":10,"cpu":50,"concurrency":20,"state":"active","message":"the count follows the requests in flight only"}`, []string{"passes 50% of its CPU", "Note: the count follows the requests in flight only"}},
		{`{"autoscale":{"min":2,"max":10}}`, `{"min":2,"max":10,"cpu":70,"concurrency":20,"state":"unavailable","message":"autoscaling needs the autoscale extension"}`, []string{"Not following it now: autoscaling needs the autoscale extension"}},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && r.URL.Path == "/api/projects/shop" {
				status := `{"desired":2,"ready":2`
				if tc.status != "" {
					status += `,"autoscale":` + tc.status
				}
				_, _ = w.Write([]byte(`{"slug":"shop","spec":{"processes":{"web":` + tc.spec + `}},"processes":{"web":` + status + `}}}`))
				return
			}
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}))
		t.Setenv("HOME", t.TempDir())
		t.Setenv("KUBECONFIG", "")
		t.Setenv("SHPYRD_URL", srv.URL)
		t.Setenv("SHPYRD_TOKEN", "test-token")
		root := New()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{"autoscale", "shop"})
		if err := root.Execute(); err != nil {
			t.Fatalf("%s: %v", tc.spec, err)
		}
		srv.Close()
		for _, want := range tc.text {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%s / %s printed:\n%s\nwant %q", tc.spec, tc.status, out.String(), want)
			}
		}
	}
}

// `shpyrd scale web=3` on a process with bounds says the count is now the
// minimum (the server set it): the detail read after the scale says which
// processes have bounds.
func TestScaleSaysWhenTheCountIsAMinimum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/projects/shop/scale":
			_, _ = w.Write([]byte(`{"slug":"shop"}`))
		case r.Method == "GET" && r.URL.Path == "/api/projects/shop":
			_, _ = w.Write([]byte(`{"slug":"shop","spec":{"processes":{"web":{"autoscale":{"min":3,"max":10}},"worker":{}}}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("SHPYRD_URL", srv.URL)
	t.Setenv("SHPYRD_TOKEN", "test-token")
	root := New()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"scale", "web=3", "worker=1", "--app", "shop"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Scaling shop: web=3 worker=1") || !strings.Contains(out.String(), "web follows its load between 3 and 10 instances; the count is its minimum.") || strings.Contains(out.String(), "worker follows") {
		t.Errorf("printed:\n%s", out.String())
	}
}
