package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/api"
)

// `shpyrd maintenance on --message` and `off` are one PUT each, and the
// command says how the project answers afterwards; without a subcommand it
// reads the detail, in words or as JSON.
func TestMaintenanceOnOffAndShow(t *testing.T) {
	since := metav1.Now()
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PUT" && r.URL.Path == "/api/projects/shop/maintenance":
			if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
				t.Error(err)
			}
			sum := api.AppSummary{Slug: "shop"}
			if sent["enabled"] == true {
				sum.Maintenance = &shpyrdv1.Maintenance{Enabled: true, Message: sent["message"].(string), Since: &since, By: "ana@acme.test"}
			}
			_ = json.NewEncoder(w).Encode(sum)
		case r.Method == "GET" && r.URL.Path == "/api/projects/shop":
			_ = json.NewEncoder(w).Encode(api.AppDetail{Slug: "shop", Spec: api.AppDetailSpec{Maintenance: &shpyrdv1.Maintenance{Enabled: true, Message: "Back at 15:00", Since: &since, By: "ana@acme.test"}}})
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
	run := func(args ...string) string {
		root := New()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out.String()
	}

	out := run("maintenance", "on", "--project", "shop", "--message", "Back at 15:00")
	if sent["enabled"] != true || sent["message"] != "Back at 15:00" {
		t.Errorf("on sent %v", sent)
	}
	if !strings.Contains(out, "shop serves its maintenance page since") || !strings.Contains(out, "by ana@acme.test") || !strings.Contains(out, `"Back at 15:00"`) {
		t.Errorf("on printed:\n%s", out)
	}

	out = run("maintenance", "off", "--project", "shop")
	if sent["enabled"] != false || sent["message"] != "" {
		t.Errorf("off sent %v", sent)
	}
	if !strings.Contains(out, "shop serves the app: maintenance is off.") {
		t.Errorf("off printed:\n%s", out)
	}

	out = run("maintenance", "--project", "shop")
	if !strings.Contains(out, "shop serves its maintenance page since") {
		t.Errorf("show printed:\n%s", out)
	}
	out = run("maintenance", "--project", "shop", "--json")
	var v struct {
		Project     string               `json:"project"`
		Maintenance shpyrdv1.Maintenance `json:"maintenance"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.Project != "shop" || !v.Maintenance.Enabled || v.Maintenance.Message != "Back at 15:00" || v.Maintenance.By != "ana@acme.test" {
		t.Errorf("--json = %s (%v)", out, err)
	}
}
