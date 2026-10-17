package api

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
	"github.com/shpyrd-io/shpyrd/pkg/projectarchive"
)

// A project's buckets in an archive (storage plan step 5). By reference
// the manifest names them and a restore finds their objects in the store;
// with files (export?files=true) the objects come along too, one tar per
// bucket, up to archiveFilesCap together: for an export before a destroy
// (which deletes the bucket) and a move between clouds. The copy runs
// before the project is paused: objects need no pause.

// measureArchiveBuckets says what the project's buckets hold together
// and marks each as carried, refusing in words past the cap, before the
// project is touched.
func (s *Server) measureArchiveBuckets(ctx context.Context, app *shpyrdv1.App, m *projectArchiveMetadata) error {
	var total int64
	for i := range m.Buckets {
		client, err := s.archiveBucketClient(ctx, app.Namespace, m.Buckets[i].Name, 0)
		if err != nil {
			return err
		}
		for obj := range client.Client.ListObjects(ctx, client.Bucket, minio.ListObjectsOptions{Recursive: true}) {
			if obj.Err != nil {
				return fmt.Errorf("bucket %s: %w", m.Buckets[i].Name, obj.Err)
			}
			total += obj.Size
		}
		m.Buckets[i].Files = true
	}
	if total > archiveFilesCap {
		return fmt.Errorf("The buckets hold %s together; an archive carries up to %s of files. Export without files and keep the buckets, or remove files first.", humanQuantity(fmt.Sprint(total)), humanQuantity(fmt.Sprint(int64(archiveFilesCap))))
	}
	return nil
}

// exportArchiveBuckets streams each carried bucket's objects into the
// archive as buckets/<name>.tar: the key as the entry's name, the object
// as its body.
func (s *Server) exportArchiveBuckets(ctx context.Context, app *shpyrdv1.App, b *projectarchive.Bundle, m *projectArchiveMetadata) error {
	for _, bucket := range m.Buckets {
		if !bucket.Files {
			continue
		}
		client, err := s.archiveBucketClient(ctx, app.Namespace, bucket.Name, 0)
		if err != nil {
			return err
		}
		if err := addArchiveStream(ctx, b, "buckets/"+bucket.Name+".tar", func(out io.Writer) error {
			return exportBucketObjects(ctx, client, out)
		}); err != nil {
			return err
		}
	}
	return nil
}

func exportBucketObjects(ctx context.Context, client *objectstore.S3, out io.Writer) error {
	tw := tar.NewWriter(out)
	for obj := range client.Client.ListObjects(ctx, client.Bucket, minio.ListObjectsOptions{Recursive: true}) {
		if obj.Err != nil {
			return obj.Err
		}
		if obj.Size < 0 {
			continue
		}
		body, err := client.Client.GetObject(ctx, client.Bucket, obj.Key, minio.GetObjectOptions{})
		if err != nil {
			return err
		}
		hdr := &tar.Header{Name: obj.Key, Mode: 0o644, Size: obj.Size, ModTime: obj.LastModified, Typeflag: tar.TypeReg}
		if obj.ContentType != "" {
			hdr.PAXRecords = map[string]string{"SHPYRD.content-type": obj.ContentType}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			body.Close()
			return err
		}
		_, err = io.CopyN(tw, body, obj.Size)
		body.Close()
		if err != nil {
			return fmt.Errorf("object %s: %w", obj.Key, err)
		}
	}
	return tw.Close()
}

// restoreBucketObjects writes a tar of objects into the bucket, each
// entry under its name.
func restoreBucketObjects(ctx context.Context, client *objectstore.S3, in io.Reader) error {
	tr := tar.NewReader(in)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg || !safeArchiveKey(hdr.Name) {
			continue
		}
		opts := minio.PutObjectOptions{}
		if ct := hdr.PAXRecords["SHPYRD.content-type"]; ct != "" {
			opts.ContentType = ct
		}
		if _, err := client.Client.PutObject(ctx, client.Bucket, hdr.Name, tr, hdr.Size, opts); err != nil {
			return fmt.Errorf("object %s: %w", hdr.Name, err)
		}
	}
}

// safeArchiveKey refuses an object name that is empty or walks.
func safeArchiveKey(key string) bool {
	if key == "" || key[0] == '/' {
		return false
	}
	for _, part := range splitKey(key) {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

func splitKey(key string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(key); i++ {
		if i == len(key) || key[i] == '/' {
			parts = append(parts, key[start:i])
			start = i + 1
		}
	}
	return parts
}

// archiveBucketClient opens a project's bucket with its own credential,
// waiting up to wait for the bucket to be ready (a restore made it a
// moment ago); the credential never leaves the server.
func (s *Server) archiveBucketClient(ctx context.Context, namespace, name string, wait time.Duration) (*objectstore.S3, error) {
	deadline := time.Now().Add(wait)
	for {
		b := &shpyrdv1.ObjectBucket{}
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, b); err != nil {
			return nil, err
		}
		if b.Status.Phase == shpyrdv1.BucketReady && b.Status.SecretName != "" {
			sec := &corev1.Secret{}
			if err := s.apps.Get(ctx, types.NamespacedName{Namespace: namespace, Name: b.Status.SecretName}, sec); err == nil {
				get := func(k string) string { return string(sec.Data[k]) }
				return objectstore.NewS3(firstNonEmpty(get("AWS_ENDPOINT_URL"), b.Status.Endpoint), firstNonEmpty(get("AWS_REGION"), objectstore.Region), b.Status.Bucket, "", get("AWS_ACCESS_KEY_ID"), get("AWS_SECRET_ACCESS_KEY"))
			}
		}
		if b.Status.Phase == shpyrdv1.BucketFailed {
			return nil, fmt.Errorf("bucket %s: %s", name, firstNonEmpty(b.Status.Message, "failed"))
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("bucket %s is not ready (%s)", name, firstNonEmpty(b.Status.Message, b.Status.Phase, "provisioning"))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}
