package pgdump

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Converting a database (storage plan, step 3) moves its rows between a
// dedicated cluster and a shared server: the source is checked, fenced,
// dumped into the shared server's bucket as a copy of the new database,
// the copy restored into the target as its owner, and every table counted
// on both sides. The copy stays, so the source's last state can be
// restored for seven days, as any copy can.

// KindConverted marks the copy a conversion takes of its source.
const KindConverted = "converted"

// A Database is one side of a conversion.
type Database interface {
	// Tables lists the ordinary tables, schema-qualified and quoted.
	Tables(ctx context.Context) ([]string, error)
	// Count is the rows of one table.
	Count(ctx context.Context, table string) (int64, error)
	// Extensions lists the installed extensions, plpgsql left out.
	Extensions(ctx context.Context) ([]string, error)
	// Size is the database's size in bytes.
	Size(ctx context.Context) (int64, error)
	Dump(ctx context.Context, w io.Writer) error
	Restore(ctx context.Context, dump io.Reader) error
}

// Convert is one conversion.
type Convert struct {
	Source, Target Database
	Objects        Objects
	Getter         Getter
	// Key is the copy's key in the bucket (the target database's folder).
	Key string
	// Allowed, when set, are the extensions the target can hold: a source
	// with any other is refused before anything moves.
	Allowed []string
	// Cap, when positive, is the most bytes the source may hold.
	Cap int64
	// Fence closes the source to its owner for the copy, Lift opens it
	// again when the copy does not land; both optional (a shared source
	// is fenced by the platform).
	Fence, Lift func(ctx context.Context) error
	Log         *slog.Logger
}

// Moved says what a conversion moved.
type Moved struct {
	Tables int
	Rows   int64
	Bytes  int64
}

// Do runs the conversion; an error before the copy leaves the source as it
// was, one after the fence lifts it.
func (c Convert) Do(ctx context.Context) (Moved, error) {
	var rep Moved
	logger := c.Log
	if logger == nil {
		logger = slog.Default()
	}
	if err := c.check(ctx, &rep); err != nil {
		return rep, err
	}
	if c.Fence != nil {
		if err := c.Fence(ctx); err != nil {
			return rep, fmt.Errorf("pause the source's connections: %w", err)
		}
	}
	err := c.move(ctx, &rep, logger)
	if err != nil && c.Lift != nil {
		if lifted := c.Lift(ctx); lifted != nil {
			logger.Error("the source stays paused", "err", lifted.Error())
		}
	}
	return rep, err
}

