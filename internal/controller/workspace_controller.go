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
	// LookupLB finds the public front door's address when Config did not
	// know it at start (a first install), for the readiness checks.
	LookupLB func(ctx context.Context) string
	// Forget drops the App controller's memory of workspaces and plans on
	// Notify, so a change of ceilings is seen by the next reconcile.
	Forget func()

	events  chan event.GenericEvent
	checker readinessChecker
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
	if r.Forget != nil {
		r.Forget()
	}
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
	pending := false            // a workspace whose door does not answer yet
	for i := range all {
		ws := &all[i]
		if ws.Address == "" {
			continue // no address yet: nothing to publish (RFC-0080: every workspace gets one)
		}
		wanted[workspaceFrontDoorName(ws.Slug)] = true
		wanted[sourcesFrontDoorName(workspaceFrontDoorName(ws.Slug))] = true
		wanted[archivesFrontDoorName(workspaceFrontDoorName(ws.Slug))] = true
		if err := r.ensureFrontDoor(ctx, ws); err != nil {
			return ctrl.Result{}, err
		}
		// What the door looks like from outside, recorded on the
		// workspace; a door not yet answering is looked at again soon.
		if ws.Status != store.WorkspaceSuspended && !r.checkReadiness(ctx, ws) {
			pending = true
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
			if h.Kind == store.HostCustom {
				wanted[sourcesFrontDoorName(workspaceHostFrontDoorName(ws.Slug, h.Host))] = true
				wanted[archivesFrontDoorName(workspaceHostFrontDoorName(ws.Slug, h.Host))] = true
			}
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
	// The projects' quotas follow their workspace's ceilings.
	if err := r.syncQuotas(ctx, all); err != nil {
		logger.Error(err, "workspace quotas")
	}
	if pending {
		return ctrl.Result{RequeueAfter: readinessRetry}, nil
	}
	return ctrl.Result{RequeueAfter: workspaceSync}, nil
}

// workspaceHostFrontDoorName names the Ingress and Certificate of one of a
// workspace's other hosts.
func workspaceHostFrontDoorName(slug, host string) string {
	sum := sha256.Sum256([]byte(host))
	return workspaceFrontDoorName(slug) + "-h-" + hex.EncodeToString(sum[:])[:8]
}

// frontDoorAnnotations are the ingress-nginx settings of every Ingress
// that fronts the platform's server (the console's Ingress, rendered from
// deploy/components/shpyrd/base/ingress.yaml, carries the same). Request
// bodies keep nginx's 1 MiB default: the API is JSON, and nginx buffers a
// body before the server sees it. The one upload has its own Ingress.
func frontDoorAnnotations() map[string]string {
	return map[string]string{
		"nginx.ingress.kubernetes.io/ssl-redirect": "true",
		// Streams (build logs, log follow, the terminal) may stay
		// silent longer than nginx's 60s default.
		"nginx.ingress.kubernetes.io/proxy-read-timeout": "3600",
		"nginx.ingress.kubernetes.io/proxy-send-timeout": "3600",
	}
}

// SourcesPath is where `shpyrd deploy` uploads the source archive (and
// `shpyrd cluster restore` its archives): the one request body of any size
// the platform's server takes, up to SourcesBodySize (pkg/api/sources.go
// maxSourceSize). It gets an Ingress of its own per front door so the
// larger body applies to that path alone; nginx folds it into the same
// server as a second location. Streaming (no request buffering) lets the
// server refuse an unauthenticated upload before the body is read.
const (
	SourcesPath                = "/api/sources"
	SourcesBodySize            = "512m"
	ProjectArchivesPath        = "/api/project-archives"
	ClusterProjectArchivesPath = "/api/cluster/project-archives"
	ProjectArchivesBodySize    = "65g"
)

func sourcesFrontDoorAnnotations() map[string]string {
	return map[string]string{
		"nginx.ingress.kubernetes.io/ssl-redirect":            "true",
		"nginx.ingress.kubernetes.io/proxy-read-timeout":      "3600",
		"nginx.ingress.kubernetes.io/proxy-send-timeout":      "3600",
		"nginx.ingress.kubernetes.io/proxy-body-size":         SourcesBodySize,
		"nginx.ingress.kubernetes.io/proxy-request-buffering": "off",
	}
}

// sourcesFrontDoorName names the companion Ingress of a front door.
func sourcesFrontDoorName(frontDoor string) string { return frontDoor + "-sources" }

func archivesFrontDoorName(frontDoor string) string { return frontDoor + "-archives" }

// ensureSourcesFrontDoor keeps the companion Ingress of the front door
// `name`: the same host and TLS, one exact path (SourcesPath), the upload
// settings. Labelled as a front door so the reconcile loop removes it with
// its workspace or host.
func (r *WorkspaceReconciler) ensureSourcesFrontDoor(ctx context.Context, name string, labels map[string]string, host string, tls networkingv1.IngressTLS, class string) error {
	pathType := networkingv1.PathTypeExact
	backend := networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "shpyrd-server", Port: networkingv1.ServiceBackendPort{Name: "http"}}}
	ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: sourcesFrontDoorName(name), Namespace: r.Config.SystemNamespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ing, func() error {
		ing.Labels = mergeMaps(ing.Labels, labels)
		ing.Annotations = mergeMaps(ing.Annotations, sourcesFrontDoorAnnotations())
		ing.Spec.IngressClassName = &class
		ing.Spec.TLS = []networkingv1.IngressTLS{tls}
		ing.Spec.Rules = []networkingv1.IngressRule{{
			Host:             host,
			IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{Path: SourcesPath, PathType: &pathType, Backend: backend}}}},
		}}
		return nil
	})
	if err != nil {
		return fmt.Errorf("sources front door for %s: %w", host, err)
	}
	return r.ensureArchivesFrontDoor(ctx, name, labels, host, tls, class)
}

