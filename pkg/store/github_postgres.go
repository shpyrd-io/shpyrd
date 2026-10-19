package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

const githubColumns = `g.id, w.slug, g.account_login, g.account_type, g.connected_by, g.created_at, g.suspended_at, g.deleted_at`

func scanGitHubInstallation(row pgx.Row) (GitHubInstallation, error) {
	var g GitHubInstallation
	err := row.Scan(&g.ID, &g.Workspace, &g.AccountLogin, &g.AccountType, &g.ConnectedBy, &g.CreatedAt, &g.SuspendedAt, &g.DeletedAt)
	return g, err
}

func (p *Postgres) AddGitHubInstallation(ctx context.Context, ws string, inst GitHubInstallation) error {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return err
	}
	// SET sees the row as it was: created_at stays for a connection that
	// was never uninstalled.
	_, err = p.pool.Exec(ctx, `INSERT INTO github_installations (id, workspace_id, account_login, account_type, connected_by, suspended_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id, workspace_id) DO UPDATE SET
		 created_at = CASE WHEN github_installations.deleted_at IS NULL THEN github_installations.created_at ELSE now() END,
		 account_login = EXCLUDED.account_login, account_type = EXCLUDED.account_type,
		 connected_by = EXCLUDED.connected_by, suspended_at = EXCLUDED.suspended_at, deleted_at = NULL`,
		inst.ID, wsID, inst.AccountLogin, inst.AccountType, inst.ConnectedBy, inst.SuspendedAt)
	return err
}

func (p *Postgres) GitHubInstallations(ctx context.Context, ws string) ([]GitHubInstallation, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	return p.githubInstallations(ctx, `g.workspace_id = $1`, wsID)
}

// githubInstallations are the live rows where cond holds, oldest first.
func (p *Postgres) githubInstallations(ctx context.Context, cond string, arg any) ([]GitHubInstallation, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+githubColumns+` FROM github_installations g JOIN workspaces w ON w.id = g.workspace_id
		WHERE `+cond+` AND g.deleted_at IS NULL ORDER BY g.created_at, g.id, w.slug`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GitHubInstallation{}
	for rows.Next() {
		g, err := scanGitHubInstallation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (p *Postgres) GitHubInstallationConnections(ctx context.Context, id int64) ([]GitHubInstallation, error) {
	return p.githubInstallations(ctx, `g.id = $1`, id)
}

func (p *Postgres) SetGitHubInstallationState(ctx context.Context, id int64, suspended, deleted bool) error {
	tag, err := p.pool.Exec(ctx, `UPDATE github_installations SET
		suspended_at = CASE WHEN $2 THEN COALESCE(suspended_at, now()) ELSE NULL END,
		deleted_at = CASE WHEN $3 THEN COALESCE(deleted_at, now()) ELSE deleted_at END
		WHERE id = $1`, id, suspended, deleted)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) DeleteGitHubInstallation(ctx context.Context, ws string, id int64) error {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx, `DELETE FROM github_installations WHERE id = $1 AND workspace_id = $2`, id, wsID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) PutGitHubUserLink(ctx context.Context, l GitHubUserLink) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO github_user_links (provider, subject, github_user_id, github_login, refresh_token, refresh_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (provider, subject) DO UPDATE SET
		 linked_at = CASE WHEN github_user_links.github_user_id = EXCLUDED.github_user_id THEN github_user_links.linked_at ELSE now() END,
		 github_user_id = EXCLUDED.github_user_id, github_login = EXCLUDED.github_login,
		 refresh_token = EXCLUDED.refresh_token, refresh_expires_at = EXCLUDED.refresh_expires_at, updated_at = now()`,
		l.Provider, l.Subject, l.GitHubUserID, l.GitHubLogin, l.RefreshToken, l.RefreshExpiresAt)
	return err
}

func (p *Postgres) GitHubUserLink(ctx context.Context, provider, subject string) (*GitHubUserLink, error) {
	l := GitHubUserLink{Provider: provider, Subject: subject}
	err := p.pool.QueryRow(ctx, `SELECT github_user_id, github_login, refresh_token, refresh_expires_at, linked_at, updated_at
		FROM github_user_links WHERE provider = $1 AND subject = $2`, provider, subject).
		Scan(&l.GitHubUserID, &l.GitHubLogin, &l.RefreshToken, &l.RefreshExpiresAt, &l.LinkedAt, &l.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (p *Postgres) DeleteGitHubUserLink(ctx context.Context, provider, subject string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM github_user_links WHERE provider = $1 AND subject = $2`, provider, subject)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) ReplaceGitHubUserLinkToken(ctx context.Context, provider, subject, oldSealed string, l GitHubUserLink) (bool, error) {
	tag, err := p.pool.Exec(ctx, `UPDATE github_user_links SET
		 linked_at = CASE WHEN github_user_id = $4 THEN linked_at ELSE now() END,
		 github_user_id = $4, github_login = $5, refresh_token = $6, refresh_expires_at = $7, updated_at = now()
		WHERE provider = $1 AND subject = $2 AND refresh_token = $3`,
		provider, subject, oldSealed, l.GitHubUserID, l.GitHubLogin, l.RefreshToken, l.RefreshExpiresAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (p *Postgres) DeleteGitHubUserLinkIf(ctx context.Context, provider, subject, sealed string) (bool, error) {
	tag, err := p.pool.Exec(ctx, `DELETE FROM github_user_links WHERE provider = $1 AND subject = $2 AND refresh_token = $3`, provider, subject, sealed)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
