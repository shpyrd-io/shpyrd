package objectgateway

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/middleware"
	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
	"github.com/versity/versitygw/backend/s3proxy"
	"github.com/versity/versitygw/s3api"
	"github.com/versity/versitygw/s3api/middlewares"
)

// Region is what the platform's own consumers sign with (registry,
// sources, backups, a database's backups): kept from Garage so nothing
// is rolled. AppsRegion is what the projects' buckets sign with, on a
// listener of their own (storage plan step 5): a SigV4 scope, not a
// place, and the string every SDK accepts.
const (
	Region     = objectstore.Region
	AppsRegion = objectstore.AppsRegion
)

type Config struct {
	Endpoint, Region, Bucket, AccessKey, SecretKey, AdminToken string
	// S3Address is the platform consumers' listener, AppsAddress the
	// projects' (signing with AppsRegion, rate-limited per bucket, behind
	// the public door), AdminAddress the administrative API and metrics.
	S3Address, AppsAddress, AdminAddress string
	// SingleWriter: this is the only gateway process writing descriptors
	// (SHPYRD_GATEWAY_SINGLE_WRITER), which a provider that ignores If-Match
	// requires; see Records.
	SingleWriter bool
	// BucketRate is how many requests a second one bucket may make on the
	// apps listener, with a burst of twice that; 0 means no limit.
	BucketRate float64
}
type Server struct {
	S3      *s3api.S3ApiServer // the platform's listener
	Apps    *s3api.S3ApiServer // the projects' listener
	Records *Records
	Admin   http.Handler
}

// DefaultBucketRate is the per-bucket request rate when none is set: a
// thousand a second, with a burst of twice that. The load test on dev
// (2026-10-16) showed one pod answering a few hundred small requests a
// second to one bucket; the limit is a backstop against a runaway client,
// not a share of the gateway.
const DefaultBucketRate = 1000

