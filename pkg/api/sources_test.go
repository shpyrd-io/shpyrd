package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
)

func TestSourceArchivesSurviveAWriterRestart(t *testing.T) {
	for _, backend := range []string{"filesystem", "s3"} {
		t.Run(backend, func(t *testing.T) {
			archive := []byte("an archive addressed by its content")
			name := fmt.Sprintf("%x.tgz", sha256.Sum256(archive))
			dir := t.TempDir()
			source := &SourceStore{Dir: dir, BaseURL: "http://sources.internal"}
			var cloudData []byte
			if backend == "s3" {
				remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/images/sources/"+name {
						t.Errorf("unexpected S3 key %q", r.URL.Path)
						w.WriteHeader(404)
						return
					}
					switch r.Method {
					case "PUT":
						var body io.Reader = r.Body
						if r.Header.Get("Content-Encoding") == "aws-chunked" {
							body = httputil.NewChunkedReader(r.Body)
						}
						cloudData, _ = io.ReadAll(body)
						w.Header().Set("ETag", `"test-etag"`)
					case "GET", "HEAD":
						w.Header().Set("ETag", `"test-etag"`)
						http.ServeContent(w, r, name, time.Unix(100, 0), bytes.NewReader(cloudData))
					default:
						t.Errorf("unexpected method %s", r.Method)
						w.WriteHeader(405)
					}
				}))
				defer remote.Close()
				var err error
				source.Bucket, err = objectstore.NewS3(remote.URL, "test-region", "images", "sources", "key", "secret")
				if err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 2; i++ {
				info, err := source.Put(bytes.NewReader(archive))
				if err != nil {
					t.Fatal(err)
				}
				if info.URL != source.BaseURL+"/api/sources/"+name || info.Size != int64(len(archive)) {
					t.Fatalf("wrong upload identity: %+v", info)
				}
			}
			if backend == "s3" {
				files, _ := os.ReadDir(dir)
				if len(files) != 0 {
					t.Fatal("S3 upload left persistent data or a temporary file")
				}
				source = &SourceStore{Dir: t.TempDir(), BaseURL: source.BaseURL, Bucket: source.Bucket}
			} else if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
			srv := &Server{sources: source}
			router := gin.New()
			router.GET("/api/sources/:name", srv.serveSource)
			for _, partial := range []bool{false, true} {
				req := httptest.NewRequest("GET", "/api/sources/"+name, nil)
				want, code := archive, http.StatusOK
				if partial {
					req.Header.Set("Range", "bytes=3-8")
					want, code = archive[3:9], http.StatusPartialContent
				}
				resp := httptest.NewRecorder()
				router.ServeHTTP(resp, req)
				if resp.Code != code || !bytes.Equal(resp.Body.Bytes(), want) {
					t.Fatalf("download: %d %q, want %d %q", resp.Code, resp.Body.String(), code, want)
				}
			}
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequest("GET", "/api/sources/not-a-hash.tgz", nil))
			if resp.Code != http.StatusNotFound {
				t.Fatal("invalid source name accepted")
			}
		})
	}
}

func TestSourceErrorsDoNotLeaveTemporaryFiles(t *testing.T) {
	s := &SourceStore{Dir: t.TempDir()}
	if _, err := s.Put(strings.NewReader("")); err == nil {
		t.Error("empty archive accepted")
	}
	files, _ := os.ReadDir(s.Dir)
	if len(files) != 0 {
		t.Fatal("failed upload left a temporary file")
	}
	s.Bucket, _ = objectstore.NewS3("https://objects.invalid", "test-region", "sources", "sources", "key", "secret")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.PutContext(ctx, strings.NewReader("archive")); err == nil {
		t.Error("cancelled upload succeeded")
	}
	files, _ = os.ReadDir(s.Dir)
	if len(files) != 0 {
		t.Fatal("cancelled S3 upload left a temporary file")
	}
}

func TestSourceReadRequiresObjectCapability(t *testing.T) {
	source := &SourceStore{Dir: t.TempDir(), BaseURL: "http://sources.internal", SigningKey: []byte("test-platform-signing-key")}
	one, err := source.Put(strings.NewReader("private source one"))
	if err != nil {
		t.Fatal(err)
	}
	two, err := source.Put(strings.NewReader("private source two"))
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{sources: source}
	router := gin.New()
	router.GET("/api/sources/:name", server.serveSource)
	for _, tc := range []struct {
		url    string
		status int
	}{{one.URL, 200}, {source.BaseURL + "/api/sources/" + one.SHA256 + ".tgz", 404}, {strings.Replace(one.URL, one.SHA256, two.SHA256, 1), 404}, {one.URL + "wrong", 404}} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest("GET", tc.url, nil))
		if rr.Code != tc.status {
			t.Errorf("response=%d want %d", rr.Code, tc.status)
		}
	}
	// Signature stays valid across restart and is invalid after key rotation.
	restarted := &SourceStore{SigningKey: source.SigningKey}
	if !restarted.validURL(one.URL) {
		t.Fatal("capability lost on restart")
	}
	restarted.SigningKey = []byte("rotated-key")
	if restarted.validURL(one.URL) {
		t.Fatal("old capability survived rotation")
	}
}
