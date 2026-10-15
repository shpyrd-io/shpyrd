package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/install"
)

// The shared Postgres servers at the console (storage plan, step 3): the
// fleet with its numbers, a server's databases a page at a time, and a
// database found across every server. The console only reads here: a
// pooler, a drop date or a cap are the platform's or the owner's to
// change.

// The labels the controller puts on a shared server and on its tenants'
// objects (internal/controller/postgres_shared.go).
const (
	sharedServerLabel          = "shpyrd.io/shared-postgres"
	sharedOfServerLabel        = "shpyrd.io/shared-server"
	sharedTenantNamespaceLabel = "shpyrd.io/postgres-namespace"
	sharedTenantNameLabel      = "shpyrd.io/postgres-name"
)

// PostgresServersView is the fleet: one row per platform-owned server,
// what needs attention across them, and the dedicated clusters beside.
type PostgresServersView struct {
	Servers   []PostgresServerView  `json:"servers"`
	Dedicated PostgresDedicatedView `json:"dedicated"`
	Attention PostgresAttentionView `json:"attention"`
	// MeasuredAt is when this answer was put together.
	MeasuredAt string `json:"measuredAt,omitempty"`
}

// PostgresServerView is one shared server: what fills it, what protects it.
type PostgresServerView struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Ready     int64  `json:"ready"`
	Instances int64  `json:"instances"`
	Phase     string `json:"phase"`
	Tenants   int    `json:"tenants"`
	Limit     int    `json:"limit"`
	// Open to new databases: under the limit and healthy.
	Open        bool                      `json:"open"`
	Disk        PostgresServerDisk        `json:"disk"`
	Connections PostgresServerConnections `json:"connections"`
	Pooler      PostgresServerPooler      `json:"pooler"`
	// Archiving is "ok", "failing" or "starting", from the server's
	// ContinuousArchiving condition.
	Archiving      string               `json:"archiving"`
	LastBaseBackup string               `json:"lastBaseBackup,omitempty"`
	LastDumps      *PostgresServerDumps `json:"lastDumps"`
	// Kept is the databases behind a drop date; Converting the
	// conversions under way on this server.
	Kept       int    `json:"kept"`
	Converting int    `json:"converting"`
	CreatedAt  string `json:"createdAt"`
}

// PostgresServerDisk is the server's volume: what is used of what was asked.
type PostgresServerDisk struct {
	UsedBytes int64 `json:"usedBytes"`
	SizeBytes int64 `json:"sizeBytes"`
}

// PostgresServerConnections is the seven-day p95 of client connections
// against max_connections; the pooler comes on at 60 %.
type PostgresServerConnections struct {
	P95 int `json:"p95"`
	Max int `json:"max"`
}

// PostgresServerPooler says whether the server's pooler is on, since when,
// and how many of its replicas answer.
type PostgresServerPooler struct {
	On    bool   `json:"on"`
	Since string `json:"since,omitempty"`
	Ready *int32 `json:"ready,omitempty"`
}

// PostgresServerDumps is the server's newest nightly run.
type PostgresServerDumps struct {
	At     string `json:"at"`
	Dumped int    `json:"dumped"`
	Failed int    `json:"failed"`
	Took   string `json:"took"`
}

// PostgresDedicatedView counts the dedicated clusters beside the fleet.
type PostgresDedicatedView struct {
	Clusters  int   `json:"clusters"`
	Ready     int   `json:"ready"`
	DiskBytes int64 `json:"diskBytes"`
	BackedUp  int   `json:"backedUp"`
}

// PostgresAttentionView is what an operator should look at: databases
// with their connections paused, over or near their cap, servers not
// healthy, servers whose last dumps failed.
type PostgresAttentionView struct {
	Paused      int `json:"paused"`
	Over        int `json:"over"`
	Near        int `json:"near"`
	Degraded    int `json:"degraded"`
	DumpsFailed int `json:"dumpsFailed"`
}

