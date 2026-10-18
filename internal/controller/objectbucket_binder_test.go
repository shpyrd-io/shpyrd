package controller

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// Storage plan step 5: an attached bucket becomes the variables an S3
// client reads, under the binding's prefix; a bucket still provisioning
// is not ready rather than missing.
func TestABucketBindsAsTheVariablesAnS3ClientReads(t *testing.T) {
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	ready := &shpyrdv1.ObjectBucket{ObjectMeta: metav1.ObjectMeta{Name: "uploads", Namespace: "app-shop"}, Status: shpyrdv1.ObjectBucketStatus{Phase: shpyrdv1.BucketReady, SecretName: "uploads-object-storage"}}
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "uploads-object-storage", Namespace: "app-shop"}, Data: map[string][]byte{
		"AWS_ACCESS_KEY_ID": []byte("shpyrd-abc"), "AWS_SECRET_ACCESS_KEY": []byte("s3cr3t"), "AWS_ENDPOINT_URL": []byte("http://object-storage.shpyrd-system.svc:3901"),
		"AWS_REGION": []byte("us-east-1"), "BUCKET": []byte("shpyrd-abc"), "AWS_PUBLIC_ENDPOINT_URL": []byte("https://buckets.example.test"),
	}}
	pending := &shpyrdv1.ObjectBucket{ObjectMeta: metav1.ObjectMeta{Name: "slow", Namespace: "app-shop"}, Status: shpyrdv1.ObjectBucketStatus{Phase: shpyrdv1.BucketPending}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ready, sec, pending).Build()
	ctx := context.Background()

	vars, err := (ObjectBucketBinder{}).ConfigVars(ctx, c, "app-shop", "uploads", "AWS")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"AWS_ACCESS_KEY_ID": "shpyrd-abc", "AWS_SECRET_ACCESS_KEY": "s3cr3t", "AWS_ENDPOINT_URL": "http://object-storage.shpyrd-system.svc:3901",
		"AWS_REGION": "us-east-1", "AWS_BUCKET": "shpyrd-abc", "AWS_URL": "s3://shpyrd-abc", "AWS_PUBLIC_ENDPOINT_URL": "https://buckets.example.test",
	}
	if len(vars) != len(want) {
		t.Errorf("vars = %v", vars)
	}
	for k, v := range want {
		if vars[k] != v {
			t.Errorf("%s = %q, want %q", k, vars[k], v)
		}
	}
	if (ObjectBucketBinder{}).DefaultPrefix() != "BUCKET" {
		t.Error("the default prefix is BUCKET")
	}
	var notReady *NotReadyError
	if _, err := (ObjectBucketBinder{}).ConfigVars(ctx, c, "app-shop", "slow", "BUCKET"); !errors.As(err, &notReady) {
		t.Errorf("a pending bucket = %v, want not ready", err)
	}
}
