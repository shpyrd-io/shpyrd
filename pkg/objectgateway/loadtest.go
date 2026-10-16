package objectgateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// The load test of the storage plan's step 5: many concurrent small
// objects through the gateway, with the latency recorded on the client
// side. It runs where an app would (a Job in the cluster, with a bucket's
// own credential) as `shpyrd-server object-gateway-load`, reading the
// bucket's variables (AWS_ENDPOINT_URL, AWS_REGION, BUCKET,
// AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY) and
// SHPYRD_LOAD_CONCURRENCY (200), SHPYRD_LOAD_OBJECTS (2000),
// SHPYRD_LOAD_SIZE (4096 bytes). It writes a report and exits 1 when the
// p95 of a PUT or a GET is above SHPYRD_LOAD_P95_MS (200).

type loadReport struct {
	Operation string
	Count     int64
	Errors    int64
	P50, P95  time.Duration
	Max       time.Duration
	Elapsed   time.Duration
}

func (r loadReport) String() string {
	rate := float64(r.Count) / r.Elapsed.Seconds()
	return fmt.Sprintf("%-6s %6d ok %4d failed  p50 %6.1f ms  p95 %6.1f ms  max %7.1f ms  %6.0f/s", r.Operation, r.Count, r.Errors, ms(r.P50), ms(r.P95), ms(r.Max), rate)
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

type latencies struct {
	mu     sync.Mutex
	values []time.Duration
	errors atomic.Int64
}

func (l *latencies) add(d time.Duration) {
	l.mu.Lock()
	l.values = append(l.values, d)
	l.mu.Unlock()
}

func (l *latencies) report(op string, elapsed time.Duration) loadReport {
	l.mu.Lock()
	defer l.mu.Unlock()
	sort.Slice(l.values, func(i, j int) bool { return l.values[i] < l.values[j] })
	out := loadReport{Operation: op, Count: int64(len(l.values)), Errors: l.errors.Load(), Elapsed: elapsed}
	if n := len(l.values); n > 0 {
		out.P50, out.P95, out.Max = l.values[n/2], l.values[(n*95)/100], l.values[n-1]
	}
	return out
}

func envInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// LoadMain is `shpyrd-server object-gateway-load`.
func LoadMain(out io.Writer) error {
	endpoint, bucket := os.Getenv("AWS_ENDPOINT_URL"), os.Getenv("BUCKET")
	if endpoint == "" || bucket == "" || os.Getenv("AWS_ACCESS_KEY_ID") == "" {
		return errors.New("the bucket's variables are required: AWS_ENDPOINT_URL, AWS_REGION, BUCKET, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY")
	}
	concurrency, objects, size, p95 := envInt("SHPYRD_LOAD_CONCURRENCY", 200), envInt("SHPYRD_LOAD_OBJECTS", 2000), envInt("SHPYRD_LOAD_SIZE", 4096), envInt("SHPYRD_LOAD_P95_MS", 200)
	host, secure := endpoint, false
	if after, ok := cutScheme(endpoint, "https://"); ok {
		host, secure = after, true
	} else if after, ok := cutScheme(endpoint, "http://"); ok {
		host = after
	}
	client, err := minio.New(host, &minio.Options{Creds: credentials.NewStaticV4(os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY"), ""), Secure: secure, Region: os.Getenv("AWS_REGION"), BucketLookup: minio.BucketLookupPath})
	if err != nil {
		return err
	}
	ctx := context.Background()
	body := bytes.Repeat([]byte("shpyrd load test "), size/17+1)[:size]
	prefix := fmt.Sprintf("load-%d/", time.Now().Unix())
	fmt.Fprintf(out, "object gateway load test: %d objects of %d bytes, %d at a time, against %s/%s\n", objects, size, concurrency, endpoint, bucket)

	run := func(op string, fn func(ctx context.Context, i int) error) loadReport {
		var l latencies
		start := time.Now()
		var wg sync.WaitGroup
		sem := make(chan struct{}, concurrency)
		for i := 0; i < objects; i++ {
			wg.Add(1)
			sem <- struct{}{}
			go func(i int) {
				defer wg.Done()
				defer func() { <-sem }()
				began := time.Now()
				if err := fn(ctx, i); err != nil {
					l.errors.Add(1)
					if l.errors.Load() <= 3 {
						fmt.Fprintf(out, "  %s %d: %v\n", op, i, err)
					}
					return
				}
				l.add(time.Since(began))
			}(i)
		}
		wg.Wait()
		return l.report(op, time.Since(start))
	}
	put := run("PUT", func(ctx context.Context, i int) error {
		_, err := client.PutObject(ctx, bucket, fmt.Sprintf("%s%05d", prefix, i), bytes.NewReader(body), int64(len(body)), minio.PutObjectOptions{})
		return err
	})
	fmt.Fprintln(out, put)
	get := run("GET", func(ctx context.Context, i int) error {
		obj, err := client.GetObject(ctx, bucket, fmt.Sprintf("%s%05d", prefix, i), minio.GetObjectOptions{})
		if err != nil {
			return err
		}
		defer obj.Close()
		_, err = io.Copy(io.Discard, obj)
		return err
	})
	fmt.Fprintln(out, get)
	del := run("DELETE", func(ctx context.Context, i int) error {
		return client.RemoveObject(ctx, bucket, fmt.Sprintf("%s%05d", prefix, i), minio.RemoveObjectOptions{})
	})
	fmt.Fprintln(out, del)
	limit := time.Duration(p95) * time.Millisecond
	if put.Errors+get.Errors > 0 || put.P95 > limit || get.P95 > limit {
		return fmt.Errorf("the gateway did not sustain the load: p95 PUT %.1f ms, GET %.1f ms (limit %d ms), %d failures", ms(put.P95), ms(get.P95), p95, put.Errors+get.Errors)
	}
	fmt.Fprintf(out, "the gateway sustained the load: p95 under %d ms\n", p95)
	return nil
}

func cutScheme(s, scheme string) (string, bool) {
	if len(s) >= len(scheme) && s[:len(scheme)] == scheme {
		return s[len(scheme):], true
	}
	return s, false
}
