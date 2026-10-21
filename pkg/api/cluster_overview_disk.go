package api

// Disk space on a node is judged against the kubelet's eviction threshold,
// not a fixed line (#207): a node evicts pods when its filesystem falls
// below it, so a warning must come before. And pressure
// passes in minutes, so the overview also says what happened in the last
// 24 hours and 7 days, from recordings that keep it.
import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/shpyrd-io/shpyrd/pkg/install"
)

const (
	// The kubelet's defaults: images 15%, the node's data 10%. With the
	// image and node filesystems one (CRI-O, containerd on one disk), the
	// stricter is what evicts. The operator says otherwise with
	// SHPYRD_DISK_EVICTION_PCT. The kubelets' own config is not read: that
	// needs the kubelet proxy, which reaches far more than it.
	defaultEvictionPct = 15
	// How far above the threshold the overview warns, and calls it urgent.
	diskWarnMargin     = 10
	diskCriticalMargin = 3
)

// evictionPct is the threshold the operator set, else the default; false
// when it is the default.
func evictionPct(vars map[string]string) (float64, bool) {
	if v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(vars[install.VarDiskEvictionPct]), "%"), 64); err == nil && v >= 0 && v < 100 {
		return v, true
	}
	return defaultEvictionPct, false
}

// diskHistoryQuery is what the last day and week say of each node's disk,
// from the recordings alone: the lowest free share in 24 hours, the
// episodes of pressure (rising edges of a 0/1 series: its changes, plus
// one when it holds now, halved), and the kubelet's evictions for disk.
const diskHistoryQuery = `label_replace(min_over_time(shpyrd_node:diskAvailablePct[24h]),"history","diskAvailablePctMin24h","","")` +
	` or label_replace((changes(shpyrd_node:diskPressure[24h]) + shpyrd_node:diskPressure) / 2,"history","diskPressure24h","","")` +
	` or label_replace((changes(shpyrd_node:diskPressure[7d]) + shpyrd_node:diskPressure) / 2,"history","diskPressure7d","","")` +
	` or label_replace(increase(shpyrd_node:diskEvictions[24h]),"history","diskEvictions24h","","")` +
	` or label_replace(increase(shpyrd_node:diskEvictions[7d]),"history","diskEvictions7d","","")`

// diskHistory adds the history values to the nodes: counts whole, the
// lowest share as it is.
func (s *Server) diskHistory(ctx context.Context, nodes map[string]*overviewNode) error {
	samples, err := s.prom.Query(ctx, diskHistoryQuery)
	if err != nil {
		return err
	}
	for _, v := range samples {
		n, key := nodes[v.Labels["node"]], v.Labels["history"]
		if n == nil || key == "" || math.IsNaN(v.Value) {
			continue
		}
		if key != "diskAvailablePctMin24h" {
			v.Value = math.Ceil(v.Value - 0.01) // increase() extrapolates: 2.003 evictions are 2
		}
		n.Values[key] = v.Value
	}
	return nil
}

// diskIssues says what a node's disk needs saying: how close it is to the
// threshold now, how close it came today, and whether it evicted lately.
func diskIssues(n *overviewNode) []string {
	var out []string
	limit, hasLimit := n.Values["diskEvictionPct"]
	free, hasFree := n.Values["diskAvailablePct"]
	near := false
	if hasFree && hasLimit {
		switch {
		case free < limit+diskCriticalMargin:
			out = append(out, fmt.Sprintf("Disk about to evict pods: %s free, evicts below %s", pct(free), pct(limit)))
			near = true
		case free < limit+diskWarnMargin:
			out = append(out, fmt.Sprintf("Low disk space: %s free, evicts below %s", pct(free), pct(limit)))
			near = true
		}
	}
	if low, ok := n.Values["diskAvailablePctMin24h"]; ok && hasLimit && !near && low < limit+diskCriticalMargin {
		out = append(out, fmt.Sprintf("Free disk fell to %s in the last 24 hours; evicts below %s", pct(low), pct(limit)))
	}
	episodes, evictions := n.Values["diskPressure7d"], n.Values["diskEvictions7d"]
	if episodes > 0 || evictions > 0 {
		var parts []string
		if episodes > 0 {
			parts = append(parts, plural(int(episodes), "disk-pressure episode", "disk-pressure episodes"))
		}
		if evictions > 0 {
			parts = append(parts, plural(int(evictions), "pod evicted", "pods evicted"))
		}
		out = append(out, "Evicting: "+strings.Join(parts, ", ")+" in 7 days")
	}
	return out
}

// diskNear says the node is within the warning margin of its threshold.
func diskNear(n overviewNode) (bool, bool) {
	free, ok := n.Values["diskAvailablePct"]
	limit, known := n.Values["diskEvictionPct"]
	if !ok || !known {
		return false, false
	}
	return free < limit+diskWarnMargin, true
}

// pct is a share with one decimal at most: 13.5%, 15%.
func pct(v float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(v, 'f', 1, 64), ".0") + "%"
}
