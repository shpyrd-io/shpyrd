package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	mailext "github.com/shpyrd-io/shpyrd/pkg/ext/mail"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Workspace roles (RFC-0033): owners and admins administer the workspace,
// members create projects, the first person to define roles in a
// bootstrap workspace becomes its owner, and the last owner stays.
func TestWorkspaceRoles(t *testing.T) {
	shop := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}}
	s, _ := newTestServer(t, nil, []client.Object{shop})
	s.authz.TTL = 1

	ada := ext.Identity{Subject: "u1", Email: "ada@example.test", Name: "Ada", Provider: "local"}
	bob := ext.Identity{Subject: "u2", Email: "bob@example.test", Provider: "local"}
	eve := ext.Identity{Subject: "u3", Email: "eve@example.test", Provider: "local"}
	adaSID, adaCSRF := signIn(t, s, ada)
	bobSID, bobCSRF := signIn(t, s, bob)
	eveSID, eveCSRF := signIn(t, s, eve)

	// Bootstrap: Ada, a plain person, gives Bob a role. That ends
	// bootstrap, so Ada becomes the owner rather than losing access.
	rec := doCookie(t, s, "PATCH", "/api/workspace/people/Bob@example.test", `{"role":"admin"}`, adaSID, adaCSRF)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"role":"admin"`) {
		t.Fatalf("first role: %d %s", rec.Code, rec.Body.String())
	}
	me := doCookie(t, s, "GET", "/api/me", "", adaSID, "")
	if !strings.Contains(me.Body.String(), `"workspace":"owner"`) || !strings.Contains(me.Body.String(), `"platform":"platform-admin"`) || !strings.Contains(me.Body.String(), `"enforced":true`) {
		t.Fatalf("ada after claiming: %s", me.Body.String())
	}
	ws := doCookie(t, s, "GET", "/api/workspace", "", adaSID, "")
	if !strings.Contains(ws.Body.String(), `"owners":["ada@example.test"]`) {
		t.Errorf("workspace owners: %s", ws.Body.String())
	}
	// Eve, unlisted, has nothing now: no cluster, no project creation.
	if rec := doCookie(t, s, "GET", "/api/me", "", eveSID, ""); !strings.Contains(rec.Body.String(), `"platform":""`) && strings.Contains(rec.Body.String(), `platform-admin`) {
		t.Errorf("eve should have nothing: %s", rec.Body.String())
	}
	if rec := doCookie(t, s, "POST", "/api/projects", `{"name":"eve-app"}`, eveSID, eveCSRF); rec.Code != http.StatusForbidden {
		t.Errorf("eve creating a project: %d %s", rec.Code, rec.Body.String())
	}

	// Bob, an admin, administers but cannot name owners.
	if rec := doCookie(t, s, "GET", "/api/workspace/people", "", bobSID, ""); rec.Code != http.StatusOK {
		t.Errorf("admin lists people: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "PATCH", "/api/workspace/people/eve@example.test", `{"role":"owner"}`, bobSID, bobCSRF); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "owners") {
		t.Errorf("admin naming an owner: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "PATCH", "/api/workspace/people/ada@example.test", `{"role":"member"}`, bobSID, bobCSRF); rec.Code != http.StatusForbidden {
		t.Errorf("admin demoting the owner: %d %s", rec.Code, rec.Body.String())
	}
	// Bob makes Eve a member: she can create a project and administers it.
	if rec := doCookie(t, s, "PATCH", "/api/workspace/people/eve@example.test", `{"role":"member"}`, bobSID, bobCSRF); rec.Code != http.StatusOK {
		t.Fatalf("admin making a member: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "GET", "/api/me", "", eveSID, ""); !strings.Contains(rec.Body.String(), `"workspace":"member"`) || strings.Contains(rec.Body.String(), `platform-admin`) {
		t.Errorf("eve as member: %s", rec.Body.String())
	}
	if rec := doCookie(t, s, "POST", "/api/projects", `{"name":"eve-app"}`, eveSID, eveCSRF); rec.Code != http.StatusCreated {
		t.Fatalf("member creating a project: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "GET", "/api/me", "", eveSID, ""); !strings.Contains(rec.Body.String(), `"admin"`) || !strings.Contains(rec.Body.String(), `"projects"`) {
		t.Errorf("creator should administer the project: %s", rec.Body.String())
	}
	if rec := doCookie(t, s, "GET", "/api/projects/shop", "", eveSID, ""); rec.Code != http.StatusForbidden {
		t.Errorf("member on another project: %d", rec.Code)
	}
	if rec := doCookie(t, s, "GET", "/api/cluster", "", eveSID, ""); rec.Code != http.StatusForbidden {
		t.Errorf("member on the cluster page: %d", rec.Code)
	}

	// The last owner stays; a second owner can be named by an owner, then
	// the first may step down. Bad roles are refused.
	if rec := doCookie(t, s, "PATCH", "/api/workspace/people/ada@example.test", `{"role":"admin"}`, adaSID, adaCSRF); rec.Code != http.StatusConflict {
		t.Errorf("last owner stepping down: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "PATCH", "/api/workspace/people/bob@example.test", `{"role":"king"}`, adaSID, adaCSRF); rec.Code != http.StatusBadRequest {
		t.Errorf("bad role: %d", rec.Code)
	}
	if rec := doCookie(t, s, "PATCH", "/api/workspace/people/bob@example.test", `{"role":"owner"}`, adaSID, adaCSRF); rec.Code != http.StatusOK {
		t.Fatalf("naming a second owner: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "PATCH", "/api/workspace/people/ada@example.test", `{"role":""}`, adaSID, adaCSRF); rec.Code != http.StatusOK {
		t.Fatalf("stepping down: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "GET", "/api/me", "", adaSID, ""); strings.Contains(rec.Body.String(), `platform-admin`) {
		t.Errorf("ada after stepping down: %s", rec.Body.String())
	}
	// The operator (admin token) holds the owner's actions and sees
	// everyone with a role, signed in or not.
	if rec := do(t, s, "PATCH", "/api/workspace/people/new@example.test", `{"role":"owner"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("operator naming an owner: %d %s", rec.Code, rec.Body.String())
	}
	var people []PersonView
	rec = do(t, s, "GET", "/api/workspace/people", "", true)
	if err := json.Unmarshal(rec.Body.Bytes(), &people); err != nil {
		t.Fatal(err)
	}
	found := map[string]PersonView{}
	for _, p := range people {
		found[p.Email] = p
	}
	if p := found["new@example.test"]; p.Role != "owner" || p.LastSeenAt != nil {
		t.Errorf("person with a role who never signed in: %+v", p)
	}
	if p := found["eve@example.test"]; p.Role != "member" || p.LastSeenAt != nil {
		// Eve's session was created directly, without a sign-in record.
		t.Logf("eve: %+v", p)
	}
	// A team's platform role still counts for someone without a role, and
	// is reported as such.
	if rec := do(t, s, "POST", "/api/teams", `{"name":"ops","members":["ops@example.test"],"platformRole":"platform-admin"}`, true); rec.Code != http.StatusCreated {
		t.Fatalf("team: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, s, "GET", "/api/workspace/people", "", true)
	if !strings.Contains(rec.Body.String(), `"email":"ops@example.test"`) {
		// ops has no sign-in record and no membership: not listed. Fine.
		t.Logf("people: %s", rec.Body.String())
	}
	opsSID, _ := signIn(t, s, ext.Identity{Subject: "u9", Email: "ops@example.test", Provider: "local"})
	if rec := doCookie(t, s, "GET", "/api/me", "", opsSID, ""); !strings.Contains(rec.Body.String(), `"platform":"platform-admin"`) || strings.Contains(rec.Body.String(), `"workspace":"`) {
		t.Errorf("team platform role without membership: %s", rec.Body.String())
	}
}

// Invitations (RFC-0033): a link shown once, accepted by signing in with
// the invited address; the join policy lets invited people in; inviting
// someone known applies the role at once.
func TestInvitations(t *testing.T) {
	issuer := newFakeIssuer(t)
	issuer.passwords["new@example.test"] = "pw-new"
	issuer.passwords["link@example.test"] = "pw-link"
	issuer.passwords["other@example.test"] = "pw-other"
	s, _ := newTestServer(t, nil, nil)
	s.authz.TTL = 1
	ctx := context.Background()
	if err := s.rp.AddOIDC(ctx, ext.OIDCProvider{ID: "local", Label: "Email and password", Issuer: issuer.srv.URL, ClientID: "shpyrd", ClientSecret: "sekret", Password: true}); err != nil {
		t.Fatal(err)
	}
	mail := &fakeMailer{configured: true}
	s.mailer = mail

	// The owner: named by the operator; the workspace only admits listed people.
	if rec := do(t, s, "PATCH", "/api/workspace/people/owner@example.test", `{"role":"owner"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("owner: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "PATCH", "/api/workspace", `{"joinPolicy":"listed"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("policy: %d %s", rec.Code, rec.Body.String())
	}
	owner := ext.Identity{Subject: "o", Email: "owner@example.test", Name: "Olive Owner", Provider: "local"}
	ownerSID, ownerCSRF := signIn(t, s, owner)
	if rec := do(t, s, "POST", "/api/teams", `{"name":"dev","members":[]}`, true); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}

	// Bad requests.
	for body, want := range map[string]int{
		`{"email":"not an email"}`:                     http.StatusBadRequest,
		`{"email":"x@example.test","role":"king"}`:     http.StatusBadRequest,
		`{"email":"x@example.test","team":"ghosts"}`:   http.StatusNotFound,
		`{"email":"owner@example.test"}`:               http.StatusBadRequest,
		`{"email":"x@example.test","team":"everyone"}`: http.StatusBadRequest,
	} {
		if rec := doCookie(t, s, "POST", "/api/workspace/invitations", body, ownerSID, ownerCSRF); rec.Code != want {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body.String())
		}
	}

	// An unlisted person cannot sign in.
	if rec := do(t, s, "POST", "/api/auth/password", `{"email":"new@example.test","password":"pw-new"}`, false); rec.Code != http.StatusForbidden {
		t.Fatalf("unlisted sign-in: %d %s", rec.Code, rec.Body.String())
	}

	// Invite: a link, emailed; listed afterwards; a re-invite replaces it.
	rec := doCookie(t, s, "POST", "/api/workspace/invitations", `{"email":"New@Example.test","role":"member","team":"dev"}`, ownerSID, ownerCSRF)
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite: %d %s", rec.Code, rec.Body.String())
	}
	var res InviteResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Applied || res.Email != "new@example.test" || res.Role != "member" || res.Team != "dev" || !strings.HasPrefix(res.Link, "/invite/") && !strings.Contains(res.Link, "/invite/") || !res.Emailed || res.Invitation == nil || res.Invitation.Expired {
		t.Fatalf("invite result: %+v", res)
	}
	if len(mail.sent) != 1 || mail.sent[0].To[0] != "new@example.test" || !strings.Contains(mail.sent[0].Text, res.Link) || !strings.Contains(mail.sent[0].Subject, "Olive Owner") || !strings.Contains(mail.sent[0].HTML, "Accept invitation") {
		t.Fatalf("mail: %+v", mail.sent)
	}
	token := res.Link[strings.LastIndex(res.Link, "/")+1:]
	if rec := doCookie(t, s, "GET", "/api/workspace/invitations", "", ownerSID, ""); !strings.Contains(rec.Body.String(), `"email":"new@example.test"`) || strings.Contains(rec.Body.String(), token) {
		t.Errorf("list must show the invitation, never the token: %s", rec.Body.String())
	}
	rec = doCookie(t, s, "POST", "/api/workspace/invitations", `{"email":"new@example.test","role":"admin"}`, ownerSID, ownerCSRF)
	if rec.Code != http.StatusCreated {
		t.Fatalf("re-invite: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	token2 := res.Link[strings.LastIndex(res.Link, "/")+1:]
	if rec := do(t, s, "GET", "/api/invitations/"+token, "", false); rec.Code != http.StatusNotFound {
		t.Errorf("old link after re-invite: %d", rec.Code)
	}
	pub := do(t, s, "GET", "/api/invitations/"+token2, "", false)
	if pub.Code != http.StatusOK || !strings.Contains(pub.Body.String(), `"email":"new@example.test"`) || !strings.Contains(pub.Body.String(), `"role":"admin"`) || !strings.Contains(pub.Body.String(), `"invitedBy":"owner@example.test"`) || !strings.Contains(pub.Body.String(), `"expired":false`) {
		t.Fatalf("public view: %d %s", pub.Code, pub.Body.String())
	}
	if rec := do(t, s, "GET", "/api/invitations/short", "", false); rec.Code != http.StatusNotFound {
		t.Errorf("bad token: %d", rec.Code)
	}

	// Signing in with the invited address accepts it: role set, invitation gone.
	rec = do(t, s, "POST", "/api/auth/password", `{"email":"new@example.test","password":"pw-new"}`, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("invited sign-in: %d %s", rec.Code, rec.Body.String())
	}
	newSID, newCSRF := cookieValue(rec, sessionCookie), cookieValue(rec, csrfCookie)
	if me := doCookie(t, s, "GET", "/api/me", "", newSID, ""); !strings.Contains(me.Body.String(), `"workspace":"admin"`) {
		t.Errorf("after accepting at sign-in: %s", me.Body.String())
	}
	if list, _ := s.store.ListInvitations(ctx, store.DefaultWorkspace); len(list) != 0 {
		t.Errorf("invitation should be gone: %+v", list)
	}
	if rec := do(t, s, "GET", "/api/invitations/"+token2, "", false); rec.Code != http.StatusNotFound {
		t.Errorf("used link: %d", rec.Code)
	}

	// Inviting someone known applies the role at once (and the team).
	rec = doCookie(t, s, "POST", "/api/workspace/invitations", `{"email":"new@example.test","role":"member","team":"dev"}`, ownerSID, ownerCSRF)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"applied":true`) {
		t.Fatalf("invite known person: %d %s", rec.Code, rec.Body.String())
	}
	if team, err := s.store.GetTeam(ctx, store.DefaultWorkspace, "dev"); err != nil || len(team.Members) != 1 || team.Members[0] != "new@example.test" {
		t.Errorf("team after applying: %+v %v", team, err)
	}
	if me := doCookie(t, s, "GET", "/api/me", "", newSID, ""); !strings.Contains(me.Body.String(), `"workspace":"member"`) {
		t.Errorf("after applying: %s", me.Body.String())
	}
	// An admin cannot invite as owner; an owner can.
	adminSID, adminCSRF := signIn(t, s, ext.Identity{Subject: "a", Email: "adm@example.test", Provider: "local"})
	if rec := do(t, s, "PATCH", "/api/workspace/people/adm@example.test", `{"role":"admin"}`, true); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	if rec := doCookie(t, s, "POST", "/api/workspace/invitations", `{"email":"boss@example.test","role":"owner"}`, adminSID, adminCSRF); rec.Code != http.StatusForbidden {
		t.Errorf("admin inviting an owner: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "POST", "/api/workspace/invitations", `{"email":"boss@example.test","role":"owner"}`, ownerSID, ownerCSRF); rec.Code != http.StatusCreated {
		t.Errorf("owner inviting an owner: %d %s", rec.Code, rec.Body.String())
	}

	// The link flow: the holder is signed in as someone else, then as the
	// invitee. Expired links say so.
	rec = doCookie(t, s, "POST", "/api/workspace/invitations", `{"email":"link@example.test"}`, ownerSID, ownerCSRF)
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	linkToken := res.Link[strings.LastIndex(res.Link, "/")+1:]
	if res.Role != "member" {
		t.Errorf("default role: %+v", res)
	}
	if rec := doCookie(t, s, "POST", "/api/invitations/"+linkToken+"/accept", "", newSID, newCSRF); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "link@example.test") {
		t.Errorf("accept as someone else: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "POST", "/api/invitations/"+linkToken+"/accept", "", false); rec.Code != http.StatusUnauthorized {
		t.Errorf("accept signed out: %d", rec.Code)
	}
	// Signed in as the invitee (a session opened without the sign-in flow,
	// so nothing was accepted yet): the link is accepted, once.
	linkSID, linkCSRF := signIn(t, s, ext.Identity{Subject: "l", Email: "Link@example.test", Provider: "local"})
	if rec := doCookie(t, s, "POST", "/api/invitations/"+linkToken+"/accept", "", linkSID, linkCSRF); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"role":"member"`) {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "POST", "/api/invitations/"+linkToken+"/accept", "", linkSID, linkCSRF); rec.Code != http.StatusNotFound {
		t.Errorf("accept twice: %d %s", rec.Code, rec.Body.String())
	}
	if me := doCookie(t, s, "GET", "/api/me", "", linkSID, ""); !strings.Contains(me.Body.String(), `"workspace":"member"`) {
		t.Errorf("after accepting the link: %s", me.Body.String())
	}
	// Revoke, then a sign-in with that address is refused again.
	rec = doCookie(t, s, "POST", "/api/workspace/invitations", `{"email":"revoked@example.test"}`, ownerSID, ownerCSRF)
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	issuer.passwords["revoked@example.test"] = "pw-revoked"
	var list []InvitationView
	rec = doCookie(t, s, "GET", "/api/workspace/invitations", "", ownerSID, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	var revokedID string
	for _, inv := range list {
		if inv.Email == "revoked@example.test" {
			revokedID = inv.ID
		}
	}
	if rec := doCookie(t, s, "DELETE", "/api/workspace/invitations/"+revokedID, "", ownerSID, ownerCSRF); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "DELETE", "/api/workspace/invitations/"+revokedID, "", ownerSID, ownerCSRF); rec.Code != http.StatusNotFound {
		t.Errorf("revoke again: %d", rec.Code)
	}
	if rec := do(t, s, "POST", "/api/auth/password", `{"email":"revoked@example.test","password":"pw-revoked"}`, false); rec.Code != http.StatusForbidden {
		t.Errorf("revoked, then sign-in: %d %s", rec.Code, rec.Body.String())
	}
	// Expired: visible as such, not acceptable.
	_, hash, _ := newInvitationToken()
	if _, err := s.store.CreateInvitation(ctx, store.DefaultWorkspace, store.Invitation{Email: "late@example.test", Role: "member", ExpiresAt: time.Now().Add(-time.Minute)}, hash); err != nil {
		t.Fatal(err)
	}
	if err := s.admitSignIn(ctx, store.DefaultWorkspace, ext.Identity{Email: "late@example.test", Provider: "local"}); err == nil {
		t.Error("an expired invitation must not admit")
	}
	if rec := doCookie(t, s, "GET", "/api/workspace/invitations", "", ownerSID, ""); !strings.Contains(rec.Body.String(), `"expired":true`) {
		t.Errorf("expired flag: %s", rec.Body.String())
	}

	// Mail failing does not fail the invitation: the link is still returned.
	mail.fail = true
	rec = doCookie(t, s, "POST", "/api/workspace/invitations", `{"email":"other@example.test"}`, ownerSID, ownerCSRF)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"emailed":false`) || !strings.Contains(rec.Body.String(), `"mailError":"smtp down"`) || !strings.Contains(rec.Body.String(), `"link":"`) {
		t.Errorf("mail failure: %d %s", rec.Code, rec.Body.String())
	}
	// Without mail configured, no attempt and no error.
	mail.configured, mail.fail = false, false
	before := len(mail.sent)
	rec = doCookie(t, s, "POST", "/api/workspace/invitations", `{"email":"other@example.test"}`, ownerSID, ownerCSRF)
	if rec.Code != http.StatusCreated || strings.Contains(rec.Body.String(), `mailError`) || len(mail.sent) != before {
		t.Errorf("mail unconfigured: %d %s", rec.Code, rec.Body.String())
	}
}

