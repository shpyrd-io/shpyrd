// Package edge is the identity the platform hands to apps (RFC-0033): the
// ingress asks /edge/auth who the caller is and whether they may open the
// app; the answer carries a short-lived JWT signed with the platform's key,
// which apps verify against the JWKS. The same key signs the per-app-host
// cookie the browser gets after signing in, so an app never sees the
// dashboard's session cookie — only a token bound to that one project.
package edge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// KeysSecretName holds the signing keys (RFC-0033): the current key pair
// and the public halves of the keys retired lately.
const KeysSecretName = "shpyrd-edge-keys"

// Lifetimes.
const (
	TokenTTL  = 5 * time.Minute  // the JWT an app receives
	CookieTTL = 12 * time.Hour   // the per-app-host cookie (the session is checked on every use anyway)
	CodeTTL   = 60 * time.Second // the one-time code carrying a session to an app host
)

// Rotation: a new signing key every RotateEvery; a retired key still
// verifies for KeepRetired, so cookies and tokens it signed live out their
// lifetimes and apps that cached the JWKS catch up.
const (
	RotateEvery = 30 * 24 * time.Hour
	KeepRetired = 7 * 24 * time.Hour
)

// keyPair is one key: the private half only for the current one.
type keyPair struct {
	KID     string
	Private ed25519.PrivateKey
	Public  ed25519.PublicKey
	Created time.Time
	Retired time.Time // zero for the current key
}

// Keys is the platform's signing key ring: the current Ed25519 key pair,
// which signs, and the retired keys, which only verify. Safe for concurrent
// use; Run keeps it rotated and in step with the other replicas.
type Keys struct {
	mu       sync.RWMutex
	current  keyPair
	retired  []keyPair
	kube     kubernetes.Interface
	ns       string
	revision string
	now      func() time.Time
}

// KID is the current key's id.
func (k *Keys) KID() string {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.current.KID
}

// Public is the current key's public half.
func (k *Keys) Public() ed25519.PublicKey {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.current.Public
}

// LoadOrCreateKeys reads the key ring from the Secret, generating the first
// key once, and rotates it when it is due.
func LoadOrCreateKeys(ctx context.Context, kube kubernetes.Interface, namespace string) (*Keys, error) {
	k := &Keys{kube: kube, ns: namespace, now: time.Now}
	if err := k.load(ctx); err != nil {
		return nil, err
	}
	if _, err := k.rotateIfDue(ctx); err != nil {
		return nil, err
	}
	return k, nil
}

// GenerateKeys makes a fresh key ring (tests, clusterless runs).
func GenerateKeys() (*Keys, error) {
	kp, err := newKeyPair(time.Now())
	if err != nil {
		return nil, err
	}
	return &Keys{current: kp, now: time.Now}, nil
}

func newKeyPair(now time.Time) (keyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return keyPair{}, err
	}
	sum := sha256.Sum256(pub)
	return keyPair{KID: hex.EncodeToString(sum[:8]), Private: priv, Public: pub, Created: now}, nil
}

// Run rotates the key when due and picks up what another replica rotated,
// once a minute, until ctx ends.
func (k *Keys) Run(ctx context.Context) {
	if k.kube == nil {
		return
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = k.load(ctx)
			_, _ = k.rotateIfDue(ctx)
		}
	}
}

// retiredKey is the persisted form of a retired key.
type retiredKey struct {
	KID     string    `json:"kid"`
	Public  []byte    `json:"public"`
	Retired time.Time `json:"retired"`
}

