package api

import (
	"context"
	"encoding/json"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/projectarchive"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Emulate only the binder's Bound status. All lifecycle assertions below use
// real API methods; a disposable Kubernetes test covers the real PV controller.
type migrationBinder struct{ client.Client }

func (b migrationBinder) Create(ctx context.Context, object client.Object, options ...client.CreateOption) error {
	if err := b.Client.Create(ctx, object, options...); err != nil {
		return err
	}
	if claim, ok := object.(*corev1.PersistentVolumeClaim); ok {
		pv := &corev1.PersistentVolume{}
		if err := b.Get(ctx, client.ObjectKey{Name: claim.Spec.VolumeName}, pv); err != nil {
			return err
		}
		if claim.Spec.StorageClassName == nil || *claim.Spec.StorageClassName != pv.Spec.StorageClassName {
			return context.Canceled
		}
		claim.Status.Phase = corev1.ClaimBound
		return b.Status().Update(ctx, claim)
	}
	return nil
}

func migrationFixture(t *testing.T) (*Server, *shpyrdv1.App, *projectArchiveOperation) {
	t.Helper()
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "project", Namespace: "app-project", UID: "project-uid"}}
	vol := &shpyrdv1.Volume{ObjectMeta: metav1.ObjectMeta{Name: "uploads", Namespace: app.Namespace}, Spec: shpyrdv1.VolumeSpec{StorageClass: "provider", AccessMode: corev1.ReadWriteMany, Size: resource.MustParse("50Gi"), FromSnapshot: "seed-snapshot"}}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: vol.PVCName(), Namespace: app.Namespace, UID: "old-claim", Annotations: map[string]string{"pv.kubernetes.io/bind-completed": "yes", "volume.kubernetes.io/selected-node": "old-node", "cnpg.io/instanceName": "keep"}}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "old-disk", StorageClassName: ptr.To("provider"), AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("50Gi")}}, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"provider": "old"}}, DataSource: &corev1.TypedLocalObjectReference{Kind: "VolumeSnapshot", Name: "seed-snapshot"}}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	source := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "old-disk"}, Spec: corev1.PersistentVolumeSpec{StorageClassName: "provider", PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete, Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("50Gi")}, ClaimRef: &corev1.ObjectReference{Namespace: app.Namespace, Name: claim.Name, UID: claim.UID}}, Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeReleased}}
	sc := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: controller.LocalStorageClass}, Provisioner: "shpyrd.io/local-path"}
	s, cr := newTestServer(t, nil, []client.Object{app, vol, claim, source, sc})
	s.apps = migrationBinder{cr}
	move, err := s.prepareProjectMove(context.Background(), app, placementGroup{PlacementGroup: controller.PlacementGroup{ID: "volume:uploads", Volumes: []string{"uploads"}}}, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{corev1.LabelHostname: "target"}}}, controller.LocalStorageClass)
	if err != nil {
		t.Fatal(err)
	}
	if !move.Claims[0].RetainSource {
		t.Fatal("provider source not marked for retention")
	}
	move.Claims[0].TargetClaim = "temporary"
	move.Claims[0].TargetPV = "local-disk"
	target := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "local-disk"}, Spec: corev1.PersistentVolumeSpec{StorageClassName: controller.LocalStorageClass, PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete}}
	if err := cr.Create(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	op := &projectArchiveOperation{ID: "migration", Kind: "move", Move: move, StartedAt: time.Now(), State: projectarchive.TransactionState{Phase: "committing"}}
	for _, name := range []string{"old-disk", "local-disk"} {
		if err := s.retainMovePV(context.Background(), name, op.ID); err != nil {
			t.Fatal(err)
		}
	}
	return s, app, op
}

