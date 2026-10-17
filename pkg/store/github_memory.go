package store

import (
	"context"
	"sort"
)

// githubRow keeps the workspace by ID, as Postgres does: a slug may change.
type githubRow struct {
	GitHubInstallation
	workspaceID string
}

func (m *Memory) slugOfID(id string) string {
	for _, w := range m.workspaces {
		if w.ID == id {
			return w.Slug
		}
	}
	return ""
}

func (m *Memory) githubOut(r *githubRow) GitHubInstallation {
	out := r.GitHubInstallation
	out.Workspace = m.slugOfID(r.workspaceID)
	return out
}

func (m *Memory) AddGitHubInstallation(_ context.Context, ws string, inst GitHubInstallation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsAny(ws)
	if err != nil {
		return err
	}
	if m.github == nil {
		m.github = map[int64]*githubRow{}
	}
	created := m.now()
	if prev, ok := m.github[inst.ID]; ok && prev.DeletedAt == nil {
		if prev.workspaceID != w.ID {
			return &GitHubHeldError{Workspace: m.slugOfID(prev.workspaceID)}
		}
		created = prev.CreatedAt
	}
	row := &githubRow{GitHubInstallation: inst, workspaceID: w.ID}
	row.CreatedAt, row.DeletedAt = created, nil
	m.github[inst.ID] = row
	return nil
}

func (m *Memory) GitHubInstallations(_ context.Context, ws string) ([]GitHubInstallation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsAny(ws)
	if err != nil {
		return nil, err
	}
	out := []GitHubInstallation{}
	for _, r := range m.github {
		if r.workspaceID == w.ID && r.DeletedAt == nil {
			out = append(out, m.githubOut(r))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (m *Memory) GitHubInstallation(_ context.Context, id int64) (*GitHubInstallation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.github[id]
	if !ok || r.DeletedAt != nil {
		return nil, ErrNotFound
	}
	out := m.githubOut(r)
	return &out, nil
}

func (m *Memory) SetGitHubInstallationState(_ context.Context, id int64, suspended, deleted bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.github[id]
	if !ok {
		return ErrNotFound
	}
	now := m.now()
	switch {
	case !suspended:
		r.SuspendedAt = nil
	case r.SuspendedAt == nil:
		r.SuspendedAt = &now
	}
	if deleted && r.DeletedAt == nil {
		r.DeletedAt = &now
	}
	return nil
}

func (m *Memory) DeleteGitHubInstallation(_ context.Context, ws string, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsAny(ws)
	if err != nil {
		return err
	}
	r, ok := m.github[id]
	if !ok || r.workspaceID != w.ID {
		return ErrNotFound
	}
	delete(m.github, id)
	return nil
}

func githubLinkKey(provider, subject string) string { return provider + "\x00" + subject }

func (m *Memory) PutGitHubUserLink(_ context.Context, l GitHubUserLink) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.githubLinks == nil {
		m.githubLinks = map[string]GitHubUserLink{}
	}
	now := m.now()
	key := githubLinkKey(l.Provider, l.Subject)
	l.LinkedAt, l.UpdatedAt = now, now
	l.RefreshExpiresAt = l.RefreshExpiresAt.UTC()
	if prev, ok := m.githubLinks[key]; ok && prev.GitHubUserID == l.GitHubUserID {
		l.LinkedAt = prev.LinkedAt
	}
	m.githubLinks[key] = l
	return nil
}

func (m *Memory) GitHubUserLink(_ context.Context, provider, subject string) (*GitHubUserLink, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.githubLinks[githubLinkKey(provider, subject)]
	if !ok {
		return nil, ErrNotFound
	}
	return &l, nil
}

func (m *Memory) DeleteGitHubUserLink(_ context.Context, provider, subject string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := githubLinkKey(provider, subject)
	if _, ok := m.githubLinks[key]; !ok {
		return ErrNotFound
	}
	delete(m.githubLinks, key)
	return nil
}

func (m *Memory) ReplaceGitHubUserLinkToken(_ context.Context, provider, subject, oldSealed string, l GitHubUserLink) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := githubLinkKey(provider, subject)
	prev, ok := m.githubLinks[key]
	if !ok || prev.RefreshToken != oldSealed {
		return false, nil
	}
	l.Provider, l.Subject = provider, subject
	l.LinkedAt, l.UpdatedAt = m.now(), m.now()
	if prev.GitHubUserID == l.GitHubUserID {
		l.LinkedAt = prev.LinkedAt
	}
	l.RefreshExpiresAt = l.RefreshExpiresAt.UTC()
	m.githubLinks[key] = l
	return true, nil
}

func (m *Memory) DeleteGitHubUserLinkIf(_ context.Context, provider, subject, sealed string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := githubLinkKey(provider, subject)
	if prev, ok := m.githubLinks[key]; !ok || prev.RefreshToken != sealed {
		return false, nil
	}
	delete(m.githubLinks, key)
	return true, nil
}
