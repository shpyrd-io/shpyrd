package install

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"helm.sh/helm/v3/pkg/postrender"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// Node pools (RFC-0077). A cloud cluster's nodes carry the label
// shpyrd.io/pool: platform, apps or data. The controller pins what it runs
// for projects (processes and builds to apps, databases and stores to
// data); the installer pins what it installs: every pod a component makes
// runs on the component's pool, through a node selector added to what the
// component renders, Helm's or Kustomize's. DaemonSets are left as they
// are: they run on every node by design. Without a pool (kind, or an empty
// SHPYRD_PLATFORM_POOL) nothing gets a selector.

// PoolLabel is the node label naming a node pool.
const PoolLabel = "shpyrd.io/pool"

// The pools a component may ask for (component.yaml `pool`).
const (
	PoolPlatform = "platform"
	PoolData     = "data"
)

// DataPool is the label value of the pool the projects' data runs on: the
// data pool, or the platform pool on a cluster without one. get reads an
// install variable.
func DataPool(get func(string) string) string {
	if p := get(VarDataPool); p != "" {
		return p
	}
	return get(VarPlatformPool)
}

// poolOf is the label value of the pool the component's pods run on, ""
// when they may run anywhere.
func (e *Engine) poolOf(c *Component) string {
	if c.Pool == PoolData {
		return DataPool(func(k string) string { return e.vars[k] })
	}
	return e.vars[VarPlatformPool]
}

// postRenderer pins a chart's pods to the component's pool, nil when it
// has none. Helm keeps hooks out of post-rendering: a hook Job runs once,
// wherever it lands.
func (e *Engine) postRenderer(c *Component) postrender.PostRenderer {
	if p := e.poolOf(c); p != "" {
		return poolPostRenderer{pool: p}
	}
	return nil
}

// podSelectorPath is where an object keeps the node selector of the pods
// it makes; nil for objects that make none, and for DaemonSets.
func podSelectorPath(o *unstructured.Unstructured) []string {
	switch o.GroupVersionKind().GroupKind() {
	case schema.GroupKind{Group: "apps", Kind: "Deployment"},
		schema.GroupKind{Group: "apps", Kind: "StatefulSet"},
		schema.GroupKind{Group: "apps", Kind: "ReplicaSet"},
		schema.GroupKind{Group: "batch", Kind: "Job"}:
		return []string{"spec", "template", "spec", "nodeSelector"}
	case schema.GroupKind{Group: "batch", Kind: "CronJob"}:
		return []string{"spec", "jobTemplate", "spec", "template", "spec", "nodeSelector"}
	case schema.GroupKind{Kind: "Pod"},
		// The Prometheus operator makes their StatefulSets from these.
		schema.GroupKind{Group: "monitoring.coreos.com", Kind: "Prometheus"},
		schema.GroupKind{Group: "monitoring.coreos.com", Kind: "Alertmanager"}:
		return []string{"spec", "nodeSelector"}
	}
	return nil
}

// pinToPool adds the pool to the node selector of every pod the objects
// make, next to the selectors already there. Nothing changes when pool is
// "".
func pinToPool(objs []*unstructured.Unstructured, pool string) error {
	if pool == "" {
		return nil
	}
	for _, o := range objs {
		path := podSelectorPath(o)
		if path == nil {
			continue
		}
		sel, _, err := unstructured.NestedStringMap(o.Object, path...)
		if err != nil {
			return fmt.Errorf("%s %s: node selector: %w", o.GetKind(), o.GetName(), err)
		}
		if sel == nil {
			sel = map[string]string{}
		}
		sel[PoolLabel] = pool
		if err := unstructured.SetNestedStringMap(o.Object, sel, path...); err != nil {
			return fmt.Errorf("%s %s: node selector: %w", o.GetKind(), o.GetName(), err)
		}
	}
	return nil
}

// poolPostRenderer is pinToPool as a Helm post-renderer. Documents that
// make no pods pass through as Helm wrote them.
type poolPostRenderer struct{ pool string }

func (p poolPostRenderer) Run(in *bytes.Buffer) (*bytes.Buffer, error) {
	out := &bytes.Buffer{}
	reader := utilyaml.NewYAMLReader(bufio.NewReader(in))
	for {
		doc, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("pin to pool %s: %w", p.pool, err)
		}
		var m map[string]interface{}
		if err := yaml.Unmarshal(doc, &m); err != nil {
			return nil, fmt.Errorf("pin to pool %s: %w", p.pool, err)
		}
		if o := (&unstructured.Unstructured{Object: m}); len(m) > 0 && podSelectorPath(o) != nil {
			if err := pinToPool([]*unstructured.Unstructured{o}, p.pool); err != nil {
				return nil, err
			}
			if doc, err = yaml.Marshal(o.Object); err != nil {
				return nil, err
			}
		}
		out.WriteString("---\n")
		out.Write(doc)
		if !bytes.HasSuffix(doc, []byte("\n")) {
			out.WriteString("\n")
		}
	}
	return out, nil
}