func New(ctx context.Context, c Config, opts ...s3api.Option) (*Server, error) {
	if c.AdminToken == "" {
		return nil, errors.New("gateway admin token is required")
	}
	store, err := objectstore.NewS3(c.Endpoint, c.Region, c.Bucket, "", c.AccessKey, c.SecretKey)
	if err != nil {
		return nil, err
	}
	if ok, err := store.Client.BucketExists(ctx, c.Bucket); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("gateway physical bucket does not exist: %s", c.Bucket)
	}
	client := s3.New(s3.Options{Region: c.Region, Credentials: credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, ""), BaseEndpoint: func() *string {
		if c.Endpoint == "" {
			return nil
		}
		return aws.String(c.Endpoint)
	}(), UsePathStyle: true, Retryer: aws.NopRetryer{}, RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired, ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
		APIOptions: []func(*middleware.Stack) error{observeProvider}})
	proxy, err := s3proxy.NewWithClient(ctx, client, "")
	if err != nil {
		return nil, err
	}
	records := &Records{Store: store, client: client, SingleWriter: c.SingleWriter}
	note, err := records.Preflight(ctx)
	if err != nil {
		return nil, err
	}
	if note != "" {
		log.Print(note)
	}
	be := &Backend{proxy: proxy, physical: c.Bucket, records: records}
	// Root is an unexposed, process-local random account. Administration uses
	// the separate bearer-authenticated API, never the S3 root account.
	raw := make([]byte, 64)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	root := middlewares.RootUserConfig{Access: hex.EncodeToString(raw[:32]), Secret: hex.EncodeToString(raw[32:])}
	iam := &IAM{Records: records}
	listener := func(name, region string, extra ...s3api.Option) (*s3api.S3ApiServer, error) {
		options := []s3api.Option{s3api.WithQuiet(), s3api.WithKeepAlive(), s3api.WithHealth("/healthz"), s3api.WithConcurrencyLimiter(1024, 128), s3api.WithMpMaxParts(10000), s3api.WithMiddleware("/", observeRequests(name))}
		options = append(options, extra...)
		options = append(options, opts...)
		return s3api.New(be, root, region, iam, nil, nil, nil, nil, options...)
	}
	platform, err := listener("platform", Region)
	if err != nil {
		return nil, err
	}
	// The projects' listener: their region, and a request rate per bucket
	// (a byte cap bounds storage, not requests).
	var appsOptions []s3api.Option
	if c.BucketRate > 0 {
		appsOptions = append(appsOptions, s3api.WithMiddleware("/", newBucketLimiters(c.BucketRate, int(c.BucketRate*2)).middleware()))
	}
	apps, err := listener("apps", AppsRegion, appsOptions...)
	if err != nil {
		return nil, err
	}
	return &Server{S3: platform, Apps: apps, Records: records, Admin: adminHandler(records, c.AdminToken)}, nil
}
func adminHandler(records *Records, token string) http.Handler {
	mux := http.NewServeMux()
	// The gateway's numbers, for the monitoring stack; the admin port is
	// reachable from the platform's namespaces only (the NetworkPolicy).
	mux.Handle("/metrics", MetricsHandler())
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			http.Error(w, "unauthorized", 401)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		var in struct {
			Spec                         objectstore.BucketSpec
			Bucket, AccessKey, SecretKey string
		}
		if r.Method == "POST" {
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				http.Error(w, "invalid request", 400)
				return
			}
		}
		var out any = struct{}{}
		var err error
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/health":
		case "POST /v1/ensure-bucket":
			err = records.Ensure(r.Context(), in.Spec)
		case "POST /v1/ensure-user":
			out, err = records.Credential(r.Context(), in.Bucket, in.AccessKey, in.SecretKey)
		case "POST /v1/delete-user":
			err = records.Revoke(r.Context(), in.Bucket)
		case "POST /v1/delete-bucket":
			err = records.Delete(r.Context(), in.Bucket)
		case "GET /v1/usage":
			out, err = records.Usage(r.Context(), r.URL.Query().Get("bucket"))
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	return mux
}
func Main() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	c := Config{Endpoint: os.Getenv("SHPYRD_GATEWAY_ENDPOINT"), Region: os.Getenv("SHPYRD_GATEWAY_REGION"), Bucket: os.Getenv("SHPYRD_GATEWAY_BUCKET"), AccessKey: os.Getenv("AWS_ACCESS_KEY_ID"), SecretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"), AdminToken: os.Getenv("SHPYRD_GATEWAY_ADMIN_TOKEN"), S3Address: os.Getenv("SHPYRD_GATEWAY_LISTEN"), AppsAddress: os.Getenv("SHPYRD_GATEWAY_APPS_LISTEN"), AdminAddress: os.Getenv("SHPYRD_GATEWAY_ADMIN_LISTEN"), BucketRate: DefaultBucketRate}
	if c.S3Address == "" {
		c.S3Address = ":3900"
	}
	if c.AppsAddress == "" {
		c.AppsAddress = ":3901"
	}
	if c.AdminAddress == "" {
		c.AdminAddress = ":3903"
	}
	if v := os.Getenv("SHPYRD_GATEWAY_BUCKET_RATE"); v != "" {
		rate, err := strconv.ParseFloat(v, 64)
		if err != nil || rate < 0 {
			return fmt.Errorf("SHPYRD_GATEWAY_BUCKET_RATE must be a number of requests a second, not %q", v)
		}
		c.BucketRate = rate
	}
	switch v := os.Getenv("SHPYRD_GATEWAY_SINGLE_WRITER"); v {
	case "true":
		c.SingleWriter = true
	case "", "false":
	default:
		return fmt.Errorf("SHPYRD_GATEWAY_SINGLE_WRITER must be true or false, not %q", v)
	}
	s, err := New(ctx, c)
	if err != nil {
		return err
	}
	admin := &http.Server{Addr: c.AdminAddress, Handler: s.Admin, ReadHeaderTimeout: 10 * time.Second}
	failures := make(chan error, 3)
	go func() { failures <- admin.ListenAndServe() }()
	go func() { failures <- s.S3.ServeMultiPort([]string{c.S3Address}) }()
	go func() { failures <- s.Apps.ServeMultiPort([]string{c.AppsAddress}) }()
	go s.Loops(ctx)
	select {
	case <-ctx.Done():
	case err = <-failures:
		cancel()
	}
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()
	_ = admin.Shutdown(shutdown)
	_ = s.S3.ShutDown()
	_ = s.Apps.ShutDown()
	// What this process counted goes to the records before it goes.
	if ferr := s.Records.Flush(shutdown); ferr != nil {
		log.Printf("gateway counter flush at shutdown failed: %v", ferr)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// FlushEvery is how often a replica writes its counters onto the
// records; a replica lost between flushes loses at most this much of
// its own writes, until the sweep measures.
const FlushEvery = time.Minute

// Loops runs the periodic work until ctx ends: the counter flush every
// minute, the sweep (retention, the measurement, stale uploads) hourly.
func (s *Server) Loops(ctx context.Context) {
	flush := time.NewTicker(FlushEvery)
	sweep := time.NewTicker(time.Hour)
	defer flush.Stop()
	defer sweep.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-flush.C:
			if err := s.Records.Flush(ctx); err != nil {
				log.Printf("gateway counter flush failed: %v", err)
			}
		case <-sweep.C:
			if err := s.Records.Sweep(ctx); err != nil {
				log.Printf("gateway sweep failed: %v", err)
			}
		}
	}
}
