package api

// The operational overview never enumerates projects or tenant pods. It reads a
// shared, immutable node snapshot and low-cardinality recording rules. Snapshots
// are retained for cursor pagination; a refresh cannot reorder an open page.
import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shpyrd-io/shpyrd/deploy"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type overviewNode struct {
	NodeInfo
	Values        map[string]float64 `json:"values"`
	Conditions    []string           `json:"conditions"`
	Unschedulable bool               `json:"unschedulable"`
	Issues        []string           `json:"issues"`
	DiskMount     string             `json:"diskMount,omitempty"`
	Misplaced     int                `json:"misplaced"`
}
type overviewPool struct {
	Name         string             `json:"name"`
	Nodes        int                `json:"nodes"`
	Ready        int                `json:"ready"`
	Issues       int                `json:"issues"`
	Missing      int                `json:"missing"`
	Values       map[string]float64 `json:"values"`
	Coverage     map[string]int     `json:"coverage"`
	Distribution map[string]int     `json:"distribution"`
}
type overviewSnapshot struct {
	ID      string         `json:"snapshot"`
	At      time.Time      `json:"updatedAt"`
	Nodes   []overviewNode `json:"-"`
	Pools   []overviewPool `json:"pools"`
	Errors  []string       `json:"errors"`
	Install *InstallInfo   `json:"install,omitempty"`
}
type overviewCache struct {
	mu        sync.Mutex
	snapshots []*overviewSnapshot
	pending   chan struct{}
	attempted time.Time
	err       error
}

func (s *Server) overviewSnapshot(ctx context.Context, id string) (*overviewSnapshot, error) {
	c := &s.overview
	c.mu.Lock()
	if id != "" {
		defer c.mu.Unlock()
		for _, snap := range c.snapshots {
			if snap.ID == id {
				return snap, nil
			}
		}
		return nil, errors.New("snapshot expired; refresh the overview")
	}
	var latest *overviewSnapshot
	if len(c.snapshots) > 0 {
		latest = c.snapshots[len(c.snapshots)-1]
	}
	if c.pending == nil && time.Since(c.attempted) >= 30*time.Second {
		c.pending = make(chan struct{})
		c.attempted = time.Now()
		go func() {
			// One shared refresh survives a viewer navigating away, but has a hard deadline.
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			snap, err := s.buildOverview(ctx)
			c.mu.Lock()
			defer c.mu.Unlock()
			c.err = err
			if err == nil {
				c.snapshots = append(c.snapshots, snap)
				if len(c.snapshots) > 10 {
					c.snapshots = c.snapshots[1:]
				}
			}
			close(c.pending)
			c.pending = nil
		}()
	}
	pending, err := c.pending, c.err
	c.mu.Unlock()
	if latest != nil {
		return latest, nil
	}
	if pending == nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-pending:
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.snapshots) > 0 {
		return c.snapshots[len(c.snapshots)-1], nil
	}
	return nil, c.err
}

func (s *Server) buildOverview(ctx context.Context) (*overviewSnapshot, error) {
	out := &overviewSnapshot{ID: strconv.FormatInt(time.Now().UnixNano(), 36), At: time.Now().UTC(), Nodes: []overviewNode{}, Pools: []overviewPool{}, Errors: []string{}}
	token := ""
	for {
		list, err := s.kube.Kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 500, Continue: token})
		if err != nil {
			return nil, err
		}
		for _, n := range list.Items {
			row := overviewNode{NodeInfo: nodeInfo(n), Values: map[string]float64{}, Conditions: []string{}, Issues: []string{}, Unschedulable: n.Spec.Unschedulable}
			for _, cond := range n.Status.Conditions {
				if cond.Status == corev1.ConditionTrue && cond.Type != corev1.NodeReady {
					row.Conditions = append(row.Conditions, string(cond.Type))
				}
			}
			out.Nodes = append(out.Nodes, row)
		}
		token = list.Continue
		if token == "" {
			break
		}
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].Name < out.Nodes[j].Name })
	byName := map[string]*overviewNode{}
	for i := range out.Nodes {
		byName[out.Nodes[i].Name] = &out.Nodes[i]
	}
	if s.prom == nil {
		out.Errors = append(out.Errors, "Metrics are not configured.")
	} else {
		// Raw pod series and filesystem/device labels never enter this query.
		samples, err := s.prom.Query(ctx, `{__name__=~"shpyrd_node:(cpuUsed|cpuCapacity|cpuAllocatable|cpuRequested|memoryUsed|memoryCapacity|memoryAllocatable|memoryRequested|pods|podCapacity|diskSize|diskFree|diskAvailable|diskAvailablePct|inodesAvailablePct|readIOPS|writeIOPS|readBytes|writeBytes|receiveBytes|transmitBytes|receiveErrors|transmitErrors|receiveDrops|transmitDrops)",node!=""}`)
		if err != nil {
			out.Errors = append(out.Errors, "Node metrics could not be refreshed.")
		} else {
			for _, v := range samples {
				if v.Timestamp < float64(time.Now().Add(-120*time.Second).Unix()) {
					continue
				}
				if n := byName[v.Labels["node"]]; n != nil {
					if v.Labels["mountpoint"] != "" {
						n.DiskMount = v.Labels["mountpoint"]
					}
					n.Values[strings.TrimPrefix(v.Labels["__name__"], "shpyrd_node:")] = v.Value
				}
			}
			if len(samples) == 0 {
				out.Errors = append(out.Errors, "Node recording rules are unavailable. Update the monitoring component; no raw pod scan is used as a fallback.")
			}
		}
	}
	if info, err := install.ReadInstallInfo(ctx, s.kube, ""); err == nil {
		out.Install = &InstallInfo{Profile: info.Profile, Version: info.Version, Domain: info.Vars[install.VarDomain], UpdatedAt: info.UpdatedAt}
		s.overviewPlacement(ctx, out, byName, info.Vars)
	}
	for i := range out.Nodes {
		evaluateOverviewNode(&out.Nodes[i])
	}
	out.Pools = aggregateOverview(out.Nodes)
	return out, nil
}

