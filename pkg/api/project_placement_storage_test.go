package api

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/install"
)

// A move copies node-local data. A group on provider block storage needs a
// migration first only on a node-local profile; on a profile whose disks are
// block volumes the disk follows the process and nothing needs copying.
func TestPlacementMigrationFlagFollowsTheProfile(t *testing.T) {
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "project", Namespace: "app-project"}}
	vol := &shpyrdv1.Volume{ObjectMeta: metav1.ObjectMeta{Name: "uploads", Namespace: app.Namespace}, Spec: shpyrdv1.VolumeSpec{Size: resource.MustParse("50Gi")}}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: shpyrdv1.PVCPrefix + "uploads", Namespace: app.Namespace}, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: ptr.To("oci-bv")}}
	s, _ := newTestServer(t, nil, []client.Object{app, vol, claim})
	flag := func(profileClass string) bool {
		t.Helper()
		s.opts.Vars = func(key string) string {
			if key == install.VarProjectStorageClass {
				return profileClass
			}
			return ""
		}
		groups, _, err := s.projectPlacement(context.Background(), app)
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range groups {
			if g.ID == "volume:uploads" {
				return g.NeedsMigration
			}
		}
		t.Fatalf("no group for the volume: %+v", groups)
		return false
	}
	if flag("") {
		t.Error("on a block-storage profile a block disk needs no migration to be moved")
	}
	if !flag(install.LocalStorageClass) {
		t.Error("on a node-local profile a block disk must be migrated before a move")
	}
}
