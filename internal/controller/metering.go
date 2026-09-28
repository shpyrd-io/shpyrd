package controller

import (
	"context"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/prom"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// MeteringLoop scrapes Prometheus every five minutes for all project
// namespaces and writes usage_buckets into the control-plane store
// (RFC-0075). It runs on the leader only (NeedLeaderElection returns true)
// alongside the workspace and registry GC runnables.
//
// The namespace → (workspace, project) mapping comes from the Kubernetes
// API (the controller labels every project namespace), not from
// kube-state-metrics: KSM only exports labels that are on its allowlist,
// and depending on that made the loop a silent no-op on clusters where the
// allowlist did not include namespace labels.
type MeteringLoop struct {
	Store store.Store
	Prom  *prom.Client
	// Client reads namespaces. When nil (tests), the mapping falls back to
	// kube_namespace_labels in Prometheus.
	Client client.Client
}

func (m *MeteringLoop) NeedLeaderElection() bool { return true }

// bucketInterval is the metering granularity.
const bucketInterval = 5 * time.Minute

// bucketDelay is how long after a window closes before querying it (to
// absorb late Prometheus scrapes).
const bucketDelay = 2 * time.Minute

// catchupWindow is how far back the catch-up pass looks (Prometheus
// default retention is 7 days).
const catchupWindow = 7 * 24 * time.Hour

// rollupKeep is how much five-minute history stays in usage_buckets; older
// hours fold into usage_hourly (RFC-0075). Two days keeps the project usage
// graphs sharp and bounds the table: a cluster with 100 projects and six
// metrics writes ~170k rows a day at five minutes, ~14k at one hour.
const rollupKeep = 48 * time.Hour

// rollupEvery is how often the rollup runs.
const rollupEvery = 6 * time.Hour

// Start runs the metering loop until ctx is cancelled.
func (m *MeteringLoop) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("metering")
	if m.Prom == nil {
		logger.Info("no Prometheus client; metering disabled")
		return nil
	}
	logger.Info("metering loop started")
	// On startup, run a catch-up pass for any gaps in the last 7 days.
	go func() {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		m.catchup(cctx)
	}()
	// Roll up closed hours: once the catch-up has had time to land, then
	// periodically. The catch-up writes five-minute rows for the last 7 days
	// and the rollup folds all but the last 48 hours of them.
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Minute):
		}
		t := time.NewTicker(rollupEvery)
		defer t.Stop()
		for {
			if n, err := m.Store.RollupHourly(ctx, time.Now().Add(-rollupKeep)); err != nil {
				logger.Error(err, "hourly rollup failed")
			} else if n > 0 {
				logger.Info("hourly rollup", "hours", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	// Close the first bucket 2 min after the next 5-min boundary.
	next := nextBoundary(time.Now()).Add(bucketDelay)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Until(next)):
			t := next.Add(-bucketDelay)
			end := t.Truncate(bucketInterval)
			start := end.Add(-bucketInterval)
			n, err := m.writeBuckets(ctx, start, end)
			if err != nil {
				logger.Error(err, "bucket write failed", "start", start)
			} else {
				logger.V(1).Info("bucket closed", "start", start, "buckets", n)
			}
			next = next.Add(bucketInterval)
		}
	}
}

// catchup writes missing buckets for the last 7 days. Writes are
// idempotent (ON CONFLICT DO NOTHING on the bucket key), so re-running is
// safe after a restart.
func (m *MeteringLoop) catchup(ctx context.Context) {
	logger := log.FromContext(ctx).WithName("metering-catchup")
	now := time.Now().UTC().Truncate(bucketInterval)
	start := now.Add(-catchupWindow)
	var written, windows, failed int
	for t := start; t.Before(now); t = t.Add(bucketInterval) {
		select {
		case <-ctx.Done():
			logger.Info("catch-up stopped early", "windows", windows, "buckets", written, "failed", failed)
			return
		default:
		}
		end := t.Add(bucketInterval)
		if now.Sub(end) < bucketDelay {
			continue // not old enough yet
		}
		n, err := m.writeBuckets(ctx, t, end)
		if err != nil {
			failed++
			if failed <= 3 {
				logger.Error(err, "catch-up bucket failed", "start", t)
			}
			continue
		}
		windows++
		written += n
	}
	logger.Info("catch-up finished", "windows", windows, "buckets", written, "failed", failed)
}