// PostgresTenantView is a database on a shared server as the operator
// sees it: whose it is, its hashed name, how full it is, its state.
type PostgresTenantView struct {
	Workspace string `json:"workspace"`
	Project   string `json:"project"`
	Name      string `json:"name"`
	Database  string `json:"database"`
	Server    string `json:"server"`
	Phase     string `json:"phase"`
	CapBytes  int64  `json:"capBytes"`
	UsedBytes int64  `json:"usedBytes"`
	// State is "" within the cap, "near" from 80 %, "over" from 100 %,
	// "paused" once the fence applies.
	State string `json:"state"`
	// Keep is how many nightly copies of it stay.
	Keep int `json:"keep"`
}

// PostgresTenantPage is a page of a server's databases and how many
// there are after narrowing.
type PostgresTenantPage struct {
	Items []PostgresTenantView `json:"items"`
	Total int                  `json:"total"`
	Page  int                  `json:"page"`
	Size  int                  `json:"size"`
}

const (
	tenantPageSize    = 50
	tenantPageMost    = 500
	tenantSearchMost  = 50
	tenantSearchLeast = 2
	defaultCapBytes   = int64(1 << 30)
	defaultDiskBytes  = int64(5 << 30)
)

// postgresServers answers GET /api/cluster/postgres-servers: the fleet.
func (s *Server) postgresServers(c *gin.Context) {
	ctx := c.Request.Context()
	servers, err := s.sharedServers(ctx)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	databases, err := s.sharedDatabases(ctx, "")
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	pgs, err := s.listPostgres(ctx)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	runs, messages, err := s.sharedDumpRuns(ctx)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	poolers, err := s.kube.Kube.AppsV1().Deployments(controller.SharedPostgresNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	poolerReady := map[string]int32{}
	for _, d := range poolers.Items {
		poolerReady[d.Name] = d.Status.ReadyReplicas
	}

	tenants, kept, converting := map[string]int{}, map[string]int{}, map[string]int{}
	for _, db := range databases {
		server := db.GetLabels()[sharedOfServerLabel]
		tenants[server]++
		if db.GetAnnotations()[controller.AnnotationDropAfter] != "" {
			kept[server]++
		}
	}
	view := PostgresServersView{Servers: []PostgresServerView{}, MeasuredAt: time.Now().UTC().Format(time.RFC3339)}
	for i := range pgs {
		pg := &pgs[i]
		if pg.Annotations[controller.AnnotationConvert] != "" {
			converting[pg.Annotations[controller.AnnotationSharedServer]]++
		}
		if pg.Spec.Type != shpyrdv1.PostgresTypeShared {
			view.Dedicated.Clusters++
			if pg.Status.Phase == shpyrdv1.ResourceReady {
				view.Dedicated.Ready++
			}
			if pg.Spec.Backups != nil {
				view.Dedicated.BackedUp++
			}
			view.Dedicated.DiskBytes += storageBytes(pg.Spec.Storage, defaultDiskBytes)
			continue
		}
		switch tenantState(pg) {
		case "paused":
			view.Attention.Paused++
		case "over":
			view.Attention.Over++
		case "near":
			view.Attention.Near++
		}
	}

	limit := s.sharedServerLimit(ctx)
	used := s.sharedDiskUsed(ctx)
	for i := range servers {
		server := &servers[i]
		name := server.GetName()
		v := PostgresServerView{
			Name:       name,
			Tenants:    tenants[name],
			Limit:      limit,
			Kept:       kept[name],
			Converting: converting[name],
			Archiving:  archivingOf(server),
			CreatedAt:  server.GetCreationTimestamp().UTC().Format(time.RFC3339),
		}
		image, _, _ := unstructured.NestedString(server.Object, "spec", "imageName")
		v.Version = versionOfImage(image)
		v.Instances, _, _ = unstructured.NestedInt64(server.Object, "spec", "instances")
		v.Ready, _, _ = unstructured.NestedInt64(server.Object, "status", "readyInstances")
		v.Phase, _, _ = unstructured.NestedString(server.Object, "status", "phase")
		v.LastBaseBackup, _, _ = unstructured.NestedString(server.Object, "status", "lastSuccessfulBackup")
		if sizeText, _, _ := unstructured.NestedString(server.Object, "spec", "storage", "size"); sizeText != "" {
			if size, err := resource.ParseQuantity(sizeText); err == nil {
				v.Disk.SizeBytes = size.Value()
			}
		}
		for claim, bytes := range used {
			if strings.HasPrefix(claim, name+"-") && int64(bytes) > v.Disk.UsedBytes {
				v.Disk.UsedBytes = int64(bytes)
			}
		}
		maxText, _, _ := unstructured.NestedString(server.Object, "spec", "postgresql", "parameters", "max_connections")
		v.Connections.Max, _ = strconv.Atoi(maxText)
		v.Connections.P95, _ = strconv.Atoi(server.GetAnnotations()[controller.AnnotationConnectionsP95])
		if since := server.GetAnnotations()[controller.AnnotationPooler]; since != "" {
			ready := poolerReady[name+"-pooler"]
			v.Pooler = PostgresServerPooler{On: true, Since: since, Ready: &ready}
		}
		healthy := serverHealthy(v.Phase)
		v.Open = v.Tenants < v.Limit && healthy
		if !healthy || v.Ready < v.Instances {
			view.Attention.Degraded++
		}
		if run, ok := runs[name]; ok {
			v.LastDumps = dumpsView(run, messages[run.Name], v.Tenants)
			if v.LastDumps.Failed > 0 {
				view.Attention.DumpsFailed++
			}
		}
		view.Servers = append(view.Servers, v)
	}
	sort.Slice(view.Servers, func(i, j int) bool {
		a, b := view.Servers[i].Name, view.Servers[j].Name
		if na, nb := trailingNumber(a), trailingNumber(b); na != nb {
			return na < nb
		}
		return a < b
	})
	c.JSON(http.StatusOK, view)
}

// postgresServerDatabases answers GET
// /api/cluster/postgres-servers/:name/databases?page&size&state&q: a page
// of one server's databases, narrowed by state and by a word, in the
// order of workspace, project, name.
func (s *Server) postgresServerDatabases(c *gin.Context) {
	ctx := c.Request.Context()
	server := c.Param("name")
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(c.Query("size"))
	if size < 1 {
		size = tenantPageSize
	}
	if size > tenantPageMost {
		size = tenantPageMost
	}
	state := c.Query("state")
	q := strings.ToLower(strings.TrimSpace(c.Query("q")))

	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(controller.CNPGClusterGVK)
	if err := s.apps.Get(ctx, types.NamespacedName{Namespace: controller.SharedPostgresNamespace, Name: server}, cluster); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			abort(c, http.StatusNotFound, fmt.Errorf("no shared server %q", server))
			return
		}
		abort(c, http.StatusBadGateway, err)
		return
	}
	databases, err := s.sharedDatabases(ctx, server)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	pgs, err := s.listPostgres(ctx)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	byKey := map[string]*shpyrdv1.Postgres{}
	for i := range pgs {
		byKey[pgs[i].Namespace+"/"+pgs[i].Name] = &pgs[i]
	}
	owners, err := s.projectNamespaces(ctx)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	items := []PostgresTenantView{}
	for _, db := range databases {
		l := db.GetLabels()
		pg := byKey[l[sharedTenantNamespaceLabel]+"/"+l[sharedTenantNameLabel]]
		if pg == nil {
			continue // a database kept behind its drop date; its Postgres is gone
		}
		name, _, _ := unstructured.NestedString(db.Object, "spec", "name")
		t := tenantView(pg, name, server, owners[pg.Namespace])
		if tenantMatches(t, state, q) {
			items = append(items, t)
		}
	}
	sortTenants(items)
	total := len(items)
	start := min((page-1)*size, total)
	end := min(start+size, total)
	c.JSON(http.StatusOK, PostgresTenantPage{Items: items[start:end], Total: total, Page: page, Size: size})
}

