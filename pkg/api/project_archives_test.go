package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/projectarchive"
	corev1 "k8s.io/api/core/v1"
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
