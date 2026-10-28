package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// RFC-0047: bounds go on the processes endpoint like sleep does. They need
// KEDA's kinds (the autoscale extension); the web process alone follows
// its load; the numbers are checked in words. A count on a process with
// bounds sets its minimum and is kept; above the maximum, or zero, it is
// refused; off clears the bounds and the last count stays.
func TestAutoscaleBoundsOnTheProcessesEndpoint(t *testing.T) {
	s, cr := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning)})
	key := types.NamespacedName{Namespace: "app-web1", Name: "web1"}
	web := func() shpyrdv1.Process {
		t.Helper()
		got := &shpyrdv1.App{}
		if err := cr.Get(context.Background(), key, got); err != nil {
			t.Fatal(err)
		}
		return got.Spec.Processes["web"]
	}

	// Refused up-front on a cluster without KEDA's kinds; off never needs them.
	s.autoscaleAvailable = func() bool { return false }
	if rec := do(t, s, "POST", "/api/projects/web1/processes", `{"processes":{"web":{"autoscale":{"min":2,"max":10}}}}`, true); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "enable the autoscale extension") {
		t.Errorf("bounds without KEDA: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "POST", "/api/projects/web1/processes", `{"processes":{"web":{"autoscale":{"off":true}}}}`, true); rec.Code != http.StatusOK {
		t.Errorf("off without KEDA: %d %s", rec.Code, rec.Body.String())
	}
	s.autoscaleAvailable = func() bool { return true }

	for _, bad := range []struct{ body, says string }{
		{`{"processes":{"web":{"autoscale":{"min":0,"max":10}}}}`, "at least 1"},
		{`{"processes":{"web":{"autoscale":{"min":5,"max":4}}}}`, "at least the minimum"},
		{`{"processes":{"web":{"autoscale":{"min":1,"max":101}}}}`, "at most 100"},
		{`{"processes":{"web":{"autoscale":{"min":1,"max":4,"cpu":0}}}}`, "percentage"},
		{`{"processes":{"web":{"autoscale":{"min":1,"max":4,"cpu":101}}}}`, "percentage"},
		{`{"processes":{"web":{"autoscale":{"min":1,"max":4,"concurrency":0}}}}`, "at least 1"},
		{`{"processes":{"worker":{"autoscale":{"min":1,"max":4}}}}`, "only the web process"},
	} {
		rec := do(t, s, "POST", "/api/projects/web1/processes", bad.body, true)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), bad.says) {
			t.Errorf("%s: %d %s, want 400 saying %q", bad.body, rec.Code, rec.Body.String(), bad.says)
		}
	}

	if rec := do(t, s, "POST", "/api/projects/web1/processes", `{"processes":{"web":{"autoscale":{"min":2,"max":10,"cpu":60}}}}`, true); rec.Code != http.StatusOK {
		t.Fatalf("bounds: %d %s", rec.Code, rec.Body.String())
	}
	if as := web().Autoscale; as == nil || as.Min != 2 || as.Max != 10 || as.CPU == nil || *as.CPU != 60 || as.Concurrency != nil {
		t.Errorf("bounds not stored: %+v", as)
	}

	// A count sets the minimum, by the scale endpoint or in a batch.
	if rec := do(t, s, "POST", "/api/projects/web1/scale", `{"process":"web","replicas":3}`, true); rec.Code != http.StatusOK {
		t.Fatalf("scale: %d %s", rec.Code, rec.Body.String())
	}
	if p := web(); p.Autoscale.Min != 3 || p.Autoscale.Max != 10 || p.Replicas == nil || *p.Replicas != 3 {
		t.Errorf("scale on bounds: autoscale %+v replicas %v, want min 3 and the count kept", p.Autoscale, p.Replicas)
	}
	rec := do(t, s, "POST", "/api/projects/web1/scale", `{"process":"web","replicas":11}`, true)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "above the maximum") {
		t.Errorf("scale above the maximum: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, s, "POST", "/api/projects/web1/processes", `{"processes":{"web":{"replicas":0}}}`, true)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "turn autoscaling off") {
		t.Errorf("zero on bounds: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "POST", "/api/projects/web1/processes", `{"processes":{"web":{"replicas":4}}}`, true); rec.Code != http.StatusOK {
		t.Fatalf("batch count on bounds: %d %s", rec.Code, rec.Body.String())
	}
	if p := web(); p.Autoscale.Min != 4 {
		t.Errorf("batch count must set the minimum, got %+v", p.Autoscale)
	}

	// Off clears the bounds; the last count stays.
	if rec := do(t, s, "POST", "/api/projects/web1/processes", `{"processes":{"web":{"autoscale":{"off":true}}}}`, true); rec.Code != http.StatusOK {
		t.Fatalf("off: %d %s", rec.Code, rec.Body.String())
	}
	if p := web(); p.Autoscale != nil || p.Replicas == nil || *p.Replicas != 4 {
		t.Errorf("off: %+v, want no bounds and 4 instances", p)
	}
}

// The maximum is what a process may come to, so the workspace's instance
// ceiling judges it (RFC-0042 bounds max), and the usage counts it.
func TestAutoscaleMaximumCountsAgainstTheInstanceCeiling(t *testing.T) {
	s, _, st := newTenantServer(t)
	s.autoscaleAvailable = func() bool { return true }
	// What the workspace runs besides shop's one instance.
	others := workspaceUsageOf(t, s, "acme.shpyrd.test").Instances - 1
	ceiling := others + 4
	setLimits(t, st, "acme", &store.Limits{Instances: ceiling})
	rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/processes", `{"processes":{"web":{"autoscale":{"min":1,"max":5}}}}`)
	if want := fmt.Sprintf("This would bring the workspace to %d of %d instances.", others+5, ceiling); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), want) {
		t.Errorf("a maximum above the ceiling = %d %s, want %q", rec.Code, rec.Body.String(), want)
	}
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/processes", `{"processes":{"web":{"autoscale":{"min":1,"max":4}}}}`); rec.Code != http.StatusOK {
		t.Errorf("a maximum within the ceiling = %d %s", rec.Code, rec.Body.String())
	}
	if u := workspaceUsageOf(t, s, "acme.shpyrd.test"); u.Instances != ceiling {
		t.Errorf("usage instances = %d, want %d (the maximum counts)", u.Instances, ceiling)
	}
}
