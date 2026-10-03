package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/projectarchive"
)

// Run only against the disposable cluster documented in contrib. Never use
// the caller's current kubeconfig/context implicitly.
func TestProjectArchiveKubernetesRoundTrip(t *testing.T) {
	path := os.Getenv("SHPYRD_ARCHIVE_TEST_KUBECONFIG")
	if path == "" {
		t.Skip("set SHPYRD_ARCHIVE_TEST_KUBECONFIG to a disposable kind cluster")
	}
	raw, err := clientcmd.LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if raw.CurrentContext != "kind-shpyrd-portability-test" {
		t.Fatal("refusing to run outside kind-shpyrd-portability-test")
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		t.Fatal(err)
	}
	k, err := kube.NewClient(cfg, "shpyrd-system")
	if err != nil {
		t.Fatal(err)
	}
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	cr, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
	defer cancel()
	ns := fmt.Sprintf("archive-e2e-%d", time.Now().Unix())
	reuse := os.Getenv("SHPYRD_ARCHIVE_TEST_NAMESPACE")
	if reuse != "" {
		if !strings.HasPrefix(reuse, "archive-e2e-") {
			t.Fatal("invalid disposable namespace")
		}
		ns = reuse
	}
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
	if err := cr.Create(ctx, namespace); err != nil && reuse == "" {
		t.Fatal(err)
	}
	defer func() { _ = cr.Delete(context.Background(), namespace) }()
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "project", Namespace: ns}}
	if err := cr.Create(ctx, app); err != nil && reuse == "" {
		t.Fatal(err)
	}
	s, err := newServer(k, Options{Token: testToken, Apps: cr, Public: PublicConfig{Domain: "test.invalid"}, Vars: func(key string) string {
		if key == install.VarServerImage {
			if image := os.Getenv("SHPYRD_ARCHIVE_TEST_IMAGE"); image != "" {
				return image
			}
			return "shpyrd-portability-test:latest"
		}
		return ""
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.waitArchiveCondition(ctx, func(c context.Context) (bool, error) {
		account := &corev1.ServiceAccount{}
		err := cr.Get(c, client.ObjectKey{Namespace: ns, Name: "default"}, account)
		return err == nil, client.IgnoreNotFound(err)
	}); err != nil {
		t.Fatal(err)
	}
	recorder := record.NewFakeRecorder(10000)
	ar := &controller.AppReconciler{Client: cr, APIReader: cr, Scheme: scheme, Recorder: recorder, Config: controller.Config{Domain: "test.invalid"}.Defaults()}
	vr := &controller.VolumeReconciler{Client: cr, Scheme: scheme, Recorder: recorder, DefaultClass: controller.LocalStorageClass, SharedClass: controller.LocalStorageClass}
	pr := &controller.PostgresReconciler{Client: cr, Scheme: scheme, Recorder: recorder, SystemNamespace: k.Namespace, Storage: controller.StorageProfile{Class: controller.LocalStorageClass}}
	loop, stop := context.WithCancel(context.Background())
	appKey := client.ObjectKeyFromObject(app)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for {
			_, _ = ar.Reconcile(loop, ctrl.Request{NamespacedName: appKey})
			var volumes shpyrdv1.VolumeList
			_ = cr.List(loop, &volumes, client.InNamespace(ns))
			for _, v := range volumes.Items {
				_, _ = vr.Reconcile(loop, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&v)})
			}
			var databases shpyrdv1.PostgresList
			_ = cr.List(loop, &databases, client.InNamespace(ns))
			for _, pg := range databases.Items {
				_, _ = pr.Reconcile(loop, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&pg)})
			}
			select {
			case <-loop.Done():
				return
			case <-tick.C:
			}
		}
	}()
	defer func() { stop(); wg.Wait() }()
	if reuse != "" {
		if err := cr.Get(ctx, appKey, app); err != nil {
			t.Fatal(err)
		}
		base := "/api/cluster/project-archives/" + archiveProjectID(app)
		request := archiveKubernetesRequest(t, ctx, s)
		if _, err := s.readProjectArchive(ctx, ns); err == nil {
			request("POST", base+"/recover", nil)
			t.Log("recovered durable maintenance after test API interruption")
		}
		runKubernetesMoves(t, ctx, s, app, base, request)
		return
	}
	if err := s.waitArchiveCondition(ctx, func(c context.Context) (bool, error) {
		if err := cr.Get(c, client.ObjectKeyFromObject(app), app); err != nil {
			return false, err
		}
		return app.Status.Phase == shpyrdv1.PhasePending, nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"uploads", "documents"} {
		volume := &shpyrdv1.Volume{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Spec: shpyrdv1.VolumeSpec{Size: resource.MustParse("1Gi")}}
		if err := cr.Create(ctx, volume); err != nil {
			t.Fatal(err)
		}
		if err := s.waitArchiveCondition(ctx, func(c context.Context) (bool, error) {
			pvc := &corev1.PersistentVolumeClaim{}
			return cr.Get(c, client.ObjectKey{Namespace: ns, Name: volume.PVCName()}, pvc) == nil, nil
		}); err != nil {
			t.Fatal(err)
		}
		command, err := s.archiveVolumeHelper(ctx, app, volume, "seed")
		if err != nil {
			t.Fatal(err)
		}
		// The test image includes a shell solely to seed and inspect the test PVC.
		if err := command(ctx, []string{"sh", "-c", "printf original > /data/value"}, nil, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"primary", "analytics"} {
		pg := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Spec: shpyrdv1.PostgresSpec{Version: "17"}}
		if err := cr.Create(ctx, pg); err != nil {
			t.Fatal(err)
		}
		var db projectarchive.PostgreSQL
		if err := s.waitArchiveCondition(ctx, func(c context.Context) (bool, error) {
			var err error
			db, err = s.archiveDatabase(c, ns, name)
			if err != nil {
				return false, nil
			}
			return db.Check(c) == nil, nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(ctx, []string{"psql", "-U", "postgres", "-d", "app", "-v", "ON_ERROR_STOP=1", "-c", "CREATE TABLE marker(value text); INSERT INTO marker VALUES ('original')"}, nil, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.cleanupArchiveHelpers(ctx, ns, "seed"); err != nil {
		t.Fatal(err)
	}
	// Use the HTTP handler and its binary response, with normal auth, just as
	// the browser does. The target project deliberately has no application image.
	request := archiveKubernetesRequest(t, ctx, s)
	// This legacy namespace is not app-<slug>; the console middleware resolves
	// its immutable project identity, which is the operator's actual API path.
	if err := cr.Get(ctx, client.ObjectKeyFromObject(app), app); err != nil {
		t.Fatal(err)
	}
	base := "/api/cluster/project-archives/" + archiveProjectID(app)
	for _, group := range []string{"volume:uploads", "postgres:primary"} {
		data, _ := json.Marshal(map[string]string{"group": group})
		measurement := request("POST", base+"/placement/measure", bytes.NewReader(data))
		var result struct {
			DiskUsedBytes int64 `json:"diskUsedBytes"`
		}
		if err := json.Unmarshal(measurement.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.DiskUsedBytes <= 0 || group == "volume:uploads" && result.DiskUsedBytes != 8 {
			t.Fatalf("invalid measurement for %s: %s", group, measurement.Body.String())
		}
		if err := cr.Get(ctx, client.ObjectKeyFromObject(app), app); err != nil {
			t.Fatal(err)
		}
		if app.Annotations[shpyrdv1.AnnotationMaintenance] != "" {
			t.Fatal("read-only measurement paused project")
		}
	}
	t.Log("measured volume and PostgreSQL data without maintenance")
	out := request("POST", base+"/export", nil)
	ticket := struct {
		Ticket string `json:"ticket"`
	}{}
	if err := json.Unmarshal(out.Body.Bytes(), &ticket); err != nil {
		t.Fatal(err)
	}
	archive := request("GET", base+"/download?ticket="+ticket.Ticket, nil).Body.Bytes()
	for _, name := range []string{"uploads", "documents"} {
		v := &shpyrdv1.Volume{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
		command, err := s.archiveVolumeHelper(ctx, app, v, "mutate")
		if err != nil {
			t.Fatal(err)
		}
		if err := command(ctx, []string{"sh", "-c", "printf changed > /data/value"}, nil, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.cleanupArchiveHelpers(ctx, ns, "mutate"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"primary", "analytics"} {
		db, err := s.archiveDatabase(ctx, ns, name)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(ctx, []string{"psql", "-U", "postgres", "-d", "app", "-c", "UPDATE marker SET value='changed'"}, nil, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	request("POST", base+"/restore", bytes.NewReader(archive))
	for _, name := range []string{"uploads", "documents"} {
		v := &shpyrdv1.Volume{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
		command, err := s.archiveVolumeHelper(ctx, app, v, "verify")
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := command(ctx, []string{"cat", "/data/value"}, nil, &out); err != nil {
			t.Fatal(err)
		}
		if out.String() != "original" {
			t.Fatalf("%s: %q", name, out.String())
		}
	}
	for _, name := range []string{"primary", "analytics"} {
		db, err := s.archiveDatabase(ctx, ns, name)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := db.Exec(ctx, []string{"psql", "-U", "postgres", "-d", "app", "-Atc", "SELECT value FROM marker"}, nil, &out); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(out.String()) != "original" {
			t.Fatalf("%s: %q", name, out.String())
		}
	}
	t.Logf("HTTP archive round trip restored 2 volumes and 2 real CNPG databases (%d archive bytes)", len(archive))
	if err := s.cleanupArchiveHelpers(ctx, ns, "verify"); err != nil {
		t.Fatal(err)
	}
	runKubernetesMoves(t, ctx, s, app, base, request)
}

func archiveKubernetesRequest(t *testing.T, ctx context.Context, s *Server) func(string, string, io.Reader) *httptest.ResponseRecorder {
	return func(method, path string, body io.Reader) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, body).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer "+testToken)
		out := httptest.NewRecorder()
		s.Handler().ServeHTTP(out, req)
		if out.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", method, path, out.Code, out.Body.String())
		}
		return out
	}
}

func runKubernetesMoves(t *testing.T, ctx context.Context, s *Server, app *shpyrdv1.App, base string, request func(string, string, io.Reader) *httptest.ResponseRecorder) {
	t.Helper()
	ns := app.Namespace
	cr := s.apps
	var nodes corev1.NodeList
	if err := cr.List(ctx, &nodes); err != nil {
		t.Fatal(err)
	}
	// Move an unmounted volume and a database independently. Their original
	// PVs remain available until the destination is verified and resumed.
	for _, group := range []string{"volume:uploads", "postgres:primary"} {
		groups, _, err := s.projectPlacement(ctx, app)
		if err != nil {
			t.Fatal(err)
		}
		var sources []string
		for _, g := range groups {
			if g.ID == group {
				sources = g.Nodes
			}
		}
		target := ""
		for _, node := range nodes.Items {
			if eligiblePlacementNode(&node) != "" {
				continue
			}
			source := false
			for _, name := range sources {
				if name == node.Name || name == node.Labels[corev1.LabelHostname] {
					source = true
				}
			}
			if !source {
				target = node.Name
				break
			}
		}
		if target == "" {
			t.Fatal("no different ready destination node")
		}
		t.Logf("moving %s from %v to %s", group, sources, target)
		data, _ := json.Marshal(map[string]string{"group": group, "node": target})
		request("POST", base+"/move", bytes.NewReader(data))
		t.Logf("moved %s", group)
	}
	v := &shpyrdv1.Volume{ObjectMeta: metav1.ObjectMeta{Name: "uploads", Namespace: ns}}
	command, err := s.archiveVolumeHelper(ctx, app, v, "verify-move")
	if err != nil {
		t.Fatal(err)
	}
	var contents bytes.Buffer
	if err := command(ctx, []string{"cat", "/data/value"}, nil, &contents); err != nil {
		t.Fatal(err)
	}
	if contents.String() != "original" {
		t.Fatalf("moved volume: %s", contents.String())
	}
	db, err := s.archiveDatabase(ctx, ns, "primary")
	if err != nil {
		t.Fatal(err)
	}
	contents.Reset()
	if err := db.Exec(ctx, []string{"psql", "-U", "postgres", "-d", "app", "-Atc", "SELECT value FROM marker"}, nil, &contents); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(contents.String()) != "original" {
		t.Fatalf("moved database: %s", contents.String())
	}
	t.Log("both physical node moves preserved the original contents")

}
