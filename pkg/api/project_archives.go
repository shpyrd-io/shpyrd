package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/projectarchive"
)

func (s *Server) projectArchiveStatus(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	op, err := s.readProjectArchive(c.Request.Context(), app.Namespace)
	activity, active := s.archiveActive.Load(app.Namespace)
	if apierrors.IsNotFound(err) {
		phase := "idle"
		if active {
			phase = "validating"
			if activity == "measuring" {
				phase = "measuring"
			}
		}
		c.JSON(http.StatusOK, gin.H{"phase": phase, "active": active})
		return
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": op.ID, "kind": op.Kind, "startedAt": op.StartedAt, "phase": op.State.Phase, "resource": op.State.Resource, "error": op.State.Error, "active": active, "warnings": op.Warnings})
}

func (s *Server) exportProjectArchive(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	if !s.acquireArchiveRequest(c, app.Namespace) {
		return
	}
	defer s.releaseArchiveRequest(app.Namespace)
	if rolloutInProgress(app) {
		abort(c, http.StatusConflict, errors.New("wait for the current deployment to finish before exporting the project"))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Hour)
	defer cancel()
	m, err := s.projectArchiveMetadata(ctx, app)
	if err != nil {
		abort(c, http.StatusConflict, err)
		return
	}
	b, err := projectarchive.New("", projectarchive.DefaultLimit)
	if err != nil {
		abort(c, http.StatusInsufficientStorage, err)
		return
	}
	retained := false
	defer func() {
		if !retained {
			_ = b.Close()
		}
	}()
	op, err := s.beginProjectArchive(ctx, app, "export", m)
	if err != nil {
		abort(c, http.StatusConflict, err)
		return
	}
	filename, err := s.buildProjectArchive(ctx, app, op, b, m)
	if err != nil {
		recovery, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
		defer stop()
		recoveryErr := s.resumeProjectArchive(recovery, app, op)
		if recoveryErr == nil {
			recoveryErr = s.clearProjectArchive(recovery, app.Namespace, op)
		} else {
			op.State.Error = recoveryErr.Error()
			_ = s.saveProjectArchive(recovery, app.Namespace, op)
		}
		abort(c, http.StatusBadGateway, errors.Join(err, recoveryErr))
		return
	}
	if err := s.resumeProjectArchive(ctx, app, op); err != nil {
		op.State.Error = err.Error()
		_ = s.saveProjectArchive(context.WithoutCancel(ctx), app.Namespace, op)
		abort(c, http.StatusBadGateway, err)
		return
	}
	if err := s.clearProjectArchive(ctx, app.Namespace, op); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	ticket, err := s.archiveDownloads.keep(b, filename, fmt.Sprintf("%s-%s.tgz", project.SlugOf(app), op.StartedAt.Format("20060102-150405")), app.Namespace)
	if err != nil {
		abort(c, http.StatusServiceUnavailable, err)
		return
	}
	retained = true
	s.audit(c, project.SlugOf(app), "project.export", project.SlugOf(app), "portable project archive generated")
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"ticket": ticket, "expiresInSeconds": 300, "warnings": op.Warnings})
}

func (s *Server) buildProjectArchive(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation, b *projectarchive.Bundle, m *projectArchiveMetadata) (string, error) {
	if err := s.exportProjectImages(ctx, b, m); err != nil {
		return "", err
	}
	if err := s.exportProjectBlob(ctx, b, m); err != nil {
		return "", err
	}
	if err := s.exportProjectGit(ctx, app, op, b, m); err != nil {
		return "", err
	}
	if err := s.drainProjectArchive(ctx, app, op, true); err != nil {
		return "", err
	}
	if err := s.fenceArchiveDatabases(ctx, app, op, m.Databases); err != nil {
		return "", err
	}
	for _, v := range m.Volumes {
		volume := &shpyrdv1.Volume{ObjectMeta: metav1.ObjectMeta{Name: v.Name, Namespace: app.Namespace}, Spec: v.Spec}
		command, err := s.archiveVolumeHelper(ctx, app, volume, op.ID, op.warn)
		if err != nil {
			return "", err
		}
		if err := addArchiveStream(ctx, b, "volumes/"+v.Name+".tar", func(out io.Writer) error {
			return command(ctx, []string{"/shpyrd-server", "project-volume", "export", "/data"}, nil, out)
		}); err != nil {
			return "", err
		}
	}
	for _, pg := range m.Databases {
		db, err := s.archiveDatabase(ctx, app.Namespace, pg.Name)
		if err != nil {
			return "", err
		}
		if err := addArchiveStream(ctx, b, "databases/"+pg.Name+".dump", func(out io.Writer) error { return db.Export(ctx, out) }); err != nil {
			return "", err
		}
	}
	if err := addProjectMetadata(ctx, b, m); err != nil {
		return "", err
	}
	return b.File(ctx)
}
