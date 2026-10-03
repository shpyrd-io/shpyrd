package api

import (
	"encoding/base64"
	"errors"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	corev1 "k8s.io/api/core/v1"
)

func archiveProjectID(app *shpyrdv1.App) string {
	if app.Spec.ID != "" {
		return app.Spec.ID
	}
	return "legacy-" + base64.RawURLEncoding.EncodeToString([]byte(app.Namespace+"/"+app.Name))
}

func (s *Server) clusterArchiveProjects(c *gin.Context) {
	ctx := c.Request.Context()
	var apps shpyrdv1.AppList
	if err := s.apps.List(ctx, &apps); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	var pods corev1.PodList
	if err := s.apps.List(ctx, &pods); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	nodes := map[string]map[string]bool{}
	for _, pod := range pods.Items {
		if pod.Spec.NodeName == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		if nodes[pod.Namespace] == nil {
			nodes[pod.Namespace] = map[string]bool{}
		}
		nodes[pod.Namespace][pod.Spec.NodeName] = true
	}
	result := []gin.H{}
	for i := range apps.Items {
		app := &apps.Items[i]
		var placed []string
		for name := range nodes[app.Namespace] {
			placed = append(placed, name)
		}
		sort.Strings(placed)
		phase := app.Status.Phase
		if app.Annotations[shpyrdv1.AnnotationMaintenance] != "" {
			phase = "Maintenance"
		}
		result = append(result, gin.H{"id": archiveProjectID(app), "slug": project.SlugOf(app), "name": project.DisplayName(app), "workspace": s.workspaceOfApp(ctx, app), "phase": phase, "nodes": placed})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i]["workspace"].(string)+"/"+result[i]["slug"].(string) < result[j]["workspace"].(string)+"/"+result[j]["slug"].(string)
	})
	c.JSON(http.StatusOK, result)
}

// This middleware is mounted only after the console and ClusterAdmin guards.
// A workspace caller never selects an arbitrary namespace through an archive.
func (s *Server) clusterArchiveProject(c *gin.Context) {
	var apps shpyrdv1.AppList
	if err := s.apps.List(c.Request.Context(), &apps); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	for i := range apps.Items {
		app := &apps.Items[i]
		if archiveProjectID(app) == c.Param("projectID") {
			c.Set("shpyrd.project", app)
			c.Next()
			return
		}
	}
	abort(c, http.StatusNotFound, errors.New("project not found"))
}
