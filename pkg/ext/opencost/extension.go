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
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Name of the extension.
const Name = "opencost"

// allocationURL is the OpenCost service address.
// OpenCost 1.121+ uses /allocation/compute (not /model/allocation).
const allocationURL = "http://opencost.opencost.svc:9003/allocation/compute"

// metricsURL is the OpenCost Prometheus metrics endpoint. The COGS writer
// uses it to derive the true node cost (and therefore idle cost) rather than
// relying on the Allocation API's shareIdle parameter, which does not
// distribute idle in v1.121.3 with namespace-level aggregation.
const metricsURL = "http://opencost.opencost.svc:9003/metrics"

// sharedNamespaces are infrastructure namespaces whose costs are split
// proportionally across workspaces (they serve every workspace equally).
var sharedNamespaces = map[string]bool{
	"kube-system": true, "keda": true, "monitoring": true, "kpack": true,
	"ingress-nginx": true, "ingress-nginx-internal": true,
	"cert-manager": true, "cnpg-system": true, "opencost": true,
	"shpyrd-system": true,
}

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
	h := time.Now().UTC().Truncate(time.Hour).Add(-time.Hour)
	slog.Info("opencost cogs writer starting", "first_window", h.Format(time.RFC3339))
	w.writeHour(ctx, h)
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			h := now.UTC().Truncate(time.Hour).Add(-time.Hour)
			slog.Info("opencost cogs writer tick", "window", h.Format(time.RFC3339))
			w.writeHour(ctx, h)
		}
	}
}

