package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
	"github.com/shpyrd-io/shpyrd/pkg/pgdump"
)

// A shared database's backups (storage plan, step 3) are the platform's
// nightly dumps of it: pg_dump copies in its server's bucket, the newest
// seven kept. The listing reads the bucket; a backup asked for now is a
// Job cut from the server's CronJob for that one database, which the
// listing shows as running or failed until its copy is in the bucket.

const (
	errSharedDumpsNotASetting = "A shared database is backed up by the platform's nightly dumps; there is nothing to turn on or off."
	errSharedDumpsNotReady    = "The platform's nightly dumps are not set up for this database's server yet; try again in a few minutes."
	errSharedPointInTime      = "Point-in-time restore is a dedicated database feature. A shared database restores one of its copies: shpyrd pg restore <name> --as <new> [--backup <copy>]."
	sharedDumpsKeep           = 7
)

// DumpsView says how a shared database is backed up.
type DumpsView struct {
	// Keep is how many copies of the database stay.
	Keep int `json:"keep"`
}

// sharedDumpLister opens the bucket named by a dumps Secret; a test puts a
// fake here.
var sharedDumpLister = func(sec *corev1.Secret) (pgdump.Lister, error) {
	data := func(key string) string { return string(sec.Data[key]) }
	s3, err := objectstore.NewS3(data("AWS_ENDPOINT_URL"), data("AWS_REGION"), data("BUCKET"), "", data("AWS_ACCESS_KEY_ID"), data("AWS_SECRET_ACCESS_KEY"))
	if err != nil {
		return nil, err
	}
	return pgdump.Bucket{Client: s3.Client, Name: s3.Bucket}, nil
}

// sharedBackupsOf lists a shared database's dumps, newest first: the
// copies in the bucket and the Jobs asked for that have no copy yet.
func (s *Server) sharedBackupsOf(ctx context.Context, pg *shpyrdv1.Postgres) ([]BackupView, *metav1.Time, error) {
	out := []BackupView{}
	server := pg.Annotations[controller.AnnotationSharedServer]
	if server == "" {
		return out, nil, nil
	}
	at := map[string]time.Time{}
	var last *metav1.Time
	sec, err := s.kube.Kube.CoreV1().Secrets(controller.SharedPostgresNamespace).Get(ctx, controller.SharedDumpsName(server), metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		// The bucket is not ready yet: nothing in it.
	case err != nil:
		return nil, nil, err
	default:
		lister, err := sharedDumpLister(sec)
		if err != nil {
			return nil, nil, fmt.Errorf("the dumps bucket: %w", err)
		}
		db := controller.SharedDatabaseName(pg)
		keys, err := lister.List(ctx, db+"/")
		if err != nil {
			return nil, nil, fmt.Errorf("list the dumps: %w", err)
		}
		for _, key := range keys {
			_, name, taken, kind, ok := pgdump.Parse(key)
			if !ok {
				continue
			}
			out = append(out, BackupView{Name: name, Started: taken.UTC().Format(time.RFC3339), Phase: "completed", Kind: dumpKind(kind)})
			at[name] = taken
			if last == nil || taken.After(last.Time) {
				last = &metav1.Time{Time: taken}
			}
		}
	}
	jobs, err := s.kube.Kube.BatchV1().Jobs(controller.SharedPostgresNamespace).List(ctx, metav1.ListOptions{LabelSelector: controller.SharedDumpJobsSelector(pg)})
	if err != nil {
		return nil, nil, err
	}
	for _, job := range jobs.Items {
		asked, ok := controller.SharedDumpAt(&job)
		if !ok || job.Status.Succeeded > 0 {
			continue // its copy is in the bucket
		}
		name := pgdump.Name(asked, pgdump.KindNow)
		if _, listed := at[name]; listed {
			continue
		}
		b := BackupView{Name: name, Started: asked.UTC().Format(time.RFC3339), Phase: "running", Kind: dumpKind(pgdump.KindNow)}
		for _, cond := range job.Status.Conditions {
			if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
				b.Phase, b.Error = "failed", firstNonEmpty(cond.Message, cond.Reason)
			}
		}
		out = append(out, b)
		at[name] = asked
	}
	sort.SliceStable(out, func(i, j int) bool { return at[out[i].Name].After(at[out[j].Name]) })
	return out, last, nil
}

