package install

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

const kindCorefile = `.:53 {
    errors
    health {
       lameduck 5s
    }
    ready
    kubernetes cluster.local in-addr.arpa ip6.arpa {
       pods insecure
       fallthrough in-addr.arpa ip6.arpa
       ttl 30
    }
    prometheus :9153
    forward . /etc/resolv.conf {
       max_concurrent 1000
    }
    cache 30
    loop
    reload
    loadbalance
}
`

func TestInternalNamesZoneIsAddedOnceAndFollowsTheDoor(t *testing.T) {
	first := WithInternalNames(kindCorefile, "10.96.1.2")
	if !strings.HasPrefix(first, kindCorefile) {
		t.Error("the cluster's own block must stay as it was")
	}
	if !strings.Contains(first, "internal:53 {") || !strings.Contains(first, `answer "{{ .Name }} 60 IN A 10.96.1.2"`) || !strings.Contains(first, "template IN ANY internal") {
		t.Errorf("block:\n%s", first)
	}
	// Only <project>.internal and <project>.<workspace>.internal are the
	// platform's; the rest of .internal (a node's search domain on AWS or
	// GCP) goes upstream, so a search for go.dev does not end at the door.
	block := first[strings.Index(first, internalNamesBegin):]
	if strings.Count(block, `match "^[^.]+\.([^.]+\.)?internal\.$"`) != 2 || strings.Count(block, "fallthrough") != 2 || !strings.Contains(block, "forward . /etc/resolv.conf") {
		t.Errorf("the templates must match one or two labels, fall through and forward the rest:\n%s", block)
	}
	if again := WithInternalNames(first, "10.96.1.2"); again != first {
		t.Errorf("applying the same address must change nothing:\n%s", again)
	}
	moved := WithInternalNames(first, "10.96.7.7")
	if strings.Count(moved, "internal:53 {") != 1 || !strings.Contains(moved, "10.96.7.7") || strings.Contains(moved, "10.96.1.2") {
		t.Errorf("a new address replaces the block, not adds one:\n%s", moved)
	}
}

func TestInternalNamesHookWritesCoreDNS(t *testing.T) {
	cs := kubefake.NewSimpleClientset(
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "coredns", Namespace: "kube-system"}, Data: map[string]string{"Corefile": kindCorefile}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "ingress-nginx-controller", Namespace: "ingress-nginx"}, Spec: corev1.ServiceSpec{ClusterIP: "10.96.0.9"}},
	)
	rep := &stepReporter{}
	e := &Engine{kube: &kube.Client{Kube: cs}, rep: rep}
	c := &Component{Name: "shpyrd"}
	ctx := context.Background()
	if err := internalNamesDNSHook(ctx, e, c); err != nil {
		t.Fatal(err)
	}
	cm, _ := cs.CoreV1().ConfigMaps("kube-system").Get(ctx, "coredns", metav1.GetOptions{})
	if !strings.Contains(cm.Data["Corefile"], "IN A 10.96.0.9") {
		t.Errorf("one front door: its address\n%s", cm.Data["Corefile"])
	}
	// An internal controller takes precedence once it is there.
	if _, err := cs.CoreV1().Services("ingress-nginx-internal").Create(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "ingress-nginx-internal-controller", Namespace: "ingress-nginx-internal"}, Spec: corev1.ServiceSpec{ClusterIP: "10.96.0.10"}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := internalNamesDNSHook(ctx, e, c); err != nil {
		t.Fatal(err)
	}
	cm, _ = cs.CoreV1().ConfigMaps("kube-system").Get(ctx, "coredns", metav1.GetOptions{})
	if !strings.Contains(cm.Data["Corefile"], "IN A 10.96.0.10") || strings.Contains(cm.Data["Corefile"], "10.96.0.9") {
		t.Errorf("the internal door:\n%s", cm.Data["Corefile"])
	}
	// No CoreDNS: a line for the operator, no error.
	none := kubefake.NewSimpleClientset(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "ingress-nginx-controller", Namespace: "ingress-nginx"}, Spec: corev1.ServiceSpec{ClusterIP: "10.96.0.9"}})
	rep.steps = nil
	e.kube = &kube.Client{Kube: none}
	if err := internalNamesDNSHook(ctx, e, c); err != nil {
		t.Fatal(err)
	}
	if len(rep.steps) != 1 || !strings.Contains(rep.steps[0], "no coredns ConfigMap") || !strings.Contains(rep.steps[0], "10.96.0.9") {
		t.Errorf("steps = %v", rep.steps)
	}
}
