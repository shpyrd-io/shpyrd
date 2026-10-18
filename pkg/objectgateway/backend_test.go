package objectgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
	"github.com/versity/versitygw/auth"
	"github.com/versity/versitygw/backend/s3proxy"
	"github.com/versity/versitygw/s3err"
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
// the backend forwards and answers them well enough for the proxy; it
// remembers what each object weighs, for HEAD.
type standInProvider struct {
	mu    sync.Mutex
	seen  []http.Header
	sizes map[string]int64
}

func (p *standInProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.seen = append(p.seen, r.Header.Clone())
	if p.sizes == nil {
		p.sizes = map[string]int64{}
	}
	key := strings.TrimPrefix(r.URL.Path, "/")
	n, _ := io.Copy(io.Discard, r.Body)
	if r.Method == http.MethodPut && r.Header.Get("X-Amz-Copy-Source") == "" {
		p.sizes[key] = n
	}
	size, known := p.sizes[key]
	p.mu.Unlock()
	switch {
	case r.Method == http.MethodHead:
		if !known {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(size))
		w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
	case r.Method == http.MethodDelete:
		p.mu.Lock()
		delete(p.sizes, key)
		p.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
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
	// The descriptors live elsewhere (a fake of their own); consumer-a
	// exists with no cap, consumer-b holds up to 8 bytes.
	records, _ := testRecords(t)
	for _, spec := range []objectstore.BucketSpec{{Name: "consumer-a"}, {Name: "consumer-b", MaxBytes: 8}} {
		if err := records.Ensure(context.Background(), spec); err != nil {
			t.Fatal(err)
		}
	}
	return &Backend{proxy: proxy, physical: "phys", records: records}, provider
}

// The cap (storage plan step 5): an upload that would pass it is refused
// with the sentence, before the provider sees a byte; what is deleted is
// counted out at once; the counter reaches the record on a flush.
func TestTheCapRefusesAnUploadThatWouldPassIt(t *testing.T) {
	b, provider := standInBackend(t)
	ctx := context.Background()
	put := func(key, body string) error {
		_, err := b.PutObject(ctx, s3response.PutObjectInput{Bucket: aws.String("consumer-b"), Key: aws.String(key), Body: strings.NewReader(body), ContentLength: aws.Int64(int64(len(body)))})
		return err
	}
	if err := put("one", "12345"); err != nil {
		t.Fatalf("5 of 8 bytes: %v", err)
	}
	seen := len(provider.seen)
	err := put("two", "12345")
	var apiErr s3err.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "QuotaExceeded" || apiErr.Description != "This bucket holds up to 8 bytes; this upload would bring it to 10 bytes." {
		t.Fatalf("10 of 8 bytes = %v", err)
	}
	if len(provider.seen) != seen {
		t.Error("a refused upload reached the provider")
	}
	if err := put("two", "123"); err != nil {
		t.Fatalf("8 of 8 bytes: %v", err)
	}
	// Room freed is room usable: the delete counts the object out.
	if _, err := b.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String("consumer-b"), Key: aws.String("one")}); err != nil {
		t.Fatal(err)
	}
	if err := put("three", "12345"); err != nil {
		t.Fatalf("after a delete: %v", err)
	}
	// The counter is this replica's until the flush writes it.
	if d := b.records.pendingDelta("consumer-b"); d.bytes != 8 || d.objects != 2 {
		t.Errorf("pending = %+v, want 8 bytes in 2 objects", d)
	}
	if err := b.records.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	rec, err := b.records.Get(ctx, "consumer-b")
	if err != nil || rec.UsedBytes != 8 || rec.Objects != 2 {
		t.Errorf("record after the flush = %+v, %v", rec, err)
	}
	if d := b.records.pendingDelta("consumer-b"); d != (delta{}) {
		t.Errorf("pending after the flush = %+v", d)
	}
	usage, err := b.records.Usage(ctx, "consumer-b")
	if err != nil || usage.Buckets["consumer-b"].Bytes != 8 || usage.Buckets["consumer-b"].Objects != 2 {
		t.Errorf("usage from the counter = %+v, %v", usage, err)
	}
	// A bucket without a cap takes anything; a bucket unknown to the
	// records is denied before the provider.
	big := strings.Repeat("x", 64)
	if _, err := b.PutObject(ctx, s3response.PutObjectInput{Bucket: aws.String("consumer-a"), Key: aws.String("big"), Body: strings.NewReader(big), ContentLength: aws.Int64(int64(len(big)))}); err != nil {
		t.Errorf("no cap: %v", err)
	}
	if _, err := b.PutObject(ctx, s3response.PutObjectInput{Bucket: aws.String("nobody"), Key: aws.String("x"), Body: strings.NewReader("x"), ContentLength: aws.Int64(1)}); !errors.Is(err, s3err.GetAPIError(s3err.ErrAccessDenied)) {
		t.Errorf("unknown bucket = %v", err)
	}
}