func dumpKind(kind string) string {
	switch kind {
	case pgdump.KindNow:
		return "on demand"
	case pgdump.KindLast:
		return "at deletion"
	}
	return "nightly"
}

func (s *Server) sharedBackupsView(c *gin.Context, pg *shpyrdv1.Postgres) {
	backups, last, err := s.sharedBackupsOf(c.Request.Context(), pg)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, BackupsView{Name: pg.Name, Dumps: &DumpsView{Keep: sharedDumpsKeep}, LastBackup: last, Backups: backups})
}

// takeSharedDump starts a dump of a shared database now and answers with
// the copy's name; GET follows it.
func (s *Server) takeSharedDump(c *gin.Context, pg *shpyrdv1.Postgres) {
	ctx := c.Request.Context()
	server := pg.Annotations[controller.AnnotationSharedServer]
	if server == "" {
		abort(c, http.StatusConflict, errors.New(errSharedDumpsNotReady))
		return
	}
	cj, err := s.kube.Kube.BatchV1().CronJobs(controller.SharedPostgresNamespace).Get(ctx, controller.SharedDumpsName(server), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		abort(c, http.StatusConflict, errors.New(errSharedDumpsNotReady))
		return
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	jobs, err := s.kube.Kube.BatchV1().Jobs(controller.SharedPostgresNamespace).List(ctx, metav1.ListOptions{LabelSelector: controller.SharedDumpJobsSelector(pg)})
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	for _, job := range jobs.Items {
		if job.Status.Succeeded == 0 && !jobFailed(job) {
			abort(c, http.StatusConflict, fmt.Errorf("a backup of %q is running already", pg.Name))
			return
		}
	}
	at := time.Now().UTC().Truncate(time.Second)
	job := controller.SharedDumpJob(pg, cj, at, pgdump.KindNow)
	if _, err := s.kube.Kube.BatchV1().Jobs(controller.SharedPostgresNamespace).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			abort(c, http.StatusConflict, errors.New("a backup of this database started this second already"))
			return
		}
		abort(c, http.StatusBadGateway, err)
		return
	}
	name := pgdump.Name(at, pgdump.KindNow)
	s.audit(c, c.Param("slug"), "postgres.backup", pg.Name, name)
	c.JSON(http.StatusCreated, BackupView{Name: name, Started: at.Format(time.RFC3339), Phase: "pending", Kind: dumpKind(pgdump.KindNow)})
}

func jobFailed(job batchv1.Job) bool {
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// restoreSharedPostgres makes a new shared database from one copy of
// another's: the one named, else the newest. The controller loads it
// once the new database exists; the source keeps running.
func (s *Server) restoreSharedPostgres(c *gin.Context, source *shpyrdv1.Postgres, req RestoreRequest) {
	if req.To != "" {
		abort(c, http.StatusConflict, errors.New(errSharedPointInTime))
		return
	}
	if req.Size != "" {
		abort(c, http.StatusConflict, errors.New(errSharedNoInstanceSettings))
		return
	}
	t, ok := s.resourceType("Postgres")
	if !ok {
		abort(c, http.StatusBadRequest, errors.New("databases are not available on this cluster (enable the postgres extension)"))
		return
	}
	copies, _, err := s.sharedBackupsOf(c.Request.Context(), source)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	chosen := ""
	for _, b := range copies {
		if b.Phase != "completed" {
			continue
		}
		if req.Backup == "" || b.Name == req.Backup {
			chosen = b.Name
			break
		}
	}
	switch {
	case chosen == "" && req.Backup != "":
		abort(c, http.StatusConflict, fmt.Errorf("no copy %q of %q: shpyrd pg backups list %s shows them", req.Backup, source.Name, source.Name))
		return
	case chosen == "":
		abort(c, http.StatusConflict, fmt.Errorf("the database %q has no copy from the platform's nightly dumps yet; shpyrd pg backup %s takes one now", source.Name, source.Name))
		return
	}
	spec := map[string]interface{}{
		"type":     shpyrdv1.PostgresTypeShared,
		"recovery": map[string]interface{}{"from": source.Name, "backup": chosen},
	}
	storage := req.Storage
	if storage == "" && source.Spec.Storage != nil {
		storage = source.Spec.Storage.String()
	}
	if storage != "" {
		spec["storage"] = storage
	}
	s.createResourceOf(c, t, req.As, spec, "postgres.restore")
}
