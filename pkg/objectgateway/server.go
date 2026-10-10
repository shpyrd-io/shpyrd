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
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
	"github.com/versity/versitygw/backend/s3proxy"
	"github.com/versity/versitygw/s3api"
	"github.com/versity/versitygw/s3api/middlewares"
)

const Region = "garage" // keep consumer compatibility when replacing Garage

type Config struct {
	Endpoint, Region, Bucket, AccessKey, SecretKey, AdminToken string
	S3Address, AdminAddress                                    string
	// SingleWriter: this is the only gateway process writing descriptors
	// (SHPYRD_GATEWAY_SINGLE_WRITER), which a provider that ignores If-Match
	// requires; see Records.
	SingleWriter bool
}
type Server struct {
	S3      *s3api.S3ApiServer
	Records *Records
	Admin   http.Handler
}

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
	}(), UsePathStyle: true, Retryer: aws.NopRetryer{}, RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired, ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired})
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
	options := []s3api.Option{s3api.WithQuiet(), s3api.WithKeepAlive(), s3api.WithHealth("/healthz"), s3api.WithConcurrencyLimiter(1024, 128), s3api.WithMpMaxParts(10000)}
	options = append(options, opts...)
	server, err := s3api.New(be, root, Region, &IAM{Records: records}, nil, nil, nil, nil, options...)
	if err != nil {
		return nil, err
	}
	return &Server{S3: server, Records: records, Admin: adminHandler(records, c.AdminToken)}, nil
}
func adminHandler(records *Records, token string) http.Handler {
	mux := http.NewServeMux()
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
	c := Config{Endpoint: os.Getenv("SHPYRD_GATEWAY_ENDPOINT"), Region: os.Getenv("SHPYRD_GATEWAY_REGION"), Bucket: os.Getenv("SHPYRD_GATEWAY_BUCKET"), AccessKey: os.Getenv("AWS_ACCESS_KEY_ID"), SecretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"), AdminToken: os.Getenv("SHPYRD_GATEWAY_ADMIN_TOKEN"), S3Address: os.Getenv("SHPYRD_GATEWAY_LISTEN"), AdminAddress: os.Getenv("SHPYRD_GATEWAY_ADMIN_LISTEN")}
	if c.S3Address == "" {
		c.S3Address = ":3900"
	}
	if c.AdminAddress == "" {
		c.AdminAddress = ":3903"
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
	failures := make(chan error, 2)
	go func() { failures <- admin.ListenAndServe() }()
	go func() { failures <- s.S3.ServeMultiPort([]string{c.S3Address}) }()
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.Records.Sweep(ctx); err != nil {
					log.Printf("gateway retention sweep failed: %v", err)
				}
			}
		}
	}()
	select {
	case <-ctx.Done():
	case err = <-failures:
		cancel()
	}
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()
	_ = admin.Shutdown(shutdown)
	_ = s.S3.ShutDown()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
