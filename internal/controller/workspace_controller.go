package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
)

// WorkspaceReconciler gives every explicit workspace (RFC-0033 phase 6) a
// front door: an Ingress at its address pointing at the platform server,
// with a certificate, so its dashboard, sign-in and edge answer there. The
// implicit workspace has the platform's own Ingress; on the open-source
// platform this reconciler therefore does nothing. Workspaces come from
// the control-plane store, not from Kubernetes objects, so the pass runs
// on Notify and on a timer.
type WorkspaceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Store  store.Store
	Config Config

	events chan event.GenericEvent
}

const workspaceSync = 2 * time.Minute

var workspacesRequest = reconcile.Request{NamespacedName: client.ObjectKey{Name: "workspaces"}}

func (r *WorkspaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.events = make(chan event.GenericEvent, 1)
	return ctrl.NewControllerManagedBy(mgr).
		Named("workspaces").
		WatchesRawSource(source.Channel(r.events, handler.EnqueueRequestsFromMapFunc(func(context.Context, client.Object) []reconcile.Request {
			return []reconcile.Request{workspacesRequest}
		}))).
		Complete(r)
}

// Notify asks for a pass now (after a workspace was created or changed).
func (r *WorkspaceReconciler) Notify() {
	if r.events == nil {
		return
	}
	select {
	case r.events <- event.GenericEvent{Object: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "workspaces"}}}:
	default:
	}
}

// Start kicks the first pass once the manager runs (the store has no
// watch to trigger it).
func (r *WorkspaceReconciler) Start(ctx context.Context) error {
	r.Notify()
	<-ctx.Done()
	return nil
}

func (r *WorkspaceReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	all, err := r.Store.ListWorkspaces(ctx)
	if err != nil {
		return ctrl.Result{RequeueAfter: workspaceSync}, err
	}
	wanted := map[string]bool{} // front door Ingress names to keep
	for i := range all {
		ws := &all[i]
		if ws.Implicit() || ws.Address == "" {
			continue
		}
		wanted[workspaceFrontDoorName(ws.Slug)] = true
		if err := r.ensureFrontDoor(ctx, ws); err != nil {
			return ctrl.Result{}, err
		}
		// The workspace's other names (RFC-0033 names): a verified custom
		// domain gets a front door with its own certificate (HTTP-01: the
		// company's DNS points here); a previous address keeps answering
		// with redirects until it expires.
		hosts, err := r.Store.ListWorkspaceHosts(ctx, ws.Slug)
		if err != nil {
			return ctrl.Result{RequeueAfter: workspaceSync}, err
		}
		for j := range hosts {
			h := &hosts[j]
			if !tenancy.HostServes(h) {
				continue
			}
			wanted[workspaceHostFrontDoorName(ws.Slug, h.Host)] = true
			if err := r.ensureHostFrontDoor(ctx, ws, h); err != nil {
				return ctrl.Result{}, err
			}
		}
	}
	// Front doors of workspaces and hosts that are gone.
	var list networkingv1.IngressList
	if err := r.List(ctx, &list, client.InNamespace(r.Config.SystemNamespace), client.MatchingLabels{shpyrdv1.LabelManagedBy: "shpyrd", "shpyrd.io/workspace-front-door": "true"}); err != nil {
		return ctrl.Result{}, err
	}
	for i := range list.Items {
		ing := &list.Items[i]
		if !wanted[ing.Name] {
			if err := r.Delete(ctx, ing); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			cert := &unstructured.Unstructured{}
			cert.SetGroupVersionKind(CertificateGVK)
			cert.SetName(ing.Name + "-tls")
			cert.SetNamespace(ing.Namespace)
			if err := r.Delete(ctx, cert); err != nil && !apierrors.IsNotFound(err) && !meta.IsNoMatchError(err) {
				return ctrl.Result{}, err
			}
			logger.Info("workspace front door removed", "ingress", ing.Name, "workspace", ing.Labels[shpyrdv1.LabelWorkspace])
		}
	}
	return ctrl.Result{RequeueAfter: workspaceSync}, nil
}

// workspaceHostFrontDoorName names the Ingress and Certificate of one of a
// workspace's other hosts.
func workspaceHostFrontDoorName(slug, host string) string {
	sum := sha256.Sum256([]byte(host))
	return workspaceFrontDoorName(slug) + "-h-" + hex.EncodeToString(sum[:])[:8]
}