func TestProviderMigrationRebindsSameClaimAndRollsBackStorageIntent(t *testing.T) {
	s, app, op := migrationFixture(t)
	ctx := context.Background()
	claim := &op.Move.Claims[0]
	for _, forward := range []bool{true, false} {
		if err := s.bindMoveClaim(ctx, app, op, claim, forward); err != nil {
			t.Fatal(err)
		}
		if err := s.setMoveStorage(ctx, app, op, forward); err != nil {
			t.Fatal(err)
		}
		current := &corev1.PersistentVolumeClaim{}
		if err := s.apps.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: claim.Original.Name}, current); err != nil {
			t.Fatal(err)
		}
		volume := &shpyrdv1.Volume{}
		if err := s.apps.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: "uploads"}, volume); err != nil {
			t.Fatal(err)
		}
		if forward {
			if current.Spec.VolumeName != "local-disk" || *current.Spec.StorageClassName != controller.LocalStorageClass || current.Spec.AccessModes[0] != corev1.ReadWriteOnce || current.Spec.Selector != nil || current.Spec.DataSource != nil {
				t.Fatalf("unsafe local binding: %+v", current.Spec)
			}
			if volume.Spec.StorageClass != controller.LocalStorageClass || volume.Spec.FromSnapshot != "" || !volume.Shared() {
				t.Fatal("volume intent did not migrate")
			}
		} else {
			if current.Spec.VolumeName != "old-disk" || *current.Spec.StorageClassName != "provider" || current.Spec.AccessModes[0] != corev1.ReadWriteMany || current.Spec.DataSource == nil {
				t.Fatal("original binding not restored")
			}
			if volume.Spec.StorageClass != "provider" || volume.Spec.FromSnapshot != "seed-snapshot" {
				t.Fatal("original volume intent not restored")
			}
		}
		if current.Annotations["volume.kubernetes.io/selected-node"] != "" || current.Annotations["cnpg.io/instanceName"] != "keep" {
			t.Fatal("PVC identity annotations not preserved correctly")
		}

		if forward {
			if err := s.apps.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: projectOperationSecret, Namespace: app.Namespace, Labels: map[string]string{projectOperationLabel: op.ID}}}); err != nil {
				t.Fatal(err)
			}
			if err := s.saveProjectArchive(ctx, app.Namespace, op); err != nil {
				t.Fatal(err)
			}
			// Simulate an API restart after rebinding. Rollback must use only the
			// durable journal, not in-memory Volume specs or original claim pointers.
			var err error
			op, err = s.readProjectArchive(ctx, app.Namespace)
			if err != nil {
				t.Fatal(err)
			}
			claim = &op.Move.Claims[0]
		}
	}
}

func TestMigrationRetainsOldDiskUntilSeparateAuthorizedDeletion(t *testing.T) {
	s, app, op := migrationFixture(t)
	ctx := context.Background()
	if err := s.bindMoveClaim(ctx, app, op, &op.Move.Claims[0], true); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.finishProjectMove(ctx, app, op, true); err != nil {
			t.Fatal(err)
		}
	}
	retained, err := s.retainedMigrationDisks(ctx, app)
	if err != nil || len(retained) != 1 || retained[0].Deleting {
		t.Fatalf("retained=%+v, %v", retained, err)
	}
	pv := &corev1.PersistentVolume{}
	if err := s.apps.Get(ctx, client.ObjectKey{Name: "old-disk"}, pv); err != nil {
		t.Fatal(err)
	}
	if pv.Spec.PersistentVolumeReclaimPolicy != corev1.PersistentVolumeReclaimRetain {
		t.Fatal("old disk would be deleted")
	}
	path := "/api/cluster/project-archives/" + archiveProjectID(app) + "/retained-volumes/old-disk"
	if response := do(t, s, "DELETE", path, "", false); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated cleanup: %d", response.Code)
	}
	// A concurrent/recovered operation must finish before cleanup can proceed.
	if err := s.apps.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: projectOperationSecret, Namespace: app.Namespace, Labels: map[string]string{projectOperationLabel: op.ID}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.saveProjectArchive(ctx, app.Namespace, op); err != nil {
		t.Fatal(err)
	}
	if response := do(t, s, "DELETE", path, "", true); response.Code != http.StatusConflict {
		t.Fatalf("cleanup during maintenance: %d %s", response.Code, response.Body)
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: projectOperationSecret, Namespace: app.Namespace}}
	if err := s.apps.Delete(ctx, secret); err != nil {
		t.Fatal(err)
	}
	// Project recreation must not inherit authorization to erase old data.
	impostor := *app
	impostor.UID = types.UID("different")
	if disks, err := s.retainedMigrationDisks(ctx, &impostor); err != nil || len(disks) != 0 {
		t.Fatal("retained disk leaked to a different project identity")
	}
	if response := do(t, s, "DELETE", path, "", true); response.Code != http.StatusOK {
		t.Fatalf("cleanup: %d %s", response.Code, response.Body)
	}
	if err := s.apps.Get(ctx, client.ObjectKey{Name: "old-disk"}, pv); err != nil {
		t.Fatal(err)
	}
	if pv.Spec.PersistentVolumeReclaimPolicy != corev1.PersistentVolumeReclaimDelete {
		t.Fatal("provider deletion not requested")
	}
}

