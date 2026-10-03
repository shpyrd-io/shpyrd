package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const retainedMigrationAnnotation = "shpyrd.io/retained-migration"

type retainedMigrationDisk struct {
	Name         string    `json:"name"`
	Namespace    string    `json:"namespace"`
	ProjectUID   types.UID `json:"projectUID"`
	Claim        string    `json:"claim"`
	StorageClass string    `json:"storageClass"`
	Capacity     string    `json:"capacity"`
	RetainedAt   time.Time `json:"retainedAt"`
	Deleting     bool      `json:"deleting,omitempty"`
}

// Called only while the project is paused and the volume controllers are
// frozen. Journaled specs restore both the original binding and its intent.
func (s *Server) setMoveStorage(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation, forward bool) error {
	for name, original := range op.Move.BeforeVolumes {
		volume := &shpyrdv1.Volume{}
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: name}, volume); err != nil {
			return err
		}
		before := volume.DeepCopy()
		volume.Spec = *original.DeepCopy()
		if forward {
			volume.Spec.StorageClass = controller.LocalStorageClass
			volume.Spec.FromSnapshot = ""
			// Existing provider claims may have been rounded to a provider minimum.
			for _, claim := range op.Move.Claims {
				if claim.Original.Name == volume.PVCName() {
					volume.Spec.Size = claim.Original.Spec.Resources.Requests[corev1.ResourceStorage]
				}
			}
		}
		if err := s.apps.Patch(ctx, volume, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
	}
	if op.Move.Group.Database == "" || op.Move.BeforeDatabaseStorage == nil {
		return nil
	}
	pg := &shpyrdv1.Postgres{}
	if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: op.Move.Group.Database}, pg); err != nil {
		return err
	}
	beforePG := pg.DeepCopy()
	pg.Spec.Storage = op.Move.BeforePostgresStorage
	if forward {
		size, _ := op.Move.BeforeDatabaseStorage["size"].(string)
		quantity, err := resource.ParseQuantity(size)
		if err != nil {
			return err
		}
		pg.Spec.Storage = &quantity
	}
	if err := s.apps.Patch(ctx, pg, client.MergeFromWithOptions(beforePG, client.MergeFromWithOptimisticLock{})); err != nil {
		return err
	}
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(controller.CNPGClusterGVK)
	if err := s.apps.Get(ctx, client.ObjectKeyFromObject(pg), cluster); err != nil {
		return err
	}
	before := cluster.DeepCopy()
	if err := unstructured.SetNestedMap(cluster.Object, op.Move.BeforeDatabaseStorage, "spec", "storage"); err != nil {
		return err
	}
	if forward {
		if err := unstructured.SetNestedField(cluster.Object, controller.LocalStorageClass, "spec", "storage", "storageClass"); err != nil {
			return err
		}
	}
	return s.apps.Patch(ctx, cluster, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}

func (s *Server) retainMigratedDisk(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation, claim moveClaim, pv *corev1.PersistentVolume) error {
	if pv.Annotations[retainedMigrationAnnotation] != "" {
		return nil
	}
	capacity := pv.Spec.Capacity[corev1.ResourceStorage]
	record := retainedMigrationDisk{Name: pv.Name, Namespace: app.Namespace, ProjectUID: app.UID, Claim: claim.Original.Name, StorageClass: pv.Spec.StorageClassName, Capacity: capacity.String(), RetainedAt: op.StartedAt}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	before := pv.DeepCopy()
	pv.Annotations[retainedMigrationAnnotation] = string(data)
	pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
	return s.apps.Patch(ctx, pv, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}

func (s *Server) retainedMigrationDisks(ctx context.Context, app *shpyrdv1.App) ([]retainedMigrationDisk, error) {
	var volumes corev1.PersistentVolumeList
	if err := s.apps.List(ctx, &volumes); err != nil {
		return nil, err
	}
	result := []retainedMigrationDisk{}
	for _, pv := range volumes.Items {
		var record retainedMigrationDisk
		if json.Unmarshal([]byte(pv.Annotations[retainedMigrationAnnotation]), &record) != nil || record.Namespace != app.Namespace || record.ProjectUID != app.UID {
			continue
		}
		record.Name = pv.Name
		record.Deleting = pv.DeletionTimestamp != nil || pv.Spec.PersistentVolumeReclaimPolicy == corev1.PersistentVolumeReclaimDelete
		result = append(result, record)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// Releasing a retained provider disk is deliberately separate from migration.
// Its CSI provisioner deletes the backing disk; finalizers are never bypassed.
func (s *Server) deleteRetainedMigrationDisk(c *gin.Context) {
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
		abort(c, http.StatusConflict, errors.New("finish project maintenance before deleting retained disks"))
		return
	}
	pv := &corev1.PersistentVolume{}
	if err := s.apps.Get(ctx, types.NamespacedName{Name: c.Param("volume")}, pv); err != nil {
		abort(c, http.StatusNotFound, err)
		return
	}
	var record retainedMigrationDisk
	if json.Unmarshal([]byte(pv.Annotations[retainedMigrationAnnotation]), &record) != nil || record.Namespace != app.Namespace || record.ProjectUID != app.UID {
		abort(c, http.StatusNotFound, errors.New("retained disk not found for this project"))
		return
	}
	if pv.DeletionTimestamp != nil || pv.Spec.PersistentVolumeReclaimPolicy == corev1.PersistentVolumeReclaimDelete {
		c.JSON(http.StatusOK, gin.H{"status": "deleting"})
		return
	}
	if pv.Status.Phase != corev1.VolumeReleased || pv.Spec.ClaimRef == nil || pv.Spec.ClaimRef.Namespace != app.Namespace || pv.Spec.ClaimRef.Name != record.Claim {
		abort(c, http.StatusConflict, errors.New("retained disk is not released"))
		return
	}
	var claims corev1.PersistentVolumeClaimList
	if err := s.apps.List(ctx, &claims); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	for _, claim := range claims.Items {
		if claim.Spec.VolumeName == pv.Name {
			abort(c, http.StatusConflict, errors.New("disk is still referenced by a claim"))
			return
		}
	}
	before := pv.DeepCopy()
	pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimDelete
	if err := s.apps.Patch(ctx, pv, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
		abort(c, http.StatusConflict, err)
		return
	}
	s.audit(c, app.Name, "project.migration.disk.delete", pv.Name, "released retained provider disk")
	c.JSON(http.StatusOK, gin.H{"status": "deleting"})
}
