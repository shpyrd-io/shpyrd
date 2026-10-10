// Package objectgateway implements isolated logical S3 buckets over a shared
// provider bucket. Only the explicitly implemented data operations can reach
// the provider; bucket administration is intentionally unsupported.
package objectgateway

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

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
func safeKey(key string) bool {
	for _, part := range strings.Split(key, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return !strings.ContainsRune(key, '\x00')
}
func (b *Backend) mapKey(bucket, key **string) error {
	if *bucket == nil || !validName(**bucket) || *key == nil || !safeKey(**key) {
		return denied()
	}
	*key = aws.String(prefix(**bucket) + **key)
	*bucket = aws.String(b.physical)
	return nil
}
func (b *Backend) GetBucketAcl(ctx context.Context, in *s3.GetBucketAclInput) ([]byte, error) {
	r, err := b.records.Get(ctx, aws.ToString(in.Bucket))
	if err != nil {
		return nil, err
	}
	return json.Marshal(auth.ACL{Owner: r.Name, Grantees: []auth.Grantee{{Access: r.Name, Permission: auth.PermissionFullControl, Type: types.TypeCanonicalUser}}})
}
func (b *Backend) GetBucketPolicy(ctx context.Context, bucket string) ([]byte, error) {
	return nil, s3err.GetAPIError(s3err.ErrNoSuchBucketPolicy)
}
func (b *Backend) GetBucketOwnershipControls(ctx context.Context, bucket string) (types.ObjectOwnership, error) {
	return types.ObjectOwnershipBucketOwnerEnforced, nil
}
func (b *Backend) HeadBucket(ctx context.Context, in *s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
	_, err := b.records.Get(ctx, aws.ToString(in.Bucket))
	return &s3.HeadBucketOutput{}, err
}
func (b *Backend) ListBuckets(ctx context.Context, in s3response.ListBucketsInput) (s3response.ListAllMyBucketsResult, error) {
	out := s3response.ListAllMyBucketsResult{Owner: s3response.CanonicalUser{ID: in.Owner}}
	r, err := b.records.Get(ctx, in.Owner)
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

func (b *Backend) PutObject(ctx context.Context, in s3response.PutObjectInput) (s3response.PutObjectOutput, error) {
	in.GrantFullControl = nil
	in.GrantRead = nil
	in.GrantReadACP = nil
	in.GrantWriteACP = nil
	in.ContentEncoding = withoutAWSChunked(in.ContentEncoding)
	if err := b.mapKey(&in.Bucket, &in.Key); err != nil {
		return s3response.PutObjectOutput{}, err
	}
	return b.proxy.PutObject(ctx, in)
}
func (b *Backend) HeadObject(ctx context.Context, in *s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
	if err := b.mapKey(&in.Bucket, &in.Key); err != nil {
		return nil, err
	}
	return b.proxy.HeadObject(ctx, in)
}
func (b *Backend) GetObject(ctx context.Context, in *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
	if err := b.mapKey(&in.Bucket, &in.Key); err != nil {
		return nil, err
	}
	return b.proxy.GetObject(ctx, in)
}
func (b *Backend) DeleteObject(ctx context.Context, in *s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error) {
	if err := b.mapKey(&in.Bucket, &in.Key); err != nil {
		return nil, err
	}
	return b.proxy.DeleteObject(ctx, in)
}
func (b *Backend) UploadPart(ctx context.Context, in *s3.UploadPartInput) (*s3.UploadPartOutput, error) {
	if err := b.mapKey(&in.Bucket, &in.Key); err != nil {
		return nil, err
	}
	return b.proxy.UploadPart(ctx, in)
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
	if err := b.mapKey(&in.Bucket, &in.Key); err != nil {
		return s3response.InitiateMultipartUploadResult{}, err
	}
	out, err := b.proxy.CreateMultipartUpload(ctx, in)
	out.Bucket = bucket
	out.Key = key
	return out, err
}
func (b *Backend) CompleteMultipartUpload(ctx context.Context, in *s3.CompleteMultipartUploadInput) (s3response.CompleteMultipartUploadResult, string, error) {
	bucket, key := in.Bucket, in.Key
	if err := b.mapKey(&in.Bucket, &in.Key); err != nil {
		return s3response.CompleteMultipartUploadResult{}, "", err
	}
	out, version, err := b.proxy.CompleteMultipartUpload(ctx, in)
	out.Bucket = bucket
	out.Key = key
	out.Location = nil
	return out, version, err
}
func (b *Backend) AbortMultipartUpload(ctx context.Context, in *s3.AbortMultipartUploadInput) error {
	if err := b.mapKey(&in.Bucket, &in.Key); err != nil {
		return err
	}
	return b.proxy.AbortMultipartUpload(ctx, in)
}
func (b *Backend) ListParts(ctx context.Context, in *s3.ListPartsInput) (s3response.ListPartsResult, error) {
	bucket, key := aws.ToString(in.Bucket), aws.ToString(in.Key)
	if err := b.mapKey(&in.Bucket, &in.Key); err != nil {
		return s3response.ListPartsResult{}, err
	}
	out, err := b.proxy.ListParts(ctx, in)
	out.Bucket = bucket
	out.Key = key
	return out, err
}
func (b *Backend) copySource(bucket string, source *string) (*string, error) {
	raw, err := url.PathUnescape(aws.ToString(source))
	if err != nil {
		return nil, denied()
	}
	parts := strings.SplitN(strings.TrimPrefix(raw, "/"), "/", 2)
	if len(parts) != 2 || parts[0] != bucket || !safeKey(parts[1]) {
		return nil, denied()
	}
	return aws.String(url.PathEscape(b.physical + "/" + prefix(bucket) + parts[1])), nil
}
func (b *Backend) CopyObject(ctx context.Context, in s3response.CopyObjectInput) (s3response.CopyObjectOutput, error) {
	var err error
	in.CopySource, err = b.copySource(aws.ToString(in.Bucket), in.CopySource)
	if err != nil {
		return s3response.CopyObjectOutput{}, err
	}
	in.GrantFullControl = nil
	in.GrantRead = nil
	in.GrantReadACP = nil
	in.GrantWriteACP = nil
	if err = b.mapKey(&in.Bucket, &in.Key); err != nil {
		return s3response.CopyObjectOutput{}, err
	}
	return b.proxy.CopyObject(ctx, in)
}
func (b *Backend) UploadPartCopy(ctx context.Context, in *s3.UploadPartCopyInput) (s3response.CopyPartResult, error) {
	var err error
	in.CopySource, err = b.copySource(aws.ToString(in.Bucket), in.CopySource)
	if err != nil {
		return s3response.CopyPartResult{}, err
	}
	if err = b.mapKey(&in.Bucket, &in.Key); err != nil {
		return s3response.CopyPartResult{}, err
	}
	return b.proxy.UploadPartCopy(ctx, in)
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
	p := prefix(bucket)
	if !validName(bucket) || !safeKey(aws.ToString(in.Prefix)) {
		return s3response.ListObjectsResult{}, denied()
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
	p := prefix(bucket)
	if !validName(bucket) || !safeKey(aws.ToString(in.Prefix)) {
		return s3response.ListObjectsV2Result{}, denied()
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
	p := prefix(bucket)
	if !validName(bucket) || in.Delete == nil {
		return s3response.DeleteResult{}, denied()
	}
	for i := range in.Delete.Objects {
		if !safeKey(aws.ToString(in.Delete.Objects[i].Key)) {
			return s3response.DeleteResult{}, denied()
		}
		in.Delete.Objects[i].Key = aws.String(p + aws.ToString(in.Delete.Objects[i].Key))
	}
	in.Bucket = aws.String(b.physical)
	out, err := b.proxy.DeleteObjects(ctx, in)
	for i := range out.Deleted {
		out.Deleted[i].Key = strip(p, out.Deleted[i].Key)
	}
	for i := range out.Error {
		out.Error[i].Key = strip(p, out.Error[i].Key)
	}
	return out, err
}

func (b *Backend) GetBucketVersioning(ctx context.Context, bucket string) (s3response.GetBucketVersioningOutput, error) {
	return s3response.GetBucketVersioningOutput{}, nil
}
func (b *Backend) ListMultipartUploads(ctx context.Context, in *s3.ListMultipartUploadsInput) (s3response.ListMultipartUploadsResult, error) {
	bucket := aws.ToString(in.Bucket)
	p := prefix(bucket)
	if !validName(bucket) || !safeKey(aws.ToString(in.Prefix)) {
		return s3response.ListMultipartUploadsResult{}, denied()
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
