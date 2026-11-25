package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// lockedApp is a project behind sign-in, with an id (so grants are keyed by
// it), a git source, a release and a failing process: all that a card must
// not carry.
func lockedApp(name string) *shpyrdv1.App {
	app := sampleApp(name, shpyrdv1.PhaseRunning, shpyrdv1.Release{Number: 7})
	app.Spec.ID = uuid.NewString()
	app.Spec.Access = shpyrdv1.AccessAuthenticated
	app.Status.Message = "secret rollout detail"
	app.Status.Processes = map[string]shpyrdv1.ProcessStatus{"web": {Desired: 2, Ready: 1, Failing: 1, Reason: "CrashLoopBackOff (exit 128)", Size: "large"}}
	return app
}

func listedSlugs(t *testing.T, body string) []string {
	t.Helper()
	var list []struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("list %q: %v", body, err)
	}
	out := []string{}
	for _, p := range list {
		out = append(out, p.Slug)
	}
	return out
}

// A person whose roles only open apps sees their cards on the launcher, and
// only what a card shows; nothing else of the project opens to them.
func TestLauncherCardsOfWhoOnlyOpens(t *testing.T) {
	shop, blog, vault := lockedApp("shop"), lockedApp("blog"), lockedApp("vault")
	elsewhere := lockedApp("elsewhere")
	elsewhere.Labels = map[string]string{shpyrdv1.LabelWorkspace: "other"}
	pub := sampleApp("open-day", shpyrdv1.PhaseRunning)
	shop.Spec.Domains = []string{"shop.acme.test"}
	shop.Status.Domains = []shpyrdv1.DomainStatus{{Host: "shop.acme.test", DNS: "ok", Certificate: "ready"}}
	shop.Spec.Maintenance = &shpyrdv1.Maintenance{Enabled: true, Message: "back at noon", By: "ops@acme.test"}
	shop.Status.Processes["web"] = shpyrdv1.ProcessStatus{Desired: 0, Sleep: &shpyrdv1.SleepStatus{State: "sleeping"}, Reason: "hidden reason"}
	s, cr := newTestServer(t, nil, []client.Object{shop, blog, vault, elsewhere, pub})
	s.authz.TTL = 1
	ctx := t.Context()

	user := ext.Identity{Subject: "u-user", Email: "user@example.test", Provider: "local"}
	reader := ext.Identity{Subject: "u-reader", Email: "reader@example.test", Provider: "local"}
	stranger := ext.Identity{Subject: "u-stranger", Email: "stranger@example.test", Provider: "local"}
	userSID, userCSRF := signIn(t, s, user)
	readerSID, _ := signIn(t, s, reader)
	strangerSID, _ := signIn(t, s, stranger)

	// Roles are enforced once a team exists.
	for _, team := range []store.Team{{Name: "ops", Members: []string{"ops@example.test"}}, {Name: "readers", Members: []string{reader.Email}}, {Name: "staff", Members: []string{"staff@example.test"}}} {
		if _, _, err := s.store.PutTeam(ctx, store.DefaultWorkspace, team); err != nil {
			t.Fatal(err)
		}
	}
	// user: user on shop directly; reader: reader on blog through a team and
	// a member of the workspace. A team the person is not in holds a grant
	// on vault, which the stranger and the others must not learn of.
	if rec := do(t, s, "POST", "/api/projects/shop/members", `{"role":"user","user":"`+user.Email+`"}`, true); rec.Code != http.StatusCreated {
		t.Fatalf("grant user on shop = %d %s", rec.Code, rec.Body.String())
	}
	for _, g := range []store.Grant{
		{Project: ids.Short(blog.Spec.ID), Role: shpyrdv1.RoleReader, Team: "readers"},
		{Project: ids.Short(vault.Spec.ID), Role: shpyrdv1.RoleDeveloper, Team: "staff"},
	} {
		if _, err := s.store.AddGrant(ctx, store.DefaultWorkspace, g); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.store.TouchIdentity(ctx, store.DefaultWorkspace, store.Identity{Email: reader.Email, Provider: "local"}); err != nil {
		t.Fatal(err)
	}
	if rec := do(t, s, "PATCH", "/api/workspace/people/"+reader.Email, `{"role":"member"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("make reader a member = %d %s", rec.Code, rec.Body.String())
	}
	s.authz.Invalidate()
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/></svg>`
	if rec := do(t, s, "PUT", "/api/projects/shop/icon", `{"icon":"`+dataURL("image/svg+xml", svg)+`"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("put icon = %d %s", rec.Code, rec.Body.String())
	}

	// The maintenance mode of shop is on, as the API marks it.
	live := &shpyrdv1.App{}
	if err := cr.Get(ctx, keyOf(t, s, "shop"), live); err != nil {
		t.Fatal(err)
	}
	live.Annotations[shpyrdv1.AnnotationMaintenance] = "on"
	if err := cr.Update(ctx, live); err != nil {
		t.Fatal(err)
	}

	// C1: one card each, the person's own and no other (C2: not vault, not
	// the other workspace's, not the public app they have no role on).
	rec := doCookie(t, s, "GET", "/api/projects", "", userSID, "")
	if got := listedSlugs(t, rec.Body.String()); rec.Code != http.StatusOK || !slices.Equal(got, []string{"shop"}) {
		t.Fatalf("user list = %d %v", rec.Code, got)
	}
	rec = doCookie(t, s, "GET", "/api/projects", "", readerSID, "")
	if got := listedSlugs(t, rec.Body.String()); rec.Code != http.StatusOK || !slices.Equal(got, []string{"blog"}) {
		t.Fatalf("reader list = %d %v", rec.Code, got)
	}
	rec = doCookie(t, s, "GET", "/api/projects", "", strangerSID, "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("stranger list = %d %s", rec.Code, rec.Body.String())
	}

	// C1 and C5: what a card shows, and nothing of how the project runs.
	rec = doCookie(t, s, "GET", "/api/projects", "", userSID, "")
	var cards []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cards); err != nil || len(cards) != 1 {
		t.Fatalf("cards = %s", rec.Body.String())
	}
	card := cards[0]
	for _, k := range []string{"source", "digest", "release", "namespace", "message", "catalogEntry", "catalog", "processes", "createdAt", "teams", "internalHost"} {
		if _, ok := card[k]; ok {
			t.Errorf("card has %q: %s", k, rec.Body.String())
		}
	}
	for _, secret := range []string{"https://example.test/shop", "CrashLoopBackOff", "secret rollout detail", "back at noon", "ops@acme.test", "hidden reason", "sha256", "app-shop"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("card carries %q: %s", secret, rec.Body.String())
		}
	}
	if card["slug"] != "shop" || card["displayName"] != "shop" || card["access"] != "authenticated" || card["phase"] != "Running" || card["role"] != "user" ||
		card["url"] != "https://shop.example.test:8443" || card["domain"] != "shop.acme.test" || card["asleep"] != true {
		t.Errorf("card = %v", card)
	}
	if m, _ := card["maintenance"].(map[string]any); m["enabled"] != true || len(m) != 1 {
		t.Errorf("maintenance = %v: on, and nothing more", card["maintenance"])
	}
	iconURL, _ := card["iconUrl"].(string)
	if !strings.HasPrefix(iconURL, "/api/projects/shop/icon?v=") || card["iconType"] != "image/svg+xml" {
		t.Errorf("icon = %v %v", card["iconUrl"], card["iconType"])
	}

	// C3: the image loads for who has the card, and for nobody else; it is
	// not theirs to change.
	if rec := doCookie(t, s, "GET", iconURL, "", userSID, ""); rec.Code != http.StatusOK || rec.Body.String() != svg {
		t.Errorf("user icon = %d %q", rec.Code, rec.Body.String())
	}
	for name, sid := range map[string]string{"reader": readerSID, "stranger": strangerSID} {
		if rec := doCookie(t, s, "GET", iconURL, "", sid, ""); rec.Code != http.StatusForbidden {
			t.Errorf("%s icon = %d, want 403", name, rec.Code)
		}
	}
	if rec := doCookie(t, s, "PUT", "/api/projects/shop/icon", `{"icon":"`+dataURL("image/svg+xml", svg)+`"}`, userSID, userCSRF); rec.Code != http.StatusForbidden {
		t.Errorf("user put icon = %d", rec.Code)
	}
	if rec := doCookie(t, s, "DELETE", "/api/projects/shop/icon", "", userSID, userCSRF); rec.Code != http.StatusForbidden {
		t.Errorf("user delete icon = %d", rec.Code)
	}
	// The icon is served during maintenance too: the card still shows it.
	if rec := doCookie(t, s, "GET", iconURL, "", userSID, ""); rec.Code != http.StatusOK {
		t.Errorf("icon in maintenance = %d", rec.Code)
	}

	// C6: the card opens nothing else of the project.
	for _, path := range []string{"", "/logs", "/builds", "/secrets", "/metrics", "/audit", "/domains", "/resources", "/members", "/usage", "/allow", "/volumes", "/drains"} {
		if rec := doCookie(t, s, "GET", "/api/projects/shop"+path, "", userSID, ""); rec.Code != http.StatusForbidden {
			t.Errorf("user GET %s = %d, want 403", path, rec.Code)
		}
	}
	for _, m := range [][3]string{
		{"PATCH", "", `{"description":"x"}`}, {"DELETE", "", ""}, {"POST", "/scale", `{"process":"web","replicas":2}`},
		{"POST", "/deploy", `{}`}, {"PUT", "/secrets", `{"set":{"A":"b"}}`}, {"POST", "/members", `{"role":"admin","user":"user@example.test"}`},
	} {
		if rec := doCookie(t, s, m[0], "/api/projects/shop"+m[1], m[2], userSID, userCSRF); rec.Code != http.StatusForbidden {
			t.Errorf("user %s %s = %d, want 403", m[0], m[1], rec.Code)
		}
	}
	if rec := doCookie(t, s, "GET", "/api/projects/blog", "", userSID, ""); rec.Code != http.StatusForbidden {
		t.Errorf("user GET blog = %d, want 403", rec.Code)
	}

	// C7: who sees the project keeps the whole summary, teams included.
	rec = do(t, s, "GET", "/api/projects", "", true)
	var full []AppSummary
	_ = json.Unmarshal(rec.Body.Bytes(), &full)
	if len(full) != 4 || full[0].Slug != "blog" || full[0].Namespace == "" || full[0].Release != 7 || full[0].Teams[0] != "readers" {
		t.Errorf("admin list = %s", rec.Body.String())
	}
}
