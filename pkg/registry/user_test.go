package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

type accountEntry struct {
	name, body string
	kind       byte
}

func accountArchive(t *testing.T, entries []accountEntry, compression string) []byte {
	t.Helper()
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	for _, e := range entries {
		kind := e.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Mode: 0644, Typeflag: kind, Size: int64(len(e.body))}
		if kind == tar.TypeSymlink {
			h.Linkname = "elsewhere"
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	switch compression {
	case "gzip":
		w := gzip.NewWriter(&out)
		_, _ = w.Write(raw.Bytes())
		_ = w.Close()
	case "zstd":
		w, err := zstd.NewWriter(&out)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(raw.Bytes())
		_ = w.Close()
	default:
		return raw.Bytes()
	}
	return out.Bytes()
}

func TestBuildMetadataNamedUser(t *testing.T) {
	base := []accountEntry{{name: "etc/passwd", body: "root:x:0:0::/:/bin/sh\nnode:x:1000:1000::/home/node:/bin/sh\n"}, {name: "etc/group", body: "node:x:1000:\napp:x:2000:\n"}}
	cases := []struct {
		name, user, compression string
		upper                   []accountEntry
		want                    *ImageUser
	}{
		{name: "node", user: "node", compression: "gzip", want: &ImageUser{1000, 1000}},
		{name: "zstd", user: "node", compression: "zstd", want: &ImageUser{1000, 1000}},
		{name: "named group", user: "node:app", want: &ImageUser{1000, 2000}},
		{name: "numeric group", user: "node:42", want: &ImageUser{1000, 42}},
		{name: "numeric user named group", user: "1001:app", want: &ImageUser{1001, 2000}},
		{name: "numeric user", user: "1000"},
		{name: "root", user: "root"},
		{name: "numeric root", user: "0:app"},
		{name: "missing", user: "missing"},
		{name: "missing group", user: "node:missing"},
		{name: "override", user: "node", upper: []accountEntry{{name: "./etc/passwd", body: "node:x:1234:5678::/:/bin/sh\n"}}, want: &ImageUser{1234, 5678}},
		{name: "whiteout", user: "node", upper: []accountEntry{{name: "etc/.wh.passwd"}}},
		{name: "opaque", user: "node", upper: []accountEntry{{name: "etc/.wh..wh..opq"}}},
		{name: "deleted directory", user: "node", upper: []accountEntry{{name: ".wh.etc"}}},
		{name: "symlink", user: "node", upper: []accountEntry{{name: "etc/passwd", kind: tar.TypeSymlink}}},
		{name: "new file alongside whiteout", user: "node", upper: []accountEntry{{name: "etc/passwd", body: "node:x:1234:5678::/:/bin/sh\n"}, {name: "etc/.wh..wh..opq"}}, want: &ImageUser{1234, 5678}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			media := "application/vnd.oci.image.layer.v1.tar"
			if tc.compression != "" {
				media += "+" + tc.compression
			}
			blobs := map[string][]byte{"base": accountArchive(t, base, tc.compression), "upper": accountArchive(t, tc.upper, tc.compression)}
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v2/app/manifests/sha256:image":
					_ = json.NewEncoder(w).Encode(map[string]any{"config": map[string]any{"digest": "config"}, "layers": []imageLayer{{Digest: "base", MediaType: media}, {Digest: "upper", MediaType: media}}})
				case "/v2/app/blobs/config":
					_ = json.NewEncoder(w).Encode(map[string]any{"config": map[string]any{"User": tc.user}})
				default:
					blob, ok := blobs[strings.TrimPrefix(r.URL.Path, "/v2/app/blobs/")]
					if !ok {
						http.NotFound(w, r)
						return
					}
					_, _ = w.Write(blob)
				}
			}))
			defer srv.Close()
			c := &Client{Host: strings.TrimPrefix(srv.URL, "https://"), HTTP: srv.Client()}
			got, err := c.BuildMetadata(context.Background(), "app", "sha256:image")
			if err != nil {
				t.Fatal(err)
			}
			if got.UserError != nil {
				t.Fatal(got.UserError)
			}
			if (got.User == nil) != (tc.want == nil) || (got.User != nil && *got.User != *tc.want) {
				t.Fatalf("user = %+v, want %+v", got.User, tc.want)
			}
		})
	}
}
