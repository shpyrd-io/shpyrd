package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/audit"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// RFC-0023: drains render into Vector's config; header values never do.

func TestRenderDrains(t *testing.T) {
	out := renderDrains([]renderedDrain{
		{ID: "drain_app_shop_datadog", Namespace: "app-shop", Name: "datadog", URL: "https://http-intake.logs.datadoghq.com/api/v2/logs", Format: "json",
			Processes: []string{"web"}, Headers: map[string]string{"DD-API-KEY": "DRAIN_APP_SHOP_DATADOG_DD_API_KEY"}},
		{ID: "drain_shpyrd_system_siem", Namespace: "shpyrd-system", Name: "siem", URL: "syslog+tls://logs.example.com:6514", Format: "syslog", Cluster: true},
		{ID: "drain_shpyrd_system_acme_axiom", Namespace: "shpyrd-system", Name: "acme-axiom", URL: "https://api.axiom.co/v1/datasets/acme/ingest", Format: "json", Workspace: "acme"},
	}, "shpyrd-system")

	// Valid YAML with the expected components.
	var doc struct {
		Transforms map[string]map[string]any `json:"transforms"`
		Sinks      map[string]map[string]any `json:"sinks"`
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("rendered config is not YAML: %v\n%s", err, out)
	}
	// Project drain: filtered by project and process; header via env var only.
	dd := doc.Transforms["drain_app_shop_datadog"]
	if dd["type"] != "filter" || dd["condition"] != `.namespace == "app-shop" && includes(["web"], .process)` {
		t.Errorf("datadog filter = %v", dd)
	}
	sink := doc.Sinks["drain_app_shop_datadog_sink"]
	if sink["type"] != "http" || sink["uri"] != "https://http-intake.logs.datadoghq.com/api/v2/logs" {
		t.Errorf("datadog sink = %v", sink)
	}
	if !strings.Contains(out, `"DD-API-KEY": "${DRAIN_APP_SHOP_DATADOG_DD_API_KEY}"`) {
		t.Errorf("header must be an env var reference:\n%s", out)
	}
	// Workspace drain: the lines of the workspace, whatever the project.
	if ax := doc.Transforms["drain_shpyrd_system_acme_axiom"]; ax["condition"] != `.workspace == "acme"` {
		t.Errorf("workspace drain condition = %v", ax["condition"])
	}
	// Cluster drain: no project condition; syslog gets a formatting remap
	// and a TLS socket sink fed by it.
	siem := doc.Transforms["drain_shpyrd_system_siem"]
	if siem["condition"] != "true" {
		t.Errorf("cluster drain condition = %v", siem["condition"])
	}
	fmtT := doc.Transforms["drain_shpyrd_system_siem_fmt"]
	if fmtT["type"] != "remap" || !strings.Contains(fmtT["source"].(string), `">1 "`) {
		t.Errorf("syslog remap = %v", fmtT)
	}
	ssink := doc.Sinks["drain_shpyrd_system_siem_sink"]
	if ssink["type"] != "socket" || ssink["address"] != "logs.example.com:6514" || ssink["tls"] == nil {
		t.Errorf("syslog sink = %v", ssink)
	}
	if inputs, _ := ssink["inputs"].([]any); len(inputs) != 1 || inputs[0] != "drain_shpyrd_system_siem_fmt" {
		t.Errorf("syslog sink must read the formatted stream: %v", ssink["inputs"])
	}
	// Deterministic.
	if again := renderDrains([]renderedDrain{
		{ID: "drain_shpyrd_system_acme_axiom", Namespace: "shpyrd-system", Name: "acme-axiom", URL: "https://api.axiom.co/v1/datasets/acme/ingest", Format: "json", Workspace: "acme"},
		{ID: "drain_shpyrd_system_siem", Namespace: "shpyrd-system", Name: "siem", URL: "syslog+tls://logs.example.com:6514", Format: "syslog", Cluster: true},
		{ID: "drain_app_shop_datadog", Namespace: "app-shop", Name: "datadog", URL: "https://http-intake.logs.datadoghq.com/api/v2/logs", Format: "json",
			Processes: []string{"web"}, Headers: map[string]string{"DD-API-KEY": "DRAIN_APP_SHOP_DATADOG_DD_API_KEY"}},
	}, "shpyrd-system"); again != out {
		t.Error("rendering must not depend on input order")
	}
	// Empty.
	if e := renderDrains(nil, "shpyrd-system"); !strings.Contains(e, "transforms: {}") || !strings.Contains(e, "sinks: {}") {
		t.Errorf("empty render = %q", e)
	}
}

