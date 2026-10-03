package objectstore

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3 connects to an existing bucket. Prefix separates platform sources from
// registry objects when both use the same bucket.
type S3 struct {
	Client *minio.Client
	Bucket string
	Prefix string
}

func NewS3(endpoint, region, bucket, prefix, accessKey, secretKey string) (*S3, error) {
	if bucket == "" || region == "" || accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("S3 bucket, region and both credentials are required")
	}
	if endpoint == "" {
		endpoint = "https://s3." + region + ".amazonaws.com"
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || (u.Path != "" && u.Path != "/") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid S3 endpoint")
	}
	c, err := minio.New(u.Host, &minio.Options{
		Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Region: region,
		Secure: u.Scheme == "https", BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, err
	}
	return &S3{Client: c, Bucket: bucket, Prefix: strings.Trim(prefix, "/")}, nil
}

func (s *S3) Key(name string) string {
	if s.Prefix == "" {
		return name
	}
	return s.Prefix + "/" + name
}

// Usage counts stored objects, including unreferenced registry blobs. A
// failed listing never reports a partial total as a successful measurement.
func (s *S3) Usage(ctx context.Context) (Usage, error) {
	var out Usage
	prefix := ""
	if s.Prefix != "" {
		prefix = s.Prefix + "/"
	}
	for obj := range s.Client.ListObjects(ctx, s.Bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return Usage{}, obj.Err
		}
		out.Bytes += obj.Size
		out.Objects++
	}
	return out, ctx.Err()
}
