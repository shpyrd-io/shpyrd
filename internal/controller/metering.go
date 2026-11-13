package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
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
	// SleepAllowed and WorkspaceSleepDefault are the Postgres reconciler's
	// own (storage plan step 6): the activity signal is kept for every
	// database whose effective policy could put it to sleep, the
	// workspace's default included, not only for one with a policy of its
	// own. Nil: databases with a policy of their own only.
	SleepAllowed          func() bool
	WorkspaceSleepDefault func(ctx context.Context, namespace string) string
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
			// Postgres sleep (RFC-0075): the activity signal for every
			// database with a policy, on the same cadence.
			if err := m.postgresActivity(ctx, end); err != nil {
				logger.Error(err, "postgres activity check failed")
			}
			if err := m.dedicatedSizes(ctx, end); err != nil {
				logger.Error(err, "dedicated database sizes")
			}
			if err := m.sharedCaps(ctx, end); err != nil {
				logger.Error(err, "shared database caps")
			}
			if err := m.sharedServerDisks(ctx, end); err != nil {
				logger.Error(err, "shared server disks")
			}
			if err := m.sharedServerPoolers(ctx, end); err != nil {
				logger.Error(err, "shared server poolers")
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

// nsMapping resolves namespace → {workspace key, project key} for every
// project namespace: the ledger's keys (RFC-0076). The workspace key is the
// workspace's UUID and the project key the project's short id, read off the
// identity labels; a namespace the controller has not stamped yet (a legacy
// project seen before its first reconcile) maps by its slug labels, which
// the store re-keys once the id arrives. Reads the Kubernetes API when a
// client is available; otherwise falls back to kube_namespace_labels.
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
			if ws, proj, ok := ledgerKeys(ns.Labels); ok {
				out[ns.Name] = [2]string{ws, proj}
			}
		}
		return out, nil
	}
	series, err := m.Prom.QueryInstantSeries(ctx, `kube_namespace_labels{label_shpyrd_io_workspace!=""}`)
	if err != nil {
		return nil, err
	}
	for _, s := range series {
		ns := s.Labels["namespace"]
		labels := map[string]string{
			shpyrdv1.LabelWorkspace:   s.Labels["label_shpyrd_io_workspace"],
			shpyrdv1.LabelProject:     s.Labels["label_shpyrd_io_project"],
			shpyrdv1.LabelWorkspaceID: s.Labels["label_shpyrd_io_workspace_id"],
			shpyrdv1.LabelProjectID:   s.Labels["label_shpyrd_io_project_id"],
		}
		if ws, proj, ok := ledgerKeys(labels); ok && ns != "" {
			out[ns] = [2]string{ws, proj}
		}
	}
	return out, nil
}

// ledgerKeys picks the ledger's workspace and project keys off a project
// namespace's labels: ids when present (the workspace's as its UUID, the
// project's in its short form), slugs otherwise.
func ledgerKeys(labels map[string]string) (ws, proj string, ok bool) {
	ws, proj = labels[shpyrdv1.LabelWorkspace], labels[shpyrdv1.LabelProject]
	if short := labels[shpyrdv1.LabelWorkspaceID]; short != "" {
		if id, err := ids.Decode(short); err == nil {
			ws = id
		}
	}
	if short := labels[shpyrdv1.LabelProjectID]; short != "" {
		proj = short
	}
	return ws, proj, ws != "" && proj != ""
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
	buckets = append(buckets, m.sharedBuckets(ctx, start, end, nsMap)...)
	buckets = append(buckets, m.bucketBuckets(ctx, start, end, nsMap)...)
	buckets = append(buckets, m.egressBuckets(ctx, start, end, nsMap)...)
	if len(buckets) == 0 {
		return 0, nil
	}
	if err := m.Store.WriteBuckets(ctx, buckets); err != nil {
		return 0, err
	}
	return len(buckets), nil
}

