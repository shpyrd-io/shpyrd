package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// A one-off, read-only survey. No project maintenance or persistent data catalog.
// The same operation gate excludes moves/restores while these PVCs are mounted.
func (s *Server) measureProjectPlacement(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	var body struct {
		Group string `json:"group"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	if !s.acquireArchiveRequest(c, app.Namespace) {
		return
	}
	defer s.releaseArchiveRequest(app.Namespace)
	s.archiveActive.Store(app.Namespace, "measuring")
	if _, err := s.readProjectArchive(c.Request.Context(), app.Namespace); !apierrors.IsNotFound(err) {
		abort(c, http.StatusConflict, errors.New("finish project maintenance before measuring data"))
		return
	}
	if app.Annotations[shpyrdv1.AnnotationMaintenance] != "" {
		abort(c, http.StatusConflict, errors.New("project is in maintenance"))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Minute)
	defer cancel()
	groups, _, err := s.projectPlacement(ctx, app)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	var group *placementGroup
	for i := range groups {
		if groups[i].ID == body.Group {
			group = &groups[i]
		}
	}
	if group == nil {
		abort(c, http.StatusNotFound, errors.New("placement group no longer exists"))
		return
	}
	if len(group.Claims) == 0 && group.Database != "" {
		abort(c, http.StatusConflict, errors.New("database has no data claim yet"))
		return
	}
	operation := strings.ReplaceAll(uuid.NewString(), "-", "")
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer stop()
		_ = s.cleanupArchiveHelpers(cleanup, app.Namespace, operation)
	}()
	var total int64
	for _, claim := range group.Claims {
		pvc := &corev1.PersistentVolumeClaim{}
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: claim}, pvc); err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
		if pvc.Spec.VolumeName == "" {
			abort(c, http.StatusConflict, fmt.Errorf("volume %s is not provisioned yet", claim))
			return
		}
		command, err := s.projectClaimHelper(ctx, app, claim, claim, operation, "", true, nil)
		if err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
		var output bytes.Buffer
		if err := command(ctx, []string{"/shpyrd-server", "project-volume", "usage", "/data"}, nil, &output); err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
		var usage struct {
			Bytes int64 `json:"bytes"`
		}
		if err := json.Unmarshal(output.Bytes(), &usage); err != nil || usage.Bytes < 0 {
			abort(c, http.StatusBadGateway, errors.New("invalid volume size measurement"))
			return
		}
		total += usage.Bytes
	}
	c.JSON(http.StatusOK, gin.H{"group": group.ID, "diskUsedBytes": total, "measuredAt": time.Now().UTC()})
}
