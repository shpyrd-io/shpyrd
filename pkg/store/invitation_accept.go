package store

import (
	"context"
	"encoding/json"
	"strings"
)

// AcceptInvitation atomically consumes a pending invitation and binds its access
// to one verified principal. Concurrent sign-ins cannot accept it twice.
func (p *Postgres) AcceptInvitation(ctx context.Context, ws, invitationID, principalID string) error {
	person, err := p.GetIdentity(ctx, ws, principalID)
	if err != nil {
		return err
	}
	if person.ID != principalID || person.Realm != RealmWorkspace || person.Subject == "" || person.Status == StatusSuspended {
		return ErrNotFound
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var email, role string
	var teamID *string
	err = tx.QueryRow(ctx, `SELECT email,role,team_id::text FROM invitations WHERE id::text=$1 AND workspace_id=$2 AND expires_at>now() FOR UPDATE`, invitationID, person.WorkspaceID).Scan(&email, &role, &teamID)
	if err != nil {
		return ErrNotFound
	}
	if !strings.EqualFold(email, person.Email) {
		return ErrNotFound
	}
	_, err = tx.Exec(ctx, `INSERT INTO memberships(id,workspace_id,email,role,principal_id) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(workspace_id,principal_id) WHERE principal_id<>'' DO UPDATE SET role=EXCLUDED.role,email=EXCLUDED.email,updated_at=now()`, newID(), person.WorkspaceID, person.Email, role, person.ID)
	if err != nil {
		return err
	}
	if teamID != nil {
		ids, _ := json.Marshal([]string{person.ID})
		emails, _ := json.Marshal([]string{person.Email})
		_, err = tx.Exec(ctx, `UPDATE teams SET principal_ids=CASE WHEN principal_ids ? $2 THEN principal_ids ELSE principal_ids || $3::jsonb END,members=CASE WHEN members ? $4 THEN members ELSE members || $5::jsonb END,updated_at=now() WHERE id::text=$1`, *teamID, person.ID, ids, person.Email, emails)
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM invitations WHERE id::text=$1`, invitationID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (m *Memory) AcceptInvitation(_ context.Context, ws, invitationID, principalID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return err
	}
	var person *Identity
	for i := range m.identities {
		p := &m.identities[i]
		if p.WorkspaceID == w.ID && p.ID == principalID && p.Realm == RealmWorkspace && p.Subject != "" && p.Status != StatusSuspended {
			person = p
		}
	}
	if person == nil {
		return ErrNotFound
	}
	for i, entry := range m.invitations {
		inv := entry.inv
		if inv.ID != invitationID || inv.WorkspaceID != w.ID || inv.Expired(m.now()) || !strings.EqualFold(inv.Email, person.Email) {
			continue
		}
		found := false
		for j := range m.memberships {
			mb := &m.memberships[j]
			if mb.WorkspaceID == w.ID && mb.PrincipalID == person.ID {
				mb.Role = inv.Role
				mb.Email = person.Email
				mb.UpdatedAt = m.now()
				found = true
			}
		}
		if !found {
			m.memberships = append(m.memberships, Membership{ID: newID(), WorkspaceID: w.ID, PrincipalID: person.ID, Email: person.Email, Role: inv.Role, CreatedAt: m.now(), UpdatedAt: m.now()})
		}
		for j := range m.teams {
			team := &m.teams[j]
			if team.WorkspaceID == w.ID && team.Name == inv.Team && !team.Everyone {
				team.PrincipalIDs = dedupe(append(team.PrincipalIDs, person.ID))
				team.Members = normalizeEmails(append(team.Members, person.Email))
				team.UpdatedAt = m.now()
			}
		}
		m.invitations = append(m.invitations[:i], m.invitations[i+1:]...)
		return nil
	}
	return ErrNotFound
}
