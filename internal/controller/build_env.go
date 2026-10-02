package controller

import (
	"context"
	"fmt"
	"sort"

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
// build gets the project's build.env as values on the kpack Image, and the
// workspace's global config vars, the project's own, the bound vars of its
// attachments and the plain env: of shpyrd.yaml through the ConfigMap
// <app>-build-env, in the order the processes read them (the later wins),
// with build.env above them all.
//
// kpack refuses Secret references in a build's env (a tenant able to
// write an Image could read any Secret of its namespace through a build),
// so the platform mirrors the values into a ConfigMap of its own and the
// Image names them by reference: the Image never carries a value. A value
// that changes is read by the next build; a name that appears joins the
// Image when a build happens anyway (a new archive, a redeploy), so setting
// a config var never starts a build by itself. A Git source is the
// exception: kpack builds each new commit without the Image changing, so a
// new name joins at once, with one build.

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

// reconcileBuildEnv writes the ConfigMap the build reads the variables
// from, or removes it when the app is not built by buildpacks.
func (r *AppReconciler) reconcileBuildEnv(ctx context.Context, app *shpyrdv1.App, vars map[string]string) error {
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: app.BuildEnvName(), Namespace: app.Namespace}}
	if !app.HasSource() || app.BuildStrategy() == shpyrdv1.StrategyDockerfile {
		if err := r.Get(ctx, client.ObjectKeyFromObject(cm), cm); err == nil {
			return r.deleteIfExists(ctx, cm)
		}
		return nil
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		cm.Labels = mergeMaps(cm.Labels, commonLabels(app))
		cm.Data = vars
		return controllerutil.SetControllerReference(app, cm, r.Scheme)
	})
	if err != nil {
		return fmt.Errorf("build variables: %w", err)
	}
	return nil
}

// buildVarNames lists the variables a build is given by reference: those
// of vars that the project's build.env does not set itself, sorted.
func buildVarNames(app *shpyrdv1.App, vars map[string]string) []string {
	own := map[string]bool{}
	if app.Spec.Build != nil {
		for _, e := range app.Spec.Build.Env {
			own[e.Name] = true
		}
	}
	var out []string
	for k := range vars {
		if !own[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// buildVarRef is the kpack env entry that reads one variable from the
// ConfigMap; optional, so a variable removed since does not stop a build.
func buildVarRef(app *shpyrdv1.App, name string) map[string]interface{} {
	return map[string]interface{}{
		"name": name,
		"valueFrom": map[string]interface{}{"configMapKeyRef": map[string]interface{}{
			"name": app.BuildEnvName(), "key": name, "optional": true,
		}},
	}
}

func isBuildVarRef(e interface{}) bool {
	m, ok := e.(map[string]interface{})
	if !ok {
		return false
	}
	_, ref := m["valueFrom"]
	return ref
}

// buildChanges says desired builds something else than current: another
// source, or other build.env values. Not the whole spec: kpack writes
// defaults into a stored Image (build.resources: {}), so the stored spec
// never equals the one rendered.
func buildChanges(current, desired *unstructured.Unstructured) bool {
	curSrc, _, _ := unstructured.NestedMap(current.Object, "spec", "source")
	desSrc, _, _ := unstructured.NestedMap(desired.Object, "spec", "source")
	return !equalJSON(curSrc, desSrc) || !equalJSON(buildValues(current), buildValues(desired))
}

// buildValues are the build.env entries with values (the project's own).
func buildValues(img *unstructured.Unstructured) []interface{} {
	env, _, _ := unstructured.NestedSlice(img.Object, "spec", "build", "env")
	var out []interface{}
	for _, e := range env {
		if !isBuildVarRef(e) {
			out = append(out, e)
		}
	}
	return out
}

// buildRefs are the build.env entries that read a variable by reference.
func buildRefs(img *unstructured.Unstructured) []interface{} {
	env, _, _ := unstructured.NestedSlice(img.Object, "spec", "build", "env")
	var out []interface{}
	for _, e := range env {
		if isBuildVarRef(e) {
			out = append(out, e)
		}
	}
	return out
}

// withBuildVarsOf is desired with the variable names current carries: what
// the Image would be if only the values of build.env and the rest changed.
func withBuildVarsOf(desired, current *unstructured.Unstructured) *unstructured.Unstructured {
	out := desired.DeepCopy()
	des, _, _ := unstructured.NestedSlice(out.Object, "spec", "build", "env")
	cur, _, _ := unstructured.NestedSlice(current.Object, "spec", "build", "env")
	own := map[string]bool{}
	var env []interface{}
	for _, e := range des {
		if !isBuildVarRef(e) {
			env = append(env, e)
			own[fmt.Sprint(e.(map[string]interface{})["name"])] = true
		}
	}
	for _, e := range cur {
		if isBuildVarRef(e) && !own[fmt.Sprint(e.(map[string]interface{})["name"])] {
			env = append(env, e)
		}
	}
	if len(env) > 0 {
		_ = unstructured.SetNestedSlice(out.Object, env, "spec", "build", "env")
		return out
	}
	unstructured.RemoveNestedField(out.Object, "spec", "build", "env")
	if b, _, _ := unstructured.NestedMap(out.Object, "spec", "build"); len(b) == 0 {
		unstructured.RemoveNestedField(out.Object, "spec", "build")
	}
	return out
}
