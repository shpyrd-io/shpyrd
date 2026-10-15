package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
	"github.com/shpyrd-io/shpyrd/pkg/pgdump"
)

// runDumps is `shpyrd-server dumps`: one night of a shared Postgres
// server's dumps (storage plan, step 3). The CronJob the controller
// renders in the server's namespace runs it inside the CloudNativePG
// image, for pg_dump: PG* in the environment name the server and the dump
// role, the bucket's Secret (BUCKET, AWS_*) the target, SHPYRD_DUMPS_KEEP
// how many copies of each database stay.
func runDumps(logger *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	conn, err := pgx.Connect(ctx, "")
	if err != nil {
		return fmt.Errorf("connect to the server: %w", err)
	}
	defer conn.Close(ctx)
	s3, err := objectstore.NewS3(os.Getenv("AWS_ENDPOINT_URL"), os.Getenv("AWS_REGION"), os.Getenv("BUCKET"), "", os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY"))
	if err != nil {
		return fmt.Errorf("the dumps bucket: %w", err)
	}
	keep := 7
	if v, err := strconv.Atoi(os.Getenv("SHPYRD_DUMPS_KEEP")); err == nil && v >= 0 {
		keep = v
	}
	run := pgdump.Run{Source: pgdump.Postgres{Conn: conn}, Objects: pgdump.Bucket{Client: s3.Client, Name: s3.Bucket}, Keep: keep, Log: logger}
	rep, err := run.Do(ctx)
	logger.Info("dumps done", "dumped", len(rep.Dumped), "failed", len(rep.Failed), "removed", rep.Removed)
	return err
}

// copySelf is `shpyrd-server copy-self DIR`: this binary, as shpyrd-server
// in DIR. An init container runs it to put the platform's static binary
// into a pod of another image (the dumps pod is the CloudNativePG image,
// for pg_dump).
func copySelf(dir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	src, err := os.Open(exe)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(filepath.Join(dir, "shpyrd-server"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}
