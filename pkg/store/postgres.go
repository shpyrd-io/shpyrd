package store

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Postgres is the Store on a PostgreSQL database: the in-cluster
// control-plane-db component, or a managed database (SHPYRD_DATABASE_URL).
type Postgres struct {
	pool *pgxpool.Pool
}

// Open connects; the URL is a libpq/pgx connection string.
func Open(ctx context.Context, url string) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("database url: %w", err)
	}
	cfg.MaxConns = 8
	cfg.MinConns = 1
	cfg.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: %w", err)
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() { p.pool.Close() }

// Migrate brings the schema to the current version with golang-migrate:
// versioned up/down files embedded from migrations/, applied under the
// tool's advisory lock so two servers starting together do not race. An
// install migrated by the runner shpyrd had before v0.9.11 (a
// schema_migrations table of file names) is bridged once: its applied
// files are counted and the tool is told that version. The implicit
// workspace and its built-in team are seeded afterwards.
func (p *Postgres) Migrate(ctx context.Context, def DefaultWorkspaceSpec) error {
	if def.Slug == "" {
		def.Slug = DefaultWorkspace
	}
	if def.Name == "" {
		def.Name = def.Slug
	}
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(7245891)`); err != nil {
		conn.Release()
		return err
	}
	err = bridgeLegacyMigrations(ctx, conn)
	_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(7245891)`)
	conn.Release()
	if err != nil {
		return err
	}

	m, close, err := p.migrator()
	if err != nil {
		return err
	}
	defer close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate: %w", err)
	}

	// The operator's default workspace (RFC-0078), with an address of its
	// own (RFC-0080). An install from before two doors has the row without
	// an address: it receives the one the installer derived.
	if _, err := p.pool.Exec(ctx, `INSERT INTO workspaces (id, slug, name, address, owner) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (slug) DO NOTHING`, newID(), def.Slug, def.Name, def.Address, WorkspaceOwnerOperator); err != nil {
		return err
	}
	if def.Address != "" {
		if _, err := p.pool.Exec(ctx, `UPDATE workspaces SET address = $1, updated_at = now() WHERE slug = $2 AND address = ''`, def.Address, def.Slug); err != nil {
			return err
		}
	}
	if _, err := p.pool.Exec(ctx, `UPDATE workspaces SET owner = $1 WHERE slug = $2 AND owner <> $1`, WorkspaceOwnerOperator, def.Slug); err != nil {
		return err
	}
	// Its built-in team.
	if _, err = p.pool.Exec(ctx, `INSERT INTO teams (id, workspace_id, name, description, kind)
		SELECT $1, id, $2, 'Everyone who has signed in', 'everyone' FROM workspaces WHERE slug = $3
		ON CONFLICT (workspace_id, name) DO UPDATE SET kind = 'everyone'`, newID(), TeamEveryone, def.Slug); err != nil {
		return err
	}
	// Record it as the default unless the operator already chose another
	// (PATCH /api/cluster/settings, RFC-0078).
	var existing string
	err = p.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, SettingDefaultWorkspaceID).Scan(&existing)
	if errors.Is(err, pgx.ErrNoRows) || existing == "" {
		_, err = p.pool.Exec(ctx, `INSERT INTO settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`, SettingDefaultWorkspaceID, def.Slug)
	} else {
		err = nil
	}
	return err
}

// SchemaVersion is the migration version the database is at and whether
// a migration was interrupted (dirty), for the operator.
func (p *Postgres) SchemaVersion() (version uint, dirty bool, err error) {
	m, close, err := p.migrator()
	if err != nil {
		return 0, false, err
	}
	defer close()
	version, dirty, err = m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	return version, dirty, err
}

// migrator builds the golang-migrate instance over the embedded files and
// a database/sql handle on the pool. The driver keeps one connection of
// the pool for itself; the returned close gives it back (leaving it out
// would drain the pool a connection per call).
func (p *Postgres) migrator() (*migrate.Migrate, func(), error) {
	src, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		return nil, nil, err
	}
	db := stdlib.OpenDBFromPool(p.pool)
	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	m, err := migrate.NewWithInstance("iofs", src, "pgx5", driver)
	if err != nil {
		_ = driver.Close()
		db.Close()
		return nil, nil, err
	}
	return m, func() {
		_, _ = m.Close() // closes the source and the driver's connection
		db.Close()
	}, nil
}

// legacyMigration matches the file names the old runner recorded
// (0001_init.sql ...).
var legacyMigration = regexp.MustCompile(`^0*(\d+)_`)

// bridgeLegacyMigrations converts the old runner's record (file names)
// into golang-migrate's (a version and a dirty flag, in a table of the
// same name): the highest numbered file applied becomes the version, in
// one transaction, so a crash between the two leaves either record whole.
// Nothing happens on a fresh database or one already bridged.
func bridgeLegacyMigrations(ctx context.Context, conn *pgxpool.Conn) error {
	var hasName bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'schema_migrations' AND column_name = 'name')`).Scan(&hasName); err != nil {
		return err
	}
	if !hasName {
		return nil
	}
	rows, err := conn.Query(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return err
	}
	version := 0
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		if m := legacyMigration.FindStringSubmatch(name); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > version {
				version = n
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `DROP TABLE schema_migrations`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE schema_migrations (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL)`); err != nil {
		return err
	}
	if version > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, dirty) VALUES ($1, false)`, version); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// notFoundOnBadID turns Postgres' complaint about a malformed uuid (an id
// taken from a URL) into ErrNotFound: no row can have that id.
func notFoundOnBadID(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
		return ErrNotFound
	}
	return err
}

func (p *Postgres) wsID(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, slug string) (string, error) {
	// A slug, or the ID itself (the metering loop reads IDs off namespace
	// labels since RFC-0076; older callers pass slugs).
	var id string
	err := q.QueryRow(ctx, `SELECT id FROM workspaces WHERE slug = $1 OR id::text = $1`, slug).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

const workspaceColumns = `id, slug, name, address, status, owner, settings, created_at, updated_at, readiness, ready_at, owner_invite_pending`

func scanWorkspace(row pgx.Row) (*Workspace, error) {
	var w Workspace
	var settings, readiness []byte
	err := row.Scan(&w.ID, &w.Slug, &w.Name, &w.Address, &w.Status, &w.Owner, &settings, &w.CreatedAt, &w.UpdatedAt, &readiness, &w.ReadyAt, &w.OwnerInvitePending)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(settings, &w.Settings)
	// '{}' is the column's default: no look yet.
	if len(readiness) > 2 {
		var r WorkspaceReadiness
		if json.Unmarshal(readiness, &r) == nil {
			w.Readiness = &r
		}
	}
	return &w, nil
}

func (p *Postgres) Workspace(ctx context.Context, slug string) (*Workspace, error) {
	return scanWorkspace(p.pool.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE slug = $1`, slug))
}

