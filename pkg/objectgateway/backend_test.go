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
	"github.com/versity/versitygw/backend/s3proxy"
	"github.com/versity/versitygw/s3response"
)

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
	client := s3.New(s3.Options{
		BaseEndpoint: aws.String(srv.URL), Region: "us-east-1", UsePathStyle: true,
		Credentials: credentials.NewStaticCredentialsProvider("test", "test-only-secret", ""),
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
