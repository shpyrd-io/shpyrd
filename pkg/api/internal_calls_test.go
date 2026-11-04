package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// instantProm answers instant queries: a vector per fragment of PromQL.
func instantProm(t *testing.T, answers map[string][]map[string]any) (*PromClient, *[]string) {
	t.Helper()
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		queries = append(queries, q)
		result := []map[string]any{}
		for match, series := range answers {
			if strings.Contains(q, match) {
				result = series
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": result}})
	}))
	t.Cleanup(srv.Close)
	return &PromClient{BaseURL: srv.URL, HTTP: srv.Client()}, &queries
}

func TestProjectCallsComeFromTheServiceCallsCounter(t *testing.T) {
	crm := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "crm", Namespace: "app-crm"}, Spec: shpyrdv1.AppSpec{Allow: []shpyrdv1.AllowEntry{{Project: "expenses"}, {Project: "shop"}}}}
	last := time.Date(2026, 11, 4, 18, 30, 0, 0, time.UTC)
	prom, queries := instantProm(t, map[string][]map[string]any{
		"increase(shpyrd_edge_service_calls_total{workspace=\"default\",project=\"crm\"}[24h])": {
			{"metric": map[string]string{"caller": "expenses"}, "value": []any{1.0, "1203.6"}},
			{"metric": map[string]string{"caller": "shop"}, "value": []any{1.0, "7"}},
			{"metric": map[string]string{"caller": "billing"}, "value": []any{1.0, "0"}}, // nothing in the window
		},
		"max_over_time": {
			{"metric": map[string]string{"caller": "expenses"}, "value": []any{1.0, fmt.Sprint(last.Unix())}},
		},
	})
	s, _ := newTestServer(t, prom, []client.Object{crm})
	rec := do(t, s, "GET", "/api/projects/crm/calls", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("calls = %d %s", rec.Code, rec.Body.String())
	}
	var out ProjectCalls
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Available || len(out.Callers) != 2 {
		t.Fatalf("calls = %+v", out)
	}
	// Most calls first, counts rounded, the last call to the minute; a
	// caller the window saw nothing from is not listed.
	if out.Callers[0].Caller != "expenses" || out.Callers[0].Calls != 1204 || out.Callers[0].LastAt == nil || !out.Callers[0].LastAt.Equal(last) {
		t.Errorf("expenses = %+v (last %v)", out.Callers[0], out.Callers[0].LastAt)
	}
	if out.Callers[1].Caller != "shop" || out.Callers[1].Calls != 7 || out.Callers[1].LastAt != nil {
		t.Errorf("shop = %+v", out.Callers[1])
	}
	// The queries name the callee by workspace and project, over a day.
	if len(*queries) != 2 || !strings.Contains((*queries)[0], `workspace="default",project="crm"`) || !strings.Contains((*queries)[1], "[24h:1m]") {
		t.Errorf("queries = %v", *queries)
	}
	// A project that is not there.
	if rec := do(t, s, "GET", "/api/projects/nope/calls", "", true); rec.Code != http.StatusNotFound {
		t.Errorf("unknown project = %d", rec.Code)
	}
}

func TestProjectCallsWithoutMetricsSaySo(t *testing.T) {
	crm := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "crm", Namespace: "app-crm"}}
	s, _ := newTestServer(t, nil, []client.Object{crm})
	rec := do(t, s, "GET", "/api/projects/crm/calls", "", true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"available":false`) || !strings.Contains(rec.Body.String(), `"callers":[]`) {
		t.Errorf("no metrics = %d %s", rec.Code, rec.Body.String())
	}
}
