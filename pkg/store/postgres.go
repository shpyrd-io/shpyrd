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
func (p *Postgres) Migrate(ctx context.Context, defaultName string) error {
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

	if _, err := p.pool.Exec(ctx, `INSERT INTO workspaces (id, slug, name) VALUES ($1, $2, $3) ON CONFLICT (slug) DO NOTHING`, newID(), DefaultWorkspace, defaultName); err != nil {
		return err
	}
	// The built-in team of the implicit workspace.
	_, err = p.pool.Exec(ctx, `INSERT INTO teams (id, workspace_id, name, description, kind)
		SELECT $1, id, $2, 'Everyone who has signed in', 'everyone' FROM workspaces WHERE slug = $3
		ON CONFLICT (workspace_id, name) DO UPDATE SET kind = 'everyone'`, newID(), TeamEveryone, DefaultWorkspace)
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

func (p *Postgres) wsID(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, slug string) (string, error) {
	var id string
	err := q.QueryRow(ctx, `SELECT id FROM workspaces WHERE slug = $1`, slug).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

const workspaceColumns = `id, slug, name, address, status, settings, created_at, updated_at`

func scanWorkspace(row pgx.Row) (*Workspace, error) {
	var w Workspace
	var settings []byte
	err := row.Scan(&w.ID, &w.Slug, &w.Name, &w.Address, &w.Status, &settings, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(settings, &w.Settings)
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

func (p *Postgres) CreateWorkspace(ctx context.Context, w Workspace) (*Workspace, error) {
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
	id := newID()
	if _, err := tx.Exec(ctx, `INSERT INTO workspaces (id, slug, name, address, status, settings) VALUES ($1, $2, $3, $4, $5, $6)`,
		id, w.Slug, w.Name, strings.ToLower(strings.TrimSpace(w.Address)), status, settings); err != nil {
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

func (p *Postgres) UpdateWorkspaceSettings(ctx context.Context, slug string, settings WorkspaceSettings) (*Workspace, error) {
	raw, _ := json.Marshal(settings)
	tag, err := p.pool.Exec(ctx, `UPDATE workspaces SET settings = $2, updated_at = now() WHERE slug = $1`, slug, raw)
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
		return err
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
	return &Dump{Version: DumpVersion, Workspace: *w, Identities: ids, Teams: teams, Grants: grants, Domains: domains}, nil
}

func (p *Postgres) Import(ctx context.Context, ws string, d *Dump, overwrite bool) (*ImportResult, error) {
	return importDump(ctx, p, ws, d, overwrite)
}

// ---- sessions and codes ------------------------------------------------------

func (p *Postgres) PutSession(ctx context.Context, ws string, sess Session) error {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return err
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
	_, err = p.pool.Exec(ctx, `INSERT INTO sessions (id, workspace_id, identity, csrf, id_token, created_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET identity = EXCLUDED.identity, csrf = EXCLUDED.csrf, id_token = EXCLUDED.id_token, last_seen_at = EXCLUDED.last_seen_at`,
		sess.ID, wsID, identity, sess.CSRF, sess.IDToken, sess.CreatedAt, sess.LastSeenAt)
	return err
}

func (p *Postgres) GetSession(ctx context.Context, id string) (*Session, error) {
	var s Session
	err := p.pool.QueryRow(ctx, `SELECT id, workspace_id, identity, csrf, id_token, created_at, last_seen_at FROM sessions WHERE id = $1`, id).
		Scan(&s.ID, &s.WorkspaceID, &s.Identity, &s.CSRF, &s.IDToken, &s.CreatedAt, &s.LastSeenAt)
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

// ---- API tokens (RFC-0031) -------------------------------------------------

func (p *Postgres) CreateToken(ctx context.Context, ws string, t APIToken, hash string) (*APIToken, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	roles, _ := json.Marshal(t.ProjectRoles)
	row := p.pool.QueryRow(ctx, `INSERT INTO api_tokens (id, workspace_id, name, owner_email, hash, platform_role, project_roles, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, workspace_id, name, owner_email, platform_role, project_roles, created_at, expires_at, last_used_at`,
		newID(), wsID, t.Name, strings.ToLower(t.OwnerEmail), hash, t.PlatformRole, roles, t.ExpiresAt)
	out, err := scanToken(row)
	if isUnique(err) {
		return nil, ErrConflict
	}
	return out, err
}

func scanToken(row pgx.Row) (*APIToken, error) {
	var t APIToken
	var roles []byte
	if err := row.Scan(&t.ID, &t.WorkspaceID, &t.Name, &t.OwnerEmail, &t.PlatformRole, &roles, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(roles, &t.ProjectRoles)
	return &t, nil
}

func (p *Postgres) LookupToken(ctx context.Context, hash string) (*APIToken, error) {
	row := p.pool.QueryRow(ctx, `SELECT id, workspace_id, name, owner_email, platform_role, project_roles, created_at, expires_at, last_used_at
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
	q := `SELECT id, workspace_id, name, owner_email, platform_role, project_roles, created_at, expires_at, last_used_at FROM api_tokens WHERE workspace_id = $1`
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
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

var _ Store = (*Postgres)(nil)
var _ Store = (*Memory)(nil)