// postgresActivity is the sleep activity signal (RFC-0075, section 5):
// for every Postgres with a sleep policy it asks Prometheus how many client
// backends the database has (cnpg_backends_total, excluding CNPG's own
// postgres and streaming_replica sessions — idle application connections
// count, by design: a connection pool keeps its database awake) and records
// the check time plus, when clients were seen, the activity time. The
// reconciler hibernates only when the check is fresh and the activity old.
func (m *MeteringLoop) postgresActivity(ctx context.Context, at time.Time) error {
	if m.Client == nil {
		return nil
	}
	var list shpyrdv1.PostgresList
	if err := m.Client.List(ctx, &list); err != nil {
		return err
	}
	var withPolicy []shpyrdv1.Postgres
	for _, pg := range list.Items {
		if m.sleepApplies(ctx, &pg) {
			withPolicy = append(withPolicy, pg)
		}
	}
	if len(withPolicy) == 0 {
		return nil
	}
	// Two queries for all of them. CNPG's PodMonitor labels each database's
	// series job="<namespace>/<name>". The exporter's own session
	// (cnpg_metrics_exporter) proves the database is scraped — that is the
	// "answered" set; client sessions are everything but CNPG's own users.
	// A database that is scraped but has no client series has zero clients.
	scraped, err := m.Prom.QueryInstantSeriesAt(ctx,
		`max by (job) (max_over_time(cnpg_backends_total[5m]))`, at)
	if err != nil {
		return fmt.Errorf("cnpg backends: %w", err)
	}
	clients, err := m.Prom.QueryInstantSeriesAt(ctx,
		`max by (job) (max_over_time(cnpg_backends_total{usename!~"postgres|streaming_replica|cnpg_metrics_exporter"}[5m]))`, at)
	if err != nil {
		return fmt.Errorf("cnpg client backends: %w", err)
	}
	answered := map[string]bool{} // "<ns>/<name>" scraped in the window
	seen := map[string]bool{}     // "<ns>/<name>" with ≥1 client backend
	for _, s := range scraped {
		answered[s.Labels["job"]] = true
	}
	for _, s := range clients {
		if s.Value > 0 {
			seen[s.Labels["job"]] = true
		}
	}
	now := metav1.NewTime(at)
	for i := range withPolicy {
		pg := &withPolicy[i]
		key := pg.Namespace + "/" + pg.Name
		if pg.Status.Sleep == nil {
			pg.Status.Sleep = &shpyrdv1.PostgresSleepStatus{}
		}
		// Only awake databases have backends to count; a sleeping one has
		// no pods and no series. Its check is not stale — it is asleep.
		if pg.Status.Sleep.State != "" && pg.Status.Sleep.State != "awake" {
			continue
		}
		if !answered[key] {
			// No series at all: the PodMonitor is not scraped yet (a new
			// database, or metrics still propagating). Not a fresh check.
			continue
		}
		patch := client.MergeFrom(pg.DeepCopy())
		pg.Status.Sleep.ActivityCheckedAt = &now
		if seen[key] {
			pg.Status.Sleep.LastActivityAt = &now
		}
		if err := m.Client.Status().Patch(ctx, pg, patch); err != nil {
			log.FromContext(ctx).Error(err, "postgres activity patch failed", "postgres", key)
		}
	}
	return nil
}

