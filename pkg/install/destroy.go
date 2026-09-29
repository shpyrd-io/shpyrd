package install

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// Destroy removes the platform from the cluster and leaves the cluster:
// `shpyrd cluster destroy --keep-cluster`. The inverse of Apply, in the
// order that lets operators finish their work before they go: projects
// first (their databases, caches and volumes with them), the system
// namespace, then the aggregated APIs and webhooks that would otherwise
// deadlock namespace deletion, the Helm releases, every rendered
// cluster-scoped object, the components' namespaces, the custom resource
// definitions, and what the hooks left in kube-system. A namespace that
// stalls has the finalizers of its remaining objects cleared, and each one
// is reported: a finalizer nobody removed is a bug in the platform, not a
// step of the runbook.
//
// Cluster infrastructure the platform installs on top of but does not own
// (Calico, through the network-policy component) is kept.

// DestroyOptions tunes Destroy.
type DestroyOptions struct {
	// Keep names components left in place: cluster infrastructure.
	Keep []string
	// Wait bounds how long a namespace may take to terminate before the
	// finalizers of its remaining objects are cleared. Zero: three minutes.
	Wait time.Duration
}

// ClearedFinalizer is a finalizer Destroy had to remove by hand.
type ClearedFinalizer struct {
	Namespace, Kind, Name, Finalizer string
}

func (f ClearedFinalizer) String() string {
	return fmt.Sprintf("%s/%s %s (%s)", f.Namespace, f.Name, f.Kind, f.Finalizer)
}

// crdGroups are the API groups of custom resource definitions the
// platform's components install, Helm's included (Helm does not remove
// CRDs on uninstall). Rendered kustomize components add theirs at
// runtime; this list is the floor.
var crdGroups = []string{
	"shpyrd.io",
	"kpack.io",
	"dex.coreos.com",
	"cert-manager.io", "acme.cert-manager.io", "trust.cert-manager.io",
	"postgresql.cnpg.io", "barmancloud.cnpg.io",
	"keda.sh", "http.keda.sh",
	"monitoring.coreos.com",
	"snapshot.storage.k8s.io",
}

// kubeSystemLeftovers are objects hooks create in kube-system, outside any
// component's manifests.
var kubeSystemLeftovers = []struct{ kind, name string }{
	{"Secret", "cluster-autoscaler-oci"},
	{"ConfigMap", "cluster-autoscaler-status"},
	{"ConfigMap", "shpyrd-ca-bundle"},
}

