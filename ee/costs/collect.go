//go:build !foss

package costs

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/shpyrd-io/shpyrd/ee/licensing"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The collector writes cost lines: every hour the usage the metering loop
// measured and OpenCost's estimate of the hours just closed; once a day
// the provider's bill of the days just past, which settles late, so the
// last few are read again. Lines are keyed by what they are about, so a
// re-read changes only what changed. It runs on the leader, and only with
// a license in force.

// Collector is the costs loop.
type Collector struct {
	Store        store.Store
	Kube         kubernetes.Interface
	Namespace    string // the system namespace: the OCI credentials' Secret
	OpenCostURL  string
	HTTP         *http.Client
	Logger       *slog.Logger
	Every        time.Duration // how often it wakes; an hour by default
	HoursBack    int           // closed hours read again each time; 3 by default
	DaysBack     int           // closed days of the bill read again; 3 by default
	now          func() time.Time
	lastBillDay  time.Time
	ociEndpoints ociEndpoints
}

func (c *Collector) NeedLeaderElection() bool { return true }

func (c *Collector) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// Start runs until ctx ends.
func (c *Collector) Start(ctx context.Context) error {
	every := c.Every
	if every <= 0 {
		every = time.Hour
	}
	// A little after the hour, when OpenCost has the hour closed.
	first := time.Until(c.clock().Truncate(time.Hour).Add(time.Hour + 5*time.Minute))
	timer := time.NewTimer(min(first, every))
	defer timer.Stop()
	c.Collect(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			c.Collect(ctx)
			timer.Reset(every)
		}
	}
}

// Collect is one pass; each part logs its own trouble and the others go on.
func (c *Collector) Collect(ctx context.Context) {
	if !licensing.Active() {
		return
	}
	log := c.Logger
	if log == nil {
		log = slog.Default()
	}
	now := c.clock().UTC()
	hours := c.HoursBack
	if hours <= 0 {
		hours = 3
	}
	wsIDs := c.workspaceIDs(ctx)
	for i := hours; i >= 1; i-- {
		start := now.Truncate(time.Hour).Add(-time.Duration(i) * time.Hour)
		end := start.Add(time.Hour)
		if err := c.collectUsage(ctx, start, end); err != nil {
			log.Warn("costs: usage", "hour", start, "error", err)
		}
		if err := c.collectEstimate(ctx, start, end, wsIDs); err != nil {
			log.Warn("costs: estimate", "hour", start, "error", err)
		}
	}
	day := now.Truncate(24 * time.Hour)
	if !c.lastBillDay.Equal(day) {
		switch err := c.collectBill(ctx, day); {
		case err == errNoCredentials:
		case err != nil:
			log.Warn("costs: the provider's bill", "error", err)
		default:
			c.lastBillDay = day
		}
	}
}

// workspaceIDs maps the short ids the labels carry to the store's ids, the
// form every line uses.
func (c *Collector) workspaceIDs(ctx context.Context) map[string]string {
	out := map[string]string{}
	all, err := c.Store.ListWorkspaces(ctx)
	if err != nil {
		return out
	}
	for _, w := range all {
		out[ids.Short(w.ID)] = w.ID
	}
	return out
}

// collectUsage sums the metering loop's buckets of an hour into lines, per
// workspace, project, component and metric.
func (c *Collector) collectUsage(ctx context.Context, start, end time.Time) error {
	all, err := c.Store.ListWorkspaces(ctx)
	if err != nil {
		return err
	}
	type key struct{ ws, project, component, metric, unit string }
	sums := map[key]float64{}
	for _, w := range all {
		buckets, err := c.Store.QueryBuckets(ctx, w.Slug, "", start, end)
		if err != nil {
			return err
		}
		for _, b := range buckets {
			if b.Quantity == nil || b.PeriodStart.Before(start) || !b.PeriodStart.Before(end) {
				continue
			}
			sums[key{w.ID, b.Project, b.Component, b.Metric, b.Unit}] += *b.Quantity
		}
	}
	keys := make([]key, 0, len(sums))
	for k := range sums {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].project+keys[i].component+keys[i].metric < keys[j].project+keys[j].component+keys[j].metric
	})
	lines := make([]store.CostLine, 0, len(keys))
	for _, k := range keys {
		q := sums[k]
		lines = append(lines, store.CostLine{Kind: store.CostUsage, Source: "metering", Start: start, End: end, Workspace: k.ws, Project: k.project, Process: k.component, Metric: k.metric, Quantity: &q, Unit: k.unit})
	}
	_, err = c.Store.UpsertCostLines(ctx, lines)
	return err
}

// collectEstimate reads OpenCost's allocation of an hour into lines.
func (c *Collector) collectEstimate(ctx context.Context, start, end time.Time, wsIDs map[string]string) error {
	if c.OpenCostURL == "" {
		return nil
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	data, err := fetchAllocation(ctx, client, c.OpenCostURL, start, end)
	if err != nil {
		return err
	}
	lines := estimateLines(data, start, end, c.resources(ctx))
	for i := range lines {
		if id := wsIDs[lines[i].Workspace]; id != "" {
			lines[i].Workspace = id
		}
	}
	_, err = c.Store.UpsertCostLines(ctx, lines)
	return err
}

// resources reads the cluster's nodes and volumes for the names the
// provider bills them by.
func (c *Collector) resources(ctx context.Context) resources {
	r := resources{nodes: map[string]string{}, volumes: map[string]string{}}
	if c.Kube == nil {
		return r
	}
	if nodes, err := c.Kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); err == nil {
		for _, n := range nodes.Items {
			r.nodes[n.Name] = n.Spec.ProviderID
		}
	}
	if pvs, err := c.Kube.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{}); err == nil {
		for _, pv := range pvs.Items {
			r.volumes[pv.Name] = volumeHandle(&pv)
		}
	}
	return r
}

func volumeHandle(pv *corev1.PersistentVolume) string {
	if pv.Spec.CSI != nil {
		return pv.Spec.CSI.VolumeHandle
	}
	return ""
}
