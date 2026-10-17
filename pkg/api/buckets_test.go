package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/ext/all"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Buckets for apps (storage plan step 5): what a bucket is admitted
// with, what it is refused in words, what the project sees of it.

func createResource(t *testing.T, s *Server, body string) (int, string) {
	t.Helper()
	rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/resources", body)
	return rec.Code, rec.Body.String()
}

// The free workspace: one bucket of 1 GB. A bucket without a cap gets the
// plan's; a second one, or one asking for more, is refused in words; what
// the gateway does not do is refused before anything is made.
func TestTheFreeWorkspaceGetsOneBucketOfOneGigabyte(t *testing.T) {
	s, _, st := newTenantServer(t)
	s.opts.Extensions = all.All()
	setLimits(t, st, "acme", &store.Limits{ObjectStorage: &store.ObjectStorageLimits{Buckets: 1, BucketCap: "1Gi", Total: "1Gi"}})
	if code, body := createResource(t, s, `{"kind":"ObjectBucket","name":"big","spec":{"maxBytes":"5Gi"}}`); code != http.StatusBadRequest || !strings.Contains(body, "A bucket holds up to 1 GB.") {
		t.Errorf("a bucket above the plan's cap = %d %s", code, body)
	}
	if code, body := createResource(t, s, `{"kind":"ObjectBucket","name":"old","spec":{"versioning":true}}`); code != http.StatusBadRequest || !strings.Contains(body, errBucketVersioning) {
		t.Errorf("versioning = %d %s", code, body)
	}
	if code, body := createResource(t, s, `{"kind":"ObjectBucket","name":"other","spec":{"alias":"shpyrd-0123"}}`); code != http.StatusBadRequest || !strings.Contains(body, errBucketAlias) {
		t.Errorf("an alias by hand = %d %s", code, body)
	}
	code, body := createResource(t, s, `{"kind":"ObjectBucket","name":"uploads"}`)
	if code != http.StatusCreated || !strings.Contains(body, `"maxBytes":"1Gi"`) || !strings.Contains(body, "Your bucket can hold up to 1 GB.") {
		t.Fatalf("the bucket the plan includes = %d %s", code, body)
	}
	if code, body := createResource(t, s, `{"kind":"ObjectBucket","name":"second"}`); code != http.StatusBadRequest || !strings.Contains(body, "This would bring the workspace to 2 of 1 buckets.") {
		t.Errorf("a second bucket = %d %s", code, body)
	}
	u := workspaceUsageOf(t, s, "acme.shpyrd.test")
	if u.Buckets != 1 || u.ObjectStorage != "1Gi" {
		t.Errorf("usage = %+v, want one bucket of 1Gi", u)
	}
}

// The buckets' caps count together against the workspace's object
// storage, and a workspace without a plan gets buckets without caps.
func TestBucketCapsCountTogether(t *testing.T) {
	s, _, st := newTenantServer(t)
	s.opts.Extensions = all.All()
	setLimits(t, st, "acme", &store.Limits{ObjectStorage: &store.ObjectStorageLimits{Buckets: 5, BucketCap: "2Gi", Total: "3Gi"}})
	if code, body := createResource(t, s, `{"kind":"ObjectBucket","name":"a","spec":{"maxBytes":"2Gi"}}`); code != http.StatusCreated {
		t.Fatalf("the first bucket = %d %s", code, body)
	}
	if code, body := createResource(t, s, `{"kind":"ObjectBucket","name":"b"}`); code != http.StatusBadRequest || !strings.Contains(body, "This would bring the workspace to 4 GB of 3 GB of object storage.") {
		t.Errorf("a bucket past the total = %d %s", code, body)
	}
	if code, body := createResource(t, s, `{"kind":"ObjectBucket","name":"b","spec":{"maxBytes":"1Gi"}}`); code != http.StatusCreated {
		t.Errorf("a bucket that fits = %d %s", code, body)
	}
	setLimits(t, st, "acme", nil)
	code, body := createResource(t, s, `{"kind":"ObjectBucket","name":"free"}`)
	if code != http.StatusCreated || strings.Contains(body, "maxBytes") || strings.Contains(body, "can hold up to") {
		t.Errorf("a bucket without a plan = %d %s", code, body)
	}
}

