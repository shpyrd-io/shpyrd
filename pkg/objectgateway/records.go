package objectgateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

// Record is a logical bucket's descriptor: its credential, what it may
// hold and what it holds (storage plan step 5), kept in the provider
// bucket under metadataPrefix and rewritten with conditional writes.
type Record struct {
	Name          string
	Secret        string
	Created       time.Time
	RetentionDays int32
	Disabled      bool
	// MaxBytes caps what the bucket holds (0: no cap); Public lets anyone
	// read an object by its address; Alias names the logical bucket whose
	// prefix this one serves (a bucket restored into another project);
	// CORS is the bucket's CORS configuration as the consumer put it.
	MaxBytes int64  `json:",omitempty"`
	Public   bool   `json:",omitempty"`
	Alias    string `json:",omitempty"`
	CORS     string `json:",omitempty"`
	// UsedBytes and Objects are the counter: what the bucket held when
	// MeasuredAt was listed, plus every delta flushed since. Zero
	// MeasuredAt means never measured: usage lists until the sweep does.
	UsedBytes  int64     `json:",omitempty"`
	Objects    int64     `json:",omitempty"`
	MeasuredAt time.Time `json:",omitempty"`
	etag       string
}

// prefix is where the record's objects live: its alias's prefix when it
// serves another bucket's objects, its own otherwise.
func (rec Record) prefix() string {
	if rec.Alias != "" {
		return prefix(rec.Alias)
	}
	return prefix(rec.Name)
}

// maxCORS bounds a CORS configuration so the descriptor stays well under
// the 16 KiB a read allows.
const maxCORS = 8 << 10

// Records performs direct, conditional S3 metadata reads and writes. No
// per-consumer IAM/Kubernetes resources are needed; authentication reads
// through a small, short-lived memory of descriptors (see descriptorCache).
// Revocation applies to newly authenticated requests on this replica
// immediately and on the other within descriptorTTL; already authorized
// transfers can finish.
type Records struct {
	Store  *objectstore.S3
	client *s3.Client
	// SingleWriter says no other process writes descriptors while this one
	// runs. Updates are serialized in-process whatever the provider (see
	// lock); on a provider that ignores If-Match that serialization is all
	// that prevents a lost update, so Preflight accepts such a provider only
	// with SingleWriter set, and a deployment that sets it runs one gateway.
	SingleWriter bool

	locks sync.Map // descriptor name -> *sync.Mutex

	cacheOnce sync.Once
	cache     *descriptorCache

	// pending are this replica's unflushed deltas per logical bucket:
	// what its data operations added or removed since the last flush.
	pendingMu sync.Mutex
	pending   map[string]*delta
}

// delta is what a replica's own writes changed in a bucket since its
// last flush.
type delta struct{ bytes, objects int64 }

func (r *Records) descriptors() *descriptorCache {
	r.cacheOnce.Do(func() {
		if r.cache == nil {
			r.cache = newDescriptorCache(descriptorCacheSize, descriptorTTL, nil)
		}
	})
	return r.cache
}

