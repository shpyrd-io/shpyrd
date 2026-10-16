package objectgateway

import (
	"context"
	"encoding/json"
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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/minio/minio-go/v7"
	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
	"github.com/versity/versitygw/auth"
)

// fakeS3 is one bucket in memory: enough of the S3 API for descriptors
// (conditional GET/PUT), listings and batched deletes, and it counts what
// the gateway asks of it. With ignoreIfMatch it behaves like OCI Object
// Storage, accepting a stale If-Match.
type fakeS3 struct {
	mu            sync.Mutex
	objects       map[string]string // key -> body
	etags         map[string]string
	history       map[string][]string // key -> every body PUT, in order
	seq           int
	gets          map[string]int
	lists         int
	listFail      bool
	ignoreIfMatch bool
	batches       []int // sizes of each multi-object delete
	singleDeles   int
	uploads       []fakeUpload // unfinished multipart uploads
	aborted       []string     // upload ids aborted
}

type fakeUpload struct {
	key, id   string
	initiated time.Time
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: map[string]string{}, etags: map[string]string{}, history: map[string][]string{}, gets: map[string]int{}}
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
	case r.Method == http.MethodGet && q.Has("uploads"):
		var b strings.Builder
		b.WriteString(`<ListMultipartUploadsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Bucket>test</Bucket><IsTruncated>false</IsTruncated>`)
		for _, u := range f.uploads {
			if strings.HasPrefix(u.key, q.Get("prefix")) {
				fmt.Fprintf(&b, `<Upload><Key>%s</Key><UploadId>%s</UploadId><Initiated>%s</Initiated></Upload>`, u.key, u.id, u.initiated.UTC().Format("2006-01-02T15:04:05.000Z"))
			}
		}
		b.WriteString("</ListMultipartUploadsResult>")
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, b.String())
	case r.Method == http.MethodDelete && q.Has("uploadId"):
		id := q.Get("uploadId")
		kept := f.uploads[:0]
		for _, u := range f.uploads {
			if u.id != id {
				kept = append(kept, u)
			}
		}
		f.uploads = kept
		f.aborted = append(f.aborted, id)
		w.WriteHeader(http.StatusNoContent)
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
		if m := strings.Trim(r.Header.Get("If-Match"), `"`); !f.ignoreIfMatch && m != "" && m != f.etags[key] {
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
		f.history[key] = append(f.history[key], string(body))
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

func TestDeleteEmptiesTheBucketInBatches(t *testing.T) {
	records, fake := testRecords(t)
	ctx := context.Background()
	if err := records.Ensure(ctx, objectstore.BucketSpec{Name: "consumer-a"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2500; i++ {
		fake.put(fmt.Sprintf("buckets/consumer-a/dir/%05d", i), "x")
	}
	fake.put("buckets/consumer-ab/kept", "x") // a neighbour whose name shares a prefix
	if err := records.Delete(ctx, "consumer-a"); err != nil {
		t.Fatal(err)
	}
	if n := fake.keysWithPrefix("buckets/consumer-a/"); n != 0 {
		t.Fatalf("%d objects left", n)
	}
	if n := fake.keysWithPrefix("buckets/consumer-ab/"); n != 1 {
		t.Fatal("neighbour deleted")
	}
	fake.mu.Lock()
	batches, singles := fake.batches, fake.singleDeles
	fake.mu.Unlock()
	if singles != 0 {
		t.Fatalf("%d objects deleted one by one", singles)
	}
	if len(batches) != 3 || batches[0] != 1000 || batches[1] != 1000 || batches[2] != 500 {
		t.Fatalf("batches %v, want [1000 1000 500]", batches)
	}
	rec, err := records.Get(ctx, "consumer-a")
	if err != nil || !rec.Disabled || rec.Secret != "" {
		t.Fatalf("tombstone %+v, %v", rec, err)
	}
}

func TestDeleteReportsAListingFailure(t *testing.T) {
	records, fake := testRecords(t)
	ctx := context.Background()
	if err := records.Ensure(ctx, objectstore.BucketSpec{Name: "consumer-a"}); err != nil {
		t.Fatal(err)
	}
	fake.put("buckets/consumer-a/one", "x")
	fake.mu.Lock()
	fake.listFail = true
	fake.mu.Unlock()
	if err := records.Delete(ctx, "consumer-a"); minio.ToErrorResponse(err).Code != "AccessDenied" {
		t.Fatalf("listing failure: %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.batches) != 0 || fake.singleDeles != 0 {
		t.Fatalf("deletes after a failed listing: %v, %d", fake.batches, fake.singleDeles)
	}
}

// On a provider that ignores If-Match (OCI Object Storage), a single
// writer's concurrent issues, revocations and retention changes of one
// descriptor lose no update: concurrent issues hand out one secret and write
// it once, a live secret is only ever replaced after a revocation, and every
// version written is consistent. Run with -race.
func TestSingleWriterSerializesDescriptorUpdates(t *testing.T) {
	records, fake := testRecords(t)
	records.SingleWriter = true
	fake.mu.Lock()
	fake.ignoreIfMatch = true
	fake.mu.Unlock()
	ctx := context.Background()
	if _, err := records.Preflight(ctx); err != nil {
		t.Fatal(err)
	}
	if err := records.Ensure(ctx, objectstore.BucketSpec{Name: "consumer-a"}); err != nil {
		t.Fatal(err)
	}
	key := descriptorKey("consumer-a")
	versions := func() []Record {
		t.Helper()
		fake.mu.Lock()
		defer fake.mu.Unlock()
		var out []Record
		for i, body := range fake.history[key] {
			var rec Record
			if err := json.Unmarshal([]byte(body), &rec); err != nil {
				t.Fatalf("version %d: %v", i, err)
			}
			out = append(out, rec)
		}
		return out
	}

	// Eight concurrent issues: one secret, written once.
	var wg sync.WaitGroup
	secrets := make(chan string, 8)
	errs := make(chan error, 64)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cred, err := records.Credential(ctx, "consumer-a", "", "")
			if err != nil {
				errs <- err
				return
			}
			secrets <- cred.SecretKey
		}()
	}
	wg.Wait()
	close(secrets)
	first := <-secrets
	for s := range secrets {
		if s != first {
			t.Fatal("concurrent issues handed out different secrets")
		}
	}
	if v := versions(); len(v) != 2 || v[1].Secret != first {
		t.Fatalf("%d versions after eight concurrent issues, want the creation and one secret", len(v))
	}

	// Issues, revocations and retention changes together.
	for i := 0; i < 8; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			if _, err := records.Credential(ctx, "consumer-a", "", ""); err != nil {
				errs <- err
			}
		}()
		go func(days int32) {
			defer wg.Done()
			if err := records.Ensure(ctx, objectstore.BucketSpec{Name: "consumer-a", RetentionDays: days}); err != nil {
				errs <- err
			}
		}(int32(i + 1))
		go func() {
			defer wg.Done()
			if err := records.Revoke(ctx, "consumer-a"); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	var prev Record
	for i, rec := range versions() {
		if rec.Disabled != (rec.Secret == "") {
			t.Errorf("version %d is inconsistent: %+v", i, rec)
		}
		if i > 0 && prev.Secret != "" && rec.Secret != "" && rec.Secret != prev.Secret {
			t.Errorf("version %d replaced a live secret: an update was lost", i)
		}
		prev = rec
	}
	rec, err := records.Get(ctx, "consumer-a")
	if err != nil || rec.Disabled != (rec.Secret == "") || rec.RetentionDays < 1 || rec.RetentionDays > 8 {
		t.Fatalf("final descriptor %+v, %v", rec, err)
	}
}

// The sweep measures every bucket (the listing corrects what a lost
// flush left), applies retention and aborts the multipart uploads nobody
// will finish; a measured bucket's usage comes from its counter.
func TestTheSweepMeasuresBucketsAndAbortsStaleUploads(t *testing.T) {
	records, fake := testRecords(t)
	ctx := context.Background()
	if err := records.Ensure(ctx, objectstore.BucketSpec{Name: "consumer-a", MaxBytes: 100}); err != nil {
		t.Fatal(err)
	}
	// An older descriptor, never measured: its usage is a listing until
	// the sweep.
	fake.put(descriptorKey("consumer-old"), `{"Name":"consumer-old","Created":"2024-01-01T00:00:00Z"}`)
	fake.put("buckets/consumer-old/a", "12345")
	fake.put("buckets/consumer-old/b", "123")
	usage, err := records.Usage(ctx, "consumer-old")
	if err != nil || usage.Buckets["consumer-old"].Bytes != 8 || usage.Buckets["consumer-old"].Objects != 2 {
		t.Fatalf("an unmeasured bucket's usage = %+v, %v", usage, err)
	}
	// A lost flush: the record says 0, the prefix holds 10.
	fake.put("buckets/consumer-a/x", "1234567890")
	records.Add("consumer-a", 3, 1) // a write this replica has not flushed
	fake.mu.Lock()
	fake.uploads = []fakeUpload{
		{key: "buckets/consumer-a/stale", id: "old-1", initiated: time.Now().Add(-8 * 24 * time.Hour)},
		{key: "buckets/consumer-a/fresh", id: "new-1", initiated: time.Now().Add(-time.Hour)},
	}
	fake.mu.Unlock()
	records.client = s3ClientFor(t, fake)
	if err := records.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"consumer-a", "consumer-old"} {
		rec, err := records.Get(ctx, name)
		if err != nil || rec.MeasuredAt.IsZero() {
			t.Fatalf("%s after the sweep = %+v, %v", name, rec, err)
		}
		want := map[string][2]int64{"consumer-a": {10, 1}, "consumer-old": {8, 2}}[name]
		if rec.UsedBytes != want[0] || rec.Objects != want[1] {
			t.Errorf("%s measured %d bytes in %d objects, want %v", name, rec.UsedBytes, rec.Objects, want)
		}
	}
	if d := records.pendingDelta("consumer-a"); d != (delta{}) {
		t.Errorf("the measurement left a pending delta %+v", d)
	}
	fake.mu.Lock()
	aborted, left := fake.aborted, len(fake.uploads)
	fake.mu.Unlock()
	if len(aborted) != 1 || aborted[0] != "old-1" || left != 1 {
		t.Errorf("aborted %v, %d uploads left; want the stale one gone and the fresh one kept", aborted, left)
	}
	// The cap judges from the measurement now.
	if err := records.Allow(ctx, "consumer-a", 91); err == nil {
		t.Error("101 of 100 bytes allowed")
	}
	if err := records.Allow(ctx, "consumer-a", 90); err != nil {
		t.Errorf("100 of 100 bytes refused: %v", err)
	}
}

// s3ClientFor is the conditional-delete client the sweep uses, pointed at
// the fake.
func s3ClientFor(t *testing.T, fake *fakeS3) *s3.Client {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return s3.New(s3.Options{BaseEndpoint: aws.String(srv.URL), Region: "us-east-1", UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider("test", "test-secret", ""), Retryer: aws.NopRetryer{}, RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired, ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired})
}

// Two flushes add up: a replica's increment goes onto the record as it
// is, not as it was read before the other replica's flush.
func TestFlushesAddUp(t *testing.T) {
	records, _ := testRecords(t)
	other, _ := testRecords(t)
	other.Store = records.Store // the same provider bucket
	ctx := context.Background()
	if err := records.Ensure(ctx, objectstore.BucketSpec{Name: "consumer-a"}); err != nil {
		t.Fatal(err)
	}
	records.Add("consumer-a", 5, 1)
	other.Add("consumer-a", 7, 1)
	if err := records.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := other.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	rec, err := records.Get(ctx, "consumer-a")
	if err != nil || rec.UsedBytes != 12 || rec.Objects != 2 {
		t.Errorf("after two replicas' flushes = %+v, %v", rec, err)
	}
	// A delta of a bucket deleted meanwhile is dropped, not kept forever.
	records.Add("consumer-a", 1, 0)
	if err := records.Delete(ctx, "consumer-a"); err != nil {
		t.Fatal(err)
	}
	if err := records.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if d := records.pendingDelta("consumer-a"); d != (delta{}) {
		t.Errorf("a deleted bucket keeps a pending delta %+v", d)
	}
}
