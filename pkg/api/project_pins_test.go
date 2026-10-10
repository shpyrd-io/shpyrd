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
	"github.com/shpyrd-io/shpyrd/internal/controller"
)

func pinnedProject(t *testing.T, volumeClass, dbClass string) (*Server, *shpyrdv1.App, *shpyrdv1.Postgres) {
	t.Helper()
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop", Annotations: map[string]string{controller.AnnotationProcessNodes: `{"web":"node-a"}`}},
		Spec:       shpyrdv1.AppSpec{ID: "shop-id", Processes: map[string]shpyrdv1.Process{"web": {Volumes: []shpyrdv1.VolumeMount{{Name: "uploads", Path: "/uploads"}}}}},
	}
	vol := &shpyrdv1.Volume{ObjectMeta: metav1.ObjectMeta{Name: "uploads", Namespace: app.Namespace}, Spec: shpyrdv1.VolumeSpec{Size: resource.MustParse("1Gi")}, Status: shpyrdv1.VolumeStatus{StorageClass: volumeClass}}
	pg := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: app.Namespace, Annotations: map[string]string{shpyrdv1.AnnotationPlacement: "node-a"}}}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "db-1", Namespace: app.Namespace, Labels: map[string]string{"cnpg.io/cluster": "db"}}, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: ptr.To(dbClass)}}
	s, _ := newTestServer(t, nil, []client.Object{app, vol, pg, claim})
	return s, app, pg
}

// Clearing the pins a move left hands the project back to the scheduler.
func TestClearProjectPins(t *testing.T) {
	s, app, pg := pinnedProject(t, "oci-bv", "oci-bv")
	ctx := context.Background()
	rec := do(t, s, "DELETE", "/api/cluster/project-archives/"+archiveProjectID(app)+"/placement/pins", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("unpin: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Processes []string `json:"processes"`
		Databases []string `json:"databases"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Processes) != 1 || out.Processes[0] != "web" || len(out.Databases) != 1 || out.Databases[0] != "db" {
		t.Errorf("cleared = %+v", out)
	}
	if err := s.apps.Get(ctx, client.ObjectKeyFromObject(app), app); err != nil {
		t.Fatal(err)
	}
	if app.Annotations[controller.AnnotationProcessNodes] != "" {
		t.Errorf("process pins still there: %q", app.Annotations[controller.AnnotationProcessNodes])
	}
	if err := s.apps.Get(ctx, client.ObjectKeyFromObject(pg), pg); err != nil {
		t.Fatal(err)
	}
	if pg.Annotations[shpyrdv1.AnnotationPlacement] != "" {
		t.Errorf("database pin still there: %q", pg.Annotations[shpyrdv1.AnnotationPlacement])
	}
	// Nothing left to clear is not an error.
	if rec := do(t, s, "DELETE", "/api/cluster/project-archives/"+archiveProjectID(app)+"/placement/pins", "", true); rec.Code != http.StatusOK {
		t.Errorf("second unpin: %d %s", rec.Code, rec.Body.String())
	}
}

// A pin on data that lives on a node's disk stays: the pin is what keeps the
// process or database with its data.
func TestClearProjectPinsRefusesNodeLocalData(t *testing.T) {
	for _, tc := range []struct{ volume, db, want string }{
		{controller.LocalStorageClass, "oci-bv", "migrate the disk"},
		{"oci-bv", controller.LocalStorageClass, "migrate the database"},
	} {
		s, app, _ := pinnedProject(t, tc.volume, tc.db)
		rec := do(t, s, "DELETE", "/api/cluster/project-archives/"+archiveProjectID(app)+"/placement/pins", "", true)
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("volume %s / db %s: %d %s", tc.volume, tc.db, rec.Code, rec.Body.String())
		}
	}
}
