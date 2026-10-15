package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext/all"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// A shared database's details say so and carry what the screen reads: the
// shared server's version, the cap as written and in bytes, what it holds
// from the percentage the metering recorded, whether it goes through the
// server's pooler, and its nightly copies; never a size, instances or a
// disk's backup window. A dedicated one says "dedicated" and keeps its own.
func TestPostgresDetailsSayTheKind(t *testing.T) {
	s, cr, _ := newTenantServer(t)
	s.opts.Extensions = all.All()
	ctx := context.Background()
	const ns = "app-acme-shop"
	measured := &shpyrdv1.Postgres{
		ObjectMeta: metav1.ObjectMeta{Name: "analytics", Namespace: ns},
		Spec:       shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeShared, Storage: ptr.To(resource.MustParse("2Gi")), Backups: &shpyrdv1.PostgresBackups{Retention: "14d"}},
		Status:     shpyrdv1.ResourceStatus{Phase: shpyrdv1.ResourceReady, Storage: "614Mi of 2Gi", CapPercent: ptr.To(int32(30))},
	}
	fresh := &shpyrdv1.Postgres{
		ObjectMeta: metav1.ObjectMeta{Name: "events", Namespace: ns},
		Spec:       shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeShared, Storage: ptr.To(resource.MustParse("1Gi"))},
	}
	dedicated := &shpyrdv1.Postgres{
		ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: ns},
		Spec:       shpyrdv1.PostgresSpec{Version: "16", Size: "shared-m", Storage: ptr.To(resource.MustParse("10Gi")), Instances: ptr.To(int32(1)), Backups: &shpyrdv1.PostgresBackups{Retention: "7d"}},
	}
	for _, o := range []client.Object{measured, fresh, dedicated} {
		if err := cr.Create(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	// The project's Service of analytics points at the server's pooler;
	// the one of events still at the server's primary.
	services := s.kube.Kube.CoreV1().Services(ns)
	for name, target := range map[string]string{"analytics": "pg-shared-1-pooler.shpyrd-shared-postgres.svc.cluster.local", "events": "pg-shared-1-rw.shpyrd-shared-postgres.svc.cluster.local"} {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: target}}
		if _, err := services.Create(ctx, svc, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	rec := at(t, s, "acme.shpyrd.test", "GET", "/api/projects/shop/resources", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("resources = %d %s", rec.Code, rec.Body.String())
	}
	var list []ResourceView
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	details := map[string]map[string]string{}
	for _, r := range list {
		if r.Kind == "Postgres" {
			details[r.Name] = r.Details
		}
	}
	if len(details) != 3 {
		t.Fatalf("databases = %s", rec.Body.String())
	}
	for name, want := range map[string]map[string]string{
		"analytics": {"type": "shared", "version": "17", "storage": "2Gi", "capBytes": "2147483648", "usedBytes": "644245094", "pooler": "on", "backups": "nightly, kept 14"},
		"events":    {"type": "shared", "version": "17", "storage": "1Gi", "capBytes": "1073741824", "usedBytes": "0", "pooler": "off", "backups": "nightly, kept 7"},
		"main":      {"type": "dedicated", "version": "16", "size": "shared-m", "storage": "10Gi", "instances": "1", "backups": "daily, kept 7d"},
	} {
		for k, v := range want {
			if got := details[name][k]; got != v {
				t.Errorf("%s %s = %q, want %q", name, k, got, v)
			}
		}
	}
	for _, name := range []string{"analytics", "events"} {
		for _, k := range []string{"size", "instances"} {
			if v, has := details[name][k]; has {
				t.Errorf("%s carries %s %q", name, k, v)
			}
		}
	}
	for _, k := range []string{"capBytes", "usedBytes", "pooler"} {
		if v, has := details["main"][k]; has {
			t.Errorf("a dedicated database carries %s %q", k, v)
		}
	}
}

// The config at a workspace door carries the plan's rules for databases,
// with the defaults when the workspace has none; the console has no plan.
func TestConfigCarriesThePostgresRulesAtAWorkspaceDoor(t *testing.T) {
	s, _, st := newTenantServer(t)
	ctx := context.Background()
	if rec := at(t, s, "acme.shpyrd.test", "GET", "/api/config", ""); !strings.Contains(rec.Body.String(), `"postgres":{"types":["shared","dedicated"],"defaultType":"dedicated","sharedCap":"1Gi"}`) {
		t.Errorf("the defaults: %s", rec.Body.String())
	}
	if _, err := st.UpdateWorkspaceSettings(ctx, "acme", store.WorkspaceSettings{Limits: &store.Limits{Postgres: &store.PostgresLimits{Types: []string{"shared"}, DefaultType: "shared", SharedCap: "2Gi"}}}); err != nil {
		t.Fatal(err)
	}
	if rec := at(t, s, "acme.shpyrd.test", "GET", "/api/config", ""); !strings.Contains(rec.Body.String(), `"postgres":{"types":["shared"],"defaultType":"shared","sharedCap":"2Gi"}`) {
		t.Errorf("the plan's rules: %s", rec.Body.String())
	}
	if rec := at(t, s, "shpyrd.example.test", "GET", "/api/config", ""); strings.Contains(rec.Body.String(), `"postgres":`) {
		t.Errorf("the console has no plan: %s", rec.Body.String())
	}
}