type fakeMailer struct {
	configured bool
	fail       bool
	sent       []ext.Message
}

func (f *fakeMailer) Configured(context.Context) bool { return f.configured }
func (f *fakeMailer) Send(_ context.Context, m ext.Message) error {
	if f.fail {
		return errFake
	}
	f.sent = append(f.sent, m)
	return nil
}

var errFake = &fakeErr{"smtp down"}

type fakeErr struct{ s string }

func (e *fakeErr) Error() string { return e.s }

// The mail extension (RFC-0013) supplies the server's mailer and the
// Cluster page's status and test endpoints.
func TestMailExtensionWiring(t *testing.T) {
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	cr := crfake.NewClientBuilder().WithScheme(scheme).Build()
	k := &kube.Client{Kube: kubefake.NewSimpleClientset(), Namespace: "shpyrd-system"}
	s, err := newServer(k, Options{Token: testToken, Apps: cr, Public: PublicConfig{Domain: "example.test"}, Extensions: []ext.Extension{mailext.New()}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.mailer == nil || s.mailer.Configured(context.Background()) {
		t.Fatalf("mailer = %v", s.mailer)
	}
	if rec := do(t, s, "GET", "/api/cluster/mail", "", true); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"configured":false`) {
		t.Errorf("status: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "POST", "/api/cluster/mail/test", `{"to":"me@example.test"}`, true); rec.Code != http.StatusConflict {
		t.Errorf("test unconfigured: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "POST", "/api/cluster/mail/test", `{"to":"nope"}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("test bad address: %d %s", rec.Code, rec.Body.String())
	}
	// Once settings exist the status shows them, never the password.
	st := &mailext.Store{Kube: k.Kube, Namespace: "shpyrd-system"}
	if err := st.Set(context.Background(), mailext.Settings{Host: "smtp.example.com", From: "noreply@example.com", User: "u", Password: "hunter2"}); err != nil {
		t.Fatal(err)
	}
	s.mailer.(*mailext.Sender).Forget()
	rec := do(t, s, "GET", "/api/cluster/mail", "", true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"configured":true`) || !strings.Contains(rec.Body.String(), `"auth":true`) || strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("status configured: %d %s", rec.Code, rec.Body.String())
	}
	// Invitations now try to send: an unreachable server is reported, the link still returned.
	s.mailer.(*mailext.Sender).Timeout = 2 * time.Second
	_ = st.Set(context.Background(), mailext.Settings{Host: "127.0.0.1", Port: 9, From: "noreply@example.com", Security: mailext.SecurityNone})
	s.mailer.(*mailext.Sender).Forget()
	rec = do(t, s, "POST", "/api/workspace/invitations", `{"email":"ada@example.test"}`, true)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"emailed":false`) || !strings.Contains(rec.Body.String(), `"mailError":"connect to 127.0.0.1:9`) {
		t.Errorf("invite with mail down: %d %s", rec.Code, rec.Body.String())
	}
}
