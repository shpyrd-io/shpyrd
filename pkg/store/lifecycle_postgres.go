package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// The workspace lifecycle in Postgres (#274). Each change is one UPDATE
// guarded by the statuses it is allowed from; zero rows means the
// workspace does not exist (ErrNotFound) or is in a state that refuses the
// change (ErrConflict), told apart by a second look.

// liveStatuses are the statuses a workspace can be suspended, activated or
// scheduled from: not yet gone.
const liveStatuses = `('` + WorkspaceActive + `', '` + WorkspaceSuspended + `')`

// updateWorkspace runs set (the SET clause, with $2.. for args) on the
// workspace named by slug or id while its status is one of statuses (a SQL
// list), and returns the row.
func (p *Postgres) updateWorkspace(ctx context.Context, slug, statuses, set string, args ...any) (*Workspace, error) {
	all := append([]any{slug}, args...)
	tag, err := p.pool.Exec(ctx, `UPDATE workspaces SET `+set+`, updated_at = now() WHERE (slug = $1 OR id::text = $1) AND status IN `+statuses, all...)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspaces WHERE slug = $1 OR id::text = $1)`, slug).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrNotFound
		}
		return nil, ErrConflict
	}
	return p.workspaceBySlugOrID(ctx, slug)
}

func (p *Postgres) workspaceBySlugOrID(ctx context.Context, slug string) (*Workspace, error) {
	return scanWorkspace(p.pool.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE slug = $1 OR id::text = $1`, slug))
}

func (p *Postgres) SuspendWorkspace(ctx context.Context, slug, reason string) (*Workspace, error) {
	return p.updateWorkspace(ctx, slug, liveStatuses,
		`status = '`+WorkspaceSuspended+`', suspended_at = COALESCE(suspended_at, now()), suspend_reason = CASE WHEN $2 <> '' THEN $2 ELSE suspend_reason END`, reason)
}

func (p *Postgres) ActivateWorkspace(ctx context.Context, slug string) (*Workspace, error) {
	return p.updateWorkspace(ctx, slug, liveStatuses,
		`status = '`+WorkspaceActive+`', suspended_at = NULL, suspend_reason = '', delete_after = NULL, delete_reason = ''`)
}

func (p *Postgres) ScheduleWorkspaceDeletion(ctx context.Context, slug string, after time.Time, reason string) (*Workspace, error) {
	return p.updateWorkspace(ctx, slug, `('`+WorkspaceSuspended+`')`, `delete_after = $2, delete_reason = $3`, after, reason)
}

func (p *Postgres) SetWorkspaceExpiry(ctx context.Context, slug string, at *time.Time) (*Workspace, error) {
	return p.updateWorkspace(ctx, slug, liveStatuses, `expires_at = $2`, at)
}

func (p *Postgres) SetWorkspaceProtected(ctx context.Context, slug string, on bool) (*Workspace, error) {
	return p.updateWorkspace(ctx, slug, liveStatuses, `protected = $2`, on)
}

// RefreshWorkspaceActivity takes the latest of: the time recorded, the
// time the caller saw, the last time a person of the workspace was seen,
// a session was used, a token was used, or a project woke for a visitor.
// GREATEST skips NULLs, so a source with nothing to say says nothing; the
// creation time is the floor.
func (p *Postgres) RefreshWorkspaceActivity(ctx context.Context, slug string, seen time.Time) (*Workspace, error) {
	var seenArg *time.Time
	if !seen.IsZero() {
		seenArg = &seen
	}
	return p.updateWorkspace(ctx, slug, liveStatuses, `last_activity_at = GREATEST(
		last_activity_at, created_at, $2::timestamptz,
		(SELECT max(last_seen_at) FROM identities WHERE identities.workspace_id::text = workspaces.id::text),
		(SELECT max(last_seen_at) FROM sessions WHERE sessions.workspace_id::text = workspaces.id::text),
		(SELECT max(last_used_at) FROM api_tokens WHERE api_tokens.workspace_id::text = workspaces.id::text),
		(SELECT max(at) FROM sleep_events WHERE sleep_events.workspace_id::text = workspaces.id::text AND event = 'wake'))`, seenArg)
}

