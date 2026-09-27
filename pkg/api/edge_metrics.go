package api

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Edge decisions are counted, not logged per request (RFC-0033 "Audit"):
// a denial of a signed-in person is a number per project, on the server's
// metrics port, next to the controller's.
var (
	edgeDenials = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "shpyrd_edge_denials_total",
		Help: "Requests to an app the edge refused for a signed-in caller, by project and reason.",
	}, []string{"workspace", "project", "reason"})
	edgeAdmissions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "shpyrd_edge_admissions_total",
		Help: "Requests to an app the edge admitted with an identity, by project.",
	}, []string{"workspace", "project"})
	registerEdgeMetrics sync.Once
)

// Denial reasons.
const (
	denialNoRole     = "no_role"   // signed in, no role on the project
	denialSuspended  = "suspended" // the person is suspended
	denialPreview    = "preview"   // a preview's teams may not open the app
	denialAnonymous  = "anonymous" // nobody signed in on an authenticated app
	denialWorkspace  = "workspace" // the workspace is suspended or unknown
	denialToken      = "bad_token" // a platform token that resolves to nobody
	denialBadPreview = "anonymous_preview"
)

func init() {
	registerEdgeMetrics.Do(func() {
		ctrlmetrics.Registry.MustRegister(edgeDenials, edgeAdmissions)
	})
}

func countDenial(workspace, project, reason string) {
	edgeDenials.WithLabelValues(workspace, project, reason).Inc()
}

func countAdmission(workspace, project string) {
	edgeAdmissions.WithLabelValues(workspace, project).Inc()
}
