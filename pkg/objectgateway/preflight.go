package objectgateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/minio/minio-go/v7"
)

// ErrIfMatchIgnored is CheckConditionalWrites' outcome on a provider that
// honours If-None-Match but not If-Match (OCI Object Storage's S3
// compatibility): creating a descriptor is atomic there, updating one is
// not. Preflight decides whether that is acceptable.
var ErrIfMatchIgnored = errors.New("gateway backend must reject a stale If-Match")

// CheckConditionalWrites tells what an S3-compatible provider honours of the
// conditions that protect descriptors from concurrent provisioning: nil when
// If-None-Match and If-Match both work, ErrIfMatchIgnored when only creation
// is protected, another error when If-None-Match is ignored too or the probe
// itself failed. Each invocation uses its own disposable object, outside
// consumer metadata.
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
		if err == nil {
			return ErrIfMatchIgnored
		}
		return fmt.Errorf("gateway backend must reject a stale If-Match: %v", err)
	}
	opts = minio.PutObjectOptions{}
	opts.SetMatchETag(info.ETag)
	if _, err := put(opts); err != nil {
		return fmt.Errorf("gateway backend must accept a matching If-Match: %w", err)
	}
	return nil
}

// Preflight is the check a process runs before writing descriptors:
// CheckConditionalWrites, then the single-writer rule. A provider that
// ignores If-Match is accepted when this process is the only writer of
// descriptors (SingleWriter), since the in-process lock then prevents the
// lost update If-Match would have; the returned note says so, for the caller
// to log. Without SingleWriter such a provider is refused and the error
// names the setting. A provider that ignores If-None-Match is always refused.
func (r *Records) Preflight(ctx context.Context) (note string, err error) {
	err = r.CheckConditionalWrites(ctx)
	switch {
	case err == nil:
		return "", nil
	case !errors.Is(err, ErrIfMatchIgnored):
		return "", err
	case !r.SingleWriter:
		return "", fmt.Errorf("%w; the provider accepts a stale update, which is safe only when one gateway process writes descriptors: set SHPYRD_GATEWAY_SINGLE_WRITER to true and SHPYRD_GATEWAY_REPLICAS to 1", err)
	}
	return "the object storage provider ignores If-Match; this gateway runs as the single writer of its descriptors (SHPYRD_GATEWAY_SINGLE_WRITER=true) and serializes their updates in-process", nil
}
