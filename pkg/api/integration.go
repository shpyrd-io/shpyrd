package api

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"time"
)

// These routers never mount the dashboard, tenant API or session middleware.
func (s *Server) integrationRouter(surface string) *gin.Engine {
	r := gin.New()
	r.RedirectTrailingSlash = false
	r.RedirectFixedPath = false
	_ = r.SetTrustedProxies(nil)
	r.Use(gin.Recovery(), securityHeaders(), func(c *gin.Context) {
		id := uuid.NewString()
		c.Header("X-Request-ID", id)
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		s.log.Info("integration_request", "surface", surface, "request_id", id, "method", c.Request.Method, "route", route, "status", c.Writer.Status(), "duration_ms", time.Since(start).Milliseconds())
	})
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	return r
}

func (s *Server) InternalAPIHandler() http.Handler { return s.internalAPI }
func (s *Server) PublicAPIHandler() http.Handler   { return s.publicAPI }
