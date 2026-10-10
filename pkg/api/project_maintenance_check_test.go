package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// The check before a pause dials the front door of the project's own
// exposure, and gives up with a clear error when that door never shows the
// maintenance page (an internal project asked through the external
// controller answers 404 forever).
func TestConfirmProjectMaintenanceDialsTheProjectsFrontDoorAndGivesUp(t *testing.T) {
	var internalHits, externalHits atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		internalHits.Add(1)
		w.Header().Set("X-Shpyrd-Maintenance", "true")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer internal.Close()
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalHits.Add(1)
		http.NotFound(w, r)
	}))
	defer external.Close()
	s, _ := newTestServer(t, nil, nil)
	s.opts.IngressServiceInternal = strings.TrimPrefix(internal.URL, "http://")
	s.opts.IngressServiceExternal = strings.TrimPrefix(external.URL, "http://")
	old := maintenanceConfirmTimeout
	maintenanceConfirmTimeout = 3 * time.Second
	defer func() { maintenanceConfirmTimeout = old }()

	app := &shpyrdv1.App{Spec: shpyrdv1.AppSpec{Exposure: "internal"}, Status: shpyrdv1.AppStatus{URL: "http://platform-agent.example"}}
	if err := s.confirmProjectMaintenance(context.Background(), app); err != nil {
		t.Fatalf("an internal project is confirmed through the internal front door: %v", err)
	}
	if internalHits.Load() < 3 || externalHits.Load() != 0 {
		t.Errorf("hits: internal %d external %d", internalHits.Load(), externalHits.Load())
	}

	// The same project asked as external: the external door does not know
	// the host, the check gives up and says what it saw.
	app.Spec.Exposure = "external"
	err := s.confirmProjectMaintenance(context.Background(), app)
	if err == nil || !strings.Contains(err.Error(), "did not show the maintenance page") || !strings.Contains(err.Error(), "404") {
		t.Fatalf("a door that never shows the page must fail with what it answered: %v", err)
	}
	// A caller that gives up first gets its own error, not the bound's.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.confirmProjectMaintenance(ctx, app); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("a cancelled caller: %v", err)
	}
}

// A rollback drains without insisting on the maintenance page: the door it
// cannot reach is noted, the pause goes on, the project will be resumed.
func TestRollbackDrainToleratesAnUnconfirmedMaintenancePage(t *testing.T) {
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }))
	defer external.Close()
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "p-shop"}, Spec: shpyrdv1.AppSpec{Exposure: "external"}, Status: shpyrdv1.AppStatus{URL: "http://platform-shop.example"}}
	record := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shpyrd-project-operation", Namespace: "p-shop"}}
	s, _ := newTestServer(t, nil, []client.Object{app, record})
	s.opts.IngressServiceExternal = strings.TrimPrefix(external.URL, "http://")
	old := maintenanceConfirmTimeout
	maintenanceConfirmTimeout = time.Second
	defer func() { maintenanceConfirmTimeout = old }()
	op := &projectArchiveOperation{ID: "op1", Kind: "move", Move: &projectMove{}}
	if err := s.drainProjectArchive(context.Background(), app, op, false); err != nil {
		t.Fatalf("a rollback's drain must go on without the page: %v", err)
	}
	if len(op.Warnings) != 1 || !strings.Contains(op.Warnings[0], "not confirmed before the rollback") {
		t.Errorf("the unconfirmed page is noted: %v", op.Warnings)
	}
	if err := s.drainProjectArchive(context.Background(), app, op, true); err == nil {
		t.Error("a move's drain insists on the page")
	}
}