// Placement diagnostics inspect only namespaces of installed platform components,
// never all namespaces or all pods. Work is bounded even for a misconfigured
// platform namespace, and incompleteness is visible instead of reported as zero.
func (s *Server) overviewPlacement(ctx context.Context, out *overviewSnapshot, nodes map[string]*overviewNode, vars map[string]string) {
	platform := vars[install.VarPlatformPool]
	if platform == "" {
		return
	}
	records, err := install.ReadRecords(ctx, s.kube, "")
	if err != nil {
		out.Errors = append(out.Errors, "Platform placement is unavailable.")
		return
	}
	namespaces := map[string]bool{install.DefaultSystemNamespace: true}
	for name := range records {
		if comp, e := install.LoadComponent(deploy.FS, name); e == nil && comp.Namespace != "" {
			namespaces[comp.Namespace] = true
		}
	}
	names := make([]string, 0, len(namespaces))
	for ns := range namespaces {
		names = append(names, ns)
	}
	sort.Strings(names)
	scanned := 0
	for _, ns := range names {
		token := ""
		for {
			list, e := s.kube.Kube.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{Limit: 100, Continue: token})
			if e != nil {
				out.Errors = append(out.Errors, "Platform placement is incomplete.")
				return
			}
			for _, p := range list.Items {
				scanned++
				if scanned > 5000 {
					out.Errors = append(out.Errors, "Platform placement inspection reached its 5,000-pod limit; counts are partial.")
					return
				}
				n := nodes[p.Spec.NodeName]
				if n == nil || n.Pool == "" || n.Pool == platform || p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed || p.Spec.NodeSelector[install.PoolLabel] == n.Pool {
					continue
				}
				skip := false
				for _, owner := range p.OwnerReferences {
					if owner.Kind == "DaemonSet" {
						skip = true
					}
				}
				if ns == "kube-system" && (p.Labels["k8s-app"] == "kube-dns" || strings.HasPrefix(p.Name, "coredns-") || strings.HasPrefix(p.Name, "kube-dns-autoscaler-")) {
					skip = true
				}
				if !skip {
					n.Misplaced++
				}
			}
			token = list.Continue
			if token == "" {
				break
			}
		}
	}
}
func ratio(v map[string]float64, a, b string) (float64, bool) {
	x, ok := v[a]
	y, yes := v[b]
	if !ok || !yes || y <= 0 {
		return 0, false
	}
	return 100 * x / y, true
}
func evaluateOverviewNode(n *overviewNode) {
	if !n.Ready {
		n.Issues = append(n.Issues, "Not ready")
	}
	for _, c := range n.Conditions {
		if strings.HasSuffix(c, "Pressure") || c == "NetworkUnavailable" {
			n.Issues = append(n.Issues, c)
		}
	}
	for _, r := range []struct{ key, a, b string }{{"cpuBusyPct", "cpuUsed", "cpuCapacity"}, {"cpuRequestedPct", "cpuRequested", "cpuAllocatable"}, {"memoryUsedPct", "memoryUsed", "memoryCapacity"}, {"memoryRequestedPct", "memoryRequested", "memoryAllocatable"}, {"podUsedPct", "pods", "podCapacity"}} {
		if v, ok := ratio(n.Values, r.a, r.b); ok {
			n.Values[r.key] = v
		}
	}
	if v, ok := n.Values["diskAvailablePct"]; ok && v < 10 {
		n.Issues = append(n.Issues, "Low filesystem space")
	}
	if v, ok := n.Values["inodesAvailablePct"]; ok && v < 10 {
		n.Issues = append(n.Issues, "Low free inodes")
	}
	if v, ok := n.Values["cpuRequestedPct"]; ok && v >= 90 {
		n.Issues = append(n.Issues, "CPU requests ≥90%")
	}
	if v, ok := n.Values["memoryRequestedPct"]; ok && v >= 90 {
		n.Issues = append(n.Issues, "Memory requests ≥90%")
	}
	if v, ok := n.Values["podUsedPct"]; ok && v >= 90 {
		n.Issues = append(n.Issues, "Pod slots ≥90%")
	}
	if n.Misplaced > 0 {
		n.Issues = append(n.Issues, fmt.Sprintf("%d misplaced platform pods", n.Misplaced))
	}
	if _, ok := n.Values["cpuBusyPct"]; !ok {
		n.Issues = append(n.Issues, "CPU telemetry unavailable")
	}
	if _, ok := n.Values["memoryUsedPct"]; !ok {
		n.Issues = append(n.Issues, "Memory telemetry unavailable")
	}
}
func aggregateOverview(nodes []overviewNode) []overviewPool {
	pools := map[string]*overviewPool{}
	for _, n := range nodes {
		p := pools[n.Pool]
		if p == nil {
			p = &overviewPool{Name: n.Pool, Values: map[string]float64{}, Coverage: map[string]int{}, Distribution: map[string]int{}}
			pools[n.Pool] = p
		}
		p.Nodes++
		if n.Ready {
			p.Ready++
		}
		if len(n.Issues) > 0 {
			p.Issues++
		}
		_, cpuOK := n.Values["cpuBusyPct"]
		_, memoryOK := n.Values["memoryUsedPct"]
		if !cpuOK || !memoryOK {
			p.Missing++
		}
		for _, k := range []string{"cpuBusyPct", "cpuRequestedPct", "memoryUsedPct", "memoryRequestedPct", "podUsedPct"} {
			if n.Values[k] >= 85 {
				p.Distribution[k]++
			}
		}
		for _, k := range []string{"diskAvailablePct", "inodesAvailablePct"} {
			if v, ok := n.Values[k]; ok {
				p.Coverage[k]++
				if v < 10 {
					p.Distribution[k]++
				}
			}
		}
		// Each ratio uses only nodes having BOTH numerator and denominator. Missing
		// telemetry must never depress a pool's utilization by contributing capacity.
		for _, r := range []struct{ key, a, b string }{{"cpuBusyPct", "cpuUsed", "cpuCapacity"}, {"cpuRequestedPct", "cpuRequested", "cpuAllocatable"}, {"memoryUsedPct", "memoryUsed", "memoryCapacity"}, {"memoryRequestedPct", "memoryRequested", "memoryAllocatable"}, {"podUsedPct", "pods", "podCapacity"}} {
			if _, ok := ratio(n.Values, r.a, r.b); ok {
				p.Values[r.a] += n.Values[r.a]
				p.Values[r.b] += n.Values[r.b]
				p.Coverage[r.key]++
			}
		}
		for _, k := range []string{"readIOPS", "writeIOPS", "readBytes", "writeBytes", "receiveBytes", "transmitBytes"} {
			if v, ok := n.Values[k]; ok {
				p.Values[k] += v
				p.Coverage[k]++
			}
		}
	}
	out := []overviewPool{}
	for _, p := range pools {
		for _, r := range []struct{ key, a, b string }{{"cpuBusyPct", "cpuUsed", "cpuCapacity"}, {"cpuRequestedPct", "cpuRequested", "cpuAllocatable"}, {"memoryUsedPct", "memoryUsed", "memoryCapacity"}, {"memoryRequestedPct", "memoryRequested", "memoryAllocatable"}, {"podUsedPct", "pods", "podCapacity"}} {
			if v, ok := ratio(p.Values, r.a, r.b); ok {
				p.Values[r.key] = v
			}
		}
		out = append(out, *p)
	}
	rank := func(s string) string {
		switch s {
		case "platform":
			return "0"
		case "data":
			return "1"
		case "apps":
			return "2"
		}
		return "3" + s
	}
	sort.Slice(out, func(i, j int) bool { return rank(out[i].Name) < rank(out[j].Name) })
	return out
}
func (s *Server) clusterOverview(c *gin.Context) {
	snap, err := s.overviewSnapshot(c.Request.Context(), "")
	if err != nil {
		abort(c, 502, err)
		return
	}
	c.JSON(http.StatusOK, snap)
}
func (s *Server) overviewNodes(c *gin.Context) {
	snap, err := s.overviewSnapshot(c.Request.Context(), c.Query("snapshot"))
	if err != nil {
		abort(c, http.StatusConflict, err)
		return
	}
	limit, err := boundedInt(c.DefaultQuery("limit", "50"), 1, 100)
	if err != nil {
		abort(c, 400, err)
		return
	}
	offset, err := boundedInt(c.DefaultQuery("offset", "0"), 0, len(snap.Nodes))
	if err != nil {
		abort(c, 400, err)
		return
	}
	if offset > 0 && c.Query("snapshot") == "" {
		abort(c, 400, errors.New("pagination requires a snapshot"))
		return
	}
	rows := filterOverviewNodes(snap.Nodes, c.Query("pool"), c.Query("search"), c.Query("zone"), c.Query("status"), c.Query("issues") == "true")
	key := c.DefaultQuery("sort", "cpuRequestedPct")
	allowed := map[string]bool{"severity": true, "name": true, "cpuRequestedPct": true, "cpuBusyPct": true, "memoryUsedPct": true, "memoryRequestedPct": true, "podUsedPct": true, "diskAvailablePct": true, "readIOPS": true, "writeIOPS": true, "receiveBytes": true, "transmitBytes": true}
	if !allowed[key] {
		abort(c, 400, errors.New("invalid sort"))
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if key == "severity" {
			a, b := overviewSeverity(rows[i]), overviewSeverity(rows[j])
			if a != b {
				return a > b
			}
			return rows[i].Name < rows[j].Name
		}
		if key == "name" {
			return rows[i].Name < rows[j].Name
		}
		a, ok := rows[i].Values[key]
		b, yes := rows[j].Values[key]
		if ok != yes {
			return ok
		}
		if a == b {
			return rows[i].Name < rows[j].Name
		}
		if key == "diskAvailablePct" {
			return a < b
		}
		return a > b
	})
	total := len(rows)
	if offset > total {
		offset = total
	}
	end := min(total, offset+limit)
	view := c.DefaultQuery("view", "compute")
	if view != "compute" && view != "storage" && view != "network" {
		abort(c, 400, errors.New("invalid view"))
		return
	}
	page := make([]overviewNode, 0, end-offset)
	for _, n := range rows[offset:end] {
		n.Values = overviewView(n.Values, view)
		page = append(page, n)
	}
	next := -1
	if end < total {
		next = end
	}
	c.JSON(200, gin.H{"snapshot": snap.ID, "updatedAt": snap.At, "nodes": page, "total": total, "next": next})
}
func filterOverviewNodes(nodes []overviewNode, pool, search, zone, status string, issues bool) []overviewNode {
	out := []overviewNode{}
	search = strings.ToLower(search)
	for _, n := range nodes {
		if pool != "*" && n.Pool != pool || search != "" && !strings.Contains(strings.ToLower(n.Name), search) || zone != "" && n.Zone != zone || status == "ready" && !n.Ready || status == "not-ready" && n.Ready || issues && len(n.Issues) == 0 {
			continue
		}
		out = append(out, n)
	}
	return out
}
func overviewView(values map[string]float64, view string) map[string]float64 {
	keys := map[string][]string{"compute": {"cpuBusyPct", "cpuRequestedPct", "memoryUsedPct", "memoryRequestedPct", "pods", "podCapacity", "cpuCapacity", "cpuAllocatable", "memoryCapacity", "memoryAllocatable"}, "storage": {"diskSize", "diskFree", "diskAvailable", "diskAvailablePct", "inodesAvailablePct", "readIOPS", "writeIOPS", "readBytes", "writeBytes"}, "network": {"receiveBytes", "transmitBytes", "receiveErrors", "transmitErrors", "receiveDrops", "transmitDrops"}}
	out := map[string]float64{}
	for _, k := range keys[view] {
		if v, ok := values[k]; ok {
			out[k] = v
		}
	}
	return out
}
func boundedInt(s string, lo, hi int) (int, error) {
	n, e := strconv.Atoi(s)
	if e != nil || n < lo || n > hi {
		return 0, fmt.Errorf("value must be between %d and %d", lo, hi)
	}
	return n, nil
}

func overviewSeverity(n overviewNode) int {
	if !n.Ready {
		return 3
	}
	if len(n.Conditions) > 0 {
		return 2
	}
	if len(n.Issues) > 0 {
		return 1
	}
	return 0
}
