package api

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

func indexPod(ns, name, ip string, phase corev1.PodPhase, labels map[string]string) *corev1.Pod {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: labels}}
	p.Status.Phase = phase
	p.Status.PodIPs = []corev1.PodIP{{IP: ip}}
	return p
}

func newTestIndex() *podIndex {
	x := newPodIndex(nil, "shpyrd-system")
	x.putNamespace(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "p-shop", Labels: map[string]string{shpyrdv1.LabelProject: "shop", shpyrdv1.LabelWorkspace: "acme"}}})
	x.putNamespace(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "p-agent", Labels: map[string]string{shpyrdv1.LabelProject: "agent", shpyrdv1.LabelWorkspace: "platform"}}})
	return x
}

var serverLabels = map[string]string{"app.kubernetes.io/name": "shpyrd-server"}

// A finished pod keeps its last address in its status after the network has
// handed it to a live pod. Whatever order the informer delivers them in, the
// address belongs to the live pod: here the platform's server, whose
// forwarded calls were judged as calls from another workspace's project.
func TestFinishedPodDoesNotClaimALiveAddress(t *testing.T) {
	for _, phase := range []corev1.PodPhase{corev1.PodFailed, corev1.PodSucceeded} {
		for _, finishedLast := range []bool{false, true} {
			x := newTestIndex()
			server := indexPod("shpyrd-system", "shpyrd-server-a", "10.0.5.66", corev1.PodRunning, serverLabels)
			finished := indexPod("p-shop", "psql-db", "10.0.5.66", phase, nil)
			if finishedLast {
				x.putPod(server)
				x.putPod(finished)
			} else {
				x.putPod(finished)
				x.putPod(server)
			}
			if c, ok := x.Lookup("10.0.5.66"); !ok || !c.Platform {
				t.Errorf("%s pod, put last=%v: 10.0.5.66 is %+v %v, want the platform", phase, finishedLast, c, ok)
			}
			// The finished pod going away leaves the live pod's address alone.
			x.dropPod(finished)
			if c, ok := x.Lookup("10.0.5.66"); !ok || !c.Platform {
				t.Errorf("%s pod deleted: 10.0.5.66 is %+v %v, want the platform", phase, c, ok)
			}
		}
	}
}

// A pod that finishes gives its address back at once.
func TestPodThatFinishesIsForgotten(t *testing.T) {
	x := newTestIndex()
	build := indexPod("p-shop", "build-1", "10.0.7.10", corev1.PodRunning, nil)
	x.putPod(build)
	if c, ok := x.Lookup("10.0.7.10"); !ok || c.Project != "shop" {
		t.Fatalf("running pod: %+v %v", c, ok)
	}
	build.Status.Phase = corev1.PodSucceeded
	x.putPod(build)
	if c, ok := x.Lookup("10.0.7.10"); ok {
		t.Errorf("finished pod still answers for its address: %+v", c)
	}
}

// When an address moves to a new pod before the old pod's last update
// arrives, the old pod's update or deletion must not take it away.
func TestStaleUpdateLeavesTheNewOwnersAddress(t *testing.T) {
	x := newTestIndex()
	old := indexPod("p-shop", "web-old", "10.0.7.194", corev1.PodRunning, nil)
	x.putPod(old)
	x.putPod(indexPod("p-agent", "web-new", "10.0.7.194", corev1.PodRunning, nil))

	old.Status.Phase = corev1.PodFailed
	x.putPod(old)
	x.dropPod(cache.DeletedFinalStateUnknown{Key: "p-shop/web-old", Obj: old})

	if c, ok := x.Lookup("10.0.7.194"); !ok || c.Project != "agent" || c.Workspace != "platform" {
		t.Errorf("10.0.7.194 is %+v %v, want svc:platform/agent", c, ok)
	}
}

// Pods on the host's network share the node's address: none of them is
// the caller behind it.
func TestHostNetworkPodsAreNotCallers(t *testing.T) {
	x := newTestIndex()
	p := indexPod("p-shop", "agent", "10.0.1.120", corev1.PodRunning, nil)
	p.Spec.HostNetwork = true
	x.putPod(p)
	if c, ok := x.Lookup("10.0.1.120"); ok {
		t.Errorf("host-network pod claimed the node's address: %+v", c)
	}
}
