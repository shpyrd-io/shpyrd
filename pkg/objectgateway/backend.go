// Package objectgateway implements isolated logical S3 buckets over a shared
// provider bucket. Only the explicitly implemented data operations can reach
// the provider; bucket administration is intentionally unsupported.
package objectgateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/versity/versitygw/auth"
	"github.com/versity/versitygw/backend"
	"github.com/versity/versitygw/backend/s3proxy"
	"github.com/versity/versitygw/s3err"
	"github.com/versity/versitygw/s3response"
)

type Backend struct {
	backend.BackendUnsupported
	proxy    *s3proxy.S3Proxy
	physical string
	records  *Records
}

func (b *Backend) String() string                               { return "shpyrd object gateway" }
func (b *Backend) NormalizeObjectKey(bucket, key string) string { return key }
func prefix(bucket string) string                               { return "buckets/" + bucket + "/" }
func denied() error                                             { return s3err.GetAPIError(s3err.ErrAccessDenied) }

// prefixOf is where a logical bucket's objects live, through its record:
// its own prefix, or the one it serves as an alias (storage plan step 5).
// The record comes from the authentication cache, so a data operation
// costs no provider read.
func (b *Backend) prefixOf(ctx context.Context, bucket string) (string, error) {
	if !validName(bucket) {
		return "", denied()
	}
	rec, err := b.records.cachedGet(ctx, bucket)
	if err != nil {
		if errors.Is(err, s3err.GetAPIError(s3err.ErrNoSuchBucket)) {
			return "", denied()
		}
		return "", err
	}
	return rec.prefix(), nil
}
func safeKey(key string) bool {
	for _, part := range strings.Split(key, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return !strings.ContainsRune(key, '\x00')
}
func (b *Backend) mapKey(ctx context.Context, bucket, key **string) error {
	if *bucket == nil || *key == nil || !safeKey(**key) {
		return denied()
	}
	p, err := b.prefixOf(ctx, **bucket)
	if err != nil {
		return err
	}
	*key = aws.String(p + **key)
	*bucket = aws.String(b.physical)
	return nil
}

// sizeOf is an object's size at the provider, 0 when it is not there.
func (b *Backend) sizeOf(ctx context.Context, physicalKey string) int64 {
	out, err := b.proxy.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(b.physical), Key: aws.String(physicalKey)})
	if err != nil || out == nil {
		return 0
	}
	return aws.ToInt64(out.ContentLength)
}

func (b *Backend) GetBucketAcl(ctx context.Context, in *s3.GetBucketAclInput) ([]byte, error) {
	r, err := b.records.cachedGet(ctx, aws.ToString(in.Bucket))
	if err != nil {
		return nil, err
	}
	return json.Marshal(auth.ACL{Owner: r.Name, Grantees: []auth.Grantee{{Access: r.Name, Permission: auth.PermissionFullControl, Type: types.TypeCanonicalUser}}})
}

