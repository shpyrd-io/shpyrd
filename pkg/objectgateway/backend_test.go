package objectgateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/versity/versitygw/backend/s3proxy"
	"github.com/versity/versitygw/s3response"
)

// unseekable is what the front door hands the backend: a body that can only
// be read once, as the chunk decoders are.
type unseekable struct{ r io.Reader }

func (u unseekable) Read(p []byte) (int, error) { return u.r.Read(p) }

func TestWithoutAWSChunked(t *testing.T) {
	cases := map[string]*string{
		"aws-chunked":             nil,
		"AWS-CHUNKED":             nil,
		"":                        nil,
		"aws-chunked, gzip":       aws.String("gzip"),
		"gzip,aws-chunked":        aws.String("gzip"),
		"gzip, br":                aws.String("gzip, br"),
		"identity":                aws.String("identity"),
		" aws-chunked ,  gzip , ": aws.String("gzip"),
	}
	for in, want := range cases {
		got := withoutAWSChunked(aws.String(in))
		if aws.ToString(got) != aws.ToString(want) || (got == nil) != (want == nil) {
			t.Errorf("withoutAWSChunked(%q) = %v, want %v", in, aws.ToString(got), aws.ToString(want))
		}
	}
	if withoutAWSChunked(nil) != nil {
		t.Error("nil must stay nil")
	}
}

// standInProvider is an S3 endpoint that records the headers of the requests
// the backend forwards and answers them well enough for the proxy.
type standInProvider struct {
	mu   sync.Mutex
	seen []http.Header
}