func nextBoundary(t time.Time) time.Time {
	return t.UTC().Truncate(bucketInterval).Add(bucketInterval)
}

// nsMapping resolves namespace → {workspace slug, project slug} for every
// project namespace. Reads the Kubernetes API when a client is available;
// otherwise falls back to kube_namespace_labels in Prometheus.
func (m *MeteringLoop) nsMapping(ctx context.Context) (map[string][2]string, error) {
	out := map[string][2]string{}
	if m.Client != nil {
		var list corev1.NamespaceList
		if err := m.Client.List(ctx, &list, client.HasLabels{shpyrdv1.LabelProject}); err != nil {
			return nil, err
		}
		for _, ns := range list.Items {
			if ns.DeletionTimestamp != nil {
				continue
			}
			ws := ns.Labels[shpyrdv1.LabelWorkspace]
			proj := ns.Labels[shpyrdv1.LabelProject]
			if ws == "" || proj == "" {
				continue
			}
			out[ns.Name] = [2]string{ws, proj}
		}
		return out, nil
	}
	series, err := m.Prom.QueryInstantSeries(ctx, `kube_namespace_labels{label_shpyrd_io_workspace!=""}`)
	if err != nil {
		return nil, err
	}
	for _, s := range series {
		ns := s.Labels["namespace"]
		ws := s.Labels["label_shpyrd_io_workspace"]
		proj := s.Labels["label_shpyrd_io_project"]
		if ns == "" || ws == "" || proj == "" {
			continue
		}
		out[ns] = [2]string{ws, proj}
	}
	return out, nil
}

// writeBuckets closes one 5-minute bucket [start, end) for all projects and
// returns how many buckets it handed to the store.
func (m *MeteringLoop) writeBuckets(ctx context.Context, start, end time.Time) (int, error) {
	nsMap, err := m.nsMapping(ctx)
	if err != nil {
		return 0, fmt.Errorf("namespace mapping: %w", err)
	}
	if len(nsMap) == 0 {
		return 0, nil // nothing to meter yet
	}
	var buckets []store.UsageBucket
	buckets = append(buckets, m.cpuBuckets(ctx, start, end, nsMap)...)
	buckets = append(buckets, m.memoryBuckets(ctx, start, end, nsMap)...)
	buckets = append(buckets, m.storageBuckets(ctx, start, end, nsMap)...)
	buckets = append(buckets, m.egressBuckets(ctx, start, end, nsMap)...)
	if len(buckets) == 0 {
		return 0, nil
	}
	if err := m.Store.WriteBuckets(ctx, buckets); err != nil {
		return 0, err
	}
	return len(buckets), nil
}

// Component attribution (RFC-0075). Every metered thing is named after what
// the customer created: a process type (web, worker, release), a database
// (postgres/<name>), a store (redis/<name>), a volume (volume/<name>), or the
// build machinery (build for the build pods, build-cache for the buildpack
// cache claim). The labels come through kube-state-metrics' allowlist
// (deploy/components/monitoring/values.yaml); anything unlabelled is "other".
//
// componentOfPod names the component of a pod from its (KSM-exported) labels.
func componentOfPod(labels map[string]string, _ string) string {
	switch {
	case labels["label_shpyrd_io_process"] != "":
		return labels["label_shpyrd_io_process"]
	case labels["label_cnpg_io_cluster"] != "":
		return "postgres/" + labels["label_cnpg_io_cluster"]
	case labels["label_shpyrd_io_redis"] != "":
		return "redis/" + labels["label_shpyrd_io_redis"]
	case labels["label_kpack_io_build"] != "":
		return "build"
	}
	return "other"
}

// componentOfClaim names the component of a PersistentVolumeClaim. The
// buildpack cache is recognised by kpack's naming (<image>-cache, the Image
// being named after the project) because kpack creates that claim itself.
func componentOfClaim(labels map[string]string, project string) string {
	switch {
	case labels["label_cnpg_io_cluster"] != "":
		return "postgres/" + labels["label_cnpg_io_cluster"]
	case labels["label_shpyrd_io_volume"] != "":
		return "volume/" + labels["label_shpyrd_io_volume"]
	case labels["label_shpyrd_io_redis"] != "":
		return "redis/" + labels["label_shpyrd_io_redis"]
	case labels["persistentvolumeclaim"] == project+"-cache":
		return "build-cache"
	case labels["persistentvolumeclaim"] != "":
		return "other/" + labels["persistentvolumeclaim"]
	}
	return "other"
}

