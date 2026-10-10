package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/projectarchive"
)

func (s *Server) restoreProjectArchive(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	if !s.acquireArchiveRequest(c, app.Namespace) {
		return
	}
	defer s.releaseArchiveRequest(app.Namespace)
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Hour)
	defer cancel()
	if _, err := s.readProjectArchive(ctx, app.Namespace); !apierrors.IsNotFound(err) {
		if err == nil {
			err = errors.New("project has an unfinished operation; recover it before uploading another archive")
		}
		abort(c, http.StatusConflict, err)
		return
	}
	// Raw tgz upload; multipart buffering would duplicate a large upload in
	// RAM or in another temporary file. Validate everything before pausing.
	b, err := projectarchive.Read(ctx, "", http.MaxBytesReader(c.Writer, c.Request.Body, projectarchive.DefaultLimit+(1<<30)), projectarchive.DefaultLimit)
	if err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	defer b.Close()
	m, err := validateProjectArchive(ctx, b)
	if err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	original, err := s.projectArchiveMetadata(ctx, app)
	if err != nil {
		abort(c, http.StatusConflict, err)
		return
	}
	if err := s.checkExposure(ctx, s.workspaceOfApp(ctx, app), app.Spec.Exposure, m.Spec.Exposure); err != nil {
		exposureError(c, err)
		return
	}
	if err := s.checkArchiveCompatibility(ctx, app, original, m); err != nil {
		abort(c, http.StatusConflict, err)
		return
	}
	op, err := s.beginProjectArchive(ctx, app, "restore", original)
	if err != nil {
		abort(c, http.StatusConflict, err)
		return
	}
	if err := s.runProjectRestore(ctx, app, op, b, m); err != nil {
		recovery, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
		defer stop()
		var recoveryErr error
		if op.canRollback() {
			recoveryErr = s.rollbackProjectArchive(recovery, app, op)
		}
		if recoveryErr != nil {
			op.State = projectarchive.TransactionState{Phase: "recovery-required", Error: recoveryErr.Error()}
			_ = s.saveProjectArchive(recovery, app.Namespace, op)
		}
		abort(c, http.StatusBadGateway, errors.Join(err, recoveryErr))
		return
	}
	s.audit(c, project.SlugOf(app), "project.restore", project.SlugOf(app), "restored a portable project archive")
	c.JSON(http.StatusOK, gin.H{"status": "restored", "warnings": op.Warnings})
}

func (s *Server) checkArchiveCompatibility(ctx context.Context, app *shpyrdv1.App, original, restored *projectArchiveMetadata) error {
	if rolloutInProgress(app) {
		return errors.New("wait for the current deployment to finish before restoring")
	}
	if err := validateDeployRequest(&DeployRequest{Build: restored.Spec.Build, Processes: restored.Spec.Processes, Env: restored.Spec.Env, Exposure: restored.Spec.Exposure}); err != nil {
		return err
	}
	for key := range restored.Config {
		if err := ValidateEnvName(key); err != nil {
			return err
		}
	}
	for key := range restored.Globals {
		if err := ValidateEnvName(key); err != nil {
			return err
		}
	}
	volumes := map[string]archivedVolume{}
	for _, v := range restored.Volumes {
		volumes[v.Name] = v
	}
	databases := map[string]archivedDatabase{}
	for _, pg := range restored.Databases {
		databases[pg.Name] = pg
	}
	for _, v := range original.Volumes {
		if _, ok := volumes[v.Name]; !ok {
			return fmt.Errorf("archive does not contain existing volume %s; restore into an empty project instead", v.Name)
		}
	}
	for _, pg := range original.Databases {
		want, ok := databases[pg.Name]
		if !ok {
			return fmt.Errorf("archive does not contain existing database %s; restore into an empty project instead", pg.Name)
		}
		if firstNonEmpty(want.Spec.Version, "17") != firstNonEmpty(pg.Spec.Version, "17") {
			return fmt.Errorf("database %s needs the same PostgreSQL major version", pg.Name)
		}
	}
	for name, p := range restored.Spec.Processes {
		for _, mount := range p.Volumes {
			if _, ok := volumes[mount.Name]; !ok {
				return fmt.Errorf("process %s refers to missing volume %s", name, mount.Name)
			}
		}
	}
	for _, binding := range restored.Spec.Bindings {
		if !strings.EqualFold(binding.Kind, "postgres") {
			return fmt.Errorf("unsupported archived binding kind %s", binding.Kind)
		}
		if _, ok := databases[binding.Name]; !ok {
			return fmt.Errorf("missing database binding %s", binding.Name)
		}
	}
	var extra resource.Quantity
	oldV := map[string]bool{}
	for _, v := range original.Volumes {
		oldV[v.Name] = true
	}
	for _, v := range restored.Volumes {
		if !oldV[v.Name] {
			extra.Add(v.Spec.Size)
		}
	}
	oldPG := map[string]bool{}
	for _, pg := range original.Databases {
		oldPG[pg.Name] = true
	}
	for _, pg := range restored.Databases {
		if !oldPG[pg.Name] {
			if pg.Spec.Storage != nil {
				extra.Add(*pg.Spec.Storage)
			} else {
				extra.Add(resource.MustParse("5Gi"))
			}
		}
	}
	ws := s.workspaceOfApp(ctx, app)
	if err := s.checkStorageLimit(ctx, ws, extra); err != nil {
		return err
	}
	after := app.DeepCopy()
	after.Spec = restored.Spec
	after.Spec.ID = app.Spec.ID
	after.Spec.Slug = app.Spec.Slug
	return s.checkLimits(ctx, ws, after)
}

