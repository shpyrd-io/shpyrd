package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

func legacyTeam(name string, members ...string) *unstructured.Unstructured {
	ms := make([]interface{}, len(members))
	for i, m := range members {
		ms[i] = m
	}
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "shpyrd.io/v1alpha1", "kind": "Team",
		"metadata": map[string]interface{}{"name": name},
		"spec":     map[string]interface{}{"members": ms},
	}}
}

func legacyMember(name, project, role, user, team string) *unstructured.Unstructured {
	spec := map[string]interface{}{"project": project, "role": role}
	if user != "" {
		spec["user"] = user
	}
	if team != "" {
		spec["team"] = team
	}
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "shpyrd.io/v1alpha1", "kind": "ProjectMember",
		"metadata": map[string]interface{}{"name": name},
		"spec":     spec,
	}}
}

func legacyClient(objs ...runtime.Object) *dynfake.FakeDynamicClient {
	return dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		shpyrdv1.LegacyTeams:          "TeamList",
		shpyrdv1.LegacyProjectMembers: "ProjectMemberList",
	}, objs...)
}

func migratedMark(t *testing.T, dyn *dynfake.FakeDynamicClient, gvr schema.GroupVersionResource, name string) string {
	t.Helper()
	u, err := dyn.Resource(gvr).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return u.GetAnnotations()[shpyrdv1.AnnotationMigrated]
}

func TestImportCRDsImportsIntoAStoreThatAlreadyHasTeams(t *testing.T) {
	// The built-in team "everyone" is in every store from its first start,
	// and the old importer ran only into a store with no team at all: it
	// never imported anything (#249). Nor did it know people nobody had
	// signed in as yet, which a grant needs.
	ctx := context.Background()
	st := NewMemory()
	dyn := legacyClient(
		legacyTeam("platform", "ops@example.test"),
		legacyMember("shop-admin-team-platform", "shop", "admin", "", "platform"),
		legacyMember("shop-developer-user-dev", "shop", "developer", "dev@example.test", ""),
	)
	imp, err := ImportCRDs(ctx, dyn, st, DefaultWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if imp.Teams != 1 || imp.Grants != 2 || len(imp.Kept) != 0 || len(imp.Skipped) != 0 {
		t.Fatalf("import = %+v, want 1 team and 2 grants", imp)
	}
	if _, err := st.GetTeam(ctx, DefaultWorkspace, "platform"); err != nil {
		t.Errorf("team platform: %v", err)
	}
	grants, _ := st.ListGrants(ctx, DefaultWorkspace)
	if len(grants) != 2 {
		t.Errorf("grants = %+v", grants)
	}
	if mark := migratedMark(t, dyn, shpyrdv1.LegacyProjectMembers, "shop-developer-user-dev"); mark == "" || strings.HasPrefix(mark, "skipped") {
		t.Errorf("member mark = %q, want an import time", mark)
	}

	// Idempotent: a second start finds every object marked.
	again, err := ImportCRDs(ctx, dyn, st, DefaultWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if again.Teams+again.Grants+len(again.Kept)+len(again.Skipped) != 0 {
		t.Errorf("second import = %+v, want nothing to do", again)
	}
}

func TestImportCRDsKeepsTheStoresTeam(t *testing.T) {
	// A partly migrated cluster: the store has the team, edited since, and the
	// object was never marked. The store's version stands.
	ctx := context.Background()
	st := NewMemory()
	if _, err := st.TouchIdentity(ctx, DefaultWorkspace, Identity{Email: "new@example.test"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PutTeam(ctx, DefaultWorkspace, Team{Name: "platform", Description: "edited", Members: []string{"new@example.test"}}); err != nil {
		t.Fatal(err)
	}
	dyn := legacyClient(legacyTeam("platform", "old@example.test"))
	imp, err := ImportCRDs(ctx, dyn, st, DefaultWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if imp.Teams != 0 || len(imp.Kept) != 1 || imp.Kept[0] != "platform" {
		t.Errorf("import = %+v, want platform kept", imp)
	}
	team, _ := st.GetTeam(ctx, DefaultWorkspace, "platform")
	if team.Description != "edited" {
		t.Errorf("team = %+v, want the store's version", team)
	}
	if migratedMark(t, dyn, shpyrdv1.LegacyTeams, "platform") == "" {
		t.Error("a kept team must be marked, or the installer waits on it forever")
	}
}

func TestImportCRDsMarksWhatItCannotImportAsSkipped(t *testing.T) {
	// A grant to a team that no longer exists has nowhere to go. It is not
	// dropped quietly: the mark says why, the result names it, and the
	// installer exports the object before it removes the definitions.
	ctx := context.Background()
	st := NewMemory()
	dyn := legacyClient(legacyMember("shop-viewer-team-gone", "shop", "viewer", "", "gone"))
	imp, err := ImportCRDs(ctx, dyn, st, DefaultWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if imp.Grants != 0 || len(imp.Skipped) != 1 || !strings.Contains(imp.Skipped[0], "shop-viewer-team-gone") {
		t.Errorf("import = %+v, want the member named as skipped", imp)
	}
	if mark := migratedMark(t, dyn, shpyrdv1.LegacyProjectMembers, "shop-viewer-team-gone"); !strings.HasPrefix(mark, "skipped:") {
		t.Errorf("mark = %q, want it to say skipped and why", mark)
	}
}

func TestImportCRDsWithoutTheDefinitions(t *testing.T) {
	// A fresh install, or one whose definitions are already retired.
	dyn := legacyClient()
	dyn.PrependReactor("list", "*", func(a k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Group: "shpyrd.io", Resource: a.GetResource().Resource}, "")
	})
	imp, err := ImportCRDs(context.Background(), dyn, NewMemory(), DefaultWorkspace)
	if err != nil || imp.Teams+imp.Grants != 0 {
		t.Errorf("import = %+v, %v; want nothing and no error", imp, err)
	}
}

func TestImportCRDsReportsAFailureToList(t *testing.T) {
	// Anything but absent definitions is an error, not "nothing to import":
	// the old importer said nothing on a forbidden list.
	dyn := legacyClient(legacyTeam("platform"))
	dyn.PrependReactor("list", "teams", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "shpyrd.io", Resource: "teams"}, "", errors.New("denied"))
	})
	if _, err := ImportCRDs(context.Background(), dyn, NewMemory(), DefaultWorkspace); err == nil {
		t.Error("a forbidden list was taken for absent definitions")
	}
}
