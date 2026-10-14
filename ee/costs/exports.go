//go:build !foss

package costs

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shpyrd-io/shpyrd/ee/licensing"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

type exportWorker struct{ store store.Store }

func (*exportWorker) NeedLeaderElection() bool { return true }
func (w *exportWorker) Start(ctx context.Context) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if !licensing.Active() {
				continue
			}
			jobCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			_, err := w.store.BuildCostExport(jobCtx)
			cancel()
			if err != nil {
				slog.Warn("cost export failed", "error", err)
			}
		}
	}
}
func (h *handlers) createExport(c *gin.Context) {
	q, _, err := h.explorerQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	job, err := h.store.CreateCostExport(c.Request.Context(), q)
	if errors.Is(err, store.ErrConflict) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Five exports are already queued. Try again when one finishes."})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, job)
}
func (h *handlers) exportStatus(c *gin.Context) {
	job, err := h.store.CostExport(c.Request.Context(), c.Param("id"))
	if errors.Is(err, store.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Export not found or expired"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, job)
}
func (h *handlers) downloadExport(c *gin.Context) {
	job, err := h.store.CostExport(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Export not found or expired"})
		return
	}
	if job.Status != "ready" {
		c.JSON(http.StatusConflict, gin.H{"error": "Export is not ready"})
		return
	}
	first, err := h.store.CostExportChunk(c.Request.Context(), job.ID, 0)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Export could not be read"})
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="cluster-costs.csv"`)
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Length", strconv.FormatInt(job.Bytes, 10))
	if _, err = c.Writer.Write(first); err != nil {
		return
	}
	for seq := 1; seq < job.Chunks; seq++ {
		b, err := h.store.CostExportChunk(c.Request.Context(), job.ID, seq)
		if err != nil {
			panic(http.ErrAbortHandler)
		}
		if _, err = c.Writer.Write(b); err != nil {
			return
		}
	}
}