// findPostgresDatabases answers GET /api/cluster/postgres-databases?q=: the
// first fifty shared databases, across every server, whose workspace,
// project, name or hashed name has the word. Fewer than two characters
// find nothing.
func (s *Server) findPostgresDatabases(c *gin.Context) {
	ctx := c.Request.Context()
	q := strings.ToLower(strings.TrimSpace(c.Query("q")))
	found := []PostgresTenantView{}
	if len([]rune(q)) < tenantSearchLeast {
		c.JSON(http.StatusOK, found)
		return
	}
	pgs, err := s.listPostgres(ctx)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	owners, err := s.projectNamespaces(ctx)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	for i := range pgs {
		pg := &pgs[i]
		if pg.Spec.Type != shpyrdv1.PostgresTypeShared {
			continue
		}
		t := tenantView(pg, controller.SharedDatabaseName(pg), pg.Annotations[controller.AnnotationSharedServer], owners[pg.Namespace])
		if tenantMatches(t, "", q) {
			found = append(found, t)
		}
	}
	sortTenants(found)
	if len(found) > tenantSearchMost {
		found = found[:tenantSearchMost]
	}
	c.JSON(http.StatusOK, found)
}

// sharedServers lists the shared servers: the CloudNativePG Clusters the
// controller marked as such. None when the operator is not installed.
func (s *Server) sharedServers(ctx context.Context) ([]unstructured.Unstructured, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(controller.CNPGClusterGVK.GroupVersion().WithKind("ClusterList"))
	if err := s.apps.List(ctx, list, client.InNamespace(controller.SharedPostgresNamespace), client.MatchingLabels{sharedServerLabel: "server"}); err != nil {
		if meta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, err
	}
	return list.Items, nil
}

