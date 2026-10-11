package api

import (
	"context"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"strings"
)

// Existing authorization scenarios supply fake people by email. These fixtures
// explicitly provision their IdP identities before assigning permissions. The
// production store never performs this email-to-identity association.
type principalFixtures struct{ *store.Memory }

func principalFixtureStore() *principalFixtures { return &principalFixtures{store.NewMemory()} }
func (s *principalFixtures) TouchIdentity(ctx context.Context, ws string, p store.Identity) (*store.Identity, error) {
	if p.Issuer == "" {
		p.Issuer = "https://fixtures.test"
		p.Subject = strings.ToLower(strings.TrimSpace(p.Email))
	}
	return s.Memory.TouchIdentity(ctx, ws, p)
}
func (s *principalFixtures) ensure(ctx context.Context, ws, key string) (*store.Identity, error) {
	if p, err := s.GetIdentity(ctx, ws, key); err == nil {
		return p, nil
	}
	return s.TouchIdentity(ctx, ws, store.Identity{Email: key, Provider: "local"})
}
func (s *principalFixtures) PutMembership(ctx context.Context, ws, key, role string) (*store.Membership, error) {
	p, err := s.ensure(ctx, ws, key)
	if err != nil {
		return nil, err
	}
	return s.Memory.PutMembership(ctx, ws, p.ID, role)
}
func (s *principalFixtures) PutTeam(ctx context.Context, ws string, t store.Team) (*store.Team, bool, error) {
	for _, email := range t.Members {
		if _, err := s.ensure(ctx, ws, email); err != nil {
			return nil, false, err
		}
	}
	return s.Memory.PutTeam(ctx, ws, t)
}
func (s *principalFixtures) AddGrant(ctx context.Context, ws string, g store.Grant) (*store.Grant, error) {
	if g.User != "" {
		p, err := s.ensure(ctx, ws, g.User)
		if err != nil {
			return nil, err
		}
		g.PrincipalID = p.ID
	}
	return s.Memory.AddGrant(ctx, ws, g)
}
func (s *principalFixtures) AddConsoleUser(ctx context.Context, key, by string) (*store.ConsoleUser, error) {
	if strings.Contains(key, "@") {
		p, err := s.TouchIdentity(ctx, store.DefaultWorkspace, store.Identity{Realm: store.RealmConsole, Email: key, Provider: "local"})
		if err != nil {
			return nil, err
		}
		key = p.ID
	}
	return s.Memory.AddConsoleUser(ctx, key, by)
}
func (s *principalFixtures) CreateToken(ctx context.Context, ws string, t store.APIToken, hash string) (*store.APIToken, error) {
	if t.PrincipalID == "" && t.OwnerEmail != "" {
		p, err := s.ensure(ctx, ws, t.OwnerEmail)
		if err != nil {
			return nil, err
		}
		t.PrincipalID = p.ID
	}
	return s.Memory.CreateToken(ctx, ws, t, hash)
}
func principalFixtureIdentity(s *Server, ctx context.Context, realm, ws string, id ext.Identity) (ext.Identity, error) {
	if id.Email == "" || id.Provider == "token" || id.Provider == "kubeconfig" {
		return id, nil
	}
	if id.Provider != "" && s.rp.provider(id.Provider) == nil {
		s.rp.mu.Lock()
		s.rp.providers[id.Provider] = &oidcProvider{OIDCProvider: ext.OIDCProvider{ID: id.Provider, Issuer: "https://fixtures.test", Password: true}}
		s.rp.order = append(s.rp.order, id.Provider)
		s.rp.mu.Unlock()
	}
	if ws == "" {
		ws = s.defaultSlug(ctx)
	}
	p, err := s.store.TouchIdentity(ctx, ws, store.Identity{Realm: realm, Email: id.Email, Name: id.Name, Provider: id.Provider, Groups: id.Groups})
	if err != nil {
		return id, err
	}
	id.Subject = p.ID
	id.Issuer = p.Issuer
	id.ExternalSubject = p.Subject
	id.EmailVerified = true
	return id, nil
}
func principalFixtureSession(s *Server, ctx context.Context, realm, ws string, id ext.Identity, token string) (*session, error) {
	id, err := principalFixtureIdentity(s, ctx, realm, ws, id)
	if err != nil {
		return nil, err
	}
	return s.rp.sessions.create(ctx, realm, ws, id, token)
}
