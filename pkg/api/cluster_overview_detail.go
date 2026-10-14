package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

type overviewQueryEntry struct {
	ready chan struct{}
	at    time.Time
	data  []byte
	err   error
}
type overviewQueryCache struct {
	mu      sync.Mutex
	entries map[string]*overviewQueryEntry
	active  int
}

// A bounded cache deduplicates identical requests and limits concurrent expensive
// work. Errors are cached briefly too, preventing a failing provider retry storm.
func (s *Server) cachedOverview(c *gin.Context, key string, load func(context.Context) (any, error)) {
	cache := &s.overviewQueries
	cache.mu.Lock()
	if cache.entries == nil {
		cache.entries = map[string]*overviewQueryEntry{}
	}
	entry := cache.entries[key]
	if entry != nil && time.Since(entry.at) > 30*time.Second {
		select {
		case <-entry.ready:
			delete(cache.entries, key)
			entry = nil
		default:
		}
	}
	if entry == nil {
		if cache.active >= 4 {
			cache.mu.Unlock()
			c.Header("Retry-After", "5")
			abort(c, 429, errors.New("metrics queries are busy; retry shortly"))
			return
		}
		if len(cache.entries) >= 64 {
			oldest := ""
			var at time.Time
			for k, v := range cache.entries {
				select {
				case <-v.ready:
					if oldest == "" || v.at.Before(at) {
						oldest = k
						at = v.at
					}
				default:
				}
			}
			if oldest != "" {
				delete(cache.entries, oldest)
			}
		}
		entry = &overviewQueryEntry{ready: make(chan struct{}), at: time.Now()}
		cache.entries[key] = entry
		cache.active++
		go func(e *overviewQueryEntry) {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			v, err := load(ctx)
			var b []byte
			if err == nil {
				b, err = json.Marshal(v)
			}
			cache.mu.Lock()
			e.data = b
			e.err = err
			cache.active--
			close(e.ready)
			cache.mu.Unlock()
		}(entry)
	}
	cache.mu.Unlock()
	select {
	case <-c.Request.Context().Done():
		return
	case <-entry.ready:
	}
	if entry.err != nil {
		abort(c, 502, entry.err)
		return
	}
	c.Data(200, "application/json", entry.data)
}
func (s *Server) overviewCharts(c *gin.Context) {
	if s.prom == nil {
		abort(c, 501, errors.New("metrics are not configured"))
		return
	}
	rng := c.DefaultQuery("range", "1h")
	dur, ok := rangeOptions[rng]
	if !ok {
		abort(c, 400, errors.New("invalid range"))
		return
	}
	view := c.DefaultQuery("view", "compute")
	if view != "compute" && view != "storage" && view != "network" {
		abort(c, 400, errors.New("invalid view"))
		return
	}
	names := []string{}
	if c.Query("nodes") != "" {
		names = strings.Split(c.Query("nodes"), ",")
	}
	if len(names) > 5 {
		abort(c, 400, errors.New("compare at most five nodes"))
		return
	}
	for _, n := range names {
		if len(n) > 253 || n == "" {
			abort(c, 400, errors.New("invalid node"))
			return
		}
	}
	sort.Strings(names)
	device, mount := c.Query("device"), c.Query("mount")
	if (device != "" || mount != "") && (len(names) != 1 || view == "compute") {
		abort(c, 400, errors.New("device charts require one node and a storage or network view"))
		return
	}
	if len(device) > 256 || len(mount) > 1024 {
		abort(c, 400, errors.New("invalid device selector"))
		return
	}
	pool := c.Query("pool")
	if len(pool) > 63 {
		abort(c, 400, errors.New("invalid pool"))
		return
	}
	sel := "pool=" + strconv.Quote(pool)
	prefix := "shpyrd_pool:"
	label := "pool"
	if len(names) > 0 {
		escaped := []string{}
		for _, n := range names {
			escaped = append(escaped, regexp.QuoteMeta(n))
		}
		sel = "node=~" + strconv.Quote(strings.Join(escaped, "|"))
		prefix = "shpyrd_node:"
		label = "node"
	}
	type panel struct{ id, title, unit string }
	panels := map[string][]panel{"compute": {{"cpuBusyPct", "CPU busy", "%"}, {"memoryUsedPct", "Memory used", "%"}}, "storage": {{"readIOPS", "Disk reads", "IOPS"}, {"writeIOPS", "Disk writes", "IOPS"}, {"readBytes", "Disk read throughput", "bytes/s"}, {"writeBytes", "Disk write throughput", "bytes/s"}}, "network": {{"receiveBytes", "Network receive", "bytes/s"}, {"transmitBytes", "Network transmit", "bytes/s"}}}[view]
	if device != "" || mount != "" {
		if view == "storage" && mount != "" {
			panels = []panel{{"available", "Available filesystem space", "bytes"}, {"used", "Used filesystem space", "bytes"}}
		} else if view == "storage" {
			panels = append(panels, panel{"readLatency", "Read latency", "ms"}, panel{"writeLatency", "Write latency", "ms"})
		}
	}
	s.cachedOverview(c, "charts:"+rng+":"+view+":"+sel+":"+device+":"+mount, func(ctx context.Context) (any, error) {
		end := time.Now().Truncate(30 * time.Second)
		step := time.Duration((int64(dur/time.Second)+238)/239) * time.Second
		if step < 30*time.Second {
			step = 30 * time.Second
		}
		charts := []Chart{}
		for _, p := range panels {
			q := prefix + p.id + "{" + sel + "}"
			queryLabel := label
			if device != "" || mount != "" {
				q = overviewDeviceQuery(names[0], device, mount, view, p.id)
				queryLabel = "device"
			}

			ch := s.runChart(ctx, chartQuery{Chart: Chart{ID: p.id, Title: p.title, Unit: p.unit, Kind: "line"}, Query: q, LabelKey: queryLabel}, end.Add(-dur), end, step, metricsOptions{})
			if len(ch.Series) > 5 {
				ch.Series = ch.Series[:5]
				ch.Note = "Series limit reached"
			}
			charts = append(charts, ch)
		}
		return gin.H{"charts": charts, "updatedAt": end}, nil
	})
}

