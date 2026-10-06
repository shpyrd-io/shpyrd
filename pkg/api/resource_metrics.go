package api

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func (s *Server) resourceMetrics(c *gin.Context) {
	t, ok := s.resourceType(c.Param("kind"))
	if !ok || (t.Kind != "Postgres" && t.Kind != "Redis") {
		abort(c, http.StatusNotFound, errors.New("metrics support Postgres and Redis resources"))
		return
	}
	ns, ok := s.projectNamespace(c)
	if !ok {
		return
	}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: t.Group, Version: t.Version, Kind: t.Kind})
	if err := s.apps.Get(c.Request.Context(), types.NamespacedName{Namespace: ns, Name: c.Param("name")}, u); err != nil {
		abortNotFound(c, err, t.Kind)
		return
	}
	rng := c.DefaultQuery("range", "1h")
	dur, ok := rangeOptions[rng]
	if !ok {
		abort(c, http.StatusBadRequest, errors.New("range must be one of 15m, 1h, 6h, 24h, 7d"))
		return
	}
	if s.prom == nil {
		abort(c, http.StatusNotImplemented, errors.New("metrics are not configured"))
		return
	}
	end := time.Now().Truncate(time.Minute)
	step := stepFor(dur)
	queries := resourceChartQueries(ns, t.Kind, u.GetName())
	resp := MetricsResponse{Range: rng, Step: int(step.Seconds()), Charts: make([]Chart, len(queries)), Releases: []ReleaseMarker{}}
	var wg sync.WaitGroup
	for i, q := range queries {
		wg.Add(1)
		go func(i int, q chartQuery) {
			defer wg.Done()
			resp.Charts[i] = s.runChart(c.Request.Context(), q, end.Add(-dur), end, step, metricsOptions{})
		}(i, q)
	}
	wg.Wait()
	c.JSON(http.StatusOK, resp)
}

// Join ownership, rather than matching pod-name prefixes: a Redis store and
// a CNPG cluster can share a name, and neither may include an app's pods.
// kube-state-metrics provides these owner and volume relations without a
// custom label allowlist or database exporter.
func resourceChartQueries(namespace, kind, name string) []chartQuery {
	owner := "Cluster"
	if kind == "Redis" {
		owner = "StatefulSet"
	}
	pods := fmt.Sprintf(`max by (namespace, pod) (kube_pod_owner{namespace=%q,owner_kind=%q,owner_name=%q,owner_is_controller="true"})`, namespace, owner, name)
	containers := fmt.Sprintf(`namespace=%q,container!="",container!="POD"`, namespace)
	join := " * on (namespace, pod) group_left " + pods
	volumes := fmt.Sprintf(`max by (namespace, persistentvolumeclaim) (kube_pod_spec_volumes_persistentvolumeclaims_info{namespace=%q}%s)`, namespace, join)
	panel := func(id, title, unit, query string) chartQuery {
		return chartQuery{Chart: Chart{ID: id, Title: title, Unit: unit, Kind: "line"}, Query: query}
	}
	return []chartQuery{
		panel("cpu", "CPU", "cores", fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{%s}[2m])%s)`, containers, join)),
		panel("memory", "Memory", "bytes", fmt.Sprintf(`sum(container_memory_working_set_bytes{%s}%s)`, containers, join)),
		{Chart: Chart{ID: "network", Title: "Network", Unit: "bytes/s", Kind: "line"}, Fixed: map[string]string{
			"in":  fmt.Sprintf(`sum(rate(container_network_receive_bytes_total{namespace=%q,interface!="lo"}[2m])%s)`, namespace, join),
			"out": fmt.Sprintf(`sum(rate(container_network_transmit_bytes_total{namespace=%q,interface!="lo"}[2m])%s)`, namespace, join),
		}},
		{Chart: Chart{ID: "storage", Title: "Storage", Unit: "bytes", Kind: "line"}, Fixed: map[string]string{
			"used":     fmt.Sprintf(`sum(max by (namespace, persistentvolumeclaim) (kubelet_volume_stats_used_bytes{namespace=%q}) * on (namespace, persistentvolumeclaim) %s)`, namespace, volumes),
			"capacity": fmt.Sprintf(`sum(max by (namespace, persistentvolumeclaim) (kubelet_volume_stats_capacity_bytes{namespace=%q}) * on (namespace, persistentvolumeclaim) %s)`, namespace, volumes),
		}},
	}
}