func TestMigrationRollbackNeverRetiresSourceDisk(t *testing.T) {
	s, app, op := migrationFixture(t)
	if err := s.finishProjectMove(context.Background(), app, op, false); err != nil {
		t.Fatal(err)
	}
	disks, err := s.retainedMigrationDisks(context.Background(), app)
	if err != nil || len(disks) != 0 {
		t.Fatal("rollback cataloged live source as retired")
	}
	source := &corev1.PersistentVolume{}
	if err := s.apps.Get(context.Background(), client.ObjectKey{Name: "old-disk"}, source); err != nil {
		t.Fatal(err)
	}
	if source.Annotations[projectOperationLabel] != "" {
		t.Fatal("source ownership not restored")
	}
}

func TestMigrationDatabaseStorageChangesAndRecoversWithoutShrinking(t *testing.T) {
	ctx := context.Background()
	pg := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-project"}, Spec: shpyrdv1.PostgresSpec{Storage: ptr.To(resource.MustParse("5Gi"))}}
	cluster := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "postgresql.cnpg.io/v1", "kind": "Cluster", "metadata": map[string]interface{}{"name": pg.Name, "namespace": pg.Namespace}, "spec": map[string]interface{}{"storage": map[string]interface{}{"storageClass": "provider", "size": "50Gi"}}}}
	s, _ := newTestServer(t, nil, []client.Object{pg, cluster})
	original, _, _ := unstructured.NestedMap(cluster.Object, "spec", "storage")
	op := &projectArchiveOperation{Move: &projectMove{Group: placementGroup{Database: pg.Name}, BeforePostgresStorage: ptr.To(pg.Spec.Storage.DeepCopy()), BeforeDatabaseStorage: original}}
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Namespace: pg.Namespace}}
	for _, forward := range []bool{true, false} {
		if err := s.setMoveStorage(ctx, app, op, forward); err != nil {
			t.Fatal(err)
		}
		if err := s.apps.Get(ctx, client.ObjectKeyFromObject(pg), pg); err != nil {
			t.Fatal(err)
		}
		if err := s.apps.Get(ctx, client.ObjectKeyFromObject(cluster), cluster); err != nil {
			t.Fatal(err)
		}
		class, _, _ := unstructured.NestedString(cluster.Object, "spec", "storage", "storageClass")
		size, _, _ := unstructured.NestedString(cluster.Object, "spec", "storage", "size")
		if size != "50Gi" {
			t.Fatal("cluster storage shrank")
		}
		if forward && (class != controller.LocalStorageClass || pg.Spec.Storage.String() != "50Gi") {
			t.Fatal("database migration lost provider minimum")
		}
		if !forward && (class != "provider" || pg.Spec.Storage.String() != "5Gi") {
			t.Fatal("database intent not restored")
		}
	}
}

func TestMigrationRejectsDeletionOfReusedRetainedDisk(t *testing.T) {
	s, app, op := migrationFixture(t)
	ctx := context.Background()
	pv := &corev1.PersistentVolume{}
	if err := s.apps.Get(ctx, client.ObjectKey{Name: "old-disk"}, pv); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(retainedMigrationDisk{Namespace: app.Namespace, ProjectUID: app.UID, Claim: op.Move.Claims[0].Original.Name})
	pv.Annotations[retainedMigrationAnnotation] = string(data)
	if err := s.apps.Update(ctx, pv); err != nil {
		t.Fatal(err)
	}
	response := do(t, s, "DELETE", "/api/cluster/project-archives/"+archiveProjectID(app)+"/retained-volumes/old-disk", "", true)
	if response.Code != http.StatusConflict {
		t.Fatalf("deleted referenced provider disk: %d %s", response.Code, response.Body)
	}
}

