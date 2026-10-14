package api

import (
	"encoding/json"
	"net/http"
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestProjectDomainRedirectValidationAndRemoval(t *testing.T) {
	app := sampleApp("shop", shpyrdv1.PhaseRunning)
	app.Spec.Domains = []string{"shop.example.com", "waiting.example.com"}
	app.Status.Domains = []shpyrdv1.DomainStatus{{Host: "shop.example.com", DNS: "ok", Certificate: "ready"}, {Host: "waiting.example.com", DNS: "ok", Certificate: "issuing"}, {Host: "removed.example.com", DNS: "ok", Certificate: "ready"}}
	s, cr := newTestServer(t, nil, []client.Object{app})
	for _, host := range []string{"waiting.example.com", "removed.example.com", "evil.example.com", "shop.example.test"} {
		rec := do(t, s, "PATCH", "/api/projects/shop/domains", `{"redirectTo":"`+host+`"}`, true)
		if rec.Code == http.StatusOK {
			t.Fatalf("accepted %s", host)
		}
	}
	rec := do(t, s, "PATCH", "/api/projects/shop/domains", `{"redirectTo":"shop.example.com"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var answer DomainsResult
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.RedirectTo != "shop.example.com" || answer.Target != "shop.example.test" {
		t.Fatalf("%+v", answer)
	}
	rec = do(t, s, "GET", "/api/projects/shop", "", true)
	var detail AppDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Spec.RedirectTo != "shop.example.com" {
		t.Fatal("project info lost redirect")
	}
	rec = do(t, s, "PATCH", "/api/projects/shop/domains", `{"redirectTo":""}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", rec.Code, rec.Body.String())
	}
	do(t, s, "PATCH", "/api/projects/shop/domains", `{"redirectTo":"shop.example.com"}`, true)
	rec = do(t, s, "DELETE", "/api/projects/shop/domains/shop.example.com", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	got := &shpyrdv1.App{}
	if err := cr.Get(t.Context(), client.ObjectKeyFromObject(app), got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.RedirectTo != "" {
		t.Fatal("removed destination still selected")
	}
}

func TestWorkspaceAppRedirectRequiresVerifiedPrimary(t *testing.T) {
	s, _, st := newTenantServer(t)
	ctx := t.Context()
	_, err := st.PutWorkspaceHost(ctx, "acme", store.WorkspaceHost{Host: "acme.example.com", Kind: store.HostCustom})
	if err != nil {
		t.Fatal(err)
	}
	patch := func(body string) int {
		return at(t, s, "acme.shpyrd.test", "PATCH", "/api/workspace/domains/acme.example.com", body).Code
	}
	if got := patch(`{"redirectApps":true}`); got != http.StatusBadRequest {
		t.Fatalf("unverified: %d", got)
	}
	now := metav1.Now().Time
	_, err = st.PutWorkspaceHost(ctx, "acme", store.WorkspaceHost{Host: "acme.example.com", Kind: store.HostCustom, VerifiedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	if got := patch(`{"redirectApps":true}`); got != http.StatusBadRequest {
		t.Fatalf("not primary: %d", got)
	}
	if got := patch(`{"primary":true,"redirectApps":true}`); got != http.StatusOK {
		t.Fatalf("enable: %d", got)
	}
	ws, _ := st.Workspace(ctx, "acme")
	if !ws.Settings.RedirectApps {
		t.Fatal("redirect preference not saved")
	}
	if got := patch(`{"redirectApps":false}`); got != http.StatusOK {
		t.Fatalf("disable: %d", got)
	}
	ws, _ = st.Workspace(ctx, "acme")
	if ws.Settings.RedirectApps {
		t.Fatal("redirect preference not disabled")
	}
}
