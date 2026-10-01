package authlocal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestConnectorStore(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	gvk := schema.GroupVersionKind{Group: "dex.coreos.com", Version: "v1", Kind: "Connector"}
	scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind("ConnectorList"), &unstructured.UnstructuredList{})
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{ConnectorGVR: "ConnectorList"})
	s := &ConnectorStore{Dynamic: dyn, Namespace: "shpyrd-system", Issuer: "https://auth.example.test/"}

	existed, err := s.Add(ctx, ConnectorSpec{Type: "github", ClientID: "gh1", ClientSecret: "ghs", Org: "acme"})
	if err != nil || existed {
		t.Fatalf("add: existed=%v err=%v", existed, err)
	}
	// Dex reads type, name, id and a base64 JSON config with its callback.
	u, err := dyn.Resource(ConnectorGVR).Namespace("shpyrd-system").Get(ctx, "github", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	typ, _, _ := unstructured.NestedString(u.Object, "type")
	name, _, _ := unstructured.NestedString(u.Object, "name")
	enc, _, _ := unstructured.NestedString(u.Object, "config")
	raw, _ := base64.StdEncoding.DecodeString(enc)
	var cfg githubConfig
	_ = json.Unmarshal(raw, &cfg)
	if typ != "github" || name != "GitHub" || cfg.ClientSecret != "ghs" || cfg.RedirectURI != "https://auth.example.test/callback" || len(cfg.Orgs) != 1 || cfg.Orgs[0].Name != "acme" || cfg.TeamNameField != "slug" || cfg.LoadAllGroups {
		t.Errorf("stored connector: type=%s name=%s cfg=%+v", typ, name, cfg)
	}

	existed, err = s.Add(ctx, ConnectorSpec{Type: "google", ClientID: "g1", ClientSecret: "gs", HostedDomain: "acme.com", Name: "Acme Google"})
	if err != nil || existed {
		t.Fatalf("add google: %v %v", existed, err)
	}
	if existed, err = s.Add(ctx, ConnectorSpec{Type: "github", ClientID: "gh2", ClientSecret: "ghs2"}); err != nil || !existed {
		t.Fatalf("replace: existed=%v err=%v", existed, err)
	}
	list, err := s.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if list[0].ID != "github" || list[0].Detail != "" || list[1].ID != "google" || list[1].Name != "Acme Google" || list[1].Detail != "domain acme.com" {
		t.Errorf("list = %+v", list)
	}
	// A workspace's own connector (RFC-0033 per-workspace SSO): prefixed
	// Dex id, labelled, listed in its scope only, removed from its scope
	// only.
	// A workspace's connector is keyed by the workspace's short id (RFC-0080).
	if existed, err := s.Add(ctx, ConnectorSpec{Type: "google", Realm: ext.RealmWorkspace, Workspace: "1p1c19fh1amxymmq1yqv87q0j", ClientID: "g2", ClientSecret: "gs2", HostedDomain: "acme.com"}); err != nil || existed {
		t.Fatalf("add workspace connector: %v %v", existed, err)
	}
	u, err = dyn.Resource(ConnectorGVR).Namespace("shpyrd-system").Get(ctx, "ws-1p1c19fh1amxymmq1yqv87q0j-google", metav1.GetOptions{})
	if err != nil || u.GetLabels()[LabelWorkspaceID] != "1p1c19fh1amxymmq1yqv87q0j" || u.GetLabels()[LabelRealm] != ext.RealmWorkspace {
		t.Fatalf("workspace connector object: %v labels=%v", err, u.GetLabels())
	}
	// The console's own methods are a scope apart (RFC-0080).
	if _, err := s.Add(ctx, ConnectorSpec{Type: "google", Realm: ext.RealmConsole, ClientID: "g3", ClientSecret: "gs3", HostedDomain: "shpyrd.io"}); err != nil {
		t.Fatalf("add console connector: %v", err)
	}
	if all, _ := s.List(ctx); len(all) != 4 {
		t.Errorf("list all = %+v", all)
	}
	acme, _ := s.ListFor(ctx, ext.RealmWorkspace, "1p1c19fh1amxymmq1yqv87q0j")
	if len(acme) != 1 || acme[0].ID != "google" || acme[0].FullID != "ws-1p1c19fh1amxymmq1yqv87q0j-google" || acme[0].Workspace != "1p1c19fh1amxymmq1yqv87q0j" || acme[0].Detail != "domain acme.com" {
		t.Errorf("acme's = %+v", acme)
	}
	if platform, _ := s.ListFor(ctx, ext.RealmPlatform, ""); len(platform) != 2 || platform[0].Workspace != "" || platform[0].Realm != ext.RealmPlatform {
		t.Errorf("platform's = %+v", platform)
	}
	if console, _ := s.ListFor(ctx, ext.RealmConsole, ""); len(console) != 1 || console[0].FullID != "console-google" || console[0].ID != "google" || console[0].Detail != "domain shpyrd.io" {
		t.Errorf("console's = %+v", console)
	}
	if err := s.Remove(ctx, ext.RealmPlatform, "", "ws-1p1c19fh1amxymmq1yqv87q0j-google"); err == nil {
		t.Error("the platform scope must not remove a workspace's connector by its full id")
	}
	if err := s.Remove(ctx, ext.RealmWorkspace, "1p1c19fh1amxymmq1yqv87q0j", "google"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, ext.RealmConsole, "", "google"); err != nil {
		t.Fatal(err)
	}
	if err := (&ConnectorSpec{Type: "github", ID: "ws-x", ClientID: "a", ClientSecret: "b"}).Validate(); err == nil {
		t.Error("a platform id with the workspace prefix was accepted")
	}
	if err := s.Remove(ctx, ext.RealmPlatform, "", "google"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, ext.RealmPlatform, "", "google"); err == nil {
		t.Error("removing twice must fail")
	}
	if err := (&ConnectorSpec{Type: "okta", ClientID: "a", ClientSecret: "b"}).Validate(); err == nil {
		t.Error("unknown type accepted")
	}
	if err := (&ConnectorSpec{Type: "github", ID: "local", ClientID: "a", ClientSecret: "b"}).Validate(); err == nil {
		t.Error("id local accepted")
	}
}

