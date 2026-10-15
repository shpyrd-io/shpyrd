package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/install"
)

// fakeClaimsProm answers the one instant query the fleet makes with what
// each claim of the shared namespace holds.
func fakeClaimsProm(t *testing.T, claims map[string]string) *PromClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := []map[string]any{}
		if strings.Contains(r.URL.Query().Get("query"), "kubelet_volume_stats_used_bytes") {
			for claim, value := range claims {
				result = append(result, map[string]any{"metric": map[string]string{"persistentvolumeclaim": claim}, "value": []any{1.0, value}})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": result}})
	}))
	t.Cleanup(srv.Close)
	return &PromClient{BaseURL: srv.URL, HTTP: srv.Client()}
}

const gi = int64(1 << 30)

// sharedFleet is two shared servers and what is on them, as the
// controller and the metering loop leave them: pg-shared-1 healthy with a
// pooler, three databases (one near its cap, one paused, one converting)
// and one kept behind a drop date; pg-shared-2 failing over with one
// database over its cap and a nightly run that failed. Two dedicated
// clusters beside.
func sharedFleet(t *testing.T) *Server {
	t.Helper()
	server := func(name, size string, status map[string]interface{}, annotations map[string]string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(controller.CNPGClusterGVK)
		u.SetNamespace(controller.SharedPostgresNamespace)
		u.SetName(name)
		u.SetLabels(map[string]string{sharedServerLabel: "server"})
		u.SetAnnotations(annotations)
		u.SetCreationTimestamp(metav1.NewTime(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)))
		u.Object["spec"] = map[string]interface{}{
			"imageName":  "ghcr.io/cloudnative-pg/postgresql:17",
			"instances":  int64(2),
			"storage":    map[string]interface{}{"size": size},
			"postgresql": map[string]interface{}{"parameters": map[string]interface{}{"max_connections": "400"}},
		}
		u.Object["status"] = status
		return u
	}
	one := server("pg-shared-1", "50Gi",
		map[string]interface{}{"phase": "Cluster in healthy state", "readyInstances": int64(2), "lastSuccessfulBackup": "2026-10-15T02:00:00Z",
			"conditions": []interface{}{map[string]interface{}{"type": "ContinuousArchiving", "status": "True"}}},
		map[string]string{controller.AnnotationPooler: "2026-10-10T10:00:00Z", controller.AnnotationConnectionsP95: "260"})
	two := server("pg-shared-2", "100Gi",
		map[string]interface{}{"phase": "Failing over", "readyInstances": int64(1)},
		map[string]string{controller.AnnotationConnectionsP95: "40"})
	database := func(hash, server, namespace, name string, annotations map[string]string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(controller.CNPGDatabaseGVK)
		u.SetNamespace(controller.SharedPostgresNamespace)
		u.SetName("db-" + hash)
		u.SetLabels(map[string]string{sharedOfServerLabel: server, sharedTenantNamespaceLabel: namespace, sharedTenantNameLabel: name})
		u.SetAnnotations(annotations)
		u.Object["spec"] = map[string]interface{}{"name": "db_" + hash, "cluster": map[string]interface{}{"name": server}}
		return u
	}
	cap := resource.MustParse("1Gi")
	tenant := func(namespace, name, hash, server string, percent int32, reason string) *shpyrdv1.Postgres {
		return &shpyrdv1.Postgres{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: types.UID("uid-" + hash),
				Annotations: map[string]string{controller.AnnotationSharedServer: server, controller.AnnotationSharedTenant: hash}},
			Spec: shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeShared, Storage: &cap},
			Status: shpyrdv1.ResourceStatus{Phase: shpyrdv1.ResourceReady, CapPercent: ptr.To(percent),
				Conditions: []metav1.Condition{{Type: controller.ConditionCap, Status: metav1.ConditionTrue, Reason: reason}}},
		}
	}
	main := tenant("app-acme-shop", "main", "aaaa0001", "pg-shared-1", 85, controller.CapNear)
	main.Spec.Backups = &shpyrdv1.PostgresBackups{Retention: "14d"}
	paused := tenant("app-acme-api", "db", "aaaa0002", "pg-shared-1", 160, controller.CapFar)
	crm := tenant("app-northwind-crm", "db", "aaaa0003", "pg-shared-1", 40, controller.CapWithin)
	crm.Annotations[controller.AnnotationConvert] = "dedicated"
	site := tenant("app-contoso-site", "db", "bbbb0001", "pg-shared-2", 100, controller.CapOver)
	twenty := resource.MustParse("20Gi")
	big := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "big", Namespace: "app-acme-shop", UID: "uid-big"},
		Spec:   shpyrdv1.PostgresSpec{Storage: &twenty, Backups: &shpyrdv1.PostgresBackups{Retention: "14d"}},
		Status: shpyrdv1.ResourceStatus{Phase: shpyrdv1.ResourceReady}}
	fresh := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "fresh", Namespace: "app-northwind-crm", UID: "uid-fresh"},
		Status: shpyrdv1.ResourceStatus{Phase: shpyrdv1.ResourceProvisioning}}

	namespace := func(name, workspace, project string) *corev1.Namespace {
		return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{shpyrdv1.LabelWorkspace: workspace, shpyrdv1.LabelProject: project}}}
	}
	at := func(s string) *metav1.Time { t := metav1.NewTime(mustTime(s)); return &t }
	job := func(name, started, finished string, failed bool) *batchv1.Job {
		j := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: controller.SharedPostgresNamespace, CreationTimestamp: *at(started)},
			Status: batchv1.JobStatus{StartTime: at(started)}}
		if finished != "" {
			j.Status.CompletionTime = at(finished)
			j.Status.Succeeded = 1
		}
		if failed {
			j.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded"}}
		}
		return j
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pg-shared-1-dumps-200-x7k2p", Namespace: controller.SharedPostgresNamespace,
			Labels: map[string]string{sharedServerLabel: "dumps", batchv1.JobNameLabel: "pg-shared-1-dumps-200"}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "dump",
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, Message: "dumped=3 failed=0 removed=2 took=51s"}}}}},
	}
	record := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: install.InstallRecordName, Namespace: install.DefaultSystemNamespace},
		Data: map[string]string{"profile": "oci", "vars": install.VarSharedPostgresPerServer + ": 150\n"}}
	pooler := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "pg-shared-1-pooler", Namespace: controller.SharedPostgresNamespace}, Status: appsv1.DeploymentStatus{ReadyReplicas: 2}}

	prom := fakeClaimsProm(t, map[string]string{"pg-shared-1-1": "21474836480", "pg-shared-1-2": "19327352832", "pg-shared-2-1": "32212254720", "pg-shared-10-1": "106300440576"})
	s, _ := newTestServer(t, prom, []client.Object{
		one, two,
		database("aaaa0001", "pg-shared-1", "app-acme-shop", "main", nil),
		database("aaaa0002", "pg-shared-1", "app-acme-api", "db", nil),
		database("aaaa0003", "pg-shared-1", "app-northwind-crm", "db", nil),
		database("aaaa0004", "pg-shared-1", "app-gone-shop", "db", map[string]string{controller.AnnotationDropAfter: "2026-10-20T00:00:00Z"}),
		database("bbbb0001", "pg-shared-2", "app-contoso-site", "db", nil),
		main, paused, crm, site, big, fresh,
	},
		namespace("app-acme-shop", "acme", "shop"), namespace("app-acme-api", "acme", "api"), namespace("app-northwind-crm", "northwind", "crm"), namespace("app-contoso-site", "contoso", "site"),
		record, pooler, pod,
		job("pg-shared-1-dumps-100", "2026-10-14T03:07:00Z", "2026-10-14T03:07:48Z", false),
		job("pg-shared-1-dumps-200", "2026-10-15T03:07:00Z", "2026-10-15T03:07:51Z", false),
		job("pg-shared-2-dumps-300", "2026-10-15T03:14:00Z", "", true),
		// A dump of one database asked for now is the CronJob's too, but not a nightly run.
		job("dump-aaaa0001-20261015120000", "2026-10-15T12:00:00Z", "2026-10-15T12:00:09Z", false),
	)
	return s
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func getJSON[T any](t *testing.T, s *Server, path string) (T, string) {
	t.Helper()
	rec := do(t, s, "GET", path, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", path, rec.Code, rec.Body.String())
	}
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("GET %s: %v: %s", path, err, rec.Body.String())
	}
	return v, rec.Body.String()
}

