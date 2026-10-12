package api

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIntegrationIsolation(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	routes := routeGroups{s: s}
	routes.Internal().GET("/internal-proof", func(c *gin.Context) { c.Status(204) })
	routes.External().POST("/webhook-proof", func(c *gin.Context) { c.Status(202) })
	for name, handler := range map[string]http.Handler{"internal": s.InternalAPIHandler(), "public": s.PublicAPIHandler()} {
		for _, path := range []string{"/api/projects", "/edge/auth", "/api/tokens", "/api/config", "/", "/api/sources/archive"} {
			req := httptest.NewRequest("GET", path, nil)
			req.Header.Set("Authorization", "Bearer "+testToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != 404 {
				t.Errorf("%s %s = %d", name, path, rec.Code)
			}
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
		if rec.Code != 200 || rec.Header().Get("X-Request-ID") == "" {
			t.Fatalf("%s health: %d", name, rec.Code)
		}
	}
	for _, tc := range []struct {
		handler      http.Handler
		method, path string
		want         int
	}{
		{s.InternalAPIHandler(), "GET", "/api/internal-proof", 204},
		{s.PublicAPIHandler(), "GET", "/api/internal-proof", 404},
		{s.Handler(), "GET", "/api/internal-proof", 404},
		{s.PublicAPIHandler(), "POST", "/api/webhook-proof", 202},
		{s.InternalAPIHandler(), "POST", "/api/webhook-proof", 404},
		{s.Handler(), "POST", "/api/webhook-proof", 404},
	} {
		rec := httptest.NewRecorder()
		tc.handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("%s %s: %d want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}
}
