package objectgateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
)

// probeStore is an S3 endpoint for the preflight alone, honouring every
// condition but the one named in ignored ("" for a full provider); exists
// reports whether the probe object is there.
func probeStore(t *testing.T, ignored string) (*objectstore.S3, *atomic.Bool) {
	t.Helper()
	var exists atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			exists.Store(false)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPut {
			t.Errorf("unexpected method: %s", r.Method)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		stale := ignored != "If-Match" && r.Header.Get("If-Match") != "" && r.Header.Get("If-Match") != `"test-etag"`
		collision := ignored != "If-None-Match" && r.Header.Get("If-None-Match") == "*" && exists.Load()
		if stale || collision {
			w.WriteHeader(http.StatusPreconditionFailed)
			fmt.Fprint(w, `<Error><Code>PreconditionFailed</Code></Error>`)
			return
		}
		exists.Store(true)
		w.Header().Set("ETag", `"test-etag"`)
	}))
	t.Cleanup(srv.Close)
	store, err := objectstore.NewS3(srv.URL, "us-east-1", "test", "", "test", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	return store, &exists
}

// The preflight tells a provider that honours both conditions from one that
// honours only If-None-Match (OCI Object Storage) and from one that honours
// neither, and leaves no probe behind.
func TestConditionalWritePreflight(t *testing.T) {
	for _, c := range []struct {
		ignored            string
		full, creationOnly bool
	}{{"", true, false}, {"If-Match", false, true}, {"If-None-Match", false, false}} {
		t.Run("ignored="+c.ignored, func(t *testing.T) {
			store, exists := probeStore(t, c.ignored)
			err := (&Records{Store: store}).CheckConditionalWrites(context.Background())
			if (err == nil) != c.full || errors.Is(err, ErrIfMatchIgnored) != c.creationOnly {
				t.Fatalf("ignored=%q, preflight error=%v", c.ignored, err)
			}
			if exists.Load() {
				t.Error("preflight left an object behind")
			}
		})
	}
}

// A provider that ignores If-Match passes the preflight for a single writer
// alone, with a note saying so; for anyone else the error names the setting.
// A provider that ignores If-None-Match never passes.
func TestPreflightAcceptsCreationOnlyForASingleWriter(t *testing.T) {
	ctx := context.Background()
	store, _ := probeStore(t, "If-Match")
	note, err := (&Records{Store: store, SingleWriter: true}).Preflight(ctx)
	if err != nil || !strings.Contains(note, "If-Match") || !strings.Contains(note, "SHPYRD_GATEWAY_SINGLE_WRITER") {
		t.Fatalf("single writer on a provider that ignores If-Match: note=%q err=%v", note, err)
	}
	note, err = (&Records{Store: store}).Preflight(ctx)
	if !errors.Is(err, ErrIfMatchIgnored) || !strings.Contains(err.Error(), "SHPYRD_GATEWAY_SINGLE_WRITER") || note != "" {
		t.Fatalf("two writers on a provider that ignores If-Match: note=%q err=%v", note, err)
	}
	store, _ = probeStore(t, "If-None-Match")
	if note, err := (&Records{Store: store, SingleWriter: true}).Preflight(ctx); err == nil || errors.Is(err, ErrIfMatchIgnored) || note != "" {
		t.Fatalf("a provider that ignores If-None-Match must be refused: note=%q err=%v", note, err)
	}
	store, _ = probeStore(t, "")
	if note, err := (&Records{Store: store, SingleWriter: true}).Preflight(ctx); err != nil || note != "" {
		t.Fatalf("a full provider passes without a note: note=%q err=%v", note, err)
	}
}