// GetBucketPolicy is how a public bucket opens its objects to anyone
// (storage plan step 5): a policy allowing GetObject to every principal,
// and nothing else, so an address is readable and a listing is not. A
// bucket that is not public has no policy. Policies are never written by
// consumers; the bucket's resource says whether it is public.
func (b *Backend) GetBucketPolicy(ctx context.Context, bucket string) ([]byte, error) {
	r, err := b.records.cachedGet(ctx, bucket)
	if err != nil {
		return nil, err
	}
	if !r.Public {
		return nil, s3err.GetAPIError(s3err.ErrNoSuchBucketPolicy)
	}
	return json.Marshal(map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{{
			"Effect": "Allow", "Principal": "*", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::" + bucket + "/*",
		}},
	})
}

// CORS is the consumer's, kept on the record: a browser uploads through a
// presigned address only when the bucket allows its origin.
func (b *Backend) PutBucketCors(ctx context.Context, bucket string, cors []byte) error {
	if !validName(bucket) {
		return denied()
	}
	return b.records.SetCORS(ctx, bucket, cors)
}
func (b *Backend) GetBucketCors(ctx context.Context, bucket string) ([]byte, error) {
	r, err := b.records.cachedGet(ctx, bucket)
	if err != nil {
		return nil, err
	}
	if r.CORS == "" {
		return nil, s3err.GetAPIError(s3err.ErrNoSuchCORSConfiguration)
	}
	return []byte(r.CORS), nil
}
func (b *Backend) DeleteBucketCors(ctx context.Context, bucket string) error {
	if !validName(bucket) {
		return denied()
	}
	return b.records.SetCORS(ctx, bucket, nil)
}
func (b *Backend) GetBucketOwnershipControls(ctx context.Context, bucket string) (types.ObjectOwnership, error) {
	return types.ObjectOwnershipBucketOwnerEnforced, nil
}
func (b *Backend) HeadBucket(ctx context.Context, in *s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
	_, err := b.records.cachedGet(ctx, aws.ToString(in.Bucket))
	return &s3.HeadBucketOutput{}, err
}
func (b *Backend) ListBuckets(ctx context.Context, in s3response.ListBucketsInput) (s3response.ListAllMyBucketsResult, error) {
	out := s3response.ListAllMyBucketsResult{Owner: s3response.CanonicalUser{ID: in.Owner}}
	r, err := b.records.cachedGet(ctx, in.Owner)
	if err != nil {
		return out, err
	}
	if strings.HasPrefix(r.Name, in.Prefix) {
		out.Buckets.Bucket = []s3response.ListAllMyBucketsEntry{{Name: r.Name, CreationDate: r.Created}}
	}
	return out, nil
}

// withoutAWSChunked drops the "aws-chunked" token from a Content-Encoding the
// consumer sent. The front door has already decoded the chunked body by the
// time the backend runs, so the token describes nothing the provider will
// receive; forwarded, a provider that honours the SigV4 streaming rules (OCI,
// AWS itself) refuses the PUT: "x-amz-content-sha256 must be
// STREAMING-AWS4-HMAC-SHA256-PAYLOAD ... for aws-chunked uploads". What the
// consumer declared beyond that token ("aws-chunked, gzip") is the object's
// own encoding and stays.
func withoutAWSChunked(encoding *string) *string {
	if encoding == nil {
		return nil
	}
	var kept []string
	for _, token := range strings.Split(*encoding, ",") {
		token = strings.TrimSpace(token)
		if token == "" || strings.EqualFold(token, "aws-chunked") {
			continue
		}
		kept = append(kept, token)
	}
	if len(kept) == 0 {
		return nil
	}
	return aws.String(strings.Join(kept, ", "))
}

// putChecksumValues are the checksum values a consumer can declare on a PUT.
// When none is set but an algorithm is, the front door derived the algorithm
// from a trailer it has already consumed and verified; forwarded alone, the
// algorithm makes the provider client compute the checksum itself, which
// turns the upload back into an aws-chunked trailer stream (or fails outright
// on a plain-HTTP provider, where the SDK refuses an unseekable body).
func putChecksumValues(in *s3response.PutObjectInput) []*string {
	return []*string{in.ChecksumCRC32, in.ChecksumCRC32C, in.ChecksumSHA1, in.ChecksumSHA256, in.ChecksumCRC64NVME, in.ChecksumSHA512, in.ChecksumMD5, in.ChecksumXXHASH64, in.ChecksumXXHASH3, in.ChecksumXXHASH128}
}

func partChecksumValues(in *s3.UploadPartInput) []*string {
	return []*string{in.ChecksumCRC32, in.ChecksumCRC32C, in.ChecksumCRC64NVME, in.ChecksumSHA1, in.ChecksumSHA256}
}

// unvaluedAlgorithm reports an algorithm declared without any checksum value.
func unvaluedAlgorithm(algorithm types.ChecksumAlgorithm, values []*string) bool {
	if algorithm == "" {
		return false
	}
	for _, v := range values {
		if v != nil && *v != "" {
			return false
		}
	}
	return true
}

func (b *Backend) PutObject(ctx context.Context, in s3response.PutObjectInput) (s3response.PutObjectOutput, error) {
	in.GrantFullControl = nil
	in.GrantRead = nil
	in.GrantReadACP = nil
	in.GrantWriteACP = nil
	in.ContentEncoding = withoutAWSChunked(in.ContentEncoding)
	if unvaluedAlgorithm(in.ChecksumAlgorithm, putChecksumValues(&in)) {
		in.ChecksumAlgorithm = ""
	}
	in.ExpectedBucketOwner = nil
	bucket, size := aws.ToString(in.Bucket), aws.ToInt64(in.ContentLength)
	if err := b.mapKey(ctx, &in.Bucket, &in.Key); err != nil {
		return s3response.PutObjectOutput{}, err
	}
	// The cap (storage plan step 5): an upload that would pass it is
	// refused before a byte reaches the provider. An overwrite counts as
	// new bytes until the sweep measures; the cap errs on the full side.
	if err := b.records.Allow(ctx, bucket, size); err != nil {
		return s3response.PutObjectOutput{}, err
	}
	out, err := b.proxy.PutObject(ctx, in)
	if err == nil {
		b.records.Add(bucket, size, 1)
	}
	return out, err
}
func (b *Backend) HeadObject(ctx context.Context, in *s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
	if err := b.mapKey(ctx, &in.Bucket, &in.Key); err != nil {
		return nil, err
	}
	return b.proxy.HeadObject(ctx, in)
}
func (b *Backend) GetObject(ctx context.Context, in *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
	if err := b.mapKey(ctx, &in.Bucket, &in.Key); err != nil {
		return nil, err
	}
	return b.proxy.GetObject(ctx, in)
}
func (b *Backend) DeleteObject(ctx context.Context, in *s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error) {
	bucket := aws.ToString(in.Bucket)
	if err := b.mapKey(ctx, &in.Bucket, &in.Key); err != nil {
		return nil, err
	}
	// What goes is counted out at once, so room freed is room usable.
	size := b.sizeOf(ctx, aws.ToString(in.Key))
	out, err := b.proxy.DeleteObject(ctx, in)
	if err == nil && size > 0 {
		b.records.Add(bucket, -size, -1)
	}
	return out, err
}
func (b *Backend) UploadPart(ctx context.Context, in *s3.UploadPartInput) (*s3.UploadPartOutput, error) {
	if unvaluedAlgorithm(in.ChecksumAlgorithm, partChecksumValues(in)) {
		in.ChecksumAlgorithm = ""
	}
	in.ExpectedBucketOwner = nil
	bucket, size := aws.ToString(in.Bucket), aws.ToInt64(in.ContentLength)
	if err := b.mapKey(ctx, &in.Bucket, &in.Key); err != nil {
		return nil, err
	}
	// Parts of an unfinished upload count: they are bytes the provider
	// holds; an abort counts them out, the sweep aborts what is forgotten.
	if err := b.records.Allow(ctx, bucket, size); err != nil {
		return nil, err
	}
	out, err := b.proxy.UploadPart(ctx, in)
	if err == nil {
		b.records.Add(bucket, size, 0)
	}
	return out, err
}
func (b *Backend) CreateMultipartUpload(ctx context.Context, in s3response.CreateMultipartUploadInput) (s3response.InitiateMultipartUploadResult, error) {
	bucket, key := aws.ToString(in.Bucket), aws.ToString(in.Key)
	// Never forward consumer ACL grants to the physical bucket.
	in.ACL = ""
	in.GrantFullControl = nil
	in.GrantRead = nil
	in.GrantReadACP = nil
	in.GrantWriteACP = nil
	in.ContentEncoding = withoutAWSChunked(in.ContentEncoding)
	in.ExpectedBucketOwner = nil
	if err := b.mapKey(ctx, &in.Bucket, &in.Key); err != nil {
		return s3response.InitiateMultipartUploadResult{}, err
	}
	out, err := b.proxy.CreateMultipartUpload(ctx, in)
	out.Bucket = bucket
	out.Key = key
	return out, err
}
func (b *Backend) CompleteMultipartUpload(ctx context.Context, in *s3.CompleteMultipartUploadInput) (s3response.CompleteMultipartUploadResult, string, error) {
	bucket, key := in.Bucket, in.Key
	if err := b.mapKey(ctx, &in.Bucket, &in.Key); err != nil {
		return s3response.CompleteMultipartUploadResult{}, "", err
	}
	out, version, err := b.proxy.CompleteMultipartUpload(ctx, in)
	out.Bucket = bucket
	out.Key = key
	out.Location = nil
	if err == nil {
		// The parts were counted as they came; the object is one more.
		b.records.Add(aws.ToString(bucket), 0, 1)
	}
	return out, version, err
}
func (b *Backend) AbortMultipartUpload(ctx context.Context, in *s3.AbortMultipartUploadInput) error {
	bucket := aws.ToString(in.Bucket)
	if err := b.mapKey(ctx, &in.Bucket, &in.Key); err != nil {
		return err
	}
	parts := b.partsSize(ctx, in.Bucket, in.Key, in.UploadId)
	if err := b.proxy.AbortMultipartUpload(ctx, in); err != nil {
		return err
	}
	if parts > 0 {
		b.records.Add(bucket, -parts, 0)
	}
	return nil
}

// partsSize is what the parts of an unfinished upload weigh, 0 when they
// cannot be listed.
func (b *Backend) partsSize(ctx context.Context, physicalBucket, key, uploadID *string) int64 {
	var total int64
	in := &s3.ListPartsInput{Bucket: physicalBucket, Key: key, UploadId: uploadID}
	for {
		out, err := b.proxy.ListParts(ctx, in)
		if err != nil {
			return 0
		}
		for _, p := range out.Parts {
			total += p.Size
		}
		if !out.IsTruncated || out.NextPartNumberMarker <= 0 {
			return total
		}
		in.PartNumberMarker = aws.String(strconv.Itoa(out.NextPartNumberMarker))
	}
}
func (b *Backend) ListParts(ctx context.Context, in *s3.ListPartsInput) (s3response.ListPartsResult, error) {
	bucket, key := aws.ToString(in.Bucket), aws.ToString(in.Key)
	if err := b.mapKey(ctx, &in.Bucket, &in.Key); err != nil {
		return s3response.ListPartsResult{}, err
	}
	out, err := b.proxy.ListParts(ctx, in)
	out.Bucket = bucket
	out.Key = key
	return out, err
}

// copySource maps a copy's source to the provider; the source's physical
// key comes back too, for its size.
func (b *Backend) copySource(ctx context.Context, bucket string, source *string) (*string, string, error) {
	raw, err := url.PathUnescape(aws.ToString(source))
	if err != nil {
		return nil, "", denied()
	}
	parts := strings.SplitN(strings.TrimPrefix(raw, "/"), "/", 2)
	if len(parts) != 2 || parts[0] != bucket || !safeKey(parts[1]) {
		return nil, "", denied()
	}
	p, err := b.prefixOf(ctx, bucket)
	if err != nil {
		return nil, "", err
	}
	return aws.String(url.PathEscape(b.physical + "/" + p + parts[1])), p + parts[1], nil
}
func (b *Backend) CopyObject(ctx context.Context, in s3response.CopyObjectInput) (s3response.CopyObjectOutput, error) {
	var err error
	bucket := aws.ToString(in.Bucket)
	var sourceKey string
	in.CopySource, sourceKey, err = b.copySource(ctx, bucket, in.CopySource)
	if err != nil {
		return s3response.CopyObjectOutput{}, err
	}
	in.GrantFullControl = nil
	in.GrantRead = nil
	in.GrantReadACP = nil
	in.GrantWriteACP = nil
	// The front door fills the expected owners with the consumer's own
	// access key and whatever the request said; the provider knows neither,
	// and AWS refuses anything but the real account id. The provider's
	// storage class is the platform's choice, never a consumer's.
	in.ExpectedBucketOwner = nil
	in.ExpectedSourceBucketOwner = nil
	in.StorageClass = ""
	in.ContentEncoding = withoutAWSChunked(in.ContentEncoding)
	if err = b.mapKey(ctx, &in.Bucket, &in.Key); err != nil {
		return s3response.CopyObjectOutput{}, err
	}
	size := b.sizeOf(ctx, sourceKey)
	if err := b.records.Allow(ctx, bucket, size); err != nil {
		return s3response.CopyObjectOutput{}, err
	}
	out, err := b.proxy.CopyObject(ctx, in)
	if err == nil {
		b.records.Add(bucket, size, 1)
	}
	return out, err
}
func (b *Backend) UploadPartCopy(ctx context.Context, in *s3.UploadPartCopyInput) (s3response.CopyPartResult, error) {
	var err error
	bucket := aws.ToString(in.Bucket)
	var sourceKey string
	in.CopySource, sourceKey, err = b.copySource(ctx, bucket, in.CopySource)
	if err != nil {
		return s3response.CopyPartResult{}, err
	}
	in.ExpectedBucketOwner = nil
	in.ExpectedSourceBucketOwner = nil
	if err = b.mapKey(ctx, &in.Bucket, &in.Key); err != nil {
		return s3response.CopyPartResult{}, err
	}
	size := b.sizeOf(ctx, sourceKey)
	if rng := aws.ToString(in.CopySourceRange); rng != "" {
		size = rangeSize(rng, size)
	}
	if err := b.records.Allow(ctx, bucket, size); err != nil {
		return s3response.CopyPartResult{}, err
	}
	out, err := b.proxy.UploadPartCopy(ctx, in)
	if err == nil {
		b.records.Add(bucket, size, 0)
	}
	return out, err
}

// rangeSize is what a "bytes=a-b" range of an object of size n covers.
func rangeSize(rng string, n int64) int64 {
	spec, ok := strings.CutPrefix(rng, "bytes=")
	if !ok {
		return n
	}
	first, last, ok := strings.Cut(spec, "-")
	if !ok {
		return n
	}
	a, err := strconv.ParseInt(first, 10, 64)
	if err != nil {
		return n
	}
	bEnd := n - 1
	if last != "" {
		if bEnd, err = strconv.ParseInt(last, 10, 64); err != nil {
			return n
		}
	}
	if bEnd >= n {
		bEnd = n - 1
	}
	if a > bEnd {
		return 0
	}
	return bEnd - a + 1
}
func strip(p string, s *string) *string {
	if s == nil {
		return nil
	}
	return aws.String(strings.TrimPrefix(*s, p))
}
func stripObjects(p string, objects []s3response.Object, common []types.CommonPrefix) {
	for i := range objects {
		objects[i].Key = strip(p, objects[i].Key)
		objects[i].Owner = nil
	}
	for i := range common {
		common[i].Prefix = strip(p, common[i].Prefix)
	}
}
func (b *Backend) ListObjects(ctx context.Context, in *s3.ListObjectsInput) (s3response.ListObjectsResult, error) {
	bucket := aws.ToString(in.Bucket)
	p, err := b.prefixOf(ctx, bucket)
	if err != nil || !safeKey(aws.ToString(in.Prefix)) {
		return s3response.ListObjectsResult{}, firstError(err, denied())
	}
	in.Bucket = aws.String(b.physical)
	in.Prefix = aws.String(p + aws.ToString(in.Prefix))
	in.EncodingType = ""
	if aws.ToString(in.Marker) != "" {
		in.Marker = aws.String(p + *in.Marker)
	}
	out, err := b.proxy.ListObjects(ctx, in)
	out.Name = aws.String(bucket)
	out.Prefix = strip(p, out.Prefix)
	out.Marker = strip(p, out.Marker)
	out.NextMarker = strip(p, out.NextMarker)
	stripObjects(p, out.Contents, out.CommonPrefixes)
	return out, err
}
func (b *Backend) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input) (s3response.ListObjectsV2Result, error) {
	bucket := aws.ToString(in.Bucket)
	p, err := b.prefixOf(ctx, bucket)
	if err != nil || !safeKey(aws.ToString(in.Prefix)) {
		return s3response.ListObjectsV2Result{}, firstError(err, denied())
	}
	in.Bucket = aws.String(b.physical)
	in.Prefix = aws.String(p + aws.ToString(in.Prefix))
	in.EncodingType = ""
	if aws.ToString(in.StartAfter) != "" {
		in.StartAfter = aws.String(p + *in.StartAfter)
	}
	out, err := b.proxy.ListObjectsV2(ctx, in)
	out.Name = aws.String(bucket)
	out.Prefix = strip(p, out.Prefix)
	out.StartAfter = strip(p, in.StartAfter)
	stripObjects(p, out.Contents, out.CommonPrefixes)
	return out, err
}
func (b *Backend) DeleteObjects(ctx context.Context, in *s3.DeleteObjectsInput) (s3response.DeleteResult, error) {
	bucket := aws.ToString(in.Bucket)
	p, err := b.prefixOf(ctx, bucket)
	if err != nil || in.Delete == nil {
		return s3response.DeleteResult{}, firstError(err, denied())
	}
	for i := range in.Delete.Objects {
		if !safeKey(aws.ToString(in.Delete.Objects[i].Key)) {
			return s3response.DeleteResult{}, denied()
		}
		in.Delete.Objects[i].Key = aws.String(p + aws.ToString(in.Delete.Objects[i].Key))
	}
	in.Bucket = aws.String(b.physical)
	sizes := b.sizesOf(ctx, in.Delete.Objects)
	out, err := b.proxy.DeleteObjects(ctx, in)
	var freed, gone int64
	for i := range out.Deleted {
		if size := sizes[aws.ToString(out.Deleted[i].Key)]; size > 0 {
			freed += size
			gone++
		}
		out.Deleted[i].Key = strip(p, out.Deleted[i].Key)
	}
	for i := range out.Error {
		out.Error[i].Key = strip(p, out.Error[i].Key)
	}
	if gone > 0 {
		b.records.Add(bucket, -freed, -gone)
	}
	return out, err
}

// sizesOf asks the provider what each object weighs, a few at a time.
func (b *Backend) sizesOf(ctx context.Context, objects []types.ObjectIdentifier) map[string]int64 {
	var mu sync.Mutex
	sizes := map[string]int64{}
	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	for _, o := range objects {
		key := aws.ToString(o.Key)
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			size := b.sizeOf(ctx, key)
			mu.Lock()
			sizes[key] = size
			mu.Unlock()
		}()
	}
	wg.Wait()
	return sizes
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func (b *Backend) GetBucketVersioning(ctx context.Context, bucket string) (s3response.GetBucketVersioningOutput, error) {
	return s3response.GetBucketVersioningOutput{}, nil
}
func (b *Backend) ListMultipartUploads(ctx context.Context, in *s3.ListMultipartUploadsInput) (s3response.ListMultipartUploadsResult, error) {
	bucket := aws.ToString(in.Bucket)
	p, err := b.prefixOf(ctx, bucket)
	if err != nil || !safeKey(aws.ToString(in.Prefix)) {
		return s3response.ListMultipartUploadsResult{}, firstError(err, denied())
	}
	in.Bucket = aws.String(b.physical)
	in.Prefix = aws.String(p + aws.ToString(in.Prefix))
	in.EncodingType = ""
	if aws.ToString(in.KeyMarker) != "" {
		in.KeyMarker = aws.String(p + *in.KeyMarker)
	}
	out, err := b.proxy.ListMultipartUploads(ctx, in)
	out.Bucket = bucket
	out.Prefix = strings.TrimPrefix(out.Prefix, p)
	out.KeyMarker = strings.TrimPrefix(out.KeyMarker, p)
	out.NextKeyMarker = strings.TrimPrefix(out.NextKeyMarker, p)
	for i := range out.Uploads {
		out.Uploads[i].Key = strings.TrimPrefix(out.Uploads[i].Key, p)
		out.Uploads[i].Owner = s3response.Owner{}
		out.Uploads[i].Initiator = s3response.Initiator{}
	}
	for i := range out.CommonPrefixes {
		out.CommonPrefixes[i].Prefix = strings.TrimPrefix(out.CommonPrefixes[i].Prefix, p)
	}
	return out, err
}

func (b *Backend) GetObjectLockConfiguration(ctx context.Context, bucket string) ([]byte, error) {
	return nil, s3err.GetAPIError(s3err.ErrObjectLockConfigurationNotFound)
}
func (b *Backend) GetObjectRetention(ctx context.Context, bucket, key, version string) ([]byte, error) {
	return nil, s3err.GetAPIError(s3err.ErrNoSuchObjectLockConfiguration)
}
func (b *Backend) GetObjectLegalHold(ctx context.Context, bucket, key, version string) (*bool, error) {
	return nil, s3err.GetAPIError(s3err.ErrNoSuchObjectLockConfiguration)
}
