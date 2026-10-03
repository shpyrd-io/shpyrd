package install

import (
	"bytes"
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// The extensions whose components hold pods, so they render with the
// profile in these tests.
var poolTestExtensions = []ExtensionComponent{
	{Extension: "auth-local", Component: "dex", Runlevel: "rc4"},
	{Extension: "postgres", Component: "barman-cloud", Runlevel: "rc3"},
	{Extension: "postgres", Component: "pg-gateway", Runlevel: "rc4"},
	{Extension: "object-storage", Component: "object-storage", Runlevel: "rc3"},
	{Extension: "logs-agent", Component: "logs-agent", Runlevel: "rc3"},
}

// podSelectors renders every Kustomize component of the profile and
// returns the pool each pod-making object asks for, by "component kind/name".
func podSelectors(t *testing.T, profile string, vars map[string]string) map[string]string {
	t.Helper()
	eng, err := New(nil, Options{Profile: profile, Vars: vars, Extensions: poolTestExtensions, Reporter: &quiet{}})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, c := range eng.components {
		if c.Kustomize == nil {
			continue
		}
		objs, err := eng.renderComponent(c)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		for _, o := range objs {
			key := c.Name + " " + o.GetKind() + "/" + o.GetName()
			switch o.GetKind() {
			case "DaemonSet":
				sel, _, _ := unstructured.NestedStringMap(o.Object, "spec", "template", "spec", "nodeSelector")
				out[key] = sel[PoolLabel]
			default:
				if path := podSelectorPath(o); path != nil {
					sel, _, _ := unstructured.NestedStringMap(o.Object, path...)
					out[key] = sel[PoolLabel]
				}
			}
		}
	}
	return out
}

// On a cloud profile every pod the platform installs runs on the platform
// pool, what holds the projects' data on the data pool, and DaemonSets
// stay on every node.
func TestCloudProfilesPutThePlatformOnItsPool(t *testing.T) {
	vars := map[string]string{
		VarDomain: "example.com", VarACMEEmail: "ops@example.com", VarDataPool: "data",
		VarNodePoolID: "ocid1.nodepool.oc1..apps", VarBackupTarget: "s3://backups/shpyrd",
	}
	for _, profile := range []string{"oci", "aws"} {
		got := podSelectors(t, profile, vars)
		want := map[string]string{
			"shpyrd Deployment/shpyrd-server":                    "platform",
			"kpack Deployment/kpack-controller":                  "platform",
			"kpack Deployment/kpack-webhook":                     "platform",
			"dex Deployment/dex":                                 "platform",
			"registry Deployment/registry":                       "platform",
			"control-plane-db StatefulSet/control-plane-db":      "platform",
			"platform-backup CronJob/platform-backup":            "platform",
			"barman-cloud Deployment/barman-cloud":               "platform",
			"snapshot-controller Deployment/snapshot-controller": "platform",
			"object-storage Deployment/object-storage":           "data",
			"pg-gateway Deployment/pg-gateway":                   "data",
			"registry-nodes DaemonSet/registry-nodes":            "",
			"logs-agent DaemonSet/vector":                        "",
		}
		if profile == "oci" {
			want["cluster-autoscaler Deployment/cluster-autoscaler"] = "platform"
			want["network-policy Deployment/calico-kube-controllers"] = "platform"
			want["network-policy DaemonSet/calico-node"] = ""
		}
		for key, pool := range want {
			if p, ok := got[key]; !ok {
				t.Errorf("%s: %s was not rendered", profile, key)
			} else if p != pool {
				t.Errorf("%s: %s runs on %q, want %q", profile, key, p, pool)
			}
		}
		for key, pool := range got {
			if _, named := want[key]; !named && !strings.Contains(key, "DaemonSet/") && pool != "platform" {
				t.Errorf("%s: %s runs on %q, want the platform pool", profile, key, pool)
			}
		}
	}
}

// Without a data pool the projects' data goes to the platform pool.
func TestDataComponentsFallBackToThePlatformPool(t *testing.T) {
	got := podSelectors(t, "oci", map[string]string{VarDomain: "example.com"})
	for _, key := range []string{"object-storage Deployment/object-storage", "pg-gateway Deployment/pg-gateway"} {
		if got[key] != "platform" {
			t.Errorf("%s runs on %q, want the platform pool", key, got[key])
		}
	}
}

// kind has one node and no pool: nothing gets a selector.
func TestLocalProfilePinsNothing(t *testing.T) {
	for key, pool := range podSelectors(t, "local", map[string]string{VarDomain: "example.test"}) {
		if pool != "" {
			t.Errorf("%s runs on %q on kind", key, pool)
		}
	}
}

// A chart's pods are pinned by Helm's post-render step: the selectors the
// chart set stay, objects that make no pods pass through untouched.
func TestHelmPostRendererPinsWhatMakesPods(t *testing.T) {
	in := `---
# Source: keda/templates/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: keda-operator
spec:
  ports: [{port: 9666}]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: keda-operator
spec:
  template:
    spec:
      nodeSelector:
        kubernetes.io/os: linux
      containers: [{name: keda, image: keda}]
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-exporter
spec:
  template:
    spec:
      containers: [{name: x, image: x}]
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: nightly
spec:
  jobTemplate:
    spec:
      template:
        spec:
          containers: [{name: x, image: x}]
---
apiVersion: monitoring.coreos.com/v1
kind: Prometheus
metadata:
  name: monitoring-prometheus
spec:
  retention: 7d
`
	out, err := poolPostRenderer{pool: "platform"}.Run(bytes.NewBufferString(in))
	if err != nil {
		t.Fatal(err)
	}
	objs, err := decodeObjects(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 5 {
		t.Fatalf("%d objects out of 5", len(objs))
	}
	if !strings.Contains(out.String(), "# Source: keda/templates/service.yaml") {
		t.Errorf("the Service did not pass through as written:\n%s", out)
	}
	want := map[string]map[string]string{
		"Deployment": {"kubernetes.io/os": "linux", PoolLabel: "platform"},
		"DaemonSet":  nil,
		"CronJob":    {PoolLabel: "platform"},
		"Prometheus": {PoolLabel: "platform"},
		"Service":    nil,
	}
	for _, o := range objs {
		var sel map[string]string
		if path := podSelectorPath(o); path != nil {
			sel, _, _ = unstructured.NestedStringMap(o.Object, path...)
		}
		if w := want[o.GetKind()]; len(w) != len(sel) || (len(w) > 0 && !mapsEqual(w, sel)) {
			t.Errorf("%s node selector = %v, want %v", o.GetKind(), sel, w)
		}
	}
	if o := objs[2]; o.GetKind() == "DaemonSet" {
		if _, found, _ := unstructured.NestedMap(o.Object, "spec", "template", "spec", "nodeSelector"); found {
			t.Error("a DaemonSet got a node selector")
		}
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// poolEngine is an engine on a fake cluster with the given nodes, for the
// checks that read the cluster.
func poolEngine(t *testing.T, profile string, vars map[string]string, objs ...runtime.Object) (*Engine, *fake.Clientset) {
	t.Helper()
	eng, err := New(nil, Options{Profile: profile, Vars: vars, Reporter: &quiet{}})
	if err != nil {
		t.Fatal(err)
	}
	cs := fake.NewClientset(objs...)
	eng.kube = &kube.Client{Kube: cs}
	return eng, cs
}

func node(name, pool string) *corev1.Node {
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{}}}
	if pool != "" {
		n.Labels[PoolLabel] = pool
	}
	return n
}

// init refuses what would never be scheduled: a cloud profile with no
// platform pool named, or a pool with no node.
func TestInitRefusesAPoolWithNoNode(t *testing.T) {
	ctx := context.Background()
	base := map[string]string{VarDomain: "example.com"}

	eng, _ := poolEngine(t, "oci", base, node("10.0.1.10", "apps"))
	if err := eng.checkPools(ctx); err == nil || !strings.Contains(err.Error(), "shpyrd.io/pool=platform") {
		t.Errorf("no platform node: %v", err)
	}

	eng, _ = poolEngine(t, "oci", base, node("10.0.1.10", "apps"), node("10.0.1.118", "platform"))
	if err := eng.checkPools(ctx); err != nil {
		t.Errorf("a platform node: %v", err)
	}

	// An older vars file wrote an empty platform pool for a single pool.
	eng, _ = poolEngine(t, "oci", map[string]string{VarDomain: "example.com", VarPlatformPool: ""}, node("10.0.1.118", "platform"))
	if err := eng.checkPools(ctx); err == nil || !strings.Contains(err.Error(), VarPlatformPool) {
		t.Errorf("empty platform pool on oci: %v", err)
	}

	// kind: no pool, nothing to check.
	eng, _ = poolEngine(t, "local", map[string]string{VarDomain: "example.test"}, node("kind-control-plane", ""))
	if err := eng.checkPools(ctx); err != nil {
		t.Errorf("local: %v", err)
	}
}

// cluster status lists the platform's pods outside the platform pool, and
// nothing that belongs where it is, nor the provider's DNS.
func TestMisplacedPodsAreThePlatformsOutsideItsPool(t *testing.T) {
	ctx := context.Background()
	pod := func(ns, name, nodeName string, sel map[string]string, owner metav1.OwnerReference, labels map[string]string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: labels, OwnerReferences: []metav1.OwnerReference{owner}},
			Spec: corev1.PodSpec{NodeName: nodeName, NodeSelector: sel, Containers: []corev1.Container{{Name: "c", Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m")},
			}}}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		}
	}
	rs := func(name string) metav1.OwnerReference {
		return metav1.OwnerReference{Kind: "ReplicaSet", Name: name, Controller: ptr.To(true)}
	}
	ds := metav1.OwnerReference{Kind: "DaemonSet", Name: "node-exporter", Controller: ptr.To(true)}
	done := pod("kpack", "build-1", "10.0.1.10", nil, metav1.OwnerReference{Kind: "Job", Name: "build", Controller: ptr.To(true)}, nil)
	done.Status.Phase = corev1.PodSucceeded
	cs := fake.NewClientset(
		node("10.0.1.10", "apps"), node("10.0.1.118", "platform"), node("10.0.1.228", "data"),
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "p-shop", Labels: map[string]string{"shpyrd.io/project": "shop"}}},
		pod("keda", "keda-operator-6d9f-x1", "10.0.1.10", nil, rs("keda-operator-6d9f"), map[string]string{"pod-template-hash": "6d9f"}),
		pod("shpyrd-system", "shpyrd-server-7c4b-y2", "10.0.1.228", nil, rs("shpyrd-server-7c4b"), map[string]string{"pod-template-hash": "7c4b"}),
		pod("monitoring", "node-exporter-z3", "10.0.1.10", nil, ds, nil),
		pod("kube-system", "coredns-6d46-dmg5t", "10.0.1.10", nil, rs("coredns-6d46"), map[string]string{"pod-template-hash": "6d46"}),
		pod("p-shop", "web-1", "10.0.1.10", nil, rs("web-1"), nil),
		pod("shpyrd-system", "object-storage-1", "10.0.1.228", map[string]string{PoolLabel: "data"}, rs("object-storage-1"), nil),
		pod("cert-manager", "cert-manager-1", "10.0.1.118", nil, rs("cert-manager-1"), nil),
		done,
	)
	k := &kube.Client{Kube: cs}
	got, err := MisplacedPods(ctx, k, map[string]string{VarPlatformPool: "platform"})
	if err != nil {
		t.Fatal(err)
	}
	want := []MisplacedPod{
		{Namespace: "keda", Name: "keda-operator-6d9f-x1", Owner: "Deployment/keda-operator", Node: "10.0.1.10", Pool: "apps", CPU: "250m"},
		{Namespace: "shpyrd-system", Name: "shpyrd-server-7c4b-y2", Owner: "Deployment/shpyrd-server", Node: "10.0.1.228", Pool: "data", CPU: "250m"},
	}
	if a, b := dump(t, got), dump(t, want); a != b {
		t.Errorf("misplaced:\n%s\nwant:\n%s", a, b)
	}
	if got, _ := MisplacedPods(ctx, k, map[string]string{}); got != nil {
		t.Errorf("a cluster without a platform pool has nothing misplaced: %v", got)
	}
}

func dump(t *testing.T, v any) string {
	t.Helper()
	b, err := yaml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
