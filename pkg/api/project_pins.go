package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
)

// clearProjectPins removes the node pins a move left on a project: the
// process-to-node map on the App and the project-node annotation on each of
// its databases. The controllers then render the workloads with the pool
// selector alone and the scheduler places them; each pinned process and
// database rolls once. Refused while an archive operation or maintenance is
// on, while a deploy is in progress, and while a pinned process or database
// still has its data on a node's disk: the pin is what keeps it with that
// data, so the disk is migrated first (storage plan, step 1).
func (s *Server) clearProjectPins(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	if !s.acquireArchiveRequest(c, app.Namespace) {
		return
	}
	defer s.releaseArchiveRequest(app.Namespace)
	ctx := c.Request.Context()
	if _, err := s.readProjectArchive(ctx, app.Namespace); !apierrors.IsNotFound(err) || app.Annotations[shpyrdv1.AnnotationMaintenance] != "" {
		abort(c, http.StatusConflict, errors.New("finish project maintenance before clearing its pins"))
		return
	}
	if rolloutInProgress(app) {
		abort(c, http.StatusConflict, errors.New("a deploy is in progress: clear the pins when it is done"))
		return
	}
	pinned := controller.ProcessNodes(app)
	var databases shpyrdv1.PostgresList
	if err := s.apps.List(ctx, &databases, client.InNamespace(app.Namespace)); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	var pinnedDatabases []*shpyrdv1.Postgres
	for i := range databases.Items {
		if databases.Items[i].Annotations[shpyrdv1.AnnotationPlacement] != "" {
			pinnedDatabases = append(pinnedDatabases, &databases.Items[i])
		}
	}
	if len(pinned) == 0 && len(pinnedDatabases) == 0 {
		c.JSON(http.StatusOK, gin.H{"processes": []string{}, "databases": []string{}, "note": "nothing was pinned"})
		return
	}
	local := s.pinsOnLocalData(ctx, app, pinned, pinnedDatabases)

	processes := make([]string, 0, len(pinned))
	for process := range pinned {
		processes = append(processes, process)
	}
	sort.Strings(processes)
	if len(pinned) > 0 {
		current := &shpyrdv1.App{}
		if err := s.apps.Get(ctx, client.ObjectKeyFromObject(app), current); err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
		before := current.DeepCopy()
		delete(current.Annotations, controller.AnnotationProcessNodes)
		if err := s.apps.Patch(ctx, current, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
	}
	names := make([]string, 0, len(pinnedDatabases))
	for _, pg := range pinnedDatabases {
		before := pg.DeepCopy()
		delete(pg.Annotations, shpyrdv1.AnnotationPlacement)
		if err := s.apps.Patch(ctx, pg, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
		names = append(names, pg.Name)
	}
	sort.Strings(names)
	s.audit(c, app.Name, "project.unpin", app.Name, fmt.Sprintf("processes %v, databases %v", processes, names))
	note := "the scheduler places them by pool now; each pinned process and database restarts once"
	if len(local) > 0 {
		note += "; " + strings.Join(local, ", ") + " stay on their node either way: their data is on its disk"
	}
	c.JSON(http.StatusOK, gin.H{"processes": processes, "databases": names, "note": note})
}

// pinsOnLocalData names the pinned processes and databases whose data is on
// a node's disk. Their pin can go: the volume's own node affinity keeps the
// pod on that node, so the answer says the restart changes nothing for them.
// Clearing it is what a storage migration asks for before it runs.
func (s *Server) pinsOnLocalData(ctx context.Context, app *shpyrdv1.App, pinned map[string]string, databases []*shpyrdv1.Postgres) []string {
	var out []string
	var volumes shpyrdv1.VolumeList
	if err := s.apps.List(ctx, &volumes, client.InNamespace(app.Namespace)); err != nil {
		return nil
	}
	local := map[string]bool{}
	for _, v := range volumes.Items {
		if firstNonEmpty(v.Status.StorageClass, v.Spec.StorageClass) == controller.LocalStorageClass {
			local[v.Name] = true
		}
	}
	processes := app.EffectiveProcesses()
	for process := range pinned {
		for _, m := range processes[process].Volumes {
			if local[m.Name] {
				out = append(out, "process "+process)
				break
			}
		}
	}
	if len(databases) > 0 {
		var claims corev1.PersistentVolumeClaimList
		if err := s.apps.List(ctx, &claims, client.InNamespace(app.Namespace)); err == nil {
			for _, pg := range databases {
				for _, claim := range claims.Items {
					if claim.Labels["cnpg.io/cluster"] == pg.Name && claim.Spec.StorageClassName != nil && *claim.Spec.StorageClassName == controller.LocalStorageClass {
						out = append(out, "database "+pg.Name)
						break
					}
				}
			}
		}
	}
	sort.Strings(out)
	return out
}
