package api

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"net/http"
)

// bindPrincipal is an explicit administrator action, never an email-login side
// effect. The configured provider supplies the trusted issuer and connector.
func (s *Server) bindPrincipal(c *gin.Context) {
	var req struct {
		Provider string `json:"provider"`
		Subject  string `json:"subject"`
	}
	if c.ShouldBindJSON(&req) != nil || req.Subject == "" || len(req.Subject) > 512 {
		abort(c, 400, errors.New("provider and upstream subject are required"))
		return
	}
	if s.rp == nil {
		abort(c, 400, errors.New("no identity provider configured"))
		return
	}
	p := s.rp.provider(req.Provider)
	if p == nil || !s.offersAt(c, p.ID) {
		abort(c, 400, errors.New("provider is not available at this door"))
		return
	}
	ws, realm := s.workspace(c), store.RealmWorkspace
	if s.atConsole(c) {
		ws = s.defaultSlug(c.Request.Context())
		realm = store.RealmConsole
	}
	person, err := s.store.GetIdentity(c.Request.Context(), ws, c.Param("id"))
	if err != nil || person.ID != c.Param("id") || person.Realm != realm {
		abort(c, 404, errors.New("principal not found in this realm"))
		return
	}
	issuer := p.Issuer
	if p.UpstreamIssuer != "" {
		issuer = p.UpstreamIssuer
	}
	if err := s.store.BindIdentity(c.Request.Context(), ws, person.ID, issuer, req.Subject, p.ConnectorID); err != nil {
		storeErr(c, err, "identity binding")
		return
	}
	s.membershipChanged()
	s.audit(c, "", "identity.bind", person.ID, "provider "+p.ID+", subject "+req.Subject)
	c.Status(http.StatusNoContent)
}

func (s *Server) consolePrincipals(c *gin.Context) {
	people, err := s.store.ListIdentities(c.Request.Context(), s.defaultSlug(c.Request.Context()))
	if err != nil {
		abort(c, 502, err)
		return
	}
	out := []store.Identity{}
	for _, p := range people {
		if p.Realm == store.RealmConsole {
			out = append(out, p)
		}
	}
	c.JSON(200, out)
}