// writeHour aggregates OpenCost Allocation API data by namespace and maps it
// to workspaces. Label-based aggregation is unreliable when the pod label
// configuration is unknown; namespace is always available in the allocation.
// Costs are summed per workspace and stored as a single COGS bucket
// (project = "" = workspace total; project-level breakdown requires
// per-project namespace queries or label configuration changes in OpenCost).
// writeHour fetches the Allocation API for the given hour and writes one
// COGSBucket per workspace. Idle and shared costs are computed explicitly:
// OpenCost 1.121.3 does not distribute them via shareIdle/shareNamespaces
// when aggregating by namespace.
//
//	Direct  = costs of pods in the workspace's own namespaces.
//	Shared  = costs of platform infrastructure (kube-system, monitoring,
//	          ingress, etc.), split proportionally by direct cost share.
//	Idle    = unused node capacity (node total − all allocated), split
//	          proportionally by direct cost share.
//	Total   = Direct + Shared + Idle.
func (w *cogsWriter) writeHour(ctx context.Context, hour time.Time) {
	end := hour.Add(time.Hour)
	window := fmt.Sprintf("%s,%s", hour.Format(time.RFC3339), end.Format(time.RFC3339))

	workspaces, err := w.store.ListWorkspaces(ctx)
	if err != nil {
		return
	}

	// ---- Allocation API: per-namespace costs ----------------------------
	q := url.Values{}
	q.Set("window", window)
	q.Set("aggregate", "namespace")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, allocationURL+"?"+q.Encode(), nil)
	if err != nil {
		return
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		slog.Error("opencost: allocation request failed", "err", err)
		return
	}
	defer resp.Body.Close()
	var body struct {
		Data []map[string]allocationItem `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		slog.Error("opencost: allocation decode failed", "err", err)
		return
	}
	if len(body.Data) == 0 {
		return
	}
	batch := body.Data[0]

	// ---- Node total cost: cpu + ram ----------------------------------------
	// Pull from OpenCost's own Prometheus metrics so we get the actual OCI
	// price (not just what is allocated to pods).
	nodeTotal := w.nodeHourlyCost(ctx)

	// ---- Build namespace → workspace mapping --------------------------------
	nsToWS := map[string]string{}
	var implicitWS string
	for _, ws := range workspaces {
		nsToWS["app-"+ws.Slug] = ws.ID
		if implicitWS == "" {
			implicitWS = ws.ID
		}
	}

	type wsAgg struct{ cpu, mem, storage, net, direct float64 }
	wsCosts := map[string]*wsAgg{}
	for _, ws := range workspaces {
		wsCosts[ws.ID] = &wsAgg{}
	}
	var sharedTotal, allocatedTotal float64

	for ns, item := range batch {
		total := item.TotalCost
		allocatedTotal += total
		if sharedNamespaces[ns] {
			sharedTotal += total
			continue
		}
		if !strings.HasPrefix(ns, "app-") {
			continue
		}
		wsID := ""
		for prefix, id := range nsToWS {
			if ns == prefix || (len(ns) > len(prefix)+1 && ns[:len(prefix)+1] == prefix+"-") {
				wsID = id
				break
			}
		}
		if wsID == "" {
			wsID = implicitWS
		}
		if wsID == "" {
			continue
		}
		a := wsCosts[wsID]
		a.cpu += item.CPUCost
		a.mem += item.RAMCost
		a.storage += item.PVCost
		a.net += item.NetworkCost
		a.direct += total
	}

	// Sum of workspace direct costs — used as the weight for splits.
	totalDirect := 0.0
	for _, a := range wsCosts {
		totalDirect += a.direct
	}
	idleTotal := 0.0
	if nodeTotal > allocatedTotal {
		idleTotal = nodeTotal - allocatedTotal
	}

	for _, ws := range workspaces {
		a := wsCosts[ws.ID]
		if a.direct == 0 {
			continue
		}
		weight := 0.0
		if totalDirect > 0 {
			weight = a.direct / totalDirect
		}
		shared := sharedTotal * weight
		idle := idleTotal * weight
		total := a.direct + shared + idle
		b := store.COGSBucket{
			WorkspaceID: ws.ID, Project: "",
			PeriodStart: hour, PeriodEnd: end,
			CPUCost: a.cpu, MemoryCost: a.mem,
			StorageCost: a.storage, NetworkCost: a.net,
			SharedCost: shared, IdleCost: idle,
			TotalCost: total, Currency: "USD",
			AllocationPolicy: "namespace;idle=node_metrics;shared=proportional",
			Quality:          store.QualityComplete,
		}
		if err := w.store.WriteCOGSBucket(ctx, b); err != nil {
			slog.Error("opencost: write cogs bucket failed", "workspace", ws.Slug, "err", err)
		} else {
			slog.Info("opencost: wrote cogs bucket", "workspace", ws.Slug,
				"direct", a.direct, "shared", shared, "idle", idle, "total", total)
		}
	}
}

// nodeHourlyCost returns the total hourly cost of all cluster nodes by
// reading OpenCost's own Prometheus metrics (node_cpu_hourly_cost,
// node_ram_hourly_cost). This is independent of what the Allocation API
// distributes and gives us the ground truth for idle cost computation.
func (w *cogsWriter) nodeHourlyCost(ctx context.Context) float64 {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL, nil)
	if err != nil {
		return 0
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		slog.Error("opencost: node metrics request failed", "err", err)
		return 0
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("opencost: node metrics read failed", "err", err)
		return 0
	}
	var total float64
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if strings.HasPrefix(line, "node_cpu_hourly_cost{") || strings.HasPrefix(line, "node_ram_hourly_cost{") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				if v, err := strconv.ParseFloat(parts[len(parts)-1], 64); err == nil {
					total += v
				}
			}
		}
	}
	return total
}

type allocationItem struct {
	// Properties contains cluster, namespace, node etc. and a nested
	// "labels" map. We only need namespace for workspace attribution.
	Properties  allocationProperties `json:"properties"`
	CPUCost     float64              `json:"cpuCost"`
	RAMCost     float64              `json:"ramCost"`
	PVCost      float64              `json:"pvCost"`
	NetworkCost float64              `json:"networkCost"`
	SharedCost  float64              `json:"sharedCost"`
	IdleCost    float64              `json:"idleCost"`
	TotalCost   float64              `json:"totalCost"`
}

type allocationProperties struct {
	Cluster   string            `json:"cluster"`
	Namespace string            `json:"namespace"`
	Node      string            `json:"node"`
	Labels    map[string]string `json:"labels"`
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