func (p *standInProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.seen = append(p.seen, r.Header.Clone())
	p.mu.Unlock()
	_, _ = io.Copy(io.Discard, r.Body)
	switch {
	case r.Method == http.MethodPost && r.URL.Query().Has("uploads"):
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<InitiateMultipartUploadResult><Bucket>phys</Bucket><Key>k</Key><UploadId>upload-1</UploadId></InitiateMultipartUploadResult>`)
	case r.Header.Get("X-Amz-Copy-Source") != "" && r.URL.Query().Has("partNumber"):
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<CopyPartResult><ETag>"d41d8cd98f00b204e9800998ecf8427e"</ETag><LastModified>2026-10-10T00:00:00.000Z</LastModified></CopyPartResult>`)
	case r.Header.Get("X-Amz-Copy-Source") != "":
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<CopyObjectResult><ETag>"d41d8cd98f00b204e9800998ecf8427e"</ETag><LastModified>2026-10-10T00:00:00.000Z</LastModified></CopyObjectResult>`)
	default:
		w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
	}
}

func (p *standInProvider) last() http.Header {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seen[len(p.seen)-1]
}

func standInBackend(t *testing.T) (*Backend, *standInProvider) {
	t.Helper()
	provider := &standInProvider{}
	srv := httptest.NewServer(provider)
	t.Cleanup(srv.Close)
	// The same options the gateway runs with (server.go), so the SDK's own
	// checksum behaviour in the test is the production one.
	client := s3.New(s3.Options{
		BaseEndpoint: aws.String(srv.URL), Region: "us-east-1", UsePathStyle: true,
		Credentials:                credentials.NewStaticCredentialsProvider("test", "test-only-secret", ""),
		Retryer:                    aws.NopRetryer{},
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	proxy, err := s3proxy.NewWithClient(context.Background(), client, "")
	if err != nil {
		t.Fatal(err)
	}
	return &Backend{proxy: proxy, physical: "phys"}, provider
}

// The front door decodes an aws-chunked body before the backend runs; the
// provider must never be told the body is still chunked, or it refuses the
// upload (OCI and AWS answer "x-amz-content-sha256 must be STREAMING-...").
func TestBackendDropsAWSChunkedEncoding(t *testing.T) {
	b, provider := standInBackend(t)
	ctx := context.Background()
	_, err := b.PutObject(ctx, s3response.PutObjectInput{
		Bucket: aws.String("consumer-a"), Key: aws.String("sources/x.tgz"),
		Body: strings.NewReader("hello"), ContentLength: aws.Int64(5),
		ContentEncoding: aws.String("aws-chunked"), ContentType: aws.String("application/gzip"),
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := provider.last().Get("Content-Encoding"); got != "" {
		t.Errorf("PutObject forwarded Content-Encoding %q", got)
	}
	if got := provider.last().Get("Content-Type"); got != "application/gzip" {
		t.Errorf("PutObject lost the Content-Type: %q", got)
	}

	_, err = b.PutObject(ctx, s3response.PutObjectInput{
		Bucket: aws.String("consumer-a"), Key: aws.String("sources/y.tgz"),
		Body: strings.NewReader("hello"), ContentLength: aws.Int64(5),
		ContentEncoding: aws.String("aws-chunked, gzip"),
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := provider.last().Get("Content-Encoding"); got != "gzip" {
		t.Errorf("PutObject must keep the object's own encoding, got %q", got)
	}

	_, err = b.CreateMultipartUpload(ctx, s3response.CreateMultipartUploadInput{
		Bucket: aws.String("consumer-a"), Key: aws.String("sources/z.tgz"),
		ContentEncoding: aws.String("aws-chunked"),
	})
	if err != nil {
		t.Fatalf("create multipart: %v", err)
	}
	if got := provider.last().Get("Content-Encoding"); got != "" {
		t.Errorf("CreateMultipartUpload forwarded Content-Encoding %q", got)
	}
}

// A consumer that uploaded with a trailing checksum reaches the backend with
// the algorithm set and no value (the front door consumed the trailer). The
// provider must not be asked to compute one: that re-frames the upload as
// aws-chunked, and on a plain-HTTP provider the SDK refuses the unseekable
// body outright.
func TestBackendDropsAnAlgorithmWithoutAValue(t *testing.T) {
	b, provider := standInBackend(t)
	ctx := context.Background()
	_, err := b.PutObject(ctx, s3response.PutObjectInput{
		Bucket: aws.String("consumer-a"), Key: aws.String("sources/t.tgz"),
		Body: unseekable{strings.NewReader("hello")}, ContentLength: aws.Int64(5),
		ChecksumAlgorithm: types.ChecksumAlgorithmCrc32c,
	})
	if err != nil {
		t.Fatalf("put with a trailer-derived algorithm: %v", err)
	}
	h := provider.last()
	if h.Get("Content-Encoding") != "" || h.Get("X-Amz-Trailer") != "" || h.Get("X-Amz-Sdk-Checksum-Algorithm") != "" {
		t.Errorf("PutObject re-framed the upload: Content-Encoding=%q X-Amz-Trailer=%q X-Amz-Sdk-Checksum-Algorithm=%q", h.Get("Content-Encoding"), h.Get("X-Amz-Trailer"), h.Get("X-Amz-Sdk-Checksum-Algorithm"))
	}

	// A value the consumer supplied in a header stays, for the provider to check.
	_, err = b.PutObject(ctx, s3response.PutObjectInput{
		Bucket: aws.String("consumer-a"), Key: aws.String("sources/u.tgz"),
		Body: unseekable{strings.NewReader("hello")}, ContentLength: aws.Int64(5),
		ChecksumAlgorithm: types.ChecksumAlgorithmCrc32c, ChecksumCRC32C: aws.String("mnG7TA=="),
	})
	if err != nil {
		t.Fatalf("put with a checksum value: %v", err)
	}
	if got := provider.last().Get("X-Amz-Checksum-Crc32c"); got != "mnG7TA==" {
		t.Errorf("a supplied checksum value must reach the provider, got %q", got)
	}

	_, err = b.UploadPart(ctx, &s3.UploadPartInput{
		Bucket: aws.String("consumer-a"), Key: aws.String("sources/v.tgz"), UploadId: aws.String("upload-1"), PartNumber: aws.Int32(1),
		Body: unseekable{strings.NewReader("hello")}, ContentLength: aws.Int64(5),
		ChecksumAlgorithm: types.ChecksumAlgorithmCrc32c,
	})
	if err != nil {
		t.Fatalf("upload part with a trailer-derived algorithm: %v", err)
	}
	h = provider.last()
	if h.Get("Content-Encoding") != "" || h.Get("X-Amz-Trailer") != "" {
		t.Errorf("UploadPart re-framed the upload: Content-Encoding=%q X-Amz-Trailer=%q", h.Get("Content-Encoding"), h.Get("X-Amz-Trailer"))
	}
}

// The copy paths carry what the front door made up about owners and what
// the consumer said about the provider's storage class; neither is the
// provider's business.
func TestBackendCopiesWithoutOwnersOrStorageClass(t *testing.T) {
	b, provider := standInBackend(t)
	ctx := context.Background()
	_, err := b.CopyObject(ctx, s3response.CopyObjectInput{
		Bucket: aws.String("consumer-a"), Key: aws.String("sources/copy.tgz"), CopySource: aws.String("consumer-a/sources/x.tgz"),
		ExpectedBucketOwner: aws.String("CONSUMERACCESSKEY"), ExpectedSourceBucketOwner: aws.String("123456789012"),
		StorageClass: types.StorageClassGlacier, ContentEncoding: aws.String("aws-chunked"), MetadataDirective: types.MetadataDirectiveReplace,
	})
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	h := provider.last()
	for _, name := range []string{"X-Amz-Expected-Bucket-Owner", "X-Amz-Source-Expected-Bucket-Owner", "X-Amz-Storage-Class", "Content-Encoding"} {
		if got := h.Get(name); got != "" {
			t.Errorf("CopyObject forwarded %s=%q", name, got)
		}
	}
	if got := h.Get("X-Amz-Copy-Source"); !strings.Contains(got, "buckets%2Fconsumer-a%2Fsources%2Fx.tgz") {
		t.Errorf("copy source not mapped to the physical layout: %q", got)
	}

	_, err = b.UploadPartCopy(ctx, &s3.UploadPartCopyInput{
		Bucket: aws.String("consumer-a"), Key: aws.String("sources/big.tgz"), UploadId: aws.String("upload-1"), PartNumber: aws.Int32(1),
		CopySource:          aws.String("consumer-a/sources/x.tgz"),
		ExpectedBucketOwner: aws.String("CONSUMERACCESSKEY"), ExpectedSourceBucketOwner: aws.String("123456789012"),
	})
	if err != nil {
		t.Fatalf("upload part copy: %v", err)
	}
	h = provider.last()
	for _, name := range []string{"X-Amz-Expected-Bucket-Owner", "X-Amz-Source-Expected-Bucket-Owner"} {
		if got := h.Get(name); got != "" {
			t.Errorf("UploadPartCopy forwarded %s=%q", name, got)
		}
	}
}
