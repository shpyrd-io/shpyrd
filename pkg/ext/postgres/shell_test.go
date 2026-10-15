package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// psql for a shared database runs in a helper pod of the project with the
// tenant's credentials from the database's own secret, never in the
// platform's server; a database not yet Ready is said so.
func TestSharedPsqlRunsInAProjectHelper(t *testing.T) {
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	pg := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "p-shop"}, Spec: shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeShared}, Status: shpyrdv1.ResourceStatus{Phase: shpyrdv1.ResourceReady}}
	helper := psqlHelperPod(pg)
	env := map[string]string{}
	for _, e := range helper.Spec.Containers[0].Env {
		env[e.Name] = e.ValueFrom.SecretKeyRef.Name + "/" + e.ValueFrom.SecretKeyRef.Key
	}
	if env["PGHOST"] != "db-app/host" || env["PGDATABASE"] != "db-app/dbname" || env["PGUSER"] != "db-app/username" || env["PGSSLMODE"] != "db-app/sslmode" || helper.Spec.ActiveDeadlineSeconds == nil || *helper.Spec.ActiveDeadlineSeconds != 3600 {
		t.Errorf("helper = %v deadline %v", env, helper.Spec.ActiveDeadlineSeconds)
	}
	running := helper.DeepCopy()
	running.Status.Phase = corev1.PodRunning
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pg, running).Build()
	target, err := extension{}.ResourceShell(context.Background(), ext.Deps{Client: c}, "Postgres", "p-shop", "db", []string{"-c", "select 1"})
	if err != nil || target.Pod != "psql-db" || target.Container != "psql" || strings.Join(target.Command, " ") != "psql -c select 1" {
		t.Errorf("target = %+v %v", target, err)
	}
	// Not Ready: said, no helper created.
	pending := pg.DeepCopy()
	pending.Status = shpyrdv1.ResourceStatus{Phase: shpyrdv1.ResourceProvisioning, Message: "securing your database"}
	c2 := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pending).Build()
	if _, err := sharedPsqlHelper(context.Background(), c2, pending, time.Second); err == nil || !strings.Contains(err.Error(), "provisioning") {
		t.Errorf("a database not ready: %v", err)
	}
	var pods corev1.PodList
	if err := c2.List(context.Background(), &pods, client.InNamespace("p-shop")); err != nil || len(pods.Items) != 0 {
		t.Errorf("no helper for a database not ready: %d %v", len(pods.Items), err)
	}
}
