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
	if _, err := tx.Exec(ctx, `INSERT INTO workspaces (id, slug, name, address, status, settings) VALUES ($1, $2, $3, $4, $5, $6)`,
		id, w.Slug, w.Name, address, status, settings); err != nil {
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

// ---- billing (RFC-0075) -------------------------------------------------------

func (p *Postgres) CreatePlan(ctx context.Context, pl Plan) (*Plan, error) {
	if pl.Currency == "" {
		pl.Currency = "USD"
	}
	var out Plan
	err := p.pool.QueryRow(ctx, `INSERT INTO plans (name, cpu_hour, memory_gib_hour, storage_gib_month, egress_gib, min_monthly, currency, effective_from)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id, name, cpu_hour, memory_gib_hour, storage_gib_month, egress_gib, min_monthly, currency, effective_from, created_at`,
		pl.Name, pl.CPUHour, pl.MemoryGiBHour, pl.StorageGiBMonth, pl.EgressGiB, pl.MinMonthly, pl.Currency, pl.EffectiveFrom).
		Scan(&out.ID, &out.Name, &out.CPUHour, &out.MemoryGiBHour, &out.StorageGiBMonth, &out.EgressGiB, &out.MinMonthly, &out.Currency, &out.EffectiveFrom, &out.CreatedAt)
	if isUnique(err) {
		return nil, ErrConflict
	}
	return &out, err
}
func (p *Postgres) ListPlans(ctx context.Context) ([]Plan, error) {
	rows, err := p.pool.Query(ctx, `SELECT id, name, cpu_hour, memory_gib_hour, storage_gib_month, egress_gib, min_monthly, currency, effective_from, created_at FROM plans ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Plan
	for rows.Next() {
		var pl Plan
		if err := rows.Scan(&pl.ID, &pl.Name, &pl.CPUHour, &pl.MemoryGiBHour, &pl.StorageGiBMonth, &pl.EgressGiB, &pl.MinMonthly, &pl.Currency, &pl.EffectiveFrom, &pl.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, pl)
	}
	return out, rows.Err()
}
func (p *Postgres) GetPlan(ctx context.Context, nameOrID string) (*Plan, error) {
	var pl Plan
	err := p.pool.QueryRow(ctx, `SELECT id, name, cpu_hour, memory_gib_hour, storage_gib_month, egress_gib, min_monthly, currency, effective_from, created_at FROM plans WHERE id::text = $1 OR name = $1`, nameOrID).
		Scan(&pl.ID, &pl.Name, &pl.CPUHour, &pl.MemoryGiBHour, &pl.StorageGiBMonth, &pl.EgressGiB, &pl.MinMonthly, &pl.Currency, &pl.EffectiveFrom, &pl.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &pl, err
}
func (p *Postgres) AssignPlan(ctx context.Context, ws, nameOrID string) (*WorkspacePlan, error) {
	plan, err := p.GetPlan(ctx, nameOrID)
	if err != nil {
		return nil, err
	}
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `UPDATE workspace_plans SET ends_at = now() WHERE workspace_id = $1 AND ends_at IS NULL`, wsID); err != nil {
		return nil, err
	}
	var wp WorkspacePlan
	err = tx.QueryRow(ctx, `INSERT INTO workspace_plans (workspace_id, plan_id) VALUES ($1,$2) RETURNING id, workspace_id, plan_id, starts_at`, wsID, plan.ID).
		Scan(&wp.ID, &wp.WorkspaceID, &wp.PlanID, &wp.StartsAt)
	if err != nil {
		return nil, err
	}
	wp.PlanName = plan.Name
	return &wp, tx.Commit(ctx)
}
func (p *Postgres) WorkspacePlan(ctx context.Context, ws string) (*WorkspacePlan, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	var wp WorkspacePlan
	err = p.pool.QueryRow(ctx, `SELECT wp.id, wp.workspace_id, wp.plan_id, pl.name, wp.starts_at, wp.ends_at FROM workspace_plans wp JOIN plans pl ON pl.id = wp.plan_id WHERE wp.workspace_id = $1 AND wp.ends_at IS NULL`, wsID).
		Scan(&wp.ID, &wp.WorkspaceID, &wp.PlanID, &wp.PlanName, &wp.StartsAt, &wp.EndsAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &wp, err
}
func (p *Postgres) WorkspacePlanHistory(ctx context.Context, ws string) ([]WorkspacePlan, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT wp.id, wp.workspace_id, wp.plan_id, pl.name, wp.starts_at, wp.ends_at FROM workspace_plans wp JOIN plans pl ON pl.id = wp.plan_id WHERE wp.workspace_id = $1 ORDER BY wp.starts_at DESC`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkspacePlan
	for rows.Next() {
		var wp WorkspacePlan
		if err := rows.Scan(&wp.ID, &wp.WorkspaceID, &wp.PlanID, &wp.PlanName, &wp.StartsAt, &wp.EndsAt); err != nil {
			return nil, err
		}
		out = append(out, wp)
	}
	return out, rows.Err()
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
func (p *Postgres) UpsertInvoiceLine(ctx context.Context, line InvoiceLine) error {
	wsID, err := p.wsID(ctx, p.pool, strings.Split(line.WorkspaceID, "/")[0]) // accepts slug or id
	if err != nil {
		wsID = line.WorkspaceID
	} // already an ID
	_, err = p.pool.Exec(ctx, `INSERT INTO invoice_lines (workspace_id, period_start, period_end, component, metric, quantity, unit, unit_price, gross_amount, plan_id, quality, revision, finalized)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,'')::uuid,$11,$12,$13)
		ON CONFLICT (id) DO UPDATE SET quantity=EXCLUDED.quantity, gross_amount=EXCLUDED.gross_amount, finalized=EXCLUDED.finalized`,
		wsID, line.PeriodStart, line.PeriodEnd, line.Component, line.Metric, line.Quantity, line.Unit, line.UnitPrice, line.GrossAmount, line.PlanID, line.Quality, line.Revision, line.Finalized)
	return err
}
func (p *Postgres) QueryInvoiceLines(ctx context.Context, ws string, from, to time.Time, finalized *bool) ([]InvoiceLine, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	q := `SELECT id::text, workspace_id::text, period_start, period_end, component, metric, quantity, unit, unit_price, gross_amount, COALESCE(plan_id::text,''), quality, revision, finalized, created_at FROM invoice_lines WHERE workspace_id=$1 AND period_start<$3 AND period_end>$2`
	args := []any{wsID, from, to}
	if finalized != nil {
		q += ` AND finalized=$4`
		args = append(args, *finalized)
	}
	rows, err := p.pool.Query(ctx, q+` ORDER BY period_start`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InvoiceLine
	for rows.Next() {
		var l InvoiceLine
		if err := rows.Scan(&l.ID, &l.WorkspaceID, &l.PeriodStart, &l.PeriodEnd, &l.Component, &l.Metric, &l.Quantity, &l.Unit, &l.UnitPrice, &l.GrossAmount, &l.PlanID, &l.Quality, &l.Revision, &l.Finalized, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
func (p *Postgres) WriteCOGSBucket(ctx context.Context, b COGSBucket) error {
	wsID, err := p.wsID(ctx, p.pool, b.WorkspaceID)
	if err != nil {
		wsID = b.WorkspaceID
	}
	_, err = p.pool.Exec(ctx, `INSERT INTO cogs_buckets (workspace_id, project, period_start, period_end, cpu_cost, memory_cost, storage_cost, network_cost, shared_cost, idle_cost, total_cost, currency, allocation_policy, quality)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (workspace_id, project, period_start) DO UPDATE SET cpu_cost=EXCLUDED.cpu_cost, memory_cost=EXCLUDED.memory_cost, storage_cost=EXCLUDED.storage_cost, network_cost=EXCLUDED.network_cost, shared_cost=EXCLUDED.shared_cost, idle_cost=EXCLUDED.idle_cost, total_cost=EXCLUDED.total_cost, quality=EXCLUDED.quality`,
		wsID, b.Project, b.PeriodStart, b.PeriodEnd, b.CPUCost, b.MemoryCost, b.StorageCost, b.NetworkCost, b.SharedCost, b.IdleCost, b.TotalCost, b.Currency, b.AllocationPolicy, b.Quality)
	return err
}
func (p *Postgres) QueryCOGSBuckets(ctx context.Context, ws string, from, to time.Time) ([]COGSBucket, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT workspace_id::text, project, period_start, period_end, cpu_cost, memory_cost, storage_cost, network_cost, shared_cost, idle_cost, total_cost, currency, allocation_policy, quality FROM cogs_buckets WHERE workspace_id=$1 AND period_start<$3 AND period_end>$2 ORDER BY period_start`, wsID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []COGSBucket
	for rows.Next() {
		var b COGSBucket
		if err := rows.Scan(&b.WorkspaceID, &b.Project, &b.PeriodStart, &b.PeriodEnd, &b.CPUCost, &b.MemoryCost, &b.StorageCost, &b.NetworkCost, &b.SharedCost, &b.IdleCost, &b.TotalCost, &b.Currency, &b.AllocationPolicy, &b.Quality); err != nil {
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

var _ Store = (*Postgres)(nil)
var _ Store = (*Memory)(nil)
