package api

import (
	"context"

	"encoding/base64"
	"encoding/json"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"net/http"
	"strings"
	"testing"
)

// A second logo replaces the first: the view carries a new address and
// that address answers the new image.
func TestWorkspaceLogoChanges(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	set := func(payload string) WorkspaceView {
		t.Helper()
		png := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\n"+payload))
		rec := adminJSON(t, s, "PATCH", "/api/workspace", `{"logo":"`+png+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("set %s: %d %s", payload, rec.Code, rec.Body.String())
		}
		var view WorkspaceView
		_ = json.Unmarshal(rec.Body.Bytes(), &view)
		return view
	}
	first := set("first")
	second := set("second")
	if first.Branding.LogoURL == second.Branding.LogoURL {
		t.Fatalf("same address for two logos: %s", first.Branding.LogoURL)
	}
	rec := do(t, s, "GET", second.Branding.LogoURL, "", false)
	if rec.Code != http.StatusOK || !strings.HasSuffix(rec.Body.String(), "second") {
		t.Errorf("second logo: %d %q", rec.Code, rec.Body.String())
	}
	rec = do(t, s, "GET", "/api/config", "", false)
	if !strings.Contains(rec.Body.String(), second.Branding.LogoURL) {
		t.Errorf("config still names the old logo: %s", rec.Body.String())
	}
	rec = adminJSON(t, s, "GET", "/api/workspace", "")
	if !strings.Contains(rec.Body.String(), second.Branding.LogoURL) {
		t.Errorf("workspace still names the old logo: %s", rec.Body.String())
	}
}

// On a cluster the server runs as two replicas, and the one that did not
// take a branding change still remembers the workspace as it was for a few
// seconds (the host resolver's memory). A logo's address names its
// version, and the browser keeps what it gets at an address for a day: so
// the image served at an address must be the version the address names,
// never what a stale memory holds, or the new address shows the old logo
// for a day and the logo cannot be changed.
func TestWorkspaceLogoIsTheVersionItsAddressNames(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	image := func(payload string) string { return "\x89PNG\r\n\x1a\n" + payload }
	rec := adminJSON(t, s, "PATCH", "/api/workspace", `{"logo":"data:image/png;base64,`+base64.StdEncoding.EncodeToString([]byte(image("first")))+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("first: %d %s", rec.Code, rec.Body.String())
	}
	var view WorkspaceView
	_ = json.Unmarshal(rec.Body.Bytes(), &view)
	firstURL := view.Branding.LogoURL
	// Served once: the resolver now remembers the workspace with this logo.
	if rec := do(t, s, "GET", firstURL, "", false); rec.Code != http.StatusOK || !strings.HasSuffix(rec.Body.String(), "first") {
		t.Fatalf("first logo: %d %q", rec.Code, rec.Body.String())
	}
	// The second logo lands behind this server's back: the other replica
	// took the change and told the browser its new address.
	ctx := context.Background()
	w, err := s.store.Workspace(ctx, store.DefaultWorkspaceSlug(ctx, s.store))
	if err != nil {
		t.Fatal(err)
	}
	settings := w.Settings
	settings.Branding = &store.Branding{Logo: base64.StdEncoding.EncodeToString([]byte(image("second"))), LogoType: "image/png"}
	if w, err = s.store.UpdateWorkspaceSettings(ctx, w.Slug, settings); err != nil {
		t.Fatal(err)
	}
	secondURL := brandingView(w).LogoURL
	if secondURL == firstURL {
		t.Fatalf("the address did not change: %s", secondURL)
	}
	rec = do(t, s, "GET", secondURL, "", false)
	if rec.Code != http.StatusOK || !strings.HasSuffix(rec.Body.String(), "second") {
		t.Errorf("the new address serves %d %q, not the second logo", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age") {
		t.Errorf("the new address is not kept: Cache-Control %q", cc)
	}
	// Having seen it, this server tells the truth about the workspace too.
	if rec := do(t, s, "GET", "/api/config", "", false); !strings.Contains(rec.Body.String(), secondURL) {
		t.Errorf("config still names the old logo: %s", rec.Body.String())
	}
	// The old address shows what is there now, but is not to be kept.
	rec = do(t, s, "GET", firstURL, "", false)
	if rec.Code != http.StatusOK || !strings.HasSuffix(rec.Body.String(), "second") {
		t.Errorf("the old address serves %d %q", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("the old address is kept: Cache-Control %q", cc)
	}
}