func (s *Server) createArchiveResources(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation, m *projectArchiveMetadata) error {
	for _, v := range m.Volumes {
		current := &shpyrdv1.Volume{}
		err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: v.Name}, current)
		if err == nil {
			continue
		}
		if !apierrors.IsNotFound(err) {
			return err
		}
		op.CreatedVolumes = append(op.CreatedVolumes, v.Name)
		if err := s.saveProjectArchive(ctx, app.Namespace, op); err != nil {
			return err
		}
		current = &shpyrdv1.Volume{ObjectMeta: metav1.ObjectMeta{Name: v.Name, Namespace: app.Namespace, Labels: map[string]string{projectOperationLabel: op.ID}}, Spec: v.Spec}
		current.Spec.FromSnapshot = ""
		current.Spec.StorageClass = ""
		// On the profile's class, as a new disk: the provider minimum
		// applies (RFC-0060), or the Volume would ask for less than the
		// disk it gets and refuse every later resize up to that size.
		current.Spec.Size, _ = s.applyVolumeMinimum(current.Spec.Size)
		if err := s.apps.Create(ctx, current); err != nil {
			return err
		}
	}
	for _, pg := range m.Databases {
		current := &shpyrdv1.Postgres{}
		err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: pg.Name}, current)
		if err == nil {
			continue
		}
		if !apierrors.IsNotFound(err) {
			return err
		}
		op.CreatedDatabases = append(op.CreatedDatabases, pg.Name)
		if err := s.saveProjectArchive(ctx, app.Namespace, op); err != nil {
			return err
		}
		current = &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: pg.Name, Namespace: app.Namespace, Labels: map[string]string{projectOperationLabel: op.ID}, Annotations: map[string]string{shpyrdv1.AnnotationMaintenance: op.ID}}, Spec: pg.Spec}
		current.Spec.Recovery = nil
		if err := s.apps.Create(ctx, current); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) archiveParticipants(app *shpyrdv1.App, op *projectArchiveOperation, m *projectArchiveMetadata, b *projectarchive.Bundle) []projectarchive.Participant {
	var resources []projectarchive.Participant
	for _, v := range m.Volumes {
		invoke := func(ctx context.Context, action string, r io.Reader) error {
			volume := &shpyrdv1.Volume{ObjectMeta: metav1.ObjectMeta{Name: v.Name, Namespace: app.Namespace}, Spec: v.Spec}
			cmd, err := s.archiveVolumeHelper(ctx, app, volume, op.ID, op.warn)
			if err != nil {
				return err
			}
			return cmd(ctx, []string{"/shpyrd-server", "project-volume", action, "/data", op.ID}, r, io.Discard)
		}
		resources = append(resources, projectarchive.Participant{Name: "volume/" + v.Name,
			Stage: func(ctx context.Context) error {
				if b == nil {
					return errors.New("no uploaded archive available")
				}
				f, err := b.Open("volumes/" + v.Name + ".tar")
				if err != nil {
					return err
				}
				defer f.Close()
				return invoke(ctx, "stage", f)
			},
			Commit: func(ctx context.Context) error { return invoke(ctx, "commit", nil) }, Rollback: func(ctx context.Context) error { return invoke(ctx, "rollback", nil) }, Finish: func(ctx context.Context) error { return invoke(ctx, "finish", nil) }})
	}
	for _, pg := range m.Databases {
		withDB := func(ctx context.Context, fn func(projectarchive.PostgreSQL) error) error {
			db, err := s.archiveDatabase(ctx, app.Namespace, pg.Name)
			if err != nil {
				return err
			}
			return fn(db)
		}
		resources = append(resources, projectarchive.Participant{Name: "postgres/" + pg.Name,
			Stage: func(ctx context.Context) error {
				if b == nil {
					return errors.New("no uploaded archive available")
				}
				f, err := b.Open("databases/" + pg.Name + ".dump")
				if err != nil {
					return err
				}
				defer f.Close()
				return withDB(ctx, func(db projectarchive.PostgreSQL) error { return db.Stage(ctx, op.ID, f) })
			},
			Commit: func(ctx context.Context) error {
				return withDB(ctx, func(db projectarchive.PostgreSQL) error { return db.Commit(ctx, op.ID) })
			}, Rollback: func(ctx context.Context) error {
				return withDB(ctx, func(db projectarchive.PostgreSQL) error { return db.Rollback(ctx, op.ID) })
			}, Finish: func(ctx context.Context) error {
				return withDB(ctx, func(db projectarchive.PostgreSQL) error { return db.Finish(ctx, op.ID) })
			}})
	}
	return resources
}

