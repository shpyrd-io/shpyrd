package api

import (
	"net/http"
	"testing"
)

func TestMaintenanceHTTPDoesNotAcknowledgeOrRedirect(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		t.Run(method, func(t *testing.T) {
			r := do(t, s, method, "/_shpyrd/maintenance", "", false)
			if r.Code != http.StatusServiceUnavailable {
				t.Fatalf("got %d: %s", r.Code, r.Body.String())
			}
			if r.Header().Get("Retry-After") != "60" || r.Header().Get("Cache-Control") != "no-store" || r.Header().Get("X-Shpyrd-Maintenance") != "true" {
				t.Fatal(r.Header())
			}
			if r.Header().Get("Location") != "" || r.Header().Get("Set-Cookie") != "" {
				t.Fatal("maintenance must not start a login flow")
			}
			if method == "HEAD" && r.Body.Len() != 0 {
				t.Fatal("HEAD has a body")
			}
		})
	}
}
