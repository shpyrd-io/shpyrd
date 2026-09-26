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

// metricsOptions are the request's shaping parameters. The table reads them so
// the handler does not have to branch per chart.
type metricsOptions struct {
	Mode     string // percent (default) or total
	By       string // process (default) or instance
	Process  string // one process type, or "" for all
	Replaced bool   // include instances that no longer exist
	Agg      string // none (default), sum, avg or max — collapses by=instance series
}

// absolute reports whether this chart is expressed in its own units rather
// than as a proportion of an allocation.
func (o metricsOptions) absolute() bool { return o.Mode == "total" }

// byInstance reports whether the charts break a process down into its running
// instances rather than summing them.
func (o metricsOptions) byInstance() bool { return o.By == "instance" }

// chartQueries defines the app dashboard, modelled on what Heroku, Fly,
// Render and Railway show: throughput by status class, response time
// percentiles, instance count, CPU and memory per process type, network.
func chartQueries(app *shpyrdv1.App, opts metricsOptions) []chartQuery {
	hosts := hostRegex(app)
	ns := app.Namespace
	name := app.Name
	containers := fmt.Sprintf(`namespace="%s",container!="",container!="POD"`, ns)
	stripApp := func(s string) string { return strings.TrimPrefix(s, name+"-") }

	// The process label comes along so a series can be traced back to its
	// allocation; grouping by pod collapses several containers into one
	// instance.
	group := "label_shpyrd_io_process"
	seriesLabel := "label_shpyrd_io_process"
	if opts.byInstance() {
		group = "pod, label_shpyrd_io_process"
		seriesLabel = "pod"
	}
	procFilter := ""
	if opts.Process != "" {
		procFilter = fmt.Sprintf(`,label_shpyrd_io_process=%q`, opts.Process)
	}
	podLabels := fmt.Sprintf(`kube_pod_labels{namespace="%s",label_shpyrd_io_app="%s"%s}`, ns, name, procFilter)

	// Network is counted for the whole namespace, which is this project and
	// nothing else, so the unjoined sum remains the cheapest way to ask the
	// default question. Naming one process or splitting by instance needs the
	// pod labels joined in, which is also what narrows the sum.
	netQuery := func(metric string) string {
		if !opts.byInstance() && opts.Process == "" {
			return fmt.Sprintf(`sum(rate(%s{namespace="%s"}[2m]))`, metric, ns)
		}
		return fmt.Sprintf(`sum by (%s) (rate(%s{namespace="%s"}[2m]) * on (namespace, pod) group_left (label_shpyrd_io_process) %s)`, group, metric, ns, podLabels)
	}

	cpuQuery := fmt.Sprintf(`sum by (%s) (rate(container_cpu_usage_seconds_total{%s}[2m]) * on (namespace, pod) group_left (label_shpyrd_io_process) %s)`, group, containers, podLabels)
	cpuUnit := "cores"
	if !opts.absolute() {
		cpuQuery = fmt.Sprintf(`100 * %s / sum by (%s) (kube_pod_container_resource_requests{%s,resource="cpu"} * on (namespace, pod) group_left (label_shpyrd_io_process) %s)`, cpuQuery, group, containers, podLabels)
		cpuUnit = "%"
	}

	memQuery := fmt.Sprintf(`sum by (%s) (container_memory_working_set_bytes{%s} * on (namespace, pod) group_left (label_shpyrd_io_process) %s)`, group, containers, podLabels)
	memUnit := "bytes"
	if !opts.absolute() {
		memQuery = fmt.Sprintf(`100 * %s / sum by (%s) (kube_pod_container_resource_requests{%s,resource="memory"} * on (namespace, pod) group_left (label_shpyrd_io_process) %s)`, memQuery, group, containers, podLabels)
		memUnit = "%"
	}

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
			// Deployments are named "<app>-<process>", which is what stripApp
			// relies on to name a series, so the process is already in the
			// label the query groups by — a filter here just narrows down to
			// the one deployment rather than needing a join. That is also why
			// this chart was missed when the process filter was scoped to
			// "charts which carry a pod": it identifies a process by
			// deployment name, not by a pod label, so the pod-based test for
			// what to filter didn't catch it. instanceCapable stays false: a
			// replica count is a single number, with nothing to break down per
			// instance.
			Chart:    Chart{ID: "instances", Title: "Instances", Unit: "count", Kind: "step"},
			Query:    instancesQuery(ns, name, opts.Process),
			LabelKey: "deployment",
			nameMap:  stripApp,
		},
		{
			// Usage as a percentage of the process allocation (the CPU
			// request, i.e. the instance size), averaged over its instances.
			// Shared sizes may burst above 100%. In total mode it is raw
			// cores instead, which is also what it falls back to when no
			// requests exist.
			Chart:        Chart{ID: "cpu", Title: "CPU", Unit: cpuUnit, Kind: "line", InstanceCapable: true},
			Query:        cpuQuery,
			LabelKey:     seriesLabel,
			Fallback:     fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{%s}[2m]))`, containers),
			FallbackName: "all",
			FallbackUnit: "cores",
		},
		{
			Chart:        Chart{ID: "memory", Title: "Memory", Unit: memUnit, Kind: "line", InstanceCapable: true},
			Query:        memQuery,
			LabelKey:     seriesLabel,
			Fallback:     fmt.Sprintf(`sum(container_memory_working_set_bytes{%s})`, containers),
			FallbackName: "all",
			FallbackUnit: "bytes",
		},
		{
			Chart: Chart{ID: "network", Title: "Network", Unit: "bytes/s", Kind: "line", InstanceCapable: true},
			// One chart, two directions: with instances the direction rides in
			// the series name so the chart set stays the same shape.
			Fixed: map[string]string{
				"in":  netQuery("container_network_receive_bytes_total"),
				"out": netQuery("container_network_transmit_bytes_total"),
			},
		},
	}
}

// instancesQuery builds the instances chart's query, constrained to one
// deployment when a process is selected.
func instancesQuery(ns, app, process string) string {
	if process == "" {
		return fmt.Sprintf(`sum by (deployment) (kube_deployment_status_replicas_available{namespace="%s"})`, ns)
	}
	return fmt.Sprintf(`sum by (deployment) (kube_deployment_status_replicas_available{namespace="%s",deployment="%s-%s"})`, ns, app, process)
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
