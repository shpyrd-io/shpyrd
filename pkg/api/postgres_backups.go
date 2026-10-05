package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
)

// Backups of a database (RFC-0038) through the API (#74): `shpyrd pg
// backups enable|disable|list`, `shpyrd pg backup` and `shpyrd pg
// restore` speak these routes, so a person signed in with `shpyrd login`
// needs no kubeconfig. Everything is found in the request's project: a
// database of another project, or of another workspace, cannot be named.

// BackupView is one base backup of a database.
type BackupView struct {
	Name string `json:"name"`
	// Started is when it began (RFC 3339), "" while pending.
	Started string `json:"started,omitempty"`
	// Phase is CloudNativePG's: pending, running, completed, failed.
	Phase string `json:"phase"`
	// Kind is "scheduled" or "on demand".
	Kind  string `json:"kind"`
	Error string `json:"error,omitempty"`
}

// BackupsView is a database's backups: the policy (nil when off), the
// window that can be restored and the base backups, newest first.
type BackupsView struct {
	Name            string                    `json:"name"`
	Policy          *shpyrdv1.PostgresBackups `json:"policy"`
	LastBackup      *metav1.Time              `json:"lastBackup,omitempty"`
	RecoverableFrom *metav1.Time              `json:"recoverableFrom,omitempty"`
	Backups         []BackupView              `json:"backups"`
}

// postgresOf is a database of the request's project; errors are written.
func (s *Server) postgresOf(c *gin.Context) (*shpyrdv1.Postgres, bool) {
	ns, ok := s.projectNamespace(c)
	if !ok {
		return nil, false
	}
	pg := &shpyrdv1.Postgres{}
	if err := s.apps.Get(c.Request.Context(), types.NamespacedName{Namespace: ns, Name: c.Param("name")}, pg); err != nil {
		if apierrors.IsNotFound(err) {
			abort(c, http.StatusNotFound, fmt.Errorf("no database %q in this project", c.Param("name")))
		} else {
			abort(c, http.StatusBadGateway, err)
		}
		return nil, false
	}
	return pg, true
}

// backupsOf lists the base backups of a database, newest first; none when
// CloudNativePG is not installed.
func (s *Server) backupsOf(ctx context.Context, pg *shpyrdv1.Postgres) ([]BackupView, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(controller.CNPGBackupGVK.GroupVersion().WithKind("BackupList"))
	out := []BackupView{}
	if err := s.apps.List(ctx, list, client.InNamespace(pg.Namespace)); err != nil {
		if strings.Contains(err.Error(), "no matches for kind") {
			return out, nil
		}
		return nil, err
	}
	at := map[string]time.Time{}
	for _, item := range list.Items {
		if cl, _, _ := unstructured.NestedString(item.Object, "spec", "cluster", "name"); cl != pg.Name {
			continue
		}
		phase, _, _ := unstructured.NestedString(item.Object, "status", "phase")
		started, _, _ := unstructured.NestedString(item.Object, "status", "startedAt")
		msg, _, _ := unstructured.NestedString(item.Object, "status", "error")
		b := BackupView{Name: item.GetName(), Phase: firstNonEmpty(phase, "pending"), Kind: "on demand", Error: msg}
		for _, o := range item.GetOwnerReferences() {
			if o.Kind == "ScheduledBackup" {
				b.Kind = "scheduled"
			}
		}
		at[b.Name] = item.GetCreationTimestamp().Time
		if t, err := time.Parse(time.RFC3339, started); err == nil {
			b.Started = t.UTC().Format(time.RFC3339)
			at[b.Name] = t
		}
		out = append(out, b)
	}
	sort.SliceStable(out, func(i, j int) bool { return at[out[i].Name].After(at[out[j].Name]) })
	return out, nil
}