// checkPools refuses to install what would never be scheduled: a profile
// that runs the platform on a pool of its own with no pool named, or a
// pool the selected components are pinned to that has no node.
func (e *Engine) checkPools(ctx context.Context) error {
	if want := e.profile.Vars[VarPlatformPool]; want != "" && e.vars[VarPlatformPool] == "" {
		return fmt.Errorf("the %s profile runs the platform on a node pool of its own and %s is empty (an older --vars-file?): set it to the pool's %s label, %q (RFC-0077)", e.profile.Name, VarPlatformPool, PoolLabel, want)
	}
	pinned := map[string][]string{} // pool → components
	for _, rl := range e.profile.Runlevels {
		for _, c := range e.selected(rl) {
			if p := e.poolOf(c); p != "" {
				pinned[p] = append(pinned[p], c.Name)
			}
		}
	}
	pools := make([]string, 0, len(pinned))
	for p := range pinned {
		pools = append(pools, p)
	}
	sort.Strings(pools)
	for _, p := range pools {
		nodes, err := e.kube.Kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: PoolLabel + "=" + p})
		if err != nil {
			return fmt.Errorf("node pools: %w", err)
		}
		if len(nodes.Items) == 0 {
			return fmt.Errorf("no node is labelled %s=%s, where %s would run (RFC-0077): contrib/*/terraform labels its node pools; on another cluster, label the nodes (kubectl label node <name> %s=%s)", PoolLabel, p, strings.Join(pinned[p], ", "), PoolLabel, p)
		}
	}
	return nil
}

// providerDNS are the Deployments of the DNS the provider installs in
// kube-system: CoreDNS and, on OKE, the autoscaler of its replicas. They
// stay where the provider puts them, spread over every node: OKE
// reconciles them on a basic cluster, and the cluster's names should not
// hang on the platform pool alone. Not counted as misplaced.
var providerDNS = map[string]bool{"Deployment/coredns": true, "Deployment/kube-dns-autoscaler": true}

// MisplacedPod is a pod of the platform running outside the platform pool,
// where no selector of its own put it.
type MisplacedPod struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// Owner is what made it, "Deployment/keda-operator": a rollout restart
	// of it moves the pod once it is pinned.
	Owner string `json:"owner,omitempty"`
	Node  string `json:"node"`
	Pool  string `json:"pool"`
	// CPU is what it reserves on that node, "250m".
	CPU string `json:"cpu,omitempty"`
}

// MisplacedPods lists the pods of the platform running on an apps or data
// node without a node selector asking for that pool: what takes room and
// pods from the projects there, and keeps the autoscaler from draining an
// idle node. Projects' pods (their namespaces carry shpyrd.io/project),
// DaemonSets, the provider's DNS and finished pods are not counted. Nil on
// a cluster without a platform pool. vars are the installed variables.
func MisplacedPods(ctx context.Context, k *kube.Client, vars map[string]string) ([]MisplacedPod, error) {
	platform := vars[VarPlatformPool]
	if platform == "" {
		return nil, nil
	}
	nodes, err := k.Kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: PoolLabel})
	if err != nil {
		return nil, err
	}
	projects, err := k.Kube.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: "shpyrd.io/project"})
	if err != nil {
		return nil, err
	}
	project := map[string]bool{}
	for _, ns := range projects.Items {
		project[ns.Name] = true
	}
	poolOf := map[string]string{} // node → its pool, other than the platform's
	for _, n := range nodes.Items {
		if pool := n.Labels[PoolLabel]; pool != "" && pool != platform {
			poolOf[n.Name] = pool
		}
	}
	if len(poolOf) == 0 {
		return nil, nil
	}
	pods, err := k.Kube.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var out []MisplacedPod
	for _, p := range pods.Items {
		pool := poolOf[p.Spec.NodeName]
		if pool == "" || project[p.Namespace] || p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		if p.Spec.NodeSelector[PoolLabel] == pool {
			continue // it asked for this pool
		}
		owner := podOwner(p)
		if strings.HasPrefix(owner, "DaemonSet/") || (p.Namespace == "kube-system" && providerDNS[owner]) {
			continue
		}
		out = append(out, MisplacedPod{Namespace: p.Namespace, Name: p.Name, Owner: owner, Node: p.Spec.NodeName, Pool: pool, CPU: podCPU(p)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Node != out[j].Node {
			return out[i].Node < out[j].Node
		}
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// podOwner names what made the pod, the Deployment rather than its
// ReplicaSet.
func podOwner(p corev1.Pod) string {
	for _, ref := range p.OwnerReferences {
		if ref.Controller == nil || !*ref.Controller {
			continue
		}
		if hash := p.Labels["pod-template-hash"]; ref.Kind == "ReplicaSet" && hash != "" && strings.HasSuffix(ref.Name, "-"+hash) {
			return "Deployment/" + strings.TrimSuffix(ref.Name, "-"+hash)
		}
		return ref.Kind + "/" + ref.Name
	}
	return ""
}

// podCPU is the CPU the pod's containers request, "" when none.
func podCPU(p corev1.Pod) string {
	var total int64
	for _, c := range p.Spec.Containers {
		total += c.Resources.Requests.Cpu().MilliValue()
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("%dm", total)
}