const overviewFSFilter = `fstype!~"tmpfs|devtmpfs|overlay|squashfs|nsfs|proc|sysfs|cgroup2?",mountpoint!~"/var/lib/kubelet/pods/.*|/run/.*|/etc/(hostname|hosts|resolv[.]conf)"`
const overviewDiskFilter = `device=~"(sd|vd|xvd)[a-z]+|nvme[0-9]+n[0-9]+"`
const overviewNetFilter = `device=~"(eth[0-9]+|en[a-zA-Z0-9]+)"`

type overviewDevice struct {
	Name   string             `json:"name"`
	Device string             `json:"device"`
	Mount  string             `json:"mount,omitempty"`
	Values map[string]float64 `json:"values"`
}

func (s *Server) overviewNodeDetail(c *gin.Context) {
	name := c.Query("node")
	view := c.DefaultQuery("view", "compute")
	if name == "" || len(name) > 253 {
		abort(c, 400, errors.New("node is required"))
		return
	}
	if view != "compute" && view != "storage" && view != "network" {
		abort(c, 400, errors.New("invalid view"))
		return
	}
	snap, err := s.overviewSnapshot(c.Request.Context(), c.Query("snapshot"))
	if err != nil {
		abort(c, 409, err)
		return
	}
	var node *overviewNode
	for i := range snap.Nodes {
		if snap.Nodes[i].Name == name {
			node = &snap.Nodes[i]
			break
		}
	}
	if node == nil {
		abort(c, 404, errors.New("node not found"))
		return
	}
	s.cachedOverview(c, "detail:"+snap.ID+":"+view+":"+name, func(ctx context.Context) (any, error) {
		rows := map[string]*overviewDevice{}
		errs := []string{}
		truncated := false
		if view != "compute" && s.prom == nil {
			errs = append(errs, "Metrics are not configured.")
		}
		if view != "compute" && s.prom != nil {
			selector := "node=" + strconv.Quote(name)
			queries := map[string]string{}
			if view == "storage" {
				fs := "{" + selector + "," + overviewFSFilter + "}"
				// The same 100 lowest-space filesystems participate in every column.
				chosen := `bottomk(100,max by(node,device,mountpoint)(node_filesystem_avail_bytes` + fs + `) / max by(node,device,mountpoint)(node_filesystem_size_bytes` + fs + `))`
				for key, metric := range map[string]string{"size": "size_bytes", "free": "free_bytes", "available": "avail_bytes", "inodes": "files", "inodesFree": "files_free"} {
					queries[key] = `max by(node,device,mountpoint)(node_filesystem_` + metric + fs + `) and on(node,device,mountpoint) (` + chosen + `)`
				}
				dev := "{" + selector + "," + overviewDiskFilter + "}"
				for key, metric := range map[string]string{"readIOPS": "reads_completed_total", "writeIOPS": "writes_completed_total", "readBytes": "read_bytes_total", "writeBytes": "written_bytes_total", "readSeconds": "read_time_seconds_total", "writeSeconds": "write_time_seconds_total"} {
					queries[key] = `topk(100,max by(node,device)(rate(node_disk_` + metric + dev + `[2m])))`
				}
			} else {
				dev := "{" + selector + "," + overviewNetFilter + "}"
				for key, metric := range map[string]string{"receiveBytes": "receive_bytes_total", "transmitBytes": "transmit_bytes_total", "receiveErrors": "receive_errs_total", "transmitErrors": "transmit_errs_total", "receiveDrops": "receive_drop_total", "transmitDrops": "transmit_drop_total"} {
					queries[key] = `topk(100,max by(node,device)(rate(node_network_` + metric + dev + `[2m])))`
				}
			}
			keys := []string{}
			for k := range queries {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, key := range keys {
				samples, e := s.prom.Query(ctx, queries[key])
				if e != nil {
					errs = append(errs, key+" unavailable")
					continue
				}
				if len(samples) >= 100 {
					truncated = true
				}
				for _, v := range samples {
					id := v.Labels["device"]
					if mount := v.Labels["mountpoint"]; mount != "" {
						id = mount + " (" + id + ")"
					}
					r := rows[id]
					if r == nil {
						if len(rows) >= 200 {
							truncated = true
							continue
						}
						r = &overviewDevice{Name: id, Device: v.Labels["device"], Mount: v.Labels["mountpoint"], Values: map[string]float64{}}
						rows[id] = r
					}
					r.Values[key] = v.Value
				}
			}
		}
		devices := []overviewDevice{}
		for _, v := range rows {
			devices = append(devices, *v)
		}
		sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
		return gin.H{"node": node, "devices": devices, "errors": errs, "truncated": truncated, "updatedAt": time.Now().UTC(), "snapshotAt": snap.At}, nil
	})
}
func (s *Server) overviewNodePods(c *gin.Context) {
	name := c.Query("node")
	if name == "" || len(name) > 253 {
		abort(c, 400, errors.New("node is required"))
		return
	}
	limit, err := boundedInt(c.DefaultQuery("limit", "50"), 1, 100)
	if err != nil {
		abort(c, 400, err)
		return
	}
	selector := fields.OneTermEqualSelector("spec.nodeName", name)
	phase := c.Query("phase")
	if phase != "" {
		switch phase {
		case "Running", "Pending", "Succeeded", "Failed", "Unknown":
		default:
			abort(c, 400, errors.New("invalid phase"))
			return
		}
		selector = fields.AndSelectors(selector, fields.OneTermEqualSelector("status.phase", phase))
	}
	token := c.Query("continue")
	if len(token) > 8192 {
		abort(c, 400, errors.New("invalid cursor"))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	list, err := s.kube.Kube.CoreV1().Pods(c.Query("namespace")).List(ctx, metav1.ListOptions{FieldSelector: selector.String(), Limit: int64(limit), Continue: token})
	if err != nil {
		abort(c, http.StatusBadGateway, fmt.Errorf("node pods: %w", err))
		return
	}
	pods := []gin.H{}
	for _, p := range list.Items {
		if len(pods) == limit {
			break
		}
		pods = append(pods, gin.H{"name": p.Name, "namespace": p.Namespace, "phase": p.Status.Phase, "createdAt": p.CreationTimestamp})
	}
	c.JSON(200, gin.H{"pods": pods, "continue": list.Continue})
}

// Device histories are always scoped to exactly one node and one observed
// device/mount. They never expand a pool into per-device time series.
func overviewDeviceQuery(node, device, mount, view, metric string) string {
	sel := `{node=` + strconv.Quote(node) + `,device=` + strconv.Quote(device)
	if mount != "" {
		sel += `,mountpoint=` + strconv.Quote(mount)
	}
	sel += `}`
	if mount != "" {
		if metric == "available" {
			return `max by(device)(node_filesystem_avail_bytes` + sel + `)`
		}
		return `max by(device)(node_filesystem_size_bytes` + sel + `) - max by(device)(node_filesystem_free_bytes` + sel + `)`
	}
	prefix := "node_disk_"
	suffix := map[string]string{"readIOPS": "reads_completed_total", "writeIOPS": "writes_completed_total", "readBytes": "read_bytes_total", "writeBytes": "written_bytes_total"}[metric]
	if view == "network" {
		prefix = "node_network_"
		suffix = map[string]string{"receiveBytes": "receive_bytes_total", "transmitBytes": "transmit_bytes_total"}[metric]
	}
	if metric == "readLatency" || metric == "writeLatency" {
		direction, ops := "read", "reads"
		if metric == "writeLatency" {
			direction, ops = "write", "writes"
		}
		return `1000 * max by(device)(rate(node_disk_` + direction + `_time_seconds_total` + sel + `[2m])) / max by(device)(rate(node_disk_` + ops + `_completed_total` + sel + `[2m]))`
	}
	return `max by(device)(rate(` + prefix + suffix + sel + `[2m]))`
}