// load reads the Secret; a missing one is created with a first key. A
// Secret from before rotation existed (no created time) is read as a key
// created now.
func (k *Keys) load(ctx context.Context) error {
	sec, err := k.kube.CoreV1().Secrets(k.ns).Get(ctx, KeysSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		kp, err := newKeyPair(k.now())
		if err != nil {
			return err
		}
		k.mu.Lock()
		k.current, k.retired = kp, nil
		k.mu.Unlock()
		if err := k.save(ctx, true); err != nil {
			if apierrors.IsAlreadyExists(err) { // another replica won
				return k.load(ctx)
			}
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	if len(sec.Data["private"]) != ed25519.SeedSize {
		return errors.New("edge keys secret: no usable private key")
	}
	priv := ed25519.NewKeyFromSeed(sec.Data["private"])
	cur := keyPair{KID: string(sec.Data["kid"]), Private: priv, Public: priv.Public().(ed25519.PublicKey)}
	if t, err := time.Parse(time.RFC3339, string(sec.Data["created"])); err == nil {
		cur.Created = t
	} else {
		cur.Created = k.now()
	}
	var retired []keyPair
	if raw := sec.Data["retired"]; len(raw) > 0 {
		var list []retiredKey
		if err := json.Unmarshal(raw, &list); err == nil {
			for _, r := range list {
				if len(r.Public) == ed25519.PublicKeySize {
					retired = append(retired, keyPair{KID: r.KID, Public: ed25519.PublicKey(r.Public), Retired: r.Retired})
				}
			}
		}
	}
	k.mu.Lock()
	k.current, k.retired, k.revision = cur, retired, sec.ResourceVersion
	k.mu.Unlock()
	return nil
}

// save writes the ring: creates the Secret, or updates the version that
// was loaded (another replica's rotation in between is a conflict, and the
// caller reloads).
func (k *Keys) save(ctx context.Context, create bool) error {
	k.mu.RLock()
	revision := k.revision
	list := make([]retiredKey, 0, len(k.retired))
	for _, r := range k.retired {
		list = append(list, retiredKey{KID: r.KID, Public: r.Public, Retired: r.Retired})
	}
	retired, _ := json.Marshal(list)
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: KeysSecretName, Namespace: k.ns, Labels: map[string]string{"app.kubernetes.io/managed-by": "shpyrd"}, ResourceVersion: revision},
		Data: map[string][]byte{
			"private": k.current.Private.Seed(), "public": k.current.Public, "kid": []byte(k.current.KID),
			"created": []byte(k.current.Created.UTC().Format(time.RFC3339)), "retired": retired,
		},
	}
	k.mu.RUnlock()
	var saved *corev1.Secret
	var err error
	if create {
		sec.ResourceVersion = ""
		saved, err = k.kube.CoreV1().Secrets(k.ns).Create(ctx, sec, metav1.CreateOptions{})
	} else {
		saved, err = k.kube.CoreV1().Secrets(k.ns).Update(ctx, sec, metav1.UpdateOptions{})
	}
	if err != nil {
		return err
	}
	k.mu.Lock()
	k.revision = saved.ResourceVersion
	k.mu.Unlock()
	return nil
}

// rotateIfDue retires the current key and signs with a new one when the
// current key is older than RotateEvery; retired keys older than
// KeepRetired are dropped. It reports whether it rotated.
func (k *Keys) rotateIfDue(ctx context.Context) (bool, error) {
	now := k.now()
	k.mu.RLock()
	due := now.Sub(k.current.Created) >= RotateEvery
	var stale bool
	for _, r := range k.retired {
		if now.Sub(r.Retired) > KeepRetired {
			stale = true
		}
	}
	k.mu.RUnlock()
	if !due && !stale {
		return false, nil
	}
	if k.kube == nil {
		return false, nil // a generated ring never rotates
	}
	k.mu.Lock()
	kept := k.retired[:0:0]
	for _, r := range k.retired {
		if now.Sub(r.Retired) <= KeepRetired {
			kept = append(kept, r)
		}
	}
	k.retired = kept
	if due {
		fresh, err := newKeyPair(now)
		if err != nil {
			k.mu.Unlock()
			return false, err
		}
		old := k.current
		old.Private, old.Retired = nil, now
		k.retired = append([]keyPair{old}, k.retired...)
		k.current = fresh
	}
	k.mu.Unlock()
	if err := k.save(ctx, false); err != nil {
		if apierrors.IsConflict(err) { // another replica rotated first: take theirs
			return false, k.load(ctx)
		}
		return false, err
	}
	return due, nil
}

// JWKS is the public key set apps verify against: the current key and the
// retired ones still within KeepRetired.
func (k *Keys) JWKS() map[string]any {
	k.mu.RLock()
	defer k.mu.RUnlock()
	keys := []map[string]any{jwk(k.current)}
	for _, r := range k.retired {
		keys = append(keys, jwk(r))
	}
	return map[string]any{"keys": keys}
}

func jwk(kp keyPair) map[string]any {
	return map[string]any{
		"kty": "OKP", "crv": "Ed25519", "alg": "EdDSA", "use": "sig", "kid": kp.KID,
		"x": base64.RawURLEncoding.EncodeToString(kp.Public),
	}
}

