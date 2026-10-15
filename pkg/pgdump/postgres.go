package pgdump

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
)

// Postgres is a server reached as the dump role, the connection from the
// PG* environment: pg_dump runs next to this process (the CloudNativePG
// image has it) with the same environment.
type Postgres struct{ Conn *pgx.Conn }

// Databases lists the tenants' databases the role may open: those named
// by the platform (db_<hash>) and granted CONNECT, so a database whose
// grants have not run yet is skipped, not failed.
func (p Postgres) Databases(ctx context.Context) ([]string, error) {
	rows, err := p.Conn.Query(ctx, `SELECT datname FROM pg_database WHERE datname LIKE 'db\_%' AND datallowconn AND has_database_privilege(current_user, datname, 'CONNECT') ORDER BY datname`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// Dump is pg_dump of one database in the custom format, owners and
// grants left out: a restore gives the rows to whoever restores them.
func (p Postgres) Dump(ctx context.Context, database string, w io.Writer) error {
	cmd := exec.CommandContext(ctx, "pg_dump", "--format=custom", "--no-owner", "--no-acl", "--dbname="+database)
	cmd.Env = os.Environ()
	cmd.Stdout = w
	var stderr tail
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_dump %s: %w: %s", database, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// tail keeps the last few kilobytes written to it.
type tail struct{ buf bytes.Buffer }

func (t *tail) Write(p []byte) (int, error) {
	t.buf.Write(p)
	if t.buf.Len() > 4096 {
		t.buf.Next(t.buf.Len() - 4096)
	}
	return len(p), nil
}

func (t *tail) String() string { return t.buf.String() }

// Bucket is the server's dumps bucket.
type Bucket struct {
	Client *minio.Client
	Name   string
}

// uploadPartSize bounds the memory an upload of unknown length takes:
// the client buffers one part at a time, and without a part size it sizes
// parts for a 5 TiB object (about 537 MiB each), which is the dump pod's
// whole memory limit and more (seen on dev: every dump pod OOM-killed after
// two databases). 16 MiB parts hold a dump of up to 160 GiB.
const uploadPartSize = 16 << 20

func putOptions() minio.PutObjectOptions {
	return minio.PutObjectOptions{ContentType: "application/octet-stream", PartSize: uploadPartSize}
}

// Put streams an object of unknown length: a multipart upload that is
// abandoned, not completed, when the reader fails.
func (b Bucket) Put(ctx context.Context, key string, r io.Reader) error {
	_, err := b.Client.PutObject(ctx, b.Name, key, r, -1, putOptions())
	return err
}

func (b Bucket) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for obj := range b.Client.ListObjects(ctx, b.Name, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		keys = append(keys, obj.Key)
	}
	return keys, nil
}

func (b Bucket) Remove(ctx context.Context, key string) error {
	return b.Client.RemoveObject(ctx, b.Name, key, minio.RemoveObjectOptions{})
}
