package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
)

// maxSourceSize bounds uploaded source archives. The companion Ingress of
// each front door allows the same for this path alone (controller
// SourcesBodySize, deploy/components/shpyrd/base/ingress.yaml); nginx
// would otherwise stop uploads at 1 MiB with a 413 page before they reach
// this handler.
const maxSourceSize = 512 << 20

var (
	errSourceTooLarge = fmt.Errorf("archive exceeds %d MiB", maxSourceSize>>20)
	errSourceEmpty    = errors.New("empty archive")
)

// SourcesPort is where the server serves source archives to build pods
// (RFC-0033: the API and the edge are for the front doors only).
const SourcesPort = controller.SourcesPort

var sourceName = regexp.MustCompile(`^[a-f0-9]{64}\.tgz$`)

// SourceStore keeps uploaded archives on disk or in S3, addressed by their
// SHA-256. kpack fetches them from the same cluster-internal blob URL.
type SourceStore struct {
	// SigningKey turns archive URLs into per-object read capabilities. It is
	// persisted in a platform Secret and included in encrypted platform backups.
	SigningKey []byte
	Dir        string
	// Bucket is optional. Dir is then only temporary upload staging.
	Bucket *objectstore.S3
	// BaseURL is the cluster-internal address of this server, e.g.
	// http://shpyrd-server.shpyrd-system.svc.
	BaseURL string
}

// SourceInfo is returned after an upload.
type SourceInfo struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	URL    string `json:"url"`
}

func (s *SourceStore) fileName(sha string) string { return sha + ".tgz" }

func (s *SourceStore) url(sha string) string {
	u := s.BaseURL + "/api/sources/" + s.fileName(sha)
	if len(s.SigningKey) > 0 {
		u += "?token=" + s.token(s.fileName(sha))
	}
	return u
}

// A capability authorizes one immutable archive, never a bucket listing or
// another object. Its lifetime follows the source archive so rebuilds keep
// working; rotating the platform signing key revokes issued capabilities.
func (s *SourceStore) token(name string) string {
	return objectstore.SourceCapability(s.SigningKey, name)
}

func (s *SourceStore) validToken(name, token string) bool {
	return len(s.SigningKey) == 0 || hmac.Equal([]byte(s.token(name)), []byte(token))
}
func (s *SourceStore) ownsURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	base, err := url.Parse(s.BaseURL)
	return err == nil && u.Hostname() == base.Hostname()
}
func (s *SourceStore) validURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	name := filepath.Base(u.Path)
	return sourceName.MatchString(name) && s.validToken(name, u.Query().Get("token"))
}

// Put stores an archive and returns its identity. Duplicate uploads share
// the same content-addressed name.
func (s *SourceStore) Put(r io.Reader) (*SourceInfo, error) {
	return s.PutContext(context.Background(), r)
}

// PutContext stages the upload while hashing, then commits it to the backend.
func (s *SourceStore) PutContext(ctx context.Context, r io.Reader) (*SourceInfo, error) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(s.Dir, "upload-*.part")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r, maxSourceSize+1))
	if err != nil {
		return nil, err
	}
	if n > maxSourceSize {
		return nil, errSourceTooLarge
	}
	if n == 0 {
		return nil, errSourceEmpty
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	sha := hex.EncodeToString(h.Sum(nil))
	if s.Bucket != nil {
		f, err := os.Open(tmp.Name())
		if err != nil {
			return nil, err
		}
		defer f.Close()
		_, err = s.Bucket.Client.PutObject(ctx, s.Bucket.Bucket, s.Bucket.Key(s.fileName(sha)), f, n, minio.PutObjectOptions{ContentType: "application/gzip"})
		if err != nil {
			return nil, fmt.Errorf("store source archive: %w", err)
		}
		return &SourceInfo{SHA256: sha, Size: n, URL: s.url(sha)}, nil
	}
	final := filepath.Join(s.Dir, s.fileName(sha))
	if _, err := os.Stat(final); err == nil {
		return &SourceInfo{SHA256: sha, Size: n, URL: s.url(sha)}, nil
	}
	if err := os.Rename(tmp.Name(), final); err != nil {
		return nil, err
	}
	return &SourceInfo{SHA256: sha, Size: n, URL: s.url(sha)}, nil
}

func (s *Server) uploadSource(c *gin.Context) {
	if s.sources == nil {
		abort(c, http.StatusServiceUnavailable, errors.New("source store not configured"))
		return
	}
	info, err := s.sources.PutContext(c.Request.Context(), c.Request.Body)
	if err != nil {
		status := http.StatusBadGateway
		switch {
		case errors.Is(err, errSourceTooLarge):
			status = http.StatusRequestEntityTooLarge
		case errors.Is(err, errSourceEmpty):
			status = http.StatusBadRequest
		}
		abort(c, status, err)
		return
	}
	s.log.Info("source uploaded", "sha256", info.SHA256[:12], "size", info.Size)
	c.JSON(http.StatusCreated, info)
}

func (s *Server) serveSource(c *gin.Context) {
	name := c.Param("name")
	if s.sources == nil || !sourceName.MatchString(name) || !s.sources.validToken(name, c.Query("token")) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if bucket := s.sources.Bucket; bucket != nil {
		obj, err := bucket.Client.GetObject(c.Request.Context(), bucket.Bucket, bucket.Key(name), minio.GetObjectOptions{})
		if err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
		defer obj.Close()
		info, err := obj.Stat()
		if err != nil {
			if minio.ToErrorResponse(err).Code == "NoSuchKey" {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			} else {
				abort(c, http.StatusBadGateway, err)
			}
			return
		}
		c.Header("Content-Type", "application/gzip")
		http.ServeContent(c.Writer, c.Request, name, info.LastModified, obj)
		return
	}
	path := filepath.Join(s.sources.Dir, name)
	if _, err := os.Stat(path); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.Header("Content-Type", "application/gzip")
	c.File(path)
}
