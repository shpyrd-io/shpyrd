package store

import (
	"context"
	"time"
)

// The workspace lifecycle in memory (#274): the Postgres store's
// behaviour, for tests and development.

// wsLive finds a workspace that is not gone: ErrNotFound when none,
// ErrConflict when it is deleting or deleted.
func (m *Memory) wsLive(slug string) (*Workspace, error) {
	w, err := m.wsAny(slug)
	if err != nil {
		return nil, err
	}
	if w.Gone() {
		return nil, ErrConflict
	}
	return w, nil
}

func (m *Memory) SuspendWorkspace(_ context.Context, slug, reason string) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsLive(slug)
	if err != nil {
		return nil, err
	}
	now := m.now()
	w.Status = WorkspaceSuspended
	if w.SuspendedAt == nil {
		w.SuspendedAt = &now
	}
	if reason != "" {
		w.SuspendReason = reason
	}
	w.UpdatedAt = now
	c := *w
	return &c, nil
}

func (m *Memory) ActivateWorkspace(_ context.Context, slug string) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsLive(slug)
	if err != nil {
		return nil, err
	}
	w.Status, w.SuspendedAt, w.SuspendReason, w.DeleteAfter, w.DeleteReason = WorkspaceActive, nil, "", nil, ""
	w.UpdatedAt = m.now()
	c := *w
	return &c, nil
}

func (m *Memory) ScheduleWorkspaceDeletion(_ context.Context, slug string, after time.Time, reason string) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsLive(slug)
	if err != nil {
		return nil, err
	}
	if w.Status != WorkspaceSuspended {
		return nil, ErrConflict
	}
	after = after.UTC()
	w.DeleteAfter, w.DeleteReason, w.UpdatedAt = &after, reason, m.now()
	c := *w
	return &c, nil
}

func (m *Memory) SetWorkspaceExpiry(_ context.Context, slug string, at *time.Time) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsLive(slug)
	if err != nil {
		return nil, err
	}
	if at != nil {
		t := at.UTC()
		at = &t
	}
	w.ExpiresAt, w.UpdatedAt = at, m.now()
	c := *w
	return &c, nil
}

func (m *Memory) SetWorkspaceProtected(_ context.Context, slug string, on bool) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsLive(slug)
	if err != nil {
		return nil, err
	}
	w.Protected, w.UpdatedAt = on, m.now()
	c := *w
	return &c, nil
}

func (m *Memory) RefreshWorkspaceActivity(_ context.Context, slug string, seen time.Time) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsLive(slug)
	if err != nil {
		return nil, err
	}
	latest := w.CreatedAt
	bump := func(t time.Time) {
		if t.After(latest) {
			latest = t
		}
	}
	if w.LastActivityAt != nil {
		bump(*w.LastActivityAt)
	}
	bump(seen)
	for _, id := range m.identities {
		if id.WorkspaceID == w.ID {
			bump(id.LastSeenAt)
		}
	}
	for _, s := range m.sessions {
		if s.WorkspaceID == w.ID {
			bump(s.LastSeenAt)
		}
	}
	for _, t := range m.tokens {
		if t.token.WorkspaceID == w.ID && t.token.LastUsedAt != nil {
			bump(*t.token.LastUsedAt)
		}
	}
	if m.bill != nil {
		for _, e := range m.bill.sleepEvents {
			if e.WorkspaceID == w.ID && e.Event == "wake" {
				bump(e.At)
			}
		}
	}
	latest = latest.UTC()
	w.LastActivityAt, w.UpdatedAt = &latest, m.now()
	c := *w
	return &c, nil
}

func (m *Memory) BeginWorkspaceDeletion(_ context.Context, slug string) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsAny(slug)
	if err != nil {
		return nil, err
	}
	switch w.Status {
	case WorkspaceDeleting:
		c := *w
		return &c, nil
	case WorkspaceDeleted:
		return nil, ErrConflict
	}
	now := m.now()
	w.Status, w.Deletion, w.UpdatedAt = WorkspaceDeleting, &WorkspaceDeletion{StartedAt: now}, now
	c := *w
	return &c, nil
}

func (m *Memory) SetWorkspaceDeletion(_ context.Context, slug string, d WorkspaceDeletion) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsAny(slug)
	if err != nil {
		return nil, err
	}
	if !w.Gone() {
		return nil, ErrConflict
	}
	dd := d
	w.Deletion, w.UpdatedAt = &dd, m.now()
	c := *w
	return &c, nil
}

