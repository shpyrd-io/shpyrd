package pgdump

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/minio/minio-go/v7"
)

// Getter opens a copy in the bucket.
type Getter interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// Restorer loads a dump into the database the environment names.
type Restorer interface {
	Restore(ctx context.Context, dump io.Reader) error
}

// Get opens a copy; a missing one is said at once, not at the first read.
func (b Bucket) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := b.Client.GetObject(ctx, b.Name, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, fmt.Errorf("the copy %s: %w", key, err)
	}
	return obj, nil
}

// Restore is pg_restore of a dump into the database PGDATABASE names, as
// the role the environment connects with, so what it creates is that
// role's; owners and grants in the dump are left out, and either every
// object lands or none does. The dump is written to a file first (TMPDIR:
// the Job mounts scratch space there) so its list of items can be read
// and the comments on extensions left out: the platform installs pgvector
// in every shared database, and only an extension's owner may comment it.
func (p Postgres) Restore(ctx context.Context, dump io.Reader) error {
	file, err := os.CreateTemp("", "copy-*.dump")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := io.Copy(file, dump); err != nil {
		file.Close()
		return fmt.Errorf("save the copy: %w", err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	toc, err := p.run(ctx, "pg_restore", "--list", file.Name())
	if err != nil {
		return err
	}
	list, err := os.CreateTemp("", "copy-*.list")
	if err != nil {
		return err
	}
	defer os.Remove(list.Name())
	if _, err := list.WriteString(RestoreList(toc)); err != nil {
		list.Close()
		return err
	}
	if err := list.Close(); err != nil {
		return err
	}
	_, err = p.run(ctx, "pg_restore", "--exit-on-error", "--single-transaction", "--no-owner", "--no-acl", "--use-list="+list.Name(), "--dbname="+p.database(), file.Name())
	return err
}

// run is one PostgreSQL client command with the connection environment;
// its last words are the error's.
func (p Postgres) run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = p.env()
	var out strings.Builder
	var stderr tail
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

// RestoreList is pg_restore's list of a dump's items without the comments
// on extensions, which only an extension's owner may set and which say
// nothing anyone wrote.
func RestoreList(toc string) string {
	var out strings.Builder
	sc := bufio.NewScanner(strings.NewReader(toc))
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, " COMMENT - EXTENSION ") {
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

// Restore loads one copy into the database.
func Restore(ctx context.Context, from Getter, key string, into Restorer) error {
	dump, err := from.Get(ctx, key)
	if err != nil {
		return err
	}
	defer dump.Close()
	return into.Restore(ctx, dump)
}
