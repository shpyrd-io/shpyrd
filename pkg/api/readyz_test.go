package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// #147: /api/readyz says, one line per check, what a deploy, a backup and
// a sleeping app need; a component the cluster does not run is skipped,
// a stopped one makes the report degraded (503).
func TestReadyzReportsEachStorageCheck(t *testing.T) {
	deploy := func(ns, name string, ready int32) *appsv1.Deployment {
		return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)}, Status: appsv1.DeploymentStatus{ReadyReplicas: ready}}
	}
	stale := metav1.NewTime(time.Now().Add(-30 * time.Hour))
	backup := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "platform-backup", Namespace: "shpyrd-system", CreationTimestamp: metav1.NewTime(time.Now().Add(-72 * time.Hour))}, Status: batchv1.CronJobStatus{LastSuccessfulTime: &stale}}
	s, _ := newTestServer(t, nil, []client.Object{}, deploy("shpyrd-system", "registry", 1), deploy("shpyrd-system", "pg-gateway", 0), backup)
	rec := do(t, s, "GET", "/api/readyz", "", false)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz = %d %s", rec.Code, rec.Body)
	}
	var report ReadyReport
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "degraded" {
		t.Errorf("status = %s", report.Status)
	}
	if ch := report.Checks["pg-gateway"]; ch.OK || ch.Skipped || ch.Message != "pg-gateway has no ready instance (1 wanted)" {
		t.Errorf("pg-gateway = %+v", ch)
	}
	if ch := report.Checks["backup"]; ch.OK || ch.Message != "the last platform backup succeeded 30h0m0s ago" {
		t.Errorf("backup = %+v", ch)
	}
	if ch := report.Checks["registry"]; !ch.OK || ch.Message != "1 of 1 ready" {
		t.Errorf("registry = %+v", ch)
	}
	for _, name := range []string{"object-gateway", "keda-http", "shared-postgres", "disks"} {
		if ch := report.Checks[name]; !ch.OK || !ch.Skipped {
			t.Errorf("%s should be skipped on a cluster without it, got %+v", name, ch)
		}
	}

	// Everything the cluster runs is up: ok.
	fresh := metav1.Now()
	backup.Status.LastSuccessfulTime = &fresh
	s2, _ := newTestServer(t, nil, []client.Object{}, deploy("shpyrd-system", "registry", 1), deploy("shpyrd-system", "pg-gateway", 2), deploy("shpyrd-system", "object-storage", 1), deploy("keda", "keda-add-ons-http-interceptor", 2), backup)
	rec = do(t, s2, "GET", "/api/readyz", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthy readyz = %d %s", rec.Code, rec.Body)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &report)
	if report.Status != "ok" || !report.Checks["object-gateway"].OK || report.Checks["object-gateway"].Skipped || report.Checks["pg-gateway"].Message != "2 of 1 ready" {
		t.Errorf("healthy report = %+v", report)
	}
}
