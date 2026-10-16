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

func (m *Memory) AddGitHubInstallation(_ context.Context, ws string, inst GitHubInstallation) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsAny(ws)
	if err != nil {
		return "", err
	}
	if m.github == nil {
		m.github = map[int64]*githubRow{}
	}
	moved, created := "", m.now()
	if prev, ok := m.github[inst.ID]; ok && prev.DeletedAt == nil {
		if prev.workspaceID != w.ID {
			moved = m.slugOfID(prev.workspaceID)
		} else {
			created = prev.CreatedAt
		}
	}
	row := &githubRow{GitHubInstallation: inst, workspaceID: w.ID}
	row.CreatedAt, row.DeletedAt = created, nil
	m.github[inst.ID] = row
	return moved, nil
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
