package store

import (
	"context"
	"strings"
)

func firstID(id string) string {
	if id != "" {
		return id
	}
	return newID()
}

// Email is only an administrative convenience. Ambiguous addresses must be
// addressed by principal ID; authentication never uses this lookup.
func selectIdentity(people []Identity, key string) (*Identity, error) {
	key = strings.TrimSpace(key)
	for _, p := range people {
		if p.ID == key {
			return &p, nil
		}
	}
	var found *Identity
	for _, p := range people {
		if strings.EqualFold(p.Email, key) {
			if found != nil {
				return nil, ErrConflict
			}
			copy := p
			found = &copy
		}
	}
	if found == nil {
		return nil, ErrNotFound
	}
	return found, nil
}

func firstKey(id, contact string) string {
	if id != "" {
		return id
	}
	return contact
}

// resolveTeam binds administrative selections at write time. It never grants
// access to a future account merely because it later acquires an email address.
func resolveTeam(ctx context.Context, st Store, ws string, t Team) (Team, error) {
	if t.Name == TeamEveryone {
		return t, ErrBuiltIn
	}
	keys := normalizeEmails(t.Members)
	if t.PrincipalIDs != nil {
		keys = t.PrincipalIDs
	}
	t.PrincipalIDs = []string{}
	t.Members = []string{}
	for _, key := range keys {
		p, err := st.GetIdentity(ctx, ws, key)
		if err != nil {
			return t, err
		}
		if p.Realm != RealmWorkspace {
			return t, ErrNotFound
		}
		t.PrincipalIDs = append(t.PrincipalIDs, p.ID)
		t.Members = append(t.Members, p.Email)
	}
	return t, nil
}

// BindIdentity is an explicit administrative migration of an unbound legacy
// principal. Already-bound principals cannot silently change identity.
func (p *Postgres) BindIdentity(ctx context.Context, ws, id, issuer, subject, connector string) error {
	if issuer == "" || subject == "" {
		return ErrConflict
	}
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx, `UPDATE identities SET issuer=$3,subject=$4,connector=$5 WHERE workspace_id=$1 AND id::text=$2 AND subject=''`, wsID, id, issuer, subject, connector)
	if isUnique(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}
func (m *Memory) BindIdentity(_ context.Context, ws, id, issuer, subject, connector string) error {
	if issuer == "" || subject == "" {
		return ErrConflict
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return err
	}
	var target *Identity
	for i := range m.identities {
		p := &m.identities[i]
		if p.WorkspaceID == w.ID && p.ID == id {
			target = p
		}
	}
	if target == nil {
		return ErrNotFound
	}
	if target.Subject != "" {
		return ErrConflict
	}
	for _, p := range m.identities {
		if p.WorkspaceID == w.ID && p.Realm == target.Realm && p.Issuer == issuer && p.Subject == subject && p.Connector == connector {
			return ErrConflict
		}
	}
	target.Issuer, target.Subject, target.Connector = issuer, subject, connector
	return nil
}