// sharedDatabases lists the tenants' Database objects: every server's, or
// one server's.
func (s *Server) sharedDatabases(ctx context.Context, server string) ([]unstructured.Unstructured, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(controller.CNPGDatabaseGVK.GroupVersion().WithKind("DatabaseList"))
	opts := []client.ListOption{client.InNamespace(controller.SharedPostgresNamespace)}
	if server != "" {
		opts = append(opts, client.MatchingLabels{sharedOfServerLabel: server})
	}
	if err := s.apps.List(ctx, list, opts...); err != nil {
		if meta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, err
	}
	return list.Items, nil
}

// listPostgres lists every Postgres of the cluster; none when the
// extension is not installed.
func (s *Server) listPostgres(ctx context.Context) ([]shpyrdv1.Postgres, error) {
	var list shpyrdv1.PostgresList
	if err := s.apps.List(ctx, &list); err != nil {
		if meta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, err
	}
	return list.Items, nil
}

// projectNamespaces reads the projects' namespaces once and gives each
// one's labels, where the workspace and the project are.
func (s *Server) projectNamespaces(ctx context.Context) (map[string]map[string]string, error) {
	list, err := s.kube.Kube.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: shpyrdv1.LabelProject})
	if err != nil {
		return nil, err
	}
	out := make(map[string]map[string]string, len(list.Items))
	for _, ns := range list.Items {
		out[ns.Name] = ns.Labels
	}
	return out, nil
}

// sharedServerLimit is how many databases a server takes before it
// closes: the installer's variable, else the controller's default.
func (s *Server) sharedServerLimit(ctx context.Context) int {
	vars := map[string]string{}
	if info, err := install.ReadInstallInfo(ctx, s.kube, ""); err == nil && info.Vars != nil {
		vars = info.Vars
	}
	return install.SharedPostgresPerServer(func(name string) string { return vars[name] })
}

// sharedDiskUsed is what each claim of the shared servers' namespace
// holds, by claim name, from Prometheus; nothing without one.
func (s *Server) sharedDiskUsed(ctx context.Context) map[string]float64 {
	if s.prom == nil {
		return nil
	}
	series, err := s.prom.QueryInstantSeries(ctx, fmt.Sprintf(`max by (persistentvolumeclaim) (kubelet_volume_stats_used_bytes{namespace=%q})`, controller.SharedPostgresNamespace))
	if err != nil {
		s.log.Warn("shared servers' disk usage not read", "err", err.Error())
		return nil
	}
	out := make(map[string]float64, len(series))
	for _, sample := range series {
		out[sample.Labels["persistentvolumeclaim"]] = sample.Value
	}
	return out
}

