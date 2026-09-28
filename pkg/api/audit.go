package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/audit"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// audit records a mutation performed through the API. project is "" for
// cluster-level actions. Failures are logged, never returned: auditing must
// not break the action.
func (s *Server) audit(c *gin.Context, project, action, target, detail string) {
	if s.kube == nil || s.kube.Kube == nil {
		return
	}
	actor, realm := "anonymous", ""
	if id, ok := ext.IdentityFrom(c); ok {
		actor = firstNonEmpty(id.Email, id.Name, id.Subject)
		realm = realmOf(id)
		switch id.Provider {
		case "token":
			actor = "admin token"
		case "api-token":
			// joao@acme.test (token ci): who, and which credential acted.
			actor = fmt.Sprintf("%s (token %s)", id.Email, id.Name)
			if id.Email == "" {
				actor = fmt.Sprintf("admin token (token %s)", id.Name)
			}
		}
	}
	// Events attach to the App when it exists (RFC-0076: found by slug,
	// named by id); an action on a project that is gone (destroy) is
	// recorded against the cluster, the target still naming it.
	ref := audit.ClusterRef(s.deps().SystemNamespace)
	if project != "" {
		if app, err := s.findApp(c.Request.Context(), s.workspace(c), project); err == nil {
			ref = audit.AppRefIn(app.Namespace, app.Name)
		}
	}
	entry := audit.Entry{Actor: actor, Action: action, Target: target, Detail: detail, From: c.ClientIP(), Via: "api", Realm: realm}
	if err := audit.Record(c.Request.Context(), s.kube.Kube, ref, entry); err != nil {
		s.log.Warn("audit: cannot record", "action", action, "error", err)
	}
}

// auditActor records an action on behalf of a named person when the
// request itself carries no identity (the OAuth token endpoint, where the
// client speaks for the person who consented).
func (s *Server) auditActor(c *gin.Context, actor, action, target, detail string) {
	if s.kube == nil || s.kube.Kube == nil {
		return
	}
	entry := audit.Entry{Actor: firstNonEmpty(actor, "anonymous"), Action: action, Target: target, Detail: detail, From: c.ClientIP(), Via: "api", Realm: "workspace"}
	if err := audit.Record(c.Request.Context(), s.kube.Kube, audit.ClusterRef(s.deps().SystemNamespace), entry); err != nil {
		s.log.Warn("audit: cannot record", "action", action, "error", err)
	}
}

// auditAnonymous records a security event of an unauthenticated client.
func (s *Server) auditAnonymous(c *gin.Context, action, detail string) {
	s.auditFailure(c, action, c.ClientIP(), detail)
}

// auditFailure records a failed attempt against a named target (an account
// that was tried) by an unauthenticated client.
func (s *Server) auditFailure(c *gin.Context, action, target, detail string) {
	if s.kube == nil || s.kube.Kube == nil {
		return
	}
	entry := audit.Entry{Actor: "anonymous", Action: action, Target: target, Detail: detail, From: c.ClientIP(), Via: "api"}
	if err := audit.Record(c.Request.Context(), s.kube.Kube, audit.ClusterRef(s.deps().SystemNamespace), entry); err != nil {
		s.log.Warn("audit: cannot record", "action", action, "error", err)
	}
}

// appAudit lists the audit trail of a project.
func (s *Server) appAudit(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	entries, err := audit.List(c.Request.Context(), s.kube.Kube, audit.AppRefIn(app.Namespace, app.Name), limit)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, entries)
}

// realmOf says where an identity lives (RFC-0033): the operator realm for
// the admin token and kubeconfig sessions (and tokens the admin token
// minted), the workspace for everyone else.
func realmOf(id ext.Identity) string {
	if id.Provider == "token" || id.Provider == "kubeconfig" || id.Subject == "admin-token" || (id.Provider == "api-token" && id.Email == "") {
		return "operator"
	}
	return "workspace"
}
