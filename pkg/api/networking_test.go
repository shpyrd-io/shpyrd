package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestInternalExposureDefaultsAndOverrides(t *testing.T) {
	cloud := InternalExposurePolicy(func(w *store.Workspace) bool { return w.OwnedByOperator() })
	for _, tc := range []struct {
		name, owner string
		policy      InternalExposurePolicy
		override    *bool
		want        bool
	}{
		{"OSS", store.WorkspaceOwnerOperator, nil, nil, true},
		{"self-hosted custom workspace", store.WorkspaceOwnerCustomer, nil, nil, true},
		{"cloud operator", store.WorkspaceOwnerOperator, cloud, nil, true},
		{"cloud customer", store.WorkspaceOwnerCustomer, cloud, nil, false},
		{"manual connection", store.WorkspaceOwnerCustomer, cloud, ptr.To(true), true},
		{"operator disabled", store.WorkspaceOwnerOperator, cloud, ptr.To(false), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &store.Workspace{Owner: tc.owner, Settings: store.WorkspaceSettings{InternalExposure: tc.override}}
			if got := EffectiveInternalExposure(w, tc.policy); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInternalExposureCreateAndMutations(t *testing.T) {
	s, cr := newTestServer(t, nil, nil)
	s.opts.InternalExposure = func(*store.Workspace) bool { return false }
	// Rejection is before creating even the namespace.
	rec := do(t, s, "POST", "/api/projects", `{"name":"Private","slug":"private","exposure":"internal"}`, true)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "internal_exposure_unavailable") {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var namespaces corev1.NamespaceList
	if err := cr.List(t.Context(), &namespaces); err != nil || len(namespaces.Items) != 0 {
		t.Fatalf("side effects: %+v %v", namespaces, err)
	}
	for _, path := range []string{"/api/config", "/api/workspace"} {
		rec = do(t, s, "GET", path, "", true)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"internalExposure":false`) {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	rec = do(t, s, "POST", "/api/projects", `{"name":"Shop","slug":"shop"}`, true)
	if rec.Code != 201 {
		t.Fatalf("create external: %d %s", rec.Code, rec.Body.String())
	}
	for _, tc := range []struct{ method, path, body string }{
		{"PUT", "/api/projects/shop/exposure", `{"exposure":"internal"}`},
		{"POST", "/api/projects/shop/deploy", `{"image":"example.test/shop:v1","exposure":"internal"}`},
	} {
		rec = do(t, s, tc.method, tc.path, tc.body, true)
		if rec.Code != 403 {
			t.Fatalf("%s: %d %s", tc.path, rec.Code, rec.Body.String())
		}
	}
	// A fresh store read observes the override despite an already resolved tenant.
	if _, err := s.store.SetWorkspaceInternalExposure(t.Context(), store.DefaultWorkspace, ptr.To(true)); err != nil {
		t.Fatal(err)
	}
	rec = do(t, s, "PUT", "/api/projects/shop/exposure", `{"exposure":"internal"}`, true)
	if rec.Code != 200 {
		t.Fatalf("enabled: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := s.store.SetWorkspaceInternalExposure(t.Context(), store.DefaultWorkspace, ptr.To(false)); err != nil {
		t.Fatal(err)
	}
	// Legacy internal apps remain maintainable, including declarative redeployment.
	rec = do(t, s, "POST", "/api/projects/shop/deploy", `{"image":"example.test/shop:v2","exposure":"internal"}`, true)
	if rec.Code != 202 {
		t.Fatalf("legacy deploy: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, s, "PUT", "/api/projects/shop/exposure", `{"exposure":"external"}`, true)
	if rec.Code != 200 {
		t.Fatalf("external: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSelfHostedInternalCreation(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	rec := do(t, s, "POST", "/api/projects", `{"name":"Private","slug":"private","exposure":"internal"}`, true)
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"exposure":"internal"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestInternalArchiveRejectedBeforeMaintenance(t *testing.T) {
	app := sampleApp("private", shpyrdv1.PhasePending)
	app.Spec.Source = nil
	app.Status.Image = ""
	app.Status.URL = ""
	app.Spec.Exposure = "internal"
	s, cr := newTestServer(t, nil, []client.Object{app})
	rec := do(t, s, "POST", "/api/project-archives/private/export", "", true)
	if rec.Code != 200 {
		t.Fatalf("export: %d %s", rec.Code, rec.Body.String())
	}
	var ticket struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ticket); err != nil {
		t.Fatal(err)
	}
	rec = do(t, s, "GET", "/api/project-archives/private/download?ticket="+ticket.Ticket, "", true)
	if rec.Code != 200 {
		t.Fatalf("download: %d %s", rec.Code, rec.Body.String())
	}
	archive := rec.Body.String()
	rec = do(t, s, "PUT", "/api/projects/private/exposure", `{"exposure":"external"}`, true)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	// Check both the workspace and cluster operator routes.
	s.opts.InternalExposure = func(*store.Workspace) bool { return false }
	for _, path := range []string{"/api/project-archives/private/restore", "/api/cluster/project-archives/" + archiveProjectID(app) + "/restore"} {
		rec = do(t, s, "POST", path, archive, true)
		if rec.Code != 403 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
		current := &shpyrdv1.App{}
		if err := cr.Get(t.Context(), client.ObjectKeyFromObject(app), current); err != nil {
			t.Fatal(err)
		}
		if current.Spec.Exposure != "external" || current.Annotations[shpyrdv1.AnnotationMaintenance] != "" {
			t.Fatalf("restore changed app: %+v", current)
		}
	}
}

func TestClusterRestoreUsesDestinationWorkspace(t *testing.T) {
	app := sampleApp("private", shpyrdv1.PhasePending)
	app.Spec.Source = nil
	app.Status.Image, app.Status.URL = "", ""
	app.Spec.Exposure = "internal"
	s, cr := newTestServer(t, nil, []client.Object{app})
	rec := do(t, s, "POST", "/api/project-archives/private/export", "", true)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	var ticket struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ticket); err != nil {
		t.Fatal(err)
	}
	rec = do(t, s, "GET", "/api/project-archives/private/download?ticket="+ticket.Ticket, "", true)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	archive := rec.Body.String()
	if _, err := s.store.CreateWorkspace(t.Context(), store.Workspace{Slug: "acme", Name: "Acme", Owner: store.WorkspaceOwnerCustomer}); err != nil {
		t.Fatal(err)
	}
	if err := cr.Get(t.Context(), client.ObjectKeyFromObject(app), app); err != nil {
		t.Fatal(err)
	}
	app.Spec.Exposure = "external"
	app.Labels = map[string]string{shpyrdv1.LabelWorkspace: "acme"}
	if err := cr.Update(t.Context(), app); err != nil {
		t.Fatal(err)
	}
	s.opts.InternalExposure = func(w *store.Workspace) bool { return w.Slug != "acme" }
	rec = do(t, s, "POST", "/api/cluster/project-archives/"+archiveProjectID(app)+"/restore", archive, true)
	if rec.Code != 403 {
		t.Fatalf("operator bypassed destination policy: %d %s", rec.Code, rec.Body.String())
	}
}
