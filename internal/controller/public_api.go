package controller

import (
	"context"
	"fmt"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// A dedicated listener, rather than an ingress path filter, is the boundary.
// Disabling the feature removes its public ingress, including on upgrades.
func (r *WorkspaceReconciler) ensurePublicAPI(ctx context.Context) error {
	const name = "shpyrd-public-api"
	if !r.Config.PublicAPIEnabled {
		return r.deleteSharedIngress(ctx, name)
	}
	host := r.Config.PublicAPIHost
	if host == "" || len(validation.IsDNS1123Subdomain(host)) != 0 {
		return fmt.Errorf("invalid public API hostname %q", host)
	}
	labels := map[string]string{sharedFrontDoorLabel: "true"}
	if err := r.ensureSharedCertificate(ctx, name+"-tls", []string{host}, r.Config.PublicAPIIssuer, labels); err != nil {
		return err
	}
	ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: r.Config.SystemNamespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ing, func() error {
		class := r.ingressClass()
		prefix := networkingv1.PathTypePrefix
		ing.Labels = mergeMaps(ing.Labels, labels)
		ing.Annotations = mergeMaps(ing.Annotations, map[string]string{"nginx.ingress.kubernetes.io/ssl-redirect": "true"})
		ing.Spec = networkingv1.IngressSpec{
			IngressClassName: &class,
			TLS:              []networkingv1.IngressTLS{{Hosts: []string{host}, SecretName: name + "-tls"}},
			Rules:            []networkingv1.IngressRule{{Host: host, IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{Path: "/", PathType: &prefix, Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: name, Port: networkingv1.ServiceBackendPort{Name: "http"}}}}}}}}},
		}
		return nil
	})
	return err
}