func TestMigrationRequiresOptInBeforePausing(t *testing.T) {
	s, app, _ := migrationFixture(t)
	ctx := context.Background()
	// The fixture prepares owned disks for binding tests; remove that operation
	// marker to exercise the actual HTTP preflight on an ordinary provider PVC.
	pv := &corev1.PersistentVolume{}
	if err := s.apps.Get(ctx, client.ObjectKey{Name: "old-disk"}, pv); err != nil {
		t.Fatal(err)
	}
	delete(pv.Annotations, projectOperationLabel)
	if err := s.apps.Update(ctx, pv); err != nil {
		t.Fatal(err)
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "target", Labels: map[string]string{corev1.LabelHostname: "target"}}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}
	if err := s.apps.Create(ctx, node); err != nil {
		t.Fatal(err)
	}
	// On a profile whose disks are block volumes a move copies nothing: the
	// answer says so and does not ask for a migration to local storage.
	response := do(t, s, "POST", "/api/cluster/project-archives/"+archiveProjectID(app)+"/move", `{"group":"volume:uploads","node":"target"}`, true)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "follow their processes") {
		t.Fatalf("move of a block disk on a block profile: %d %s", response.Code, response.Body)
	}
	// On a node-local profile the provider disk must be migrated on purpose.
	s.opts.Vars = func(key string) string {
		if key == install.VarProjectStorageClass {
			return install.LocalStorageClass
		}
		return ""
	}
	response = do(t, s, "POST", "/api/cluster/project-archives/"+archiveProjectID(app)+"/move", `{"group":"volume:uploads","node":"target"}`, true)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "explicit migration") {
		t.Fatalf("migration opt-in: %d %s", response.Code, response.Body)
	}
	if err := s.apps.Get(ctx, client.ObjectKeyFromObject(app), app); err != nil {
		t.Fatal(err)
	}
	if app.Annotations[shpyrdv1.AnnotationMaintenance] != "" {
		t.Fatal("paused without migration consent")
	}
}

func TestMigrationMetadataAllowsUnrelatedResourcesWithoutExportingThem(t *testing.T) {
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}}
	bucket := &shpyrdv1.ObjectBucket{ObjectMeta: metav1.ObjectMeta{Name: "backups", Namespace: app.Namespace}}
	s, _ := newTestServer(t, nil, []client.Object{app, bucket})
	if _, err := s.projectArchiveMetadata(context.Background(), app); err == nil {
		t.Fatal("archive implied unsupported bucket data was exported")
	}
	if _, err := s.projectMetadata(context.Background(), app, false); err != nil {
		t.Fatalf("unrelated bucket blocked volume migration: %v", err)
	}
}

func TestMigrationDoesNotRollbackAfterWorkloadsCanWrite(t *testing.T) {
	s, app, op := migrationFixture(t)
	ctx := context.Background()
	if err := s.bindMoveClaim(ctx, app, op, &op.Move.Claims[0], true); err != nil {
		t.Fatal(err)
	}
	op.ResumeStarted = true
	// Even an interrupted phase update cannot erase the durable write boundary.
	op.State.Phase = "committing"
	if err := s.rollbackProjectMove(ctx, app, op); err == nil {
		t.Fatal("rolled back after workload startup")
	}
	if err := s.rollbackProjectArchive(ctx, app, op); err == nil {
		t.Fatal("restored stale archive data after workload startup")
	}
	current := &corev1.PersistentVolumeClaim{}
	if err := s.apps.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: "vol-uploads"}, current); err != nil {
		t.Fatal(err)
	}
	if current.Spec.VolumeName != "local-disk" {
		t.Fatal("new writes would be discarded")
	}
	// Journals created by the previous API may lack the new boolean.
	op.ResumeStarted = false
	op.State.Phase = "starting"
	if op.canRollback() {
		t.Fatal("legacy startup journal allowed unsafe rollback")
	}
}

func TestResumeJournalsWriteBoundaryBeforeUnfencingDatabases(t *testing.T) {
	s, app, op := migrationFixture(t)
	ctx := context.Background()
	op.Limits = map[string]int{"missing-primary": -1}
	if err := s.apps.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: projectOperationSecret, Namespace: app.Namespace, Labels: map[string]string{projectOperationLabel: op.ID}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.resumeProjectArchive(ctx, app, op); err == nil {
		t.Fatal("expected failure looking up missing primary")
	}
	saved, err := s.readProjectArchive(ctx, app.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.ResumeStarted || saved.canRollback() {
		t.Fatal("database unfencing started without durable write protection")
	}
}

func TestRecoveryFinishesMigrationAfterWorkloadsResume(t *testing.T) {
	s, app, op := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.bindMoveClaim(ctx, app, op, &op.Move.Claims[0], true); err != nil {
		t.Fatal(err)
	}
	op.ResumeStarted = true
	op.State.Phase = "committing" // crash before the next phase was recorded
	if err := s.apps.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: projectOperationSecret, Namespace: app.Namespace, Labels: map[string]string{projectOperationLabel: op.ID}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.saveProjectArchive(ctx, app.Namespace, op); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/cluster/project-archives/"+archiveProjectID(app)+"/recover", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("recovery failed: %d %s", response.Code, response.Body)
	}
	current := &corev1.PersistentVolumeClaim{}
	if err := s.apps.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: "vol-uploads"}, current); err != nil {
		t.Fatal(err)
	}
	if current.Spec.VolumeName != "local-disk" {
		t.Fatal("recovery discarded the new copy")
	}
	retained, err := s.retainedMigrationDisks(ctx, app)
	if err != nil || len(retained) != 1 {
		t.Fatal("recovery failed to retain the old provider disk")
	}
}

