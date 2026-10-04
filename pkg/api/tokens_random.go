package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

// oauthRandom is n random bytes, base64url: codes and secrets.
func oauthRandom(n int) string {
	raw := make([]byte, n)
	_, _ = rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// hashToken is what the store keeps of a secret: its SHA-256, hex.
func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// validMCPName checks the name a workspace gives its MCP server (PATCH
// /api/workspace): what an assistant shows for it (RFC-0032).
func validMCPName(name string) error {
	name = strings.TrimSpace(name)
	if len(name) > 60 {
		return fmt.Errorf("the MCP name is at most 60 characters")
	}
	if strings.ContainsAny(name, "\n\r\t") {
		return fmt.Errorf("the MCP name is one line")
	}
	return nil
}

// sessionIDOf is the request's dashboard session cookie, "" without one.
func sessionIDOf(c *gin.Context) string {
	sid, _ := c.Cookie(sessionCookie)
	return sid
}