// Destroy implements the wipe. It returns the finalizers it had to clear.
func (e *Engine) Destroy(ctx context.Context, opts DestroyOptions) ([]ClearedFinalizer, error) {
	if e.kube == nil {
		return nil, errors.New("destroy needs a cluster")
	}
	wait := opts.Wait
	if wait <= 0 {
		wait = 3 * time.Minute
	}
	keep := map[string]bool{}
	for _, k := range opts.Keep {
		keep[k] = true
	}
	var cleared []ClearedFinalizer
	systemNS := e.vars[VarSystemNS]
	if systemNS == "" {
		systemNS = DefaultSystemNamespace
	}

	// The components, in runlevel order, minus the kept ones.
	var ordered []*Component
	for _, rl := range e.profile.Runlevels {
		for _, name := range rl.Components {
			if c := e.components[name]; c != nil && !keep[name] {
				ordered = append(ordered, c)
			}
		}
	}
	ours := map[string]bool{systemNS: true}
	for _, c := range ordered {
		if c.Namespace != "" && c.Namespace != "kube-system" && c.Namespace != "default" {
			ours[c.Namespace] = true
		}
	}

	// 1. Projects, while the operators that finalize them still run.
	projects, err := e.kube.Kube.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: "shpyrd.io/project"})
	if err != nil {
		return nil, err
	}
	var projectNS []string
	for _, ns := range projects.Items {
		projectNS = append(projectNS, ns.Name)
		ours[ns.Name] = true
	}
	if len(projectNS) > 0 {
		e.rep.Step("projects", fmt.Sprintf("deleting %d project namespace(s)", len(projectNS)))
		c, err := e.deleteNamespaces(ctx, projectNS, wait)
		cleared = append(cleared, c...)
		if err != nil {
			return cleared, err
		}
	}

	// 2. The system namespace: the server, the controller, the databases.
	e.rep.Step(systemNS, "deleting the system namespace")
	c, err := e.deleteNamespaces(ctx, []string{systemNS}, wait)
	cleared = append(cleared, c...)
	if err != nil {
		return cleared, err
	}

	// 3. Aggregated APIs and webhooks pointing into our namespaces: with
	// their services gone they would stall every namespace deletion.
	if err := e.deleteAggregatedAndWebhooks(ctx, ours); err != nil {
		return cleared, err
	}

	// 4. Helm releases, last runlevel first.
	for i := len(ordered) - 1; i >= 0; i-- {
		c := ordered[i]
		if c.Helm == nil {
			continue
		}
		e.rep.Step(c.Name, "helm uninstall "+c.Helm.Release)
		if err := e.helm.uninstall(c.Helm, c.Namespace, 2*time.Minute); err != nil {
			e.rep.Step(c.Name, "helm uninstall: "+err.Error()+" (continuing)")
		}
	}

	// 5. Everything the kustomize components rendered outside a namespace
	// we are about to delete: CRDs, cluster roles and bindings, webhooks,
	// cluster issuers, stacks, builders, and objects in kube-system.
	var clusterScoped []*unstructured.Unstructured
	for i := len(ordered) - 1; i >= 0; i-- {
		c := ordered[i]
		if c.Kustomize == nil {
			continue
		}
		objs, err := e.renderComponent(c)
		if err != nil {
			e.rep.Step(c.Name, "render for deletion: "+err.Error()+" (continuing)")
			continue
		}
		for _, o := range objs {
			switch {
			case o.GetKind() == "Namespace":
				continue // deleted below, with a wait
			case o.GetNamespace() == "" || o.GetNamespace() == "kube-system":
				clusterScoped = append(clusterScoped, o)
			case ours[o.GetNamespace()] || (o.GetNamespace() == "" && ours[c.Namespace]):
				continue // goes with its namespace
			}
		}
	}
	if len(clusterScoped) > 0 {
		e.rep.Step("cluster", fmt.Sprintf("deleting %d cluster-scoped object(s) the components rendered", len(clusterScoped)))
		if err := e.applier.deleteAll(ctx, clusterScoped, "kube-system"); err != nil {
			e.rep.Step("cluster", err.Error()+" (continuing)")
		}
	}

	// 6. The components' namespaces.
	var nss []string
	for ns := range ours {
		if ns != systemNS && !contains(projectNS, ns) {
			nss = append(nss, ns)
		}
	}
	sort.Strings(nss)
	if len(nss) > 0 {
		e.rep.Step("namespaces", "deleting "+strings.Join(nss, ", "))
		c, err := e.deleteNamespaces(ctx, nss, wait)
		cleared = append(cleared, c...)
		if err != nil {
			return cleared, err
		}
	}

	// 7. Custom resource definitions Helm left behind, and the ones the
	// platform's own components own.
	if err := e.deleteCRDs(ctx, ordered); err != nil {
		return cleared, err
	}

	// 8. What hooks left in kube-system.
	for _, l := range kubeSystemLeftovers {
		var err error
		switch l.kind {
		case "Secret":
			err = e.kube.Kube.CoreV1().Secrets("kube-system").Delete(ctx, l.name, metav1.DeleteOptions{})
		case "ConfigMap":
			err = e.kube.Kube.CoreV1().ConfigMaps("kube-system").Delete(ctx, l.name, metav1.DeleteOptions{})
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return cleared, fmt.Errorf("kube-system %s %s: %w", l.kind, l.name, err)
		}
	}

	// 9. Volumes: the namespaces took their claims; the CSI driver removes
	// the cloud disks. Report what stays.
	if pvs, err := e.kube.Kube.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{}); err == nil && len(pvs.Items) > 0 {
		e.rep.Step("volumes", fmt.Sprintf("%d persistent volume(s) still present (Retain policy or a slow driver); check the cloud console", len(pvs.Items)))
	}
	return cleared, nil
}

