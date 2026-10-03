package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
)

// Gateway uses shpyrd's logical-bucket administration API. Consumers continue
// using ordinary S3 clients; only the control plane holds this bearer token.
type Gateway struct{ *Store }

func ConnectGateway(endpoint, admin, token string) (*Gateway, error) {
	s, err := Connect(endpoint, admin, token)
	if err != nil {
		return nil, err
	}
	return &Gateway{s}, nil
}
func (g *Gateway) EnsureLayout(ctx context.Context, _ int64) error { return g.Ping(ctx) }
func (g *Gateway) Ping(ctx context.Context) error {
	return g.call(ctx, "GET", "/v1/health", nil, nil, nil)
}
func (g *Gateway) EnsureBucket(ctx context.Context, spec BucketSpec) error {
	return g.call(ctx, "POST", "/v1/ensure-bucket", nil, map[string]any{"Spec": spec}, nil)
}
func (g *Gateway) EnsureUser(ctx context.Context, bucket, access, secret string) (Credential, error) {
	var out Credential
	err := g.call(ctx, "POST", "/v1/ensure-user", nil, map[string]string{"Bucket": bucket, "AccessKey": access, "SecretKey": secret}, &out)
	return out, err
}
func (g *Gateway) DeleteUser(ctx context.Context, bucket, access string) error {
	return g.call(ctx, "POST", "/v1/delete-user", nil, map[string]string{"Bucket": bucket, "AccessKey": access}, nil)
}
func (g *Gateway) DeleteBucket(ctx context.Context, bucket string) error {
	return g.call(ctx, "POST", "/v1/delete-bucket", nil, map[string]string{"Bucket": bucket}, nil)
}
func (g *Gateway) Usage(ctx context.Context) (*Capacity, error) {
	var out Capacity
	err := g.call(ctx, "GET", "/v1/usage", nil, nil, &out)
	return &out, err
}
func (g *Gateway) BucketUsage(ctx context.Context, bucket string) (*Capacity, error) {
	var out Capacity
	err := g.call(ctx, "GET", "/v1/usage", url.Values{"bucket": {bucket}}, nil, &out)
	return &out, err
}

// LogicalBucketName is unambiguous across Kubernetes namespaces and remains
// S3-compatible even when either resource name is 63 characters long.
func LogicalBucketName(namespace, name string) string {
	sum := sha256.Sum256([]byte(namespace + "/" + name))
	return "shpyrd-" + hex.EncodeToString(sum[:28])
}
