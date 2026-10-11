package api

import (
	"context"
	"errors"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"testing"
)

type unavailableAdmissionStore struct {
	store.Store
	failure string
}

func (s unavailableAdmissionStore) ListIdentities(ctx context.Context, ws string) ([]store.Identity, error) {
	if s.failure == "identities" {
		return nil, errors.New("unavailable")
	}
	return s.Store.ListIdentities(ctx, ws)
}
func (s unavailableAdmissionStore) ListDomainClaims(ctx context.Context, ws string) ([]store.DomainClaim, error) {
	if s.failure == "domains" {
		return nil, errors.New("unavailable")
	}
	return s.Store.ListDomainClaims(ctx, ws)
}
func TestAdmissionRefusesUnavailableIdentityOrDomainPolicy(t *testing.T) {
	for _, failure := range []string{"identities", "domains"} {
		t.Run(failure, func(t *testing.T) {
			s, _ := newTestServer(t, nil, nil)
			s.store = unavailableAdmissionStore{Store: s.store, failure: failure}
			if err := s.admitSignIn(context.Background(), store.DefaultWorkspace, ext.Identity{Email: "new@example.test", Provider: "company"}); err == nil {
				t.Fatal("admitted without checking policy")
			}
		})
	}
}
