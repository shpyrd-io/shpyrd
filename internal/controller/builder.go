package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// Build composition (RFC-0065): a project that names buildpacks or a stack
// gets a kpack Builder of its own, composed from the platform's catalog of
// ClusterBuildpacks and ClusterStacks; the kpack Image builds with it.

// BuilderGVK is kpack's namespaced Builder.
var BuilderGVK = schema.GroupVersionKind{Group: "kpack.io", Version: "v1alpha2", Kind: "Builder"}

// BuildpackCatalog maps the short names people write in shpyrd.yaml to the
// platform's ClusterBuildpack names (deploy/components/kpack). A
// ClusterBuildpack name itself is accepted as well.
var BuildpackCatalog = map[string]string{
	"java":         "paketo-java",
	"nodejs":       "paketo-nodejs",
	"node":         "paketo-nodejs",
	"go":           "paketo-go",
	"python":       "paketo-python",
	"ruby":         "paketo-ruby",
	"php":          "paketo-php",
	"dotnet":       "paketo-dotnet-core",
	"dotnet-core":  "paketo-dotnet-core",
	"web-servers":  "paketo-web-servers",
	"nginx":        "paketo-web-servers",
	"httpd":        "paketo-web-servers",
	"procfile":     "paketo-procfile",
	"deb-packages": "heroku-deb-packages",
	"apt":          "heroku-deb-packages",
}

// Stacks maps the stack names of shpyrd.yaml to ClusterStack names.
var Stacks = map[string]string{"": "jammy", "base": "jammy", "full": "jammy-full"}

// CatalogNames lists the short names, sorted, for error messages.
func CatalogNames() []string {
	seen := map[string]bool{}
	var out []string
	for short, cluster := range BuildpackCatalog {
		if seen[cluster] {
			continue
		}
		// One name per entry: the first alphabetically is the canonical one.
		best := short
		for s, c := range BuildpackCatalog {
			if c == cluster && s < best {
				best = s
			}
		}
		seen[cluster] = true
		out = append(out, best)
	}
	sort.Strings(out)
	return out
}

// ResolveBuildpack maps a name from shpyrd.yaml to a ClusterBuildpack name.
func ResolveBuildpack(name string) (string, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	if c, ok := BuildpackCatalog[n]; ok {
		return c, true
	}
	if strings.HasPrefix(n, "paketo-") || strings.HasPrefix(n, "heroku-") {
		return n, true // a ClusterBuildpack named directly
	}
	return "", false
}

// composesBuild says the app asked for a builder of its own.
func composesBuild(app *shpyrdv1.App) bool {
	return app.Spec.Build != nil && (len(app.Spec.Build.Buildpacks) > 0 || app.Spec.Build.Stack != "" || app.Spec.Build.SystemPackages)
}

// detectionOrder is the platform builder's order, as short names.
var detectionOrder = []string{"java", "nodejs", "go", "python", "ruby", "php", "dotnet", "web-servers", "procfile"}

// builderName is the project's Builder.
func builderName(app *shpyrdv1.App) string { return app.Name + "-builder" }

// desiredBuilder renders the project's kpack Builder.
func (c Config) desiredBuilder(app *shpyrdv1.App) (*unstructured.Unstructured, error) {
	stack, ok := Stacks[strings.ToLower(app.Spec.Build.Stack)]
	if !ok {
		return nil, fmt.Errorf("build.stack must be base or full, got %q", app.Spec.Build.Stack)
	}
	tag, err := c.imageTag(app)
	if err != nil {
		return nil, err
	}
	entry := func(cluster string) map[string]interface{} {
		return map[string]interface{}{"name": cluster, "kind": "ClusterBuildpack"}
	}
	// An Aptfile puts the .deb packages buildpack in front of every group.
	var prefix []interface{}
	if app.Spec.Build.SystemPackages {
		prefix = append(prefix, entry(BuildpackCatalog["deb-packages"]))
	}
	var group []interface{}
	names := app.Spec.Build.Buildpacks
	if len(names) == 0 {
		// No explicit list: the platform's detection, one group per
		// language in the platform builder's order, on the chosen stack.
		for _, n := range detectionOrder {
			g := append(append([]interface{}{}, prefix...), entry(BuildpackCatalog[n]))
			group = append(group, map[string]interface{}{"group": g})
		}
	} else {
		entries := append([]interface{}{}, prefix...)
		for _, n := range names {
			cluster, ok := ResolveBuildpack(n)
			if !ok {
				return nil, fmt.Errorf("unknown buildpack %q; the catalog has: %s", n, strings.Join(CatalogNames(), ", "))
			}
			if cluster == BuildpackCatalog["deb-packages"] && app.Spec.Build.SystemPackages {
				continue // already first
			}
			entries = append(entries, entry(cluster))
		}
		group = []interface{}{map[string]interface{}{"group": entries}}
	}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(BuilderGVK)
	u.SetName(builderName(app))
	u.SetNamespace(app.Namespace)
	u.SetLabels(commonLabels(app))
	u.Object["spec"] = map[string]interface{}{
		"tag":                tag + "/builder",
		"stack":              map[string]interface{}{"name": stack, "kind": "ClusterStack"},
		"serviceAccountName": c.buildServiceAccountName(),
		"order":              group,
	}
	return u, nil
}

// reconcileBuilder keeps (or removes) the project's Builder and returns the
// builder reference the Image should use.
func (r *AppReconciler) reconcileBuilder(ctx context.Context, app *shpyrdv1.App) (map[string]interface{}, error) {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(BuilderGVK)
	existing.SetName(builderName(app))
	existing.SetNamespace(app.Namespace)
	if !composesBuild(app) {
		if err := r.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) && !isNoKind(err) {
			return nil, fmt.Errorf("remove builder: %w", err)
		}
		builder := r.Config.DefaultBuilder
		if app.Spec.Build != nil && app.Spec.Build.Builder != "" {
			builder = app.Spec.Build.Builder
		}
		return map[string]interface{}{"name": builder, "kind": "ClusterBuilder"}, nil
	}
	desired, err := r.Config.desiredBuilder(app)
	if err != nil {
		return nil, err
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, existing, func() error {
		existing.SetLabels(mergeMaps(existing.GetLabels(), desired.GetLabels()))
		existing.Object["spec"] = desired.Object["spec"]
		return controllerutil.SetControllerReference(app, existing, r.Scheme)
	})
	if err != nil {
		return nil, fmt.Errorf("builder: %w", err)
	}
	return map[string]interface{}{"name": builderName(app), "kind": "Builder"}, nil
}

var _ client.Object = &unstructured.Unstructured{}