// podLabels are the pod labels the queries group by; podJoin attaches them
// to container metrics. Every pod is joined (kube_pod_labels exists for all
// of them), so unlabelled pods stay in the sum instead of being dropped.
const (
	podLabels = "label_shpyrd_io_process, label_cnpg_io_cluster, label_shpyrd_io_redis, label_kpack_io_build"
	podJoin   = "* on (namespace, pod) group_left (" + podLabels + ") kube_pod_labels{}"
)

// cpuBuckets writes cpu_used (core-seconds) per (namespace, component).
func (m *MeteringLoop) cpuBuckets(ctx context.Context, start, end time.Time, nsMap map[string][2]string) []store.UsageBucket {
	dur := end.Sub(start).Seconds()
	q := fmt.Sprintf(
		`sum by (namespace, `+podLabels+`) (`+
			`increase(container_cpu_usage_seconds_total{container!="",container!="POD",namespace=~"app-.*"}[%ds]) `+
			podJoin+`)`,
		int(dur)+30,
	)
	return m.queryBuckets(ctx, start, end, q, componentOfPod, store.MetricCPUUsed, store.UnitCoreSeconds, nsMap, false)
}

// memoryBuckets writes memory_used (GiB-seconds) per (namespace, component).
func (m *MeteringLoop) memoryBuckets(ctx context.Context, start, end time.Time, nsMap map[string][2]string) []store.UsageBucket {
	dur := end.Sub(start).Seconds()
	q := fmt.Sprintf(
		`sum by (namespace, `+podLabels+`) (`+
			`avg_over_time(container_memory_working_set_bytes{container!="",container!="POD",namespace=~"app-.*"}[%ds]) `+
			podJoin+`) * %.6f / (1024*1024*1024)`,
		int(dur)+30, dur, // avg × duration = GiB-seconds
	)
	return m.queryBuckets(ctx, start, end, q, componentOfPod, store.MetricMemoryUsed, store.UnitGiBSeconds, nsMap, false)
}

// storageBuckets writes storage (GiB-seconds) per (namespace, claim
// component). The quantity is the capacity of the Bound PersistentVolume
// behind each claim — what was provisioned and what the provider bills —
// not the claim's request: a 2 Gi request on OCI yields a 50 Gi volume,
// and the customer pays for 50. A claim without a volume costs nothing.
// The PV's claimRef gives the claim's namespace and name.
//
// Claim labels come from a second query and are merged here: KSM emits
// kube_persistentvolumeclaim_labels only when claims are on its allowlist,
// and a multiplicative join would silently drop every claim on a cluster
// whose monitoring predates that.
func (m *MeteringLoop) storageBuckets(ctx context.Context, start, end time.Time, nsMap map[string][2]string) []store.UsageBucket {
	dur := end.Sub(start).Seconds()
	q := fmt.Sprintf(
		`sum by (namespace, persistentvolumeclaim) (label_replace(label_replace(`+
			`(kube_persistentvolume_capacity_bytes and on (persistentvolume) (kube_persistentvolume_status_phase{phase="Bound"} == 1)) `+
			`* on (persistentvolume) group_left (claim_namespace, name) kube_persistentvolume_claim_ref, `+
			`"namespace", "$1", "claim_namespace", "(.*)"), "persistentvolumeclaim", "$1", "name", "(.*)")`+
			`) * %.6f / (1024*1024*1024)`,
		dur,
	)
	claimLabels := map[string]map[string]string{}
	if series, err := m.Prom.QueryInstantSeriesAt(ctx, `kube_persistentvolumeclaim_labels{namespace=~"app-.*"}`, end); err == nil {
		for _, s := range series {
			claimLabels[s.Labels["namespace"]+"/"+s.Labels["persistentvolumeclaim"]] = s.Labels
		}
	}
	component := func(labels map[string]string, project string) string {
		merged := map[string]string{"persistentvolumeclaim": labels["persistentvolumeclaim"]}
		for k, v := range claimLabels[labels["namespace"]+"/"+labels["persistentvolumeclaim"]] {
			merged[k] = v
		}
		return componentOfClaim(merged, project)
	}
	return m.queryBuckets(ctx, start, end, q, component, store.MetricStorage, store.UnitGiBSeconds, nsMap, true)
}

