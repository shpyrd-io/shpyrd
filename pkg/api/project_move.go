package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/projectarchive"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type projectMove struct {
	RolledBack  bool           `json:"rolledBack,omitempty"`
	Group       placementGroup `json:"group"`
	Destination string         `json:"destination"`
	// TargetClass is the storage class the copies are made on: the node's
	// disk for a move between nodes, the profile's block class for a
	// migration off the node's disk. Empty in records from before: node-local.
	TargetClass           string                         `json:"targetClass,omitempty"`
	BeforeProcesses       string                         `json:"beforeProcesses"`
	BeforeDatabaseNode    string                         `json:"beforeDatabaseNode"`
	BeforeVolumes         map[string]shpyrdv1.VolumeSpec `json:"beforeVolumes,omitempty"`
	BeforeDatabaseStorage map[string]interface{}         `json:"beforeDatabaseStorage,omitempty"`
	BeforePostgresStorage *resource.Quantity             `json:"beforePostgresStorage,omitempty"`
	Claims                []moveClaim                    `json:"claims"`
}

// targetClass is the class the copies are made on.
func (m *projectMove) targetClass() string {
	if m.TargetClass == "" {
		return controller.LocalStorageClass
	}
	return m.TargetClass
}

type moveClaim struct {
	RetainSource  bool                                 `json:"retainSource,omitempty"`
	Original      corev1.PersistentVolumeClaim         `json:"original"`
	SourcePV      string                               `json:"sourcePV"`
	SourceReclaim corev1.PersistentVolumeReclaimPolicy `json:"sourceReclaim"`
	TargetClaim   string                               `json:"targetClaim"`
	TargetPV      string                               `json:"targetPV,omitempty"`
}

