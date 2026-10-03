package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// This is the actual maintenance upstream, not an auth_request response.
// nginx converts authentication errors into 500s; routing here preserves 503
// for webhooks and API clients, including requests without a user session.
func projectMaintenanceResponse(c *gin.Context) {
	c.Header("Retry-After", "60")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Shpyrd-Maintenance", "true")
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": "project is temporarily unavailable for maintenance; retry later"})
}
