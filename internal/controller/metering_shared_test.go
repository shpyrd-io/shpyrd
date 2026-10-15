package controller

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/prom"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Every shared server gets the seven-day p95 of its client connections
// written on it each cycle, a whole number; the pooler switch keeps its
// rule: on when the p95 passes 60 % of max_connections, and once on it
// stays on while the number follows the connections down.
func TestSharedServersRecordTheirConnectionsAndSwitchThePooler(t *testing.T) {
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	server := func(name string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(CNPGClusterGVK)
		u.SetNamespace(SharedPostgresNamespace)
		u.SetName(name)
		u.SetLabels(map[string]string{sharedServerLabel: "server"})
		_ = unstructured.SetNestedField(u.Object, "400", "spec", "postgresql", "parameters", "max_connections")
		return u
	}
	job := func(name string) map[string]string {
		return map[string]string{"job": SharedPostgresNamespace + "/" + name}
	}
	answers := map[string][]map[string]any{"cnpg_backends_total": {sample(job("pg-shared-1"), "250.4"), sample(job("pg-shared-2"), "30")}}
	srv := fakeProm(t, answers)
	defer srv.Close()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(server("pg-shared-1"), server("pg-shared-2"), server("pg-shared-3")).Build()
	m := &MeteringLoop{Store: store.NewMemory(), Prom: prom.NewClient(srv.URL), Client: c}
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	if err := m.sharedServerPoolers(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	stamps := func(name string) (p95, pooler string) {
		a := getU(t, c, "Cluster", SharedPostgresNamespace, name).GetAnnotations()
		return a[AnnotationConnectionsP95], a[AnnotationPooler]
	}
	if p95, pooler := stamps("pg-shared-1"); p95 != "250" || pooler != "2026-10-15T09:00:00Z" {
		t.Errorf("above 60 %%: p95 %q, pooler %q", p95, pooler)
	}
	if p95, pooler := stamps("pg-shared-2"); p95 != "30" || pooler != "" {
		t.Errorf("under it: p95 %q, pooler %q", p95, pooler)
	}
	if p95, pooler := stamps("pg-shared-3"); p95 != "0" || pooler != "" {
		t.Errorf("a server with no series yet: p95 %q, pooler %q", p95, pooler)
	}
	// The next cycle, the connections down: the number follows, the pooler stays.
	answers["cnpg_backends_total"] = []map[string]any{sample(job("pg-shared-1"), "10"), sample(job("pg-shared-2"), "30")}
	if err := m.sharedServerPoolers(context.Background(), at.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if p95, pooler := stamps("pg-shared-1"); p95 != "10" || pooler != "2026-10-15T09:00:00Z" {
		t.Errorf("after the connections fell: p95 %q, pooler %q", p95, pooler)
	}
}