// sleepApplies says the database could be put to sleep by a policy, so its
// activity must be known: a quiet period of its own, or none of its own and
// the workspace's default while databases may sleep by themselves. An
// explicit "off" opts out of the default (#135); a shared database and a
// high-availability one never sleep (RFC-0075). Before this a database
// sleeping by the workspace's default never hibernated: the reconciler
// waited for an activity check that nobody wrote.
func (m *MeteringLoop) sleepApplies(ctx context.Context, pg *shpyrdv1.Postgres) bool {
	if pg.Spec.Type == shpyrdv1.PostgresTypeShared || instances(pg) > 1 {
		return false
	}
	if pg.Spec.Sleep != nil {
		return sleepAfterDuration(pg.Spec.Sleep) > 0
	}
	if m.SleepAllowed == nil || !m.SleepAllowed() || m.WorkspaceSleepDefault == nil {
		return false
	}
	return parseSleepDuration(m.WorkspaceSleepDefault(ctx, pg.Namespace)) > 0
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

// dedicatedCPU selects the containers whose CPU request equals their
// limit: a dedicated size (pkg/sizes, Guaranteed QoS), whose cores are held
// for it and billed as held (shpyrd-cloud#70). Every other container runs
// a shared size and is billed on use. Both metrics are written, each for
// its own set of containers, so a dedicated core is never billed twice.
const dedicatedCPU = `(kube_pod_container_resource_requests{resource="cpu",container!="",namespace=~"(app|p)-.*"} ` +
	`== on (namespace, pod, container) kube_pod_container_resource_limits{resource="cpu"})`

// cpuBuckets writes cpu_used (core-seconds) per (namespace, component) for
// the containers of shared sizes, and cpu_reserved (core-seconds) for the
// containers of dedicated sizes: their CPU requests, summed over the window
// in 30-second steps while the pod runs, as memoryBuckets does for memory.
func (m *MeteringLoop) cpuBuckets(ctx context.Context, start, end time.Time, nsMap map[string][2]string) []store.UsageBucket {
	dur := end.Sub(start).Seconds()
	used := fmt.Sprintf(
		`sum by (namespace, `+podLabels+`) (`+
			`(increase(container_cpu_usage_seconds_total{container!="",container!="POD",namespace=~"(app|p)-.*"}[%ds]) `+
			`unless on (namespace, pod, container) max_over_time(`+dedicatedCPU+`[%ds:30s])) `+
			podJoin+`)`,
		int(dur)+30, int(dur)+30,
	)
	out := m.queryBuckets(ctx, start, end, used, componentOfPod, store.MetricCPUUsed, store.UnitCoreSeconds, nsMap, false)
	reserved := fmt.Sprintf(
		`sum by (namespace, `+podLabels+`) (`+
			`sum_over_time((`+dedicatedCPU+` and on (namespace, pod) (kube_pod_status_phase{phase="Running"} == 1))[%ds:30s]) `+
			podJoin+`) * 30`,
		int(dur),
	)
	return append(out, m.queryBuckets(ctx, start, end, reserved, componentOfPod, store.MetricCPUReserved, store.UnitCoreSeconds, nsMap, false)...)
}

// memoryBuckets writes memory_reserved (GiB-seconds) per (namespace,
// component): the memory requests of the containers of running pods,
// summed over the window in 30-second steps, so an instance that lived
// for half the window counts for half. Memory is never overcommitted
// (request equals limit, pkg/sizes), so this is what the instance took
// from its node, and what the customer is billed for: a sleeping process
// has no pod and reserves nothing. Until the switch the ledger carried
// memory_used, the working set; old months keep it.
func (m *MeteringLoop) memoryBuckets(ctx context.Context, start, end time.Time, nsMap map[string][2]string) []store.UsageBucket {
	dur := end.Sub(start).Seconds()
	q := fmt.Sprintf(
		`sum by (namespace, `+podLabels+`) (`+
			`sum_over_time((kube_pod_container_resource_requests{resource="memory",container!="",namespace=~"(app|p)-.*"} `+
			`and on (namespace, pod) (kube_pod_status_phase{phase="Running"} == 1))[%ds:30s]) `+
			podJoin+`) * 30 / (1024*1024*1024)`,
		int(dur), // one sample per 30 s, each worth 30 s of the reservation
	)
	return m.queryBuckets(ctx, start, end, q, componentOfPod, store.MetricMemoryReserved, store.UnitGiBSeconds, nsMap, false)
}

// storageBuckets writes storage (GiB-seconds) per disk: one bucket per
// Bound PersistentVolume behind a claim in a project namespace, named by
// its claim's component (volume/<name>, postgres/<name>, redis/<name>,
// build-cache) and by the disk itself (Resource: the PersistentVolume's
// name), so a database's two instances are two disks and a disk replaced
// under the same name is another disk (shpyrd-cloud#70).
//
// The quantity is the size asked for, not the size provisioned: a 10 GiB
// volume on a provider whose disks start at 50 GiB is metered as 10 GiB
// (billing applies its own minimum per disk). What was asked comes from
// the object the claim belongs to (the Volume's size, the database's or
// store's storage); a claim of nothing shpyrd knows (the buildpack cache,
// an orphan) is metered as provisioned, and so is a request past the disk
// (an expansion under way). A claim without a volume costs nothing.
//
// Prometheus says which volumes were Bound at the window's end and how
// big (kube_persistentvolume_capacity_bytes, with the claim through
// kube_persistentvolume_claim_ref); the claims' labels come from the API
// when there is a client, else from kube_persistentvolumeclaim_labels
// (KSM emits those only when claims are on its allowlist). Neither is
// joined in PromQL: a multiplicative join would silently drop every claim
// on a cluster whose monitoring predates the allowlist.
func (m *MeteringLoop) storageBuckets(ctx context.Context, start, end time.Time, nsMap map[string][2]string) []store.UsageBucket {
	dur := end.Sub(start).Seconds()
	q := `sum by (namespace, persistentvolumeclaim, persistentvolume) (label_replace(label_replace(` +
		`(kube_persistentvolume_capacity_bytes and on (persistentvolume) (kube_persistentvolume_status_phase{phase="Bound"} == 1)) ` +
		`* on (persistentvolume) group_left (claim_namespace, name) kube_persistentvolume_claim_ref, ` +
		`"namespace", "$1", "claim_namespace", "(.*)"), "persistentvolumeclaim", "$1", "name", "(.*)"))`
	series, err := m.Prom.QueryInstantSeriesAt(ctx, q, end)
	if err != nil {
		return m.missingBuckets(start, end, nsMap, store.MetricStorage, store.UnitGiBSeconds)
	}
	claims := m.claims(ctx, nsMap)
	// A claim the API did not show (gone since, or no client): its labels
	// as kube-state-metrics has them, and the disk as provisioned.
	if labelled, err := m.Prom.QueryInstantSeriesAt(ctx, `kube_persistentvolumeclaim_labels{namespace=~"(app|p)-.*"}`, end); err == nil {
		for _, s := range labelled {
			if k := s.Labels["namespace"] + "/" + s.Labels["persistentvolumeclaim"]; claims[k].labels == nil {
				claims[k] = claim{labels: s.Labels}
			}
		}
	}
	type key struct{ ws, project, comp, disk string }
	sums := map[key]float64{}
	for _, s := range series {
		meta, ok := nsMap[s.Labels["namespace"]]
		if !ok || s.Value <= 0 {
			continue
		}
		c := claims[s.Labels["namespace"]+"/"+s.Labels["persistentvolumeclaim"]]
		labels := map[string]string{"persistentvolumeclaim": s.Labels["persistentvolumeclaim"]}
		for k, v := range c.labels {
			labels[k] = v
		}
		bytes := s.Value
		if c.requested != nil && c.requested.AsApproximateFloat64() < bytes {
			bytes = c.requested.AsApproximateFloat64()
		}
		sums[key{meta[0], meta[1], componentOfClaim(labels, meta[1]), s.Labels["persistentvolume"]}] += bytes * dur / (1024 * 1024 * 1024)
	}
	var out []store.UsageBucket
	seen := map[string]bool{}
	for k, v := range sums {
		qty := v
		seen[k.ws+"/"+k.project] = true
		out = append(out, store.UsageBucket{
			WorkspaceID: k.ws, Project: k.project, Component: k.comp, Resource: k.disk,
			Metric: store.MetricStorage, PeriodStart: start, PeriodEnd: end,
			Quantity: &qty, Unit: store.UnitGiBSeconds, Quality: store.QualityComplete, Revision: 1, Source: "prom_v1",
		})
	}
	// A project without a disk gets an explicit zero (complete, not
	// missing), so a month with no storage reads as zero rather than as a gap.
	for _, meta := range nsMap {
		if seen[meta[0]+"/"+meta[1]] {
			continue
		}
		zero := 0.0
		out = append(out, store.UsageBucket{
			WorkspaceID: meta[0], Project: meta[1], Component: "other",
			Metric: store.MetricStorage, PeriodStart: start, PeriodEnd: end,
			Quantity: &zero, Unit: store.UnitGiBSeconds, Quality: store.QualityComplete, Revision: 1, Source: "prom_v1",
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].WorkspaceID+out[i].Project+out[i].Component+out[i].Resource < out[j].WorkspaceID+out[j].Project+out[j].Component+out[j].Resource
	})
	return out
}

// claim is what the metering knows of a PersistentVolumeClaim: its labels
// (as kube-state-metrics would spell them, label_<key with _>) and the size
// the owner asked for, nil when nothing shpyrd knows owns it.
type claim struct {
	labels    map[string]string
	requested *resource.Quantity
}

// claims reads the claims of the project namespaces and what their owners
// asked for: a Volume's size, a database's or a store's storage (the
// controller's default when the spec says nothing). Nothing without a
// client.
func (m *MeteringLoop) claims(ctx context.Context, nsMap map[string][2]string) map[string]claim {
	out := map[string]claim{}
	if m.Client == nil {
		return out
	}
	var pvcs corev1.PersistentVolumeClaimList
	if err := m.Client.List(ctx, &pvcs); err != nil {
		log.FromContext(ctx).Error(err, "claims for metering")
		return out
	}
	volumes, databases, stores := map[string]resource.Quantity{}, map[string]resource.Quantity{}, map[string]resource.Quantity{}
	var vols shpyrdv1.VolumeList
	if err := m.Client.List(ctx, &vols); err == nil {
		for _, v := range vols.Items {
			volumes[v.Namespace+"/"+v.Name] = v.Spec.Size
		}
	}
	var pgs shpyrdv1.PostgresList
	if err := m.Client.List(ctx, &pgs); err == nil {
		for _, pg := range pgs.Items {
			size := resource.MustParse(defaultPostgresStorage)
			if pg.Spec.Storage != nil {
				size = *pg.Spec.Storage
			}
			databases[pg.Namespace+"/"+pg.Name] = size
		}
	}
	var rds shpyrdv1.RedisList
	if err := m.Client.List(ctx, &rds); err == nil {
		for _, rd := range rds.Items {
			size := resource.MustParse(defaultRedisStorage)
			if rd.Spec.Storage != nil {
				size = *rd.Spec.Storage
			}
			stores[rd.Namespace+"/"+rd.Name] = size
		}
	}
	for _, pvc := range pvcs.Items {
		if _, ok := nsMap[pvc.Namespace]; !ok {
			continue
		}
		c := claim{labels: map[string]string{"persistentvolumeclaim": pvc.Name}}
		for k, v := range pvc.Labels {
			c.labels["label_"+strings.NewReplacer(".", "_", "/", "_", "-", "_").Replace(k)] = v
		}
		var requested resource.Quantity
		var known bool
		switch {
		case pvc.Labels[LabelVolume] != "":
			requested, known = volumes[pvc.Namespace+"/"+pvc.Labels[LabelVolume]]
		case pvc.Labels["cnpg.io/cluster"] != "":
			requested, known = databases[pvc.Namespace+"/"+pvc.Labels["cnpg.io/cluster"]]
		case pvc.Labels["shpyrd.io/redis"] != "":
			requested, known = stores[pvc.Namespace+"/"+pvc.Labels["shpyrd.io/redis"]]
		}
		if known && requested.Sign() > 0 {
			c.requested = &requested
		}
		out[pvc.Namespace+"/"+pvc.Name] = c
	}
	return out
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
