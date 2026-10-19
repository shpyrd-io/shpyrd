package store

import (
	"context"
	"sort"
)

// githubKey is a connection: an installation and a workspace's ID.
type githubKey struct {
	id          int64
	workspaceID string
}

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
		m.github = map[githubKey]*githubRow{}
	}
	key := githubKey{inst.ID, w.ID}
	created := m.now()
	if prev, ok := m.github[key]; ok && prev.DeletedAt == nil {
		created = prev.CreatedAt
	}
	row := &githubRow{GitHubInstallation: inst, workspaceID: w.ID}
	row.CreatedAt, row.DeletedAt = created, nil
	m.github[key] = row
	return nil
}

func (m *Memory) GitHubInstallations(_ context.Context, ws string) ([]GitHubInstallation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsAny(ws)
	if err != nil {
		return nil, err
	}
	return m.githubLive(func(k githubKey) bool { return k.workspaceID == w.ID }), nil
}

func (m *Memory) GitHubInstallationConnections(_ context.Context, id int64) ([]GitHubInstallation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.githubLive(func(k githubKey) bool { return k.id == id }), nil
}

// githubLive are the rows not uninstalled whose key matches, oldest first.
func (m *Memory) githubLive(match func(githubKey) bool) []GitHubInstallation {
	out := []GitHubInstallation{}
	for k, r := range m.github {
		if match(k) && r.DeletedAt == nil {
			out = append(out, m.githubOut(r))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Workspace < out[j].Workspace
	})
	return out
}

func (m *Memory) SetGitHubInstallationState(_ context.Context, id int64, suspended, deleted bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	found := false
	for k, r := range m.github {
		if k.id != id {
			continue
		}
		found = true
		switch {
		case !suspended:
			r.SuspendedAt = nil
		case r.SuspendedAt == nil:
			r.SuspendedAt = &now
		}
		if deleted && r.DeletedAt == nil {
			r.DeletedAt = &now
		}
	}
	if !found {
		return ErrNotFound
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
	key := githubKey{id, w.ID}
	if _, ok := m.github[key]; !ok {
		return ErrNotFound
	}
	delete(m.github, key)
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
