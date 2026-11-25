package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

func legacyCRD(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinition",
		"metadata": map[string]interface{}{"name": name},
	}}
}

func legacyObject(kind, name, migrated string) *unstructured.Unstructured {
	meta := map[string]interface{}{"name": name}
	if migrated != "" {
		meta["annotations"] = map[string]interface{}{shpyrdv1.AnnotationMigrated: migrated}
	}
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "shpyrd.io/v1alpha1", "kind": kind, "metadata": meta,
		"spec": map[string]interface{}{"members": []interface{}{"ops@example.test"}},
	}}
}

func legacyCluster(objs ...kruntime.Object) *dynfake.FakeDynamicClient {
	return dynfake.NewSimpleDynamicClientWithCustomListKinds(kruntime.NewScheme(), map[schema.GroupVersionResource]string{
		crdsGVR:                       "CustomResourceDefinitionList",
		shpyrdv1.LegacyTeams:          "TeamList",
		shpyrdv1.LegacyProjectMembers: "ProjectMemberList",
	}, objs...)
}

func definitionsLeft(t *testing.T, dyn *dynfake.FakeDynamicClient) []string {
	t.Helper()
	var left []string
	for _, l := range legacyMembership {
		if _, err := dyn.Resource(crdsGVR).Get(context.Background(), l.crd, metav1.GetOptions{}); err == nil {
			left = append(left, l.crd)
		} else if !apierrors.IsNotFound(err) {
			t.Fatal(err)
		}
	}
	return left
}

var retireAt = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

func TestRetireLegacyMembershipExportsThenRemoves(t *testing.T) {
	dir := t.TempDir()
	dyn := legacyCluster(
		legacyCRD("teams.shpyrd.io"), legacyCRD("projectmembers.shpyrd.io"),
		legacyObject("Team", "platform", "1761652800"),
		legacyObject("ProjectMember", "shop-viewer-team-gone", "skipped: its team or project is gone"),
	)
	got, err := retireLegacyMembership(context.Background(), dyn, dir, retireAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Removed) != 2 || got.Objects != 2 {
		t.Errorf("retirement = %+v, want both definitions and both objects", got)
	}
	if left := definitionsLeft(t, dyn); len(left) != 0 {
		t.Errorf("definitions left: %v", left)
	}
	b, err := os.ReadFile(got.Export)
	if err != nil {
		t.Fatal(err)
	}
	// A skipped object is in the export too: removing its definition is
	// the last moment anything still holds it.
	for _, want := range []string{"name: platform", "name: shop-viewer-team-gone", "kind: ProjectMember"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("export lacks %q:\n%s", want, b)
		}
	}
	if filepath.Base(got.Export) != "legacy-membership-20261028T120000Z.yaml" {
		t.Errorf("export = %s", got.Export)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(got.Export); fi.Mode().Perm() != 0o600 {
			t.Errorf("export mode = %v: it names people", fi.Mode().Perm())
		}
	}
}

func TestRetireLegacyMembershipRefusesWhileAnObjectIsUnimported(t *testing.T) {
	// Removing a definition removes its objects: one the server has not
	// imported would be lost, so nothing goes and the error names it.
	dir := t.TempDir()
	dyn := legacyCluster(
		legacyCRD("teams.shpyrd.io"), legacyCRD("projectmembers.shpyrd.io"),
		legacyObject("Team", "platform", "1761652800"),
		legacyObject("Team", "finance", ""),
	)
	_, err := retireLegacyMembership(context.Background(), dyn, dir, retireAt)
	var unimported *ErrLegacyUnimported
	if !errors.As(err, &unimported) || len(unimported.Names) != 1 || unimported.Names[0] != "team/finance" {
		t.Fatalf("err = %v, want the unimported team named", err)
	}
	if !strings.Contains(err.Error(), "rollout restart") {
		t.Errorf("err = %q, want it to say what to do", err)
	}
	if left := definitionsLeft(t, dyn); len(left) != 2 {
		t.Errorf("definitions left = %v, want both untouched", left)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("an export was written though nothing was removed: %v", entries)
	}
}

func TestRetireLegacyMembershipIsIdempotent(t *testing.T) {
	// A fresh install never had them; a second run finds them gone; a cluster
	// that kept one definition without objects loses it with no export.
	for name, objs := range map[string][]kruntime.Object{
		"none":             nil,
		"one, no objects":  {legacyCRD("projectmembers.shpyrd.io")},
		"both, no objects": {legacyCRD("teams.shpyrd.io"), legacyCRD("projectmembers.shpyrd.io")},
	} {
		t.Run(name, func(t *testing.T) {
			dyn := legacyCluster(objs...)
			first, err := retireLegacyMembership(context.Background(), dyn, t.TempDir(), retireAt)
			if err != nil {
				t.Fatal(err)
			}
			if len(first.Removed) != len(objs) || first.Export != "" {
				t.Errorf("first run = %+v", first)
			}
			second, err := retireLegacyMembership(context.Background(), dyn, t.TempDir(), retireAt)
			if err != nil || len(second.Removed) != 0 {
				t.Errorf("second run = %+v, %v; want nothing to do", second, err)
			}
		})
	}
}
