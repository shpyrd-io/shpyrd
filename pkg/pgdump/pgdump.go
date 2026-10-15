// Package pgdump takes the nightly dumps of the databases on a shared
// Postgres server (storage plan, step 3): one pg_dump per database, as the
// platform's dump role, streamed into the server's bucket; the newest few
// of each database are kept.
package pgdump

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// Source is the server: the databases the dump role may open, and a dump
// of one of them.
type Source interface {
	Databases(ctx context.Context) ([]string, error)
	Dump(ctx context.Context, database string, w io.Writer) error
}

// Objects is the bucket.
type Objects interface {
	Put(ctx context.Context, key string, r io.Reader) error
	List(ctx context.Context, prefix string) ([]string, error)
	Remove(ctx context.Context, key string) error
}

// Run is one night.
type Run struct {
	Source  Source
	Objects Objects
	// Keep is how many dumps of each database stay; 0 keeps them all.
	Keep int
	Now  func() time.Time
	Log  *slog.Logger
}

// Report says what the night did.
type Report struct {
	Dumped  []string
	Failed  map[string]error
	Removed int
}

// Key is where a dump of the database taken at that moment goes:
// <database>/<time>.dump, so one database's copies list together and
// sort by age.
func Key(database string, at time.Time) string {
	return database + "/" + at.UTC().Format("20060102T150405Z") + ".dump"
}

// Do dumps every database, each on its own: one that fails is reported
// and the rest are still taken; then that database's old copies go. The
// error names what failed.
func (r Run) Do(ctx context.Context) (Report, error) {
	rep := Report{Failed: map[string]error{}}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	logger := r.Log
	if logger == nil {
		logger = slog.Default()
	}
	dbs, err := r.Source.Databases(ctx)
	if err != nil {
		return rep, fmt.Errorf("list the databases: %w", err)
	}
	for _, db := range dbs {
		key := Key(db, now())
		if err := r.dump(ctx, db, key); err != nil {
			rep.Failed[db] = err
			logger.Error("dump failed", "database", db, "err", err.Error())
			continue
		}
		rep.Dumped = append(rep.Dumped, db)
		logger.Info("dumped", "database", db, "key", key)
		n, err := r.prune(ctx, db)
		if err != nil {
			logger.Warn("old dumps kept", "database", db, "err", err.Error())
		}
		rep.Removed += n
	}
	if len(rep.Failed) > 0 {
		names := make([]string, 0, len(rep.Failed))
		for db := range rep.Failed {
			names = append(names, db)
		}
		sort.Strings(names)
		return rep, fmt.Errorf("%d of %d databases were not dumped: %s", len(rep.Failed), len(dbs), strings.Join(names, ", "))
	}
	return rep, nil
}

// dump streams one pg_dump into the bucket through a pipe: an error on
// either side ends the other, so a failed dump never leaves an object
// that looks complete and a failed upload never leaves pg_dump hanging.
func (r Run) dump(ctx context.Context, db, key string) error {
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := r.Source.Dump(ctx, db, pw)
		pw.CloseWithError(err)
		done <- err
	}()
	putErr := r.Objects.Put(ctx, key, pr)
	pr.CloseWithError(fmt.Errorf("the upload ended"))
	dumpErr := <-done
	switch {
	case dumpErr != nil && (putErr == nil || strings.Contains(putErr.Error(), dumpErr.Error())):
		return dumpErr
	case dumpErr != nil:
		return fmt.Errorf("upload %s: %w; pg_dump: %v", key, putErr, dumpErr)
	case putErr != nil:
		return fmt.Errorf("upload %s: %w", key, putErr)
	}
	return nil
}

// prune removes a database's copies past the newest Keep.
func (r Run) prune(ctx context.Context, db string) (int, error) {
	if r.Keep <= 0 {
		return 0, nil
	}
	keys, err := r.Objects.List(ctx, db+"/")
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, key := range Older(keys, r.Keep) {
		if err := r.Objects.Remove(ctx, key); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// Older is the keys past the newest keep, oldest last; keys sort by time
// because Key names them so.
func Older(keys []string, keep int) []string {
	sorted := append([]string(nil), keys...)
	sort.Sort(sort.Reverse(sort.StringSlice(sorted)))
	if len(sorted) <= keep {
		return nil
	}
	return sorted[keep:]
}