// sharedDumpRuns is the newest nightly dumps Job of every server, by
// server, and the line a run's pod wrote on its way out, by Job name.
// The nightly Jobs are the CronJob's, named <server>-dumps-<n>; a dump
// of one database asked for now is named otherwise and is not a run.
func (s *Server) sharedDumpRuns(ctx context.Context) (map[string]batchv1.Job, map[string]string, error) {
	jobs, err := s.kube.Kube.BatchV1().Jobs(controller.SharedPostgresNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	newest := map[string]batchv1.Job{}
	for _, job := range jobs.Items {
		server, _, found := strings.Cut(job.Name, "-dumps-")
		if !found || server == "" {
			continue
		}
		if kept, ok := newest[server]; !ok || jobStarted(job).After(jobStarted(kept)) {
			newest[server] = job
		}
	}
	pods, err := s.kube.Kube.CoreV1().Pods(controller.SharedPostgresNamespace).List(ctx, metav1.ListOptions{LabelSelector: sharedServerLabel + "=dumps"})
	if err != nil {
		return nil, nil, err
	}
	messages := map[string]string{}
	seen := map[string]time.Time{}
	for _, pod := range pods.Items {
		job := jobOfPod(pod)
		message := dumpsMessage(pod)
		if job == "" || message == "" {
			continue
		}
		if at, ok := seen[job]; ok && !pod.CreationTimestamp.After(at) {
			continue // an older pod of the same Job
		}
		seen[job] = pod.CreationTimestamp.Time
		messages[job] = message
	}
	return newest, messages, nil
}

// jobStarted is when a Job started, else when it was made.
func jobStarted(job batchv1.Job) time.Time {
	if job.Status.StartTime != nil {
		return job.Status.StartTime.Time
	}
	return job.CreationTimestamp.Time
}

// jobOfPod names the Job a pod belongs to, from the labels the Job
// controller sets or its owner reference.
func jobOfPod(pod corev1.Pod) string {
	if name := pod.Labels[batchv1.JobNameLabel]; name != "" {
		return name
	}
	if name := pod.Labels["job-name"]; name != "" { // the label before batch.kubernetes.io/job-name
		return name
	}
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "Job" {
			return owner.Name
		}
	}
	return ""
}

// dumpsMessage is the termination message of the pod's dump container
// ("dumped=198 failed=0 removed=3 took=51s"), when it ended with one.
func dumpsMessage(pod corev1.Pod) string {
	for _, cs := range pod.Status.ContainerStatuses {
		for _, state := range []*corev1.ContainerStateTerminated{cs.State.Terminated, cs.LastTerminationState.Terminated} {
			if state != nil && strings.HasPrefix(state.Message, "dumped=") {
				return state.Message
			}
		}
	}
	return ""
}

// dumpsView is the newest run as the console shows it: from the Job's
// times and condition, and from the numbers its pod wrote when that pod
// is still there.
func dumpsView(job batchv1.Job, message string, tenants int) *PostgresServerDumps {
	at := jobStarted(job)
	if job.Status.CompletionTime != nil {
		at = job.Status.CompletionTime.Time
	}
	v := &PostgresServerDumps{At: at.UTC().Format(time.RFC3339), Dumped: tenants}
	if job.Status.StartTime != nil && job.Status.CompletionTime != nil {
		v.Took = fmt.Sprintf("%ds", int(job.Status.CompletionTime.Sub(job.Status.StartTime.Time).Seconds()))
	}
	if jobFailed(job) {
		v.Failed = 1
	}
	for _, field := range strings.Fields(message) {
		key, value, _ := strings.Cut(field, "=")
		switch key {
		case "dumped":
			v.Dumped, _ = strconv.Atoi(value)
		case "failed":
			v.Failed, _ = strconv.Atoi(value)
		case "took":
			v.Took = value
		}
	}
	return v
}

