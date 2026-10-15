package pgdump

import (
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
// object lands or none does.
func (p Postgres) Restore(ctx context.Context, dump io.Reader) error {
	cmd := exec.CommandContext(ctx, "pg_restore", "--exit-on-error", "--single-transaction", "--no-owner", "--no-acl", "--dbname="+os.Getenv("PGDATABASE"))
	cmd.Env = os.Environ()
	cmd.Stdin = dump
	var stderr tail
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_restore: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
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