// Without auth-local there is no Connector resource: the API server answers
// 404 "the server could not find the requested resource" (checked against a
// kind cluster), and the store says the extension is needed rather than
// leaking that error (RFC-0058).
func TestConnectorStoreWithoutAuthLocal(t *testing.T) {
	ctx := context.Background()
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{ConnectorGVR: "ConnectorList"})
	serverErr := apierrors.NewGenericServerResponse(404, "post", schema.GroupResource{Group: "dex.coreos.com", Resource: "connectors"}, "", "", 0, false)
	dyn.PrependReactor("*", "connectors", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, serverErr
	})
	s := &ConnectorStore{Dynamic: dyn, Namespace: "shpyrd-system", Issuer: "https://auth.example.test"}

	_, err := s.Add(ctx, ConnectorSpec{Type: "github", ClientID: "gh", ClientSecret: "s"})
	if !errors.Is(err, ErrNotEnabled) || !strings.Contains(err.Error(), "shpyrd extensions enable auth-local") || strings.Contains(err.Error(), "could not find") {
		t.Errorf("add without auth-local: %v", err)
	}
	if _, err := s.List(ctx); !errors.Is(err, ErrConnectorsNotEnabled) {
		t.Errorf("list without auth-local: %v", err)
	}
	if err := s.Remove(ctx, "", "", "github"); !errors.Is(err, ErrConnectorsNotEnabled) {
		t.Errorf("remove without auth-local: %v", err)
	}

	// The CLI reads the install record first and stops there.
	if extensionEnabled(&install.InstallInfo{Vars: map[string]string{install.VarExtensions: "postgres,mail"}}) {
		t.Error("auth-local reported enabled from an install record without it")
	}
	if !extensionEnabled(&install.InstallInfo{Vars: map[string]string{install.VarExtensions: "postgres, auth-local"}}) {
		t.Error("auth-local not reported enabled")
	}
}