// archivingOf reads the server's ContinuousArchiving condition: "ok" when
// it holds, "failing" when it does not, "starting" before there is one.
func archivingOf(server *unstructured.Unstructured) string {
	conditions, _, _ := unstructured.NestedSlice(server.Object, "status", "conditions")
	for _, c := range conditions {
		m, ok := c.(map[string]interface{})
		if !ok || m["type"] != "ContinuousArchiving" {
			continue
		}
		switch m["status"] {
		case "True":
			return "ok"
		case "False":
			return "failing"
		}
	}
	return "starting"
}

// versionOfImage is the tag of the server's image: "17" of
// "ghcr.io/cloudnative-pg/postgresql:17".
func versionOfImage(image string) string {
	i := strings.LastIndex(image, ":")
	if i < 0 || strings.Contains(image[i:], "/") {
		return ""
	}
	return image[i+1:]
}

// serverHealthy reads CloudNativePG's phase ("Cluster in healthy state").
func serverHealthy(phase string) bool {
	return strings.Contains(strings.ToLower(phase), "healthy")
}

// trailingNumber is the number a server's name ends with, so pg-shared-10
// comes after pg-shared-9.
func trailingNumber(name string) int {
	i := len(name)
	for i > 0 && name[i-1] >= '0' && name[i-1] <= '9' {
		i--
	}
	n, _ := strconv.Atoi(name[i:])
	return n
}

// storageBytes is a storage quantity in bytes, or the default when unset.
func storageBytes(q *resource.Quantity, fallback int64) int64 {
	if q != nil && q.Value() > 0 {
		return q.Value()
	}
	return fallback
}

// tenantState reads where a shared database stands against its cap:
// "paused" once the metering loop found it at the fence (the Cap
// condition's far reason), else from the percentage it recorded.
func tenantState(pg *shpyrdv1.Postgres) string {
	for _, c := range pg.Status.Conditions {
		if c.Type == controller.ConditionCap && c.Reason == controller.CapFar {
			return "paused"
		}
	}
	percent := int32(0)
	if pg.Status.CapPercent != nil {
		percent = *pg.Status.CapPercent
	}
	switch {
	case percent >= 100:
		return "over"
	case percent >= 80:
		return "near"
	}
	return ""
}

// tenantView is a shared database as the operator sees it. Its owner
// comes from the labels of the namespace it lives in.
func tenantView(pg *shpyrdv1.Postgres, database, server string, owner map[string]string) PostgresTenantView {
	capBytes := storageBytes(pg.Spec.Storage, defaultCapBytes)
	// What it uses is derived from the percentage the metering loop records
	// on its status every five minutes, not measured here.
	var used int64
	if pg.Status.CapPercent != nil {
		used = capBytes * int64(*pg.Status.CapPercent) / 100
	}
	return PostgresTenantView{
		Workspace: owner[shpyrdv1.LabelWorkspace],
		Project:   owner[shpyrdv1.LabelProject],
		Name:      pg.Name,
		Database:  database,
		Server:    server,
		Phase:     pg.Status.Phase,
		CapBytes:  capBytes,
		UsedBytes: used,
		State:     tenantState(pg),
		Keep:      controller.SharedDumpsKeep(pg),
	}
}

// tenantMatches narrows by state, and by a word found in the workspace,
// project, name or hashed name, case aside.
func tenantMatches(t PostgresTenantView, state, q string) bool {
	if state != "" && t.State != state {
		return false
	}
	if q == "" {
		return true
	}
	for _, field := range []string{t.Workspace, t.Project, t.Name, t.Database} {
		if strings.Contains(strings.ToLower(field), q) {
			return true
		}
	}
	return false
}

// sortTenants orders databases by workspace, project, name.
func sortTenants(items []PostgresTenantView) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Workspace != b.Workspace {
			return a.Workspace < b.Workspace
		}
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Database < b.Database
	})
}
