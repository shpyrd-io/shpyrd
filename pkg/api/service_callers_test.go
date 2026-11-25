package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// The rule the index follows: only a live pod is a caller; an address
// reported by live pods of different callers identifies none.

const callerTestSystemNS = "shpyrd-system"

func callerNS(name, workspace, project string) *corev1.Namespace {
	labels := map[string]string{}
	if project != "" {
		labels[shpyrdv1.LabelProject] = project
	}
	if workspace != "" {
		labels[shpyrdv1.LabelWorkspace] = workspace
	}
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

func callerPod(ns, name string, phase corev1.PodPhase, ips ...string) *corev1.Pod {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	p.Status.Phase = phase
	for _, ip := range ips {
		p.Status.PodIPs = append(p.Status.PodIPs, corev1.PodIP{IP: ip})
	}
	if len(ips) > 0 {
		p.Status.PodIP = ips[0]
	}
	return p
}

// callerCluster is an index running against a fake cluster, the way the
// server runs it.
type callerCluster struct {
	t   *testing.T
	cs  *kubefake.Clientset
	x   *podIndex
	ctx context.Context
	n   int
}

func startCallerCluster(t *testing.T, objs ...runtime.Object) *callerCluster {
	t.Helper()
	c := &callerCluster{t: t, cs: kubefake.NewSimpleClientset(objs...)}
	c.x = newPodIndex(c.cs, callerTestSystemNS)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.ctx = ctx
	c.x.Run(ctx)
	return c
}

// settle waits until every event sent so far has reached the index: events
// arrive in order, so a marker pod that resolves comes after all before it.
func (c *callerCluster) settle() {
	c.t.Helper()
	c.n++
	ip := fmt.Sprintf("10.250.0.%d", c.n)
	name := fmt.Sprintf("marker-%d", c.n)
	if _, err := c.cs.CoreV1().Pods("ns-a").Create(c.ctx, callerPod("ns-a", name, corev1.PodRunning, ip), metav1.CreateOptions{}); err != nil {
		c.t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := c.x.Lookup(ip); ok {
			return
		}
		if time.Now().After(deadline) {
			c.t.Fatal("the index did not catch up")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (c *callerCluster) create(p *corev1.Pod) {
	c.t.Helper()
	if _, err := c.cs.CoreV1().Pods(p.Namespace).Create(c.ctx, p, metav1.CreateOptions{}); err != nil {
		c.t.Fatal(err)
	}
}

func (c *callerCluster) update(p *corev1.Pod) {
	c.t.Helper()
	if _, err := c.cs.CoreV1().Pods(p.Namespace).Update(c.ctx, p, metav1.UpdateOptions{}); err != nil {
		c.t.Fatal(err)
	}
}

func (c *callerCluster) remove(ns, name string) {
	c.t.Helper()
	if err := c.cs.CoreV1().Pods(ns).Delete(c.ctx, name, metav1.DeleteOptions{}); err != nil {
		c.t.Fatal(err)
	}
}

func (c *callerCluster) wantCaller(ip string, want serviceCaller) {
	c.t.Helper()
	c.settle()
	if got, ok := c.x.Lookup(ip); !ok || got != want {
		c.t.Errorf("%s = %+v %v, want %+v", ip, got, ok, want)
	}
}

func (c *callerCluster) wantNobody(ip string) {
	c.t.Helper()
	c.settle()
	if got, ok := c.x.Lookup(ip); ok {
		c.t.Errorf("%s = %+v, want nobody", ip, got)
	}
}

var (
	callerP1 = serviceCaller{Workspace: "w1", Project: "p1"}
	callerP2 = serviceCaller{Workspace: "w2", Project: "p2"}
)

func projectNamespaces() []runtime.Object {
	return []runtime.Object{
		callerNS("ns-a", "w1", "p1"),
		callerNS("ns-b", "w2", "p2"),
		callerNS("ns-c", "", "p3"),
		callerNS("ns-none", "", ""),
	}
}

func TestPodIndexEndedPodIsNeverACaller(t *testing.T) {
	objs := append(projectNamespaces(),
		callerPod("ns-a", "done", corev1.PodSucceeded, "10.0.0.1"),
		callerPod("ns-a", "broken", corev1.PodFailed, "10.0.0.2"),
	)
	c := startCallerCluster(t, objs...)
	c.wantNobody("10.0.0.1")
	c.wantNobody("10.0.0.2")
	c.create(callerPod("ns-a", "done-2", corev1.PodSucceeded, "10.0.0.3"))
	c.create(callerPod("ns-a", "broken-2", corev1.PodFailed, "10.0.0.4"))
	c.wantNobody("10.0.0.3")
	c.wantNobody("10.0.0.4")
}

func TestPodIndexLivePodKeepsItsAddressWhateverTheOrder(t *testing.T) {
	const ip = "10.0.1.1"
	ended := func() *corev1.Pod { return callerPod("ns-a", "old", corev1.PodSucceeded, ip) }
	live := func() *corev1.Pod { return callerPod("ns-b", "new", corev1.PodRunning, ip) }

	t.Run("ended first", func(t *testing.T) {
		c := startCallerCluster(t, projectNamespaces()...)
		c.create(ended())
		c.create(live())
		c.wantCaller(ip, callerP2)
	})
	t.Run("live first", func(t *testing.T) {
		c := startCallerCluster(t, projectNamespaces()...)
		c.create(live())
		c.create(ended())
		c.wantCaller(ip, callerP2)
	})
	t.Run("both before the index starts", func(t *testing.T) {
		c := startCallerCluster(t, append(projectNamespaces(), ended(), live())...)
		c.wantCaller(ip, callerP2)
	})
	t.Run("ended pod updated again", func(t *testing.T) {
		c := startCallerCluster(t, projectNamespaces()...)
		c.create(ended())
		c.create(live())
		c.wantCaller(ip, callerP2)
		e := ended()
		e.Labels = map[string]string{"touched": "yes"}
		e.Annotations = map[string]string{"touched": "yes"}
		c.update(e)
		c.wantCaller(ip, callerP2)
	})
}

func TestPodIndexRemovingAnEndedPodKeepsTheLivePod(t *testing.T) {
	const ip = "10.0.2.1"
	t.Run("different namespaces", func(t *testing.T) {
		c := startCallerCluster(t, projectNamespaces()...)
		c.create(callerPod("ns-a", "old", corev1.PodFailed, ip))
		c.create(callerPod("ns-b", "new", corev1.PodRunning, ip))
		c.remove("ns-a", "old")
		c.wantCaller(ip, callerP2)
	})
	t.Run("same namespace", func(t *testing.T) {
		c := startCallerCluster(t, projectNamespaces()...)
		c.create(callerPod("ns-b", "old", corev1.PodSucceeded, ip))
		c.create(callerPod("ns-b", "new", corev1.PodRunning, ip))
		c.remove("ns-b", "old")
		c.wantCaller(ip, callerP2)
	})
}

func TestPodIndexAPodThatEndsStopsBeingACaller(t *testing.T) {
	const ip = "10.0.3.1"
	for _, phase := range []corev1.PodPhase{corev1.PodSucceeded, corev1.PodFailed} {
		t.Run(string(phase)+" alone", func(t *testing.T) {
			c := startCallerCluster(t, projectNamespaces()...)
			c.create(callerPod("ns-a", "web", corev1.PodRunning, ip))
			c.wantCaller(ip, callerP1)
			c.update(callerPod("ns-a", "web", phase, ip))
			c.wantNobody(ip)
		})
		t.Run(string(phase)+" with another project's live pod created before", func(t *testing.T) {
			c := startCallerCluster(t, projectNamespaces()...)
			c.create(callerPod("ns-b", "other", corev1.PodRunning, ip))
			c.create(callerPod("ns-a", "web", corev1.PodRunning, ip))
			c.update(callerPod("ns-a", "web", phase, ip))
			c.wantCaller(ip, callerP2)
		})
		t.Run(string(phase)+" with another project's live pod created after", func(t *testing.T) {
			c := startCallerCluster(t, projectNamespaces()...)
			c.create(callerPod("ns-a", "web", corev1.PodRunning, ip))
			c.update(callerPod("ns-a", "web", phase, ip))
			c.create(callerPod("ns-b", "other", corev1.PodRunning, ip))
			c.wantCaller(ip, callerP2)
		})
	}
}

func TestPodIndexRemovingTheLivePodFreesTheAddress(t *testing.T) {
	const ip = "10.0.4.1"
	c := startCallerCluster(t, projectNamespaces()...)
	c.create(callerPod("ns-a", "old", corev1.PodSucceeded, ip))
	c.create(callerPod("ns-b", "new", corev1.PodRunning, ip))
	c.wantCaller(ip, callerP2)
	c.remove("ns-b", "new")
	c.wantNobody(ip)
}

func TestPodIndexLivePodsOfDifferentCallersIdentifyNone(t *testing.T) {
	const ip = "10.0.5.1"
	index := func() *podIndex {
		x := newPodIndex(nil, callerTestSystemNS)
		for _, o := range projectNamespaces() {
			x.putNamespace(o)
		}
		return x
	}
	server := func(name string) *corev1.Pod {
		p := callerPod(callerTestSystemNS, name, corev1.PodRunning, ip)
		p.Labels = map[string]string{"app.kubernetes.io/name": "shpyrd-server"}
		return p
	}
	a1 := callerPod("ns-a", "a1", corev1.PodRunning, ip)
	a2 := callerPod("ns-a", "a2", corev1.PodRunning, ip)
	b1 := callerPod("ns-b", "b1", corev1.PodRunning, ip)
	none := callerPod("ns-none", "n1", corev1.PodRunning, ip)

	lookup := func(x *podIndex) (serviceCaller, bool) { return x.Lookup(ip) }
	for _, order := range [][]*corev1.Pod{{a1, b1}, {b1, a1}} {
		x := index()
		for _, p := range order {
			x.putPod(p)
		}
		if got, ok := lookup(x); ok {
			t.Errorf("pods of two projects: %+v, want nobody (order %s,%s)", got, order[0].Name, order[1].Name)
		}
		// Once one is gone, the remaining one is the caller.
		x.dropPod(order[0])
		want := map[string]serviceCaller{"a1": callerP1, "b1": callerP2}[order[1].Name]
		if got, ok := lookup(x); !ok || got != want {
			t.Errorf("remaining %s = %+v %v, want %+v", order[1].Name, got, ok, want)
		}
	}
	// Ending one of two leaves the other.
	x := index()
	x.putPod(a1)
	x.putPod(b1)
	x.putPod(callerPod("ns-a", "a1", corev1.PodFailed, ip))
	if got, ok := lookup(x); !ok || got != callerP2 {
		t.Errorf("after one ended = %+v %v, want %+v", got, ok, callerP2)
	}

	// Several pods of one project are that project.
	for _, order := range [][]*corev1.Pod{{a1, a2}, {a2, a1}} {
		x := index()
		for _, p := range order {
			x.putPod(p)
		}
		if got, ok := lookup(x); !ok || got != callerP1 {
			t.Errorf("two pods of one project = %+v %v, want %+v", got, ok, callerP1)
		}
	}
	// Several server pods are the platform.
	x = index()
	x.putPod(server("s1"))
	x.putPod(server("s2"))
	if got, ok := lookup(x); !ok || !got.Platform {
		t.Errorf("two server pods = %+v %v, want the platform", got, ok)
	}
	// The server and a project, and a project and an unlabelled namespace, are different callers.
	for _, pair := range [][2]*corev1.Pod{{server("s1"), a1}, {a1, server("s1")}, {a1, none}, {none, a1}} {
		x := index()
		x.putPod(pair[0])
		x.putPod(pair[1])
		if got, ok := lookup(x); ok {
			t.Errorf("%s with %s = %+v, want nobody", pair[0].Name, pair[1].Name, got)
		}
	}
}

func TestPodIndexSinglePod(t *testing.T) {
	server := callerPod(callerTestSystemNS, "shpyrd-server-0", corev1.PodRunning, "10.0.6.1")
	server.Labels = map[string]string{"app.kubernetes.io/name": "shpyrd-server"}
	other := callerPod(callerTestSystemNS, "registry-0", corev1.PodRunning, "10.0.6.2")
	other.Labels = map[string]string{"app.kubernetes.io/name": "registry"}
	objs := append(projectNamespaces(),
		callerPod("ns-a", "web", corev1.PodRunning, "10.0.6.3"),
		callerPod("ns-c", "worker", corev1.PodPending, "10.0.6.4"), // no workspace label: the default one
		callerPod("ns-none", "stray", corev1.PodRunning, "10.0.6.5"),
		callerPod("ns-a", "dual", corev1.PodRunning, "10.0.6.6", "fd00::6"),
		server, other,
	)
	c := startCallerCluster(t, objs...)
	c.wantCaller("10.0.6.3", callerP1)
	c.wantCaller("10.0.6.4", serviceCaller{Workspace: "default", Project: "p3"})
	c.wantCaller("10.0.6.1", serviceCaller{Platform: true})
	c.wantNobody("10.0.6.2")
	c.wantNobody("10.0.6.5")
	c.wantCaller("10.0.6.6", callerP1)
	c.wantCaller("fd00::6", callerP1)

	// A changed address: the old one stops resolving, the new one resolves.
	c.update(callerPod("ns-a", "web", corev1.PodRunning, "10.0.6.7"))
	c.wantNobody("10.0.6.3")
	c.wantCaller("10.0.6.7", callerP1)

	// A deleted pod takes its addresses with it.
	c.remove("ns-a", "dual")
	c.wantNobody("10.0.6.6")
	c.wantNobody("fd00::6")
}
