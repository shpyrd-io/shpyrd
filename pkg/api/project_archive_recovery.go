package api

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/projectarchive"
)

func (s *Server) projectGate(namespace string) *sync.RWMutex {
	gate, _ := s.projectGates.LoadOrStore(namespace, &sync.RWMutex{})
	return gate.(*sync.RWMutex)
}
func (s *Server) releaseArchiveRequest(namespace string) {
	s.archiveActive.Delete(namespace)
	s.projectGate(namespace).Unlock()
}
func (s *Server) acquireArchiveRequest(c *gin.Context, namespace string) bool {
	gate := s.projectGate(namespace)
	if !gate.TryLock() {
		abort(c, http.StatusLocked, errors.New("another project change is still running; wait and retry"))
		return false
	}

	if _, loaded := s.archiveActive.LoadOrStore(namespace, true); loaded {
		gate.Unlock()
		abort(c, http.StatusLocked, errors.New("a project archive request is still running"))
		return false
	}
	return true
}

func (s *Server) recoverProjectArchive(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	if !s.acquireArchiveRequest(c, app.Namespace) {
		return
	}
	defer s.releaseArchiveRequest(app.Namespace)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 20*time.Minute)
	defer cancel()
	op, err := s.readProjectArchive(ctx, app.Namespace)
	if err != nil {
		abort(c, http.StatusConflict, err)
		return
	}
	switch {
	case !op.canRollback():
		// Never roll back after workloads could have accepted writes.
		// If clearing maintenance was interrupted, recheck readiness first.
		if app.Annotations[shpyrdv1.AnnotationMaintenance] != "" {
			err = s.resumeProjectArchive(ctx, app, op)
		}
		if err == nil {
			err = s.clearArchiveDatabaseMaintenance(ctx, app.Namespace, op)
		}
		if err == nil && op.Kind == "move" {
			err = s.finishProjectMove(ctx, app, op, op.Move != nil && !op.Move.RolledBack)
		}
		if err == nil && op.Kind == "restore" {
			inventory := op.Original
			for _, name := range op.CreatedVolumes {
				inventory.Volumes = append(inventory.Volumes, archivedVolume{Name: name})
			}
			for _, name := range op.CreatedDatabases {
				inventory.Databases = append(inventory.Databases, archivedDatabase{Name: name})
			}
			resources := s.archiveParticipants(app, op, &inventory, nil)
			err = projectarchive.FinalizeTransaction(ctx, resources, func(context.Context, projectarchive.TransactionState) error { return nil })
		}
		if err == nil {
			err = s.cleanupArchiveHelpers(ctx, app.Namespace, op.ID)
		}
		if err == nil {
			err = s.clearProjectArchive(ctx, app.Namespace, op)
		}
	case op.Kind == "export":
		err = s.resumeProjectArchive(ctx, app, op)
		if err == nil {
			err = s.clearProjectArchive(ctx, app.Namespace, op)
		}
	case op.Kind == "move":
		err = s.rollbackProjectMove(ctx, app, op)
	case op.Kind == "restore":
		err = s.rollbackProjectArchive(ctx, app, op)
	default:
		err = errors.New("unrecognized project recovery operation")
	}
	if err != nil {
		op.State.Error = err.Error()
		_ = s.saveProjectArchive(ctx, app.Namespace, op)
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "recovered"})
}