// deleteNamespaces deletes namespaces and waits for them to go; a
// namespace still Terminating after wait has the finalizers of its
// remaining objects cleared, each one reported.
func (e *Engine) deleteNamespaces(ctx context.Context, names []string, wait time.Duration) ([]ClearedFinalizer, error) {
	for _, ns := range names {
		if err := e.kube.Kube.CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("delete namespace %s: %w", ns, err)
		}
	}
	var cleared []ClearedFinalizer
	deadline := time.Now().Add(wait)
	stripped := map[string]bool{}
	for {
		left := 0
		for _, ns := range names {
			if _, err := e.kube.Kube.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{}); err == nil {
				left++
				if time.Now().After(deadline) && !stripped[ns] {
					stripped[ns] = true
					c, err := e.clearFinalizers(ctx, ns)
					if err != nil {
						e.rep.Step(ns, "clearing finalizers: "+err.Error())
					}
					for _, f := range c {
						e.rep.Step(ns, "cleared finalizer "+f.String()+" — a bug to report: nothing removed it")
					}
					cleared = append(cleared, c...)
				}
			}
		}
		if left == 0 {
			return cleared, nil
		}
		if time.Now().After(deadline.Add(wait)) {
			return cleared, fmt.Errorf("%d namespace(s) still terminating after %s", left, (2 * wait).String())
		}
		select {
		case <-ctx.Done():
			return cleared, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// clearFinalizers removes the finalizers of every object left in a
// namespace, whatever its kind: the operators that would have run them
// are gone.
func (e *Engine) clearFinalizers(ctx context.Context, ns string) ([]ClearedFinalizer, error) {
	if e.kube.Dynamic == nil {
		return nil, nil
	}
	lists, err := e.kube.Kube.Discovery().ServerPreferredNamespacedResources()
	if err != nil && len(lists) == 0 {
		return nil, err
	}
	var out []ClearedFinalizer
	for _, l := range lists {
		gv, err := schema.ParseGroupVersion(l.GroupVersion)
		if err != nil {
			continue
		}
		for _, r := range l.APIResources {
			if strings.Contains(r.Name, "/") || !hasVerbs(r.Verbs, "list", "patch") {
				continue
			}
			res := e.kube.Dynamic.Resource(gv.WithResource(r.Name)).Namespace(ns)
			items, err := res.List(ctx, metav1.ListOptions{})
			if err != nil {
				continue
			}
			for i := range items.Items {
				o := &items.Items[i]
				if len(o.GetFinalizers()) == 0 {
					continue
				}
				for _, f := range o.GetFinalizers() {
					out = append(out, ClearedFinalizer{Namespace: ns, Kind: o.GetKind(), Name: o.GetName(), Finalizer: f})
				}
				if _, err := res.Patch(ctx, o.GetName(), types.MergePatchType, []byte(`{"metadata":{"finalizers":null}}`), metav1.PatchOptions{}); err != nil && !apierrors.IsNotFound(err) {
					return out, fmt.Errorf("%s/%s %s: %w", ns, o.GetName(), o.GetKind(), err)
				}
			}
		}
	}
	return out, nil
}

// deleteAggregatedAndWebhooks removes APIServices and webhook
// configurations whose service lives in one of our namespaces.
func (e *Engine) deleteAggregatedAndWebhooks(ctx context.Context, ours map[string]bool) error {
	if e.kube.Dynamic == nil {
		return nil
	}
	apiServices := e.kube.Dynamic.Resource(schema.GroupVersionResource{Group: "apiregistration.k8s.io", Version: "v1", Resource: "apiservices"})
	if list, err := apiServices.List(ctx, metav1.ListOptions{}); err == nil {
		for i := range list.Items {
			ns, _, _ := unstructured.NestedString(list.Items[i].Object, "spec", "service", "namespace")
			if ns != "" && ours[ns] {
				e.rep.Step("apiservices", "deleting "+list.Items[i].GetName())
				if err := apiServices.Delete(ctx, list.Items[i].GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
					return err
				}
			}
		}
	}
	for _, kind := range []string{"validatingwebhookconfigurations", "mutatingwebhookconfigurations"} {
		res := e.kube.Dynamic.Resource(schema.GroupVersionResource{Group: "admissionregistration.k8s.io", Version: "v1", Resource: kind})
		list, err := res.List(ctx, metav1.ListOptions{})
		if err != nil {
			continue
		}
		for i := range list.Items {
			hooks, _, _ := unstructured.NestedSlice(list.Items[i].Object, "webhooks")
			mine := false
			for _, h := range hooks {
				hm, _ := h.(map[string]interface{})
				ns, _, _ := unstructured.NestedString(hm, "clientConfig", "service", "namespace")
				if ns != "" && ours[ns] {
					mine = true
				}
			}
			if mine {
				e.rep.Step("webhooks", "deleting "+list.Items[i].GetName())
				if err := res.Delete(ctx, list.Items[i].GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
					return err
				}
			}
		}
	}
	return nil
}

// deleteCRDs removes the custom resource definitions of the platform's
// components: those Helm annotated with one of our releases, and those in
// the groups the components own.
func (e *Engine) deleteCRDs(ctx context.Context, comps []*Component) error {
	if e.kube.Dynamic == nil {
		return nil
	}
	releases := map[string]bool{}
	for _, c := range comps {
		if c.Helm != nil {
			releases[c.Helm.Release] = true
		}
	}
	crds := e.kube.Dynamic.Resource(schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"})
	list, err := crds.List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	n := 0
	for i := range list.Items {
		crd := &list.Items[i]
		group, _, _ := unstructured.NestedString(crd.Object, "spec", "group")
		own := releases[crd.GetAnnotations()["meta.helm.sh/release-name"]]
		for _, g := range crdGroups {
			if group == g || strings.HasSuffix(group, "."+g) {
				own = true
			}
		}
		if !own {
			continue
		}
		if err := crds.Delete(ctx, crd.GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			if meta.IsNoMatchError(err) {
				continue
			}
			return fmt.Errorf("delete CRD %s: %w", crd.GetName(), err)
		}
		n++
	}
	if n > 0 {
		e.rep.Step("crds", fmt.Sprintf("deleted %d custom resource definition(s)", n))
	}
	return nil
}

func hasVerbs(verbs []string, want ...string) bool {
	have := map[string]bool{}
	for _, v := range verbs {
		have[v] = true
	}
	for _, w := range want {
		if !have[w] {
			return false
		}
	}
	return true
}
