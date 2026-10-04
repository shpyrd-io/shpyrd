package edge

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	kubefake "k8s.io/client-go/kubernetes/fake"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

func TestKeysRoundTrip(t *testing.T) {
	kube := kubefake.NewSimpleClientset()
	k1, err := LoadOrCreateKeys(context.Background(), kube, "shpyrd-system")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := LoadOrCreateKeys(context.Background(), kube, "shpyrd-system")
	if err != nil || k2.KID() != k1.KID() || !k2.Public().Equal(k1.Public()) {
		t.Fatalf("second load must return the same key: %v", err)
	}
	jwks := k1.JWKS()
	keys := jwks["keys"].([]map[string]any)
	if len(keys) != 1 || keys[0]["kid"] != k1.KID() || keys[0]["crv"] != "Ed25519" {
		t.Errorf("jwks = %v", jwks)
	}
	x, _ := base64.RawURLEncoding.DecodeString(keys[0]["x"].(string))
	if !k1.Public().Equal(ed25519PublicKey(x)) {
		t.Error("jwks x is not the public key")
	}

	claims := Claims{Issuer: "https://shpyrd.example.test", Subject: "idn_1", Audience: "expenses", Email: "joao@acme.test", Roles: []string{"user"}, Teams: []string{"finance"}, ExpiresAt: time.Now().Add(time.Minute).Unix()}
	tok, err := k1.Sign("JWT", claims)
	if err != nil || strings.Count(tok, ".") != 2 {
		t.Fatalf("sign: %v %s", err, tok)
	}
	var back Claims
	if err := k1.Verify(tok, "JWT", &back); err != nil || back.Email != "joao@acme.test" || back.Audience != "expenses" {
		t.Fatalf("verify: %v %+v", err, back)
	}
	// Wrong type, tampered body, other key.
	if err := k1.Verify(tok, "shpyrd-edge", &back); err == nil {
		t.Error("type must be checked")
	}
	parts := strings.Split(tok, ".")
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"aud":"other"}`)) + "." + parts[2]
	if err := k1.Verify(tampered, "JWT", &back); err == nil {
		t.Error("tampered body must fail")
	}
	other, _ := GenerateKeys()
	if err := other.Verify(tok, "JWT", &back); err == nil {
		t.Error("another key must fail")
	}
}

func TestCookieAndCodes(t *testing.T) {
	k, _ := GenerateKeys()
	val, err := k.SignCookie(CookieClaims{SessionID: "s1", Project: "expenses"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := k.VerifyCookie(val, "expenses")
	if err != nil || c.SessionID != "s1" || c.ExpiresAt <= time.Now().Unix() {
		t.Fatalf("cookie: %v %+v", err, c)
	}
	if _, err := k.VerifyCookie(val, "crm"); err == nil {
		t.Error("a cookie opens one project only")
	}
	expired, _ := k.SignCookie(CookieClaims{SessionID: "s1", Project: "expenses", ExpiresAt: time.Now().Add(-time.Second).Unix()})
	if _, err := k.VerifyCookie(expired, "expenses"); err == nil {
		t.Error("expired cookie must fail")
	}

	ctx := context.Background()
	mem := store.NewMemory()
	codes := NewCodes(mem)
	code, err := codes.Mint(ctx, "Expenses.acme.test", CookieClaims{SessionID: "s1", Project: "expenses", Preview: &Preview{Teams: []string{"finance"}}})
	if err != nil {
		t.Fatal(err)
	}
	wrong, _ := codes.Mint(ctx, "expenses.acme.test", CookieClaims{SessionID: "s1", Project: "expenses"})
	if _, err := codes.Redeem(ctx, wrong, "crm.acme.test"); err == nil {
		t.Error("code is bound to the host")
	}
	if _, err := codes.Redeem(ctx, wrong, "expenses.acme.test"); err == nil {
		t.Error("an attempt for the wrong host burns the code")
	}
	got, err := codes.Redeem(ctx, code, "expenses.acme.test")
	if err != nil || got.Preview == nil || got.Preview.Teams[0] != "finance" {
		t.Fatalf("redeem: %v %+v", err, got)
	}
	if _, err := codes.Redeem(ctx, code, "expenses.acme.test"); err == nil {
		t.Error("a code is redeemed once")
	}
	codes.now = func() time.Time { return time.Now().Add(-2 * CodeTTL) } // minted in the past: already expired
	late, _ := codes.Mint(ctx, "expenses.acme.test", CookieClaims{})
	codes.now = time.Now
	if _, err := codes.Redeem(ctx, late, "expenses.acme.test"); err == nil {
		t.Error("stale code must fail")
	}
}

func ed25519PublicKey(b []byte) ed25519.PublicKey { return ed25519.PublicKey(b) }

// A gate cookie opens its own gate and nothing else: not another gate, not
// an app, and an app cookie does not open a gate. It holds a pass, never a
// session.
func TestAGateCookieOpensOnlyItsGate(t *testing.T) {
	k, _ := GenerateKeys()
	val, err := k.SignGateCookie(GateCookie{Pass: "p1", Gate: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := k.VerifyGateCookie(val, "billing")
	if err != nil || c.Pass != "p1" || c.Gate != "billing" || c.ExpiresAt <= time.Now().Unix() {
		t.Fatalf("gate cookie: %v %+v", err, c)
	}
	if _, err := k.VerifyGateCookie(val, "reports"); err == nil {
		t.Error("a gate cookie opened another gate")
	}
	if _, err := k.VerifyCookie(val, "billing"); err == nil {
		t.Error("a gate cookie passed for an app cookie")
	}
	app, _ := k.SignCookie(CookieClaims{SessionID: "s1", Project: "billing"})
	if _, err := k.VerifyGateCookie(app, "billing"); err == nil {
		t.Error("an app cookie passed for a gate cookie")
	}
	nopass, _ := k.SignGateCookie(GateCookie{Gate: "billing"})
	if _, err := k.VerifyGateCookie(nopass, "billing"); err == nil {
		t.Error("a gate cookie without a pass was accepted")
	}
	nogate, _ := k.SignGateCookie(GateCookie{Pass: "p1"})
	if _, err := k.VerifyGateCookie(nogate, ""); err == nil {
		t.Error("a gate cookie for no gate was accepted")
	}
	expired, _ := k.SignGateCookie(GateCookie{Pass: "p1", Gate: "billing", ExpiresAt: time.Now().Add(-time.Second).Unix()})
	if _, err := k.VerifyGateCookie(expired, "billing"); err == nil {
		t.Error("an expired gate cookie was accepted")
	}
}

// A begin ticket starts the way in to its own gate only, for a minute, and
// names a host to come back to; no cookie passes for one.
func TestABeginTicketStartsOnlyItsGate(t *testing.T) {
	k, _ := GenerateKeys()
	val, err := k.SignGateBegin(GateBegin{Host: "acme.example.test", Gate: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := k.VerifyGateBegin(val, "billing")
	if err != nil || b.Host != "acme.example.test" || b.Gate != "billing" || b.ExpiresAt <= time.Now().Unix() || b.ExpiresAt > time.Now().Add(CodeTTL).Unix()+1 {
		t.Fatalf("begin ticket: %v %+v", err, b)
	}
	if _, err := k.VerifyGateBegin(val, "reports"); err == nil {
		t.Error("a begin ticket started another gate")
	}
	if _, err := k.VerifyGateBegin(val, ""); err == nil {
		t.Error("a begin ticket started no gate")
	}
	expired, _ := k.SignGateBegin(GateBegin{Host: "acme.example.test", Gate: "billing", ExpiresAt: time.Now().Add(-time.Second).Unix()})
	if _, err := k.VerifyGateBegin(expired, "billing"); err == nil {
		t.Error("an expired begin ticket was accepted")
	}
	nohost, _ := k.SignGateBegin(GateBegin{Gate: "billing"})
	if _, err := k.VerifyGateBegin(nohost, "billing"); err == nil {
		t.Error("a begin ticket without a host was accepted")
	}
	gate, _ := k.SignGateCookie(GateCookie{Pass: "acme.example.test", Gate: "billing"})
	if _, err := k.VerifyGateBegin(gate, "billing"); err == nil {
		t.Error("a gate cookie passed for a begin ticket")
	}
	app, _ := k.SignCookie(CookieClaims{SessionID: "s1", Project: "billing"})
	if _, err := k.VerifyGateBegin(app, "billing"); err == nil {
		t.Error("an app cookie passed for a begin ticket")
	}
	if _, err := k.VerifyGateCookie(val, "billing"); err == nil {
		t.Error("a begin ticket passed for a gate cookie")
	}
}

// A pass is read as often as needed, at its host only, and ends when it is
// dropped; a code of another kind is no pass.
func TestAPassIsReadAtItsHostUntilDropped(t *testing.T) {
	ctx := context.Background()
	codes := NewCodes(store.NewMemory())
	claims := GateClaims{SessionID: "s1", Workspace: "acme", WorkspaceID: "w1", Gate: "billing"}
	pass, err := codes.MintPass(ctx, "Billing.example.test", claims)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		got, err := codes.Pass(ctx, pass, "billing.example.test")
		if err != nil || *got != claims {
			t.Fatalf("read %d: %v %+v", i, err, got)
		}
	}
	if _, err := codes.Pass(ctx, pass, "reports.example.test"); err == nil {
		t.Error("a pass was read at another host")
	}
	edgeCode, _ := codes.Mint(ctx, "billing.example.test", CookieClaims{SessionID: "s1", Project: "billing"})
	if _, err := codes.Pass(ctx, edgeCode, "billing.example.test"); err == nil {
		t.Error("an app's edge code was read as a pass")
	}
	gateCode, _ := codes.MintJSON(ctx, "billing.example.test", KindGate, claims)
	if _, err := codes.Pass(ctx, gateCode, "billing.example.test"); err == nil {
		t.Error("a gate code was read as a pass")
	}
	if err := codes.DropPass(ctx, pass); err != nil {
		t.Fatal(err)
	}
	if _, err := codes.Pass(ctx, pass, "billing.example.test"); err == nil {
		t.Error("a dropped pass was read")
	}
	if err := codes.DropPass(ctx, pass); err != nil {
		t.Errorf("dropping a pass twice: %v", err)
	}
	codes.now = func() time.Time { return time.Now().Add(-2 * CookieTTL) } // minted in the past: already expired
	late, _ := codes.MintPass(ctx, "billing.example.test", claims)
	codes.now = time.Now
	if _, err := codes.Pass(ctx, late, "billing.example.test"); err == nil {
		t.Error("an expired pass was read")
	}
}

// A gate code is redeemed once, at its host, as a gate code only: never as
// an app's edge code.
func TestAGateCodeIsOnlyAGateCode(t *testing.T) {
	ctx := context.Background()
	codes := NewCodes(store.NewMemory())
	claims := GateClaims{SessionID: "s1", Workspace: "acme", WorkspaceID: "w1", Gate: "billing"}
	code, err := codes.MintJSON(ctx, "billing.example.test", KindGate, claims)
	if err != nil {
		t.Fatal(err)
	}
	var got GateClaims
	if err := codes.RedeemJSON(ctx, code, "billing.example.test", KindGate, &got); err != nil || got != claims {
		t.Fatalf("redeem: %v %+v", err, got)
	}
	if err := codes.RedeemJSON(ctx, code, "billing.example.test", KindGate, &got); err == nil {
		t.Error("a gate code was redeemed twice")
	}
	asApp, _ := codes.MintJSON(ctx, "billing.example.test", KindGate, claims)
	if _, err := codes.Redeem(ctx, asApp, "billing.example.test"); err == nil {
		t.Error("a gate code was redeemed as an app's code")
	}
}

// The JWT of a gate says whether the visitor came from an operator-owned
// workspace; an app's says nothing of it.
func TestTheOperatorClaimIsOmittedWhenFalse(t *testing.T) {
	k, _ := GenerateKeys()
	tok, _ := k.Sign("JWT", Claims{Audience: "billing", Operator: true})
	var c map[string]any
	if err := k.Verify(tok, "JWT", &c); err != nil || c["operator"] != true {
		t.Fatalf("operator claim: %v %v", err, c)
	}
	tok, _ = k.Sign("JWT", Claims{Audience: "shop"})
	c = nil
	if err := k.Verify(tok, "JWT", &c); err != nil {
		t.Fatal(err)
	}
	if _, ok := c["operator"]; ok {
		t.Errorf("an app's JWT carries operator: %v", c)
	}
}

// The signing key rotates every RotateEvery: the retired key still
// verifies what it signed and stays in the JWKS for KeepRetired, then
// goes; another replica loading the Secret follows; a ring saved before
// rotation existed (no created time) is read as created now.
func TestKeysRotate(t *testing.T) {
	kube := kubefake.NewSimpleClientset()
	ctx := context.Background()
	k, err := LoadOrCreateKeys(ctx, kube, "shpyrd-system")
	if err != nil {
		t.Fatal(err)
	}
	clock := k.current.Created
	k.now = func() time.Time { return clock }
	first := k.KID()
	tok, err := k.Sign("JWT", Claims{Subject: "u1", Audience: "expenses", ExpiresAt: clock.Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	// Not due: nothing happens.
	if rotated, err := k.rotateIfDue(ctx); err != nil || rotated {
		t.Fatalf("early rotation: %v %v", rotated, err)
	}
	// Due: a new key signs, the old one verifies, both are published.
	clock = clock.Add(RotateEvery + time.Minute)
	if rotated, err := k.rotateIfDue(ctx); err != nil || !rotated {
		t.Fatalf("rotation: %v %v", rotated, err)
	}
	if k.KID() == first {
		t.Fatal("the key id did not change")
	}
	var back Claims
	if err := k.Verify(tok, "JWT", &back); err != nil {
		t.Errorf("a token of the retired key must still verify: %v", err)
	}
	keys := k.JWKS()["keys"].([]map[string]any)
	if len(keys) != 2 || keys[0]["kid"] != k.KID() || keys[1]["kid"] != first {
		t.Errorf("jwks after rotation = %v", keys)
	}
	// Another replica loads the Secret and sees the same ring.
	other, err := LoadOrCreateKeys(ctx, kube, "shpyrd-system")
	if err != nil || other.KID() != k.KID() || len(other.JWKS()["keys"].([]map[string]any)) != 2 {
		t.Fatalf("second replica: %v kid=%s", err, other.KID())
	}
	if err := other.Verify(tok, "JWT", &back); err != nil {
		t.Errorf("the other replica must verify the retired key's token: %v", err)
	}
	// After KeepRetired the retired key goes.
	clock = clock.Add(KeepRetired + time.Minute)
	if _, err := k.rotateIfDue(ctx); err != nil {
		t.Fatal(err)
	}
	if err := k.Verify(tok, "JWT", &back); err == nil {
		t.Error("a token of a dropped key must not verify")
	}
	if keys := k.JWKS()["keys"].([]map[string]any); len(keys) != 1 {
		t.Errorf("jwks after the retired key expired = %v", keys)
	}
	// A generated ring (no cluster) never rotates.
	g, _ := GenerateKeys()
	g.now = func() time.Time { return time.Now().Add(2 * RotateEvery) }
	if rotated, _ := g.rotateIfDue(ctx); rotated {
		t.Error("a generated ring rotated")
	}
}
