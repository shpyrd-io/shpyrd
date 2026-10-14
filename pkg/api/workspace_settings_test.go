package api

import (
	"context"
	"reflect"
	"strings"
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// A plan's database rules: the kinds allowed, the default kind, the shared cap.
func TestNormalizePostgresLimits(t *testing.T) {
	got, err := normalizePostgresLimits(store.PostgresLimits{Types: []string{"Shared", "shared", " dedicated "}, DefaultType: "SHARED", SharedCap: "1024Mi"})
	if err != nil || !reflect.DeepEqual(got.Types, []string{"shared", "dedicated"}) || got.DefaultType != "shared" || got.SharedCap != "1Gi" {
		t.Errorf("normalized = %+v, %v", got, err)
	}
	if _, err := normalizePostgresLimits(store.PostgresLimits{Types: []string{"shared"}, DefaultType: "dedicated"}); err == nil || !strings.Contains(err.Error(), "not among the allowed types") {
		t.Errorf("a default outside the allowed types: %v", err)
	}
	if _, err := normalizePostgresLimits(store.PostgresLimits{Types: []string{"huge"}}); err == nil {
		t.Error("an unknown type must be refused")
	}
	if _, err := normalizePostgresLimits(store.PostgresLimits{SharedCap: "lots"}); err == nil {
		t.Error("a cap that is not a quantity must be refused")
	}
	if got, err := normalizePostgresLimits(store.PostgresLimits{}); err != nil || got != nil {
		t.Errorf("nothing said is nil: %+v %v", got, err)
	}
}

// A new database's kind follows the workspace's plan: the type asked for
// within what the plan allows, else the plan's default, else dedicated; a
// shared one takes the plan's cap and carries no instance settings.
func TestApplyPostgresTypeFollowsThePlan(t *testing.T) {
	s, _, st := newTenantServer(t)
	ctx := context.Background()
	spec := map[string]interface{}{}
	if cap, err := s.applyPostgresType(ctx, "acme", spec); err != nil || spec["type"] != shpyrdv1.PostgresTypeDedicated || spec["storage"] != "5Gi" || cap != "" {
		t.Errorf("no plan rules: %v %v %q", spec, err, cap)
	}
	if _, err := st.UpdateWorkspaceSettings(ctx, "acme", store.WorkspaceSettings{Limits: &store.Limits{Postgres: &store.PostgresLimits{DefaultType: "shared", SharedCap: "2Gi"}}}); err != nil {
		t.Fatal(err)
	}
	spec = map[string]interface{}{}
	if cap, err := s.applyPostgresType(ctx, "acme", spec); err != nil || spec["type"] != shpyrdv1.PostgresTypeShared || spec["storage"] != "2Gi" || cap != "2Gi" {
		t.Errorf("the plan's default and cap: %v %v %q", spec, err, cap)
	}
	spec = map[string]interface{}{"type": "shared", "instances": int64(2)}
	if _, err := s.applyPostgresType(ctx, "acme", spec); err == nil || !strings.Contains(err.Error(), "always on") {
		t.Errorf("instance settings on a shared database: %v", err)
	}
	if _, err := st.UpdateWorkspaceSettings(ctx, "acme", store.WorkspaceSettings{Limits: &store.Limits{Postgres: &store.PostgresLimits{Types: []string{"shared"}}}}); err != nil {
		t.Fatal(err)
	}
	spec = map[string]interface{}{"type": "dedicated"}
	if _, err := s.applyPostgresType(ctx, "acme", spec); err == nil || !strings.Contains(err.Error(), "allows only shared") {
		t.Errorf("a kind the plan does not allow: %v", err)
	}
}
