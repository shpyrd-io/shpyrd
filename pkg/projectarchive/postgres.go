package projectarchive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Command streams directly through the database container's exec endpoint.
// Callers must return the remote exit status, not merely a successful attach.
type Command func(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error

// PostgreSQL operates on the managed "app" database using the container's
// local postgres administrator. Logical exports intentionally exclude cluster
// roles, provider credentials, replication slots and physical server state.
type PostgreSQL struct{ Exec Command }

func (p PostgreSQL) query(ctx context.Context, sql string) (string, error) {
	var out bytes.Buffer
	err := p.Exec(ctx, []string{"psql", "-X", "-v", "ON_ERROR_STOP=1", "-qAt", "-U", "postgres", "-d", "postgres", "-c", sql}, nil, &out)
	return strings.TrimSpace(out.String()), err
}

// Check refuses to silently omit additional user-created databases. Each
// managed Postgres resource normally owns precisely one database named app.
func (p PostgreSQL) Check(ctx context.Context) error {
	names, err := p.query(ctx, "SELECT datname FROM pg_database WHERE NOT datistemplate AND datname <> 'postgres' ORDER BY datname")
	if err != nil {
		return err
	}
	if names != "app" {
		return fmt.Errorf("project export requires exactly the managed app database; found %q", names)
	}
	return nil
}

// Fence prevents new application connections and terminates existing writers.
// The saved limit belongs in the durable project operation before proceeding.
func (p PostgreSQL) ConnectionLimit(ctx context.Context) (int, error) {
	value, err := p.query(ctx, "SELECT datconnlimit FROM pg_database WHERE datname = 'app'")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(value)
}

func (p PostgreSQL) Fence(ctx context.Context) error {
	if _, err := p.query(ctx, "ALTER DATABASE app CONNECTION LIMIT 0"); err != nil {
		return err
	}
	return p.disconnect(ctx, "app")
}

func (p PostgreSQL) Unfence(ctx context.Context, previous int) error {
	if previous < -1 {
		return errors.New("invalid saved database connection limit")
	}
	_, err := p.query(ctx, fmt.Sprintf("ALTER DATABASE app CONNECTION LIMIT %d", previous))
	return err
}

func (p PostgreSQL) disconnect(ctx context.Context, database string) error {
	// database is either the literal app or an internally generated name.
	_, err := p.query(ctx, "SELECT pg_terminate_backend(pid, 10000) FROM pg_stat_activity WHERE datname = '"+database+"' AND pid <> pg_backend_pid()")
	return err
}

func (p PostgreSQL) Export(ctx context.Context, out io.Writer) error {
	return p.Exec(ctx, []string{"pg_dump", "--format=custom", "--no-owner", "--no-acl", "-U", "postgres", "--dbname=app"}, nil, out)
}

func databaseNames(operation string) (incoming, previous string, err error) {
	if !operationName.MatchString(operation) {
		return "", "", errors.New("invalid database operation id")
	}
	return "shpyrd_in_" + operation, "shpyrd_old_" + operation, nil
}

func (p PostgreSQL) exists(ctx context.Context, name string) (bool, error) {
	result, err := p.query(ctx, "SELECT 1 FROM pg_database WHERE datname = '"+name+"'")
	return result == "1", err
}

// Stage restores into an isolated database. All databases and volumes must
// finish staging before any one of them is committed.
func (p PostgreSQL) Stage(ctx context.Context, operation string, dump io.Reader) error {
	incoming, previous, err := databaseNames(operation)
	if err != nil {
		return err
	}
	if exists, err := p.exists(ctx, previous); err != nil {
		return err
	} else if exists {
		return errors.New("database operation already committed")
	}
	if _, err := p.query(ctx, "DROP DATABASE IF EXISTS "+incoming+" WITH (FORCE)"); err != nil {
		return err
	}
	if _, err := p.query(ctx, "CREATE DATABASE "+incoming+" OWNER app TEMPLATE template0 CONNECTION LIMIT 0"); err != nil {
		return err
	}
	// Connect as postgres (the database is fenced), then restore with the
	// managed application role. Never replay archive ownership or ACLs.
	return p.Exec(ctx, []string{"pg_restore", "--exit-on-error", "--single-transaction", "--no-owner", "--no-acl", "--role=app", "-U", "postgres", "--dbname=" + incoming}, dump, io.Discard)
}

func (p PostgreSQL) Commit(ctx context.Context, operation string) error {
	incoming, previous, err := databaseNames(operation)
	if err != nil {
		return err
	}
	old, err := p.exists(ctx, previous)
	if err != nil {
		return err
	}
	next, err := p.exists(ctx, incoming)
	if err != nil {
		return err
	}
	if old && !next {
		return nil
	} // retry after the atomic rename succeeded
	if old || !next {
		return errors.New("database is not staged for commit")
	}
	if err := p.Fence(ctx); err != nil {
		return err
	}
	if err := p.disconnect(ctx, incoming); err != nil {
		return err
	}
	_, err = p.query(ctx, "BEGIN; ALTER DATABASE app RENAME TO "+previous+"; ALTER DATABASE "+incoming+" RENAME TO app; COMMIT")
	return err
}

func (p PostgreSQL) Rollback(ctx context.Context, operation string) error {
	incoming, previous, err := databaseNames(operation)
	if err != nil {
		return err
	}
	old, err := p.exists(ctx, previous)
	if err != nil {
		return err
	}
	if !old {
		return nil
	} // no commit, or a previous rollback completed
	if exists, err := p.exists(ctx, incoming); err != nil {
		return err
	} else if exists {
		return errors.New("ambiguous database recovery state")
	}
	if err := p.Fence(ctx); err != nil {
		return err
	}
	if err := p.disconnect(ctx, previous); err != nil {
		return err
	}
	_, err = p.query(ctx, "BEGIN; ALTER DATABASE app RENAME TO "+incoming+"; ALTER DATABASE "+previous+" RENAME TO app; COMMIT")
	return err
}

// Finish must only be called after the project operation has durably recorded
// success or rollback. It discards only this operation's staging/recovery DBs.
func (p PostgreSQL) Finish(ctx context.Context, operation string) error {
	incoming, previous, err := databaseNames(operation)
	if err != nil {
		return err
	}
	for _, name := range []string{incoming, previous} {
		if _, err := p.query(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			return err
		}
	}
	return nil
}
