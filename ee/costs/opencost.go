//go:build !foss

package costs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The estimate: OpenCost's allocation of what the cluster costs, at list
// prices, by pod for one hour. A pod's namespace says its workspace and
// project (the namespace labels), its labels say its process, its node is
// the resource (the node's providerID: an OCID on OKE), and its volumes
// are resources of their own. Platform pods (no project) are lines with
// the namespace as process; each node's unused capacity is an idle line,
// so the estimates on a node add up to the node.

// allocation is one entry of OpenCost's /allocation/compute.
type allocation struct {
	Name       string `json:"name"`
	Properties struct {
		Node            string            `json:"node"`
		Namespace       string            `json:"namespace"`
		Pod             string            `json:"pod"`
		Labels          map[string]string `json:"labels"`
		NamespaceLabels map[string]string `json:"namespaceLabels"`
	} `json:"properties"`
	CPUCoreHours     float64 `json:"cpuCoreHours"`
	CPUCost          float64 `json:"cpuCost"`
	RAMByteHours     float64 `json:"ramByteHours"`
	RAMCost          float64 `json:"ramCost"`
	GPUHours         float64 `json:"gpuHours"`
	GPUCost          float64 `json:"gpuCost"`
	NetworkCost      float64 `json:"networkCost"`
	LoadBalancerCost float64 `json:"loadBalancerCost"`
	PVs              map[string]struct {
		ByteHours  float64 `json:"byteHours"`
		Cost       float64 `json:"cost"`
		ProviderID string  `json:"providerID"`
	} `json:"pvs"`
}

// estimateCurrency is OpenCost's: list prices in US dollars unless its
// pricing is configured otherwise.
const estimateCurrency = "USD"

// resources maps what OpenCost names to what the provider bills: a node
// to its providerID, a PersistentVolume to its CSI volume handle.
type resources struct {
	nodes   map[string]string
	volumes map[string]string
}

func (r resources) node(name string) string {
	if id := r.nodes[name]; id != "" {
		return id
	}
	return name
}

func (r resources) volume(pv, providerID string) string {
	if providerID != "" {
		return providerID
	}
	if id := r.volumes[pv]; id != "" {
		return id
	}
	return pv
}

// fetchAllocation reads one window of OpenCost's allocation, by pod, idle
// split by node.
func fetchAllocation(ctx context.Context, client *http.Client, base string, start, end time.Time) (map[string]allocation, error) {
	q := url.Values{}
	q.Set("window", start.UTC().Format(time.RFC3339)+","+end.UTC().Format(time.RFC3339))
	q.Set("aggregate", "pod")
	q.Set("accumulate", "true")
	q.Set("includeIdle", "true")
	q.Set("idleByNode", "true")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("opencost: %s: %s", res.Status, strings.TrimSpace(string(body)))
	}
	var out struct {
		Code int                     `json:"code"`
		Data []map[string]allocation `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("opencost: %w", err)
	}
	if len(out.Data) == 0 {
		return map[string]allocation{}, nil
	}
	return out.Data[0], nil
}

// processOf names a pod's process the way the metering loop names its
// component: the process label, a database, a Redis, a build.
func processOf(labels map[string]string) string {
	switch {
	case labels["shpyrd_io_process"] != "":
		return labels["shpyrd_io_process"]
	case labels["cnpg_io_cluster"] != "":
		return "postgres/" + labels["cnpg_io_cluster"]
	case labels["shpyrd_io_redis"] != "":
		return "redis/" + labels["shpyrd_io_redis"]
	case labels["kpack_io_build"] != "":
		return "build"
	}
	return "other"
}

// estimateLines turns one window of allocation into cost lines.
func estimateLines(data map[string]allocation, start, end time.Time, res resources) []store.CostLine {
	var out []store.CostLine
	for key, a := range data {
		p := a.Properties
		base := store.CostLine{Kind: store.CostEstimated, Source: "opencost", Start: start, End: end, Currency: estimateCurrency}
		switch {
		case strings.Contains(key, "__idle__"):
			base.Process = "idle"
			if p.Node != "" {
				base.Resource, base.ResourceType = res.node(p.Node), "node"
			}
		case p.NamespaceLabels["shpyrd_io_project_id"] != "":
			base.Workspace = p.NamespaceLabels["shpyrd_io_workspace_id"]
			base.Project = p.NamespaceLabels["shpyrd_io_project_id"]
			base.Process = processOf(p.Labels)
			if strings.HasSuffix(key, "-unmounted-pvcs") {
				base.Process = "volumes"
			}
			if p.Node != "" {
				base.Resource, base.ResourceType = res.node(p.Node), "node"
			}
		default:
			// The platform itself: no project, the namespace says what.
			base.Process = firstNonEmpty(p.Namespace, "unallocated")
			if p.Node != "" {
				base.Resource, base.ResourceType = res.node(p.Node), "node"
			}
		}
		add := func(metric, unit string, quantity, cost float64) {
			if quantity == 0 && cost == 0 {
				return
			}
			l := base
			l.Metric, l.Unit = metric, unit
			if unit != "" {
				q := quantity
				l.Quantity = &q
			}
			c := cost
			l.Cost = &c
			out = append(out, l)
		}
		add("cpu", "core-hours", a.CPUCoreHours, a.CPUCost)
		add("memory", "GiB-hours", a.RAMByteHours/(1<<30), a.RAMCost)
		add("gpu", "gpu-hours", a.GPUHours, a.GPUCost)
		add("network", "", 0, a.NetworkCost)
		add("load-balancer", "", 0, a.LoadBalancerCost)
		for name, pv := range a.PVs {
			if pv.ByteHours == 0 && pv.Cost == 0 {
				continue
			}
			l := base
			l.Metric, l.Unit = "storage", "GiB-hours"
			l.Resource, l.ResourceType = res.volume(pvName(name), pv.ProviderID), "volume"
			q, c := pv.ByteHours/(1<<30), pv.Cost
			l.Quantity, l.Cost = &q, &c
			out = append(out, l)
		}
	}
	return out
}

// pvName takes the PersistentVolume's name out of OpenCost's key for it,
// "cluster=<cluster>:name=<pv>".
func pvName(key string) string {
	if i := strings.LastIndex(key, "name="); i >= 0 {
		return key[i+len("name="):]
	}
	return key
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