// A move with a target class carries a node-local disk onto the profile's
// block class: the claim keeps its name on the new disk with the new class,
// the Volume follows (rounded to the provider minimum), the old disk is
// retained; the way back restores both.
func TestClassMigrationRebindsOntoTheProfileClass(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "project", Namespace: "app-project", UID: "project-uid"}}
	vol := &shpyrdv1.Volume{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: app.Namespace}, Spec: shpyrdv1.VolumeSpec{AccessMode: corev1.ReadWriteOnce, Size: resource.MustParse("1Gi")}}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: vol.PVCName(), Namespace: app.Namespace, UID: "local-claim", Annotations: map[string]string{"volume.kubernetes.io/selected-node": "old-node"}}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "local-disk", StorageClassName: ptr.To(controller.LocalStorageClass), AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}}}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	source := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "local-disk"}, Spec: corev1.PersistentVolumeSpec{StorageClassName: controller.LocalStorageClass, PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete, Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}, ClaimRef: &corev1.ObjectReference{Namespace: app.Namespace, Name: claim.Name, UID: claim.UID}}, Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound}}
	block := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "oci-bv"}, Provisioner: "blockvolume.csi.oraclecloud.com"}
	s, cr := newTestServer(t, nil, []client.Object{app, vol, claim, source, block})
	s.apps = migrationBinder{cr}
	s.opts.Vars = func(key string) string {
		return map[string]string{install.VarStorageClass: "oci-bv", install.VarVolumeMinSize: "50Gi"}[key]
	}
	move, err := s.prepareProjectMove(ctx, app, placementGroup{PlacementGroup: controller.PlacementGroup{ID: "volume:data", Volumes: []string{"data"}}}, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{corev1.LabelHostname: "old-node"}}}, "oci-bv")
	if err != nil {
		t.Fatal(err)
	}
	if !move.Claims[0].RetainSource || move.targetClass() != "oci-bv" {
		t.Fatalf("a node-local source moving to block must be retained: %+v", move.Claims[0])
	}
	move.Claims[0].TargetClaim = "temporary"
	move.Claims[0].TargetPV = "block-disk"
	if err := cr.Create(ctx, &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "block-disk"}, Spec: corev1.PersistentVolumeSpec{StorageClassName: "oci-bv", PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete}}); err != nil {
		t.Fatal(err)
	}
	op := &projectArchiveOperation{ID: "class-migration", Kind: "move", Move: move, StartedAt: time.Now(), State: projectarchive.TransactionState{Phase: "committing"}}
	for _, name := range []string{"local-disk", "block-disk"} {
		if err := s.retainMovePV(ctx, name, op.ID); err != nil {
			t.Fatal(err)
		}
	}
	for _, forward := range []bool{true, false} {
		if err := s.bindMoveClaim(ctx, app, op, &op.Move.Claims[0], forward); err != nil {
			t.Fatal(err)
		}
		if err := s.setMoveStorage(ctx, app, op, forward); err != nil {
			t.Fatal(err)
		}
		current := &corev1.PersistentVolumeClaim{}
		if err := s.apps.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: claim.Name}, current); err != nil {
			t.Fatal(err)
		}
		volume := &shpyrdv1.Volume{}
		if err := s.apps.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: "data"}, volume); err != nil {
			t.Fatal(err)
		}
		if forward {
			if current.Spec.VolumeName != "block-disk" || *current.Spec.StorageClassName != "oci-bv" {
				t.Fatalf("forward binding: %+v", current.Spec)
			}
			if volume.Spec.StorageClass != "oci-bv" || volume.Spec.Size.String() != "50Gi" {
				t.Fatalf("forward volume: class %q size %s (want oci-bv, 50Gi)", volume.Spec.StorageClass, volume.Spec.Size.String())
			}
		} else {
			if current.Spec.VolumeName != "local-disk" || *current.Spec.StorageClassName != controller.LocalStorageClass {
				t.Fatalf("rollback binding: %+v", current.Spec)
			}
			if volume.Spec.StorageClass != "" || volume.Spec.Size.String() != "1Gi" {
				t.Fatalf("rollback volume: class %q size %s", volume.Spec.StorageClass, volume.Spec.Size.String())
			}
		}
	}
}
