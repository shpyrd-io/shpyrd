package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// Builds see the variables the app runs with (#54): `prisma generate`,
// Next pages that read the database while collecting page data,
// NEXT_PUBLIC_* inlined into assets, Rails' assets:precompile. A buildpack
// build gets the project's build.env as values on the kpack Image. The
// workspace's global config vars, the project's own, the bound vars of its
// attachments and the plain env: of shpyrd.yaml, in the order the processes
// read them (the later wins), reach it through the Secret <app>-build-env:
// the Image binds it as a service, kpack mounts it into the build and
// nowhere else, and the platform's buildpack shpyrd/build-env
// (buildpacks/build-env), first in every group of the platform's builders,
// gives it to the buildpacks after it as build-time variables, below
// build.env. kpack refuses Secret references in a build's env
// (buildpacks-community/kpack#1945); a binding is the way it offers.
//
// The Image names the Secret, never a variable or a value: setting a config
// var never starts a build, and the next build reads the Secret as it is
// then. The buildpack's layer is for the build only, so nothing of it
// reaches the image or the build cache. What the app's own build prints is
// in its log, as everywhere.

const (
	// buildEnvBindingType is the binding type the buildpack looks for.
	buildEnvBindingType = "shpyrd-env"
	// BuildEnvBuildpack is the ClusterBuildpack of buildpacks/build-env.
	BuildEnvBuildpack = "shpyrd-build-env"
)

// buildVars merges the variables of the given Secrets, later ones winning,
// and the plain variables of shpyrd.yaml's env: over them, as a process
// reads them (its env entries win over its envFrom sources).
func buildVars(app *shpyrdv1.App, sources ...*corev1.Secret) map[string]string {
	out := map[string]string{}
	for _, s := range sources {
		if s == nil {
			continue
		}
		for k, v := range s.Data {
			out[k] = string(v)
		}
	}
	for _, e := range app.Spec.Env {
		if e.ValueFrom == nil {
			out[e.Name] = e.Value
		}
	}
	return out
}

// reconcileBuildEnv writes the Secret the build reads the variables from,
// or removes it when the app is not built by buildpacks. The binding's own
// names, type and provider, cannot be variables of the build.
func (r *AppReconciler) reconcileBuildEnv(ctx context.Context, app *shpyrdv1.App, vars map[string]string) error {
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: app.BuildEnvName(), Namespace: app.Namespace}}
	if !app.HasSource() || app.BuildStrategy() == shpyrdv1.StrategyDockerfile {
		if err := r.Get(ctx, client.ObjectKeyFromObject(s), s); err == nil {
			return r.deleteIfExists(ctx, s)
		}
		return nil
	}
	data := map[string][]byte{"type": []byte(buildEnvBindingType)}
	for k, v := range vars {
		if k != "type" && k != "provider" {
			data[k] = []byte(v)
		}
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, s, func() error {
		s.Type = corev1.SecretTypeOpaque
		s.Labels = mergeMaps(s.Labels, commonLabels(app))
		s.Data = data
		return controllerutil.SetControllerReference(app, s, r.Scheme)
	})
	if err != nil {
		return fmt.Errorf("build variables: %w", err)
	}
	return nil
}

// buildEnvService is the kpack service that binds the Secret to the build.
func buildEnvService(app *shpyrdv1.App) map[string]interface{} {
	return map[string]interface{}{"name": app.BuildEnvName(), "kind": "Secret", "apiVersion": "v1"}
}

// buildChanges says desired builds something else than current: another
// source, or other build.env values. Not the whole spec: kpack writes
// defaults into a stored Image (build.resources: {}), so the stored spec
// never equals the one rendered.
func buildChanges(current, desired *unstructured.Unstructured) bool {
	curSrc, _, _ := unstructured.NestedMap(current.Object, "spec", "source")
	desSrc, _, _ := unstructured.NestedMap(desired.Object, "spec", "source")
	curEnv, _, _ := unstructured.NestedSlice(current.Object, "spec", "build", "env")
	desEnv, _, _ := unstructured.NestedSlice(desired.Object, "spec", "build", "env")
	return !equalJSON(curSrc, desSrc) || !equalJSON(curEnv, desEnv)
}

// buildServices are the services an Image binds to its builds.
func buildServices(img *unstructured.Unstructured) []interface{} {
	s, _, _ := unstructured.NestedSlice(img.Object, "spec", "build", "services")
	return s
}

// withServicesOf is desired binding what current binds: an Image from
// before builds read the project's variables binds the Secret with its next
// build, not by starting one.
func withServicesOf(desired, current *unstructured.Unstructured) *unstructured.Unstructured {
	out := desired.DeepCopy()
	if s := buildServices(current); len(s) > 0 {
		_ = unstructured.SetNestedSlice(out.Object, s, "spec", "build", "services")
		return out
	}
	unstructured.RemoveNestedField(out.Object, "spec", "build", "services")
	if b, _, _ := unstructured.NestedMap(out.Object, "spec", "build"); len(b) == 0 {
		unstructured.RemoveNestedField(out.Object, "spec", "build")
	}
	return out
}