// The fleet is rendered with each server's numbers: version, instances,
// databases against the installer's limit, disk from Prometheus,
// connections from the metering loop's stamp, the pooler, archiving, the
// newest nightly run (its pod's own numbers when the pod is still there),
// what is kept and converting; the dedicated clusters beside; and what
// needs attention across all of it.
func TestPostgresServersRenderTheFleet(t *testing.T) {
	s := sharedFleet(t)
	view, body := getJSON[PostgresServersView](t, s, "/api/cluster/postgres-servers")
	if len(view.Servers) != 2 || view.Servers[0].Name != "pg-shared-1" || view.Servers[1].Name != "pg-shared-2" {
		t.Fatalf("servers = %+v", view.Servers)
	}
	one := view.Servers[0]
	if one.Version != "17" || one.Ready != 2 || one.Instances != 2 || one.Phase != "Cluster in healthy state" || one.CreatedAt != "2026-09-01T12:00:00Z" {
		t.Errorf("pg-shared-1 = %+v", one)
	}
	if one.Tenants != 4 || one.Limit != 150 || !one.Open || one.Kept != 1 || one.Converting != 1 {
		t.Errorf("pg-shared-1 fill: tenants %d of %d, open %v, kept %d, converting %d", one.Tenants, one.Limit, one.Open, one.Kept, one.Converting)
	}
	if one.Disk.UsedBytes != 20*gi || one.Disk.SizeBytes != 50*gi {
		t.Errorf("pg-shared-1 disk = %+v (the fullest of its own claims, not pg-shared-10's)", one.Disk)
	}
	if one.Connections.P95 != 260 || one.Connections.Max != 400 || !one.Pooler.On || one.Pooler.Since != "2026-10-10T10:00:00Z" || one.Pooler.Ready == nil || *one.Pooler.Ready != 2 {
		t.Errorf("pg-shared-1 connections = %+v pooler = %+v", one.Connections, one.Pooler)
	}
	if one.Archiving != "ok" || one.LastBaseBackup != "2026-10-15T02:00:00Z" {
		t.Errorf("pg-shared-1 archiving = %s, base backup %s", one.Archiving, one.LastBaseBackup)
	}
	if one.LastDumps == nil || *one.LastDumps != (PostgresServerDumps{At: "2026-10-15T03:07:51Z", Dumped: 3, Failed: 0, Took: "51s"}) {
		t.Errorf("pg-shared-1 dumps = %+v (the newest run, with the numbers its pod wrote)", one.LastDumps)
	}
	two := view.Servers[1]
	if two.Phase != "Failing over" || two.Ready != 1 || two.Open || two.Pooler.On || two.Pooler.Ready != nil || two.Archiving != "starting" || two.LastBaseBackup != "" {
		t.Errorf("pg-shared-2 = %+v", two)
	}
	if two.Tenants != 1 || two.Disk.UsedBytes != 30*gi || two.Disk.SizeBytes != 100*gi || two.Connections.P95 != 40 {
		t.Errorf("pg-shared-2 numbers = tenants %d disk %+v connections %+v", two.Tenants, two.Disk, two.Connections)
	}
	if two.LastDumps == nil || *two.LastDumps != (PostgresServerDumps{At: "2026-10-15T03:14:00Z", Dumped: 1, Failed: 1, Took: ""}) {
		t.Errorf("pg-shared-2 dumps = %+v (a run that failed, from the Job alone)", two.LastDumps)
	}
	if view.Attention != (PostgresAttentionView{Paused: 1, Over: 1, Near: 1, Degraded: 1, DumpsFailed: 1}) {
		t.Errorf("attention = %+v", view.Attention)
	}
	if view.Dedicated != (PostgresDedicatedView{Clusters: 2, Ready: 1, DiskBytes: 25 * gi, BackedUp: 1}) {
		t.Errorf("dedicated = %+v (a cluster without a size counts 5Gi)", view.Dedicated)
	}
	if view.MeasuredAt == "" {
		t.Error("measuredAt is when the answer was made")
	}
	// The names the console reads, as it reads them.
	for _, want := range []string{`"lastDumps":{"at":"2026-10-15T03:07:51Z","dumped":3,"failed":0,"took":"51s"}`, `"pooler":{"on":true,"since":"2026-10-10T10:00:00Z","ready":2}`, `"pooler":{"on":false}`, `"disk":{"usedBytes":`, `"connections":{"p95":260,"max":400}`, `"attention":{"paused":1,"over":1,"near":1,"degraded":1,"dumpsFailed":1}`, `"dedicated":{"clusters":2,"ready":1,"diskBytes":26843545600,"backedUp":1}`, `"lastBaseBackup":"2026-10-15T02:00:00Z"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the answer lacks %s:\n%s", want, body)
		}
	}
}

// A cluster without the operator, or without a server yet, answers an
// empty fleet rather than an error.
func TestPostgresServersWithoutAServer(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	view, body := getJSON[PostgresServersView](t, s, "/api/cluster/postgres-servers")
	if len(view.Servers) != 0 || view.Attention != (PostgresAttentionView{}) || !strings.Contains(body, `"servers":[]`) {
		t.Errorf("empty fleet = %s", body)
	}
}

// A server's databases come a page at a time in the order of workspace,
// project and name, each with whose it is, its hashed name and how full
// it is; the page narrows by state and by a word in any of those names.
func TestPostgresServerDatabasesPageNarrows(t *testing.T) {
	s := sharedFleet(t)
	const path = "/api/cluster/postgres-servers/pg-shared-1/databases"
	page, body := getJSON[PostgresTenantPage](t, s, path)
	if page.Total != 3 || page.Page != 1 || page.Size != 50 || len(page.Items) != 3 {
		t.Fatalf("page = %+v", page)
	}
	owners := func(items []PostgresTenantView) string {
		var out []string
		for _, i := range items {
			out = append(out, i.Workspace+"/"+i.Project+"/"+i.Name)
		}
		return strings.Join(out, " ")
	}
	if got := owners(page.Items); got != "acme/api/db acme/shop/main northwind/crm/db" {
		t.Errorf("order = %s (the kept database, whose Postgres is gone, is not listed)", got)
	}
	want := PostgresTenantView{Workspace: "acme", Project: "api", Name: "db", Database: "db_aaaa0002", Server: "pg-shared-1", Phase: "Ready", CapBytes: gi, UsedBytes: gi * 160 / 100, State: "paused", Keep: 7}
	if page.Items[0] != want {
		t.Errorf("paused tenant = %+v\n            want %+v", page.Items[0], want)
	}
	if main := page.Items[1]; main.State != "near" || main.Keep != 14 || main.UsedBytes != gi*85/100 {
		t.Errorf("near tenant = %+v", main)
	}
	if !strings.Contains(body, `"capBytes":1073741824,"usedBytes":1717986918,"state":"paused","keep":7`) {
		t.Errorf("the names the console reads: %s", body)
	}
	for _, c := range []struct{ query, want string }{
		{"?state=near", "acme/shop/main"},
		{"?state=paused", "acme/api/db"},
		{"?state=over", ""},
		{"?q=NORTH", "northwind/crm/db"},
		{"?q=aaaa0001", "acme/shop/main"},
		{"?q=acme", "acme/api/db acme/shop/main"},
		{"?q=acme&state=near", "acme/shop/main"},
		{"?q=shop%20", "acme/shop/main"},
	} {
		page, _ := getJSON[PostgresTenantPage](t, s, path+c.query)
		if got := owners(page.Items); got != c.want || page.Total != len(page.Items) {
			t.Errorf("%s = %q total %d, want %q", c.query, got, page.Total, c.want)
		}
	}
	second, _ := getJSON[PostgresTenantPage](t, s, path+"?page=2&size=2")
	if second.Total != 3 || second.Page != 2 || second.Size != 2 || owners(second.Items) != "northwind/crm/db" {
		t.Errorf("page 2 of 2 = %+v", second)
	}
	if far, _ := getJSON[PostgresTenantPage](t, s, path+"?page=9"); far.Total != 3 || len(far.Items) != 0 {
		t.Errorf("a page past the end = %+v", far)
	}
	if empty, body := getJSON[PostgresTenantPage](t, s, path+"?state=over"); len(empty.Items) != 0 || !strings.Contains(body, `"items":[]`) {
		t.Errorf("no match is an empty list: %s", body)
	}
	if rec := do(t, s, "GET", "/api/cluster/postgres-servers/pg-shared-9/databases", "", true); rec.Code != http.StatusNotFound {
		t.Errorf("a server that is not there = %d %s", rec.Code, rec.Body.String())
	}
}

// A database is found across every server by its workspace, project,
// name or hashed name, the first fifty in the same order; fewer than two
// characters find nothing.
func TestFindPostgresDatabasesAcrossServers(t *testing.T) {
	s := sharedFleet(t)
	const path = "/api/cluster/postgres-databases?q="
	found, _ := getJSON[[]PostgresTenantView](t, s, path+"db")
	var got []string
	for _, f := range found {
		got = append(got, f.Workspace+"/"+f.Project+"/"+f.Name+"@"+f.Server)
	}
	if strings.Join(got, " ") != "acme/api/db@pg-shared-1 acme/shop/main@pg-shared-1 contoso/site/db@pg-shared-2 northwind/crm/db@pg-shared-1" {
		t.Errorf("found by name, across servers in order = %v (every hashed name begins with db_)", got)
	}
	if found, _ := getJSON[[]PostgresTenantView](t, s, path+"BBBB"); len(found) != 1 || found[0].Database != "db_bbbb0001" || found[0].Server != "pg-shared-2" || found[0].State != "over" {
		t.Errorf("found by hashed name = %+v", found)
	}
	if found, _ := getJSON[[]PostgresTenantView](t, s, path+"shop"); len(found) != 1 || found[0].Name != "main" {
		t.Errorf("found by project = %+v (the dedicated big is not a shared database)", found)
	}
	if found, body := getJSON[[]PostgresTenantView](t, s, path+"a"); len(found) != 0 || body != "[]" {
		t.Errorf("one character finds nothing: %s", body)
	}
	if found, _ := getJSON[[]PostgresTenantView](t, s, path+"nobody"); len(found) != 0 {
		t.Errorf("a word nobody has = %+v", found)
	}
}
