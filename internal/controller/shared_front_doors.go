package controller

import (
	"context"
	"fmt"

	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// Shared front doors (RFC-0033 names, amended 2026-10-04). In the shared
// layout every workspace answers at <label>.<workspaces domain> and every
// app at <label>-<app>.<apps domain>, so two front doors and two wildcard
// certificates, made once, serve them all: nothing is issued, published or
// routed per workspace, and a new workspace answers the moment it exists.
//
//   - workspaces-front-door: <workspaces domain> and *.<workspaces domain>
//     to the server (dashboards, sign-in, the API), with its upload paths;
//   - apps-front-door: <apps domain> and *.<apps domain> to the server, for
//     the hosts no app's Ingress claims: an app that moved redirects from
//     there, an unknown name says so. Each app's own Ingress names its host
//     and wins over the wildcard.
//
// Their wildcard rules are what ExternalDNS publishes for the two zones.

const (
	workspacesFrontDoorName = "workspaces-front-door"
	appsFrontDoorName       = "apps-front-door"
	// WorkspacesWildcardSecretName holds <workspaces domain> and
	// *.<workspaces domain>.
	WorkspacesWildcardSecretName = "workspaces-wildcard-tls"
	// sharedFrontDoorLabel marks the shared doors' objects, which the
	// per-workspace clean-up never touches.
	sharedFrontDoorLabel = "shpyrd.io/shared-front-door"
)

// ensureSharedFrontDoors keeps the two shared doors while the layout is on,
// and removes them when it is off.
func (r *WorkspaceReconciler) ensureSharedFrontDoors(ctx context.Context) error {
	l := r.Config.Layout
	if !l.Shared() {
		for _, name := range []string{workspacesFrontDoorName, sourcesFrontDoorName(workspacesFrontDoorName), archivesFrontDoorName(workspacesFrontDoorName), appsFrontDoorName} {
			if err := r.deleteSharedIngress(ctx, name); err != nil {
				return err
			}
		}
		return nil
	}
	issuer := r.Config.WorkspaceCertIssuer
	if issuer == "" {
		issuer = r.Config.ClusterIssuer
	}
	labels := map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", sharedFrontDoorLabel: "true"}
	ws := []string{l.WorkspacesDomain, "*." + l.WorkspacesDomain}
	if err := r.ensureSharedCertificate(ctx, WorkspacesWildcardSecretName, ws, issuer, labels); err != nil {
		return err
	}
	tls := networkingv1.IngressTLS{Hosts: ws, SecretName: WorkspacesWildcardSecretName}
	if err := r.ensureSharedIngress(ctx, workspacesFrontDoorName, ws, tls, labels); err != nil {
		return err
	}
	// Deploys and archives are sent to a workspace's own host.
	class := r.ingressClass()
	if err := r.ensureSourcesFrontDoor(ctx, workspacesFrontDoorName, labels, "*."+l.WorkspacesDomain, tls, class); err != nil {
		return err
	}
	apps := []string{l.AppsDomain, "*." + l.AppsDomain}
	if err := r.ensureSharedCertificate(ctx, AppsWildcardSecretName, apps, issuer, labels); err != nil {
		return err
	}
	return r.ensureSharedIngress(ctx, appsFrontDoorName, apps, networkingv1.IngressTLS{Hosts: apps, SecretName: AppsWildcardSecretName}, labels)
}

func (r *WorkspaceReconciler) ingressClass() string {
	if r.Config.IngressClassExternal != "" {
		return r.Config.IngressClassExternal
	}
	return "nginx"
}

// ensureSharedCertificate keeps a wildcard Certificate whose name is its
// Secret's. Wildcards need the DNS-01 issuer.
func (r *WorkspaceReconciler) ensureSharedCertificate(ctx context.Context, name string, hosts []string, issuer string, labels map[string]string) error {
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(CertificateGVK)
	cert.SetName(name)
	cert.SetNamespace(r.Config.SystemNamespace)
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cert, func() error {
		cert.SetLabels(mergeMaps(cert.GetLabels(), labels))
		_ = unstructured.SetNestedField(cert.Object, name, "spec", "secretName")
		_ = unstructured.SetNestedStringSlice(cert.Object, hosts, "spec", "dnsNames")
		_ = unstructured.SetNestedMap(cert.Object, map[string]interface{}{"kind": "ClusterIssuer", "name": issuer}, "spec", "issuerRef")
		return nil
	})
	if err != nil && !meta.IsNoMatchError(err) {
		return fmt.Errorf("certificate %s: %w", name, err)
	}
	return nil
}

// ensureSharedIngress keeps a front door sending every host to the server.
func (r *WorkspaceReconciler) ensureSharedIngress(ctx context.Context, name string, hosts []string, tls networkingv1.IngressTLS, labels map[string]string) error {
	class := r.ingressClass()
	pathType := networkingv1.PathTypePrefix
	backend := networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "shpyrd-server", Port: networkingv1.ServiceBackendPort{Name: "http"}}}
	ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: r.Config.SystemNamespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ing, func() error {
		ing.Labels = mergeMaps(ing.Labels, labels)
		ing.Annotations = mergeMaps(ing.Annotations, frontDoorAnnotations())
		ing.Spec.IngressClassName = &class
		ing.Spec.TLS = []networkingv1.IngressTLS{tls}
		ing.Spec.Rules = nil
		for _, h := range hosts {
			ing.Spec.Rules = append(ing.Spec.Rules, networkingv1.IngressRule{
				Host:             h,
				IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{Path: "/", PathType: &pathType, Backend: backend}}}},
			})
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("front door %s: %w", name, err)
	}
	return nil
}

// deleteSharedIngress removes a shared door (its Certificate stays: it
// costs nothing and is ready if the layout comes back).
func (r *WorkspaceReconciler) deleteSharedIngress(ctx context.Context, name string) error {
	ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: r.Config.SystemNamespace}}
	if err := r.Delete(ctx, ing); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