func (r *WorkspaceReconciler) ensureArchivesFrontDoor(ctx context.Context, name string, labels map[string]string, host string, tls networkingv1.IngressTLS, class string) error {
	ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: archivesFrontDoorName(name), Namespace: r.Config.SystemNamespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ing, func() error {
		ing.Labels = mergeMaps(ing.Labels, labels)
		ing.Annotations = mergeMaps(ing.Annotations, sourcesFrontDoorAnnotations())
		ing.Annotations["nginx.ingress.kubernetes.io/proxy-body-size"] = ProjectArchivesBodySize
		ing.Annotations["nginx.ingress.kubernetes.io/proxy-buffering"] = "off"
		ing.Spec.IngressClassName = &class
		ing.Spec.TLS = []networkingv1.IngressTLS{tls}
		prefix := networkingv1.PathTypePrefix
		backend := networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "shpyrd-server", Port: networkingv1.ServiceBackendPort{Name: "http"}}}
		ing.Spec.Rules = []networkingv1.IngressRule{{Host: host, IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{Path: ProjectArchivesPath, PathType: &prefix, Backend: backend}, {Path: ClusterProjectArchivesPath, PathType: &prefix, Backend: backend}}}}}}
		return nil
	})
	return err
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
		ing.Annotations = mergeMaps(ing.Annotations, frontDoorAnnotations())
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
	// Deploys can target a custom domain (`shpyrd login --url`); a moved
	// address only redirects, so it needs no upload path.
	if h.Kind == store.HostCustom {
		return r.ensureSourcesFrontDoor(ctx, name, labels, h.Host, networkingv1.IngressTLS{Hosts: hosts, SecretName: name + "-tls"}, class)
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
		ing.Annotations = mergeMaps(ing.Annotations, frontDoorAnnotations())
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
	return r.ensureSourcesFrontDoor(ctx, name, labels, ws.Address, tls, class)
}
