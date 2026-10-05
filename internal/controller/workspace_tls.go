package controller

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// WorkspaceTLSSecretName is the copy, in a project namespace, of its
// workspace's wildcard certificate (RFC-0033 phase 6): the front door of
// an explicit workspace holds *.<address> in the system namespace, and an
// Ingress can only reference Secrets of its own namespace.
const WorkspaceTLSSecretName = "workspace-tls"

// workspaceTLSSource is the Secret the workspace reconciler has cert-manager
// fill for a workspace (workspace-<slug>-tls, in the system namespace).
func workspaceTLSSource(slug string) string { return workspaceFrontDoorName(slug) + "-tls" }

// AppsTLSSecretName is the copy, in a project namespace, of the shared apps
// domain's wildcard certificate (RFC-0033 names): every app's
// <workspace>-<app>.<apps domain> is covered by it.
const AppsTLSSecretName = "apps-tls"

// AppsWildcardSecretName is the shared apps domain's wildcard certificate,
// in the system namespace, issued for the apps' front door.
const AppsWildcardSecretName = "apps-wildcard-tls"

// underWorkspaceDomain says the host is one label under the app's explicit
// workspace's address: covered by the workspace wildcard. (A custom
// domain, primary or not, has certificates per host.)
func (c Config) underWorkspaceDomain(app *shpyrdv1.App, host string) bool {
	ws := workspaceOf(app)
	address := ""
	if c.WorkspaceAddress != nil {
		address = c.WorkspaceAddress(ws)
	} else {
		address = c.appsDomain(app)
	}
	if address == "" || address == c.Domain {
		return false
	}
	return oneLabelUnder(host, address)
}

func oneLabelUnder(host, domain string) bool {
	label, ok := strings.CutSuffix(host, "."+domain)
	return ok && label != "" && !strings.Contains(label, ".")
}

// reconcileWorkspaceTLS copies the workspace's wildcard certificate into the
// project namespace (apps of the implicit workspace use the platform
// wildcard through the front door's default certificate and need nothing).
// Returns whether the copy exists, so the Ingress can reference it.
func (r *AppReconciler) reconcileWorkspaceTLS(ctx context.Context, app *shpyrdv1.App) (bool, error) {
	ws := workspaceOf(app)
	copyRef := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: WorkspaceTLSSecretName, Namespace: app.Namespace}}
	// A workspace whose address is the platform domain (the open-source
	// default) is covered by the platform wildcard, and one of the shared
	// layout by the apps' wildcard (reconcileAppsTLS): nothing to copy, and
	// a copy left from before a move goes.
	address := r.Config.workspaceAddress(ws)
	if _, shared := r.Config.Layout.Label(address); shared || address == "" || address == r.Config.Domain {
		if err := r.deleteIfExists(ctx, copyRef); err != nil {
			return false, err
		}
		return false, nil
	}
	src := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: r.Config.SystemNamespace, Name: workspaceTLSSource(ws)}, src); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil // not issued yet: the front door serves its default meanwhile
		}
		return false, fmt.Errorf("workspace certificate: %w", err)
	}
	if len(src.Data["tls.crt"]) == 0 || len(src.Data["tls.key"]) == 0 {
		return false, nil
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, copyRef, func() error {
		copyRef.Type = corev1.SecretTypeTLS
		copyRef.Labels = mergeMaps(copyRef.Labels, map[string]string{
			shpyrdv1.LabelManagedBy: "shpyrd",
			shpyrdv1.LabelWorkspace: ws,
			shpyrdv1.LabelProject:   app.Name,
		})
		copyRef.Data = map[string][]byte{"tls.crt": src.Data["tls.crt"], "tls.key": src.Data["tls.key"]}
		if ca := src.Data["ca.crt"]; len(ca) > 0 {
			copyRef.Data["ca.crt"] = ca
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("copy workspace certificate: %w", err)
	}
	return true, nil
}

// reconcileAppsTLS copies the shared apps domain's wildcard certificate into
// the project namespace when one of the project's hosts is under that
// domain, and removes the copy when none is.
func (r *AppReconciler) reconcileAppsTLS(ctx context.Context, app *shpyrdv1.App) error {
	copyRef := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: AppsTLSSecretName, Namespace: app.Namespace}}
	needed := false
	for _, h := range r.Config.domains(app) {
		needed = needed || r.Config.sharedWildcard(h)
	}
	if !needed || !hasWeb(app) {
		return r.deleteIfExists(ctx, copyRef)
	}
	src := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: r.Config.SystemNamespace, Name: AppsWildcardSecretName}, src); err != nil {
		if apierrors.IsNotFound(err) {
			return nil // not issued yet: the front door serves its default meanwhile
		}
		return fmt.Errorf("apps certificate: %w", err)
	}
	if len(src.Data["tls.crt"]) == 0 || len(src.Data["tls.key"]) == 0 {
		return nil
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, copyRef, func() error {
		copyRef.Type = corev1.SecretTypeTLS
		copyRef.Labels = mergeMaps(copyRef.Labels, map[string]string{
			shpyrdv1.LabelManagedBy: "shpyrd",
			shpyrdv1.LabelProject:   app.Name,
		})
		copyRef.Data = map[string][]byte{"tls.crt": src.Data["tls.crt"], "tls.key": src.Data["tls.key"]}
		if ca := src.Data["ca.crt"]; len(ca) > 0 {
			copyRef.Data["ca.crt"] = ca
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("copy apps certificate: %w", err)
	}
	return nil
}

// workspaceTLSToApps maps a change of a workspace's wildcard certificate to
// every App of that workspace, and a change of the apps' wildcard to every
// App, so the copies follow renewals.
func (r *AppReconciler) workspaceTLSToApps(ctx context.Context, obj client.Object) []reconcile.Request {
	if obj.GetNamespace() != r.Config.SystemNamespace {
		return nil
	}
	name := obj.GetName()
	if name == AppsWildcardSecretName {
		var apps shpyrdv1.AppList
		if err := r.List(ctx, &apps); err != nil {
			return nil
		}
		out := make([]reconcile.Request, 0, len(apps.Items))
		for _, a := range apps.Items {
			out = append(out, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: a.Namespace, Name: a.Name}})
		}
		return out
	}
	slug, ok := strings.CutSuffix(strings.TrimPrefix(name, "workspace-"), "-tls")
	if !ok || !strings.HasPrefix(name, "workspace-") || slug == "" {
		return nil
	}
	var apps shpyrdv1.AppList
	if err := r.List(ctx, &apps, client.MatchingLabels{shpyrdv1.LabelWorkspace: slug}); err != nil {
		return nil
	}
	out := make([]reconcile.Request, 0, len(apps.Items))
	for _, a := range apps.Items {
		out = append(out, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: a.Namespace, Name: a.Name}})
	}
	return out
}

// workspaceAddress is a workspace's address, "" when unknown (its apps
// domain when only that is wired, as in tests).
func (c Config) workspaceAddress(ws string) string {
	if c.WorkspaceAddress != nil {
		if a := c.WorkspaceAddress(ws); a != "" {
			return a
		}
	}
	if c.WorkspaceDomain != nil {
		return c.WorkspaceDomain(ws)
	}
	return ""
}