func (p *Postgres) WorkspaceByAddress(ctx context.Context, address string) (*Workspace, error) {
	address = strings.ToLower(strings.TrimSpace(address))
	if address == "" {
		return nil, ErrNotFound
	}
	return scanWorkspace(p.pool.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE address = $1`, address))
}

func (p *Postgres) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+workspaceColumns+` FROM workspaces ORDER BY created_at, slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

func (p *Postgres) SetWorkspaceOwner(ctx context.Context, slug, owner string) (*Workspace, error) {
	return scanWorkspace(p.pool.QueryRow(ctx, `UPDATE workspaces SET owner=$2, updated_at=now() WHERE slug=$1 RETURNING `+workspaceColumns, slug, owner))
}

func (p *Postgres) CreateWorkspace(ctx context.Context, w Workspace) (*Workspace, error) {
	if w.Owner == "" {
		w.Owner = WorkspaceOwnerCustomer
	}
	// The slug is a hostname label and a namespace part: the store is the
	// last line, whoever the caller is (the cloud's API, a restore).
	if err := project.ValidateWorkspaceSlug(w.Slug); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	settings, _ := json.Marshal(w.Settings)
	status := w.Status
	if status == "" {
		status = WorkspaceActive
	}
	address := strings.ToLower(strings.TrimSpace(w.Address))
	if address != "" {
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace_hosts WHERE host = $1)`, address).Scan(&taken); err != nil {
			return nil, err
		}
		if taken {
			return nil, ErrConflict // a custom domain or a moved address of another workspace
		}
	}
	id := newID()
	if _, err := tx.Exec(ctx, `INSERT INTO workspaces (id, slug, name, address, status, settings, owner) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, w.Slug, w.Name, address, status, settings, w.Owner); err != nil {
		if isUnique(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO teams (id, workspace_id, name, description, kind) VALUES ($1, $2, $3, 'Everyone who has signed in', 'everyone')`,
		newID(), id, TeamEveryone); err != nil {
		return nil, err
	}
	out, err := scanWorkspace(tx.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE id = $1`, id))
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func (p *Postgres) SetWorkspaceStatus(ctx context.Context, slug, status string) (*Workspace, error) {
	tag, err := p.pool.Exec(ctx, `UPDATE workspaces SET status = $2, updated_at = now() WHERE slug = $1`, slug, status)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return p.Workspace(ctx, slug)
}

func (p *Postgres) SetWorkspaceReadiness(ctx context.Context, slug string, r WorkspaceReadiness) (*Workspace, error) {
	raw, _ := json.Marshal(r)
	// ready_at is written once, the first time the door answers.
	tag, err := p.pool.Exec(ctx, `UPDATE workspaces SET readiness = $2, ready_at = CASE WHEN $3 AND ready_at IS NULL THEN now() ELSE ready_at END WHERE slug = $1`, slug, raw, r.Ready)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return p.Workspace(ctx, slug)
}

func (p *Postgres) SetWorkspaceOwnerInvitePending(ctx context.Context, slug, email string) (*Workspace, error) {
	tag, err := p.pool.Exec(ctx, `UPDATE workspaces SET owner_invite_pending = $2 WHERE slug = $1`, slug, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return p.Workspace(ctx, slug)
}

func (p *Postgres) UpdateWorkspaceSettings(ctx context.Context, slug string, settings WorkspaceSettings) (*Workspace, error) {
	raw, _ := json.Marshal(settings)
	tag, err := p.pool.Exec(ctx, `UPDATE workspaces SET settings = ($2::jsonb - 'internalExposure') || CASE WHEN settings ? 'internalExposure' THEN jsonb_build_object('internalExposure', settings->'internalExposure') ELSE '{}'::jsonb END, updated_at = now() WHERE slug = $1`, slug, raw)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return p.Workspace(ctx, slug)
}

const claimColumns = `id, workspace_id, domain, token, connector, verified_at, created_at`

func scanClaim(row pgx.Row) (*DomainClaim, error) {
	var d DomainClaim
	if err := row.Scan(&d.ID, &d.WorkspaceID, &d.Domain, &d.Token, &d.Connector, &d.VerifiedAt, &d.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &d, nil
}

func (p *Postgres) ListDomainClaims(ctx context.Context, ws string) ([]DomainClaim, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT `+claimColumns+` FROM domain_claims WHERE workspace_id = $1 ORDER BY domain`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DomainClaim
	for rows.Next() {
		d, err := scanClaim(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (p *Postgres) PutDomainClaim(ctx context.Context, ws, domain, connector string) (*DomainClaim, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	return scanClaim(p.pool.QueryRow(ctx, `INSERT INTO domain_claims (id, workspace_id, domain, token, connector) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (workspace_id, domain) DO UPDATE SET connector = EXCLUDED.connector
		RETURNING `+claimColumns, newID(), wsID, domain, newID(), connector))
}

func (p *Postgres) MarkDomainVerified(ctx context.Context, ws, domain string, at time.Time) (*DomainClaim, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	return scanClaim(p.pool.QueryRow(ctx, `UPDATE domain_claims SET verified_at = $3 WHERE workspace_id = $1 AND domain = $2 RETURNING `+claimColumns, wsID, strings.ToLower(domain), at))
}

func (p *Postgres) DeleteDomainClaim(ctx context.Context, ws, domain string) error {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx, `DELETE FROM domain_claims WHERE workspace_id = $1 AND domain = $2`, wsID, strings.ToLower(domain))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) UpdateWorkspace(ctx context.Context, slug, name string) (*Workspace, error) {
	tag, err := p.pool.Exec(ctx, `UPDATE workspaces SET name = $2, updated_at = now() WHERE slug = $1`, slug, name)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return p.Workspace(ctx, slug)
}

func (p *Postgres) TouchIdentity(ctx context.Context, ws string, id Identity) (*Identity, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	email := strings.ToLower(strings.TrimSpace(id.Email))
	realm := id.Realm
	if realm == "" {
		realm = "workspace"
	}
	groups, _ := json.Marshal(id.Groups)
	if id.Groups == nil {
		groups = nil // keep the stored value
	}
	var out Identity
	var groupsRaw []byte
	err = p.pool.QueryRow(ctx, `
		INSERT INTO identities (id, workspace_id, realm, email, name, provider, groups)
		VALUES ($1, $2, $3, $4, $5, $6, COALESCE($7::jsonb, '[]'::jsonb))
		ON CONFLICT (workspace_id, realm, email) DO UPDATE SET
			name = CASE WHEN EXCLUDED.name <> '' THEN EXCLUDED.name ELSE identities.name END,
			provider = CASE WHEN EXCLUDED.provider <> '' THEN EXCLUDED.provider ELSE identities.provider END,
			groups = COALESCE($7::jsonb, identities.groups),
			last_seen_at = now()
		RETURNING id, workspace_id, realm, email, name, provider, groups, status, first_seen_at, last_seen_at`,
		newID(), wsID, realm, email, id.Name, id.Provider, groups).
		Scan(&out.ID, &out.WorkspaceID, &out.Realm, &out.Email, &out.Name, &out.Provider, &groupsRaw, &out.Status, &out.FirstSeenAt, &out.LastSeenAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(groupsRaw, &out.Groups)
	return &out, nil
}

func (p *Postgres) ListIdentities(ctx context.Context, ws string) ([]Identity, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT id, workspace_id, realm, email, name, provider, groups, status, first_seen_at, last_seen_at FROM identities WHERE workspace_id = $1 ORDER BY email`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Identity
	for rows.Next() {
		var it Identity
		var groupsRaw []byte
		if err := rows.Scan(&it.ID, &it.WorkspaceID, &it.Realm, &it.Email, &it.Name, &it.Provider, &groupsRaw, &it.Status, &it.FirstSeenAt, &it.LastSeenAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(groupsRaw, &it.Groups)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (p *Postgres) GetIdentity(ctx context.Context, ws, email string) (*Identity, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	var it Identity
	var groupsRaw []byte
	err = p.pool.QueryRow(ctx, `SELECT id, workspace_id, realm, email, name, provider, groups, status, first_seen_at, last_seen_at FROM identities WHERE workspace_id = $1 AND lower(email) = lower($2)`, wsID, email).
		Scan(&it.ID, &it.WorkspaceID, &it.Realm, &it.Email, &it.Name, &it.Provider, &groupsRaw, &it.Status, &it.FirstSeenAt, &it.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(groupsRaw, &it.Groups)
	return &it, nil
}

func (p *Postgres) SetIdentityStatus(ctx context.Context, ws, email, status string) (*Identity, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	var out Identity
	var groupsRaw []byte
	err = p.pool.QueryRow(ctx, `UPDATE identities SET status = $3 WHERE workspace_id = $1 AND email = $2
		RETURNING id, workspace_id, realm, email, name, provider, groups, status, first_seen_at, last_seen_at`, wsID, strings.ToLower(email), status).
		Scan(&out.ID, &out.WorkspaceID, &out.Realm, &out.Email, &out.Name, &out.Provider, &groupsRaw, &out.Status, &out.FirstSeenAt, &out.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(groupsRaw, &out.Groups)
	return &out, nil
}

func (p *Postgres) DeleteIdentity(ctx context.Context, ws, email string) error {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx, `DELETE FROM identities WHERE workspace_id = $1 AND email = $2`, wsID, strings.ToLower(email))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const teamColumns = `t.id, t.workspace_id, t.name, t.description, t.members, t.groups, t.platform_role, t.kind, t.created_at, t.updated_at`

func scanTeam(row pgx.Row) (*Team, error) {
	var t Team
	var members, groups []byte
	var kind string
	if err := row.Scan(&t.ID, &t.WorkspaceID, &t.Name, &t.Description, &members, &groups, &t.PlatformRole, &kind, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal(members, &t.Members)
	_ = json.Unmarshal(groups, &t.Groups)
	if t.Members == nil {
		t.Members = []string{}
	}
	if t.Groups == nil {
		t.Groups = []string{}
	}
	t.Everyone = kind == "everyone"
	return &t, nil
}

func (p *Postgres) ListTeams(ctx context.Context, ws string) ([]Team, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT `+teamColumns+` FROM teams t WHERE t.workspace_id = $1 ORDER BY t.name`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Team
	for rows.Next() {
		t, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (p *Postgres) GetTeam(ctx context.Context, ws, name string) (*Team, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	return scanTeam(p.pool.QueryRow(ctx, `SELECT `+teamColumns+` FROM teams t WHERE t.workspace_id = $1 AND t.name = $2`, wsID, name))
}

func (p *Postgres) PutTeam(ctx context.Context, ws string, t Team) (*Team, bool, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, false, err
	}
	if t.Name == TeamEveryone {
		return nil, false, ErrBuiltIn
	}
	members, _ := json.Marshal(normalizeEmails(t.Members))
	groups, _ := json.Marshal(dedupe(t.Groups))
	var inserted bool
	row := p.pool.QueryRow(ctx, `
		INSERT INTO teams (id, workspace_id, name, description, members, groups, platform_role)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (workspace_id, name) DO UPDATE SET
			description = EXCLUDED.description, members = EXCLUDED.members, groups = EXCLUDED.groups,
			platform_role = EXCLUDED.platform_role, updated_at = now()
		RETURNING `+strings.ReplaceAll(teamColumns, "t.", "")+`, (xmax = 0) AS inserted`,
		newID(), wsID, t.Name, t.Description, members, groups, t.PlatformRole)
	var out Team
	var m, g []byte
	var kind string
	if err := row.Scan(&out.ID, &out.WorkspaceID, &out.Name, &out.Description, &m, &g, &out.PlatformRole, &kind, &out.CreatedAt, &out.UpdatedAt, &inserted); err != nil {
		return nil, false, err
	}
	_ = json.Unmarshal(m, &out.Members)
	_ = json.Unmarshal(g, &out.Groups)
	return &out, inserted, nil
}

func (p *Postgres) DeleteTeam(ctx context.Context, ws, name string) error {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return err
	}
	if name == TeamEveryone {
		return ErrBuiltIn
	}
	// Grants of the team go with it (ON DELETE CASCADE).
	tag, err := p.pool.Exec(ctx, `DELETE FROM teams WHERE workspace_id = $1 AND name = $2`, wsID, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const grantSelect = `SELECT g.id, g.workspace_id, g.project, g.role, g.user_email, COALESCE(t.name, ''), g.created_at
	FROM grants g LEFT JOIN teams t ON t.id = g.team_id`

func (p *Postgres) listGrants(ctx context.Context, where string, args ...any) ([]Grant, error) {
	rows, err := p.pool.Query(ctx, grantSelect+" WHERE "+where+" ORDER BY g.project, g.role, g.user_email, t.name", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Grant
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.ID, &g.WorkspaceID, &g.Project, &g.Role, &g.User, &g.Team, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (p *Postgres) ListGrants(ctx context.Context, ws string) ([]Grant, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	return p.listGrants(ctx, "g.workspace_id = $1", wsID)
}

func (p *Postgres) ListProjectGrants(ctx context.Context, ws, project string) ([]Grant, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	return p.listGrants(ctx, "g.workspace_id = $1 AND g.project = $2", wsID, project)
}

func (p *Postgres) AddGrant(ctx context.Context, ws string, g Grant) (*Grant, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	var teamID *string
	if g.Team != "" {
		var id string
		err := p.pool.QueryRow(ctx, `SELECT id FROM teams WHERE workspace_id = $1 AND name = $2`, wsID, g.Team).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		teamID = &id
	}
	id := newID()
	_, err = p.pool.Exec(ctx, `INSERT INTO grants (id, workspace_id, project, role, user_email, team_id) VALUES ($1, $2, $3, $4, $5, $6)`,
		id, wsID, g.Project, g.Role, strings.ToLower(strings.TrimSpace(g.User)), teamID)
	if err != nil {
		if isUnique(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	out, err := p.listGrants(ctx, "g.id = $1", id)
	if err != nil || len(out) == 0 {
		return nil, fmt.Errorf("grant %s: %v", id, err)
	}
	return &out[0], nil
}

func (p *Postgres) DeleteGrant(ctx context.Context, ws, id string) error {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx, `DELETE FROM grants WHERE workspace_id = $1 AND id = $2`, wsID, id)
	if err != nil {
		return notFoundOnBadID(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) DeleteProjectGrants(ctx context.Context, ws, project string) error {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `DELETE FROM grants WHERE workspace_id = $1 AND project = $2`, wsID, project)
	return err
}

func (p *Postgres) Export(ctx context.Context, ws string) (*Dump, error) {
	w, err := p.Workspace(ctx, ws)
	if err != nil {
		return nil, err
	}
	ids, err := p.ListIdentities(ctx, ws)
	if err != nil {
		return nil, err
	}
	teams, err := p.ListTeams(ctx, ws)
	if err != nil {
		return nil, err
	}
	grants, err := p.ListGrants(ctx, ws)
	if err != nil {
		return nil, err
	}
	domains, err := p.ListDomainClaims(ctx, ws)
	if err != nil {
		return nil, err
	}
	memberships, err := p.ListMemberships(ctx, ws)
	if err != nil {
		return nil, err
	}
	hosts, err := p.ListWorkspaceHosts(ctx, ws)
	if err != nil {
		return nil, err
	}
	return &Dump{Version: DumpVersion, Workspace: *w, Identities: ids, Teams: teams, Grants: grants, Domains: domains, Memberships: memberships, Hosts: hosts}, nil
}

func (p *Postgres) Import(ctx context.Context, ws string, d *Dump, overwrite bool) (*ImportResult, error) {
	return importDump(ctx, p, ws, d, overwrite)
}

// ---- sessions and codes ------------------------------------------------------

func (p *Postgres) PutSession(ctx context.Context, ws string, sess Session) error {
	// A console session has no workspace (RFC-0080).
	var wsID *string
	realm := RealmConsole
	if ws != "" {
		id, err := p.wsID(ctx, p.pool, ws)
		if err != nil {
			return err
		}
		wsID, realm = &id, RealmWorkspace
	}
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now()
	}
	if sess.LastSeenAt.IsZero() {
		sess.LastSeenAt = sess.CreatedAt
	}
	identity := sess.Identity
	if len(identity) == 0 {
		identity = json.RawMessage("{}")
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO sessions (id, workspace_id, realm, identity, csrf, id_token, created_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET identity = EXCLUDED.identity, csrf = EXCLUDED.csrf, id_token = EXCLUDED.id_token, last_seen_at = EXCLUDED.last_seen_at`,
		sess.ID, wsID, realm, identity, sess.CSRF, sess.IDToken, sess.CreatedAt, sess.LastSeenAt)
	return err
}

func (p *Postgres) GetSession(ctx context.Context, id string) (*Session, error) {
	var s Session
	err := p.pool.QueryRow(ctx, `SELECT id, COALESCE(workspace_id::text, ''), realm, identity, csrf, id_token, created_at, last_seen_at FROM sessions WHERE id = $1`, id).
		Scan(&s.ID, &s.WorkspaceID, &s.Realm, &s.Identity, &s.CSRF, &s.IDToken, &s.CreatedAt, &s.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (p *Postgres) TouchSession(ctx context.Context, id string, at time.Time) error {
	tag, err := p.pool.Exec(ctx, `UPDATE sessions SET last_seen_at = $2 WHERE id = $1 AND last_seen_at < $2`, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sessions WHERE id = $1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
	}
	return nil
}

func (p *Postgres) DeleteSession(ctx context.Context, id string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return err
}

func (p *Postgres) PurgeSessions(ctx context.Context, createdBefore, seenBefore time.Time) (int, error) {
	tag, err := p.pool.Exec(ctx, `DELETE FROM sessions WHERE created_at < $1 OR last_seen_at < $2`, createdBefore, seenBefore)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (p *Postgres) CountSessions(ctx context.Context, ws string) (int, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return 0, err
	}
	var n int
	err = p.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE workspace_id = $1`, wsID).Scan(&n)
	return n, err
}

func (p *Postgres) PutCode(ctx context.Context, c Code) error {
	if _, err := p.pool.Exec(ctx, `DELETE FROM edge_codes WHERE expires_at < now()`); err != nil {
		return err
	}
	claims := c.Claims
	if len(claims) == 0 {
		claims = json.RawMessage("{}")
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO edge_codes (code, host, claims, expires_at) VALUES ($1, $2, $3, $4)`, c.Code, c.Host, claims, c.ExpiresAt)
	return err
}

func (p *Postgres) TakeCode(ctx context.Context, code string) (*Code, error) {
	var c Code
	err := p.pool.QueryRow(ctx, `DELETE FROM edge_codes WHERE code = $1 RETURNING code, host, claims, expires_at`, code).
		Scan(&c.Code, &c.Host, &c.Claims, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(c.ExpiresAt) {
		return nil, ErrNotFound
	}
	return &c, nil
}

func (p *Postgres) PeekCode(ctx context.Context, code string) (*Code, error) {
	var c Code
	err := p.pool.QueryRow(ctx, `SELECT code, host, claims, expires_at FROM edge_codes WHERE code = $1`, code).
		Scan(&c.Code, &c.Host, &c.Claims, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(c.ExpiresAt) {
		return nil, ErrNotFound
	}
	return &c, nil
}

// ---- API tokens (RFC-0031) -------------------------------------------------

func (p *Postgres) CreateToken(ctx context.Context, ws string, t APIToken, hash string) (*APIToken, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	roles, _ := json.Marshal(t.ProjectRoles)
	kind := t.Kind
	if kind == "" {
		kind = TokenKindToken
	}
	row := p.pool.QueryRow(ctx, `INSERT INTO api_tokens (id, workspace_id, name, owner_email, hash, platform_role, project_roles, expires_at, kind)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, workspace_id, name, owner_email, platform_role, project_roles, created_at, expires_at, last_used_at, kind`,
		newID(), wsID, t.Name, strings.ToLower(t.OwnerEmail), hash, t.PlatformRole, roles, t.ExpiresAt, kind)
	out, err := scanToken(row)
	if isUnique(err) {
		return nil, ErrConflict
	}
	return out, err
}

func scanToken(row pgx.Row) (*APIToken, error) {
	var t APIToken
	var roles []byte
	if err := row.Scan(&t.ID, &t.WorkspaceID, &t.Name, &t.OwnerEmail, &t.PlatformRole, &roles, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt, &t.Kind); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if t.Kind == TokenKindToken {
		t.Kind = ""
	}
	_ = json.Unmarshal(roles, &t.ProjectRoles)
	return &t, nil
}

func (p *Postgres) LookupToken(ctx context.Context, hash string) (*APIToken, error) {
	row := p.pool.QueryRow(ctx, `SELECT id, workspace_id, name, owner_email, platform_role, project_roles, created_at, expires_at, last_used_at, kind
		FROM api_tokens WHERE hash = $1 AND (expires_at IS NULL OR expires_at > now())`, hash)
	t, err := scanToken(row)
	if err != nil || t == nil {
		return t, err
	}
	// Update last_used_at at most once a minute (best effort).
	if t.LastUsedAt == nil || time.Since(*t.LastUsedAt) > time.Minute {
		now := time.Now()
		_, _ = p.pool.Exec(ctx, `UPDATE api_tokens SET last_used_at = $2::timestamptz WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < $2::timestamptz - interval '1 minute')`, t.ID, now)
		t.LastUsedAt = &now
	}
	return t, nil
}

func (p *Postgres) ListTokens(ctx context.Context, ws, ownerEmail string) ([]APIToken, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	q := `SELECT id, workspace_id, name, owner_email, platform_role, project_roles, created_at, expires_at, last_used_at, kind FROM api_tokens WHERE workspace_id = $1`
	args := []any{wsID}
	if ownerEmail != "" {
		q += ` AND lower(owner_email) = lower($2)`
		args = append(args, ownerEmail)
	}
	q += ` ORDER BY name`
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		if t != nil {
			out = append(out, *t)
		}
	}
	return out, rows.Err()
}

func (p *Postgres) DeleteToken(ctx context.Context, ws, id string) error {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx, `DELETE FROM api_tokens WHERE workspace_id = $1 AND id = $2`, wsID, id)
	if err != nil {
		return notFoundOnBadID(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- workspace roles and invitations (RFC-0033) ------------------------------

const membershipColumns = `id, workspace_id, email, role, created_at, updated_at`

func scanMembership(row pgx.Row) (*Membership, error) {
	var mb Membership
	if err := row.Scan(&mb.ID, &mb.WorkspaceID, &mb.Email, &mb.Role, &mb.CreatedAt, &mb.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &mb, nil
}

func (p *Postgres) ListMemberships(ctx context.Context, ws string) ([]Membership, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT `+membershipColumns+` FROM memberships WHERE workspace_id = $1 ORDER BY email`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Membership
	for rows.Next() {
		mb, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *mb)
	}
	return out, rows.Err()
}

func (p *Postgres) PutMembership(ctx context.Context, ws, email, role string) (*Membership, error) {
	if !ValidWorkspaceRole(role) {
		return nil, fmt.Errorf("role %q is not a workspace role", role)
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, errors.New("email is required")
	}
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	return scanMembership(p.pool.QueryRow(ctx, `INSERT INTO memberships (id, workspace_id, email, role) VALUES ($1, $2, $3, $4)
		ON CONFLICT (workspace_id, email) DO UPDATE SET role = EXCLUDED.role,
			updated_at = CASE WHEN memberships.role <> EXCLUDED.role THEN now() ELSE memberships.updated_at END
		RETURNING `+membershipColumns, newID(), wsID, email, role))
}

func (p *Postgres) DeleteMembership(ctx context.Context, ws, email string) error {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx, `DELETE FROM memberships WHERE workspace_id = $1 AND email = $2`, wsID, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const invitationSelect = `SELECT i.id, i.workspace_id, i.email, i.role, COALESCE(t.name, ''), i.invited_by, i.created_at, i.expires_at
	FROM invitations i LEFT JOIN teams t ON t.id = i.team_id`

func scanInvitation(row pgx.Row) (*Invitation, error) {
	var inv Invitation
	if err := row.Scan(&inv.ID, &inv.WorkspaceID, &inv.Email, &inv.Role, &inv.Team, &inv.InvitedBy, &inv.CreatedAt, &inv.ExpiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &inv, nil
}

func (p *Postgres) ListInvitations(ctx context.Context, ws string) ([]Invitation, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, invitationSelect+` WHERE i.workspace_id = $1 ORDER BY i.created_at, i.email`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invitation
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *inv)
	}
	return out, rows.Err()
}

func (p *Postgres) CreateInvitation(ctx context.Context, ws string, inv Invitation, tokenHash string) (*Invitation, error) {
	if !ValidWorkspaceRole(inv.Role) {
		return nil, fmt.Errorf("role %q is not a workspace role", inv.Role)
	}
	inv.Email = strings.ToLower(strings.TrimSpace(inv.Email))
	if inv.Email == "" || tokenHash == "" {
		return nil, errors.New("email and token are required")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	wsID, err := p.wsID(ctx, tx, ws)
	if err != nil {
		return nil, err
	}
	var teamID *string
	if inv.Team != "" {
		var id string
		err := tx.QueryRow(ctx, `SELECT id FROM teams WHERE workspace_id = $1 AND name = $2`, wsID, inv.Team).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		teamID = &id
	}
	expires := inv.ExpiresAt
	if expires.IsZero() {
		expires = time.Now().Add(7 * 24 * time.Hour)
	}
	// A re-invite replaces the pending invitation: one link per person.
	if _, err := tx.Exec(ctx, `DELETE FROM invitations WHERE workspace_id = $1 AND email = $2`, wsID, inv.Email); err != nil {
		return nil, err
	}
	id := newID()
	if _, err := tx.Exec(ctx, `INSERT INTO invitations (id, workspace_id, email, role, team_id, token_hash, invited_by, expires_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, wsID, inv.Email, inv.Role, teamID, tokenHash, inv.InvitedBy, expires); err != nil {
		if isUnique(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	out, err := scanInvitation(tx.QueryRow(ctx, invitationSelect+` WHERE i.id = $1`, id))
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func (p *Postgres) InvitationByToken(ctx context.Context, tokenHash string) (*Invitation, error) {
	if tokenHash == "" {
		return nil, ErrNotFound
	}
	return scanInvitation(p.pool.QueryRow(ctx, invitationSelect+` WHERE i.token_hash = $1`, tokenHash))
}

func (p *Postgres) DeleteInvitation(ctx context.Context, ws, id string) error {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx, `DELETE FROM invitations WHERE workspace_id = $1 AND id = $2`, wsID, id)
	if err != nil {
		return notFoundOnBadID(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- workspace hosts (RFC-0033 names) -------------------------------------------

const hostColumns = `id, workspace_id, host, kind, is_primary, token, verified_at, expires_at, created_at`

func scanHost(row pgx.Row) (*WorkspaceHost, error) {
	var h WorkspaceHost
	if err := row.Scan(&h.ID, &h.WorkspaceID, &h.Host, &h.Kind, &h.Primary, &h.Token, &h.VerifiedAt, &h.ExpiresAt, &h.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &h, nil
}

func (p *Postgres) ListWorkspaceHosts(ctx context.Context, ws string) ([]WorkspaceHost, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT `+hostColumns+` FROM workspace_hosts WHERE workspace_id = $1 ORDER BY host`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkspaceHost
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *h)
	}
	return out, rows.Err()
}

// addressTaken says a host is some workspace's address, other than ws.
func (p *Postgres) addressTaken(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, host, exceptWorkspaceID string) (bool, error) {
	var taken bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspaces WHERE address = $1 AND id <> $2)`, host, exceptWorkspaceID).Scan(&taken)
	return taken, err
}

func (p *Postgres) PutWorkspaceHost(ctx context.Context, ws string, h WorkspaceHost) (*WorkspaceHost, error) {
	h.Host = normalizeHost(h.Host)
	if h.Host == "" {
		return nil, errors.New("host is required")
	}
	if h.Kind != HostCustom && h.Kind != HostMoved {
		return nil, fmt.Errorf("host kind %q is not custom or moved", h.Kind)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	wsID, err := p.wsID(ctx, tx, ws)
	if err != nil {
		return nil, err
	}
	var ownAddress string
	if err := tx.QueryRow(ctx, `SELECT address FROM workspaces WHERE id = $1`, wsID).Scan(&ownAddress); err != nil {
		return nil, err
	}
	if ownAddress == h.Host {
		return nil, ErrConflict
	}
	if taken, err := p.addressTaken(ctx, tx, h.Host, wsID); err != nil {
		return nil, err
	} else if taken {
		return nil, ErrConflict
	}
	if h.Primary {
		if _, err := tx.Exec(ctx, `UPDATE workspace_hosts SET is_primary = false WHERE workspace_id = $1`, wsID); err != nil {
			return nil, err
		}
	}
	token := h.Token
	if token == "" && h.Kind == HostCustom {
		token = newID()
	}
	out, err := scanHost(tx.QueryRow(ctx, `INSERT INTO workspace_hosts (id, workspace_id, host, kind, is_primary, token, verified_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (host) DO UPDATE SET is_primary = EXCLUDED.is_primary, verified_at = EXCLUDED.verified_at, expires_at = EXCLUDED.expires_at
		WHERE workspace_hosts.workspace_id = EXCLUDED.workspace_id
		RETURNING `+hostColumns, newID(), wsID, h.Host, h.Kind, h.Primary, token, h.VerifiedAt, h.ExpiresAt))
	if errors.Is(err, ErrNotFound) {
		return nil, ErrConflict // the host belongs to another workspace
	}
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func (p *Postgres) DeleteWorkspaceHost(ctx context.Context, ws, host string) error {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx, `DELETE FROM workspace_hosts WHERE workspace_id = $1 AND host = $2`, wsID, normalizeHost(host))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) WorkspaceByHost(ctx context.Context, host string) (*Workspace, *WorkspaceHost, error) {
	h, err := scanHost(p.pool.QueryRow(ctx, `SELECT `+hostColumns+` FROM workspace_hosts WHERE host = $1`, normalizeHost(host)))
	if err != nil {
		return nil, nil, err
	}
	w, err := scanWorkspace(p.pool.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE id = $1`, h.WorkspaceID))
	if err != nil {
		return nil, nil, err
	}
	return w, h, nil
}

func (p *Postgres) UpdateWorkspaceAddress(ctx context.Context, slug, address string) (*Workspace, error) {
	address = normalizeHost(address)
	if address == "" {
		return nil, errors.New("address is required")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	wsID, err := p.wsID(ctx, tx, slug)
	if err != nil {
		return nil, err
	}
	var hostTaken bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace_hosts WHERE host = $1)`, address).Scan(&hostTaken); err != nil {
		return nil, err
	}
	if hostTaken {
		return nil, ErrConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE workspaces SET address = $2, updated_at = now() WHERE id = $1`, wsID, address); err != nil {
		if isUnique(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	out, err := scanWorkspace(tx.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE id = $1`, wsID))
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

// ---- OAuth 2.1 server (RFC-0032) ----------------------------------------------

const oclientColumns = `id, workspace_id, client_id, secret_hash, name, redirect_uris, created_at`

func scanOAuthClient(row pgx.Row) (*OAuthClient, error) {
	var c OAuthClient
	var uris []byte
	if err := row.Scan(&c.ID, &c.WorkspaceID, &c.ClientID, &c.SecretHash, &c.Name, &uris, &c.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal(uris, &c.RedirectURIs)
	if c.RedirectURIs == nil {
		c.RedirectURIs = []string{}
	}
	return &c, nil
}

func (p *Postgres) CreateOAuthClient(ctx context.Context, ws string, c OAuthClient) (*OAuthClient, error) {
	if c.ClientID == "" {
		return nil, errors.New("client id is required")
	}
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	uris, _ := json.Marshal(dedupe(c.RedirectURIs))
	out, err := scanOAuthClient(p.pool.QueryRow(ctx, `INSERT INTO oauth_clients (id, workspace_id, client_id, secret_hash, name, redirect_uris) VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+oclientColumns,
		newID(), wsID, c.ClientID, c.SecretHash, c.Name, uris))
	if isUnique(err) {
		return nil, ErrConflict
	}
	return out, err
}

func (p *Postgres) OAuthClientByID(ctx context.Context, clientID string) (*OAuthClient, error) {
	return scanOAuthClient(p.pool.QueryRow(ctx, `SELECT `+oclientColumns+` FROM oauth_clients WHERE client_id = $1`, clientID))
}

func (p *Postgres) PutOAuthCode(ctx context.Context, code OAuthCode) error {
	if code.Hash == "" {
		return errors.New("code hash is required")
	}
	if _, err := p.pool.Exec(ctx, `DELETE FROM oauth_codes WHERE expires_at < now()`); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO oauth_codes (hash, workspace_id, client_id, email, subject, scope, redirect_uri, code_challenge, resource, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		code.Hash, code.WorkspaceID, code.ClientID, strings.ToLower(code.Email), code.Subject, code.Scope, code.RedirectURI, code.CodeChallenge, code.Resource, code.ExpiresAt)
	return err
}

func (p *Postgres) TakeOAuthCode(ctx context.Context, hash string) (*OAuthCode, error) {
	var c OAuthCode
	err := p.pool.QueryRow(ctx, `DELETE FROM oauth_codes WHERE hash = $1 RETURNING hash, workspace_id, client_id, email, subject, scope, redirect_uri, code_challenge, resource, expires_at`, hash).
		Scan(&c.Hash, &c.WorkspaceID, &c.ClientID, &c.Email, &c.Subject, &c.Scope, &c.RedirectURI, &c.CodeChallenge, &c.Resource, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(c.ExpiresAt) {
		return nil, ErrNotFound
	}
	return &c, nil
}

const otokenSelect = `SELECT t.id, t.workspace_id, t.client_id, COALESCE(c.name, ''), t.email, t.scope, t.hash, t.created_at, t.expires_at, t.last_used_at
	FROM oauth_tokens t LEFT JOIN oauth_clients c ON c.client_id = t.client_id`

func scanOAuthToken(row pgx.Row) (*OAuthToken, error) {
	var t OAuthToken
	if err := row.Scan(&t.ID, &t.WorkspaceID, &t.ClientID, &t.ClientName, &t.Email, &t.Scope, &t.Hash, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (p *Postgres) CreateOAuthToken(ctx context.Context, ws string, t OAuthToken) (*OAuthToken, error) {
	if t.Hash == "" {
		return nil, errors.New("token hash is required")
	}
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	id := newID()
	if _, err := p.pool.Exec(ctx, `INSERT INTO oauth_tokens (id, workspace_id, client_id, email, scope, hash, expires_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, wsID, t.ClientID, strings.ToLower(t.Email), t.Scope, t.Hash, t.ExpiresAt); err != nil {
		return nil, err
	}
	return scanOAuthToken(p.pool.QueryRow(ctx, otokenSelect+` WHERE t.id = $1`, id))
}

func (p *Postgres) OAuthTokenByHash(ctx context.Context, hash string) (*OAuthToken, error) {
	return scanOAuthToken(p.pool.QueryRow(ctx, otokenSelect+` WHERE t.hash = $1 AND t.expires_at > now()`, hash))
}

func (p *Postgres) RotateOAuthToken(ctx context.Context, id, newHash string, expiresAt time.Time) error {
	tag, err := p.pool.Exec(ctx, `UPDATE oauth_tokens SET hash = $2, expires_at = $3, last_used_at = now() WHERE id = $1`, id, newHash, expiresAt)
	if err != nil {
		return notFoundOnBadID(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) ListOAuthTokens(ctx context.Context, ws, email string) ([]OAuthToken, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	q, args := otokenSelect+` WHERE t.workspace_id = $1`, []any{wsID}
	if email != "" {
		q += ` AND t.email = $2`
		args = append(args, strings.ToLower(email))
	}
	rows, err := p.pool.Query(ctx, q+` ORDER BY t.created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OAuthToken
	for rows.Next() {
		t, err := scanOAuthToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (p *Postgres) DeleteOAuthToken(ctx context.Context, ws, id string) error {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx, `DELETE FROM oauth_tokens WHERE workspace_id = $1 AND id = $2`, wsID, id)
	if err != nil {
		return notFoundOnBadID(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- usage (RFC-0075) ---------------------------------------------------------

func isInvalidUUID(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}
func (p *Postgres) WriteBuckets(ctx context.Context, buckets []UsageBucket) error {
	if len(buckets) == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// WorkspaceID is a slug or an id (the metering loop reads slugs off
	// namespace labels); resolve once per distinct value.
	ids := map[string]string{}
	for _, b := range buckets {
		wsID, ok := ids[b.WorkspaceID]
		if !ok {
			resolved, err := p.wsID(ctx, tx, b.WorkspaceID)
			if err != nil {
				if !errors.Is(err, ErrNotFound) {
					return err
				}
				continue // workspace gone (deleted between listing and write): skip its buckets
			}
			wsID = resolved
			ids[b.WorkspaceID] = wsID
		}
		labels := "{}"
		if len(b.Labels) > 0 {
			if raw, e := json.Marshal(b.Labels); e == nil {
				labels = string(raw)
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO usage_buckets (workspace_id, project, component, metric, period_start, period_end, quantity, unit, quality, revision, source, labels)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb) ON CONFLICT DO NOTHING`,
			wsID, b.Project, b.Component, b.Metric, b.PeriodStart, b.PeriodEnd, b.Quantity, b.Unit, b.Quality, b.Revision, b.Source, labels); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// RollupHourly folds closed hours of usage_buckets into usage_hourly in one
// transaction per call. Quality: missing if any bucket in the hour is
// missing, else partial if any is partial, else complete. period_end of the
// hourly row is the hour's end regardless of how many buckets existed.
func (p *Postgres) RollupHourly(ctx context.Context, before time.Time) (int, error) {
	cutoff := before.UTC().Truncate(time.Hour)
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tag, err := tx.Exec(ctx, `
		INSERT INTO usage_hourly (workspace_id, project, component, metric, period_start, period_end, quantity, unit, quality, revision, source, labels)
		SELECT workspace_id, project, component, metric,
		       date_trunc('hour', period_start) AS hour_start,
		       date_trunc('hour', period_start) + interval '1 hour',
		       CASE WHEN bool_or(quantity IS NULL) THEN NULL ELSE sum(quantity) END,
		       min(unit),
		       CASE WHEN bool_or(quality = 'missing') THEN 'missing'
		            WHEN bool_or(quality = 'partial') OR count(*) < 12 THEN 'partial'
		            ELSE 'complete' END,
		       max(revision), min(source), '{}'::jsonb
		FROM usage_buckets
		WHERE period_start < $1
		GROUP BY workspace_id, project, component, metric, date_trunc('hour', period_start)
		ON CONFLICT (workspace_id, project, component, metric, period_start) DO NOTHING`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("rollup insert: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM usage_buckets WHERE period_start < $1`, cutoff); err != nil {
		return 0, fmt.Errorf("rollup delete: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (p *Postgres) QueryBuckets(ctx context.Context, ws, project string, from, to time.Time) ([]UsageBucket, error) {
	args := []any{ws, from, to}
	filter := "AND b.project = $4"
	if project != "" {
		args = append(args, project)
	} else {
		filter = ""
	}
	q := `SELECT workspace_id::text, project, component, metric, period_start, period_end, quantity, unit, quality, revision, source FROM usage_buckets b WHERE b.workspace_id = (SELECT id FROM workspaces WHERE slug = $1) AND b.period_start < $3 AND b.period_end > $2 ` + filter + `
	UNION ALL
	SELECT workspace_id::text, project, component, metric, period_start, period_end, quantity, unit, quality, revision, source FROM usage_hourly b WHERE b.workspace_id = (SELECT id FROM workspaces WHERE slug = $1) AND b.period_start < $3 AND b.period_end > $2 ` + filter + ` ORDER BY period_start`
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageBucket
	for rows.Next() {
		var b UsageBucket
		if err := rows.Scan(&b.WorkspaceID, &b.Project, &b.Component, &b.Metric, &b.PeriodStart, &b.PeriodEnd, &b.Quantity, &b.Unit, &b.Quality, &b.Revision, &b.Source); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (p *Postgres) WriteSleepEvent(ctx context.Context, e SleepEvent) error {
	wsID, err := p.wsID(ctx, p.pool, e.WorkspaceID)
	if err != nil {
		wsID = e.WorkspaceID
	}
	at := e.At
	if at.IsZero() {
		at = time.Now()
	}
	_, err = p.pool.Exec(ctx, `INSERT INTO sleep_events (workspace_id, project, component, event, at, duration_seconds, reason) VALUES ($1,$2,$3,$4,$5,$6,$7)`, wsID, e.Project, e.Component, e.Event, at, e.DurationSeconds, e.Reason)
	return err
}
func (p *Postgres) QuerySleepEvents(ctx context.Context, ws, project string, from, to time.Time) ([]SleepEvent, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	q := `SELECT id::text, workspace_id::text, project, component, event, at, duration_seconds, reason FROM sleep_events WHERE workspace_id=$1 AND at>=$2 AND at<$3`
	args := []any{wsID, from, to}
	if project != "" {
		q += ` AND project=$4`
		args = append(args, project)
	}
	rows, err := p.pool.Query(ctx, q+` ORDER BY at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SleepEvent
	for rows.Next() {
		var e SleepEvent
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.Project, &e.Component, &e.Event, &e.At, &e.DurationSeconds, &e.Reason); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- grants project-id rekey (RFC-0076 v0.9.47) ----------------------------------

// RekeyGrantsToIDs rewrites grants.project and api_tokens.project_roles keys
// from project slugs to short base36 IDs. Idempotent: rows whose project
// value is already a base36 ID (25 lowercase [0-9a-z] chars) are skipped.
// Rows whose slug has no live entry in the projects table are left unchanged.
func (p *Postgres) RekeyGrantsToIDs(ctx context.Context) (int, error) {
	rows, err := p.pool.Query(ctx, `SELECT workspace_id::text, slug, id::text FROM projects WHERE deleted_at IS NULL`)
	if err != nil {
		return 0, fmt.Errorf("RekeyGrantsToIDs: list projects: %w", err)
	}
	type proj struct{ wsID, slug, short string }
	var projects []proj
	for rows.Next() {
		var pr proj
		var rawID string
		if err := rows.Scan(&pr.wsID, &pr.slug, &rawID); err != nil {
			rows.Close()
			return 0, err
		}
		pr.short = ids.Short(rawID)
		if pr.short != pr.slug { // only rows that still carry the slug
			projects = append(projects, pr)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(projects) == 0 {
		return 0, nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	moved := 0
	for _, pr := range projects {
		// grants.project slug → short id
		tag, err := tx.Exec(ctx,
			`UPDATE grants SET project=$3 WHERE workspace_id=$1 AND project=$2`,
			pr.wsID, pr.slug, pr.short)
		if err != nil {
			return 0, fmt.Errorf("RekeyGrantsToIDs grants %s/%s: %w", pr.wsID, pr.slug, err)
		}
		moved += int(tag.RowsAffected())
		// api_tokens.project_roles: replace the slug key with the short id.
		// Only tokens that have the slug key but not yet the id key.
		tag, err = tx.Exec(ctx, `
			UPDATE api_tokens
			SET    project_roles =
				       (project_roles - $2::text)
				    || jsonb_build_object($3::text, project_roles->$2::text)
			WHERE  workspace_id = $1
			  AND  project_roles ? $2
			  AND  NOT (project_roles ? $3)`,
			pr.wsID, pr.slug, pr.short)
		if err != nil {
			return 0, fmt.Errorf("RekeyGrantsToIDs tokens %s/%s: %w", pr.wsID, pr.slug, err)
		}
		moved += int(tag.RowsAffected())
	}
	return moved, tx.Commit(ctx)
}

// ---- projects (RFC-0076) ------------------------------------------------------

const projectColumns = `id::text, workspace_id::text, slug, name, namespace, created_at, updated_at, deleted_at`

func scanProject(row pgx.Row) (*Project, error) {
	var pr Project
	err := row.Scan(&pr.ID, &pr.WorkspaceID, &pr.Slug, &pr.Name, &pr.Namespace, &pr.CreatedAt, &pr.UpdatedAt, &pr.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &pr, nil
}

func (p *Postgres) UpsertProject(ctx context.Context, pr Project) (*Project, error) {
	if pr.ID == "" || pr.WorkspaceID == "" || pr.Slug == "" {
		return nil, errors.New("project needs id, workspace and slug")
	}
	wsID, err := p.wsID(ctx, p.pool, pr.WorkspaceID)
	if err != nil {
		return nil, err
	}
	row := p.pool.QueryRow(ctx, `INSERT INTO projects (id, workspace_id, slug, name, namespace)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET slug = EXCLUDED.slug, name = EXCLUDED.name,
			namespace = CASE WHEN EXCLUDED.namespace = '' THEN projects.namespace ELSE EXCLUDED.namespace END,
			deleted_at = NULL, updated_at = now()
		RETURNING `+projectColumns, pr.ID, wsID, pr.Slug, pr.Name, pr.Namespace)
	out, err := scanProject(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrConflict // another live project has this slug
		}
		return nil, err
	}
	return out, nil
}

func (p *Postgres) DeleteProject(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE projects SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) ListProjects(ctx context.Context, ws string, withDeleted bool) ([]Project, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	q := `SELECT ` + projectColumns + ` FROM projects WHERE workspace_id = $1`
	if !withDeleted {
		q += ` AND deleted_at IS NULL`
	}
	rows, err := p.pool.Query(ctx, q+` ORDER BY created_at, id`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		pr, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *pr)
	}
	return out, rows.Err()
}

func (p *Postgres) ProjectBySlug(ctx context.Context, ws, slug string) (*Project, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	return scanProject(p.pool.QueryRow(ctx, `SELECT `+projectColumns+` FROM projects WHERE workspace_id = $1 AND slug = $2 AND deleted_at IS NULL`, wsID, slug))
}

func (p *Postgres) ProjectIcon(ctx context.Context, id string) ([]byte, string, error) {
	var data []byte
	var typ string
	err := p.pool.QueryRow(ctx, `SELECT data, type FROM project_icons WHERE project_id = $1`, id).Scan(&data, &typ)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	return data, typ, err
}

func (p *Postgres) SetProjectIcon(ctx context.Context, id string, data []byte, typ string) error {
	if len(data) == 0 {
		_, err := p.pool.Exec(ctx, `DELETE FROM project_icons WHERE project_id = $1`, id)
		return err
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO project_icons (project_id, data, type, updated_at) VALUES ($1, $2, $3, now())
		ON CONFLICT (project_id) DO UPDATE SET data = EXCLUDED.data, type = EXCLUDED.type, updated_at = now()`, id, data, typ)
	return err
}

// RekeyProject moves ledger rows from the legacy slug key to the ID key in
// one transaction: copy under the new key (rows already there win), then
// delete the old ones.
func (p *Postgres) RekeyProject(ctx context.Context, ws, slug, id string) (int, error) {
	if slug == "" || id == "" {
		return 0, errors.New("rekey needs a slug and an id")
	}
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return 0, err
	}
	short := ids.Short(id)
	if short == slug {
		return 0, nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	moved := 0
	for _, t := range []struct{ table, cols string }{
		{"usage_buckets", "workspace_id, project, component, metric, period_start, period_end, quantity, unit, quality, revision, source, labels"},
		{"usage_hourly", "workspace_id, project, component, metric, period_start, period_end, quantity, unit, quality, revision, source, labels"},
	} {
		newCols := strings.Replace(t.cols, "project,", "$3::text AS project,", 1)
		if _, err := tx.Exec(ctx, `INSERT INTO `+t.table+` (`+t.cols+`) SELECT `+newCols+` FROM `+t.table+` WHERE workspace_id = $1 AND project = $2 ON CONFLICT DO NOTHING`, wsID, slug, short); err != nil {
			return 0, fmt.Errorf("rekey %s: %w", t.table, err)
		}
		tag, err := tx.Exec(ctx, `DELETE FROM `+t.table+` WHERE workspace_id = $1 AND project = $2`, wsID, slug)
		if err != nil {
			return 0, fmt.Errorf("rekey %s: %w", t.table, err)
		}
		moved += int(tag.RowsAffected())
	}
	tag, err := tx.Exec(ctx, `UPDATE sleep_events SET project = $3 WHERE workspace_id = $1 AND project = $2`, wsID, slug, short)
	if err != nil {
		return 0, fmt.Errorf("rekey sleep_events: %w", err)
	}
	moved += int(tag.RowsAffected())
	return moved, tx.Commit(ctx)
}

// RenameProjectSlug changes the slug in projects + grants in one transaction.
func (p *Postgres) RenameProjectSlug(ctx context.Context, ws, oldSlug, newSlug string) error {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tag, err := tx.Exec(ctx, `UPDATE projects SET slug = $3, updated_at = now() WHERE workspace_id = $1 AND slug = $2 AND deleted_at IS NULL`, wsID, oldSlug, newSlug)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrConflict
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE grants SET project = $3 WHERE workspace_id = $1 AND project = $2`, wsID, oldSlug, newSlug); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---- settings (RFC-0078) -------------------------------------------------------

func (p *Postgres) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := p.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (p *Postgres) SetSetting(ctx context.Context, key, value string) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO settings (key, value) VALUES ($1,$2) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`, key, value)
	return err
}

// ---- console users -------------------------------------------------------------

func (p *Postgres) ListConsoleUsers(ctx context.Context) ([]ConsoleUser, error) {
	rows, err := p.pool.Query(ctx, `SELECT email, added_at, added_by FROM console_users ORDER BY email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConsoleUser{}
	for rows.Next() {
		var u ConsoleUser
		if err := rows.Scan(&u.Email, &u.AddedAt, &u.AddedBy); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (p *Postgres) AddConsoleUser(ctx context.Context, email, by string) (*ConsoleUser, error) {
	email = normEmail(email)
	if email == "" {
		return nil, errors.New("a console user needs an email")
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO console_users (email, added_by) VALUES ($1,$2) ON CONFLICT (email) DO NOTHING`, email, by); err != nil {
		return nil, err
	}
	var u ConsoleUser
	err := p.pool.QueryRow(ctx, `SELECT email, added_at, added_by FROM console_users WHERE email = $1`, email).Scan(&u.Email, &u.AddedAt, &u.AddedBy)
	return &u, err
}

func (p *Postgres) RemoveConsoleUser(ctx context.Context, email string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM console_users WHERE email = $1`, normEmail(email))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) IsConsoleUser(ctx context.Context, email string) (bool, error) {
	email = normEmail(email)
	if email == "" {
		return false, nil
	}
	var ok bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM console_users WHERE email = $1)`, email).Scan(&ok)
	return ok, err
}

var _ Store = (*Postgres)(nil)
var _ Store = (*Memory)(nil)

// SetWorkspaceInternalExposure atomically changes only the networking override.
func (p *Postgres) SetWorkspaceInternalExposure(ctx context.Context, slug string, enabled *bool) (*Workspace, error) {
	raw, err := json.Marshal(enabled)
	if err != nil {
		return nil, err
	}
	tag, err := p.pool.Exec(ctx, `UPDATE workspaces SET settings = CASE WHEN $2::jsonb = 'null'::jsonb THEN settings - 'internalExposure' ELSE jsonb_set(settings, '{internalExposure}', $2::jsonb) END, updated_at = now() WHERE slug = $1`, slug, raw)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return p.Workspace(ctx, slug)
}
