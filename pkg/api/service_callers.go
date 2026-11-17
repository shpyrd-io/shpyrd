package api

import (
	"context"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Internal names (#311): a project calls another of its workspace at
// http://<project>.internal, and the callee learns who called. The front
// door knows the caller by the address the call came from, a pod of exactly
// one project: this index keeps every pod address of the cluster mapped to
// the project whose namespace holds the pod, from the namespaces' labels,
// which the controller writes and no project can.

// serviceCaller is who an internal call comes from: a project of a
// workspace, or the platform's own server.
type serviceCaller struct {
	Workspace string
	Project   string
	Platform  bool
}

// Service is the caller's identity as the callee reads it, svc:<ws>/<project>.
func (c serviceCaller) Service() string { return "svc:" + c.Workspace + "/" + c.Project }

// callerIndex answers who a pod address belongs to.
type callerIndex interface {
	Lookup(ip string) (serviceCaller, bool)
}

// callerMap is a fixed index (tests).
type callerMap map[string]serviceCaller

func (m callerMap) Lookup(ip string) (serviceCaller, bool) {
	c, ok := m[ip]
	return c, ok
}

// podIndex is the live index: informers over pods and project namespaces.
type podIndex struct {
	kube     kubernetes.Interface
	systemNS string

	mu         sync.RWMutex
	pods       map[string]podEntry      // by address
	addresses  map[string][]string      // namespace/name → its addresses, to forget them when the pod goes
	namespaces map[string]serviceCaller // project namespace → its project
}

// podEntry is what the index keeps of a pod: which pod holds the address
// (namespace/name), where it is, and whether it is the platform's server.
type podEntry struct {
	key       string
	namespace string
	server    bool
}

// holdsAddresses says whether a pod still owns the addresses in its status.
// A pod that finished (Succeeded or Failed) keeps its last address in its
// status after the network has given it back, and the next pod scheduled on
// that node may get it: a finished build or job would otherwise claim the
// address of a live pod, the platform's server included, and calls from it
// would be judged as calls from the finished pod's project. A pod on the
// host's network has the node's address, shared by every such pod there:
// it says nothing about who is calling.
func holdsAddresses(p *corev1.Pod) bool {
	if p.Spec.HostNetwork {
		return false
	}
	switch p.Status.Phase {
	case corev1.PodSucceeded, corev1.PodFailed:
		return false
	}
	return true
}

func newPodIndex(kube kubernetes.Interface, systemNS string) *podIndex {
	return &podIndex{kube: kube, systemNS: systemNS, pods: map[string]podEntry{}, addresses: map[string][]string{}, namespaces: map[string]serviceCaller{}}
}

// Lookup maps a pod address to its project, or to the platform's server.
func (x *podIndex) Lookup(ip string) (serviceCaller, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	e, ok := x.pods[ip]
	if !ok {
		return serviceCaller{}, false
	}
	if e.server {
		return serviceCaller{Platform: true}, true
	}
	c, ok := x.namespaces[e.namespace]
	return c, ok
}

// Run keeps the index current until ctx ends; it returns once the first
// listing is in, so a lookup made after it knows every pod of the moment.
func (x *podIndex) Run(ctx context.Context) {
	pods := informers.NewSharedInformerFactory(x.kube, 0)
	podInformer := pods.Core().V1().Pods().Informer()
	// Only the address and the namespace are kept of each pod.
	_ = podInformer.SetTransform(func(obj any) (any, error) {
		p, ok := obj.(*corev1.Pod)
		if !ok {
			return obj, nil
		}
		slim := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: p.Name, Namespace: p.Namespace, Labels: map[string]string{"app.kubernetes.io/name": p.Labels["app.kubernetes.io/name"]}}}
		slim.Spec.HostNetwork = p.Spec.HostNetwork
		slim.Status.Phase = p.Status.Phase
		slim.Status.PodIPs = p.Status.PodIPs
		if len(slim.Status.PodIPs) == 0 && p.Status.PodIP != "" {
			slim.Status.PodIPs = []corev1.PodIP{{IP: p.Status.PodIP}}
		}
		return slim, nil
	})
	_, _ = podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { x.putPod(obj) },
		UpdateFunc: func(_, obj any) { x.putPod(obj) },
		DeleteFunc: func(obj any) { x.dropPod(obj) },
	})
	namespaces := informers.NewSharedInformerFactoryWithOptions(x.kube, 0, informers.WithTweakListOptions(func(o *metav1.ListOptions) {
		o.LabelSelector = shpyrdv1.LabelProject
	}))
	nsInformer := namespaces.Core().V1().Namespaces().Informer()
	_, _ = nsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { x.putNamespace(obj) },
		UpdateFunc: func(_, obj any) { x.putNamespace(obj) },
		DeleteFunc: func(obj any) { x.dropNamespace(obj) },
	})
	pods.Start(ctx.Done())
	namespaces.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), podInformer.HasSynced, nsInformer.HasSynced)
}

func (x *podIndex) putPod(obj any) {
	p, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}
	key := p.Namespace + "/" + p.Name
	entry := podEntry{key: key, namespace: p.Namespace, server: p.Namespace == x.systemNS && p.Labels["app.kubernetes.io/name"] == "shpyrd-server"}
	x.mu.Lock()
	defer x.mu.Unlock()
	x.forget(key)
	if !holdsAddresses(p) {
		return
	}
	var ips []string
	for _, ip := range p.Status.PodIPs {
		if ip.IP == "" {
			continue
		}
		x.pods[ip.IP] = entry
		ips = append(ips, ip.IP)
	}
	if len(ips) == 0 {
		delete(x.addresses, key)
		return
	}
	x.addresses[key] = ips
}

func (x *podIndex) dropPod(obj any) {
	if d, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = d.Obj
	}
	p, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	x.forget(p.Namespace + "/" + p.Name)
}

// forget drops the addresses the pod key held, leaving any of them another
// pod has taken since. Callers hold x.mu.
func (x *podIndex) forget(key string) {
	for _, ip := range x.addresses[key] {
		if x.pods[ip].key == key {
			delete(x.pods, ip)
		}
	}
	delete(x.addresses, key)
}

// putNamespace reads a project namespace's identity off its labels: the
// project's slug, and its workspace's, the implicit one when unlabelled.
func (x *podIndex) putNamespace(obj any) {
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return
	}
	project := ns.Labels[shpyrdv1.LabelProject]
	if project == "" {
		return
	}
	ws := ns.Labels[shpyrdv1.LabelWorkspace]
	if ws == "" {
		ws = store.DefaultWorkspace
	}
	x.mu.Lock()
	x.namespaces[ns.Name] = serviceCaller{Workspace: ws, Project: project}
	x.mu.Unlock()
}

func (x *podIndex) dropNamespace(obj any) {
	if d, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = d.Obj
	}
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return
	}
	x.mu.Lock()
	delete(x.namespaces, ns.Name)
	x.mu.Unlock()
}
