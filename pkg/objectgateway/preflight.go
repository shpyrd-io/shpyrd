package objectgateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/minio/minio-go/v7"
)

// CheckConditionalWrites fails closed when an S3-compatible provider ignores
// the conditions that protect credentials from concurrent provisioning.
// Each invocation uses its own disposable object, outside consumer metadata.
func (r *Records) CheckConditionalWrites(ctx context.Context) error {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	key := "__shpyrd_gateway/checks/" + hex.EncodeToString(raw)
	put := func(opts minio.PutObjectOptions) (minio.UploadInfo, error) {
		opts.DisableMultipart = true
		return r.Store.Client.PutObject(ctx, r.Store.Bucket, key, bytes.NewReader([]byte("probe")), 5, opts)
	}
	opts := minio.PutObjectOptions{}
	opts.SetMatchETagExcept("*")
	info, err := put(opts)
	if err != nil {
		return fmt.Errorf("gateway conditional-write preflight: %w", err)
	}
	defer r.Store.Client.RemoveObject(ctx, r.Store.Bucket, key, minio.RemoveObjectOptions{})
	if _, err := put(opts); minio.ToErrorResponse(err).Code != "PreconditionFailed" {
		return fmt.Errorf("gateway backend must reject an existing key with If-None-Match: %v", err)
	}
	opts = minio.PutObjectOptions{}
	opts.SetMatchETag("deliberately-wrong-etag")
	if _, err := put(opts); minio.ToErrorResponse(err).Code != "PreconditionFailed" {
		return fmt.Errorf("gateway backend must reject a stale If-Match: %v", err)
	}
	opts = minio.PutObjectOptions{}
	opts.SetMatchETag(info.ETag)
	if _, err := put(opts); err != nil {
		return fmt.Errorf("gateway backend must accept a matching If-Match: %w", err)
	}
	return nil
}
