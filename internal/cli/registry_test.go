package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// TestServerRequestSendsAWriteOnce: client-go sends a request again by
// itself when the answer is a 5xx with Retry-After. A create that reached
// the server and is sent again finds its own workspace in the way, and the
// person reads that the address is taken although it was created (#90).
func TestServerRequestSendsAWriteOnce(t *testing.T) {
	var posts, gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/proxy/") {
			http.NotFound(w, r) // the admin token's Secret
			return
		}
		switch r.Method {
		case http.MethodPost:
			posts.Add(1)
		case http.MethodGet:
			gets.Add(1)
		}
		w.Header().Set("Retry-After", "1")
		http.Error(w, `{"error":"try again"}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	cs, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	k := &kube.Client{Kube: cs}
	prev := preferKubeconfig
	preferKubeconfig = true
	defer func() { preferKubeconfig = prev }()

	if _, err := serverRequest(context.Background(), k, "POST", "api/workspaces", []byte(`{}`), "application/json"); err == nil {
		t.Fatal("POST answered 503: want the error")
	}
	if n := posts.Load(); n != 1 {
		t.Errorf("POST sent %d times, want once", n)
	}
	// A read is still worth a second try (one is enough to see it).
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if _, err := serverRequest(ctx, k, "GET", "api/workspaces", nil, ""); err == nil {
		t.Fatal("GET answered 503: want the error")
	}
	if n := gets.Load(); n < 2 {
		t.Errorf("GET sent %d times, want it tried again", n)
	}
}
