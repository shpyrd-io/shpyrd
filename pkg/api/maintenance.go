package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/pages"
	"github.com/shpyrd-io/shpyrd/pkg/project"
)

// maintenanceText is what the page says under the project's name when the
// mode carries no message of its own, and during a project operation.
const maintenanceText = "It is being worked on and will be back shortly. This page opens it again by itself."

// maintenanceMessageMax bounds the message a person writes for the page.
const maintenanceMessageMax = 500

// projectMaintenance answers every request at the hosts of a project whose
// HTTP is in maintenance: the Ingress rewrites them here, Host header
// intact, during a project operation (RFC-0081) and in maintenance mode
// (RFC-0020). This is the actual upstream, not an auth_request response:
// nginx converts authentication errors into 500s; routing here preserves
// 503 for webhooks and API clients, including requests without a user
// session. A browser gets the page, with the project's name and its
// message; a program the JSON, with the headers the operation's
// confirmation check looks for.
func (s *Server) projectMaintenance(c *gin.Context) {
	title, text, retry := "Under maintenance", maintenanceText, "60"
	if app, err := s.appByHost(c, c.Request.Host); err == nil {
		title = project.DisplayName(app) + " is under maintenance"
		if m := app.Spec.Maintenance; m != nil && m.Enabled {
			// A person's window is longer than an operation's; crawlers
			// take any 503 as temporary whatever the number says.
			retry = "300"
			if msg := strings.TrimSpace(m.Message); msg != "" {
				text = msg
			}
		}
	}
	c.Header("Retry-After", retry)
	c.Header("Cache-Control", "no-store")
	c.Header("X-Shpyrd-Maintenance", "true")
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if apiClient(c) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "project is temporarily unavailable for maintenance; retry later"})
		return
	}
	if hash := pages.ScriptHash(pages.ServiceUnavailable); hash != "" {
		c.Header("Content-Security-Policy", basePolicy+"; script-src "+hash)
	}
	// The page opens the app again by itself, once a minute: nobody has to
	// know when the window ends.
	c.Data(http.StatusServiceUnavailable, "text/html; charset=utf-8", []byte(pages.HTML(pages.ServiceUnavailable, pages.Page{Title: title, Text: text, Refresh: 60})))
}

// maintenanceRequest turns the maintenance mode on or off (RFC-0020).
type maintenanceRequest struct {
	Enabled bool   `json:"enabled"`
	Message string `json:"message"`
}

// setMaintenance is PUT /api/projects/:slug/maintenance. Turning the mode
// on records when and by whom; turning it on again only changes the
// message. Off clears it all. A project under an operation refuses, as it
// refuses every write.
func (s *Server) setMaintenance(c *gin.Context) {
	var req maintenanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if len(req.Message) > maintenanceMessageMax {
		abort(c, http.StatusBadRequest, errors.New("the message is at most 500 characters"))
		return
	}
	by := "anonymous"
	if id, ok := ext.IdentityFrom(c); ok {
		by = actorName(id)
	}
	app, err := s.mutateApp(c, func(a *shpyrdv1.App) error {
		if !req.Enabled {
			a.Spec.Maintenance = nil
			return nil
		}
		m := a.Spec.Maintenance
		if m == nil || !m.Enabled {
			now := metav1.Now()
			m = &shpyrdv1.Maintenance{Since: &now, By: by}
		}
		m.Enabled, m.Message = true, req.Message
		a.Spec.Maintenance = m
		return nil
	})
	if err != nil {
		return
	}
	detail := "off"
	if req.Enabled {
		detail = "on"
		if req.Message != "" {
			detail += ": " + req.Message
		}
	}
	s.audit(c, project.SlugOf(app), "maintenance", project.SlugOf(app), detail)
	c.JSON(http.StatusOK, summarize(app))
}
