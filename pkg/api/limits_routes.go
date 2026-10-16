package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The routes behind the usage bars (storage plan step 4): a project's
// storage against the ceiling that applies to it, and the caps a
// workspace's administrator gives its projects.

// projectUsage is GET /api/projects/:slug/usage: the project's block
// storage and the ceiling it is held to, its own cap or the workspace's.
func (s *Server) projectUsage(c *gin.Context) {
	if _, ok := s.projectNamespace(c); !ok {
		return
	}
	u := s.projectUsageOf(c.Request.Context(), s.workspace(c), c.Param("slug"))
	if u == nil {
		abort(c, http.StatusBadGateway, errors.New("the project's usage could not be read"))
		return
	}
	c.JSON(http.StatusOK, u)
}

// ProjectCapsRequest is PUT /api/workspace/project-caps: every project's
// own cap, replaced; a project left out has none.
type ProjectCapsRequest struct {
	ProjectCaps map[string]store.ProjectCap `json:"projectCaps"`
}

// putProjectCaps sets the projects' own ceilings. They are the customer's
// choice below the workspace's ceilings and survive a change of plan.
func (s *Server) putProjectCaps(c *gin.Context) {
	var req ProjectCapsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	caps, err := NormalizeProjectCaps(req.ProjectCaps)
	if err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	ctx := c.Request.Context()
	ws := s.workspace(c)
	for slug := range caps {
		if _, err := s.findApp(ctx, ws, slug); err != nil {
			abort(c, http.StatusBadRequest, errors.New("project "+slug+" is not one of the workspace's"))
			return
		}
	}
	w, err := s.store.Workspace(ctx, ws)
	if err != nil {
		storeErr(c, err, "workspace")
		return
	}
	settings := w.Settings
	settings.ProjectCaps = caps
	if w, err = s.store.UpdateWorkspaceSettings(ctx, ws, settings); err != nil {
		storeErr(c, err, "workspace")
		return
	}
	s.forgetTenants()
	s.audit(c, "", "workspace.project-caps", ws, capsDetail(caps))
	view := s.workspaceView(c, w)
	view.Limits, view.ProjectCaps, view.Sleep = w.Settings.Limits, w.Settings.ProjectCaps, w.Settings.Sleep
	view.Usage = s.usageOf(ctx, ws)
	c.JSON(http.StatusOK, view)
}

// capsDetail is what the audit log says about the caps.
func capsDetail(caps map[string]store.ProjectCap) string {
	if len(caps) == 0 {
		return "no project caps"
	}
	out := ""
	for slug, cap := range caps {
		if out != "" {
			out += ", "
		}
		out += slug + " storage=" + cap.Storage
	}
	return out
}
