package controller

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// ObjectBucketBinder exposes a bucket as config vars (storage plan step
// 5): the credential the bucket controller wrote, under a prefix. With
// the default prefix an app reads BUCKET_ACCESS_KEY_ID, BUCKET_BUCKET and
// so on; with `--prefix AWS` it reads AWS_ACCESS_KEY_ID, AWS_ENDPOINT_URL,
// AWS_REGION, the names the SDKs pick up with no code.
type ObjectBucketBinder struct{}

// DefaultPrefix gives BUCKET_URL and friends.
func (ObjectBucketBinder) DefaultPrefix() string { return "BUCKET" }

// ConfigVars reads the bucket's credential Secret.
func (ObjectBucketBinder) ConfigVars(ctx context.Context, c client.Client, namespace, name, prefix string) (map[string]string, error) {
	b := &shpyrdv1.ObjectBucket{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, b); err != nil {
		return nil, err
	}
	if b.Status.Phase != shpyrdv1.BucketReady {
		return nil, &NotReadyError{Msg: fmt.Sprintf("bucket %s is %s", name, strings.ToLower(firstNonEmpty(b.Status.Phase, "provisioning")))}
	}
	sec := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: b.CredentialSecretName()}, sec); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, &NotReadyError{Msg: fmt.Sprintf("bucket %s has no credentials yet", name)}
		}
		return nil, err
	}
	get := func(k string) string {
		if v, ok := sec.Data[k]; ok && len(v) > 0 {
			return string(v)
		}
		return sec.StringData[k]
	}
	bucket := get("BUCKET")
	vars := map[string]string{
		prefix + "_ACCESS_KEY_ID":     get("AWS_ACCESS_KEY_ID"),
		prefix + "_SECRET_ACCESS_KEY": get("AWS_SECRET_ACCESS_KEY"),
		prefix + "_ENDPOINT_URL":      get("AWS_ENDPOINT_URL"),
		prefix + "_REGION":            get("AWS_REGION"),
		prefix + "_BUCKET":            bucket,
		prefix + "_URL":               "s3://" + bucket,
	}
	if public := get("AWS_PUBLIC_ENDPOINT_URL"); public != "" {
		vars[prefix+"_PUBLIC_ENDPOINT_URL"] = public
	}
	return vars, nil
}
