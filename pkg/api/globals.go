package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/configvars"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Global config vars (RFC-0016): set once by a workspace's admins, injected
// into every project of the workspace that does not opt out, write-only
// like project vars. The console's routes are the default workspace's, for
// the dashboard of before.

// GlobalsResponse lists the global config var names, never their values,
// and how many projects receive them (each gets a release on a change).
type GlobalsResponse struct {
	Vars []configvars.Var `json:"vars"`
	// Projects is the number of projects that receive at least one global.
	Projects int `json:"projects"`
}

func (s *Server) globalsKeyFor(workspace string) types.NamespacedName {
	return types.NamespacedName{Namespace: s.kube.Namespace, Name: shpyrdv1.GlobalEnvSecretFor(workspace)}
}

// getGlobals is GET /api/globals (console, cluster.admin): the default
// workspace's, for the dashboard of before.
func (s *Server) getGlobals(c *gin.Context) { s.readGlobals(c, store.DefaultWorkspace) }

// putGlobals is PUT /api/globals (console): the default workspace's.
func (s *Server) putGlobals(c *gin.Context) { s.writeGlobals(c, store.DefaultWorkspace) }

// getWorkspaceGlobals is GET /api/workspace/globals: the config vars every
// project of the request's workspace receives, names and when each was set.
func (s *Server) getWorkspaceGlobals(c *gin.Context) { s.readGlobals(c, s.globalsWorkspace(c)) }

// putWorkspaceGlobals is PUT /api/workspace/globals: set and unset names,
// dotenv for bulk paste. The controller mirrors the change into every
// project of the workspace and records a "Global config change" release
// for each.
func (s *Server) putWorkspaceGlobals(c *gin.Context) { s.writeGlobals(c, s.globalsWorkspace(c)) }

// globalsWorkspace is the workspace of the request's door; at the console,
// the default workspace, which is the operator's own.
func (s *Server) globalsWorkspace(c *gin.Context) string {
	if ws := s.workspace(c); ws != "" {
		return ws
	}
	return store.DefaultWorkspace
}

func (s *Server) readGlobals(c *gin.Context, workspace string) {
	sec := &corev1.Secret{}
	err := s.apps.Get(c.Request.Context(), s.globalsKeyFor(workspace), sec)
	if err != nil && !apierrors.IsNotFound(err) {
		abort(c, http.StatusBadGateway, err)
		return
	}
	if err != nil {
		sec = nil
	}
	c.JSON(http.StatusOK, GlobalsResponse{Vars: configvars.List(sec), Projects: s.projectsReceivingGlobals(c.Request.Context(), workspace)})
}

func (s *Server) writeGlobals(c *gin.Context, workspace string) {
	var req ConfigVarsUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	if req.Dotenv != "" {
		parsed, err := configvars.ParseDotenv(req.Dotenv)
		if err != nil {
			abort(c, http.StatusBadRequest, err)
			return
		}
		if req.Set == nil {
			req.Set = map[string]string{}
		}
		for k, v := range parsed {
			req.Set[k] = v
		}
	}
	if len(req.Set) == 0 && len(req.Unset) == 0 {
		abort(c, http.StatusBadRequest, errors.New("nothing to change"))
		return
	}
	ctx := c.Request.Context()
	key := s.globalsKeyFor(workspace)
	for attempt := 0; attempt < 5; attempt++ {
		sec := &corev1.Secret{}
		create := false
		if err := s.apps.Get(ctx, key, sec); err != nil {
			if !apierrors.IsNotFound(err) {
				abort(c, http.StatusBadGateway, err)
				return
			}
			create = true
			sec = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace, Labels: map[string]string{shpyrdv1.LabelManagedBy: "shpyrd"}},
				Type:       corev1.SecretTypeOpaque,
			}
		}
		if err := configvars.Apply(sec, req.Set, req.Unset, time.Now()); err != nil {
			abort(c, http.StatusBadRequest, err)
			return
		}
		var err error
		if create {
			err = s.apps.Create(ctx, sec)
		} else {
			err = s.apps.Update(ctx, sec)
		}
		if err == nil {
			if len(req.Set) > 0 {
				s.audit(c, "", "globals.set", "global config vars", "set "+strings.Join(sortedKeys(req.Set), ", "))
			}
			if len(req.Unset) > 0 {
				s.audit(c, "", "globals.unset", "global config vars", "unset "+strings.Join(req.Unset, ", "))
			}
			c.JSON(http.StatusOK, GlobalsResponse{Vars: configvars.List(sec), Projects: s.projectsReceivingGlobals(ctx, workspace)})
			return
		}
		if !apierrors.IsConflict(err) && !apierrors.IsAlreadyExists(err) {
			abort(c, http.StatusBadGateway, err)
			return
		}
	}
	abort(c, http.StatusConflict, errors.New("too many conflicts"))
}

// projectsReceivingGlobals counts the projects of the workspace that have
// not opted out.
func (s *Server) projectsReceivingGlobals(ctx context.Context, workspace string) int {
	var list shpyrdv1.AppList
	if err := s.apps.List(ctx, &list); err != nil {
		return 0
	}
	n := 0
	for _, a := range list.Items {
		if workspaceOf(&a) == workspace && (a.Spec.Globals == nil || !a.Spec.Globals.Disabled) {
			n++
		}
	}
	return n
}

// globalVars lists the globals a project receives, from its mirror (already
// filtered by the project's opt-out), with when each was set.
func (s *Server) globalVars(ctx context.Context, app *shpyrdv1.App) []configvars.Var {
	sec := &corev1.Secret{}
	if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: shpyrdv1.GlobalEnvSecretName}, sec); err != nil {
		return nil
	}
	return configvars.List(sec)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