// publicFor finds the verification key for a key id: the current key or a
// retired one.
func (k *Keys) publicFor(kid string) (ed25519.PublicKey, bool) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if kid == k.current.KID {
		return k.current.Public, true
	}
	for _, r := range k.retired {
		if r.KID == kid {
			return r.Public, true
		}
	}
	return nil, false
}

// Claims is what an app receives about the caller.
type Claims struct {
	Issuer    string `json:"iss"`
	Subject   string `json:"sub"`
	Audience  string `json:"aud"` // the project slug
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	Email     string `json:"email,omitempty"`
	Name      string `json:"name,omitempty"`
	Workspace string `json:"ws"`
	// WorkspaceID is the workspace's store id: stable where the slug may
	// change with its address. WorkspaceURL is its dashboard on its
	// primary domain, for a way back to it.
	WorkspaceID  string   `json:"wsid,omitempty"`
	WorkspaceURL string   `json:"ws_url,omitempty"`
	Project      string   `json:"project"`
	Roles        []string `json:"roles"`
	Teams        []string `json:"teams"`
	Realm        string   `json:"realm"` // workspace, console:<name>, operator
	Provider     string   `json:"provider,omitempty"`
	// Preview marks an "Open as" session; Actor is who is really there.
	Preview bool   `json:"preview,omitempty"`
	Actor   *Actor `json:"act,omitempty"`
	// Operator says the visitor came from an operator-owned workspace; set
	// at a gate only (RFC-0083).
	Operator bool `json:"operator,omitempty"`
}

// Actor is the real identity behind a preview.
type Actor struct {
	Subject string `json:"sub"`
	Email   string `json:"email,omitempty"`
}

type header struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
	KID string `json:"kid"`
}

// Sign produces a compact JWT (EdDSA) of any claims value with the current
// key; typ names the token's purpose ("JWT" for apps, "shpyrd-edge" for
// cookies).
func (k *Keys) Sign(typ string, claims any) (string, error) {
	k.mu.RLock()
	cur := k.current
	k.mu.RUnlock()
	h, _ := json.Marshal(header{Alg: "EdDSA", Typ: typ, KID: cur.KID})
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(body)
	sig := ed25519.Sign(cur.Private, []byte(signing))
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// Verify checks the signature and the type and decodes the claims.
func (k *Keys) Verify(token, typ string, into any) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errors.New("malformed token")
	}
	rawHeader, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return errors.New("malformed token")
	}
	var h header
	if err := json.Unmarshal(rawHeader, &h); err != nil || h.Alg != "EdDSA" || h.Typ != typ {
		return errors.New("unexpected token header")
	}
	pub, ok := k.publicFor(h.KID)
	if !ok {
		return errors.New("unknown key")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !ed25519.Verify(pub, []byte(parts[0]+"."+parts[1]), sig) {
		return errors.New("bad signature")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errors.New("malformed token")
	}
	return json.Unmarshal(body, into)
}

// Preview is what "Open as" substitutes: the teams the app should see, or
// nobody at all.
type Preview struct {
	Teams     []string `json:"teams,omitempty"`
	Anonymous bool     `json:"anonymous,omitempty"`
}

// CookieClaims is the per-app-host cookie: it names the dashboard session
// (checked on every request, so sign-out ends it) and the one project it
// opens; a preview cookie carries what to pretend.
type CookieClaims struct {
	SessionID string   `json:"sid"`
	Project   string   `json:"project"`
	IssuedAt  int64    `json:"iat"`
	ExpiresAt int64    `json:"exp"`
	Preview   *Preview `json:"preview,omitempty"`
}

const cookieTyp = "shpyrd-edge"

// SignCookie mints the cookie value.
func (k *Keys) SignCookie(c CookieClaims) (string, error) {
	now := time.Now()
	if c.IssuedAt == 0 {
		c.IssuedAt = now.Unix()
	}
	if c.ExpiresAt == 0 {
		c.ExpiresAt = now.Add(CookieTTL).Unix()
	}
	return k.Sign(cookieTyp, c)
}