func (s *Server) importArchiveAssets(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation, b *projectarchive.Bundle, m *projectArchiveMetadata) error {
	if len(m.Images) > 0 {
		ws, err := s.store.Workspace(ctx, s.workspaceOfApp(ctx, app))
		if err != nil {
			return err
		}
		repo := s.vars(install.VarRegistryHost) + "/apps/" + ids.Short(ws.ID) + "/" + app.Name
		destination, _, err := s.archiveRepository(ctx, repo+"@sha256:"+strings.Repeat("0", 64))
		if err != nil {
			return err
		}
		for original, d := range m.Images {
			if err := b.ImportImage(ctx, destination, d); err != nil {
				return err
			}
			if err := destination.Tag(ctx, d, "restore-"+op.ID+"-"+d.Digest.Encoded()[:12]); err != nil {
				return err
			}
			if m.Status.Image == original {
				m.Status.Image = repo + "@" + d.Digest.String()
			}
		}
	}
	if m.SourceEntry != "" {
		if s.sources == nil {
			return errors.New("source storage is not configured")
		}
		f, err := b.Open(m.SourceEntry)
		if err != nil {
			return err
		}
		source, err := s.sources.PutContext(ctx, f)
		f.Close()
		if err != nil {
			return err
		}
		subpath := ""
		if m.Spec.Source != nil {
			subpath = m.Spec.Source.SubPath
		}
		m.Spec.Source = &shpyrdv1.Source{SubPath: subpath, Blob: &shpyrdv1.BlobSource{URL: source.URL, SHA256: source.SHA256, Ref: "restored archive"}}
	}
	return nil
}

func (s *Server) applyArchiveConfiguration(ctx context.Context, app *shpyrdv1.App, m *projectArchiveMetadata, portable bool) error {
	current := &shpyrdv1.App{}
	if err := s.apps.Get(ctx, client.ObjectKeyFromObject(app), current); err != nil {
		return err
	}
	spec := *m.Spec.DeepCopy()
	// Recovery must restart the exact old deployment, even when its Git
	// branch moved while the operation was in progress.
	if m.Status.Image != "" {
		spec.Image = m.Status.Image
	}
	spec.ID = current.Spec.ID
	spec.Slug = current.Spec.Slug
	// Routing and access grants belong to the authenticated destination.
	spec.Domains = current.Spec.Domains
	spec.Allow = current.Spec.Allow
	spec.Access = current.Spec.Access
	config := map[string][]byte{}
	if portable {
		for k, v := range m.Globals {
			config[k] = v
		}
		spec.Globals = &shpyrdv1.Globals{Disabled: true}
		spec.Image = m.Status.Image
	}
	for k, v := range m.Config {
		config[k] = v
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: current.EnvSecretName(), Namespace: current.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, s.apps, secret, func() error {
		secret.Data = config
		secret.Type = corev1.SecretTypeOpaque
		secret.Labels = map[string]string{shpyrdv1.LabelApp: current.Name}
		return nil
	}); err != nil {
		return err
	}
	current.Spec = spec
	if portable {
		release := 0
		if len(m.Status.Releases) > 0 {
			release = m.Status.Releases[len(m.Status.Releases)-1].Number
		}
		current.Annotations[shpyrdv1.AnnotationReleaseNote] = fmt.Sprintf("Restore %s archive (v%d)", m.Name, release)
	}
	return s.apps.Update(ctx, current)
}

