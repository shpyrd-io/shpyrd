package objectstorage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
)

func TestGatewaySummaryNeverListsTheWholeStore(t *testing.T) {
	var measured atomic.Int32
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/usage" {
			http.NotFound(w, r)
			return
		}
		bucket := r.URL.Query().Get("bucket")
		if bucket == "" {
			t.Error("the summary listed the whole store")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		measured.Add(1)
		_ = json.NewEncoder(w).Encode(objectstore.Capacity{Buckets: map[string]objectstore.Usage{bucket: {Bytes: 1000, Objects: 10}}, MeasuredAt: time.Now()})
	}))
	defer admin.Close()
	previous := connectGateway
	connectGateway = func(endpoint, _, token string) (*objectstore.Gateway, error) {
		return objectstore.ConnectGateway(endpoint, admin.URL, token)
	}
	t.Cleanup(func() {
		connectGateway = previous
		platformUsage.Lock()
		platformUsage.buckets, platformUsage.at = nil, time.Time{}
		platformUsage.Unlock()
	})
	platformUsage.Lock()
	platformUsage.buckets, platformUsage.at = nil, time.Time{}
	platformUsage.Unlock()

	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	older := metav1.NewTime(time.Now().Add(-time.Hour).Truncate(time.Second))
	newer := metav1.NewTime(time.Now().Add(-time.Minute).Truncate(time.Second))
	bucket := func(ns, name string, used, objects int64, at metav1.Time) *shpyrdv1.ObjectBucket {
		return &shpyrdv1.ObjectBucket{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
			Status:     shpyrdv1.ObjectBucketStatus{Phase: shpyrdv1.BucketReady, Bucket: objectstore.LogicalBucketName(ns, name), UsedBytes: used, Objects: objects, MeasuredAt: &at},
		}
	}
	cr := crfake.NewClientBuilder().WithScheme(scheme).WithObjects(
		bucket("app-a", "backups", 100, 1, older),
		bucket("app-b", "backups", 200, 2, newer),
	).Build()
	cs := kubefake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "shpyrd-system", Name: install.ObjectStorageAdminSecretName},
		Data:       map[string][]byte{"adminToken": []byte("tok")},
	})
	deps := ext.Deps{
		Kube: &kube.Client{Kube: cs, Namespace: "shpyrd-system"}, Client: cr, SystemNamespace: "shpyrd-system",
		Vars: func(name string) string {
			if name == install.VarGatewayBucket {
				return "gateway"
			}
			return ""
		},
	}
	sum, err := summarize(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Message != "" {
		t.Fatalf("message: %s", sum.Message)
	}
	if sum.Backend != "gateway" {
		t.Fatalf("backend %q", sum.Backend)
	}
	want := int64(100 + 200 + 1000*len(install.PlatformGatewayBuckets))
	if sum.UsedBytes != want {
		t.Fatalf("used %d, want %d (consumers' measurements plus the platform's buckets)", sum.UsedBytes, want)
	}
	if n := int(measured.Load()); n != len(install.PlatformGatewayBuckets) {
		t.Fatalf("%d bucket measurements, want one per platform bucket (%d)", n, len(install.PlatformGatewayBuckets))
	}
	if len(sum.Buckets) != 2 || sum.Buckets[0].UsedBytes != 100 || sum.Buckets[1].Objects != 2 {
		t.Fatalf("rows %+v", sum.Buckets)
	}
	if sum.MeasuredAt == nil || !sum.MeasuredAt.Equal(older.Time) {
		t.Fatalf("measured at %v, want the oldest measurement %v", sum.MeasuredAt, older.Time)
	}

	// The next page load within the TTL costs the gateway nothing.
	again, err := summarize(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if again.UsedBytes != want || int(measured.Load()) != len(install.PlatformGatewayBuckets) {
		t.Fatalf("second summary re-measured: used %d, %d measurements", again.UsedBytes, measured.Load())
	}
}
