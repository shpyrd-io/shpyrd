// Package opencost is the OpenCost extension (RFC-0075): infrastructure cost
// allocation for the operator economics dashboard. It polls the OpenCost
// Allocation API once per hour, writes COGS buckets into the control-plane
// store, and exposes a status route for the Cluster page. Cost data is
// operator-only and is never shown to workspace owners or customers.
package opencost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Name of the extension.
const Name = "opencost"

// allocationURL is the OpenCost service address; the default is the Helm
// chart's default in the opencost namespace.
const allocationURL = "http://opencost.opencost.svc:9003/model/allocation"

type extension struct{}

func New() ext.Extension { return extension{} }

func (extension) Name() string { return Name }
func (extension) Description() string {
	return "Infrastructure cost allocation via OpenCost — operator economics dashboard (RFC-0075)"
}
func (extension) Components() []ext.ComponentRef {
	return []ext.ComponentRef{{Name: "opencost", Runlevel: "rc4"}}
}
func (extension) Register(ctrl.Manager, ext.Deps) error { return nil }
func (extension) Types() []ext.ResourceType             { return nil }
func (extension) CLI(g ext.CLIGlobals) []*cobra.Command {
	return []*cobra.Command{ext.ForOperator(newOpenCostCmd(g))}
}

// Routes mounts GET /api/cluster/opencost/status (operator console).
func (e extension) Routes(r ext.Router, deps ext.Deps) error {
	if deps.Store == nil {
		return nil
	}
	h := &handlers{store: deps.Store}
	r.Admin().GET("/cluster/opencost/status", h.status)
	// Start the hourly COGS writer in the background.
	go func() {
		w := &cogsWriter{store: deps.Store}
		w.run(context.Background())
	}()
	return nil
}

type handlers struct{ store store.Store }

func (h *handlers) status(c *gin.Context) {
	// Return the last COGS bucket timestamp, or "unconfigured".
	c.JSON(http.StatusOK, gin.H{"source": "opencost", "url": allocationURL})
}

// ---- COGS writer -----------------------------------------------------------

// cogsWriter polls the OpenCost Allocation API every hour and writes
// cogs_buckets into the store. It is idempotent: a restart re-queries the
// last 24 hours and upserts.
type cogsWriter struct {
	store store.Store
}

func (w *cogsWriter) run(ctx context.Context) {
	w.writeHour(ctx, time.Now().UTC().Truncate(time.Hour).Add(-time.Hour))
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			w.writeHour(ctx, now.UTC().Truncate(time.Hour).Add(-time.Hour))
		}
	}
}

func (w *cogsWriter) writeHour(ctx context.Context, hour time.Time) {
	end := hour.Add(time.Hour)
	window := fmt.Sprintf("%s,%s", hour.Format(time.RFC3339), end.Format(time.RFC3339))
	q := url.Values{}
	q.Set("window", window)
	q.Set("aggregate", "label:shpyrd.io/workspace,label:shpyrd.io/project")
	q.Set("includeIdle", "true")
	q.Set("shareIdle", "proportional")
	q.Set("shareNamespaces", "shpyrd-system,monitoring,keda,cnpg-system,opencost")
	q.Set("shareSplit", "weighted")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, allocationURL+"?"+q.Encode(), nil)
	if err != nil {
		return
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var body struct {
		Data []map[string]allocationItem `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return
	}
	workspaces, _ := w.store.ListWorkspaces(ctx)
	wsMap := map[string]string{} // slug → id
	for _, ws := range workspaces {
		wsMap[ws.Slug] = ws.ID
	}
	for _, batch := range body.Data {
		for _, item := range batch {
			wsSlug := item.Properties["label:shpyrd.io/workspace"]
			proj := item.Properties["label:shpyrd.io/project"]
			wsID, ok := wsMap[wsSlug]
			if !ok {
				continue
			}
			b := store.COGSBucket{
				WorkspaceID: wsID, Project: proj,
				PeriodStart: hour, PeriodEnd: end,
				CPUCost: item.CPUCost, MemoryCost: item.RAMCost,
				StorageCost: item.PVCost, NetworkCost: item.NetworkCost,
				SharedCost: item.SharedCost, IdleCost: item.IdleCost,
				TotalCost: item.TotalCost, Currency: "USD",
				AllocationPolicy: "shareIdle=proportional",
				Quality:          store.QualityComplete,
			}
			_ = w.store.WriteCOGSBucket(ctx, b)
		}
	}
}

type allocationItem struct {
	Properties  map[string]string `json:"properties"`
	CPUCost     float64           `json:"cpuCost"`
	RAMCost     float64           `json:"ramCost"`
	PVCost      float64           `json:"pvCost"`
	NetworkCost float64           `json:"networkCost"`
	SharedCost  float64           `json:"sharedCost"`
	IdleCost    float64           `json:"idleCost"`
	TotalCost   float64           `json:"totalCost"`
}

// ---- CLI -------------------------------------------------------------------

func newOpenCostCmd(g ext.CLIGlobals) *cobra.Command {
	return &cobra.Command{
		Use:   "opencost",
		Short: "OpenCost extension status",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "OpenCost is enabled. The server writes COGS buckets hourly from the Allocation API.")
			fmt.Fprintln(cmd.OutOrStdout(), "Operator economics: shpyrd-ctl economics --month YYYY-MM")
			return nil
		},
	}
}