func TestValidateDrainURL(t *testing.T) {
	good := map[string]string{
		"https://in.logs.example.com/ingest": "json",
		"http://echo.default.svc:8080/":      "json",
		"syslog://logs.example.com:514":      "syslog",
		"syslog+tls://logs.example.com:6514": "syslog",
	}
	for u, f := range good {
		if err := ValidateDrainURL(u, f); err != nil {
			t.Errorf("%s (%s): %v", u, f, err)
		}
	}
	bad := map[string]string{
		"ftp://x/":                     "json",
		"https://x/":                   "syslog",
		"syslog://logs.example.com":    "syslog", // no port
		"not a url":                    "json",
		"https://in.logs.example.com/": "csv",
	}
	for u, f := range bad {
		if err := ValidateDrainURL(u, f); err == nil {
			t.Errorf("%s (%s) accepted", u, f)
		}
	}
}

func TestParseSinkCounters(t *testing.T) {
	metrics := `# HELP vector_component_sent_events_total x
vector_component_sent_events_total{component_id="drain_app_shop_dd_sink",component_kind="sink"} 1234 1790194705401
vector_component_errors_total{component_id="drain_app_shop_dd_sink",component_kind="sink",error_type="request_failed"} 2
vector_component_sent_events_total{component_id="other_sink",component_kind="sink"} 99
`
	sent, errs, ok := parseSinkCounters(metrics, "drain_app_shop_dd_sink")
	if !ok || sent != 1234 || errs != 2 {
		t.Errorf("counters = %d %d %v", sent, errs, ok)
	}
	if _, _, ok := parseSinkCounters(metrics, "missing_sink"); ok {
		t.Error("missing component must not be ok")
	}
}

func newDrainReconciler(t *testing.T, objs ...client.Object) (*LogDrainReconciler, client.Client) {
	t.Helper()
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).WithStatusSubresource(&shpyrdv1.LogDrain{}).Build()
	return &LogDrainReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), SystemNamespace: "shpyrd-system"}, c
}

func vectorDaemonSet() *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: VectorDaemonSetName, Namespace: LogsNamespace},
		Spec:       appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "vector"}}}}},
	}
}

func TestLogDrainReconcile(t *testing.T) {
	ctx := context.Background()
	drain := &shpyrdv1.LogDrain{
		ObjectMeta: metav1.ObjectMeta{Name: "dd", Namespace: "app-shop"},
		Spec:       shpyrdv1.LogDrainSpec{URL: "https://intake.example.com/v1", HeadersFrom: &corev1.LocalObjectReference{Name: "dd-headers"}},
	}
	headers := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "dd-headers", Namespace: "app-shop"}, Data: map[string][]byte{"DD-API-KEY": []byte("s3cret")}}
	cluster := &shpyrdv1.LogDrain{
		ObjectMeta: metav1.ObjectMeta{Name: "siem", Namespace: "shpyrd-system"},
		Spec:       shpyrdv1.LogDrainSpec{URL: "syslog://siem.example.com:514"},
	}
	r, c := newDrainReconciler(t, drain, headers, cluster, vectorDaemonSet())
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(drain)}); err != nil {
		t.Fatal(err)
	}
	// The rendered ConfigMap has both drains and no secret value.
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: LogsNamespace, Name: DrainsConfigMapName}, cm); err != nil {
		t.Fatal(err)
	}
	body := cm.Data["drains.yaml"]
	if !strings.Contains(body, "drain_app_shop_dd_sink") || !strings.Contains(body, "drain_shpyrd_system_siem_sink") {
		t.Errorf("config lacks drains:\n%s", body)
	}
	if strings.Contains(body, "s3cret") {
		t.Error("secret value leaked into the config")
	}
	if !strings.Contains(body, `"DD-API-KEY": "${DRAIN_APP_SHOP_DD_DD_API_KEY}"`) {
		t.Errorf("header env reference must use an env-safe name:\n%s", body)
	}
	// The headers are mirrored into the agent's namespace under env-safe
	// keys, and the DaemonSet references the mirror with the drain's prefix.
	mirror := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: LogsNamespace, Name: "drain-app-shop-dd-headers"}, mirror); err != nil {
		t.Fatalf("mirror: %v", err)
	}
	if string(mirror.Data["DD_API_KEY"]) != "s3cret" {
		t.Errorf("mirror data = %v", mirror.Data)
	}
	ds := &appsv1.DaemonSet{}
	_ = c.Get(ctx, types.NamespacedName{Namespace: LogsNamespace, Name: VectorDaemonSetName}, ds)
	ef := ds.Spec.Template.Spec.Containers[0].EnvFrom
	if len(ef) != 1 || ef[0].Prefix != "DRAIN_APP_SHOP_DD_" || ef[0].SecretRef.Name != "drain-app-shop-dd-headers" {
		t.Errorf("envFrom = %+v", ef)
	}
	// Status: pending until lines flow (no HTTP client in tests).
	got := &shpyrdv1.LogDrain{}
	_ = c.Get(ctx, client.ObjectKeyFromObject(drain), got)
	if got.Status.Phase != shpyrdv1.DrainPending {
		t.Errorf("status = %+v", got.Status)
	}

	// An invalid drain is reported and left out of the config.
	badDrain := &shpyrdv1.LogDrain{ObjectMeta: metav1.ObjectMeta{Name: "bad", Namespace: "app-shop"}, Spec: shpyrdv1.LogDrainSpec{URL: "ftp://nope/"}}
	if err := c.Create(ctx, badDrain); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(badDrain)}); err != nil {
		t.Fatal(err)
	}
	_ = c.Get(ctx, client.ObjectKeyFromObject(badDrain), badDrain)
	if badDrain.Status.Phase != shpyrdv1.DrainFailing || !strings.Contains(badDrain.Status.Message, "http(s)://") {
		t.Errorf("bad drain status = %+v", badDrain.Status)
	}
	_ = c.Get(ctx, types.NamespacedName{Namespace: LogsNamespace, Name: DrainsConfigMapName}, cm)
	if strings.Contains(cm.Data["drains.yaml"], "drain_app_shop_bad") {
		t.Error("invalid drain must not be rendered")
	}

	// Removing a drain removes it from the config and its env.
	if err := c.Delete(ctx, drain); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}); err != nil {
		t.Fatal(err)
	}
	_ = c.Get(ctx, types.NamespacedName{Namespace: LogsNamespace, Name: DrainsConfigMapName}, cm)
	if strings.Contains(cm.Data["drains.yaml"], "drain_app_shop_dd") {
		t.Error("deleted drain still rendered")
	}
	_ = c.Get(ctx, types.NamespacedName{Namespace: LogsNamespace, Name: VectorDaemonSetName}, ds)
	if len(ds.Spec.Template.Spec.Containers[0].EnvFrom) != 0 {
		t.Errorf("envFrom after delete = %+v", ds.Spec.Template.Spec.Containers[0].EnvFrom)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: LogsNamespace, Name: "drain-app-shop-dd-headers"}, mirror); err == nil {
		t.Error("mirror must be pruned with its drain")
	}
}

