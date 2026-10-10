package objectgateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/minio/minio-go/v7"
	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
	"github.com/versity/versitygw/auth"
	"github.com/versity/versitygw/s3err"
)

var bucketName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

func validName(s string) bool { return bucketName.MatchString(s) }

const metadataPrefix = "__shpyrd_gateway/buckets/"

func conditionalConflict(err error) bool {
	code := minio.ToErrorResponse(err).Code
	return code == "PreconditionFailed" || code == "ConditionalRequestConflict"
}

type Record struct {
	Name          string
	Secret        string
	Created       time.Time
	RetentionDays int32
	Disabled      bool
	etag          string
}

// Records performs direct, conditional S3 metadata reads and writes. No
// per-consumer IAM/Kubernetes resources are needed; authentication reads
// through a small, short-lived memory of descriptors (see descriptorCache).
// Revocation applies to newly authenticated requests on this replica
// immediately and on the other within descriptorTTL; already authorized
// transfers can finish.
type Records struct {
	Store  *objectstore.S3
	client *s3.Client

	cacheOnce sync.Once
	cache     *descriptorCache
}

func (r *Records) descriptors() *descriptorCache {
	r.cacheOnce.Do(func() {
		if r.cache == nil {
			r.cache = newDescriptorCache(descriptorCacheSize, descriptorTTL, nil)
		}
	})
	return r.cache
}

// cachedGet is Get for authentication: the outcome, found or unknown, is
// remembered for descriptorTTL. Writers use Get, which always carries the
// current ETag.
func (r *Records) cachedGet(ctx context.Context, name string) (Record, error) {
	cache := r.descriptors()
	if rec, err, ok := cache.lookup(name); ok {
		return rec, err
	}
	rec, err := r.Get(ctx, name)
	if err == nil || errors.Is(err, s3err.GetAPIError(s3err.ErrNoSuchBucket)) {
		cache.remember(name, rec, err)
	}
	return rec, err
}