// ensureHostFrontDoor keeps the Ingress and certificate of a custom domain
// (the host alone: its apps have Ingresses of their own with certificates
// per host) or of a moved address (the host and one label under it, so
// app links redirect too; the wildcard needs the DNS-01 issuer, which the
// platform holds for its own domain).
func (r *WorkspaceReconciler) ensureHostFrontDoor(ctx context.Context, ws *store.Workspace, h *store.WorkspaceHost) error {
	name := workspaceHostFrontDoorName(ws.Slug, h.Host)
	labels := map[string]string{
		shpyrdv1.LabelManagedBy:          "shpyrd",
		shpyrdv1.LabelWorkspace:          ws.Slug,
		"shpyrd.io/workspace-front-door": "true",
		"shpyrd.io/host-kind":            h.Kind,
	}
	hosts := []string{h.Host}
	issuer := r.Config.ClusterIssuer
	if h.Kind == store.HostMoved {
		hosts = append(hosts, "*."+h.Host)
		if r.Config.WorkspaceCertIssuer != "" {
			issuer = r.Config.WorkspaceCertIssuer
		}
	}
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(CertificateGVK)
	cert.SetName(name + "-tls")
	cert.SetNamespace(r.Config.SystemNamespace)
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, cert, func() error {
		cert.SetLabels(mergeMaps(cert.GetLabels(), labels))
		_ = unstructured.SetNestedField(cert.Object, name+"-tls", "spec", "secretName")
		_ = unstructured.SetNestedStringSlice(cert.Object, hosts, "spec", "dnsNames")
		_ = unstructured.SetNestedMap(cert.Object, map[string]interface{}{"kind": "ClusterIssuer", "name": issuer}, "spec", "issuerRef")
		return nil
	}); err != nil {
		return fmt.Errorf("certificate for %s: %w", h.Host, err)
	}
	class := r.Config.IngressClassExternal
	if class == "" {
		class = "nginx"
	}
	pathType := networkingv1.PathTypePrefix
	backend := networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "shpyrd-server", Port: networkingv1.ServiceBackendPort{Name: "http"}}}
	ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: r.Config.SystemNamespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ing, func() error {
		ing.Labels = mergeMaps(ing.Labels, labels)
		ing.Annotations = mergeMaps(ing.Annotations, map[string]string{
			"nginx.ingress.kubernetes.io/ssl-redirect":       "true",
			"nginx.ingress.kubernetes.io/proxy-read-timeout": "3600",
			"nginx.ingress.kubernetes.io/proxy-send-timeout": "3600",
		})
		ing.Spec.IngressClassName = &class
		ing.Spec.TLS = []networkingv1.IngressTLS{{Hosts: hosts, SecretName: name + "-tls"}}
		ing.Spec.Rules = nil
		for _, host := range hosts {
			ing.Spec.Rules = append(ing.Spec.Rules, networkingv1.IngressRule{
				Host:             host,
				IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{Path: "/", PathType: &pathType, Backend: backend}}}},
			})
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("front door for %s: %w", h.Host, err)
	}
	return nil
}

// workspaceFrontDoorName names the Ingress and Certificate of a workspace.
func workspaceFrontDoorName(slug string) string { return "workspace-" + slug }

// ensureFrontDoor keeps the Ingress (and certificate) of one workspace.
// Tenants reach their dashboard from the internet, so the external front
// door serves it whatever the platform's own exposure.
func (r *WorkspaceReconciler) ensureFrontDoor(ctx context.Context, ws *store.Workspace) error {
	name := workspaceFrontDoorName(ws.Slug)
	labels := map[string]string{
		shpyrdv1.LabelManagedBy:          "shpyrd",
		shpyrdv1.LabelWorkspace:          ws.Slug,
		"shpyrd.io/workspace-front-door": "true",
	}
	wildcard := r.Config.WildcardTLS && r.Config.underClusterDomain(ws.Address)
	// The Ingress serves both the workspace host and app subdomains.
	tls := networkingv1.IngressTLS{Hosts: []string{ws.Address, "*." + ws.Address}}
	if !wildcard {
		tls.SecretName = name + "-tls"
		cert := &unstructured.Unstructured{}
		cert.SetGroupVersionKind(CertificateGVK)
		cert.SetName(name + "-tls")
		cert.SetNamespace(r.Config.SystemNamespace)
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, cert, func() error {
			cert.SetLabels(mergeMaps(cert.GetLabels(), labels))
			_ = unstructured.SetNestedField(cert.Object, name+"-tls", "spec", "secretName")
			// The cert covers both the workspace dashboard (demo.shpyrd.app)
			// and its apps one label under it (*.demo.shpyrd.app), so nginx
			// can serve TLS for any app host with this one certificate.
			_ = unstructured.SetNestedStringSlice(cert.Object, []string{ws.Address, "*." + ws.Address}, "spec", "dnsNames")
			issuer := r.Config.WorkspaceCertIssuer
			if issuer == "" {
				issuer = r.Config.ClusterIssuer
			}
			_ = unstructured.SetNestedMap(cert.Object, map[string]interface{}{"kind": "ClusterIssuer", "name": issuer}, "spec", "issuerRef")
			return nil
		}); err != nil {
			return fmt.Errorf("certificate for workspace %s: %w", ws.Slug, err)
		}
	}
	class := r.Config.IngressClassExternal
	if class == "" {
		class = "nginx"
	}
	pathType := networkingv1.PathTypePrefix
	ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: r.Config.SystemNamespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ing, func() error {
		ing.Labels = mergeMaps(ing.Labels, labels)
		ing.Annotations = mergeMaps(ing.Annotations, map[string]string{
			"nginx.ingress.kubernetes.io/ssl-redirect": "true",
			// Streams (build logs, log follow, the terminal) may stay
			// silent longer than nginx's 60s default.
			"nginx.ingress.kubernetes.io/proxy-read-timeout": "3600",
			"nginx.ingress.kubernetes.io/proxy-send-timeout": "3600",
		})
		ing.Spec.IngressClassName = &class
		ing.Spec.TLS = []networkingv1.IngressTLS{tls}
		ing.Spec.Rules = []networkingv1.IngressRule{{
			Host: ws.Address,
			IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{
				Path: "/", PathType: &pathType,
				Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "shpyrd-server", Port: networkingv1.ServiceBackendPort{Name: "http"}}},
			}}}},
		},
			{
				// Wildcard rule: app hosts one label under the workspace address.
				// nginx matches the most specific rule first; unknown hosts hit
				// the global default backend (shpyrd-server → "no app here").
				Host: "*." + ws.Address,
				IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{
					Path: "/", PathType: &pathType,
					Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "shpyrd-server", Port: networkingv1.ServiceBackendPort{Name: "http"}}},
				}}}},
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("front door for workspace %s: %w", ws.Slug, err)
	}
	return nil
}
