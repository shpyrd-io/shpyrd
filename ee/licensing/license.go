//go:build !foss

// Package licensing reads the shpyrd enterprise license (ee/LICENSE): a JWT
// signed with Ed25519 that switches every ee feature on until it expires.
// The header names the signing key (kid); the claims are the license id
// (jti), the customer (sub), when it was issued (iat) and when it stops
// working (exp), in seconds since the epoch; a license the billing app
// issued also names it (iss), where the cluster renews it online and opens
// the customer's billing. Licenses are issued outside this repository
// (shpyrd-license, and the billing app); only the public keys live here, in
// keys/<kid>.pub. There is no grace period: a license is off from exp on.
package licensing

import (
	"crypto/ed25519"
	"crypto/x509"
	"embed"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"
	"time"
)

// Why a license is refused.
var (
	ErrMalformed = errors.New("the license is not a license")
	ErrAlg       = errors.New("the license is not signed with EdDSA")
	ErrKid       = errors.New("the license is signed by a key this build does not know")
	ErrSignature = errors.New("the license's signature does not match")
	ErrClaims    = errors.New("the license lacks its id, customer or dates")
)

// License is a license whose signature verified. It may have expired:
// Active says whether it is in force.
type License struct {
	ID        string    `json:"id"`
	Customer  string    `json:"customer"`
	IssuedAt  time.Time `json:"issuedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	// Issuer is the billing app that issued it (its base URL): the cluster
	// renews the license there before it expires and opens the customer's
	// billing there. "" for a license issued by hand: it renews offline.
	Issuer string `json:"issuer,omitempty"`
}

// ActiveAt says whether the license is in force at t.
func (l License) ActiveAt(t time.Time) bool { return t.Before(l.ExpiresAt) }

//go:embed keys
var keysFS embed.FS

var (
	keysOnce sync.Once
	keys     map[string]ed25519.PublicKey
	keysErr  error
)

// publicKeys are the keys licenses are verified against, by kid: the
// files keys/<kid>.pub, PEM (SPKI).
var publicKeys = func() (map[string]ed25519.PublicKey, error) {
	keysOnce.Do(func() { keys, keysErr = loadKeys(keysFS, "keys") })
	return keys, keysErr
}

func loadKeys(fsys fs.FS, dir string) (map[string]ed25519.PublicKey, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	out := map[string]ed25519.PublicKey{}
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".pub" {
			continue
		}
		raw, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		key, err := ParsePublicKey(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out[strings.TrimSuffix(e.Name(), ".pub")] = key
	}
	return out, nil
}

// ParsePublicKey reads an Ed25519 public key in PEM (SPKI), as
// shpyrd-license keygen writes it.
func ParsePublicKey(raw []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, errors.New("not a PEM public key")
	}
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("not an Ed25519 key")
	}
	return pub, nil
}

// Parse verifies a license against this build's keys. An expired license
// parses: it is still the customer's, only no longer in force.
func Parse(token string) (License, error) {
	ks, err := publicKeys()
	if err != nil {
		return License{}, fmt.Errorf("the license keys of this build: %w", err)
	}
	return ParseWith(token, ks)
}

// ParseWith verifies a license against the keys given, by kid.
func ParseWith(token string, ks map[string]ed25519.PublicKey) (License, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return License{}, ErrMalformed
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	var claims struct {
		ID       string `json:"jti"`
		Customer string `json:"sub"`
		IssuedAt int64  `json:"iat"`
		Expires  int64  `json:"exp"`
		Issuer   string `json:"iss"`
	}
	if decode(parts[0], &header) != nil || decode(parts[1], &claims) != nil {
		return License{}, ErrMalformed
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return License{}, ErrMalformed
	}
	if header.Alg != "EdDSA" {
		return License{}, ErrAlg
	}
	key, ok := ks[header.Kid]
	if !ok || len(key) != ed25519.PublicKeySize {
		return License{}, ErrKid
	}
	if !ed25519.Verify(key, []byte(parts[0]+"."+parts[1]), sig) {
		return License{}, ErrSignature
	}
	if claims.ID == "" || claims.Customer == "" || claims.IssuedAt <= 0 || claims.Expires <= 0 {
		return License{}, ErrClaims
	}
	return License{
		ID:        claims.ID,
		Customer:  claims.Customer,
		IssuedAt:  time.Unix(claims.IssuedAt, 0).UTC(),
		ExpiresAt: time.Unix(claims.Expires, 0).UTC(),
		Issuer:    strings.TrimRight(claims.Issuer, "/"),
	}, nil
}

func decode(part string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}
