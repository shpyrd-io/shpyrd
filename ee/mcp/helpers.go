//go:build !foss

package mcp

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func abort(c *gin.Context, status int, err error) {
	c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
}

func storeErr(c *gin.Context, err error, what string) {
	if errors.Is(err, store.ErrNotFound) {
		abort(c, http.StatusNotFound, errors.New(what+" not found"))
		return
	}
	abort(c, http.StatusBadGateway, err)
}

// bearerOf is the request's token, as the core reads it: X-Shpyrd-Token,
// else a bearer Authorization.
func bearerOf(c *gin.Context) string {
	tok := c.GetHeader("X-Shpyrd-Token")
	if h := c.GetHeader("Authorization"); tok == "" && len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		tok = strings.TrimSpace(h[7:])
	}
	return tok
}