// VerifyCookie checks a cookie value for the given project.
func (k *Keys) VerifyCookie(value, project string) (*CookieClaims, error) {
	var c CookieClaims
	if err := k.Verify(value, cookieTyp, &c); err != nil {
		return nil, err
	}
	if c.Project != project {
		return nil, errors.New("cookie is for another app")
	}
	if time.Now().Unix() >= c.ExpiresAt {
		return nil, errors.New("cookie expired")
	}
	return &c, nil
}

// GateClaims is what a gate code and a gate's pass carry (RFC-0083): the
// session, the workspace it was opened in, by slug to find it and by ID to
// be sure it is the same one, and the one gate it opens. A code also
// carries the nonce of the browser that asked for it, which its callback
// checks; a pass does not need it.
type GateClaims struct {
	SessionID   string `json:"sid"`
	Workspace   string `json:"ws"`
	WorkspaceID string `json:"wsid"`
	Gate        string `json:"gate"`
	Nonce       string `json:"nonce,omitempty"`
	IssuedAt    int64  `json:"iat,omitempty"`
	ExpiresAt   int64  `json:"exp,omitempty"`
}

// GateCookie is what a gate's cookie holds: a pass, a random handle the
// server maps to the session (never the session itself, which the app
// behind the gate would receive), and the one gate it opens.
type GateCookie struct {
	Pass      string `json:"pass"`
	Gate      string `json:"gate"`
	IssuedAt  int64  `json:"iat,omitempty"`
	ExpiresAt int64  `json:"exp,omitempty"`
}

const gateCookieTyp = "shpyrd-gate"

// SignGateCookie mints a gate cookie's value.
func (k *Keys) SignGateCookie(c GateCookie) (string, error) {
	now := time.Now()
	if c.IssuedAt == 0 {
		c.IssuedAt = now.Unix()
	}
	if c.ExpiresAt == 0 {
		c.ExpiresAt = now.Add(CookieTTL).Unix()
	}
	return k.Sign(gateCookieTyp, c)
}

// VerifyGateCookie checks a gate cookie for the given gate: its type, its
// gate, a pass, and its expiry.
func (k *Keys) VerifyGateCookie(value, gate string) (*GateCookie, error) {
	var c GateCookie
	if err := k.Verify(value, gateCookieTyp, &c); err != nil {
		return nil, err
	}
	if gate == "" || c.Gate != gate {
		return nil, errors.New("cookie is for another gate")
	}
	if c.Pass == "" {
		return nil, errors.New("cookie holds no pass")
	}
	if time.Now().Unix() >= c.ExpiresAt {
		return nil, errors.New("cookie expired")
	}
	return &c, nil
}

// GateBegin is what a workspace hands a gate's host to start the way in:
// the workspace, by slug and by ID, whose platform address the browser goes
// back to, and the gate, signed so that begin sends the browser nowhere a
// workspace did not name, for a minute. It carries no host: the host is
// the workspace's own, looked up when the ticket is used.
type GateBegin struct {
	Workspace   string `json:"ws"`
	WorkspaceID string `json:"wsid"`
	Gate        string `json:"gate"`
	ExpiresAt   int64  `json:"exp"`
}

const gateBeginTyp = "shpyrd-gate-begin"

// SignGateBegin mints a begin ticket, good for CodeTTL unless it says
// otherwise.
func (k *Keys) SignGateBegin(b GateBegin) (string, error) {
	if b.ExpiresAt == 0 {
		b.ExpiresAt = time.Now().Add(CodeTTL).Unix()
	}
	return k.Sign(gateBeginTyp, b)
}

// VerifyGateBegin checks a begin ticket for the given gate: its type, its
// gate, a workspace by slug and ID, and its expiry.
func (k *Keys) VerifyGateBegin(value, gate string) (*GateBegin, error) {
	var b GateBegin
	if err := k.Verify(value, gateBeginTyp, &b); err != nil {
		return nil, err
	}
	if gate == "" || b.Gate != gate {
		return nil, errors.New("ticket is for another gate")
	}
	if b.Workspace == "" || b.WorkspaceID == "" {
		return nil, errors.New("ticket names no workspace")
	}
	if time.Now().Unix() >= b.ExpiresAt {
		return nil, errors.New("ticket expired")
	}
	return &b, nil
}

// Codes hands a session from the dashboard host to an app host: a one-time
// code minted at /.shpyrd/start and redeemed at /.shpyrd/callback within a
// minute, kept in the control-plane store so every replica can redeem it.
// It also keeps gates' passes, read there on every request (RFC-0083).
type Codes struct {
	store store.Sessions
	now   func() time.Time
}