func (m *Memory) TombstoneWorkspace(_ context.Context, slug string) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsAny(slug)
	if err != nil {
		return nil, err
	}
	if w.Status == WorkspaceDeleted {
		c := *w
		return &c, nil
	}
	now := m.now()
	owners := []string{}
	for _, mb := range m.memberships {
		if mb.WorkspaceID == w.ID && mb.Role == WorkspaceRoleOwner {
			owners = append(owners, mb.Email)
		}
	}
	keepIdentities := m.identities[:0]
	for _, id := range m.identities {
		if id.WorkspaceID != w.ID {
			keepIdentities = append(keepIdentities, id)
		}
	}
	m.identities = keepIdentities
	keepTeams := m.teams[:0]
	for _, t := range m.teams {
		if t.WorkspaceID != w.ID {
			keepTeams = append(keepTeams, t)
		}
	}
	m.teams = keepTeams
	keepGrants := m.grants[:0]
	for _, g := range m.grants {
		if g.WorkspaceID != w.ID {
			keepGrants = append(keepGrants, g)
		}
	}
	m.grants = keepGrants
	for id, s := range m.sessions {
		if s.WorkspaceID == w.ID {
			delete(m.sessions, id)
		}
	}
	keepTokens := m.tokens[:0]
	for _, t := range m.tokens {
		if t.token.WorkspaceID != w.ID {
			keepTokens = append(keepTokens, t)
		}
	}
	m.tokens = keepTokens
	keepMemberships := m.memberships[:0]
	for _, mb := range m.memberships {
		if mb.WorkspaceID != w.ID {
			keepMemberships = append(keepMemberships, mb)
		}
	}
	m.memberships = keepMemberships
	keepInvitations := m.invitations[:0]
	for _, inv := range m.invitations {
		if inv.inv.WorkspaceID != w.ID {
			keepInvitations = append(keepInvitations, inv)
		}
	}
	m.invitations = keepInvitations
	keepDomains := m.domains[:0]
	for _, d := range m.domains {
		if d.WorkspaceID != w.ID {
			keepDomains = append(keepDomains, d)
		}
	}
	m.domains = keepDomains
	keepClients := m.oclients[:0]
	for _, c := range m.oclients {
		if c.WorkspaceID != w.ID {
			keepClients = append(keepClients, c)
		}
	}
	m.oclients = keepClients
	for code, c := range m.ocodes {
		if c.WorkspaceID == w.ID {
			delete(m.ocodes, code)
		}
	}
	keepOTokens := m.otokens[:0]
	for _, t := range m.otokens {
		if t.WorkspaceID != w.ID {
			keepOTokens = append(keepOTokens, t)
		}
	}
	m.otokens = keepOTokens
	for k := range m.github {
		if k.workspaceID == w.ID {
			delete(m.github, k)
		}
	}
	for hash, d := range m.devices {
		if d.WorkspaceID == w.ID {
			delete(m.devices, hash)
		}
	}
	for i := range m.projects {
		if p := &m.projects[i]; p.WorkspaceID == w.ID && p.DeletedAt == nil {
			p.DeletedAt, p.UpdatedAt = &now, now
		}
	}
	if w.Deletion == nil {
		w.Deletion = &WorkspaceDeletion{StartedAt: now}
	}
	w.Deletion.FinishedAt = &now
	w.Status, w.DeletedAt, w.TombstoneOwners = WorkspaceDeleted, &now, owners
	w.OwnerInvitePending, w.CreationFingerprint, w.Readiness, w.ReadyAt = "", "", nil, nil
	w.UpdatedAt = now
	c := *w
	return &c, nil
}

func (m *Memory) ScrubWorkspace(_ context.Context, slug string) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsAny(slug)
	if err != nil {
		return nil, err
	}
	if w.Status != WorkspaceDeleted {
		return nil, ErrConflict
	}
	keepHosts := m.hosts[:0]
	for _, h := range m.hosts {
		if h.WorkspaceID != w.ID {
			keepHosts = append(keepHosts, h)
		}
	}
	m.hosts = keepHosts
	delete(m.workspaces, w.Slug)
	w.Slug, w.Address, w.TombstoneOwners, w.UpdatedAt = ScrubbedSlug(w.ID), "", nil, m.now()
	m.workspaces[w.Slug] = w
	c := *w
	return &c, nil
}

func (m *Memory) ListDeletedWorkspaces(_ context.Context) ([]Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Workspace, 0)
	for _, w := range m.workspaces {
		if w.Status == WorkspaceDeleted {
			out = append(out, *w)
		}
	}
	sortWorkspaces(out)
	return out, nil
}