func TestLogDrainWithoutAgent(t *testing.T) {
	drain := &shpyrdv1.LogDrain{ObjectMeta: metav1.ObjectMeta{Name: "dd", Namespace: "app-shop"}, Spec: shpyrdv1.LogDrainSpec{URL: "https://x.example.com/"}}
	r, c := newDrainReconciler(t, drain)
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(drain)}); err != nil {
		t.Fatal(err)
	}
	_ = c.Get(context.Background(), client.ObjectKeyFromObject(drain), drain)
	if drain.Status.Phase != shpyrdv1.DrainPending || !strings.Contains(drain.Status.Message, "logs-agent") {
		t.Errorf("status without agent = %+v", drain.Status)
	}
}

// #16: a drain added while the logs-agent extension was off says so; once
// the agent runs, that message gives way to the drain's own state, before
// any line has gone out.
func TestLogDrainAfterAgentEnabled(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		http *http.Client
		want string
	}{
		{"no metrics client", nil, "waiting for the first lines"},
		{"agent not reporting the drain yet", &http.Client{Transport: noDrainMetrics{}}, "waiting for the agent to load it"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			drain := &shpyrdv1.LogDrain{ObjectMeta: metav1.ObjectMeta{Name: "dd", Namespace: "app-shop"}, Spec: shpyrdv1.LogDrainSpec{URL: "https://x.example.com/"}}
			r, c := newDrainReconciler(t, drain)
			r.HTTP = tc.http
			req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(drain)}
			if _, err := r.Reconcile(ctx, req); err != nil {
				t.Fatal(err)
			}
			if err := c.Create(ctx, vectorDaemonSet()); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Reconcile(ctx, req); err != nil {
				t.Fatal(err)
			}
			_ = c.Get(ctx, client.ObjectKeyFromObject(drain), drain)
			if drain.Status.Phase != shpyrdv1.DrainPending || !strings.Contains(drain.Status.Message, tc.want) {
				t.Errorf("status with the agent running = %+v, want Pending: %s", drain.Status, tc.want)
			}
		})
	}
}

// noDrainMetrics answers Vector's metrics endpoint before it has loaded
// any drain.
type noDrainMetrics struct{}

