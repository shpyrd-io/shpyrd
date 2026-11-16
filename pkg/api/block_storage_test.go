package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ext/all"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// A workspace's storage ceiling counts database disks as the namespace
// quota does, and a database that would pass it is refused before it exists,
// in words, instead of waiting on a claim the quota never admits.
func TestDatabaseDiskCountsAgainstTheStorageCeiling(t *testing.T) {
	s, _, st := newTenantServer(t)
	s.opts.Extensions = all.All() // the Postgres and Redis kinds
	ctx := context.Background()
	if _, err := st.UpdateWorkspaceSettings(ctx, "acme", store.WorkspaceSettings{Limits: &store.Limits{Storage: "10Gi"}}); err != nil {
		t.Fatal(err)
	}
	// On a block profile the 5Gi default becomes the 50Gi provider minimum.
	s.opts.Vars = func(key string) string {
		return map[string]string{install.VarStorageClass: "oci-bv", install.VarVolumeMinSize: "50Gi"}[key]
	}
	rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/resources", `{"kind":"Postgres","name":"db"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "This would bring the workspace to 50 GB of 10 GB.") {
		t.Errorf("database over the storage ceiling = %d %s", rec.Code, rec.Body.String())
	}
	// A store without persistence has no disk.
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/resources", `{"kind":"Redis","name":"cache","spec":{"persistent":false}}`); rec.Code != http.StatusCreated {
		t.Errorf("a store without a disk = %d %s", rec.Code, rec.Body.String())
	}
	// Within the ceiling the answer says what disk the database gets.
	if _, err := st.UpdateWorkspaceSettings(ctx, "acme", store.WorkspaceSettings{Limits: &store.Limits{Storage: "100Gi"}}); err != nil {
		t.Fatal(err)
	}
	rec = at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/resources", `{"kind":"Postgres","name":"db"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), "Its disk is 50Gi, the minimum size.") {
		t.Errorf("database within the ceiling = %d %s", rec.Code, rec.Body.String())
	}
	// It now counts: with the ceiling lowered under two disks, a second one
	// passes it.
	if _, err := st.UpdateWorkspaceSettings(ctx, "acme", store.WorkspaceSettings{Limits: &store.Limits{Storage: "60Gi"}}); err != nil {
		t.Fatal(err)
	}
	rec = at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/resources", `{"kind":"Postgres","name":"db2"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "This would bring the workspace to 100 GB of 60 GB.") {
		t.Errorf("second database = %d %s", rec.Code, rec.Body.String())
	}
}

// What a person reads about a disk the minimum made larger names no
// provider (shpyrd#354): the note of a new database or persistent store,
// the storage detail of one that exists, and the refusal of a shared folder
// on a profile that names no class for them.
func TestRoundedDiskWordsNameNoProvider(t *testing.T) {
	s, _, _ := newTenantServer(t)
	s.opts.Extensions = all.All() // the Postgres and Redis kinds
	s.opts.Vars = func(key string) string {
		return map[string]string{install.VarProfile: "oci", install.VarStorageClass: "oci-bv", install.VarVolumeMinSize: "50Gi"}[key]
	}
	for _, body := range []string{`{"kind":"Postgres","name":"db"}`, `{"kind":"Redis","name":"store","spec":{"persistent":true}}`} {
		rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/resources", body)
		if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), "Its disk is 50Gi, the minimum size.") || strings.Contains(rec.Body.String(), "provider") || strings.Contains(rec.Body.String(), "Oracle") {
			t.Errorf("%s = %d %s", body, rec.Code, rec.Body.String())
		}
	}
	for _, kind := range []string{"Postgres", "Redis"} {
		v := resourceViewOf(ext.ResourceType{Kind: kind}, unstructured.Unstructured{Object: map[string]interface{}{
			"metadata": map[string]interface{}{"name": "db"},
			"spec":     map[string]interface{}{"storage": "5Gi"},
			"status":   map[string]interface{}{"storage": "50Gi"},
		}})
		if got := v.Details["storage"]; got != "50Gi (5Gi requested; minimum size)" {
			t.Errorf("%s storage detail = %q", kind, got)
		}
	}
	rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/volumes", `{"name":"media","size":"1Gi","shared":true}`)
	if rec.Code != http.StatusBadRequest || rec.Body.String() != `{"error":"`+sharedFoldersUnavailable+`"}` {
		t.Errorf("shared folder on a profile without a class = %d %s", rec.Code, rec.Body.String())
	}
}

// A restored shared folder keeps its size (file systems have no minimum);
// a restored disk is rounded like a new one.
func TestRestoredVolumeSizeRoundsDisksNotSharedFolders(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	s.opts.Vars = func(key string) string {
		return map[string]string{install.VarStorageClass: "oci-bv", install.VarVolumeMinSize: "50Gi"}[key]
	}
	disk := s.restoredVolumeSize(shpyrdv1.VolumeSpec{Size: resource.MustParse("1Gi"), AccessMode: corev1.ReadWriteOnce})
	shared := s.restoredVolumeSize(shpyrdv1.VolumeSpec{Size: resource.MustParse("20Gi"), AccessMode: corev1.ReadWriteMany})
	if disk.String() != "50Gi" || shared.String() != "20Gi" {
		t.Errorf("disk %s (want 50Gi), shared %s (want 20Gi)", disk.String(), shared.String())
	}
	if got := s.databaseDiskSize(nil); got.String() != "50Gi" {
		t.Errorf("database default disk = %s, want 50Gi", got.String())
	}
	s.opts.Vars = func(string) string { return "" }
	if got := s.databaseDiskSize(nil); got.String() != "5Gi" {
		t.Errorf("database default disk without a minimum = %s, want 5Gi", got.String())
	}
	_ = controller.LocalStorageClass
}