// NewCodes returns a code store on top of the control-plane store.
func NewCodes(st store.Sessions) *Codes { return &Codes{store: st, now: time.Now} }

// Kinds of one-time codes: what a code may be redeemed as. A code minted
// for one purpose is worthless for another.
const (
	KindEdge     = "edge"      // dashboard host → app host: an app cookie
	KindSession  = "session"   // console host → workspace host: a session (RFC-0033 phase 6)
	KindGate     = "gate"      // workspace host → a gate's host: a gate's pass (RFC-0083)
	KindGatePass = "gate-pass" // a gate's host, every request: the session behind a gate cookie (RFC-0083)
)

// envelope wraps the claims of a code with their kind.
type envelope struct {
	Kind   string          `json:"kind"`
	Claims json.RawMessage `json:"claims"`
}

// Mint stores the claims for the host and returns the code.
func (c *Codes) Mint(ctx context.Context, host string, claims CookieClaims) (string, error) {
	return c.MintJSON(ctx, host, KindEdge, claims)
}

// MintJSON stores any claims of a kind for the host and returns the code.
func (c *Codes) MintJSON(ctx context.Context, host, kind string, claims any) (string, error) {
	return c.mint(ctx, host, kind, claims, CodeTTL)
}

// MintPass stores a gate's pass: the claims under a random handle for the
// gate's host, kept for CookieTTL and read on every request.
func (c *Codes) MintPass(ctx context.Context, host string, claims GateClaims) (string, error) {
	return c.mint(ctx, host, KindGatePass, claims, CookieTTL)
}

// mint stores claims of a kind for the host under a random handle, for ttl.
func (c *Codes) mint(ctx context.Context, host, kind string, claims any, ttl time.Duration) (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	inner, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(envelope{Kind: kind, Claims: inner})
	if err != nil {
		return "", err
	}
	if err := c.store.PutCode(ctx, store.Code{Code: code, Host: strings.ToLower(host), Claims: body, ExpiresAt: c.now().Add(ttl)}); err != nil {
		return "", err
	}
	return code, nil
}

// Redeem returns the claims once, for the right host. Any attempt burns
// the code.
func (c *Codes) Redeem(ctx context.Context, code, host string) (*CookieClaims, error) {
	var claims CookieClaims
	if err := c.RedeemJSON(ctx, code, host, KindEdge, &claims); err != nil {
		return nil, err
	}
	return &claims, nil
}

// RedeemJSON returns the claims once, for the right host and kind. Any
// attempt burns the code.
func (c *Codes) RedeemJSON(ctx context.Context, code, host, kind string, into any) error {
	e, err := c.store.TakeCode(ctx, code)
	if err != nil {
		return errors.New("unknown, used or expired code")
	}
	if e.Host != strings.ToLower(host) {
		return fmt.Errorf("code is for %s", e.Host)
	}
	var env envelope
	if err := json.Unmarshal(e.Claims, &env); err != nil || env.Kind == "" {
		// Codes minted before kinds existed carry bare claims of the edge kind.
		if kind != KindEdge {
			return errors.New("code is of another kind")
		}
		return json.Unmarshal(e.Claims, into)
	}
	if env.Kind != kind {
		return errors.New("code is of another kind")
	}
	return json.Unmarshal(env.Claims, into)
}

// Pass reads a pass without consuming it: for its host, of its kind.
func (c *Codes) Pass(ctx context.Context, pass, host string) (*GateClaims, error) {
	e, err := c.store.PeekCode(ctx, pass)
	if err != nil {
		return nil, errors.New("unknown or expired pass")
	}
	if e.Host != strings.ToLower(host) {
		return nil, fmt.Errorf("pass is for %s", e.Host)
	}
	var env envelope
	if err := json.Unmarshal(e.Claims, &env); err != nil || env.Kind != KindGatePass {
		return nil, errors.New("code is of another kind")
	}
	var claims GateClaims
	if err := json.Unmarshal(env.Claims, &claims); err != nil {
		return nil, err
	}
	return &claims, nil
}

// DropPass ends a pass (a gate's logout).
func (c *Codes) DropPass(ctx context.Context, pass string) error {
	if _, err := c.store.TakeCode(ctx, pass); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return nil
}