func (r *Records) Get(ctx context.Context, name string) (Record, error) {
	var out Record
	if !validName(name) {
		return out, s3err.GetAPIError(s3err.ErrNoSuchBucket)
	}
	obj, err := r.Store.Client.GetObject(ctx, r.Store.Bucket, metadataPrefix+name+".json", minio.GetObjectOptions{})
	if err != nil {
		return out, err
	}
	defer obj.Close()
	// Read first: Stat before Read performs HEAD followed by a conditional
	// GET, which can fail during concurrent credential updates. This GET
	// supplies both the descriptor and its ETag from the same snapshot.
	data, err := io.ReadAll(io.LimitReader(obj, (16<<10)+1))
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return out, s3err.GetAPIError(s3err.ErrNoSuchBucket)
		}
		return out, err
	}
	if len(data) > 16<<10 {
		return out, errors.New("invalid gateway descriptor size")
	}
	st, err := obj.Stat()
	if err != nil {
		return out, err
	}
	if st.ETag == "" {
		return out, errors.New("gateway descriptor is missing its ETag")
	}
	err = json.Unmarshal(data, &out)
	out.etag = st.ETag
	if err == nil && out.Name != name {
		err = errors.New("invalid gateway descriptor")
	}
	return out, err
}
func (r *Records) save(ctx context.Context, rec Record) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	opts := minio.PutObjectOptions{ContentType: "application/json", DisableMultipart: true}
	if rec.etag == "" {
		opts.SetMatchETagExcept("*")
	} else {
		opts.SetMatchETag(rec.etag)
	}
	_, err = r.Store.Client.PutObject(ctx, r.Store.Bucket, metadataPrefix+rec.Name+".json", bytes.NewReader(data), int64(len(data)), opts)
	// Whatever the outcome, this replica authenticates against the provider's
	// copy next; the other replica catches up within descriptorTTL.
	r.descriptors().forget(rec.Name)
	return err
}
func (r *Records) Ensure(ctx context.Context, spec objectstore.BucketSpec) error {
	if !validName(spec.Name) {
		return errors.New("invalid logical bucket name")
	}
	if spec.Versioning {
		return errors.New("logical bucket versioning is not supported; configure physical bucket versioning for disaster recovery")
	}
	if spec.RetentionDays < 0 {
		return errors.New("retentionDays must not be negative")
	}
	for attempt := 0; attempt < 5; attempt++ {
		rec, err := r.Get(ctx, spec.Name)
		if err != nil {
			if !errors.Is(err, s3err.GetAPIError(s3err.ErrNoSuchBucket)) {
				return err
			}
			rec = Record{Name: spec.Name, Created: time.Now().UTC(), Disabled: true}
		}
		if rec.etag != "" && rec.RetentionDays == spec.RetentionDays {
			return nil
		}
		rec.RetentionDays = spec.RetentionDays
		err = r.save(ctx, rec)
		if conditionalConflict(err) {
			continue
		}
		return err
	}
	return errors.New("concurrent gateway descriptor update; retry")
}
func (r *Records) Credential(ctx context.Context, name, access, secret string) (objectstore.Credential, error) {
	for attempt := 0; attempt < 5; attempt++ {
		rec, err := r.Get(ctx, name)
		if err != nil {
			return objectstore.Credential{}, err
		}
		if !rec.Disabled && rec.Secret != "" {
			return objectstore.Credential{AccessKey: name, SecretKey: rec.Secret}, nil
		}
		// Re-enabling a revoked descriptor always issues a new secret, even
		// if a stale Kubernetes Secret still contains the old credential.
		raw := make([]byte, 32)
		if _, err = rand.Read(raw); err != nil {
			return objectstore.Credential{}, err
		}
		rec.Secret = hex.EncodeToString(raw)
		rec.Disabled = false
		err = r.save(ctx, rec)
		if conditionalConflict(err) {
			continue
		}
		return objectstore.Credential{AccessKey: name, SecretKey: rec.Secret}, err
	}
	return objectstore.Credential{}, errors.New("concurrent gateway credential update; retry")
}
func (r *Records) Revoke(ctx context.Context, name string) error {
	for attempt := 0; attempt < 5; attempt++ {
		rec, err := r.Get(ctx, name)
		if errors.Is(err, s3err.GetAPIError(s3err.ErrNoSuchBucket)) {
			return nil
		}
		if err != nil {
			return err
		}
		rec.Disabled = true
		rec.Secret = ""
		err = r.save(ctx, rec)
		if conditionalConflict(err) {
			continue
		}
		return err
	}
	return errors.New("concurrent gateway revocation; retry")
}
func (r *Records) Delete(ctx context.Context, name string) error {
	if err := r.Revoke(ctx, name); err != nil {
		return err
	}
	if !validName(name) {
		return errors.New("invalid logical bucket")
	}
	// Leave a disabled tombstone: access remains revoked even during retries.
	for obj := range r.Store.Client.ListObjects(ctx, r.Store.Bucket, minio.ListObjectsOptions{Prefix: prefix(name), Recursive: true}) {
		if obj.Err != nil {
			return obj.Err
		}
		if err := r.Store.Client.RemoveObject(ctx, r.Store.Bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
	}
	return ctx.Err()
}
func (r *Records) Usage(ctx context.Context, name string) (*objectstore.Capacity, error) {
	if name != "" && !validName(name) {
		return nil, errors.New("invalid logical bucket")
	}
	p := "buckets/"
	if name != "" {
		p = prefix(name)
	}
	out := &objectstore.Capacity{Buckets: map[string]objectstore.Usage{}, MeasuredAt: time.Now()}
	for obj := range r.Store.Client.ListObjects(ctx, r.Store.Bucket, minio.ListObjectsOptions{Prefix: p, Recursive: true}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		parts := strings.SplitN(strings.TrimPrefix(obj.Key, "buckets/"), "/", 2)
		if len(parts) != 2 {
			continue
		}
		u := out.Buckets[parts[0]]
		u.Objects++
		u.Bytes += obj.Size
		out.Buckets[parts[0]] = u
		out.UsedBytes += obj.Size
	}
	return out, ctx.Err()
}

// Sweep enforces consumer retention without one cloud lifecycle rule per
// consumer. Run serially with bounded memory; cloud lifecycle also aborts
// incomplete multipart uploads globally.
func (r *Records) Sweep(ctx context.Context) error {
	for obj := range r.Store.Client.ListObjects(ctx, r.Store.Bucket, minio.ListObjectsOptions{Prefix: metadataPrefix, Recursive: true}) {
		if obj.Err != nil {
			return obj.Err
		}
		name := strings.TrimSuffix(strings.TrimPrefix(obj.Key, metadataPrefix), ".json")
		rec, err := r.Get(ctx, name)
		if err != nil {
			return err
		}
		if rec.RetentionDays <= 0 {
			continue
		}
		cutoff := time.Now().Add(-time.Duration(rec.RetentionDays) * 24 * time.Hour)
		for data := range r.Store.Client.ListObjects(ctx, r.Store.Bucket, minio.ListObjectsOptions{Prefix: prefix(name), Recursive: true}) {
			if data.Err != nil {
				return data.Err
			}
			if data.LastModified.Before(cutoff) {
				if r.client == nil {
					return errors.New("retention requires a conditional-delete client")
				}
				_, err := r.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(r.Store.Bucket), Key: aws.String(data.Key), IfMatch: aws.String(data.ETag)})
				var apiErr smithy.APIError
				if err != nil && !(errors.As(err, &apiErr) && apiErr.ErrorCode() == "PreconditionFailed") {
					return err
				}
			}
		}
	}
	return ctx.Err()
}

type IAM struct{ Records *Records }

func (i *IAM) GetUserAccount(access string) (auth.Account, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := i.Records.cachedGet(ctx, access)
	if err != nil {
		if errors.Is(err, s3err.GetAPIError(s3err.ErrNoSuchBucket)) {
			return auth.Account{}, auth.ErrNoSuchUser
		}
		return auth.Account{}, s3err.GetAPIError(s3err.ErrInternalError)
	}
	if r.Disabled || r.Secret == "" {
		return auth.Account{}, auth.ErrNoSuchUser
	}
	return auth.Account{Access: r.Name, Secret: r.Secret, Role: auth.RoleUser}, nil
}
func (*IAM) CreateAccount(auth.Account) error                  { return denied() }
func (*IAM) UpdateUserAccount(string, auth.MutableProps) error { return denied() }
func (*IAM) DeleteUserAccount(string) error                    { return denied() }
func (*IAM) ListUserAccounts() ([]auth.Account, error)         { return nil, denied() }
func (*IAM) Shutdown() error                                   { return nil }
func (i *IAM) ResolveAccounts(access []string) ([]string, error) {
	var missing []string
	for _, a := range access {
		if _, err := i.GetUserAccount(a); err != nil {
			missing = append(missing, a)
		}
	}
	return missing, nil
}
