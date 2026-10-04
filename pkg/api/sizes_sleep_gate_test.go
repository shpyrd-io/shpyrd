package api

import (
	"net/http"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// Without auto sleep (the enterprise's, with a license) a sleep policy is
// refused with 402; turning sleep off still works.
func TestASleepPolicyNeedsAutoSleep(t *testing.T) {
	shop := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}, Spec: shpyrdv1.AppSpec{Processes: map[string]shpyrdv1.Process{"web": {}}}}
	s, _ := newTestServer(t, nil, []client.Object{shop})
	s.sleepGate = func() bool { return false }
	rec := do(t, s, "POST", "/api/projects/shop/processes", `{"processes":{"web":{"sleep":{"after":"15m"}}}}`, true)
	if rec.Code != http.StatusPaymentRequired || !strings.Contains(rec.Body.String(), "available with a license") {
		t.Errorf("sleep on without auto sleep = %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, "POST", "/api/projects/shop/processes", `{"processes":{"web":{"sleep":{"after":"off"}}}}`, true); rec.Code == http.StatusPaymentRequired {
		t.Errorf("sleep off without auto sleep = %d %s", rec.Code, rec.Body)
	}
}