func (s *Server) backupsView(c *gin.Context, pg *shpyrdv1.Postgres) {
	backups, err := s.backupsOf(c.Request.Context(), pg)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, BackupsView{Name: pg.Name, Policy: pg.Spec.Backups, LastBackup: pg.Status.LastBackup, RecoverableFrom: pg.Status.RecoverableFrom, Backups: backups})
}

// listPostgresBackups is GET .../postgres/:name/backups.
func (s *Server) listPostgresBackups(c *gin.Context) {
	if pg, ok := s.postgresOf(c); ok {
		s.backupsView(c, pg)
	}
}

// putPostgresBackups turns backups on, or changes their schedule and
// retention: PUT .../postgres/:name/backups {"retention":"14d","schedule":"0 2 * * *"};
// what is left out keeps its value.
func (s *Server) putPostgresBackups(c *gin.Context) {
	var req shpyrdv1.PostgresBackups
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			abort(c, http.StatusBadRequest, errors.New(`body must be {"retention":"14d","schedule":"0 2 * * *"}, both optional`))
			return
		}
	}
	pg, err := s.mutatePostgres(c, func(pg *shpyrdv1.Postgres) error {
		next := shpyrdv1.PostgresBackups{}
		if pg.Spec.Backups != nil {
			next = *pg.Spec.Backups
		}
		if req.Retention != "" {
			next.Retention = req.Retention
		}
		if req.Schedule != "" {
			next.Schedule = req.Schedule
		}
		if err := controller.CheckBackups(&next); err != nil {
			return err
		}
		pg.Spec.Backups = &next
		return nil
	})
	if err != nil {
		return
	}
	s.audit(c, c.Param("slug"), "postgres.backups", pg.Name, fmt.Sprintf("on, kept %s, cron %s", firstNonEmpty(pg.Spec.Backups.Retention, "14d"), firstNonEmpty(pg.Spec.Backups.Schedule, "0 2 * * *")))
	s.backupsView(c, pg)
}

// deletePostgresBackups turns backups off: archiving and the schedule
// stop; the backups taken stay restorable until the database is deleted.
func (s *Server) deletePostgresBackups(c *gin.Context) {
	pg, err := s.mutatePostgres(c, func(pg *shpyrdv1.Postgres) error {
		pg.Spec.Backups = nil
		return nil
	})
	if err != nil {
		return
	}
	s.audit(c, c.Param("slug"), "postgres.backups", pg.Name, "off")
	s.backupsView(c, pg)
}