func (s *Server) moveProject(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	var req struct {
		Group          string `json:"group"`
		Node           string `json:"node"`
		MigrateToLocal bool   `json:"migrateToLocal"`
		// TargetClass moves the data onto another storage class on the way:
		// the profile's block class, for a disk still on a node's disk.
		TargetClass string `json:"targetClass,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	if !s.acquireArchiveRequest(c, app.Namespace) {
		return
	}
	defer s.releaseArchiveRequest(app.Namespace)
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Hour)
	defer cancel()
	if rolloutInProgress(app) {
		abort(c, http.StatusConflict, errors.New("wait for the current deployment to finish"))
		return
	}
	group, node, err := s.placementDestination(ctx, app, req.Group, req.Node)
	if err != nil {
		abort(c, http.StatusConflict, err)
		return
	}
	original, err := s.projectMetadata(ctx, app, false)
	if err != nil {
		abort(c, http.StatusConflict, err)
		return
	}
	// A class change is asked for by name, never inferred: it rounds the
	// disk to the provider minimum and leaves the old disk billable until
	// released, which the caller must have meant.
	target := req.TargetClass
	if target == "" {
		target = controller.LocalStorageClass
	}
	blockProfile := install.ProjectStorageClass(s.vars) != controller.LocalStorageClass
	move, err := s.prepareProjectMove(ctx, app, group, node, target)
	if err != nil {
		abort(c, http.StatusConflict, err)
		return
	}
	for _, claim := range move.Claims {
		if !claim.RetainSource {
			continue
		}
		// A copy onto a node's disk is the direction this platform retires
		// on its cloud profiles: never there, whatever the request says.
		if target == controller.LocalStorageClass && blockProfile {
			abort(c, http.StatusConflict, errors.New("this project's disks are provider block volumes that follow their processes: there is nothing to copy, so the scheduler places them (drain the node, or clear the project's pins); a move only copies node-local disks"))
			return
		}
		if target == controller.LocalStorageClass && !req.MigrateToLocal {
			abort(c, http.StatusConflict, errors.New("provider volumes require explicit migration to local storage"))
			return
		}
	}
	op, err := s.beginProjectArchive(ctx, app, "move", original)
	if err != nil {
		abort(c, http.StatusConflict, err)
		return
	}
	op.Move = move
	for i := range move.Claims {
		move.Claims[i].TargetClaim = fmt.Sprintf("move-%s-%d", op.ID[:16], i)
	}
	if err = s.saveProjectArchive(ctx, app.Namespace, op); err == nil {
		err = s.runProjectMove(ctx, app, op)
	}
	if err != nil {
		recovery, stop := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Minute)
		defer stop()
		var recoveryErr error
		if op.canRollback() {
			recoveryErr = s.rollbackProjectMove(recovery, app, op)
		}
		if recoveryErr != nil {
			op.State.Error = recoveryErr.Error()
			_ = s.saveProjectArchive(recovery, app.Namespace, op)
		}
		abort(c, http.StatusBadGateway, errors.Join(err, recoveryErr))
		return
	}
	s.audit(c, app.Name, "project.move", group.ID, "moved to "+node.Name+" on "+move.targetClass())
	c.JSON(http.StatusOK, gin.H{"status": "moved", "targetClass": move.targetClass(), "warnings": op.Warnings})
}

func (s *Server) prepareProjectMove(ctx context.Context, app *shpyrdv1.App, group placementGroup, node *corev1.Node, target string) (*projectMove, error) {
	move := &projectMove{Group: group, Destination: node.Labels[corev1.LabelHostname], TargetClass: target, BeforeProcesses: app.Annotations[controller.AnnotationProcessNodes], BeforeVolumes: map[string]shpyrdv1.VolumeSpec{}}
	var claims []corev1.PersistentVolumeClaim
	if group.Database != "" {
		pg := &shpyrdv1.Postgres{}
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: group.Database}, pg); err != nil {
			return nil, err
		}
		if pg.Spec.Instances != nil && *pg.Spec.Instances != 1 {
			return nil, errors.New("this MVP moves single-instance PostgreSQL databases only")
		}
		move.BeforeDatabaseNode = pg.Annotations[shpyrdv1.AnnotationPlacement]
		if pg.Spec.Storage != nil {
			move.BeforePostgresStorage = ptr.To(pg.Spec.Storage.DeepCopy())
		}
		cluster := &unstructured.Unstructured{}
		cluster.SetGroupVersionKind(controller.CNPGClusterGVK)
		if err := s.apps.Get(ctx, client.ObjectKeyFromObject(pg), cluster); err != nil {
			return nil, err
		}
		move.BeforeDatabaseStorage, _, _ = unstructured.NestedMap(cluster.Object, "spec", "storage")
		if _, present, _ := unstructured.NestedFieldNoCopy(cluster.Object, "spec", "walStorage"); present {
			return nil, errors.New("separate WAL storage cannot be migrated")
		}
		if tablespaces, _, _ := unstructured.NestedSlice(cluster.Object, "spec", "tablespaces"); len(tablespaces) > 0 {
			return nil, errors.New("databases with tablespaces cannot be migrated")
		}
		if _, present := move.BeforeDatabaseStorage["pvcTemplate"]; present {
			return nil, errors.New("custom database PVC templates cannot be migrated")
		}
		var list corev1.PersistentVolumeClaimList
		if err := s.apps.List(ctx, &list, client.InNamespace(app.Namespace), client.MatchingLabels{"cnpg.io/cluster": pg.Name}); err != nil {
			return nil, err
		}
		claims = list.Items
		if len(claims) != 1 {
			return nil, errors.New("database movement currently requires one data claim and no separate WAL/tablespace claims")
		}
	} else {
		for _, name := range group.Volumes {
			volume := &shpyrdv1.Volume{}
			if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: name}, volume); err != nil {
				return nil, err
			}
			if volume.Annotations[shpyrdv1.AnnotationRestoreFrom] != "" || volume.Annotations[controller.AnnotationDataMove] != "" {
				return nil, errors.New("volume already has pending maintenance")
			}
			if volume.Shared() && target != controller.LocalStorageClass {
				return nil, fmt.Errorf("the shared folder %q stays where it is: a block disk is mounted by one instance, so a move onto %s would end its sharing", name, target)
			}
			move.BeforeVolumes[name] = *volume.Spec.DeepCopy()
			pvc := corev1.PersistentVolumeClaim{}
			if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: shpyrdv1.PVCPrefix + name}, &pvc); err != nil {
				return nil, err
			}
			claims = append(claims, pvc)
		}
	}
	for _, claim := range claims {
		if claim.DeletionTimestamp != nil || claim.Status.Phase != corev1.ClaimBound || claim.Spec.VolumeName == "" {
			return nil, fmt.Errorf("claim %s must be bound before movement", claim.Name)
		}
		if claim.Spec.VolumeMode != nil && *claim.Spec.VolumeMode != corev1.PersistentVolumeFilesystem {
			return nil, errors.New("raw block devices cannot be moved")
		}
		pv := &corev1.PersistentVolume{}
		if err := s.apps.Get(ctx, types.NamespacedName{Name: claim.Spec.VolumeName}, pv); err != nil {
			return nil, err
		}
		if pv.DeletionTimestamp != nil || pv.Spec.ClaimRef == nil || pv.Spec.ClaimRef.UID != claim.UID || pv.Spec.ClaimRef.Namespace != claim.Namespace || pv.Spec.ClaimRef.Name != claim.Name {
			return nil, fmt.Errorf("claim %s no longer owns its source disk", claim.Name)
		}
		if len(pv.OwnerReferences) != 0 {
			return nil, errors.New("source disk has a garbage-collection owner; cannot guarantee retention")
		}
		if pv.Annotations[projectOperationLabel] != "" {
			return nil, errors.New("volume already belongs to another maintenance operation")
		}
		// The source disk is kept when the copy changes class: it is the way
		// back until the project is checked on the new one.
		move.Claims = append(move.Claims, moveClaim{RetainSource: pv.Spec.StorageClassName != target, Original: claim, SourcePV: pv.Name, SourceReclaim: pv.Spec.PersistentVolumeReclaimPolicy})
	}
	if len(claims) > 0 {
		sc := &storagev1.StorageClass{}
		if err := s.apps.Get(ctx, types.NamespacedName{Name: target}, sc); err != nil {
			return nil, fmt.Errorf("the storage class %s is not installed: %w", target, err)
		}
		if target == controller.LocalStorageClass && sc.Provisioner != "shpyrd.io/local-path" {
			return nil, errors.New("unexpected local storage provisioner")
		}
	}
	return move, nil
}
func (s *Server) markMoveResources(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation, stopped bool) error {
	if op.Move == nil {
		return nil
	}
	for _, name := range op.Move.Group.Volumes {
		v := &shpyrdv1.Volume{}
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: name}, v); err != nil {
			return err
		}
		before := v.DeepCopy()
		if v.Annotations == nil {
			v.Annotations = map[string]string{}
		}
		if stopped {
			v.Annotations[controller.AnnotationDataMove] = op.ID
		} else if v.Annotations[controller.AnnotationDataMove] == op.ID {
			delete(v.Annotations, controller.AnnotationDataMove)
		}
		if err := s.apps.Patch(ctx, v, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
	}
	if name := op.Move.Group.Database; name != "" {
		pg := &shpyrdv1.Postgres{}
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: name}, pg); err != nil {
			return err
		}
		before := pg.DeepCopy()
		if pg.Annotations == nil {
			pg.Annotations = map[string]string{}
		}
		if stopped {
			pg.Annotations[controller.AnnotationDataMove] = op.ID
		} else if pg.Annotations[controller.AnnotationDataMove] == op.ID {
			delete(pg.Annotations, controller.AnnotationDataMove)
		}
		if err := s.apps.Patch(ctx, pg, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
		cluster := &unstructured.Unstructured{}
		cluster.SetGroupVersionKind(controller.CNPGClusterGVK)
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: name}, cluster); err != nil {
			return err
		}
		previous := cluster.DeepCopy()
		annotations := cluster.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		if stopped {
			annotations["cnpg.io/hibernation"] = "on"
		} else {
			delete(annotations, "cnpg.io/hibernation")
		}
		cluster.SetAnnotations(annotations)
		if err := s.apps.Patch(ctx, cluster, client.MergeFromWithOptions(previous, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
	}
	return nil
}
func (s *Server) waitMoveQuiescent(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation) error {
	return s.waitArchiveCondition(ctx, func(ctx context.Context) (bool, error) {
		var pods corev1.PodList
		if err := s.apps.List(ctx, &pods, client.InNamespace(app.Namespace)); err != nil {
			return false, err
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
				continue
			}
			if pod.Labels[projectOperationLabel] == op.ID {
				continue
			}
			for _, v := range pod.Spec.Volumes {
				if v.PersistentVolumeClaim == nil {
					continue
				}
				for _, claim := range op.Move.Claims {
					if v.PersistentVolumeClaim.ClaimName == claim.Original.Name {
						return false, nil
					}
				}
			}
		}
		return true, nil
	})
}
func (s *Server) runProjectMove(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation) error {
	if err := s.drainProjectArchive(ctx, app, op); err != nil {
		return err
	}
	if err := s.fenceArchiveDatabases(ctx, app, op, op.Original.Databases); err != nil {
		return err
	}
	if err := s.markMoveResources(ctx, app, op, true); err != nil {
		return err
	}
	if err := s.waitMoveQuiescent(ctx, app, op); err != nil {
		return err
	}
	for i := range op.Move.Claims {
		op.State = projectarchive.TransactionState{Phase: "copying", Resource: op.Move.Claims[i].Original.Name}
		if err := s.saveProjectArchive(ctx, app.Namespace, op); err != nil {
			return err
		}
		if err := s.copyMoveClaim(ctx, app, op, &op.Move.Claims[i]); err != nil {
			return err
		}
	}
	if err := s.cleanupArchiveHelpers(ctx, app.Namespace, op.ID); err != nil {
		return err
	}
	op.State = projectarchive.TransactionState{Phase: "committing"}
	if err := s.saveProjectArchive(ctx, app.Namespace, op); err != nil {
		return err
	}
	for i := range op.Move.Claims {
		if err := s.bindMoveClaim(ctx, app, op, &op.Move.Claims[i], true); err != nil {
			return err
		}
	}
	if err := s.setMoveStorage(ctx, app, op, true); err != nil {
		return err
	}
	if err := s.setMovePlacement(ctx, app, op, true); err != nil {
		return err
	}
	if err := s.markMoveResources(ctx, app, op, false); err != nil {
		return err
	}
	if err := s.waitMovedDatabase(ctx, app, op); err != nil {
		return err
	}
	if err := s.resumeProjectArchive(ctx, app, op); err != nil {
		return err
	}
	if err := s.finishProjectMove(ctx, app, op, true); err != nil {
		return err
	}
	return s.clearProjectArchive(ctx, app.Namespace, op)
}
func (s *Server) copyMoveClaim(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation, claim *moveClaim) error {
	if err := s.retainMovePV(ctx, claim.SourcePV, op.ID); err != nil {
		return err
	}
	target := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: claim.TargetClaim, Namespace: app.Namespace, Labels: map[string]string{projectOperationLabel: op.ID}}, Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, StorageClassName: ptr.To(op.Move.targetClass()), Resources: claim.Original.Spec.Resources}}
	if err := s.apps.Create(ctx, target); err != nil {
		return err
	}
	destination, err := s.archiveClaimHelper(ctx, app, target.Name, target.Name, op.ID, op.Move.Destination, op.warn)
	if err != nil {
		return err
	}
	if err := s.apps.Get(ctx, client.ObjectKeyFromObject(target), target); err != nil {
		return err
	}
	claim.TargetPV = target.Spec.VolumeName
	if claim.TargetPV == "" {
		return errors.New("destination claim has no disk")
	}
	if err := s.saveProjectArchive(ctx, app.Namespace, op); err != nil {
		return err
	}
	if err := s.retainMovePV(ctx, claim.TargetPV, op.ID); err != nil {
		return err
	}
	source, err := s.projectClaimHelper(ctx, app, claim.Original.Name, "source-"+claim.TargetClaim, op.ID, "", true, op.warn)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp("", "shpyrd-move-*.tar")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	warned := len(op.Warnings)
	if err := source(ctx, []string{"/shpyrd-server", "project-volume", "export", "/data"}, nil, &archiveBoundedWriter{Writer: file, Remaining: projectarchive.DefaultLimit}); err != nil {
		return err
	}
	if len(op.Warnings) != warned {
		if err := s.saveProjectArchive(ctx, app.Namespace, op); err != nil {
			return err
		}
	}
	st, err := file.Stat()
	if err != nil {
		return err
	}
	var capacity bytes.Buffer
	if err := destination(ctx, []string{"/shpyrd-server", "project-volume", "capacity", "/data"}, nil, &capacity); err != nil {
		return err
	}
	var disk projectarchive.DiskCapacity
	if err := json.Unmarshal(capacity.Bytes(), &disk); err != nil {
		return err
	}
	if disk.Available < uint64(st.Size())+(1<<30) {
		return errors.New("destination disk lacks space for the data plus 1 GiB of headroom")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := destination(ctx, []string{"/shpyrd-server", "project-volume", "stage", "/data", op.ID}, file, io.Discard); err != nil {
		return err
	}
	if err := destination(ctx, []string{"/shpyrd-server", "project-volume", "commit", "/data", op.ID}, nil, io.Discard); err != nil {
		return err
	}
	var before, after bytes.Buffer
	for _, entry := range []struct {
		cmd projectarchive.Command
		out *bytes.Buffer
	}{{source, &before}, {destination, &after}} {
		if err := entry.cmd(ctx, []string{"/shpyrd-server", "project-volume", "fingerprint", "/data"}, nil, entry.out); err != nil {
			return err
		}
	}
	if !bytes.Equal(before.Bytes(), after.Bytes()) {
		return errors.New("destination data verification failed; source is preserved")
	}
	return destination(ctx, []string{"/shpyrd-server", "project-volume", "finish", "/data", op.ID}, nil, io.Discard)
}

type archiveBoundedWriter struct {
	io.Writer
	Remaining int64
}

func (w *archiveBoundedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.Remaining {
		return 0, errors.New("volume exceeds transfer limit")
	}
	n, err := w.Writer.Write(p)
	w.Remaining -= int64(n)
	return n, err
}

func (s *Server) retainMovePV(ctx context.Context, name, operation string) error {
	pv := &corev1.PersistentVolume{}
	if err := s.apps.Get(ctx, types.NamespacedName{Name: name}, pv); err != nil {
		return err
	}
	if owner := pv.Annotations[projectOperationLabel]; owner != "" && owner != operation {
		return errors.New("disk belongs to another operation")
	}
	before := pv.DeepCopy()
	if pv.Annotations == nil {
		pv.Annotations = map[string]string{}
	}
	pv.Annotations[projectOperationLabel] = operation
	pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
	return s.apps.Patch(ctx, pv, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}
func (s *Server) deleteMoveClaim(ctx context.Context, namespace, name, operation string, temporary bool, allowed ...string) error {
	key := types.NamespacedName{Namespace: namespace, Name: name}
	claim := &corev1.PersistentVolumeClaim{}
	if err := s.apps.Get(ctx, key, claim); err != nil {
		return client.IgnoreNotFound(err)
	}
	if temporary && claim.Labels[projectOperationLabel] != operation {
		return errors.New("temporary claim ownership changed")
	}
	if !temporary {
		valid := false
		for _, pv := range allowed {
			if pv != "" && claim.Spec.VolumeName == pv {
				valid = true
			}
		}
		if !valid {
			return errors.New("project claim was rebound by another operation")
		}
	}
	if err := s.apps.Delete(ctx, claim, client.Preconditions{UID: &claim.UID}); client.IgnoreNotFound(err) != nil {
		return err
	}
	return s.waitArchiveCondition(ctx, func(ctx context.Context) (bool, error) {
		err := s.apps.Get(ctx, key, &corev1.PersistentVolumeClaim{})
		return apierrors.IsNotFound(err), client.IgnoreNotFound(err)
	})
}
func (s *Server) bindMoveClaim(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation, claim *moveClaim, forward bool) error {
	destination := claim.SourcePV
	if forward {
		destination = claim.TargetPV
	}
	if destination == "" {
		return errors.New("move destination was not recorded")
	}
	current := &corev1.PersistentVolumeClaim{}
	key := types.NamespacedName{Namespace: app.Namespace, Name: claim.Original.Name}
	err := s.apps.Get(ctx, key, current)
	if err == nil && current.Spec.VolumeName == destination && current.DeletionTimestamp == nil {
		return nil
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	// Before deleting either claim, both disks must be protected from their
	// dynamic provisioner's ordinary PVC-deletion cleanup.
	if err := s.retainMovePV(ctx, destination, op.ID); err != nil {
		return err
	}
	if forward {
		if err := s.deleteMoveClaim(ctx, app.Namespace, claim.TargetClaim, op.ID, true); err != nil {
			return err
		}
	}
	if err := s.deleteMoveClaim(ctx, app.Namespace, claim.Original.Name, op.ID, false, claim.SourcePV, claim.TargetPV); err != nil {
		return err
	}
	pv := &corev1.PersistentVolume{}
	if err := s.apps.Get(ctx, types.NamespacedName{Name: destination}, pv); err != nil {
		return err
	}
	if pv.Annotations[projectOperationLabel] != op.ID {
		return errors.New("disk ownership changed")
	}
	if ref := pv.Spec.ClaimRef; ref != nil && (ref.Namespace != app.Namespace || (ref.Name != claim.Original.Name && ref.Name != claim.TargetClaim)) {
		return errors.New("disk is claimed by another resource")
	}
	before := pv.DeepCopy()
	pv.Spec.ClaimRef = &corev1.ObjectReference{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: app.Namespace, Name: claim.Original.Name}
	if err := s.apps.Patch(ctx, pv, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
		return err
	}
	restored := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: claim.Original.Name, Namespace: app.Namespace, Labels: claim.Original.Labels, OwnerReferences: claim.Original.OwnerReferences}, Spec: *claim.Original.Spec.DeepCopy()}
	restored.Spec.VolumeName = destination
	if forward {
		restored.Spec.StorageClassName = ptr.To(op.Move.targetClass())
		restored.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
		restored.Spec.Selector = nil
		restored.Spec.DataSource = nil
		restored.Spec.DataSourceRef = nil
		restored.Spec.VolumeAttributesClassName = nil
	}
	// Retain CNPG's instance identity annotations, excluding obsolete binder
	// and scheduler state belonging to the old PVC UID.
	restored.Annotations = map[string]string{}
	for key, value := range claim.Original.Annotations {
		if strings.HasPrefix(key, "pv.kubernetes.io/") || strings.HasPrefix(key, "volume.kubernetes.io/") || strings.HasPrefix(key, "volume.beta.kubernetes.io/") {
			continue
		}
		restored.Annotations[key] = value
	}
	if err := s.apps.Create(ctx, restored); err != nil {
		return err
	}
	return s.waitArchiveCondition(ctx, func(ctx context.Context) (bool, error) {
		if err := s.apps.Get(ctx, key, current); err != nil {
			return false, err
		}
		return current.Spec.VolumeName == destination && current.Status.Phase == corev1.ClaimBound, nil
	})
}
func (s *Server) setMovePlacement(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation, forward bool) error {
	if op.Move == nil {
		return nil
	}
	if name := op.Move.Group.Database; name != "" {
		pg := &shpyrdv1.Postgres{}
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: name}, pg); err != nil {
			return err
		}
		before := pg.DeepCopy()
		if pg.Annotations == nil {
			pg.Annotations = map[string]string{}
		}
		pg.Annotations[shpyrdv1.AnnotationPlacement] = op.Move.BeforeDatabaseNode
		if forward {
			pg.Annotations[shpyrdv1.AnnotationPlacement] = op.Move.Destination
		}
		return s.apps.Patch(ctx, pg, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	}
	current := &shpyrdv1.App{}
	if err := s.apps.Get(ctx, client.ObjectKeyFromObject(app), current); err != nil {
		return err
	}
	before := current.DeepCopy()
	if current.Annotations == nil {
		current.Annotations = map[string]string{}
	}
	value := op.Move.BeforeProcesses
	if forward {
		nodes := controller.ProcessNodes(current)
		for _, process := range op.Move.Group.Processes {
			nodes[process] = op.Move.Destination
		}
		data, err := json.Marshal(nodes)
		if err != nil {
			return err
		}
		value = string(data)
	}
	if value == "" {
		delete(current.Annotations, controller.AnnotationProcessNodes)
	} else {
		current.Annotations[controller.AnnotationProcessNodes] = value
	}
	return s.apps.Patch(ctx, current, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}
func (s *Server) waitMovedDatabase(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation) error {
	if op.Move == nil || op.Move.Group.Database == "" {
		return nil
	}
	return s.waitArchiveCondition(ctx, func(ctx context.Context) (bool, error) {
		// CNPG can briefly start a pod using the old template before it observes
		// the placement annotation, then roll it again. Do not release traffic
		// until both the cluster and ready primary use the final node selector.
		pg := &shpyrdv1.Postgres{}
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: op.Move.Group.Database}, pg); err != nil {
			return false, err
		}
		cluster := &unstructured.Unstructured{}
		cluster.SetGroupVersionKind(controller.CNPGClusterGVK)
		if err := s.apps.Get(ctx, client.ObjectKeyFromObject(pg), cluster); err != nil {
			return false, err
		}
		expected := pg.Annotations[shpyrdv1.AnnotationPlacement]
		actual, _, _ := unstructured.NestedString(cluster.Object, "spec", "affinity", "nodeSelector", corev1.LabelHostname)
		if actual != expected || cluster.GetAnnotations()["cnpg.io/hibernation"] == "on" {
			return false, nil
		}
		var pods corev1.PodList
		if err := s.apps.List(ctx, &pods, client.InNamespace(app.Namespace), client.MatchingLabels{"cnpg.io/cluster": pg.Name, "cnpg.io/instanceRole": "primary"}); err != nil {
			return false, err
		}
		ready := false
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil || pod.Spec.NodeSelector[corev1.LabelHostname] != expected {
				continue
			}
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					ready = true
				}
			}
		}
		if !ready {
			return false, nil
		}
		db, err := s.archiveDatabase(ctx, app.Namespace, op.Move.Group.Database)
		if err != nil {
			return false, nil
		}
		return db.Check(ctx) == nil, nil
	})
}
func (s *Server) rollbackProjectMove(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation) error {
	if !op.canRollback() {
		return errors.New("workloads may have resumed; automatic rollback could discard new writes")
	}
	if op.Move == nil {
		if err := s.resumeProjectArchive(ctx, app, op); err != nil {
			return err
		}
		return s.clearProjectArchive(ctx, app.Namespace, op)
	}
	if err := s.drainProjectArchive(ctx, app, op); err != nil {
		return err
	}
	if err := s.markMoveResources(ctx, app, op, true); err != nil {
		return err
	}
	if err := s.cleanupArchiveHelpers(ctx, app.Namespace, op.ID); err != nil {
		return err
	}
	if err := s.waitMoveQuiescent(ctx, app, op); err != nil {
		return err
	}
	op.State = projectarchive.TransactionState{Phase: "rolling-back"}
	if err := s.saveProjectArchive(ctx, app.Namespace, op); err != nil {
		return err
	}
	for i := range op.Move.Claims {
		if err := s.bindMoveClaim(ctx, app, op, &op.Move.Claims[i], false); err != nil {
			return err
		}
	}
	if err := s.setMoveStorage(ctx, app, op, false); err != nil {
		return err
	}
	if err := s.setMovePlacement(ctx, app, op, false); err != nil {
		return err
	}
	if err := s.markMoveResources(ctx, app, op, false); err != nil {
		return err
	}
	if err := s.waitMovedDatabase(ctx, app, op); err != nil {
		return err
	}
	// Record which side won before resume crosses the durable write boundary.
	op.Move.RolledBack = true
	if err := s.saveProjectArchive(ctx, app.Namespace, op); err != nil {
		return err
	}
	if err := s.resumeProjectArchive(ctx, app, op); err != nil {
		return err
	}
	if err := s.finishProjectMove(ctx, app, op, false); err != nil {
		return err
	}
	return s.clearProjectArchive(ctx, app.Namespace, op)
}
func (s *Server) finishProjectMove(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation, forward bool) error {
	if op.Move == nil {
		return nil
	}
	for _, claim := range op.Move.Claims {
		// A crash can happen after provisioning but before recording TargetPV.
		temporary := &corev1.PersistentVolumeClaim{}
		target := claim.TargetPV
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: claim.TargetClaim}, temporary); err == nil {
			if temporary.Labels[projectOperationLabel] != op.ID {
				return errors.New("temporary claim ownership changed")
			}
			if target == "" {
				target = temporary.Spec.VolumeName
			}
			if err := s.deleteMoveClaim(ctx, app.Namespace, claim.TargetClaim, op.ID, true); err != nil {
				return err
			}
		} else if !apierrors.IsNotFound(err) {
			return err
		}
		active, retired := claim.SourcePV, target
		if forward {
			active, retired = target, claim.SourcePV
		}
		pv := &corev1.PersistentVolume{}
		if err := s.apps.Get(ctx, types.NamespacedName{Name: active}, pv); err != nil {
			return err
		}
		if pv.Annotations[projectOperationLabel] == op.ID {
			before := pv.DeepCopy()
			delete(pv.Annotations, projectOperationLabel)
			pv.Spec.PersistentVolumeReclaimPolicy = claim.SourceReclaim
			if err := s.apps.Patch(ctx, pv, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
				return err
			}
		}
		if retired == "" {
			continue
		}
		old := &corev1.PersistentVolume{}
		if err := s.apps.Get(ctx, types.NamespacedName{Name: retired}, old); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if retired == target && old.Annotations[projectOperationLabel] == "" && old.Spec.PersistentVolumeReclaimPolicy == corev1.PersistentVolumeReclaimDelete {
			continue
		}
		if old.Annotations[projectOperationLabel] != op.ID {
			return errors.New("retired disk ownership changed")
		}
		if forward && claim.RetainSource {
			if err := s.retainMigratedDisk(ctx, app, op, claim, old); err != nil {
				return err
			}
			continue
		}
		// Reclaim through the provisioner after the winning copy is serving.
		// Do not remove finalizers or unlink host paths from the API server.
		before := old.DeepCopy()
		old.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimDelete
		if err := s.apps.Patch(ctx, old, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
	}
	return nil
}