// egressBuckets writes egress_http (bytes) per namespace from the front door.
func (m *MeteringLoop) egressBuckets(ctx context.Context, start, end time.Time, nsMap map[string][2]string) []store.UsageBucket {
	dur := end.Sub(start).Seconds()
	q := fmt.Sprintf(
		`sum by (exported_namespace) (`+
			`increase(nginx_ingress_controller_response_size_sum{exported_namespace!=""}[%ds]))`,
		int(dur)+30,
	)
	// exported_namespace is the Ingress's namespace (the controller's own is "namespace").
	series, err := m.Prom.QueryInstantSeriesAt(ctx, q, end)
	if err != nil {
		return m.missingBuckets(start, end, nsMap, store.MetricEgressHTTP, store.UnitBytes)
	}
	var out []store.UsageBucket
	for _, s := range series {
		ns := s.Labels["exported_namespace"]
		meta, ok := nsMap[ns]
		if !ok || s.Value <= 0 {
			continue
		}
		qty := s.Value
		out = append(out, store.UsageBucket{
			WorkspaceID: meta[0], Project: meta[1], Component: "web",
			Metric: store.MetricEgressHTTP, PeriodStart: start, PeriodEnd: end,
			Quantity: &qty, Unit: store.UnitBytes, Quality: store.QualityComplete, Revision: 1, Source: "prom_v1",
		})
	}
	return out
}

// queryBuckets runs an instant query at the bucket's end time and maps
// each series to a bucket: the namespace label names the project, component
// names the component from the series' labels and the project. With
// zeroFill, projects without a series get an explicit zero bucket
// (complete, not missing) so a month with no storage reads as zero rather
// than as a gap.
func (m *MeteringLoop) queryBuckets(ctx context.Context, start, end time.Time,
	q string, component func(labels map[string]string, project string) string, metric, unit string, nsMap map[string][2]string, zeroFill bool) []store.UsageBucket {
	series, err := m.Prom.QueryInstantSeriesAt(ctx, q, end)
	if err != nil {
		return m.missingBuckets(start, end, nsMap, metric, unit)
	}
	type key struct{ ws, project, comp string }
	sums := map[key]float64{}
	for _, s := range series {
		meta, ok := nsMap[s.Labels["namespace"]]
		if !ok || s.Value <= 0 {
			continue
		}
		sums[key{meta[0], meta[1], component(s.Labels, meta[1])}] += s.Value
	}
	var out []store.UsageBucket
	for k, v := range sums {
		qty := v
		out = append(out, store.UsageBucket{
			WorkspaceID: k.ws, Project: k.project, Component: k.comp,
			Metric: metric, PeriodStart: start, PeriodEnd: end,
			Quantity: &qty, Unit: unit, Quality: store.QualityComplete, Revision: 1, Source: "prom_v1",
		})
	}
	if zeroFill {
		seen := map[string]bool{}
		for k := range sums {
			seen[k.ws+"/"+k.project] = true
		}
		for _, meta := range nsMap {
			if seen[meta[0]+"/"+meta[1]] {
				continue
			}
			zero := 0.0
			out = append(out, store.UsageBucket{
				WorkspaceID: meta[0], Project: meta[1], Component: "other",
				Metric: metric, PeriodStart: start, PeriodEnd: end,
				Quantity: &zero, Unit: unit, Quality: store.QualityComplete, Revision: 1, Source: "prom_v1",
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].WorkspaceID+out[i].Project+out[i].Component < out[j].WorkspaceID+out[j].Project+out[j].Component
	})
	return out
}

// missingBuckets returns quality=missing rows for all known projects.
func (m *MeteringLoop) missingBuckets(start, end time.Time, nsMap map[string][2]string, metric, unit string) []store.UsageBucket {
	var out []store.UsageBucket
	for _, meta := range nsMap {
		out = append(out, store.UsageBucket{
			WorkspaceID: meta[0], Project: meta[1], Component: "default",
			Metric: metric, PeriodStart: start, PeriodEnd: end,
			Quantity: nil, Unit: unit, Quality: store.QualityMissing, Revision: 1, Source: "prom_v1",
		})
	}
	return out
}
