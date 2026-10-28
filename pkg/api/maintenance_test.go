package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

func TestMaintenanceHTTPDoesNotAcknowledgeOrRedirect(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		t.Run(method, func(t *testing.T) {
			r := do(t, s, method, "/_shpyrd/maintenance", "", false)
			if r.Code != http.StatusServiceUnavailable {
				t.Fatalf("got %d: %s", r.Code, r.Body.String())
			}
			if r.Header().Get("Retry-After") != "60" || r.Header().Get("Cache-Control") != "no-store" || r.Header().Get("X-Shpyrd-Maintenance") != "true" {
				t.Fatal(r.Header())
			}
			if r.Header().Get("Location") != "" || r.Header().Get("Set-Cookie") != "" {
				t.Fatal("maintenance must not start a login flow")
			}
			if method == "HEAD" && r.Body.Len() != 0 {
				t.Fatal("HEAD has a body")
			}
		})
	}
}

// At the host of a project in maintenance mode (RFC-0020) a browser gets
// the page, with the project's name and its message, escaped; a program
// gets the JSON and the headers the operation's check looks for.
func TestMaintenancePageNamesTheProjectAndSaysItsMessage(t *testing.T) {
	since := metav1.Now()
	shop := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop", Annotations: map[string]string{shpyrdv1.AnnotationDisplayName: "The Shop"}}, Spec: shpyrdv1.AppSpec{Maintenance: &shpyrdv1.Maintenance{Enabled: true, Message: "Back at 15:00 <b>UTC</b>.", Since: &since, By: "ana@acme.test"}}}
	s, _ := newTestServer(t, nil, []client.Object{shop})
	at := func(accept string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/_shpyrd/maintenance", nil)
		req.Host = "shop.example.test"
		req.Header.Set("Accept", accept)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	rec := at("text/html,application/xhtml+xml,*/*;q=0.8")
	body := rec.Body.String()
	if rec.Code != http.StatusServiceUnavailable || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("browser: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(body, "The Shop is under maintenance") || !strings.Contains(body, "Back at 15:00 &lt;b&gt;UTC&lt;/b&gt;.") || strings.Contains(body, "<b>UTC</b>") {
		t.Fatalf("the page names the project and escapes the message:\n%s", body)
	}
	if rec.Header().Get("Retry-After") != "300" || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Shpyrd-Maintenance") != "true" {
		t.Fatal(rec.Header())
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "script-src 'sha256-") {
		t.Fatalf("the moving page names its script: %s", rec.Header().Get("Content-Security-Policy"))
	}
	rec = at("application/json")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"error"`) || strings.Contains(rec.Body.String(), "<html") {
		t.Fatalf("program: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") != "300" || rec.Header().Get("X-Shpyrd-Maintenance") != "true" {
		t.Fatal(rec.Header())
	}

	// Under an operation (RFC-0081) the page has the project's name and
	// its own words, and the shorter Retry-After the operation expects.
	shop.Spec.Maintenance = nil
	shop.Annotations[shpyrdv1.AnnotationMaintenance] = "paused"
	s, _ = newTestServer(t, nil, []client.Object{shop})
	rec = at("text/html")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "The Shop is under maintenance") || !strings.Contains(rec.Body.String(), maintenanceText) || rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("operation: %d %s\n%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
}

// PUT /api/projects/:slug/maintenance turns the mode on, recording when and
// by whom, changes the message without touching them, and off clears it
// all; each is audited. A project under an operation refuses.
func TestMaintenanceModeIsSetThroughTheAPIAndAudited(t *testing.T) {
	shop := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}}
	s, c := newTestServer(t, nil, []client.Object{shop})
	put := func(body string) (*httptest.ResponseRecorder, AppSummary) {
		rec := do(t, s, "PUT", "/api/projects/shop/maintenance", body, true)
		var sum AppSummary
		_ = json.Unmarshal(rec.Body.Bytes(), &sum)
		return rec, sum
	}
	rec, sum := put(`{"enabled":true,"message":"  Back at 15:00  "}`)
	if rec.Code != http.StatusOK || sum.Maintenance == nil || !sum.Maintenance.Enabled || sum.Maintenance.Message != "Back at 15:00" || sum.Maintenance.Since == nil || sum.Maintenance.By != "admin token" {
		t.Fatalf("on: %d %s", rec.Code, rec.Body.String())
	}
	since := *sum.Maintenance.Since
	rec, sum = put(`{"enabled":true,"message":"Back at 16:00"}`)
	if rec.Code != http.StatusOK || sum.Maintenance == nil || sum.Maintenance.Message != "Back at 16:00" || sum.Maintenance.Since == nil || !sum.Maintenance.Since.Equal(&since) || sum.Maintenance.By != "admin token" {
		t.Fatalf("a new message keeps since and by: %d %s", rec.Code, rec.Body.String())
	}
	if rec, _ := put(`{"enabled":true,"message":"` + strings.Repeat("x", 501) + `"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("a long message: %d %s", rec.Code, rec.Body.String())
	}
	// The detail carries it as the spec has it, the list only while on.
	rec = do(t, s, "GET", "/api/projects/shop", "", true)
	var detail AppDetail
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	if detail.Spec.Maintenance == nil || !detail.Spec.Maintenance.Enabled || detail.Spec.Maintenance.Message != "Back at 16:00" {
		t.Fatalf("detail: %s", rec.Body.String())
	}
	rec, sum = put(`{"enabled":false}`)
	if rec.Code != http.StatusOK || sum.Maintenance != nil {
		t.Fatalf("off: %d %s", rec.Code, rec.Body.String())
	}
	cur := &shpyrdv1.App{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(shop), cur); err != nil || cur.Spec.Maintenance != nil {
		t.Fatalf("off clears the spec: %+v %v", cur.Spec.Maintenance, err)
	}
	evs, _ := s.kube.Kube.CoreV1().Events("").List(context.Background(), metav1.ListOptions{})
	var details []string
	for _, ev := range evs.Items {
		if ev.Annotations["shpyrd.io/action"] == "maintenance" {
			details = append(details, ev.Annotations["shpyrd.io/detail"])
		}
	}
	if strings.Join(details, "|") != "on: Back at 15:00|on: Back at 16:00|off" {
		t.Fatalf("audit: %v", details)
	}

	// Under an operation every write is refused, this one too.
	cur.Annotations = map[string]string{shpyrdv1.AnnotationMaintenance: "paused"}
	if err := c.Update(context.Background(), cur); err != nil {
		t.Fatal(err)
	}
	if rec, _ := put(`{"enabled":true}`); rec.Code != http.StatusLocked {
		t.Fatalf("under an operation: %d %s", rec.Code, rec.Body.String())
	}
}
