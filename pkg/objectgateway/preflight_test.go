package objectgateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
)

func TestConditionalWritePreflight(t *testing.T) {
	for _, ignored := range []string{"", "If-Match", "If-None-Match"} {
		t.Run("ignored="+ignored, func(t *testing.T) {
			exists := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					exists = false
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if r.Method != http.MethodPut {
					t.Errorf("unexpected method: %s", r.Method)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				stale := ignored != "If-Match" && r.Header.Get("If-Match") != "" && r.Header.Get("If-Match") != `"test-etag"`
				collision := ignored != "If-None-Match" && r.Header.Get("If-None-Match") == "*" && exists
				if stale || collision {
					w.WriteHeader(http.StatusPreconditionFailed)
					fmt.Fprint(w, `<Error><Code>PreconditionFailed</Code></Error>`)
					return
				}
				exists = true
				w.Header().Set("ETag", `"test-etag"`)
			}))
			defer srv.Close()
			store, err := objectstore.NewS3(srv.URL, "us-east-1", "test", "", "test", "test-secret")
			if err != nil {
				t.Fatal(err)
			}
			err = (&Records{Store: store}).CheckConditionalWrites(context.Background())
			if (err == nil) != (ignored == "") {
				t.Fatalf("ignored=%q, preflight error=%v", ignored, err)
			}
			if exists {
				t.Error("preflight left an object behind")
			}
		})
	}
}
