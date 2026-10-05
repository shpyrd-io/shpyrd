package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ext/all"
)

// Backups and restore through the API (#74): the workspace's own
// credentials do it all, with no kubeconfig. Backups go on and off, one
// is taken on demand and listed; a restore makes a new database from the
// source's backups at a moment inside their window, and is refused
// outside it, onto itself, or with a size not for databases; the same
// database cannot be reached from another workspace.
func TestPostgresBackupsThroughTheAPI(t *testing.T) {
	s, cr, _ := newTenantServer(t)
	s.opts.Extensions = all.All()
	ctx := context.Background()
	db := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-acme-shop"}, Spec: shpyrdv1.PostgresSpec{Version: "17", Size: "shared-m"}}
	if err := cr.Create(ctx, db); err != nil {
		t.Fatal(err)
	}
	const base = "/api/projects/shop/resources/postgres/db"
	call := func(method, path, body string, want int) string {
		t.Helper()
		rec := at(t, s, "acme.shpyrd.test", method, path, body)
		if rec.Code != want {
			t.Fatalf("%s %s %s = %d %s", method, path, body, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	view := func(body string) BackupsView {
		var v BackupsView
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			t.Fatalf("%v: %s", err, body)
		}
		return v
	}

	if v := view(call("GET", base+"/backups", "", http.StatusOK)); v.Policy != nil || v.Backups == nil || len(v.Backups) != 0 {
		t.Errorf("off = %+v", v)
	}
	if body := call("POST", base+"/backups", "", http.StatusConflict); !strings.Contains(body, "are off") {
		t.Errorf("a backup while off = %s", body)
	}
	if body := call("POST", base+"/restore", `{"as":"db2"}`, http.StatusBadRequest); !strings.Contains(body, "no backups to restore from") {
		t.Errorf("restore without backups = %s", body)
	}
	if v := view(call("PUT", base+"/backups", `{"retention":"7d"}`, http.StatusOK)); v.Policy == nil || v.Policy.Retention != "7d" {
		t.Errorf("on = %+v", v.Policy)
	}
	if v := view(call("PUT", base+"/backups", `{"schedule":"30 3 * * *"}`, http.StatusOK)); v.Policy.Retention != "7d" || v.Policy.Schedule != "30 3 * * *" {
		t.Errorf("schedule changed, retention kept = %+v", v.Policy)
	}
	for _, bad := range []string{`{"retention":"7"}`, `{"schedule":"daily"}`} {
		call("PUT", base+"/backups", bad, http.StatusBadRequest)
	}

	var taken BackupView
	_ = json.Unmarshal([]byte(call("POST", base+"/backups", "", http.StatusCreated)), &taken)
	if v := view(call("GET", base+"/backups", "", http.StatusOK)); len(v.Backups) != 1 || v.Backups[0].Name != taken.Name || v.Backups[0].Phase != "pending" || v.Backups[0].Kind != "on demand" {
		t.Errorf("listed = %+v, taken %+v", v.Backups, taken)
	}
	if body := call("POST", base+"/restore", `{"as":"db2"}`, http.StatusBadRequest); !strings.Contains(body, "no completed backup yet") {
		t.Errorf("restore before a backup completed = %s", body)
	}

	// The controller found a completed backup: the window opens 2h ago.
	from := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	_ = cr.Get(ctx, client.ObjectKeyFromObject(db), db)
	db.Status.LastBackup = &metav1.Time{Time: from.Add(time.Hour)}
	db.Status.RecoverableFrom = &metav1.Time{Time: from}
	if err := cr.Update(ctx, db); err != nil {
		t.Fatal(err)
	}
	for body, want := range map[string]string{
		`{"as":"db2","to":"` + from.Add(-time.Minute).Format(time.RFC3339) + `"}`:          "before the earliest point",
		`{"as":"db2","to":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`: "is in the future",
		`{"as":"db2","to":"yesterday"}`: "use RFC 3339",
		`{"as":"db"}`:                   "onto itself",
		`{"as":"db2","size":"db-xs"}`:   `unknown size \"db-xs\" for a Postgres database`,
	} {
		if got := call("POST", base+"/restore", body, http.StatusBadRequest); !strings.Contains(got, want) {
			t.Errorf("restore %s = %s, want %q", body, got, want)
		}
	}
	to := from.Add(30 * time.Minute)
	var restored ResourceView
	_ = json.Unmarshal([]byte(call("POST", base+"/restore", `{"as":"db2","to":"`+to.Format(time.RFC3339)+`"}`, http.StatusCreated)), &restored)
	got := &shpyrdv1.Postgres{}
	if err := cr.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: "db2"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Recovery == nil || got.Spec.Recovery.From != "db" || !got.Spec.Recovery.TargetTime.Equal(&metav1.Time{Time: to}) || got.Spec.Size != "shared-m" || got.Spec.Version != "17" || got.Labels[shpyrdv1.LabelProject] != "shop" || restored.Details["restoredFrom"] != "db" {
		t.Errorf("restored = %+v %v, view %+v", got.Spec, got.Labels, restored.Details)
	}
	// The generic route checks the same: a recovery from a database
	// without backups is refused there too.
	if body := call("POST", "/api/projects/shop/resources", `{"kind":"Postgres","name":"db3","spec":{"recovery":{"from":"db2"}}}`, http.StatusBadRequest); !strings.Contains(body, "no backups to restore from") {
		t.Errorf("generic restore = %s", body)
	}

	if v := view(call("DELETE", base+"/backups", "", http.StatusOK)); v.Policy != nil {
		t.Errorf("off again = %+v", v.Policy)
	}
	// Another workspace's project of the same name has no such database,
	// and cannot name it.
	for _, path := range []string{base + "/backups", base + "/restore"} {
		method := "GET"
		if strings.HasSuffix(path, "restore") {
			method = "POST"
		}
		if rec := at(t, s, "example.test", method, path, `{"as":"stolen"}`); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s from another workspace = %d %s", method, path, rec.Code, rec.Body.String())
		}
	}
}

// A viewer of the project sees its database's backups and changes nothing.
func TestPostgresBackupsNeedTheResourcePermission(t *testing.T) {
	shop := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}}
	db := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-shop"}, Spec: shpyrdv1.PostgresSpec{Size: "shared-s", Backups: &shpyrdv1.PostgresBackups{}}}
	s, _ := newTestServer(t, nil, []client.Object{shop, db})
	s.opts.Extensions = all.All()
	s.authz.TTL = 1
	if rec := do(t, s, "POST", "/api/teams", `{"name":"ops","members":["ops@example.test"],"platformRole":"platform-admin"}`, true); rec.Code != http.StatusCreated {
		t.Fatalf("create team: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "POST", "/api/projects/shop/members", `{"role":"viewer","user":"viewer@example.test"}`, true); rec.Code != http.StatusCreated {
		t.Fatalf("add viewer: %d %s", rec.Code, rec.Body.String())
	}
	sid, csrf := signIn(t, s, ext.Identity{Subject: "u2", Email: "viewer@example.test", Provider: "local"})
	const base = "/api/projects/shop/resources/postgres/db"
	if rec := doCookie(t, s, "GET", base+"/backups", "", sid, csrf); rec.Code != http.StatusOK {
		t.Errorf("viewer list = %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range []struct{ method, path, body string }{
		{"PUT", base + "/backups", `{"retention":"30d"}`},
		{"DELETE", base + "/backups", ""},
		{"POST", base + "/backups", ""},
		{"POST", base + "/restore", `{"as":"db2"}`},
		{"POST", base + "/resize", `{"size":"shared-m"}`},
	} {
		if rec := doCookie(t, s, c.method, c.path, c.body, sid, csrf); rec.Code != http.StatusForbidden {
			t.Errorf("viewer %s %s = %d %s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}