// takePostgresBackup starts a base backup now: POST .../postgres/:name/backups.
// It answers at once with the backup's name; GET follows it.
func (s *Server) takePostgresBackup(c *gin.Context) {
	pg, ok := s.postgresOf(c)
	if !ok {
		return
	}
	if pg.Spec.Backups == nil {
		abort(c, http.StatusConflict, fmt.Errorf("backups of %q are off: turn them on first (shpyrd pg backups enable %s)", pg.Name, pg.Name))
		return
	}
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(controller.CNPGBackupGVK)
	b.SetName(fmt.Sprintf("%s-%s", pg.Name, time.Now().UTC().Format("20060102-150405")))
	b.SetNamespace(pg.Namespace)
	b.SetLabels(map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", "shpyrd.io/postgres": pg.Name})
	b.Object["spec"] = map[string]interface{}{
		"cluster":             map[string]interface{}{"name": pg.Name},
		"method":              "plugin",
		"pluginConfiguration": map[string]interface{}{"name": controller.BarmanPluginName},
	}
	if err := s.apps.Create(c.Request.Context(), b); err != nil {
		switch {
		case apierrors.IsAlreadyExists(err):
			abort(c, http.StatusConflict, errors.New("a backup of this database started this second already"))
		case strings.Contains(err.Error(), "no matches for kind"):
			abort(c, http.StatusConflict, errors.New("databases are not offered on this platform yet: its operator turns them on"))
		default:
			abort(c, http.StatusBadGateway, err)
		}
		return
	}
	s.audit(c, c.Param("slug"), "postgres.backup", pg.Name, b.GetName())
	c.JSON(http.StatusCreated, BackupView{Name: b.GetName(), Phase: "pending", Kind: "on demand"})
}

// RestoreRequest is POST .../postgres/:name/restore: a new database As,
// from the backups of :name, at To (RFC 3339; the latest when empty). Size
// and Storage default to the source's.
type RestoreRequest struct {
	As      string `json:"as" binding:"required"`
	To      string `json:"to,omitempty"`
	Size    string `json:"size,omitempty"`
	Storage string `json:"storage,omitempty"`
}

// restorePostgres makes a new database from another's backups, at a point
// in time inside their window; the source keeps running.
func (s *Server) restorePostgres(c *gin.Context) {
	var req RestoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, errors.New(`body must be {"as":"<new name>","to":"<RFC 3339, optional>"}`))
		return
	}
	source, ok := s.postgresOf(c)
	if !ok {
		return
	}
	t, ok := s.resourceType("Postgres")
	if !ok {
		abort(c, http.StatusBadRequest, errors.New("databases are not available on this cluster (enable the postgres extension)"))
		return
	}
	rec := shpyrdv1.PostgresRecovery{From: source.Name}
	if req.To != "" {
		at, err := time.Parse(time.RFC3339, req.To)
		if err != nil {
			abort(c, http.StatusBadRequest, fmt.Errorf("to %q: use RFC 3339, e.g. 2026-09-25T10:00:00Z", req.To))
			return
		}
		rec.TargetTime = &metav1.Time{Time: at}
	}
	spec := shpyrdv1.PostgresSpec{Version: source.Spec.Version, Size: firstNonEmpty(req.Size, source.Spec.Size), Storage: source.Spec.Storage, Instances: source.Spec.Instances, Recovery: &rec}
	if req.Storage != "" {
		q, err := resource.ParseQuantity(req.Storage)
		if err != nil || q.Sign() <= 0 {
			abort(c, http.StatusBadRequest, fmt.Errorf("storage %q: use e.g. 10Gi", req.Storage))
			return
		}
		spec.Storage = &q
	}
	raw, err := json.Marshal(spec)
	var m map[string]interface{}
	if err == nil {
		err = json.Unmarshal(raw, &m)
	}
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	s.createResourceOf(c, t, req.As, m, "postgres.restore")
}

// checkRecovery refuses a restore the controller could not do: onto the
// source itself, from a database of the project with no backups or none
// completed yet, or to a moment outside the window its backups cover.
// A source that is gone may still have its backups: the controller looks.
func (s *Server) checkRecovery(ctx context.Context, ns, name, from string, to *time.Time) error {
	if from == "" {
		return errors.New("recovery.from names the database to restore")
	}
	if from == name {
		return errors.New("a database cannot be restored onto itself; restore to a new name")
	}
	if to != nil && to.After(time.Now()) {
		return fmt.Errorf("%s is in the future", to.UTC().Format(time.RFC3339))
	}
	source := &shpyrdv1.Postgres{}
	if err := s.apps.Get(ctx, types.NamespacedName{Namespace: ns, Name: from}, source); err != nil {
		if apierrors.IsNotFound(err) {
			return nil // its backup store may outlive it; the controller says
		}
		return err
	}
	switch {
	case source.Spec.Backups == nil:
		return fmt.Errorf("the database %q has no backups to restore from: turn its backups on first", from)
	case source.Status.LastBackup == nil:
		return fmt.Errorf("the database %q has no completed backup yet", from)
	case to != nil && source.Status.RecoverableFrom != nil && to.Before(source.Status.RecoverableFrom.Time):
		return fmt.Errorf("%s is before the earliest point the backups of %q reach (%s)", to.UTC().Format(time.RFC3339), from, source.Status.RecoverableFrom.UTC().Format(time.RFC3339))
	}
	return nil
}