// check refuses, in words, a source the target cannot hold.
func (c Convert) check(ctx context.Context, rep *Moved) error {
	if c.Allowed != nil {
		exts, err := c.Source.Extensions(ctx)
		if err != nil {
			return fmt.Errorf("list the source's extensions: %w", err)
		}
		var missing []string
		for _, e := range exts {
			if !contains(c.Allowed, e) {
				missing = append(missing, e)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return fmt.Errorf("the database uses %s, which a shared database cannot hold; drop %s first, or keep a dedicated database", joinAnd(missing), itOrThem(missing))
		}
	}
	size, err := c.Source.Size(ctx)
	if err != nil {
		return fmt.Errorf("measure the source: %w", err)
	}
	rep.Bytes = size
	if c.Cap > 0 && size > c.Cap {
		return fmt.Errorf("this database holds %s; a shared database holds up to %s", humanSize(size), humanSize(c.Cap))
	}
	return nil
}

// move copies the rows and counts them.
func (c Convert) move(ctx context.Context, rep *Moved, logger *slog.Logger) error {
	if err, _ := stream(ctx, c.Source.Dump, func(r io.Reader) error { return c.Objects.Put(ctx, c.Key, r) }); err != nil {
		return fmt.Errorf("copy the source into %s: %w", c.Key, err)
	}
	logger.Info("source copied", "key", c.Key, "bytes", rep.Bytes)
	if err := Restore(ctx, c.Getter, c.Key, c.Target); err != nil {
		return fmt.Errorf("load the copy into the target: %w", err)
	}
	tables, err := c.Source.Tables(ctx)
	if err != nil {
		return fmt.Errorf("list the source's tables: %w", err)
	}
	for _, table := range tables {
		from, err := c.Source.Count(ctx, table)
		if err != nil {
			return fmt.Errorf("count %s at the source: %w", table, err)
		}
		to, err := c.Target.Count(ctx, table)
		if err != nil {
			return fmt.Errorf("count %s at the target: %w", table, err)
		}
		if from != to {
			return fmt.Errorf("%s has %d rows at the source and %d at the target: the copy is not whole", table, from, to)
		}
		rep.Rows += from
	}
	rep.Tables = len(tables)
	logger.Info("rows verified", "tables", rep.Tables, "rows", rep.Rows)
	return nil
}

// stream pipes a dump into an upload: an error on either side ends the
// other, so a failed dump never leaves an object that looks complete and
// a failed upload never leaves pg_dump hanging. It says whether the error
// is the dump's own (else the upload's, with the dump's appended when both
// failed).
func stream(ctx context.Context, dump func(context.Context, io.Writer) error, put func(io.Reader) error) (err error, fromDump bool) {
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := dump(ctx, pw)
		pw.CloseWithError(err)
		done <- err
	}()
	putErr := put(pr)
	pr.CloseWithError(fmt.Errorf("the upload ended"))
	dumpErr := <-done
	switch {
	case dumpErr != nil && (putErr == nil || strings.Contains(putErr.Error(), dumpErr.Error())):
		return dumpErr, true
	case dumpErr != nil:
		return fmt.Errorf("%w; pg_dump: %v", putErr, dumpErr), false
	case putErr != nil:
		return putErr, false
	}
	return nil, false
}

// ConvertKey names the copy a conversion takes: in the target's folder,
// at this moment, marked converted.
func ConvertKey(database string, at time.Time) string { return Key(database, at, KindConverted) }

// Tables lists the ordinary tables of the connected database, quoted.
// Partitions are listed, their parents are not: the rows are in the
// partitions.
func (p Postgres) Tables(ctx context.Context) ([]string, error) {
	rows, err := p.Conn.Query(ctx, `SELECT format('%I.%I', n.nspname, c.relname) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'r' AND n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg\_toast%' ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// Count is the rows of one table (named as Tables names it).
func (p Postgres) Count(ctx context.Context, table string) (int64, error) {
	var n int64
	err := p.Conn.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n)
	return n, err
}

// Extensions lists the installed extensions, plpgsql left out.
func (p Postgres) Extensions(ctx context.Context) ([]string, error) {
	rows, err := p.Conn.Query(ctx, `SELECT extname FROM pg_extension WHERE extname <> 'plpgsql' ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// Size is the connected database's size in bytes.
func (p Postgres) Size(ctx context.Context) (int64, error) {
	var n int64
	err := p.Conn.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&n)
	return n, err
}

// DumpDatabase is Dump of the connected database (Database's shape).
func (p Postgres) DumpDatabase(ctx context.Context, w io.Writer) error {
	return p.Dump(ctx, p.database(), w)
}

// Connected is a Postgres as a conversion's Database: its own database.
type Connected struct{ Postgres }

func (c Connected) Dump(ctx context.Context, w io.Writer) error {
	return c.Postgres.DumpDatabase(ctx, w)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

func itOrThem(items []string) string {
	if len(items) == 1 {
		return "it"
	}
	return "them"
}

// humanSize is a byte count in words.
func humanSize(n int64) string {
	const unit = 1024
	switch {
	case n >= unit*unit*unit:
		return fmt.Sprintf("%.1f GB", float64(n)/(unit*unit*unit))
	case n >= unit*unit:
		return fmt.Sprintf("%.0f MB", float64(n)/(unit*unit))
	case n >= unit:
		return fmt.Sprintf("%.0f kB", float64(n)/unit)
	}
	return fmt.Sprintf("%d bytes", n)
}
