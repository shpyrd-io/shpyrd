package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/ext/all"
	"github.com/shpyrd-io/shpyrd/pkg/pgdump"
)

type fakeDumpLister struct{ keys []string }

func (l *fakeDumpLister) List(_ context.Context, prefix string) ([]string, error) {
	var out []string
	for _, k := range l.keys {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out, nil
}

// A shared database's backups are the platform's dumps of it: listed from
// the server's bucket, one taken now as a Job cut from the server's
// CronJob and shown by its name until its copy lands; nothing to turn on
// or off; restores not yet.
func TestSharedPostgresBackupsAreTheDumps(t *testing.T) {
	s, cr, _ := newTenantServer(t)
	s.opts.Extensions = all.All()
	ctx := context.Background()
	db := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-acme-shop", UID: types.UID("uid-shop-db"), Annotations: map[string]string{controller.AnnotationSharedServer: "pg-shared-1"}}, Spec: shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeShared}}
	if err := cr.Create(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := cr.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: "db"}, db); err != nil {
		t.Fatal(err)
	}
	dbName := controller.SharedDatabaseName(db)
	lister := &fakeDumpLister{keys: []string{dbName + "/20261013T030700Z.dump", dbName + "/20261014T030700Z.dump", "db_other/20261014T030700Z.dump", dbName + "/notes.txt"}}
	previous := sharedDumpLister
	sharedDumpLister = func(*corev1.Secret) (pgdump.Lister, error) { return lister, nil }
	t.Cleanup(func() { sharedDumpLister = previous })
	secrets := s.kube.Kube.CoreV1().Secrets(controller.SharedPostgresNamespace)
	if _, err := secrets.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg-shared-1-dumps"}, Data: map[string][]byte{"BUCKET": []byte("b")}}, metav1.CreateOptions{}); err != nil {
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

	v := view(call("GET", base+"/backups", "", http.StatusOK))
	if v.Dumps == nil || v.Dumps.Keep != 7 || v.Policy != nil || len(v.Backups) != 2 || v.Backups[0].Name != "20261014T030700Z" || v.Backups[0].Kind != "nightly" || v.Backups[0].Phase != "completed" || v.Backups[0].Started != "2026-10-14T03:07:00Z" || v.LastBackup == nil {
		t.Errorf("listing = %+v", v)
	}

	// Before the server's CronJob exists, a backup now is refused in words.
	if body := call("POST", base+"/backups", "", http.StatusConflict); !strings.Contains(body, "not set up for this database's server yet") {
		t.Errorf("without the cronjob: %s", body)
	}
	cronjobs := s.kube.Kube.BatchV1().CronJobs(controller.SharedPostgresNamespace)
	cj := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "pg-shared-1-dumps", UID: "cj-uid"}, Spec: batchv1.CronJobSpec{Schedule: "7 3 * * *", JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "dump", Env: []corev1.EnvVar{{Name: "PGHOST", Value: "pg-shared-1-rw"}}}}}}}}}}
	if _, err := cronjobs.Create(ctx, cj, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	var started BackupView
	if err := json.Unmarshal([]byte(call("POST", base+"/backups", "", http.StatusCreated)), &started); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(started.Name, ".now") || started.Kind != "on demand" || started.Phase != "pending" {
		t.Errorf("started = %+v", started)
	}
	jobs, err := s.kube.Kube.BatchV1().Jobs(controller.SharedPostgresNamespace).List(ctx, metav1.ListOptions{LabelSelector: controller.SharedDumpJobsSelector(db)})
	if err != nil || len(jobs.Items) != 1 {
		t.Fatalf("jobs = %v %d", err, len(jobs.Items))
	}
	job := jobs.Items[0]
	env := map[string]string{}
	for _, e := range job.Spec.Template.Spec.Containers[0].Env {
		env[e.Name] = e.Value
	}
	if env["SHPYRD_DUMPS_DATABASE"] != dbName || env["SHPYRD_DUMPS_KIND"] != "now" || env["PGHOST"] != "pg-shared-1-rw" || job.OwnerReferences[0].Name != "pg-shared-1-dumps" {
		t.Errorf("job env %v owners %v", env, job.OwnerReferences)
	}
	// Until it lands, the listing shows it running; a second one waits.
	v = view(call("GET", base+"/backups", "", http.StatusOK))
	if len(v.Backups) != 3 || v.Backups[0].Name != started.Name || v.Backups[0].Phase != "running" || v.Backups[0].Kind != "on demand" {
		t.Errorf("with a job running = %+v", v.Backups)
	}
	if body := call("POST", base+"/backups", "", http.StatusConflict); !strings.Contains(body, "running already") {
		t.Errorf("second backup: %s", body)
	}
	// Failed: said with the Job's reason.
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded", Message: "Job has reached the specified backoff limit"}}
	if _, err := s.kube.Kube.BatchV1().Jobs(controller.SharedPostgresNamespace).Update(ctx, &job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	v = view(call("GET", base+"/backups", "", http.StatusOK))
	if v.Backups[0].Phase != "failed" || !strings.Contains(v.Backups[0].Error, "backoff limit") {
		t.Errorf("failed job = %+v", v.Backups[0])
	}
	// Landed: the copy is listed once, completed.
	job.Status.Conditions = nil
	job.Status.Succeeded = 1
	if _, err := s.kube.Kube.BatchV1().Jobs(controller.SharedPostgresNamespace).Update(ctx, &job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	lister.keys = append(lister.keys, dbName+"/"+started.Name+".dump")
	v = view(call("GET", base+"/backups", "", http.StatusOK))
	if len(v.Backups) != 3 || v.Backups[0].Name != started.Name || v.Backups[0].Phase != "completed" || v.Backups[0].Kind != "on demand" {
		t.Errorf("landed = %+v", v.Backups)
	}

	for _, c := range []struct{ method, path, body, want string }{
		{"PUT", base + "/backups", `{"retention":"30d"}`, "nothing to turn on or off"},
		{"DELETE", base + "/backups", "", "nothing to turn on or off"},
		{"POST", base + "/restore", `{"as":"copy"}`, "not available yet"},
	} {
		if body := call(c.method, c.path, c.body, http.StatusConflict); !strings.Contains(body, c.want) {
			t.Errorf("%s %s = %s", c.method, c.path, body)
		}
	}
}
