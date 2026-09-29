package authlocal

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/install"
)

// The login methods of a door (RFC-0033 phase 3, RFC-0080): the Sign-in
// pages list, add and remove Dex connectors through these routes, and the
// login page gains or loses the button at once — no server restart. At
// the console the routes manage the console's own methods, or with
// ?scope=platform the defaults every workspace offers; at a workspace host
// they manage that workspace's own, which only its login page shows.

type connectorHandlers struct {
	deps   ext.Deps
	issuer string
	// scoped says the handlers manage the request's door's methods rather
	// than the platform's defaults (the CLI's route).
	scoped bool
}

// scope is the connectors' owner for this request: the realm and, for a
// workspace, its short id.
func (h *connectorHandlers) scope(c *gin.Context) (realm, workspace string) {
	if !h.scoped {
		return ext.RealmPlatform, ""
	}
	ws := ext.WorkspaceObjectFrom(c)
	if ws == nil {
		// The console: its own methods, or the platform's defaults on request.
		if c.Query("scope") == ext.RealmPlatform {
			return ext.RealmPlatform, ""
		}
		return ext.RealmConsole, ""
	}
	return ext.RealmWorkspace, ids.Short(ws.ID)
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
	// Realm is the door these methods belong to (RFC-0080); Workspace the
	// short id of the workspace for "workspace".
	Realm     string `json:"realm"`
	Workspace string `json:"workspace,omitempty"`
}

func (h *connectorHandlers) list(c *gin.Context) {
	realm, ws := h.scope(c)
	out := LoginMethods{Password: realm != ext.RealmWorkspace, Connectors: []Connector{}, Kinds: ConnectorKinds, Callback: strings.TrimRight(h.issuer, "/") + "/callback", Realm: realm, Workspace: ws}
	if h.deps.Kube != nil && h.deps.Kube.Dynamic != nil {
		list, err := h.store().ListFor(c.Request.Context(), realm, ws)
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
	realm, ws := h.scope(c)
	spec := ConnectorSpec{Type: req.Type, ID: req.ID, Name: req.Name, Realm: realm, Workspace: ws, ClientID: strings.TrimSpace(req.ClientID), ClientSecret: strings.TrimSpace(req.ClientSecret),
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
			full := FullConnectorID(realm, spec.Workspace, spec.ID)
			if err := h.deps.Auth.AddOIDC(c.Request.Context(), ext.OIDCProvider{
				ID: full, Label: spec.Name, Kind: spec.Type, ConnectorID: full, Issuer: h.issuer, ClientID: clientID, ClientSecret: secret, Realm: realm, Workspace: spec.Workspace,
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
	c.JSON(status, Connector{ID: spec.ID, FullID: FullConnectorID(realm, spec.Workspace, spec.ID), Type: spec.Type, Name: spec.Name, Realm: realm, Workspace: spec.Workspace})
}

func (h *connectorHandlers) remove(c *gin.Context) {
	id := c.Param("id")
	realm, ws := h.scope(c)
	if h.deps.Kube == nil || h.deps.Kube.Dynamic == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "no cluster"})
		return
	}
	// A workspace that hides the platform's methods keeps at least one of
	// its own: removing the last one would leave a login page with no
	// door at all (RFC-0033; the switch itself is guarded the same way).
	if realm == ext.RealmWorkspace {
		if wso := ext.WorkspaceObjectFrom(c); wso != nil {
			if wso.Settings.OwnMethodsOnly {
				own, err := h.store().ListFor(c.Request.Context(), realm, ws)
				if err == nil && len(own) <= 1 {
					c.JSON(http.StatusBadRequest, gin.H{"error": "this is the workspace's only sign-in method and the platform's are switched off; offer the platform's methods again first (Sign-in › Also offer the platform's methods), or add another method before removing this one"})
					return
				}
			}
			// A claimed email domain routed to this method would lock its
			// accounts out: the claim must point elsewhere first.
			if h.deps.Store != nil {
				full := FullConnectorID(realm, ws, id)
				if claims, err := h.deps.Store.ListDomainClaims(c.Request.Context(), wso.Slug); err == nil {
					for _, d := range claims {
						if d.Connector == full {
							c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("accounts of %s are routed to this method (Sign-in › Company domains); route the domain to another method or to any first", d.Domain)})
							return
						}
					}
				}
			}
		}
	}
	if err := h.store().Remove(c.Request.Context(), realm, ws, id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if h.deps.Auth != nil {
		h.deps.Auth.RemoveOIDC(FullConnectorID(realm, ws, id))
	}
	c.Status(http.StatusNoContent)
}
