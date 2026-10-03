package projectarchive

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Explicit opt-in: starts disposable PostgreSQL servers with Unix sockets
// only. It never connects to an existing server or reads PG* credentials.
func localPostgres(t *testing.T) PostgreSQL {
	t.Helper()
	if os.Getenv("SHPYRD_PROJECT_POSTGRES_TEST") != "1" {
		t.Skip("set SHPYRD_PROJECT_POSTGRES_TEST=1 to run disposable real PostgreSQL tests")
	}
	for _, binary := range []string{"initdb", "pg_ctl", "psql", "pg_dump", "pg_restore"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := os.MkdirTemp("/tmp", "shpyrd-pg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "PG") {
			env = append(env, entry)
		}
	}
	env = append(env, "PGHOST="+dir, "PGPORT=5432", "PGUSER=postgres", "PGDATABASE=postgres", "PGSSLMODE=disable")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", args[0], err, out)
		}
	}
	data := filepath.Join(dir, "data")
	run("initdb", "-D", data, "-U", "postgres", "-A", "trust", "--no-locale", "--encoding=UTF8")
	t.Cleanup(func() {
		cmd := exec.Command("pg_ctl", "-D", data, "-m", "immediate", "-w", "stop")
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("stop disposable server: %v %s", err, out)
		}
	})
	run("pg_ctl", "-D", data, "-l", filepath.Join(dir, "server.log"), "-o", "-c listen_addresses='' -c unix_socket_directories="+dir, "-w", "start")
	p := PostgreSQL{Exec: func(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Env = env
		cmd.Stdin = stdin
		cmd.Stdout = stdout
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w: %s", args[0], err, stderr.String())
		}
		return nil
	}}
	if _, err := p.query(context.Background(), "CREATE ROLE app LOGIN"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.query(context.Background(), "CREATE DATABASE app OWNER app"); err != nil {
		t.Fatal(err)
	}
	return p
}

func appSQL(t *testing.T, p PostgreSQL, sql string) string {
	t.Helper()
	var out bytes.Buffer
	if err := p.Exec(context.Background(), []string{"psql", "-X", "-v", "ON_ERROR_STOP=1", "-qAt", "-U", "postgres", "-d", "app", "-c", sql}, nil, &out); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out.String())
}

func TestPostgresRealRestoreAndRollback(t *testing.T) {
	p := localPostgres(t)
	ctx := context.Background()
	if err := p.Check(ctx); err != nil {
		t.Fatal(err)
	}
	appSQL(t, p, "SET ROLE app; CREATE TABLE records(id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, payload text); INSERT INTO records(payload) SELECT 'original-' || n FROM generate_series(1,20) n")
	limit, err := p.ConnectionLimit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Fence(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.Exec(ctx, []string{"psql", "-X", "-U", "app", "-d", "app", "-c", "SELECT 1"}, nil, io.Discard); err == nil {
		t.Fatal("application can connect while fenced")
	}
	var dump bytes.Buffer
	if err := p.Export(ctx, &dump); err != nil {
		t.Fatal(err)
	}
	appSQL(t, p, "INSERT INTO records(payload) VALUES ('after-backup'), ('after-backup-2')")
	if err := p.Stage(ctx, testOperation, bytes.NewReader(dump.Bytes())); err != nil {
		t.Fatal(err)
	}
	if got := appSQL(t, p, "SELECT count(*) FROM records"); got != "22" {
		t.Fatalf("stage changed original: %s", got)
	}
	if err := p.Commit(ctx, testOperation); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit(ctx, testOperation); err != nil {
		t.Fatalf("commit retry: %v", err)
	}
	if got := appSQL(t, p, "SELECT count(*) FROM records"); got != "20" {
		t.Fatalf("restored rows: %s", got)
	}
	if got := appSQL(t, p, "INSERT INTO records(payload) VALUES ('sequence-restored') RETURNING id"); got != "21" {
		t.Fatalf("restored sequence: %s", got)
	}
	if got := appSQL(t, p, "SELECT tableowner FROM pg_tables WHERE tablename = 'records'"); got != "app" {
		t.Fatalf("table ownership: %s", got)
	}
	if err := p.Rollback(ctx, testOperation); err != nil {
		t.Fatal(err)
	}
	if err := p.Rollback(ctx, testOperation); err != nil {
		t.Fatalf("rollback retry: %v", err)
	}
	if got := appSQL(t, p, "SELECT count(*) FROM records"); got != "22" {
		t.Fatalf("rollback rows: %s", got)
	}
	if err := p.Finish(ctx, testOperation); err != nil {
		t.Fatal(err)
	}
	if err := p.Unfence(ctx, limit); err != nil {
		t.Fatal(err)
	}
	if err := p.Exec(ctx, []string{"psql", "-X", "-U", "app", "-d", "app", "-c", "SELECT count(*) FROM records"}, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRealCorruptStageAndAdditionalDatabase(t *testing.T) {
	p := localPostgres(t)
	ctx := context.Background()
	appSQL(t, p, "SET ROLE app; CREATE TABLE important(payload text); INSERT INTO important VALUES ('intact')")
	if err := p.Fence(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.Stage(ctx, testOperation, strings.NewReader("not a postgres dump")); err == nil {
		t.Fatal("accepted invalid dump")
	}
	if got := appSQL(t, p, "SELECT payload FROM important"); got != "intact" {
		t.Fatal("corrupt stage changed data")
	}
	if err := p.Rollback(ctx, testOperation); err != nil {
		t.Fatal(err)
	}
	if err := p.Finish(ctx, testOperation); err != nil {
		t.Fatal(err)
	}
	if _, err := p.query(ctx, "CREATE DATABASE additional OWNER app"); err != nil {
		t.Fatal(err)
	}
	if err := p.Check(ctx); err == nil {
		t.Fatal("would silently omit an additional database")
	}
}
