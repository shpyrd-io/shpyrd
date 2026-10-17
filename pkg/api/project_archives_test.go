package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/projectarchive"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestProjectArchiveEmptyProjectHTTPRoundTrip(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: "app-empty"}, Status: shpyrdv1.AppStatus{Phase: shpyrdv1.PhasePending}}
	config := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: app.EnvSecretName(), Namespace: app.Namespace}, Data: map[string][]byte{"SETTING": []byte("before")}}
	s, cr := newTestServer(t, nil, []client.Object{app, config})
	response := do(t, s, "POST", "/api/project-archives/empty/export", "", true)
	if response.Code != http.StatusOK {
		t.Fatalf("export: %d %s", response.Code, response.Body.String())
	}
	var ticket struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &ticket); err != nil || ticket.Ticket == "" {
		t.Fatalf("ticket: %s %v", response.Body.String(), err)
	}
	response = do(t, s, "GET", "/api/project-archives/empty/download?ticket="+ticket.Ticket, "", true)
	if response.Code != http.StatusOK {
		t.Fatalf("download: %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("missing private download headers")
	}
	archive := response.Body.String()
	b, err := projectarchive.Read(ctx, t.TempDir(), strings.NewReader(archive), projectarchive.DefaultLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	metadata, err := validateProjectArchive(ctx, b)
	if err != nil || string(metadata.Config["SETTING"]) != "before" {
		t.Fatalf("metadata: %+v %v", metadata, err)
	}
	if response = do(t, s, "GET", "/api/project-archives/empty/download?ticket="+ticket.Ticket, "", true); response.Code != http.StatusGone {
		t.Fatalf("ticket replay %d", response.Code)
	}
	if err := cr.Get(ctx, client.ObjectKeyFromObject(config), config); err != nil {
		t.Fatal(err)
	}
	config.Data["SETTING"] = []byte("after")
	if err := cr.Update(ctx, config); err != nil {
		t.Fatal(err)
	}
	response = do(t, s, "POST", "/api/project-archives/empty/restore", archive, true)
	if response.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", response.Code, response.Body.String())
	}
	if err := cr.Get(ctx, client.ObjectKeyFromObject(config), config); err != nil {
		t.Fatal(err)
	}
	if string(config.Data["SETTING"]) != "before" {
		t.Fatalf("configuration was not restored: %v", config.Data)
	}
	if response = do(t, s, "GET", "/api/project-archives/empty", "", true); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"phase":"idle"`) {
		t.Fatalf("status: %d %s", response.Code, response.Body.String())
	}
}

func TestProjectArchiveRejectsBadUploadBeforeMaintenance(t *testing.T) {
	app := sampleApp("shop", shpyrdv1.PhaseRunning)
	s, cr := newTestServer(t, nil, []client.Object{app})
	response := do(t, s, "POST", "/api/project-archives/shop/restore", "corrupt archive", true)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	current := &shpyrdv1.App{}
	if err := cr.Get(context.Background(), client.ObjectKeyFromObject(app), current); err != nil {
		t.Fatal(err)
	}
	if current.Annotations[shpyrdv1.AnnotationMaintenance] != "" {
		t.Fatal("invalid archive paused project")
	}
}

type unreadArchiveBody struct{ t *testing.T }

func (b unreadArchiveBody) Read([]byte) (int, error) {
	b.t.Error("read upload despite an existing operation")
	return 0, io.EOF
}
func (b unreadArchiveBody) Close() error { return nil }

func TestProjectArchiveRejectsUnfinishedOperationBeforeUpload(t *testing.T) {
	app := sampleApp("shop", shpyrdv1.PhaseRunning)
	s, _ := newTestServer(t, nil, []client.Object{app})
	if _, err := s.beginProjectArchive(context.Background(), app, "restore", &projectArchiveMetadata{}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/project-archives/shop/restore", unreadArchiveBody{t})
	req.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusConflict {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
}

func TestProjectArchiveTicketCannotCrossWorkspaces(t *testing.T) {
	s, _, _ := newTenantServer(t)
	b, err := projectarchive.New(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err = b.Add(context.Background(), "project.json", strings.NewReader(`{}`)); err != nil {
		t.Fatal(err)
	}
	file, err := b.File(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := s.archiveDownloads.keep(b, file, "project.tgz", "app-acme-shop")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/project-archives/shop/download?ticket=" + ticket
	if got := at(t, s, "example.test", "GET", path, ""); got.Code != http.StatusGone {
		t.Fatalf("cross-workspace download: %d", got.Code)
	}
	if got := at(t, s, "acme.shpyrd.test", "GET", path, ""); got.Code != http.StatusOK {
		t.Fatalf("own download: %d %s", got.Code, got.Body.String())
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary archive not removed: %v", err)
	}
}

func TestProjectArchiveRecoveryOnlyClearsItsOwnDatabaseMaintenance(t *testing.T) {
	ctx := context.Background()
	op := &projectArchiveOperation{ID: "current", Original: projectArchiveMetadata{Databases: []archivedDatabase{{Name: "db"}, {Name: "other"}}}}
	db := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-shop", Annotations: map[string]string{shpyrdv1.AnnotationMaintenance: "current"}}}
	other := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "app-shop", Annotations: map[string]string{shpyrdv1.AnnotationMaintenance: "different"}}}
	s, cr := newTestServer(t, nil, []client.Object{db, other})
	if err := s.clearArchiveDatabaseMaintenance(ctx, "app-shop", op); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"db", "other"} {
		var pg shpyrdv1.Postgres
		if err := cr.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: name}, &pg); err != nil {
			t.Fatal(err)
		}
		want := ""
		if name == "other" {
			want = "different"
		}
		if pg.Annotations[shpyrdv1.AnnotationMaintenance] != want {
			t.Fatalf("%s maintenance=%q", name, pg.Annotations[shpyrdv1.AnnotationMaintenance])
		}
	}
}

// The platform-created backups bucket of a database never blocks an
// archive; a bucket of the project's own travels by reference (storage
// plan step 5): the manifest names it with its cap and its logical name,
// never a credential, and the platform's is not among them.
func TestProjectArchiveCarriesBucketsByReference(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}, Spec: shpyrdv1.AppSpec{Image: "ghcr.io/o/shop@sha256:" + strings.Repeat("a", 64)}}
	db := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: app.Namespace, UID: "db-uid"}}
	owned := true
	backups := &shpyrdv1.ObjectBucket{ObjectMeta: metav1.ObjectMeta{
		Name: "db-backups", Namespace: app.Namespace,
		Labels:          map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", shpyrdv1.LabelBucketPostgres: db.Name},
		OwnerReferences: []metav1.OwnerReference{{APIVersion: shpyrdv1.GroupVersion.String(), Kind: "Postgres", Name: db.Name, UID: db.UID, Controller: &owned, BlockOwnerDeletion: &owned}},
	}}
	cap := resource.MustParse("1Gi")
	uploads := &shpyrdv1.ObjectBucket{
		ObjectMeta: metav1.ObjectMeta{Name: "uploads", Namespace: app.Namespace, Labels: map[string]string{shpyrdv1.LabelProject: "shop"}},
		Spec:       shpyrdv1.ObjectBucketSpec{MaxBytes: &cap, Public: true, SecretName: "uploads-key"},
		Status:     shpyrdv1.ObjectBucketStatus{Phase: shpyrdv1.BucketReady, Bucket: "shpyrd-0123", SecretName: "uploads-key"},
	}
	s, _ := newTestServer(t, nil, []client.Object{app, db, backups, uploads})
	m, err := s.projectArchiveMetadata(ctx, app)
	if err != nil {
		t.Fatalf("the buckets blocked the portable export: %v", err)
	}
	if len(m.Databases) != 1 || m.Databases[0].Name != "db" {
		t.Fatalf("databases: %+v", m.Databases)
	}
	if len(m.Buckets) != 1 || m.Buckets[0].Name != "uploads" || m.Buckets[0].Logical != "shpyrd-0123" || m.Buckets[0].Files || !m.Buckets[0].Spec.Public || m.Buckets[0].Spec.MaxBytes == nil || m.Buckets[0].Spec.SecretName != "" {
		t.Fatalf("buckets: %+v", m.Buckets)
	}
	if _, err := s.projectMetadata(ctx, app, false); err != nil {
		t.Fatalf("a move: %v", err)
	}
	raw, _ := json.Marshal(m)
	if strings.Contains(string(raw), "AWS_") || strings.Contains(string(raw), "secretAccessKey") {
		t.Error("the manifest carries a credential")
	}
}

// A restore makes the buckets the archive names (storage plan step 5):
// in another namespace on the gateway the bucket serves the objects
// under their first name through an alias; a binding to it is accepted
// and counted against the workspace's buckets; a rollback takes the
// bucket but never the objects.
func TestProjectRestoreRebindsBucketsByReference(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-acme-shop", Labels: project.NamespaceLabels("acme", "shop")}, Spec: shpyrdv1.AppSpec{Slug: "shop", Image: "ghcr.io/o/shop@sha256:" + strings.Repeat("a", 64)}}
	s, cr := newTestServer(t, nil, []client.Object{app})
	s.opts.Vars = func(key string) string { return map[string]string{install.VarGatewayBucket: "shpyrd-objects"}[key] }
	cap := resource.MustParse("1Gi")
	m := &projectArchiveMetadata{Name: "shop", Spec: shpyrdv1.AppSpec{Image: app.Spec.Image, Bindings: []shpyrdv1.Binding{{Kind: "ObjectBucket", Name: "uploads", Prefix: "AWS"}}}, Buckets: []archivedBucket{{Name: "uploads", Spec: shpyrdv1.ObjectBucketSpec{MaxBytes: &cap}, Logical: objectstore.LogicalBucketName("app-shop", "uploads")}}}
	original := &projectArchiveMetadata{Name: "shop", Spec: app.Spec}
	if err := s.checkArchiveCompatibility(ctx, app, original, m); err != nil {
		t.Fatalf("a binding to an archived bucket: %v", err)
	}
	missing := *m
	missing.Buckets = nil
	if err := s.checkArchiveCompatibility(ctx, app, original, &missing); err == nil || !strings.Contains(err.Error(), "missing bucket binding uploads") {
		t.Errorf("a binding to a bucket the archive does not name = %v", err)
	}
	// The operation's record, which beginProjectArchive writes, holds
	// what the restore created.
	for ns, id := range map[string]string{app.Namespace: "op-1", "app-shop": "op-2"} {
		if err := cr.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: projectOperationSecret, Namespace: ns, Labels: map[string]string{projectOperationLabel: id}}}); err != nil {
			t.Fatal(err)
		}
	}
	op := &projectArchiveOperation{ID: "op-1", Kind: "restore", Original: *original}
	if err := s.createArchiveResources(ctx, app, op, m); err != nil {
		t.Fatal(err)
	}
	b := &shpyrdv1.ObjectBucket{}
	if err := cr.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: "uploads"}, b); err != nil {
		t.Fatal(err)
	}
	if b.Spec.Alias != objectstore.LogicalBucketName("app-shop", "uploads") || b.Labels[shpyrdv1.LabelProject] != "shop" || b.Labels[projectOperationLabel] != "op-1" || b.CapBytes() != 1<<30 {
		t.Errorf("the restored bucket = %+v", b)
	}
	if len(op.CreatedBuckets) != 1 || op.CreatedBuckets[0] != "uploads" {
		t.Errorf("created = %v", op.CreatedBuckets)
	}
	// The same namespace needs no alias: the objects are under its own name.
	same := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}, Spec: shpyrdv1.AppSpec{Slug: "shop"}}
	if err := cr.Create(ctx, same); err != nil {
		t.Fatal(err)
	}
	op2 := &projectArchiveOperation{ID: "op-2", Kind: "restore"}
	if err := s.createArchiveResources(ctx, same, op2, m); err != nil {
		t.Fatal(err)
	}
	if err := cr.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: "uploads"}, b); err != nil || b.Spec.Alias != "" {
		t.Errorf("a bucket restored into its own namespace = %+v, %v", b.Spec, err)
	}
	if !slices.Contains(op2.Warnings, bucketReferenceNote("uploads")) {
		t.Errorf("the report does not say the bucket travels by reference: %v", op2.Warnings)
	}
	// Rollback: the bucket goes with Retain, so nothing under it is emptied.
	if err := s.removeCreatedArchiveResources(ctx, app, op); err != nil {
		t.Fatal(err)
	}
	if err := cr.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: "uploads"}, b); err == nil {
		if b.DeletionTimestamp.IsZero() || b.Spec.DeletionPolicy != "Retain" {
			t.Errorf("after the rollback the bucket = %+v", b)
		}
	} else if !apierrors.IsNotFound(err) {
		t.Fatal(err)
	}
	if op.CreatedBuckets != nil {
		t.Errorf("created after the rollback = %v", op.CreatedBuckets)
	}
}
