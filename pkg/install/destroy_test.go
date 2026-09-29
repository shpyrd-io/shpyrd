package install

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// Destroy removes the platform in the order that lets operators finish:
// projects, the system namespace, then the aggregated APIs and webhooks
// pointing into our namespaces, the components' namespaces and CRDs; what
// the hooks left in kube-system; and it keeps what it was told to keep.
func TestDestroyOrderAndScope(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "p-abc", Labels: map[string]string{"shpyrd.io/project": "shop"}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "shpyrd-system"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cert-manager"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "calico-system"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cluster-autoscaler-oci", Namespace: "kube-system"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "coredns", Namespace: "kube-system"}},
	)
	apiServiceGVR := schema.GroupVersionResource{Group: "apiregistration.k8s.io", Version: "v1", Resource: "apiservices"}
	webhookGVR := schema.GroupVersionResource{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "validatingwebhookconfigurations"}
	mutatingGVR := schema.GroupVersionResource{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "mutatingwebhookconfigurations"}
	crdGVR := schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
	obj := func(gv, kind, name string, spec map[string]interface{}) *unstructured.Unstructured {
		u := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": gv, "kind": kind, "metadata": map[string]interface{}{"name": name}}}
		for k, v := range spec {
			u.Object[k] = v
		}
		return u
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		apiServiceGVR: "APIServiceList", webhookGVR: "ValidatingWebhookConfigurationList", mutatingGVR: "MutatingWebhookConfigurationList", crdGVR: "CustomResourceDefinitionList",
	},
		obj("apiregistration.k8s.io/v1", "APIService", "v1beta1.external.metrics.k8s.io", map[string]interface{}{"spec": map[string]interface{}{"service": map[string]interface{}{"namespace": "keda", "name": "metrics"}}}),
		obj("apiregistration.k8s.io/v1", "APIService", "v1.", map[string]interface{}{"spec": map[string]interface{}{}}),
		obj("admissionregistration.k8s.io/v1", "ValidatingWebhookConfiguration", "cert-manager-webhook", map[string]interface{}{"webhooks": []interface{}{map[string]interface{}{"clientConfig": map[string]interface{}{"service": map[string]interface{}{"namespace": "cert-manager"}}}}}),
		obj("admissionregistration.k8s.io/v1", "ValidatingWebhookConfiguration", "oke-resource-leak-protection.oke.com", map[string]interface{}{"webhooks": []interface{}{map[string]interface{}{"clientConfig": map[string]interface{}{"url": "https://oke"}}}}),
		obj("apiextensions.k8s.io/v1", "CustomResourceDefinition", "certificates.cert-manager.io", map[string]interface{}{"spec": map[string]interface{}{"group": "cert-manager.io"}}),
		obj("apiextensions.k8s.io/v1", "CustomResourceDefinition", "connectors.dex.coreos.com", map[string]interface{}{"spec": map[string]interface{}{"group": "dex.coreos.com"}}),
		obj("apiextensions.k8s.io/v1", "CustomResourceDefinition", "ippools.crd.projectcalico.org", map[string]interface{}{"spec": map[string]interface{}{"group": "crd.projectcalico.org"}}),
	)
	k := &kube.Client{Kube: cs, Dynamic: dyn, Namespace: "shpyrd-system"}
	e := &Engine{
		kube: k, rep: nopReporter{}, vars: map[string]string{VarSystemNS: "shpyrd-system"},
		helm:    &helmClient{},
		applier: &applier{kube: k},
		profile: &Profile{Runlevels: []Runlevel{{Name: "rc1", Components: []string{"network-policy", "cert-manager", "keda", "shpyrd"}}}},
		components: map[string]*Component{
			"network-policy": {Name: "network-policy", Namespace: "calico-system"},
			"cert-manager":   {Name: "cert-manager", Namespace: "cert-manager"},
			"keda":           {Name: "keda", Namespace: "keda"},
			"shpyrd":         {Name: "shpyrd", Namespace: "shpyrd-system"},
		},
	}
	cleared, err := e.Destroy(ctx, DestroyOptions{Keep: []string{"network-policy"}, Wait: time.Second})
	if err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if len(cleared) != 0 {
		t.Errorf("cleared finalizers on a clean cluster: %v", cleared)
	}
	// Our namespaces are gone; Calico's and kube-system stay.
	for _, ns := range []string{"p-abc", "shpyrd-system", "cert-manager"} {
		if _, err := cs.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{}); err == nil {
			t.Errorf("namespace %s survived", ns)
		}
	}
	for _, ns := range []string{"calico-system", "kube-system"} {
		if _, err := cs.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{}); err != nil {
			t.Errorf("namespace %s must stay: %v", ns, err)
		}
	}
	// Aggregated APIs and webhooks into our namespaces are gone; the
	// cluster's own stay.
	if _, err := dyn.Resource(apiServiceGVR).Get(ctx, "v1beta1.external.metrics.k8s.io", metav1.GetOptions{}); err == nil {
		t.Error("keda's APIService survived")
	}
	if _, err := dyn.Resource(webhookGVR).Get(ctx, "cert-manager-webhook", metav1.GetOptions{}); err == nil {
		t.Error("cert-manager's webhook survived")
	}
	if _, err := dyn.Resource(webhookGVR).Get(ctx, "oke-resource-leak-protection.oke.com", metav1.GetOptions{}); err != nil {
		t.Error("OKE's own webhook must stay")
	}
	// CRDs of our groups are gone; Calico's stay.
	for _, name := range []string{"certificates.cert-manager.io", "connectors.dex.coreos.com"} {
		if _, err := dyn.Resource(crdGVR).Get(ctx, name, metav1.GetOptions{}); err == nil {
			t.Errorf("CRD %s survived", name)
		}
	}
	if _, err := dyn.Resource(crdGVR).Get(ctx, "ippools.crd.projectcalico.org", metav1.GetOptions{}); err != nil {
		t.Error("Calico's CRD must stay")
	}
	// What the hooks left in kube-system is gone; the cluster's own stays.
	if _, err := cs.CoreV1().Secrets("kube-system").Get(ctx, "cluster-autoscaler-oci", metav1.GetOptions{}); err == nil {
		t.Error("the autoscaler's credentials survived")
	}
	if _, err := cs.CoreV1().ConfigMaps("kube-system").Get(ctx, "coredns", metav1.GetOptions{}); err != nil {
		t.Error("coredns must stay")
	}
}
