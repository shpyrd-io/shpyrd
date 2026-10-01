package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/project"
)

// A shell into a resource (RFC-0052): `shpyrd pg psql`, `shpyrd redis cli`
// signed in with `shpyrd login`. The extension that owns the kind names
// the pod, the container and the command (ext.Shellable); the ticket
// carries them and the web terminal's socket (RFC-0026) bridges the rest.

// shellableFor finds the enabled extension that owns kind and takes a
// shell into it.
func (s *Server) shellableFor(kind string) (ext.Shellable, string, bool) {
	for _, x := range s.opts.Extensions {
		sh, ok := x.(ext.Shellable)
		if !ok {
			continue
		}
		for _, t := range x.Types() {
			if strings.EqualFold(t.Kind, kind) || strings.EqualFold(t.Resource, kind) {
				return sh, t.Kind, true
			}
		}
	}
	return nil, "", false
}

// mintResourceShellTicket is POST /api/projects/:slug/resources/:kind/:name/shell/ticket.
func (s *Server) mintResourceShellTicket(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	sh, kind, ok := s.shellableFor(c.Param("kind"))
	if !ok {
		abort(c, http.StatusNotFound, fmt.Errorf("no shell into resources of kind %q", c.Param("kind")))
		return
	}
	name := c.Param("name")
	if !project.ValidSlug(name) {
		abort(c, http.StatusBadRequest, errors.New("invalid resource name"))
		return
	}
	// ?arg=... (repeatable) is what follows the resource's name on the
	// command line: psql's or the engine CLI's own arguments.
	args := c.QueryArray("arg")
	if len(strings.Join(args, " ")) > 4096 {
		abort(c, http.StatusBadRequest, errors.New("the arguments are too long"))
		return
	}
	target, err := sh.ResourceShell(c.Request.Context(), s.deps(), kind, app.Namespace, name, args)
	if err != nil {
		abort(c, http.StatusNotFound, err)
		return
	}
	id, _ := ext.IdentityFrom(c)
	instance := strings.ToLower(kind) + "/" + name
	slug := project.SlugOf(app)
	if s.shells.held(actorKey(id), slug+"\x00"+instance) {
		abort(c, http.StatusConflict, fmt.Errorf("you already have a shell open on %s; close it first", instance))
		return
	}
	code, err := s.execTickets.mint(execTicket{
		Identity: id, Project: slug, Instance: instance,
		Pod: target.Pod, Container: target.Container, Command: target.Command,
	})
	if errors.Is(err, errTicketsFull) {
		c.Header("Retry-After", "30")
		abort(c, http.StatusServiceUnavailable, err)
		return
	}
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ticket": code, "instance": instance})
}
