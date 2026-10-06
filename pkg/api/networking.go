package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// InternalExposurePolicy supplies the hosting default. Nil permits internal
// exposure on self-hosted OSS/EE. An operator override takes precedence.
type InternalExposurePolicy func(*store.Workspace) bool

type WorkspaceCapabilities struct {
	InternalExposure bool `json:"internalExposure"`
}

func EffectiveInternalExposure(w *store.Workspace, policy InternalExposurePolicy) bool {
	if w == nil {
		return false
	}
	if w.Settings.InternalExposure != nil {
		return *w.Settings.InternalExposure
	}
	return policy == nil || policy(w)
}

func (s *Server) workspaceCapabilities(w *store.Workspace) WorkspaceCapabilities {
	return WorkspaceCapabilities{InternalExposure: EffectiveInternalExposure(w, s.opts.InternalExposure)}
}

var errInternalExposureUnavailable = errors.New("internal exposure is not enabled for this workspace")

// Check the transition, preserving legacy internal apps and allowing their
// maintenance and explicit move to external. Read the store, not tenant caches.
func (s *Server) checkExposure(ctx context.Context, workspace, before, after string) error {
	if after != "internal" || before == "internal" {
		return nil
	}
	w, err := s.store.Workspace(ctx, workspace)
	if err != nil {
		return err
	}
	if !s.workspaceCapabilities(w).InternalExposure {
		return errInternalExposureUnavailable
	}
	return nil
}

func exposureError(c *gin.Context, err error) {
	if errors.Is(err, errInternalExposureUnavailable) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": err.Error(), "code": "internal_exposure_unavailable"})
		return
	}
	abort(c, http.StatusBadGateway, err)
}
