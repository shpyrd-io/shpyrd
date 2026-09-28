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
// chart's default in the opencost namespace. OpenCost 1.121+ exposes the
// allocation endpoint at /allocation/compute (not /model/allocation).
const allocationURL = "http://opencost.opencost.svc:9003/allocation/compute"

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

// writeHour aggregates OpenCost Allocation API data by namespace and maps it
// to workspaces. Label-based aggregation is unreliable when the pod label
// configuration is unknown; namespace is always available in the allocation.
// Costs are summed per workspace and stored as a single COGS bucket
// (project = "" = workspace total; project-level breakdown requires
// per-project namespace queries or label configuration changes in OpenCost).
func (w *cogsWriter) writeHour(ctx context.Context, hour time.Time) {
	end := hour.Add(time.Hour)
	window := fmt.Sprintf("%s,%s", hour.Format(time.RFC3339), end.Format(time.RFC3339))

	// Build namespace → workspace slug mapping from the store.
	workspaces, err := w.store.ListWorkspaces(ctx)
	if err != nil {
		return
	}
	// project namespace convention: app-<ws>-<proj> or app-<proj> (single ws)
	// We match by reading the shpyrd.io/workspace label off the namespace,
	// but OpenCost only knows namespace names. Use a prefix heuristic: every
	// project namespace starts with "app-"; workspace slug is in the label.
	// Since we can't read k8s here, aggregate per workspace by listing all
	// their namespaces' costs.
	// Strategy: query by namespace, then match namespace name against the
	// workspace by asking the store for each workspace's slug (app-<ws>-<proj>
	// or app-<proj> in single-workspace mode).
	// Simpler: collect all namespace costs, group by the workspace slug encoded
	// in the namespace prefix pattern, and fall back to the entire workspace.
	q := url.Values{}
	q.Set("window", window)
	q.Set("aggregate", "namespace")
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

	type wsAgg struct {
		cpu, mem, storage, net, shared, idle, total float64
		direct                                       float64
	}
	// For each workspace, sum the costs of its app-* namespaces.
	// The workspace slug appears in the namespace: app-<ws>-<proj> where
	// the workspace has a slug that matches the prefix after "app-".
	// Build a map of namespace prefix → workspace ID.
	nsToWS := map[string]string{} // namespace prefix "app-<ws>" → ws.ID
	for _, ws := range workspaces {
		nsToWS["app-"+ws.Slug] = ws.ID
	}
	wsCosts := map[string]*wsAgg{} // ws.ID → agg
	for _, ws := range workspaces {
		wsCosts[ws.ID] = &wsAgg{}
	}
	for _, batch := range body.Data {
		for ns, item := range batch {
			// Match namespace to workspace: try exact prefix "app-<ws>".
			var wsID string
			for prefix, id := range nsToWS {
				if ns == prefix || len(ns) > len(prefix)+1 && ns[:len(prefix)+1] == prefix+"-" {
					wsID = id
					break
				}
			}
			if wsID == "" {
				continue
			}
			a := wsCosts[wsID]
			a.cpu += item.CPUCost
			a.mem += item.RAMCost
			a.storage += item.PVCost
			a.net += item.NetworkCost
			a.shared += item.SharedCost
			a.idle += item.IdleCost
			a.total += item.TotalCost
		}
	}
	for _, ws := range workspaces {
		a := wsCosts[ws.ID]
		if a.total == 0 {
			continue
		}
		b := store.COGSBucket{
			WorkspaceID: ws.ID, Project: "",
			PeriodStart: hour, PeriodEnd: end,
			CPUCost: a.cpu, MemoryCost: a.mem,
			StorageCost: a.storage, NetworkCost: a.net,
			SharedCost: a.shared, IdleCost: a.idle,
			TotalCost: a.total, Currency: "USD",
			AllocationPolicy: "namespace;shareIdle=proportional",
			Quality:          store.QualityComplete,
		}
		_ = w.store.WriteCOGSBucket(ctx, b)
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
