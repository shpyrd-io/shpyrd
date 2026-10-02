package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// How a project looks on the launcher: its symbol or the image it sent,
// the domain the card opens, and the teams that have access to it.

// maxIcon is the largest image a project may send for its card.
const maxIcon = 128 * 1024

// iconKinds are the images a project may send: an SVG is drawn as a
// symbol, in the colour chosen; a PNG or a WebP is shown as it is.
var iconKinds = map[string]string{"image/svg+xml": "svg", "image/png": "png", "image/webp": "webp"}

// iconType is the media type of a kind of image.
func iconType(kind string) string {
	for typ, k := range iconKinds {
		if k == kind {
			return typ
		}
	}
	return ""
}

// parseIcon takes a data URL and returns the image and its media type.
func parseIcon(dataURL string) ([]byte, string, error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(dataURL), "data:")
	if !ok {
		return nil, "", errors.New("icon must be a data URL (data:image/svg+xml;base64,...)")
	}
	meta, payload, ok := strings.Cut(rest, ",")
	if !ok || !strings.HasSuffix(meta, ";base64") {
		return nil, "", errors.New("icon must be a base64 data URL")
	}
	typ := strings.TrimSuffix(meta, ";base64")
	if iconKinds[typ] == "" {
		return nil, "", errors.New("icon must be an SVG, a PNG or a WebP image")
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", errors.New("icon is not valid base64")
	}
	if len(raw) == 0 {
		return nil, "", errors.New("icon is empty")
	}
	if len(raw) > maxIcon {
		return nil, "", errors.New("icon must be at most 128 KB")
	}
	return raw, typ, nil
}

// iconURL is where the image a project sent is read, by its version, so
// it may be cached; "" when it sent none.
func iconURL(a *shpyrdv1.App) (string, string) {
	version, kind := project.IconFile(a)
	if version == "" {
		return "", ""
	}
	return "/api/projects/" + url.PathEscape(project.SlugOf(a)) + "/icon?v=" + version, iconType(kind)
}

// domainOf is the first domain of the project's own that answers: its
// DNS points here and its certificate is ready. A domain that does not
// answer yet would open nothing; "" when none does.
func domainOf(a *shpyrdv1.App) string {
	for _, host := range a.Spec.Domains {
		for _, d := range a.Status.Domains {
			if d.Host == host && d.DNS == "ok" && (d.Certificate == "ready" || d.Certificate == "wildcard") {
				return host
			}
		}
	}
	return ""
}

// teamsByProject is, for each project of the workspace (by grant key),
// the names of the teams that have access to it.
func (s *Server) teamsByProject(ctx context.Context, ws string) map[string][]string {
	grants, err := s.store.ListGrants(ctx, ws)
	if err != nil {
		return nil
	}
	out := map[string][]string{}
	for _, g := range grants {
		if g.Team != "" && !slices.Contains(out[g.Project], g.Team) {
			out[g.Project] = append(out[g.Project], g.Team)
		}
	}
	for _, teams := range out {
		sort.Strings(teams)
	}
	return out
}

// projectIcon is GET /api/projects/:slug/icon: the image the project
// sent, cacheable by its version.
func (s *Server) projectIcon(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	if v, _ := project.IconFile(app); v == "" || app.Spec.ID == "" {
		c.Status(http.StatusNotFound)
		return
	}
	raw, typ, err := s.store.ProjectIcon(c.Request.Context(), app.Spec.ID)
	if errors.Is(err, store.ErrNotFound) {
		c.Status(http.StatusNotFound)
		return
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.Header("Cache-Control", "private, max-age=86400")
	c.Header("X-Content-Type-Options", "nosniff")
	// An SVG opened as a document must not run scripts on this origin.
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	c.Data(http.StatusOK, typ, raw)
}

// IconRequest is the image a project sends for its card, as a data URL.
type IconRequest struct {
	Icon string `json:"icon" binding:"required"`
}

// putProjectIcon is PUT /api/projects/:slug/icon: the project's own image
// for its card, in place of a symbol of the set.
func (s *Server) putProjectIcon(c *gin.Context) {
	var req IconRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	raw, typ, err := parseIcon(req.Icon)
	if err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	found, ok := s.loadApp(c)
	if !ok {
		return
	}
	if found.Spec.ID == "" {
		abort(c, http.StatusConflict, errors.New("the project is still being set up; try again in a moment"))
		return
	}
	if err := s.store.SetProjectIcon(c.Request.Context(), found.Spec.ID, raw, typ); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	sum := sha256.Sum256(raw)
	version := hex.EncodeToString(sum[:6])
	app, err := s.mutateApp(c, func(a *shpyrdv1.App) error {
		project.SetIconFile(a, version, iconKinds[typ])
		return nil
	})
	if err != nil {
		return
	}
	s.audit(c, project.SlugOf(app), "project.update", project.Label(app), "icon "+iconKinds[typ])
	c.JSON(http.StatusOK, detail(app, s.buildsByDigest(c.Request.Context(), app)))
}

// deleteProjectIcon is DELETE /api/projects/:slug/icon: the card goes
// back to its symbol.
func (s *Server) deleteProjectIcon(c *gin.Context) {
	found, ok := s.loadApp(c)
	if !ok {
		return
	}
	if found.Spec.ID != "" {
		if err := s.store.SetProjectIcon(c.Request.Context(), found.Spec.ID, nil, ""); err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
	}
	app, err := s.mutateApp(c, func(a *shpyrdv1.App) error {
		project.SetIconFile(a, "", "")
		return nil
	})
	if err != nil {
		return
	}
	s.audit(c, project.SlugOf(app), "project.update", project.Label(app), "icon removed")
	c.JSON(http.StatusOK, detail(app, s.buildsByDigest(c.Request.Context(), app)))
}
