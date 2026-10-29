package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
	"github.com/shpyrd-io/shpyrd/pkg/pgdump"
)

// runConvertDB is `shpyrd-server convert-db`: one database's rows moved
// from a source server to a target (storage plan, step 3), by a Job the
// controller cuts in the shared servers' namespace. The source is named by
// SOURCE_HOST, _PORT, _USER, _PASSWORD, _DATABASE and _SSLMODE; the target
// by PG*; the copy goes to the bucket the Secret names (BUCKET, AWS_*) at
// SHPYRD_CONVERT_KEY. SHPYRD_CONVERT_EXTENSIONS (comma-separated) are the
// extensions the target can hold and SHPYRD_CONVERT_CAP the most bytes the
// source may have, both for a move to a shared database;
// SHPYRD_CONVERT_FENCE_ROLE is the source's owner to pause for the copy,
// as SOURCE_USER (a superuser) can; empty when the platform fences.
func runConvertDB(logger *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	sourceEnv := map[string]string{
		"PGHOST": os.Getenv("SOURCE_HOST"), "PGPORT": firstNonEmptyStr(os.Getenv("SOURCE_PORT"), "5432"), "PGUSER": os.Getenv("SOURCE_USER"),
		"PGPASSWORD": os.Getenv("SOURCE_PASSWORD"), "PGDATABASE": os.Getenv("SOURCE_DATABASE"), "PGSSLMODE": firstNonEmptyStr(os.Getenv("SOURCE_SSLMODE"), "require"),
	}
	sourceConn, err := connectWithPatience(ctx, connString(sourceEnv), logger, "source")
	if err != nil {
		return fmt.Errorf("connect to the source: %w", err)
	}
	defer sourceConn.Close(ctx)
	// A cluster just up may refuse its owner for a few seconds: initdb made
	// the role and the operator applies the Secret's password a moment
	// later. The target is given two minutes to take the credentials.
	targetConn, err := connectWithPatience(ctx, "", logger, "target")
	if err != nil {
		return fmt.Errorf("connect to the target: %w", err)
	}
	defer targetConn.Close(ctx)
	s3, err := objectstore.NewS3(os.Getenv("AWS_ENDPOINT_URL"), os.Getenv("AWS_REGION"), os.Getenv("BUCKET"), "", os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY"))
	if err != nil {
		return fmt.Errorf("the dumps bucket: %w", err)
	}
	bucket := pgdump.Bucket{Client: s3.Client, Name: s3.Bucket}
	source := pgdump.Connected{Postgres: pgdump.Postgres{Conn: sourceConn, Env: envOf(sourceEnv), Database: sourceEnv["PGDATABASE"]}}
	target := pgdump.Connected{Postgres: pgdump.Postgres{Conn: targetConn}}
	c := pgdump.Convert{Source: source, Target: target, Objects: bucket, Getter: bucket, Key: os.Getenv("SHPYRD_CONVERT_KEY"), Log: logger}
	// The target's superuser counts the rows: a table with row-level
	// security forced hides them from the owner the copy lands as.
	if super := os.Getenv("TARGET_SUPERUSER"); super != "" {
		superEnv := map[string]string{"PGHOST": os.Getenv("PGHOST"), "PGPORT": firstNonEmptyStr(os.Getenv("PGPORT"), "5432"), "PGDATABASE": os.Getenv("PGDATABASE"), "PGSSLMODE": firstNonEmptyStr(os.Getenv("PGSSLMODE"), "require"), "PGUSER": super, "PGPASSWORD": os.Getenv("TARGET_SUPERPASSWORD")}
		superConn, err := connectWithPatience(ctx, connString(superEnv), logger, "target superuser")
		if err != nil {
			return fmt.Errorf("connect to the target as its superuser: %w", err)
		}
		defer superConn.Close(ctx)
		c.TargetCounter = pgdump.Postgres{Conn: superConn}
	}
	if list := os.Getenv("SHPYRD_CONVERT_EXTENSIONS"); list != "" {
		c.Allowed = strings.Split(list, ",")
	}
	if capText := os.Getenv("SHPYRD_CONVERT_CAP"); capText != "" {
		if c.Cap, err = strconv.ParseInt(capText, 10, 64); err != nil {
			return fmt.Errorf("SHPYRD_CONVERT_CAP %q: %w", capText, err)
		}
	}
	if role := os.Getenv("SHPYRD_CONVERT_FENCE_ROLE"); role != "" {
		c.Fence = func(ctx context.Context) error { return fenceRole(ctx, sourceConn, role, 0) }
		c.Lift = func(ctx context.Context) error { return fenceRole(ctx, sourceConn, role, -1) }
	}
	moved, err := c.Do(ctx)
	if err != nil {
		// The pod's last words, which the database's status repeats.
		_ = os.WriteFile("/dev/termination-log", []byte(err.Error()), 0o644)
		return err
	}
	logger.Info("converted", "tables", moved.Tables, "rows", moved.Rows, "bytes", moved.Bytes, "copy", c.Key)
	_ = os.WriteFile("/dev/termination-log", []byte(fmt.Sprintf("converted tables=%d rows=%d bytes=%d copy=%s", moved.Tables, moved.Rows, moved.Bytes, c.Key)), 0o644)
	return nil
}

// connectPatience is how long a server is given to take the credentials,
// and how often it is asked.
var connectPatience, connectRetry = 2 * time.Minute, 5 * time.Second

// connectWithPatience connects to a server, trying again for a while when
// it refuses the credentials or is not there yet.
func connectWithPatience(ctx context.Context, conn string, logger *slog.Logger, what string) (*pgx.Conn, error) {
	deadline := time.Now().Add(connectPatience)
	for {
		c, err := pgx.Connect(ctx, conn)
		if err == nil {
			return c, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return nil, err
		}
		logger.Info("not accepting the connection yet; trying again", "which", what, "err", err.Error())
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(connectRetry):
		}
	}
}

// fenceRole sets a role's connection limit (0 closes it, -1 opens it) and,
// closing, ends its sessions.
func fenceRole(ctx context.Context, conn *pgx.Conn, role string, limit int) error {
	if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER ROLE %s CONNECTION LIMIT %d", pgx.Identifier{role}.Sanitize(), limit)); err != nil {
		return err
	}
	if limit == 0 {
		_, err := conn.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE usename = $1 AND pid <> pg_backend_pid()", role)
		return err
	}
	return nil
}

func connString(env map[string]string) string {
	var parts []string
	for key, name := range map[string]string{"PGHOST": "host", "PGPORT": "port", "PGUSER": "user", "PGPASSWORD": "password", "PGDATABASE": "dbname", "PGSSLMODE": "sslmode"} {
		if v := env[key]; v != "" {
			parts = append(parts, name+"="+strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), " ", `\ `))
		}
	}
	return strings.Join(parts, " ")
}

// envOf is a process environment for pg_dump: this process's with the
// connection variables replaced.
func envOf(vars map[string]string) []string {
	var out []string
	for _, kv := range os.Environ() {
		if key, _, _ := strings.Cut(kv, "="); vars[key] == "" && !strings.HasPrefix(key, "PG") {
			out = append(out, kv)
		}
	}
	for k, v := range vars {
		out = append(out, k+"="+v)
	}
	return out
}
