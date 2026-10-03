package objectgateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
)

// Run against a disposable S3 backend, never a production bucket:
// SHPYRD_GATEWAY_TEST_ENDPOINT=http://127.0.0.1:19300 go test ./pkg/objectgateway -run TestGatewayIntegration -v
// Add SHPYRD_GATEWAY_TEST_LARGE=1 for 1.125 GiB transfers and process RSS.
func TestGatewayProcess(t *testing.T) {
	if os.Getenv("SHPYRD_GATEWAY_TEST_CHILD") != "1" {
		t.Skip("subprocess helper")
	}
	if err := Main(); err != nil {
		t.Fatal(err)
	}
}
func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}
func TestGatewayIntegration(t *testing.T) {
	endpoint := os.Getenv("SHPYRD_GATEWAY_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("requires disposable S3 backend")
	}
	ctx := context.Background()
	physical := fmt.Sprintf("gateway-test-%d", time.Now().UnixNano())
	store, err := objectstore.NewS3(endpoint, "us-east-1", physical, "", "shpyrdtest", "shpyrd-test-only-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Client.MakeBucket(ctx, physical, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for e := range store.Client.RemoveObjects(ctx, physical, store.Client.ListObjects(ctx, physical, minio.ListObjectsOptions{Recursive: true}), minio.RemoveObjectsOptions{}) {
			if e.Err != nil {
				t.Log(e.Err)
			}
		}
		_ = store.Client.RemoveBucket(ctx, physical)
	})
	records := &Records{Store: store}
	makeUser := func(name string) objectstore.Credential {
		t.Helper()
		if err := records.Ensure(ctx, objectstore.BucketSpec{Name: name}); err != nil {
			t.Fatal(err)
		}
		c, err := records.Credential(ctx, name, "", "")
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	a, b := makeUser("consumer-a"), makeUser("consumer-b")
	addr, admin := freeAddress(t), freeAddress(t)
	exe := os.Getenv("SHPYRD_GATEWAY_TEST_BINARY")
	args := []string{"object-gateway"}
	if exe == "" {
		exe, err = os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		args = []string{"-test.run=^TestGatewayProcess$", "-test.v"}
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), "SHPYRD_GATEWAY_TEST_CHILD=1", "SHPYRD_GATEWAY_ENDPOINT="+endpoint, "SHPYRD_GATEWAY_REGION=us-east-1", "SHPYRD_GATEWAY_BUCKET="+physical, "AWS_ACCESS_KEY_ID=shpyrdtest", "AWS_SECRET_ACCESS_KEY=shpyrd-test-only-password", "SHPYRD_GATEWAY_ADMIN_TOKEN=local-test-admin", "SHPYRD_GATEWAY_LISTEN="+addr, "SHPYRD_GATEWAY_ADMIN_LISTEN="+admin)
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
		if t.Failed() {
			t.Log(logs.String())
		}
	})
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("gateway did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	client := func(c objectstore.Credential) *minio.Client {
		v, err := minio.New(addr, &minio.Options{Creds: credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""), Region: Region, BucketLookup: minio.BucketLookupPath})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	ca, cb := client(a), client(b)
	put := func(c *minio.Client, bucket, key, value string) {
		t.Helper()
		_, err := c.PutObject(ctx, bucket, key, strings.NewReader(value), int64(len(value)), minio.PutObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}
	put(ca, a.AccessKey, "private/data", "alpha")
	put(cb, b.AccessKey, "private/data", "beta")
	deniedCode := func(err error) {
		t.Helper()
		if minio.ToErrorResponse(err).Code != "AccessDenied" {
			t.Fatalf("expected AccessDenied, got %v", err)
		}
	}
	t.Run("isolation", func(t *testing.T) {
		_, err := ca.StatObject(ctx, b.AccessKey, "private/data", minio.StatObjectOptions{})
		deniedCode(err)
		obj, err := ca.GetObject(ctx, b.AccessKey, "private/data", minio.GetObjectOptions{})
		if err == nil {
			_, err = io.Copy(io.Discard, obj)
			obj.Close()
		}
		deniedCode(err)
		for obj := range ca.ListObjects(ctx, b.AccessKey, minio.ListObjectsOptions{Recursive: true}) {
			deniedCode(obj.Err)
		}
		for obj := range ca.ListObjects(ctx, a.AccessKey, minio.ListObjectsOptions{Recursive: true}) {
			if obj.Err != nil {
				t.Fatal(obj.Err)
			}
			if obj.Key != "private/data" {
				t.Fatalf("leaked physical prefix %q", obj.Key)
			}
		}
		_, err = ca.CopyObject(ctx, minio.CopyDestOptions{Bucket: a.AccessKey, Object: "stolen"}, minio.CopySrcOptions{Bucket: b.AccessKey, Object: "private/data"})
		deniedCode(err)
		_, err = ca.PutObject(ctx, a.AccessKey, "../consumer-b/stolen", strings.NewReader("x"), 1, minio.PutObjectOptions{})
		if err == nil {
			t.Fatal("accepted traversal")
		}
		buckets, err := ca.ListBuckets(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(buckets) != 1 || buckets[0].Name != a.AccessKey {
			t.Fatalf("bucket leak: %+v", buckets)
		}
		_, err = ca.PutObject(ctx, physical, "__shpyrd_gateway/buckets/consumer-b.json", strings.NewReader("x"), 1, minio.PutObjectOptions{})
		if err == nil {
			t.Fatal("metadata write allowed")
		}
		_, err = ca.CopyObject(ctx, minio.CopyDestOptions{Bucket: a.AccessKey, Object: "copy"}, minio.CopySrcOptions{Bucket: a.AccessKey, Object: "private/data"})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("pagination", func(t *testing.T) {
		for i := 0; i < 4; i++ {
			put(ca, a.AccessKey, fmt.Sprintf("pages/%d", i), "page")
		}
		for _, v1 := range []bool{false, true} {
			count := 0
			for obj := range ca.ListObjects(ctx, a.AccessKey, minio.ListObjectsOptions{Prefix: "pages/", Recursive: true, MaxKeys: 1, UseV1: v1}) {
				if obj.Err != nil {
					t.Fatal(obj.Err)
				}
				if !strings.HasPrefix(obj.Key, "pages/") {
					t.Fatal(obj.Key)
				}
				count++
			}
			if count != 4 {
				t.Fatalf("v1=%v: %d objects", v1, count)
			}
		}
	})
	t.Run("multipart-isolation", func(t *testing.T) {
		coreA, coreB := minio.Core{Client: ca}, minio.Core{Client: cb}
		id, err := coreA.NewMultipartUpload(ctx, a.AccessKey, "multipart", minio.PutObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer coreA.AbortMultipartUpload(ctx, a.AccessKey, "multipart", id)
		_, err = coreB.PutObjectPart(ctx, b.AccessKey, "multipart", id, 1, strings.NewReader("stolen"), 6, minio.PutObjectPartOptions{})
		if err == nil {
			t.Fatal("foreign multipart ID accepted")
		}
		_, err = coreB.ListObjectParts(ctx, a.AccessKey, "multipart", id, 0, 100)
		deniedCode(err)
		_, err = coreA.PutObjectPart(ctx, a.AccessKey, "multipart", id, 1, strings.NewReader("part"), 4, minio.PutObjectPartOptions{})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("encoded-keys-range-presign-delete", func(t *testing.T) {
		key := "encoded/ação +%?#.txt"
		put(ca, a.AccessKey, key, "0123456789")
		opts := minio.GetObjectOptions{}
		if err := opts.SetRange(2, 5); err != nil {
			t.Fatal(err)
		}
		obj, err := ca.GetObject(ctx, a.AccessKey, key, opts)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(obj)
		obj.Close()
		if err != nil || string(data) != "2345" {
			t.Fatalf("range: %q, %v", data, err)
		}
		signed, err := ca.PresignedGetObject(ctx, a.AccessKey, key, time.Minute, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.Get(signed.String())
		if err != nil {
			t.Fatal(err)
		}
		data, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK || string(data) != "0123456789" {
			t.Fatalf("presigned GET: status=%d, data=%q, err=%v", resp.StatusCode, data, err)
		}
		for _, v1 := range []bool{false, true} {
			count := 0
			for obj := range ca.ListObjects(ctx, a.AccessKey, minio.ListObjectsOptions{Prefix: "encoded/", Recursive: true, UseV1: v1}) {
				if obj.Err != nil || obj.Key != key {
					t.Fatalf("encoded listing: key=%q err=%v", obj.Key, obj.Err)
				}
				count++
			}
			if count != 1 {
				t.Fatalf("encoded listing count: %d", count)
			}
		}
		objects := make(chan minio.ObjectInfo, 1)
		objects <- minio.ObjectInfo{Key: key}
		close(objects)
		for e := range ca.RemoveObjects(ctx, a.AccessKey, objects, minio.RemoveObjectsOptions{}) {
			t.Fatal(e.Err)
		}
		if _, err := ca.StatObject(ctx, a.AccessKey, key, minio.StatObjectOptions{}); minio.ToErrorResponse(err).Code != "NoSuchKey" {
			t.Fatalf("object still accessible after batch delete: %v", err)
		}
	})
	t.Run("concurrent-provisioning", func(t *testing.T) {
		const name = "concurrent-consumer"
		if err := records.Ensure(ctx, objectstore.BucketSpec{Name: name}); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		results := make(chan objectstore.Credential, 8)
		for i := 0; i < cap(results); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cred, err := records.Credential(ctx, name, "", "")
				if err != nil {
					t.Error(err)
				}
				results <- cred
			}()
		}
		wg.Wait()
		close(results)
		want, err := records.Credential(ctx, name, "", "")
		if err != nil {
			t.Fatal(err)
		}
		for got := range results {
			if got != want {
				t.Error("concurrent provisioning returned different credentials")
			}
		}
	})
	t.Run("revocation", func(t *testing.T) {
		if err := records.Revoke(ctx, b.AccessKey); err != nil {
			t.Fatal(err)
		}
		if _, err := cb.StatObject(ctx, b.AccessKey, "private/data", minio.StatObjectOptions{}); err == nil {
			t.Fatal("revoked key works")
		}
		fresh, err := records.Credential(ctx, b.AccessKey, b.AccessKey, b.SecretKey)
		if err != nil {
			t.Fatal(err)
		}
		if fresh.SecretKey == b.SecretKey {
			t.Fatal("reactivation reused a revoked secret")
		}
		if _, err := cb.StatObject(ctx, b.AccessKey, "private/data", minio.StatObjectOptions{}); err == nil {
			t.Fatal("old credential works after reactivation")
		}
		if _, err := client(fresh).StatObject(ctx, b.AccessKey, "private/data", minio.StatObjectOptions{}); err != nil {
			t.Fatal(err)
		}
	})
	if os.Getenv("SHPYRD_GATEWAY_TEST_LARGE") != "1" {
		return
	}
	var peak atomic.Int64
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				raw, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(cmd.Process.Pid)).Output()
				if err == nil {
					n, _ := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
					for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
					}
				}
			}
		}
	}()
	const size int64 = 1152 << 20
	transfer := func(t *testing.T, name, bucket string, c *minio.Client, single bool) {
		key := "large/" + name
		h := sha256.New()
		reader := io.TeeReader(io.LimitReader(&patternReader{}, size), h)
		start := time.Now()
		info, err := c.PutObject(ctx, bucket, key, reader, size, minio.PutObjectOptions{DisableMultipart: single, PartSize: 16 << 20, NumThreads: 2})
		upload := time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size != size {
			t.Fatalf("uploaded %d", info.Size)
		}
		want := hex.EncodeToString(h.Sum(nil))
		start = time.Now()
		obj, err := c.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		h.Reset()
		n, err := io.Copy(h, obj)
		obj.Close()
		download := time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		if n != size || hex.EncodeToString(h.Sum(nil)) != want {
			t.Fatal("integrity mismatch")
		}
		t.Logf("bytes=%d upload=%s (%.1f MiB/s) download=%s (%.1f MiB/s) gateway_peak_RSS=%.1f MiB sha256=%s", size, upload, float64(size)/(1<<20)/upload.Seconds(), download, float64(size)/(1<<20)/download.Seconds(), float64(peak.Load())/1024, want)
		if err := c.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, run := range []struct {
		name, bucket string
		c            *minio.Client
		single       bool
	}{{"direct-single", physical, store.Client, true}, {"gateway-single", a.AccessKey, ca, true}, {"gateway-multipart", a.AccessKey, ca, false}} {
		t.Run(run.name, func(t *testing.T) { transfer(t, run.name, run.bucket, run.c, run.single) })
	}
	start := time.Now()
	t.Run("gateway-concurrent", func(t *testing.T) {
		for i := 0; i < 2; i++ {
			name := fmt.Sprintf("parallel-%d", i)
			t.Run(name, func(t *testing.T) { t.Parallel(); transfer(t, name, a.AccessKey, ca, false) })
		}
	})
	t.Logf("concurrent_total_bytes=%d elapsed=%s gateway_peak_RSS=%.1f MiB", 2*size, time.Since(start), float64(peak.Load())/1024)

	if peak.Load() > 512*1024 {
		t.Errorf("gateway exceeded 512MiB RSS: %.1f MiB", float64(peak.Load())/1024)
	}
}

// Deterministic bytes generated with bounded memory. xorshift avoids a giant
// input allocation and yields nontrivial data for end-to-end SHA verification.
type patternReader struct{ state uint64 }

func (r *patternReader) Read(p []byte) (int, error) {
	if r.state == 0 {
		r.state = 0x123456789abcdef
	}
	for i := range p {
		r.state ^= r.state << 13
		r.state ^= r.state >> 7
		r.state ^= r.state << 17
		p[i] = byte(r.state)
	}
	return len(p), nil
}