// A public bucket has a policy that opens its objects to anyone and
// nothing more; one that is not has none. CORS is the consumer's, kept on
// the record. Both come from the authentication cache.
func TestAPublicBucketOpensItsObjectsAndNothingElse(t *testing.T) {
	b, _ := standInBackend(t)
	ctx := context.Background()
	if _, err := b.GetBucketPolicy(ctx, "consumer-a"); !errors.Is(err, s3err.GetAPIError(s3err.ErrNoSuchBucketPolicy)) {
		t.Errorf("a private bucket's policy = %v", err)
	}
	if err := b.records.Ensure(ctx, objectstore.BucketSpec{Name: "consumer-a", Public: true}); err != nil {
		t.Fatal(err)
	}
	policy, err := b.GetBucketPolicy(ctx, "consumer-a")
	if err != nil {
		t.Fatal(err)
	}
	var parsed auth.BucketPolicy
	if err := json.Unmarshal(policy, &parsed); err != nil {
		t.Fatalf("the policy does not parse: %v\n%s", err, policy)
	}
	if !parsed.IsPublic() || len(parsed.Statement) != 2 {
		t.Fatalf("policy = %s", policy)
	}
	// The owner keeps everything; anyone else reads an object and nothing more.
	owner, anyone := parsed.Statement[0], parsed.Statement[1]
	_, ownerIsAnyone := owner.Principals["*"]
	_, anyoneIsAnyone := anyone.Principals["*"]
	if ownerIsAnyone || !owner.Actions.FindMatch(auth.ListBucketAction) || !owner.Actions.FindMatch(auth.PutObjectAction) || !owner.Resources.FindMatch("consumer-a", b.NormalizeObjectKey) || !owner.Resources.FindMatch("consumer-a/some/key", b.NormalizeObjectKey) {
		t.Errorf("the owner's statement = %+v", owner)
	}
	if !anyoneIsAnyone || !anyone.Actions.FindMatch(auth.GetObjectAction) || anyone.Actions.FindMatch(auth.ListBucketAction) || anyone.Actions.FindMatch(auth.PutObjectAction) {
		t.Errorf("the public statement = %+v", anyone)
	}
	// The owner's principal is its access key, which exists once a
	// credential was issued.
	if _, err := b.records.Credential(ctx, "consumer-a", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := parsed.Validate("consumer-a", &IAM{Records: b.records}); err != nil {
		t.Errorf("the policy does not validate: %v", err)
	}
	if _, err := b.GetBucketCors(ctx, "consumer-a"); !errors.Is(err, s3err.GetAPIError(s3err.ErrNoSuchCORSConfiguration)) {
		t.Errorf("no CORS yet = %v", err)
	}
	cors := []byte(`<CORSConfiguration><CORSRule><AllowedOrigin>https://shop.example</AllowedOrigin><AllowedMethod>PUT</AllowedMethod></CORSRule></CORSConfiguration>`)
	if err := b.PutBucketCors(ctx, "consumer-a", cors); err != nil {
		t.Fatal(err)
	}
	if got, err := b.GetBucketCors(ctx, "consumer-a"); err != nil || string(got) != string(cors) {
		t.Errorf("CORS = %s, %v", got, err)
	}
	if err := b.DeleteBucketCors(ctx, "consumer-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.GetBucketCors(ctx, "consumer-a"); !errors.Is(err, s3err.GetAPIError(s3err.ErrNoSuchCORSConfiguration)) {
		t.Errorf("CORS after its removal = %v", err)
	}
	if err := b.PutBucketCors(ctx, "consumer-a", bytes.Repeat([]byte("x"), maxCORS+1)); !errors.Is(err, s3err.GetAPIError(s3err.ErrMalformedXML)) {
		t.Errorf("a CORS past the bound = %v", err)
	}
}

// A bucket restored into another project serves the objects of the name
// they were written under (storage plan step 5): its data operations
// resolve to the alias's prefix, its usage measures that prefix, and
// deleting it takes only its key.
func TestAnAliasServesAnotherBucketsObjects(t *testing.T) {
	b, provider := standInBackend(t)
	ctx := context.Background()
	if err := b.records.Ensure(ctx, objectstore.BucketSpec{Name: "consumer-c", Alias: "consumer-a"}); err != nil {
		t.Fatal(err)
	}
	if err := b.records.Ensure(ctx, objectstore.BucketSpec{Name: "consumer-c", Alias: "consumer-b"}); err == nil {
		t.Error("an alias changed")
	}
	if _, err := b.PutObject(ctx, s3response.PutObjectInput{Bucket: aws.String("consumer-c"), Key: aws.String("shared/file"), Body: strings.NewReader("hello"), ContentLength: aws.Int64(5)}); err != nil {
		t.Fatal(err)
	}
	if _, known := provider.sizes["phys/buckets/consumer-a/shared/file"]; !known {
		t.Errorf("the object went to %v, want consumer-a's prefix", provider.sizes)
	}
	if _, err := b.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("consumer-a"), Key: aws.String("shared/file")}); err != nil {
		t.Errorf("the first project reads what the second wrote: %v", err)
	}
	if err := b.records.Delete(ctx, "consumer-c"); err != nil {
		t.Fatal(err)
	}
	if _, known := provider.sizes["phys/buckets/consumer-a/shared/file"]; !known {
		t.Error("deleting the alias emptied the prefix it served")
	}
	rec, err := b.records.Get(ctx, "consumer-c")
	if err != nil || !rec.Disabled {
		t.Errorf("the alias after its deletion = %+v, %v", rec, err)
	}
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
