package api

import (
	"fmt"
	"regexp"
	"strings"

	shpyrdv1 "shpyrd/api/v1alpha1"
)

// The chart table for a project's Metrics tab. Kept apart from the handler
// because RFC-0027 multiplies the query variants (by process or instance, an
// aggregation, percentage or absolute) and the table is worth keeping
// declarative rather than branching inside the handler.

// chartQueries defines the app dashboard, modelled on what Heroku, Fly,
// Render and Railway show: throughput by status class, response time
// percentiles, instance count, CPU and memory per process type, network.
func chartQueries(app *shpyrdv1.App) []chartQuery {
	hosts := hostRegex(app)
	ns := app.Namespace
	name := app.Name
	podLabels := fmt.Sprintf(`kube_pod_labels{namespace="%s",label_shpyrd_io_app="%s"}`, ns, name)
	containers := fmt.Sprintf(`namespace="%s",container!="",container!="POD"`, ns)
	stripApp := func(s string) string { return strings.TrimPrefix(s, name+"-") }

	return []chartQuery{
		{
			Chart:    Chart{ID: "throughput", Title: "Throughput", Unit: "rps", Kind: "stacked"},
			Query:    fmt.Sprintf(`sum by (class) (label_replace(rate(nginx_ingress_controller_requests{host=~"%s"}[2m]), "class", "${1}xx", "status", "(.).."))`, hosts),
			LabelKey: "class",
		},
		{
			Chart: Chart{ID: "latency", Title: "Response time", Unit: "ms", Kind: "line"},
			Fixed: map[string]string{
				"p50": fmt.Sprintf(`histogram_quantile(0.50, sum by (le) (rate(nginx_ingress_controller_request_duration_seconds_bucket{host=~"%s"}[5m]))) * 1000`, hosts),
				"p95": fmt.Sprintf(`histogram_quantile(0.95, sum by (le) (rate(nginx_ingress_controller_request_duration_seconds_bucket{host=~"%s"}[5m]))) * 1000`, hosts),
				"p99": fmt.Sprintf(`histogram_quantile(0.99, sum by (le) (rate(nginx_ingress_controller_request_duration_seconds_bucket{host=~"%s"}[5m]))) * 1000`, hosts),
			},
		},
		{
			Chart:    Chart{ID: "instances", Title: "Instances", Unit: "count", Kind: "step"},
			Query:    fmt.Sprintf(`sum by (deployment) (kube_deployment_status_replicas_available{namespace="%s"})`, ns),
			LabelKey: "deployment",
			nameMap:  stripApp,
		},
		{
			// Usage as a percentage of the process allocation (the CPU
			// request, i.e. the instance size), averaged over its instances.
			// Shared sizes may burst above 100%. Falls back to raw cores when
			// no requests exist.
			Chart: Chart{ID: "cpu", Title: "CPU", Unit: "%", Kind: "line"},
			Query: fmt.Sprintf(`100 * sum by (label_shpyrd_io_process) (rate(container_cpu_usage_seconds_total{%s}[2m]) * on (namespace, pod) group_left (label_shpyrd_io_process) %s)`+
				` / sum by (label_shpyrd_io_process) (kube_pod_container_resource_requests{%s,resource="cpu"} * on (namespace, pod) group_left (label_shpyrd_io_process) %s)`,
				containers, podLabels, containers, podLabels),
			LabelKey:     "label_shpyrd_io_process",
			Fallback:     fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{%s}[2m]))`, containers),
			FallbackName: "all",
			FallbackUnit: "cores",
		},
		{
			Chart: Chart{ID: "memory", Title: "Memory", Unit: "%", Kind: "line"},
			Query: fmt.Sprintf(`100 * sum by (label_shpyrd_io_process) (container_memory_working_set_bytes{%s} * on (namespace, pod) group_left (label_shpyrd_io_process) %s)`+
				` / sum by (label_shpyrd_io_process) (kube_pod_container_resource_requests{%s,resource="memory"} * on (namespace, pod) group_left (label_shpyrd_io_process) %s)`,
				containers, podLabels, containers, podLabels),
			LabelKey:     "label_shpyrd_io_process",
			Fallback:     fmt.Sprintf(`sum(container_memory_working_set_bytes{%s})`, containers),
			FallbackName: "all",
			FallbackUnit: "bytes",
		},
		{
			Chart: Chart{ID: "network", Title: "Network", Unit: "bytes/s", Kind: "line"},
			Fixed: map[string]string{
				"in":  fmt.Sprintf(`sum(rate(container_network_receive_bytes_total{namespace="%s"}[2m]))`, ns),
				"out": fmt.Sprintf(`sum(rate(container_network_transmit_bytes_total{namespace="%s"}[2m]))`, ns),
			},
		},
	}
}

// hostRegex builds a PromQL regex matching the app's ingress hosts.
func hostRegex(app *shpyrdv1.App) string {
	hosts := app.Spec.Domains
	if len(hosts) == 0 && app.Status.URL != "" {
		h := strings.TrimPrefix(app.Status.URL, "https://")
		if i := strings.IndexByte(h, ':'); i >= 0 {
			h = h[:i]
		}
		hosts = []string{h}
	}
	quoted := make([]string, 0, len(hosts))
	for _, h := range hosts {
		quoted = append(quoted, regexp.QuoteMeta(h))
	}
	if len(quoted) == 0 {
		return "^$"
	}
	// PromQL regexes are RE2 in a double-quoted string; escape backslashes.
	return strings.ReplaceAll(strings.Join(quoted, "|"), `\`, `\\`)
}
