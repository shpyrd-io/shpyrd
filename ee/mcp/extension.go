//go:build !foss

// Package mcp is the enterprise's MCP server (ee/LICENSE; RFC-0032): every
// workspace is a remote Model Context Protocol server at /mcp, with the
// OAuth 2.1 authorization server an assistant such as Claude signs a person
// in through, and the list of connected assistants on the workspace page.
// Without a license in force every route answers 402 and the OAuth access
// tokens stop being accepted.
package mcp

import (
	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/shpyrd-io/shpyrd/ee/licensing"
	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// Name of the extension.
const Name = "mcp"

// server is the MCP and OAuth handlers on the API server's Host.
type server struct{ h api.Host }

type extension struct{}

// New returns the MCP extension.
func New() ext.Extension { return extension{} }

func (extension) Name() string { return Name }
func (extension) Description() string {
	return "Every workspace is an MCP server assistants connect to, with OAuth 2.1"
}
func (extension) Components() []ext.ComponentRef        { return nil }
func (extension) Register(ctrl.Manager, ext.Deps) error { return nil }
func (extension) Routes(ext.Router, ext.Deps) error     { return nil }
func (extension) CLI(ext.CLIGlobals) []*cobra.Command   { return nil }
func (extension) Types() []ext.ResourceType             { return nil }

// MountRoot is api.RootExtension: the well-known documents, the OAuth
// endpoints and /mcp at the workspace's root, the connections on /api, and
// the OAuth access tokens among the ways a request is identified.
func (extension) MountRoot(h api.Host) error {
	s := &server{h: h}
	licensed := licensing.Require()
	root, tenant, login := h.Root(), h.Tenant(), h.Login()
	// The well-known documents sit at the root, with and without the
	// resource path (RFC 8414 §3.1, RFC 9728 §3.1); the endpoints are
	// throttled like sign-in.
	for _, p := range []string{"/.well-known/oauth-authorization-server", "/.well-known/oauth-authorization-server/mcp"} {
		root.GET(p, tenant, licensed, s.oauthMetadata)
	}
	for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		root.GET(p, tenant, licensed, s.protectedResourceMetadata)
	}
	oauth := root.Group("/oauth", tenant, login, licensed)
	oauth.POST("/register", s.oauthRegister)
	oauth.GET("/authorize", s.oauthAuthorize)
	oauth.POST("/authorize", s.oauthDecide)
	oauth.POST("/token", s.oauthToken)
	oauth.POST("/revoke", s.oauthRevoke)
	root.POST("/mcp", tenant, licensed, s.mcpAuth(), s.mcpHandle)
	root.GET("/mcp", tenant, licensed, s.mcpOther)
	root.DELETE("/mcp", tenant, licensed, s.mcpOther)
	// The caller's connected assistants (RFC-0032).
	h.API().GET("/workspace/connections", licensed, s.listConnections)
	h.API().DELETE("/workspace/connections/:id", licensed, s.deleteConnection)
	// An assistant acting for a person, within the token's scope: only
	// while the license is in force.
	h.AddIdentifier(func(c *gin.Context) bool { return licensing.Active() && s.identifyWithOAuth(c) })
	return nil
}

var _ api.RootExtension = extension{}
