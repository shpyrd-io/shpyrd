package authlocal

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestMintAndRedeemToken(t *testing.T) {
	k := fake.NewClientset(&corev1.Namespace{})
	ctx := context.Background()
	ns := "test-system"

	// Mint a reset token.
	code, err := MintAccountToken(ctx, k, ns, "ada@example.test", TokenKindReset)
	if err != nil {
		t.Fatalf("MintAccountToken: %v", err)
	}
	// Redeem it.
	email, kind, err := RedeemAccountToken(ctx, k, ns, code)
	if err != nil || email != "ada@example.test" || kind != TokenKindReset {
		t.Fatalf("RedeemAccountToken: email=%q kind=%q err=%v", email, kind, err)
	}
	// Single-use: second redeem fails.
	if _, _, err := RedeemAccountToken(ctx, k, ns, code); err == nil {
		t.Error("second redeem should fail")
	}

	// Re-mint (re-invite): old code stops working, new one works.
	code2, _ := MintAccountToken(ctx, k, ns, "ada@example.test", TokenKindInvite)
	_, _ = MintAccountToken(ctx, k, ns, "ada@example.test", TokenKindInvite)
	if _, _, err := RedeemAccountToken(ctx, k, ns, code2); err == nil {
		t.Error("old invite code should be revoked by re-mint")
	}
}

func TestWrongCode(t *testing.T) {
	_ = runtime.NewScheme()
	k := fake.NewClientset()
	ctx := context.Background()
	if _, _, err := RedeemAccountToken(ctx, k, "ns", "notacode"); err == nil {
		t.Error("bogus code must fail")
	}
}
