package authlocal

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The login methods of the workspace (RFC-0033 phase 3): the Workspace
// page lists, adds and removes Dex connectors through these routes, and
// the sign-in page gains or loses the button at once — no server restart.
// At the console the routes manage the platform's methods (offered to
// every workspace); at a workspace host they manage that workspace's own
// (per-workspace SSO), which only its login page shows.

type connectorHandlers struct {
	deps   ext.Deps
	issuer string
	// scoped says the handlers manage the request's workspace's own
	// methods rather than the platform's.
	scoped bool
}

// scope is the connectors' owner for this request: "" for the platform.
func (h *connectorHandlers) scope(c *gin.Context) string {
	if !h.scoped {
		return ""
	}
	ws := ext.WorkspaceFrom(c)
	if ws == store.DefaultWorkspace {
		return "" // the implicit workspace is the platform
	}
	return ws
}

func (h *connectorHandlers) store() *ConnectorStore {
	return &ConnectorStore{Dynamic: h.deps.Kube.Dynamic, Namespace: h.deps.SystemNamespace, Issuer: h.issuer}
}

// LoginMethods is GET /api/auth/connectors: the connectors plus the local
// password method, so the page shows every way in.
type LoginMethods struct {
	Password   bool        `json:"password"`
	Connectors []Connector `json:"connectors"`
	Kinds      []string    `json:"kinds"`
	// Callback is the redirect URI to register at the provider.
	Callback string `json:"callback"`
	// Workspace is the slug whose own methods these are; empty for the
	// platform's.
	Workspace string `json:"workspace,omitempty"`
}

func (h *connectorHandlers) list(c *gin.Context) {
	scope := h.scope(c)
	out := LoginMethods{Password: scope == "", Connectors: []Connector{}, Kinds: ConnectorKinds, Callback: strings.TrimRight(h.issuer, "/") + "/callback", Workspace: scope}
	if h.deps.Kube != nil && h.deps.Kube.Dynamic != nil {
		list, err := h.store().ListFor(c.Request.Context(), scope)
		if err != nil && !errors.Is(err, ErrNotEnabled) {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		if list != nil {
			out.Connectors = list
		}
	}
	c.JSON(http.StatusOK, out)
}

// connectorRequest is POST /api/auth/connectors.
type connectorRequest struct {
	Type         string `json:"type" binding:"required"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	ClientID     string `json:"clientId" binding:"required"`
	ClientSecret string `json:"clientSecret" binding:"required"`
	Org          string `json:"org"`
	HostedDomain string `json:"hostedDomain"`
	Tenant       string `json:"tenant"`
	Issuer       string `json:"issuer"`
}

func (h *connectorHandlers) add(c *gin.Context) {
	var req connectorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	spec := ConnectorSpec{Type: req.Type, ID: req.ID, Name: req.Name, Workspace: h.scope(c), ClientID: strings.TrimSpace(req.ClientID), ClientSecret: strings.TrimSpace(req.ClientSecret),
		Org: strings.TrimSpace(req.Org), HostedDomain: strings.ToLower(strings.TrimSpace(req.HostedDomain)), Tenant: strings.TrimSpace(req.Tenant), Issuer: strings.TrimSpace(req.Issuer)}
	if err := spec.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.deps.Kube == nil || h.deps.Kube.Dynamic == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "no cluster"})
		return
	}
	existed, err := h.store().Add(c.Request.Context(), spec)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	// The button appears now: register the provider with the same Dex
	// client the password method uses.
	if h.deps.Auth != nil {
		sec, err := h.deps.Kube.Kube.CoreV1().Secrets(h.deps.SystemNamespace).Get(c.Request.Context(), install.OIDCClientSecretName, metav1.GetOptions{})
		if err == nil {
			clientID, secret := strings.TrimSpace(string(sec.Data["client-id"])), strings.TrimSpace(string(sec.Data["client-secret"]))
			full := FullConnectorID(spec.Workspace, spec.ID)
			if err := h.deps.Auth.AddOIDC(c.Request.Context(), ext.OIDCProvider{
				ID: full, Label: spec.Name, Kind: spec.Type, ConnectorID: full, Issuer: h.issuer, ClientID: clientID, ClientSecret: secret, Workspace: spec.Workspace,
			}); err != nil {
				c.JSON(http.StatusBadGateway, gin.H{"error": "connector saved, but the sign-in page could not register it: " + err.Error()})
				return
			}
		}
	}
	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}
	c.JSON(status, Connector{ID: spec.ID, FullID: FullConnectorID(spec.Workspace, spec.ID), Type: spec.Type, Name: spec.Name, Workspace: spec.Workspace})
}

func (h *connectorHandlers) remove(c *gin.Context) {
	id := c.Param("id")
	scope := h.scope(c)
	if h.deps.Kube == nil || h.deps.Kube.Dynamic == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "no cluster"})
		return
	}
	if err := h.store().Remove(c.Request.Context(), scope, id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if h.deps.Auth != nil {
		h.deps.Auth.RemoveOIDC(FullConnectorID(scope, id))
	}
	c.Status(http.StatusNoContent)
}
