package authlocal

// Account tokens are single-use Secrets stored in the system namespace,
// following the same pattern as login tickets (pkg/api/ticket.go): 32 random
// bytes, SHA-256 hashed at rest, with an expiry and the email they belong to.
//
// RFC-0014: password reset (1 h) and invite/set-password (24 h).

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// ResetTokenTTL is how long a password-reset link stays valid.
	ResetTokenTTL = time.Hour

	// InviteTokenTTL is how long an invite / set-password link stays valid.
	InviteTokenTTL = 24 * time.Hour

	// AccountTokenLabel marks account-token Secrets.
	AccountTokenLabel = "shpyrd.io/account-token"

	// TokenKindReset is a password-reset token.
	TokenKindReset = "reset"

	// TokenKindInvite is a set-password-from-invite token.
	TokenKindInvite = "invite"
)

// TokenNamespace is where account token Secrets live (the system namespace).
// Set by the caller (the server wires it from the install record).
// Using a var lets tests override it without changing the API.
var TokenNamespace = "shpyrd-system"

// MintAccountToken creates a single-use token Secret and returns the raw code
// that goes in the email link. kind is TokenKindReset or TokenKindInvite.
func MintAccountToken(ctx context.Context, k kubernetes.Interface, ns, email, kind string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(code))
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)

	ttl := ResetTokenTTL
	if kind == TokenKindInvite {
		ttl = InviteTokenTTL
	}

	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "shpyrd-acctok-" + hex.EncodeToString(suffix),
			Namespace: ns,
			Labels: map[string]string{
				AccountTokenLabel:              kind,
				"app.kubernetes.io/managed-by": "shpyrd",
			},
		},
		Data: map[string][]byte{
			"hash":    []byte(hex.EncodeToString(sum[:])),
			"email":   []byte(email),
			"kind":    []byte(kind),
			"expires": []byte(time.Now().Add(ttl).UTC().Format(time.RFC3339)),
		},
	}
	// Revoke any existing token of the same kind for this email before
	// issuing a new one (re-invite, re-reset: old link stops working).
	_ = revokeAccountTokens(ctx, k, ns, email, kind)

	if _, err := k.CoreV1().Secrets(ns).Create(ctx, sec, metav1.CreateOptions{}); err != nil {
		return "", fmt.Errorf("create account token: %w", err)
	}
	return code, nil
}

// RedeemAccountToken validates code, consumes the Secret and returns the
// email and kind it was issued for. Expired tokens are removed on sight.
func RedeemAccountToken(ctx context.Context, k kubernetes.Interface, ns, code string) (email, kind string, err error) {
	list, err := k.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{
		LabelSelector: AccountTokenLabel,
	})
	if err != nil {
		return "", "", fmt.Errorf("list account tokens: %w", err)
	}
	sum := sha256.Sum256([]byte(code))
	want := []byte(hex.EncodeToString(sum[:]))
	now := time.Now()
	for _, sec := range list.Items {
		expires, perr := time.Parse(time.RFC3339, string(sec.Data["expires"]))
		expired := perr != nil || now.After(expires)
		match := subtle.ConstantTimeCompare(sec.Data["hash"], want) == 1
		if expired || match {
			_ = k.CoreV1().Secrets(ns).Delete(ctx, sec.Name, metav1.DeleteOptions{})
		}
		if match && !expired {
			return string(sec.Data["email"]), string(sec.Data["kind"]), nil
		}
	}
	return "", "", errors.New("link is invalid or has expired; request a new one")
}

// revokeAccountTokens deletes all existing tokens of kind for email.
func revokeAccountTokens(ctx context.Context, k kubernetes.Interface, ns, email, kind string) error {
	list, err := k.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{
		LabelSelector: AccountTokenLabel + "=" + kind,
	})
	if err != nil {
		return err
	}
	for _, sec := range list.Items {
		if string(sec.Data["email"]) == email {
			_ = k.CoreV1().Secrets(ns).Delete(ctx, sec.Name, metav1.DeleteOptions{})
		}
	}
	return nil
}