func (noDrainMetrics) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("vector_started_total 1\n")), Header: http.Header{}}, nil
}

// vectorMetrics answers Vector's metrics endpoint with the counters it is
// given: sent and errs are read on every request.
type vectorMetrics struct{ sent, errs int64 }

func (m *vectorMetrics) RoundTrip(*http.Request) (*http.Response, error) {
	body := fmt.Sprintf(`vector_component_sent_events_total{component_id="drain_app_shop_dd_sink",component_kind="sink"} %d
vector_component_errors_total{component_id="drain_app_shop_dd_sink",component_kind="sink",error_type="request_failed"} %d
`, m.sent, m.errs)
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
}

// A receiver failing for ten checks in a row is one drain.failing entry in
// the project's audit trail; lines flowing again reset the count.
func TestLogDrainFailingAudit(t *testing.T) {
	ctx := context.Background()
	drain := &shpyrdv1.LogDrain{ObjectMeta: metav1.ObjectMeta{Name: "dd", Namespace: "app-shop"}, Spec: shpyrdv1.LogDrainSpec{URL: "https://intake.example.com/v1"}}
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}}
	r, c := newDrainReconciler(t, drain, app, vectorDaemonSet())
	r.Recorder = record.NewFakeRecorder(64)
	metrics := &vectorMetrics{}
	r.HTTP = &http.Client{Transport: metrics}
	events := k8sfake.NewSimpleClientset()
	r.Kube = events

	poll := func() shpyrdv1.LogDrainStatus {
		t.Helper()
		if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(drain)}); err != nil {
			t.Fatal(err)
		}
		got := &shpyrdv1.LogDrain{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(drain), got); err != nil {
			t.Fatal(err)
		}
		return got.Status
	}
	audited := func() []audit.Entry {
		t.Helper()
		entries, err := audit.List(ctx, events, audit.AppRefIn("app-shop", "shop"), 0)
		if err != nil {
			t.Fatal(err)
		}
		return entries
	}

	for i := 1; i < drainFailingPolls; i++ {
		metrics.errs++
		if st := poll(); st.Phase != shpyrdv1.DrainFailing || int(st.FailingPolls) != i {
			t.Fatalf("check %d: status = %+v", i, st)
		}
	}
	if n := len(audited()); n != 0 {
		t.Fatalf("audited before the tenth check: %d entries", n)
	}
	metrics.errs++
	if st := poll(); int(st.FailingPolls) != drainFailingPolls {
		t.Fatalf("tenth check: status = %+v", st)
	}
	entries := audited()
	if len(entries) != 1 {
		t.Fatalf("audit entries after the tenth check = %+v", entries)
	}
	e := entries[0]
	if e.Action != "drain.failing" || e.Actor != "platform" || e.Via != "controller" || e.Target != "dd -> https://intake.example.com/v1" || !strings.Contains(e.Detail, "for 5m0s") {
		t.Errorf("entry = %+v", e)
	}
	// Still failing: the count goes on, the entry is not repeated.
	metrics.errs++
	if st := poll(); int(st.FailingPolls) != drainFailingPolls+1 {
		t.Errorf("eleventh check: status = %+v", st)
	}
	if n := len(audited()); n != 1 {
		t.Errorf("audit entries after the eleventh check: %d", n)
	}
	// Lines flow again: active, count reset.
	metrics.sent = 7
	if st := poll(); st.Phase != shpyrdv1.DrainActive || st.FailingPolls != 0 {
		t.Errorf("after delivery: status = %+v", st)
	}
}

// A cluster drain's entry goes to the cluster's trail.
func TestLogDrainFailingAuditCluster(t *testing.T) {
	ctx := context.Background()
	drain := &shpyrdv1.LogDrain{ObjectMeta: metav1.ObjectMeta{Name: "siem", Namespace: "shpyrd-system"}, Spec: shpyrdv1.LogDrainSpec{URL: "https://siem.example.com/"}}
	r, _ := newDrainReconciler(t, drain, vectorDaemonSet())
	r.Recorder = record.NewFakeRecorder(64)
	r.Kube = k8sfake.NewSimpleClientset()
	drain.Status.FailingPolls = drainFailingPolls
	drain.Status.Message = "3 delivery errors, nothing sent since the last check"
	r.auditFailing(ctx, drain)
	entries, err := audit.List(ctx, r.Kube, audit.ClusterRef("shpyrd-system"), 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("cluster entries = %+v, %v", entries, err)
	}
	if entries[0].Action != "drain.failing" || entries[0].Target != "siem -> https://siem.example.com/" {
		t.Errorf("entry = %+v", entries[0])
	}
}