func (s *Server) runProjectRestore(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation, b *projectarchive.Bundle, m *projectArchiveMetadata) error {
	if err := s.importArchiveAssets(ctx, app, op, b, m); err != nil {
		return err
	}
	if err := s.drainProjectArchive(ctx, app, op); err != nil {
		return err
	}
	if err := s.createArchiveResources(ctx, app, op, m); err != nil {
		return err
	}
	if err := s.fenceArchiveDatabases(ctx, app, op, m.Databases); err != nil {
		return err
	}
	resources := s.archiveParticipants(app, op, m, b)
	save := func(c context.Context, state projectarchive.TransactionState) error {
		op.State = state
		return s.saveProjectArchive(c, app.Namespace, op)
	}
	if err := projectarchive.RestoreTransaction(ctx, resources, save); err != nil {
		return err
	}
	if err := s.applyArchiveConfiguration(ctx, app, m, true); err != nil {
		return err
	}
	if err := s.resumeProjectArchive(ctx, app, op); err != nil {
		return err
	}
	if err := projectarchive.FinalizeTransaction(ctx, resources, save); err != nil {
		return err
	}
	if err := s.cleanupArchiveHelpers(ctx, app.Namespace, op.ID); err != nil {
		return err
	}
	return s.clearProjectArchive(ctx, app.Namespace, op)
}

func (s *Server) rollbackProjectArchive(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation) error {
	if !op.canRollback() {
		return errors.New("workloads may have resumed; automatic rollback could discard new writes")
	}
	if op.State.Phase == "preparing" {
		if err := s.resumeProjectArchive(ctx, app, op); err != nil {
			return err
		}
		return s.clearProjectArchive(ctx, app.Namespace, op)
	}
	if err := s.drainProjectArchive(ctx, app, op); err != nil {
		return err
	}
	resources := s.archiveParticipants(app, op, &op.Original, nil)
	save := func(c context.Context, state projectarchive.TransactionState) error {
		op.State = state
		return s.saveProjectArchive(c, app.Namespace, op)
	}
	if err := projectarchive.RollbackTransaction(ctx, resources, save); err != nil {
		return err
	}
	if err := s.applyArchiveConfiguration(ctx, app, &op.Original, false); err != nil {
		return err
	}
	if err := s.removeCreatedArchiveResources(ctx, app, op); err != nil {
		return err
	}
	if err := s.resumeProjectArchive(ctx, app, op); err != nil {
		return err
	}
	if err := projectarchive.FinalizeTransaction(ctx, resources, save); err != nil {
		return err
	}
	if err := s.cleanupArchiveHelpers(ctx, app.Namespace, op.ID); err != nil {
		return err
	}
	return s.clearProjectArchive(ctx, app.Namespace, op)
}

func (s *Server) removeCreatedArchiveResources(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation) error {
	for _, name := range op.CreatedVolumes {
		v := &shpyrdv1.Volume{}
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: name}, v); apierrors.IsNotFound(err) {
			continue
		} else if err != nil {
			return err
		}
		if v.Labels[projectOperationLabel] != op.ID {
			return fmt.Errorf("volume %s is no longer owned by this restore", name)
		}
		if err := s.apps.Delete(ctx, v); err != nil {
			return err
		}
	}
	for _, name := range op.CreatedDatabases {
		pg := &shpyrdv1.Postgres{}
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: name}, pg); apierrors.IsNotFound(err) {
			delete(op.Limits, name)
			continue
		} else if err != nil {
			return err
		}
		if pg.Labels[projectOperationLabel] != op.ID {
			return fmt.Errorf("database %s is no longer owned by this restore", name)
		}
		if err := s.apps.Delete(ctx, pg); err != nil {
			return err
		}
		delete(op.Limits, name)
	}
	// Recovery after workload startup finalizes only resources that still
	// exist. These newly created resources were removed during rollback.
	op.CreatedVolumes = nil
	op.CreatedDatabases = nil
	return s.saveProjectArchive(ctx, app.Namespace, op)
}