// lock serializes this process's read-modify-writes of one descriptor, so
// two of its own callers never lose each other's update; the retry on a
// conditional conflict remains for other processes. The caller runs the
// returned function when done.
func (r *Records) lock(name string) func() {
	mu, _ := r.locks.LoadOrStore(name, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	return mu.(*sync.Mutex).Unlock
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
	if spec.MaxBytes < 0 {
		return errors.New("maxBytes must not be negative")
	}
	if spec.Alias != "" && (!validName(spec.Alias) || spec.Alias == spec.Name) {
		return errors.New("invalid logical bucket alias")
	}
	defer r.lock(spec.Name)()
	for attempt := 0; attempt < 5; attempt++ {
		rec, err := r.Get(ctx, spec.Name)
		if err != nil {
			if !errors.Is(err, s3err.GetAPIError(s3err.ErrNoSuchBucket)) {
				return err
			}
			rec = Record{Name: spec.Name, Created: time.Now().UTC(), Disabled: true}
			// A new bucket of its own holds nothing: measured by
			// definition. One serving another's objects is measured by
			// the first sweep.
			if spec.Alias == "" {
				rec.MeasuredAt = rec.Created
			}
		}
		if rec.etag != "" && rec.RetentionDays == spec.RetentionDays && rec.MaxBytes == spec.MaxBytes && rec.Public == spec.Public && rec.Alias == spec.Alias {
			return nil
		}
		if rec.etag != "" && rec.Alias != spec.Alias {
			return errors.New("a logical bucket's alias cannot change")
		}
		rec.RetentionDays = spec.RetentionDays
		rec.MaxBytes = spec.MaxBytes
		rec.Public = spec.Public
		rec.Alias = spec.Alias
		err = r.save(ctx, rec)
		if conditionalConflict(err) {
			continue
		}
		return err
	}
	return errors.New("concurrent gateway descriptor update; retry")
}
func (r *Records) Credential(ctx context.Context, name, access, secret string) (objectstore.Credential, error) {
	defer r.lock(name)()
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
	defer r.lock(name)()
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
	if !validName(name) {
		return errors.New("invalid logical bucket")
	}
	rec, err := r.Get(ctx, name)
	if err != nil && !errors.Is(err, s3err.GetAPIError(s3err.ErrNoSuchBucket)) {
		return err
	}
	if err := r.Revoke(ctx, name); err != nil {
		return err
	}
	r.forgetPending(name)
	// A bucket serving another's objects takes only its key with it: the
	// objects belong to the name they were written under.
	if rec.Alias != "" {
		return nil
	}
	// Leave a disabled tombstone: access remains revoked even during retries.
	return r.removePrefix(ctx, prefix(name))
}

// ---- CORS ---------------------------------------------------------------------

// SetCORS keeps a bucket's CORS configuration on its record, as the
// consumer put it; empty removes it.
func (r *Records) SetCORS(ctx context.Context, name string, cors []byte) error {
	if len(cors) > maxCORS {
		return s3err.GetAPIError(s3err.ErrMalformedXML)
	}
	defer r.lock(name)()
	for attempt := 0; attempt < 5; attempt++ {
		rec, err := r.Get(ctx, name)
		if err != nil {
			return err
		}
		rec.CORS = string(cors)
		err = r.save(ctx, rec)
		if conditionalConflict(err) {
			continue
		}
		return err
	}
	return errors.New("concurrent gateway descriptor update; retry")
}

// ---- the counter ---------------------------------------------------------------

// Allow says whether a bucket may take size more bytes: what the record
// last flushed plus this replica's unflushed writes, against its cap. The
// record may be up to descriptorTTL old, so the overshoot is bounded by
// what is in flight plus a minute of another replica's writes (storage
// plan step 5).
func (r *Records) Allow(ctx context.Context, name string, size int64) error {
	rec, err := r.cachedGet(ctx, name)
	if err != nil {
		return err
	}
	if rec.MaxBytes <= 0 {
		return nil
	}
	used := rec.UsedBytes + r.pendingDelta(name).bytes
	if used+size > rec.MaxBytes {
		capRefusals.Inc()
		return s3err.APIError{Code: "QuotaExceeded", Description: fmt.Sprintf("This bucket holds up to %s; this upload would bring it to %s.", gbWords(rec.MaxBytes), gbWords(used+size)), HTTPStatusCode: http.StatusForbidden}
	}
	return nil
}

// Add records what a data operation changed in a bucket; flushed later.
func (r *Records) Add(name string, bytes, objects int64) {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	if r.pending == nil {
		r.pending = map[string]*delta{}
	}
	d := r.pending[name]
	if d == nil {
		d = &delta{}
		r.pending[name] = d
	}
	d.bytes += bytes
	d.objects += objects
}

func (r *Records) pendingDelta(name string) delta {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	if d := r.pending[name]; d != nil {
		return *d
	}
	return delta{}
}

// takePending returns the deltas to flush and zeroes them; a flush that
// fails gives its delta back with Add.
func (r *Records) takePending() map[string]delta {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	out := map[string]delta{}
	for name, d := range r.pending {
		if d.bytes != 0 || d.objects != 0 {
			out[name] = *d
		}
		delete(r.pending, name)
	}
	return out
}

func (r *Records) forgetPending(name string) {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	delete(r.pending, name)
}

// Flush writes this replica's deltas onto the records, each as an
// increment over the record re-read under its lock, so two replicas'
// flushes add up rather than overwrite each other (the provider's
// conditional write, or the single writer, keeps the read and the write
// one). A delta the provider would not take is kept for the next flush.
func (r *Records) Flush(ctx context.Context) error {
	var first error
	for name, d := range r.takePending() {
		if err := r.apply(ctx, name, d); err != nil {
			r.Add(name, d.bytes, d.objects)
			if first == nil {
				first = err
			}
		}
	}
	return first
}

func (r *Records) apply(ctx context.Context, name string, d delta) error {
	defer r.lock(name)()
	for attempt := 0; attempt < 5; attempt++ {
		rec, err := r.Get(ctx, name)
		if errors.Is(err, s3err.GetAPIError(s3err.ErrNoSuchBucket)) {
			return nil // deleted meanwhile: nothing to count
		}
		if err != nil {
			return err
		}
		rec.UsedBytes = max(rec.UsedBytes+d.bytes, 0)
		rec.Objects = max(rec.Objects+d.objects, 0)
		err = r.save(ctx, rec)
		if conditionalConflict(err) {
			continue
		}
		return err
	}
	return errors.New("concurrent gateway descriptor update; retry")
}

// gbWords is a number of bytes in the words a person reads: 1 GB, 1.5 GB,
// 512 MB.
func gbWords(n int64) string {
	switch {
	case n >= 1<<30:
		return trimZeros(fmt.Sprintf("%.1f", float64(n)/(1<<30))) + " GB"
	case n >= 1<<20:
		return trimZeros(fmt.Sprintf("%.1f", float64(n)/(1<<20))) + " MB"
	case n >= 1<<10:
		return trimZeros(fmt.Sprintf("%.1f", float64(n)/(1<<10))) + " KB"
	}
	return fmt.Sprintf("%d bytes", n)
}

func trimZeros(s string) string {
	return strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
}

// removePrefix empties a prefix with batched deletes, a thousand keys per
// request. A listing error stops the deletes and is reported instead of being
// handed to them as an object.
func (r *Records) removePrefix(ctx context.Context, p string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	objects := make(chan minio.ObjectInfo)
	var listErr error
	go func() {
		defer close(objects)
		for obj := range r.Store.Client.ListObjects(ctx, r.Store.Bucket, minio.ListObjectsOptions{Prefix: p, Recursive: true}) {
			if obj.Err != nil {
				listErr = obj.Err
				return
			}
			select {
			case objects <- obj:
			case <-ctx.Done():
				return
			}
		}
	}()
	for e := range r.Store.Client.RemoveObjects(ctx, r.Store.Bucket, objects, minio.RemoveObjectsOptions{}) {
		if e.Err != nil {
			return e.Err
		}
	}
	// The results channel closes after the objects channel does, so listErr is
	// settled here.
	if listErr != nil {
		return listErr
	}
	return ctx.Err()
}

// Usage is what a bucket holds: its counter once the bucket has been
// measured (a new bucket is; an older one or an alias after the first
// sweep), a listing of its prefix until then. Without a name, every
// bucket's prefix is listed.
func (r *Records) Usage(ctx context.Context, name string) (*objectstore.Capacity, error) {
	if name != "" && !validName(name) {
		return nil, errors.New("invalid logical bucket")
	}
	p := "buckets/"
	if name != "" {
		rec, err := r.Get(ctx, name)
		if err != nil {
			return nil, err
		}
		if !rec.MeasuredAt.IsZero() {
			d := r.pendingDelta(name)
			u := objectstore.Usage{Bytes: max(rec.UsedBytes+d.bytes, 0), Objects: max(rec.Objects+d.objects, 0)}
			return &objectstore.Capacity{Buckets: map[string]objectstore.Usage{name: u}, UsedBytes: u.Bytes, MeasuredAt: rec.MeasuredAt}, nil
		}
		p = rec.prefix()
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
// consumer, measures every bucket (the listing is the counter's source of
// truth, correcting what a lost flush left) and aborts multipart uploads
// older than staleUpload, which no cloud lifecycle rule does on every
// provider. Run serially with bounded memory.
func (r *Records) Sweep(ctx context.Context) error {
	for obj := range r.Store.Client.ListObjects(ctx, r.Store.Bucket, minio.ListObjectsOptions{Prefix: metadataPrefix, Recursive: true}) {
		if obj.Err != nil {
			return obj.Err
		}
		name := strings.TrimSuffix(strings.TrimPrefix(obj.Key, metadataPrefix), ".json")
		if err := r.sweepOne(ctx, name); err != nil {
			return err
		}
	}
	if err := r.abortStaleUploads(ctx); err != nil {
		return err
	}
	return ctx.Err()
}

// staleUpload is how old an unfinished multipart upload may be.
const staleUpload = 7 * 24 * time.Hour

func (r *Records) sweepOne(ctx context.Context, name string) error {
	rec, err := r.Get(ctx, name)
	if err != nil {
		return err
	}
	var cutoff time.Time
	if rec.RetentionDays > 0 {
		cutoff = time.Now().Add(-time.Duration(rec.RetentionDays) * 24 * time.Hour)
	}
	var bytes, objects int64
	for data := range r.Store.Client.ListObjects(ctx, r.Store.Bucket, minio.ListObjectsOptions{Prefix: rec.prefix(), Recursive: true}) {
		if data.Err != nil {
			return data.Err
		}
		if !cutoff.IsZero() && data.LastModified.Before(cutoff) {
			if r.client == nil {
				return errors.New("retention requires a conditional-delete client")
			}
			_, err := r.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(r.Store.Bucket), Key: aws.String(data.Key), IfMatch: aws.String(data.ETag)})
			var apiErr smithy.APIError
			if err != nil && !(errors.As(err, &apiErr) && apiErr.ErrorCode() == "PreconditionFailed") {
				return err
			}
			continue
		}
		bytes += data.Size
		objects++
	}
	// The measurement replaces the counter; what this replica wrote while
	// listing is in the listing or in the next one.
	defer r.lock(name)()
	for attempt := 0; attempt < 5; attempt++ {
		rec, err := r.Get(ctx, name)
		if errors.Is(err, s3err.GetAPIError(s3err.ErrNoSuchBucket)) {
			return nil
		}
		if err != nil {
			return err
		}
		rec.UsedBytes, rec.Objects, rec.MeasuredAt = bytes, objects, time.Now().UTC()
		r.forgetPending(name)
		err = r.save(ctx, rec)
		if conditionalConflict(err) {
			continue
		}
		return err
	}
	return errors.New("concurrent gateway descriptor update; retry")
}

// abortStaleUploads aborts every multipart upload under buckets/ begun
// more than staleUpload ago: parts nobody will complete, which the
// counter stopped seeing and the provider bills.
func (r *Records) abortStaleUploads(ctx context.Context) error {
	if r.client == nil {
		return nil
	}
	cutoff := time.Now().Add(-staleUpload)
	in := &s3.ListMultipartUploadsInput{Bucket: aws.String(r.Store.Bucket), Prefix: aws.String("buckets/")}
	for {
		out, err := r.client.ListMultipartUploads(ctx, in)
		if err != nil {
			return err
		}
		for _, u := range out.Uploads {
			if u.Initiated == nil || !u.Initiated.Before(cutoff) {
				continue
			}
			if _, err := r.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{Bucket: aws.String(r.Store.Bucket), Key: u.Key, UploadId: u.UploadId}); err != nil {
				var apiErr smithy.APIError
				if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchUpload" {
					continue
				}
				return err
			}
			staleAborts.Inc()
		}
		if out.IsTruncated == nil || !*out.IsTruncated {
			return nil
		}
		in.KeyMarker, in.UploadIdMarker = out.NextKeyMarker, out.NextUploadIdMarker
	}
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
