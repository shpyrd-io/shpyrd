package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const githubColumns = `g.id, w.slug, g.account_login, g.account_type, g.connected_by, g.created_at, g.suspended_at, g.deleted_at`

func scanGitHubInstallation(row pgx.Row) (GitHubInstallation, error) {
	var g GitHubInstallation
	err := row.Scan(&g.ID, &g.Workspace, &g.AccountLogin, &g.AccountType, &g.ConnectedBy, &g.CreatedAt, &g.SuspendedAt, &g.DeletedAt)
	return g, err
}

func (p *Postgres) AddGitHubInstallation(ctx context.Context, ws string, inst GitHubInstallation) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	wsID, err := p.wsID(ctx, tx, ws)
	if err != nil {
		return err
	}
	var prevID, prevSlug string
	var prevDeleted *time.Time
	err = tx.QueryRow(ctx, `SELECT g.workspace_id::text, w.slug, g.deleted_at FROM github_installations g
		JOIN workspaces w ON w.id = g.workspace_id WHERE g.id = $1 FOR UPDATE OF g`, inst.ID).Scan(&prevID, &prevSlug, &prevDeleted)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && prevDeleted == nil && prevID != wsID {
		return &GitHubHeldError{Workspace: prevSlug}
	}
	// SET sees the row as it was: created_at stays only for a connection
	// to the same workspace that was never uninstalled.
	tag, err := tx.Exec(ctx, `INSERT INTO github_installations (id, workspace_id, account_login, account_type, connected_by, suspended_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
		 created_at = CASE WHEN github_installations.workspace_id = EXCLUDED.workspace_id AND github_installations.deleted_at IS NULL
		  THEN github_installations.created_at ELSE now() END,
		 workspace_id = EXCLUDED.workspace_id, account_login = EXCLUDED.account_login, account_type = EXCLUDED.account_type,
		 connected_by = EXCLUDED.connected_by, suspended_at = EXCLUDED.suspended_at, deleted_at = NULL
		WHERE github_installations.workspace_id = EXCLUDED.workspace_id OR github_installations.deleted_at IS NOT NULL`,
		inst.ID, wsID, inst.AccountLogin, inst.AccountType, inst.ConnectedBy, inst.SuspendedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Another workspace's connect, inserted after the check above, won:
		// the WHERE kept it there.
		var slug string
		if err := tx.QueryRow(ctx, `SELECT w.slug FROM github_installations g JOIN workspaces w ON w.id = g.workspace_id WHERE g.id = $1`, inst.ID).Scan(&slug); err != nil {
			return err
		}
		return &GitHubHeldError{Workspace: slug}
	}
	return tx.Commit(ctx)
}

func (p *Postgres) GitHubInstallations(ctx context.Context, ws string) ([]GitHubInstallation, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT `+githubColumns+` FROM github_installations g JOIN workspaces w ON w.id = g.workspace_id
		WHERE g.workspace_id = $1 AND g.deleted_at IS NULL ORDER BY g.created_at, g.id`, wsID)
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

func (p *Postgres) GitHubInstallation(ctx context.Context, id int64) (*GitHubInstallation, error) {
	g, err := scanGitHubInstallation(p.pool.QueryRow(ctx, `SELECT `+githubColumns+` FROM github_installations g
		JOIN workspaces w ON w.id = g.workspace_id WHERE g.id = $1 AND g.deleted_at IS NULL`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
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
