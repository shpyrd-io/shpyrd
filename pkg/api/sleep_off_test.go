package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// #135: a project's sleep has three states, and the API keeps them apart.
// "off" is a policy of the project's own and is stored as such (the
// controller reads it as an opt-out of the workspace's default); "default"
// clears the policy so the workspace's default applies. Neither needs auto
// sleep or the add-on; only a quiet period does.
func TestSleepOffAndDefaultAreDistinct(t *testing.T) {
	cases := []struct {
		after string
		want  *shpyrdv1.SleepSpec
	}{
		{"off", &shpyrdv1.SleepSpec{After: "off"}},
		{"OFF", &shpyrdv1.SleepSpec{After: "off"}},
		{"false", &shpyrdv1.SleepSpec{After: "off"}},
		{"default", nil},
		{"", nil},
		{"15m", &shpyrdv1.SleepSpec{After: "15m0s", Resuming: "wait"}},
	}
	for _, tc := range cases {
		got, err := validateSleep("web", &shpyrdv1.SleepSpec{After: tc.after})
		if err != nil {
			t.Fatalf("after %q: %v", tc.after, err)
		}
		if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
			t.Errorf("after %q = %+v, want %+v", tc.after, got, tc.want)
		}
		if on := sleepTurnsOn(got); on != (tc.after == "15m") {
			t.Errorf("after %q turns sleep on = %v", tc.after, on)
		}
	}

	// Neither off nor default asks for auto sleep or the add-on.
	shop := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}, Spec: shpyrdv1.AppSpec{Processes: map[string]shpyrdv1.Process{"web": {}}}}
	s, _ := newTestServer(t, nil, []client.Object{shop})
	s.sleepGate = func() bool { return false }
	s.sleepAvailable = func() bool { return false }
	for _, after := range []string{"off", "default"} {
		if rec := do(t, s, "POST", "/api/projects/shop/processes", `{"processes":{"web":{"sleep":{"after":"`+after+`"}}}}`, true); rec.Code != http.StatusOK {
			t.Errorf("sleep %s without auto sleep or the add-on = %d %s", after, rec.Code, rec.Body)
		}
	}

	// The change's description tells the two apart, so a trail never
	// reads "off" for a project that was handed back to the workspace.
	detail := processChangesDetail(map[string]ProcessChange{
		"web": {Sleep: &shpyrdv1.SleepSpec{After: "off"}},
	})
	if detail != "web sleep off" {
		t.Errorf("detail for off = %q", detail)
	}
	detail = processChangesDetail(map[string]ProcessChange{
		"web": {Sleep: &shpyrdv1.SleepSpec{After: "default"}},
	})
	if detail != "web sleep workspace default" {
		t.Errorf("detail for default = %q", detail)
	}
	detail = processChangesDetail(map[string]ProcessChange{
		"web": {Sleep: &shpyrdv1.SleepSpec{After: "15m", Resuming: "page"}, Replicas: ptr.To[int32](2)},
	})
	if detail != "web=2 web sleep after 15m0s (page)" {
		t.Errorf("detail for a period = %q", detail)
	}
}

// The workspace's own defaults are a different thing from a project's
// policy: "off" there means no default, never a stored off.
func TestWorkspaceSleepDefaultOffIsNoDefault(t *testing.T) {
	out, err := NormalizeWorkspaceSettings(WorkspaceSettings{Sleep: &store.SleepDefaults{AppsAfter: "off", DatabasesAfter: "off"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Sleep != nil {
		t.Errorf("off as a workspace default = %+v, want none", out.Sleep)
	}
	out, err = NormalizeWorkspaceSettings(WorkspaceSettings{Sleep: &store.SleepDefaults{AppsAfter: "10m", AppsResuming: "page"}})
	if err != nil || out.Sleep == nil || out.Sleep.AppsAfter != "10m" || out.Sleep.AppsResuming != "page" {
		t.Errorf("a period as a workspace default = %+v, %v", out.Sleep, err)
	}
}

// The sentences the sleep endpoints answer with when a value is refused
// follow #52: they say what to send in words, never a command, a tool or a
// Kubernetes name.
func TestSleepRefusalsNameNoInternals(t *testing.T) {
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}, Spec: shpyrdv1.AppSpec{Processes: map[string]shpyrdv1.Process{"web": {}}}}
	db := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-shop"}}
	s, _ := newTestServer(t, nil, []client.Object{app, db})
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/api/projects/shop/processes", `{"processes":{"web":{"sleep":{"after":"soon"}}}}`},
		{"POST", "/api/projects/shop/processes", `{"processes":{"web":{"sleep":{"after":"1m"}}}}`},
		{"POST", "/api/projects/shop/processes", `{"processes":{"web":{"sleep":{"after":"15m","resuming":"spinner"}}}}`},
		{"POST", "/api/projects/shop/processes", `{"processes":{"worker":{"sleep":{"after":"15m"}}}}`},
		{"PATCH", "/api/projects/shop/resources/postgres/db/sleep", `{}`},
		{"PATCH", "/api/projects/shop/resources/postgres/db/sleep", `{"sleep":{"after":"later"}}`},
		{"PATCH", "/api/projects/shop/resources/postgres/db/sleep", `{"sleep":{"after":"48h"}}`},
	} {
		rec := do(t, s, c.method, c.path, c.body, true)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s %s %s = %d %s, want a refusal", c.method, c.path, c.body, rec.Code, rec.Body.String())
			continue
		}
		var v map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		msg, _ := v["error"].(string)
		if strings.TrimSpace(msg) == "" {
			t.Errorf("%s %s: no error sentence in %s", c.method, c.body, rec.Body.String())
		}
		if bad := controller.PlatformWordingFault(msg); bad != "" {
			t.Errorf("%s %s: %q names %q", c.method, c.body, msg, bad)
		}
	}
}
