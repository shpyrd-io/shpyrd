package objectgateway

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
	"github.com/versity/versitygw/auth"
)

// fakeS3 is one bucket in memory: enough of the S3 API for descriptors
// (conditional GET/PUT), listings and batched deletes, and it counts what
// the gateway asks of it.
type fakeS3 struct {
	mu          sync.Mutex
	objects     map[string]string // key -> body
	etags       map[string]string
	seq         int
	gets        map[string]int
	lists       int
	listFail    bool
	batches     []int // sizes of each multi-object delete
	singleDeles int
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: map[string]string{}, etags: map[string]string{}, gets: map[string]int{}}
}

func (f *fakeS3) put(key, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	f.objects[key] = body
	f.etags[key] = fmt.Sprintf("etag-%d", f.seq)
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/"), "test/")
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodGet && (key == "" || key == "/") && q.Has("list-type"):
		f.lists++
		if f.listFail {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>no</Message></Error>`)
			return
		}
		var keys []string
		for k := range f.objects {
			if strings.HasPrefix(k, q.Get("prefix")) && k > q.Get("continuation-token") {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		truncated := len(keys) > 1000
		if truncated {
			keys = keys[:1000]
		}
		var b strings.Builder
		b.WriteString(`<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>test</Name><MaxKeys>1000</MaxKeys>`)
		fmt.Fprintf(&b, "<KeyCount>%d</KeyCount><IsTruncated>%t</IsTruncated>", len(keys), truncated)
		if truncated {
			fmt.Fprintf(&b, "<NextContinuationToken>%s</NextContinuationToken>", keys[len(keys)-1])
		}
		for _, k := range keys {
			fmt.Fprintf(&b, `<Contents><Key>%s</Key><LastModified>2024-01-01T00:00:00.000Z</LastModified><ETag>"%s"</ETag><Size>%d</Size></Contents>`, k, f.etags[k], len(f.objects[k]))
		}
		b.WriteString("</ListBucketResult>")
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, b.String())
	case r.Method == http.MethodPost && q.Has("delete"):
		var in struct {
			Objects []struct{ Key string } `xml:"Object"`
		}
		body, _ := io.ReadAll(r.Body)
		if err := xml.Unmarshal(body, &in); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.batches = append(f.batches, len(in.Objects))
		for _, o := range in.Objects {
			delete(f.objects, o.Key)
			delete(f.etags, o.Key)
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<DeleteResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></DeleteResult>`)
	case r.Method == http.MethodDelete:
		f.singleDeles++
		delete(f.objects, key)
		delete(f.etags, key)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet:
		f.gets[key]++
		body, ok := f.objects[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `<Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`)
			return
		}
		w.Header().Set("ETag", `"`+f.etags[key]+`"`)
		w.Header().Set("Last-Modified", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Format(http.TimeFormat))
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		fmt.Fprint(w, body)
	case r.Method == http.MethodPut:
		_, exists := f.objects[key]
		if r.Header.Get("If-None-Match") == "*" && exists {
			w.WriteHeader(http.StatusPreconditionFailed)
			fmt.Fprint(w, `<Error><Code>PreconditionFailed</Code></Error>`)
			return
		}
		if m := strings.Trim(r.Header.Get("If-Match"), `"`); m != "" && m != f.etags[key] {
			w.WriteHeader(http.StatusPreconditionFailed)
			fmt.Fprint(w, `<Error><Code>PreconditionFailed</Code></Error>`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
			body = unchunk(body)
		}
		f.seq++
		f.objects[key] = string(body)
		f.etags[key] = fmt.Sprintf("etag-%d", f.seq)
		w.Header().Set("ETag", `"`+f.etags[key]+`"`)
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

// unchunk undoes the client's streaming-signature framing over plain HTTP:
// "<hex size>;chunk-signature=...\r\n<data>\r\n" repeated, a zero chunk last.
func unchunk(body []byte) []byte {
	var out []byte
	rest := string(body)
	for {
		nl := strings.Index(rest, "\r\n")
		if nl < 0 {
			return out
		}
		var size int
		if _, err := fmt.Sscanf(strings.SplitN(rest[:nl], ";", 2)[0], "%x", &size); err != nil || size == 0 {
			return out
		}
		rest = rest[nl+2:]
		if len(rest) < size {
			return out
		}
		out = append(out, rest[:size]...)
		rest = strings.TrimPrefix(rest[size:], "\r\n")
	}
}

func (f *fakeS3) getsOf(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gets[key]
}

func (f *fakeS3) keysWithPrefix(p string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for k := range f.objects {
		if strings.HasPrefix(k, p) {
			n++
		}
	}
	return n
}

func testRecords(t *testing.T) (*Records, *fakeS3) {
	t.Helper()
	fake := newFakeS3()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	store, err := objectstore.NewS3(srv.URL, "us-east-1", "test", "", "test", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	return &Records{Store: store}, fake
}

func descriptorKey(name string) string { return metadataPrefix + name + ".json" }

func TestAuthenticationRemembersDescriptors(t *testing.T) {
	records, fake := testRecords(t)
	clock := time.Date(2026, 10, 10, 22, 0, 0, 0, time.UTC)
	records.cache = newDescriptorCache(descriptorCacheSize, descriptorTTL, func() time.Time { return clock })
	ctx := context.Background()
	iam := &IAM{Records: records}
	if err := records.Ensure(ctx, objectstore.BucketSpec{Name: "consumer-a"}); err != nil {
		t.Fatal(err)
	}
	cred, err := records.Credential(ctx, "consumer-a", "", "")
	if err != nil {
		t.Fatal(err)
	}
	key := descriptorKey("consumer-a")
	before := fake.getsOf(key)

	// A hit: many requests, one GET.
	for i := 0; i < 5; i++ {
		acct, err := iam.GetUserAccount("consumer-a")
		if err != nil {
			t.Fatal(err)
		}
		if acct.Secret != cred.SecretKey {
			t.Fatalf("secret %q, want %q", acct.Secret, cred.SecretKey)
		}
	}
	if got := fake.getsOf(key) - before; got != 1 {
		t.Fatalf("5 authentications cost %d provider reads, want 1", got)
	}

	// Expiry: after the TTL the descriptor is read again.
	clock = clock.Add(descriptorTTL + time.Second)
	if _, err := iam.GetUserAccount("consumer-a"); err != nil {
		t.Fatal(err)
	}
	if got := fake.getsOf(key) - before; got != 2 {
		t.Fatalf("after the TTL %d provider reads, want 2", got)
	}

	// Invalidation: a revocation on this replica is in force at once.
	before = fake.getsOf(key)
	if err := records.Revoke(ctx, "consumer-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := iam.GetUserAccount("consumer-a"); !errors.Is(err, auth.ErrNoSuchUser) {
		t.Fatalf("revoked credential authenticates: %v", err)
	}
	if got := fake.getsOf(key) - before; got != 2 { // Revoke's own read, then authentication's
		t.Fatalf("after revocation %d provider reads, want 2", got)
	}

	// A new credential after revocation is also seen at once.
	cred, err = records.Credential(ctx, "consumer-a", "", "")
	if err != nil {
		t.Fatal(err)
	}
	acct, err := iam.GetUserAccount("consumer-a")
	if err != nil || acct.Secret != cred.SecretKey {
		t.Fatalf("re-issued credential: %v, secret matches %t", err, acct.Secret == cred.SecretKey)
	}
}

func TestAuthenticationRemembersUnknownKeysButNotFailures(t *testing.T) {
	records, fake := testRecords(t)
	iam := &IAM{Records: records}
	for i := 0; i < 3; i++ {
		if _, err := iam.GetUserAccount("nobody"); !errors.Is(err, auth.ErrNoSuchUser) {
			t.Fatalf("unknown key: %v", err)
		}
	}
	if got := fake.getsOf(descriptorKey("nobody")); got != 1 {
		t.Fatalf("3 unknown-key authentications cost %d provider reads, want 1", got)
	}
	// Creating the consumer on this replica makes it visible at once.
	ctx := context.Background()
	if err := records.Ensure(ctx, objectstore.BucketSpec{Name: "nobody"}); err != nil {
		t.Fatal(err)
	}
	if _, err := records.Credential(ctx, "nobody", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := iam.GetUserAccount("nobody"); err != nil {
		t.Fatalf("new consumer not seen: %v", err)
	}
	// A broken descriptor is a transient failure, never remembered.
	fake.put(descriptorKey("broken"), "{not json")
	for i := 0; i < 2; i++ {
		if _, err := iam.GetUserAccount("broken"); errors.Is(err, auth.ErrNoSuchUser) || err == nil {
			t.Fatalf("broken descriptor: %v", err)
		}
	}
	if got := fake.getsOf(descriptorKey("broken")); got != 2 {
		t.Fatalf("failures were remembered: %d reads, want 2", got)
	}
}

func TestDescriptorCacheIsBounded(t *testing.T) {
	c := newDescriptorCache(2, time.Minute, nil)
	c.remember("a", Record{Name: "a"}, nil)
	c.remember("b", Record{Name: "b"}, nil)
	if _, _, ok := c.lookup("a"); !ok { // a is now the most recent
		t.Fatal("a missing")
	}
	c.remember("c", Record{Name: "c"}, nil)
	if _, _, ok := c.lookup("b"); ok {
		t.Fatal("least recently used entry survived")
	}
	if _, _, ok := c.lookup("a"); !ok {
		t.Fatal("recently used entry evicted")
	}
	if c.recent.Len() != 2 || len(c.entries) != 2 {
		t.Fatalf("size %d/%d, want 2", c.recent.Len(), len(c.entries))
	}
	c.forget("a")
	if _, _, ok := c.lookup("a"); ok {
		t.Fatal("forgotten entry found")
	}
}
