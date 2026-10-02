package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

func dataURL(typ, body string) string {
	return "data:" + typ + ";base64," + base64.StdEncoding.EncodeToString([]byte(body))
}

// The card of a project on the launcher: its symbol and colour, or the
// image it sent, the domain of its own that answers, and its teams.
func TestProjectCard(t *testing.T) {
	app := sampleApp("shop", shpyrdv1.PhaseRunning)
	app.Spec.ID = uuid.NewString()
	app.Spec.Domains = []string{"new.acme.test", "shop.acme.test"}
	app.Status.Domains = []shpyrdv1.DomainStatus{
		{Host: "new.acme.test", DNS: "missing", Certificate: "issuing"},
		{Host: "shop.acme.test", DNS: "ok", Certificate: "ready"},
	}
	s, k := newTestServer(t, nil, []client.Object{app, sampleApp("plain", shpyrdv1.PhaseRunning)})
	ctx := context.Background()
	for _, team := range []string{"sales", "finance"} {
		if _, _, err := s.store.PutTeam(ctx, store.DefaultWorkspace, store.Team{Name: team}); err != nil {
			t.Fatal(err)
		}
	}
	// A team with two roles on the project is named once.
	for _, g := range [][2]string{{"sales", shpyrdv1.RoleUser}, {"finance", shpyrdv1.RoleUser}, {"sales", shpyrdv1.RoleDeveloper}} {
		if _, err := s.store.AddGrant(ctx, store.DefaultWorkspace, store.Grant{Project: ids.Short(app.Spec.ID), Role: g[1], Team: g[0]}); err != nil {
			t.Fatal(err)
		}
	}

	// The symbol and its colour, by name.
	rec := do(t, s, "PATCH", "/api/projects/shop", `{"icon":"briefcase","iconColor":"teal"}`, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"icon":"briefcase"`) || !strings.Contains(rec.Body.String(), `"iconColor":"teal"`) {
		t.Fatalf("set icon: %d %s", rec.Code, rec.Body.String())
	}
	for _, bad := range []string{`{"icon":"Brief Case"}`, `{"iconColor":"#ff0000"}`} {
		if rec := do(t, s, "PATCH", "/api/projects/shop", bad, true); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d want 400", bad, rec.Code)
		}
	}

	// The list: the symbol, the domain that answers, the teams once each.
	var list []AppSummary
	_ = json.Unmarshal(do(t, s, "GET", "/api/projects", "", true).Body.Bytes(), &list)
	var shop, plain AppSummary
	for _, p := range list {
		switch p.Slug {
		case "shop":
			shop = p
		case "plain":
			plain = p
		}
	}
	if shop.Icon != "briefcase" || shop.IconColor != "teal" || shop.Domain != "shop.acme.test" {
		t.Errorf("shop = %+v", shop)
	}
	if strings.Join(shop.Teams, ",") != "finance,sales" {
		t.Errorf("teams = %v, want finance,sales", shop.Teams)
	}
	if plain.Domain != "" || len(plain.Teams) != 0 || plain.IconURL != "" {
		t.Errorf("plain = %+v", plain)
	}

	// An image of its own: kept by the store, marked on the App, read at
	// an address that changes with it.
	if rec := do(t, s, "PUT", "/api/projects/shop/icon", `{"icon":"`+dataURL("image/gif", "GIF89a")+`"}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("gif: %d want 400", rec.Code)
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/></svg>`
	rec = do(t, s, "PUT", "/api/projects/shop/icon", `{"icon":"`+dataURL("image/svg+xml", svg)+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("put icon: %d %s", rec.Code, rec.Body.String())
	}
	var d AppDetail
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if !strings.HasPrefix(d.IconURL, "/api/projects/shop/icon?v=") || d.IconType != "image/svg+xml" {
		t.Errorf("detail icon = %q %q", d.IconURL, d.IconType)
	}
	stored := &shpyrdv1.App{}
	_ = k.Get(ctx, keyOf(t, s, "shop"), stored)
	if v := stored.Annotations[shpyrdv1.AnnotationIconFile]; !strings.HasSuffix(v, ".svg") || strings.Contains(v, "<svg") {
		t.Errorf("annotation = %q: a mark, not the image", v)
	}
	rec = do(t, s, "GET", d.IconURL, "", true)
	if rec.Code != http.StatusOK || rec.Body.String() != svg || rec.Header().Get("Content-Type") != "image/svg+xml" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("get icon: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}

	// Removed: the card goes back to its symbol.
	if rec := do(t, s, "DELETE", "/api/projects/shop/icon", "", true); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "iconUrl") {
		t.Errorf("delete icon: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "GET", "/api/projects/shop/icon", "", true); rec.Code != http.StatusNotFound {
		t.Errorf("icon after delete: %d want 404", rec.Code)
	}
	if _, _, err := s.store.ProjectIcon(ctx, app.Spec.ID); err != store.ErrNotFound {
		t.Errorf("store after delete: %v", err)
	}
}
