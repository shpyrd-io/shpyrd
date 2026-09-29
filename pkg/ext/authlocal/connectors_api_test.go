package authlocal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// A workspace cannot remove the sign-in method its login page depends on:
// its last own method while the platform's are switched off, or a method a
// claimed email domain routes to. Both left production's operator workspace
// with a login page nobody could pass (2026-09-29).
func TestRemoveGuardsTheLastDoor(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{Group: "dex.coreos.com", Version: "v1", Kind: "Connector"}, &unstructured.Unstructured{})
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{ConnectorGVR: "ConnectorList"})
	st := store.NewMemory()
	ws, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "acme", Name: "Acme", Address: "acme.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	cs := &ConnectorStore{Dynamic: dyn, Namespace: "shpyrd-system", Issuer: "https://auth.example.test"}
	if _, err := cs.Add(ctx, ConnectorSpec{Type: "google", Realm: ext.RealmWorkspace, Workspace: ids.Short(ws.ID), ClientID: "a", ClientSecret: "b"}); err != nil {
		t.Fatal(err)
	}
	h := &connectorHandlers{deps: ext.Deps{Kube: &kube.Client{Dynamic: dyn}, SystemNamespace: "shpyrd-system", Store: st}, issuer: cs.Issuer, scoped: true}
	remove := func() (int, string) {
		w, _ := st.Workspace(ctx, "acme")
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("DELETE", "/api/workspace/login-methods/google", nil)
		c.Params = gin.Params{{Key: "id", Value: "google"}}
		c.Set(ext.WorkspaceContextKey, w)
		h.remove(c)
		c.Writer.WriteHeaderNow()
		return c.Writer.Status(), rec.Body.String()
	}
	// 1. The platform's methods are switched off: the only own method stays.
	if _, err := st.UpdateWorkspaceSettings(ctx, "acme", store.WorkspaceSettings{OwnMethodsOnly: true}); err != nil {
		t.Fatal(err)
	}
	if code, body := remove(); code != http.StatusBadRequest || !strings.Contains(body, "only sign-in method") {
		t.Errorf("removing the last own method with the platform's off = %d %s", code, body)
	}
	// 2. Platform methods back on, but a claimed domain routes to it.
	if _, err := st.UpdateWorkspaceSettings(ctx, "acme", store.WorkspaceSettings{}); err != nil {
		t.Fatal(err)
	}
	full := FullConnectorID(ext.RealmWorkspace, ids.Short(ws.ID), "google")
	if _, err := st.PutDomainClaim(ctx, "acme", "acme.com", full); err != nil {
		t.Fatal(err)
	}
	if code, body := remove(); code != http.StatusBadRequest || !strings.Contains(body, "acme.com") {
		t.Errorf("removing a method a domain claim routes to = %d %s", code, body)
	}
	// 3. Neither guard applies: gone.
	if _, err := st.PutDomainClaim(ctx, "acme", "acme.com", ""); err != nil {
		t.Fatal(err)
	}
	if code, body := remove(); code != http.StatusNoContent {
		t.Errorf("removing a free method = %d %s", code, body)
	}
	if left, _ := cs.ListFor(ctx, ext.RealmWorkspace, ids.Short(ws.ID)); len(left) != 0 {
		t.Errorf("connector left: %+v", left)
	}
}