func (p *Postgres) BeginWorkspaceDeletion(ctx context.Context, slug string) (*Workspace, error) {
	w, err := p.workspaceBySlugOrID(ctx, slug)
	if err != nil {
		return nil, err
	}
	if w.Status == WorkspaceDeleting {
		return w, nil
	}
	raw, _ := json.Marshal(WorkspaceDeletion{StartedAt: time.Now().UTC()})
	return p.updateWorkspace(ctx, slug, liveStatuses, `status = '`+WorkspaceDeleting+`', deletion = $2`, raw)
}

func (p *Postgres) SetWorkspaceDeletion(ctx context.Context, slug string, d WorkspaceDeletion) (*Workspace, error) {
	raw, _ := json.Marshal(d)
	return p.updateWorkspace(ctx, slug, `('`+WorkspaceDeleting+`', '`+WorkspaceDeleted+`')`, `deletion = $2`, raw)
}

// TombstoneWorkspace removes what belongs to the workspace in one
// transaction and leaves the row as a tombstone. The ledger (usage,
// invoices, costs, sleep events) and the plan history stay with the row;
// the hosts stay until the scrub so the names are taken.
func (p *Postgres) TombstoneWorkspace(ctx context.Context, slug string) (*Workspace, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	w, err := scanWorkspace(tx.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE slug = $1 OR id::text = $1 FOR UPDATE`, slug))
	if err != nil {
		return nil, err
	}
	if w.Status == WorkspaceDeleted {
		return w, nil
	}
	owners := []string{}
	rows, err := tx.Query(ctx, `SELECT email FROM memberships WHERE workspace_id = $1 AND role = $2 ORDER BY email`, w.ID, WorkspaceRoleOwner)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			rows.Close()
			return nil, err
		}
		owners = append(owners, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Dependents first, so no foreign key stands in the way.
	for _, table := range []string{"sessions", "oauth_codes", "oauth_tokens", "oauth_clients", "api_tokens", "invitations", "grants", "teams", "memberships", "domain_claims", "identities", "github_installations", "cli_devices"} {
		if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE workspace_id::text = $1`, w.ID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE projects SET deleted_at = now(), updated_at = now() WHERE workspace_id::text = $1 AND deleted_at IS NULL`, w.ID); err != nil {
		return nil, err
	}
	d := w.Deletion
	if d == nil {
		d = &WorkspaceDeletion{StartedAt: time.Now().UTC()}
	}
	now := time.Now().UTC()
	d.FinishedAt = &now
	deletion, _ := json.Marshal(d)
	ownersRaw, _ := json.Marshal(owners)
	if _, err := tx.Exec(ctx, `UPDATE workspaces SET status = $2, deleted_at = now(), deletion = $3, tombstone_owners = $4, owner_invite_pending = '', creation_fingerprint = '', readiness = '{}', ready_at = NULL, updated_at = now() WHERE id = $1`,
		w.ID, WorkspaceDeleted, deletion, ownersRaw); err != nil {
		return nil, err
	}
	out, err := scanWorkspace(tx.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE id = $1`, w.ID))
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func (p *Postgres) ScrubWorkspace(ctx context.Context, slug string) (*Workspace, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	w, err := scanWorkspace(tx.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE slug = $1 OR id::text = $1 FOR UPDATE`, slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if w.Status != WorkspaceDeleted {
		return nil, ErrConflict
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workspace_hosts WHERE workspace_id::text = $1`, w.ID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE workspaces SET slug = $2, address = '', tombstone_owners = '[]', updated_at = now() WHERE id = $1`, w.ID, ScrubbedSlug(w.ID)); err != nil {
		return nil, err
	}
	out, err := scanWorkspace(tx.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE id = $1`, w.ID))
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}