// The platform's bucket (a database's backups) is not the project's: not
// listed, not counted, not attached, not deleted, not rotated. The
// project's own is listed with its cap and usage, and rotated once ready.
func TestThePlatformsBucketIsNotTheProjects(t *testing.T) {
	s, cr, st := newTenantServer(t)
	s.opts.Extensions = all.All()
	ctx := context.Background()
	setLimits(t, st, "acme", &store.Limits{ObjectStorage: &store.ObjectStorageLimits{Buckets: 1}})
	backups := &shpyrdv1.ObjectBucket{ObjectMeta: metav1.ObjectMeta{Name: "db-backups", Namespace: "app-acme-shop", Labels: map[string]string{shpyrdv1.LabelBucketPostgres: "db", shpyrdv1.LabelProject: "shop"}}}
	if err := cr.Create(ctx, backups); err != nil {
		t.Fatal(err)
	}
	if code, body := createResource(t, s, `{"kind":"ObjectBucket","name":"uploads","spec":{"maxBytes":"1Gi","public":true}}`); code != http.StatusCreated {
		t.Fatalf("the project's bucket next to the platform's = %d %s", code, body)
	}
	rec := at(t, s, "acme.shpyrd.test", "GET", "/api/projects/shop/resources", "")
	var views []ResourceView
	if err := json.Unmarshal(rec.Body.Bytes(), &views); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, v := range views {
		if v.Kind == "ObjectBucket" {
			names = append(names, v.Name)
			if !v.Bindable || v.Details["capBytes"] != "1073741824" || v.Details["public"] != "true" || v.Details["usedBytes"] != "0" {
				t.Errorf("the project's bucket in the list = %+v", v)
			}
		}
	}
	if strings.Join(names, ",") != "uploads" {
		t.Errorf("buckets listed = %v, want the project's only", names)
	}
	for _, c := range []struct{ method, path, body string }{
		{"DELETE", "/api/projects/shop/resources/objectbucket/db-backups", ""},
		{"POST", "/api/projects/shop/bindings", `{"kind":"ObjectBucket","name":"db-backups"}`},
		{"POST", "/api/projects/shop/resources/objectbucket/db-backups/rotate", ""},
	} {
		if rec := at(t, s, "acme.shpyrd.test", c.method, c.path, c.body); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s on the platform's bucket = %d %s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
	// Rotation waits for the bucket to be ready, then stamps it.
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/resources/objectbucket/uploads/rotate", ""); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "The bucket is not ready yet; rotate its key once it is.") {
		t.Errorf("rotation before ready = %d %s", rec.Code, rec.Body.String())
	}
	b := &shpyrdv1.ObjectBucket{}
	if err := cr.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: "uploads"}, b); err != nil {
		t.Fatal(err)
	}
	b.Status.Phase = shpyrdv1.BucketReady
	if err := cr.Update(ctx, b); err != nil {
		t.Fatal(err)
	}
	rec = at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/resources/objectbucket/uploads/rotate", "")
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "A new key for uploads is being written") {
		t.Errorf("rotation = %d %s", rec.Code, rec.Body.String())
	}
	if err := cr.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: "uploads"}, b); err != nil || b.Annotations[controller.AnnotationRotate] == "" {
		t.Errorf("the rotation stamp: %v %v", err, b.Annotations)
	}
	if rec := at(t, s, "acme.shpyrd.test", "GET", "/api/projects/shop/resources", ""); !strings.Contains(rec.Body.String(), `"rotating":"`) {
		t.Errorf("the list says the key is being rotated: %s", rec.Body.String())
	}
	// Attaching the project's bucket makes it the app's.
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/bindings", `{"kind":"ObjectBucket","name":"uploads","prefix":"AWS"}`); rec.Code != http.StatusOK {
		t.Errorf("attach = %d %s", rec.Code, rec.Body.String())
	}
	if u := workspaceUsageOf(t, s, "acme.shpyrd.test"); u.Buckets != 1 {
		t.Errorf("usage buckets = %d, want the project's one", u.Buckets)
	}
}
