package pgdump

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeDatabase is one side of a conversion in memory: its tables with
// their rows, its extensions and size; a dump is its tables and rows
// written out, a restore reads them back in.
type fakeDatabase struct {
	rows       map[string]int64
	extensions []string
	size       int64
	dumpErr    error
	restoreErr error
	fenced     bool
}

func (d *fakeDatabase) Tables(context.Context) ([]string, error) {
	var out []string
	for t := range d.rows {
		out = append(out, t)
	}
	return out, nil
}
func (d *fakeDatabase) Count(_ context.Context, table string) (int64, error) {
	return d.rows[table], nil
}
func (d *fakeDatabase) Extensions(context.Context) ([]string, error) { return d.extensions, nil }
func (d *fakeDatabase) Size(context.Context) (int64, error)          { return d.size, nil }
func (d *fakeDatabase) Dump(_ context.Context, w io.Writer) error {
	if d.dumpErr != nil {
		return d.dumpErr
	}
	for t, n := range d.rows {
		if _, err := io.WriteString(w, t+"="+itoa(n)+"\n"); err != nil {
			return err
		}
	}
	return nil
}
func (d *fakeDatabase) Restore(_ context.Context, dump io.Reader) error {
	if d.restoreErr != nil {
		return d.restoreErr
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, dump); err != nil {
		return err
	}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if t, n, ok := strings.Cut(line, "="); ok {
			var v int64
			for _, c := range n {
				v = v*10 + int64(c-'0')
			}
			d.rows[t] = v
		}
	}
	return nil
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func (o *fakeObjects) Get(_ context.Context, key string) (io.ReadCloser, error) {
	body, ok := o.objects[key]
	if !ok {
		return nil, errors.New("the copy " + key + ": not found")
	}
	return io.NopCloser(strings.NewReader(body)), nil
}

func TestConvertMovesTheRowsAndKeepsACopy(t *testing.T) {
	source := &fakeDatabase{rows: map[string]int64{"public.orders": 12, "public.items": 0}, extensions: []string{"pgcrypto", "vector"}, size: 10 << 20}
	target := &fakeDatabase{rows: map[string]int64{}}
	objects := &fakeObjects{objects: map[string]string{}}
	var fenced, lifted int
	c := Convert{Source: source, Target: target, Objects: objects, Getter: objects, Key: ConvertKey("db_new", tonight),
		Allowed: []string{"pgcrypto", "vector"}, Cap: 1 << 30,
		Fence: func(context.Context) error { fenced++; return nil }, Lift: func(context.Context) error { lifted++; return nil }}
	moved, err := c.Do(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if moved.Tables != 2 || moved.Rows != 12 || moved.Bytes != 10<<20 || target.rows["public.orders"] != 12 {
		t.Errorf("moved = %+v target = %v", moved, target.rows)
	}
	if _, ok := objects.objects["db_new/20261014T030700Z.converted.dump"]; !ok || fenced != 1 || lifted != 0 {
		t.Errorf("copy kept: %v, fenced %d lifted %d", objects.objects, fenced, lifted)
	}
}

func TestConvertRefusesBeforeTouchingTheSource(t *testing.T) {
	objects := &fakeObjects{objects: map[string]string{}}
	var fenced int
	fence := func(context.Context) error { fenced++; return nil }
	source := &fakeDatabase{rows: map[string]int64{"public.t": 1}, extensions: []string{"vector", "pg_stat_statements", "timescaledb"}, size: 100}
	c := Convert{Source: source, Target: &fakeDatabase{rows: map[string]int64{}}, Objects: objects, Getter: objects, Key: "db_new/x.dump", Allowed: []string{"vector"}, Fence: fence}
	if _, err := c.Do(context.Background()); err == nil || !strings.Contains(err.Error(), "uses pg_stat_statements and timescaledb, which a shared database cannot hold; drop them first") {
		t.Errorf("extensions: %v", err)
	}
	source.extensions = []string{"vector"}
	source.size = 3 << 30
	c.Cap = 1 << 30
	if _, err := c.Do(context.Background()); err == nil || !strings.Contains(err.Error(), "holds 3.0 GB; a shared database holds up to 1.0 GB") {
		t.Errorf("cap: %v", err)
	}
	if fenced != 0 || len(objects.objects) != 0 {
		t.Errorf("a refusal touches nothing: fenced %d objects %v", fenced, objects.objects)
	}
	// Without an allow-list or a cap (to a dedicated database) nothing is refused.
	c.Allowed, c.Cap = nil, 0
	if _, err := c.Do(context.Background()); err != nil {
		t.Errorf("to dedicated: %v", err)
	}
}

func TestConvertLiftsTheFenceWhenTheCopyDoesNotLand(t *testing.T) {
	objects := &fakeObjects{objects: map[string]string{}}
	var lifted int
	lift := func(context.Context) error { lifted++; return nil }
	source := &fakeDatabase{rows: map[string]int64{"public.t": 5}, size: 1}
	target := &fakeDatabase{rows: map[string]int64{}, restoreErr: errors.New("permission denied for schema public")}
	c := Convert{Source: source, Target: target, Objects: objects, Getter: objects, Key: "db_new/x.dump", Fence: func(context.Context) error { return nil }, Lift: lift}
	if _, err := c.Do(context.Background()); err == nil || !strings.Contains(err.Error(), "load the copy into the target: permission denied") || lifted != 1 {
		t.Errorf("restore failure: %v lifted %d", err, lifted)
	}
	// A count that differs is a copy that is not whole.
	target.restoreErr = nil
	target.rows = map[string]int64{"public.t": 4}
	target.restoreErr = nil
	c.Target = &countsWrong{target}
	if _, err := c.Do(context.Background()); err == nil || !strings.Contains(err.Error(), "public.t has 5 rows at the source and 4 at the target") || lifted != 2 {
		t.Errorf("count mismatch: %v lifted %d", err, lifted)
	}
	// A dump that fails leaves no half copy.
	objects.objects = map[string]string{}
	source.dumpErr = errors.New("pg_dump: connection reset")
	c.Target = target
	if _, err := c.Do(context.Background()); err == nil || !strings.Contains(err.Error(), "copy the source into db_new/x.dump: pg_dump: connection reset") || len(objects.objects) != 0 {
		t.Errorf("dump failure: %v objects %v", err, objects.objects)
	}
}

// countsWrong restores nothing, so its counts stay as they were.
type countsWrong struct{ *fakeDatabase }

func (c *countsWrong) Restore(_ context.Context, dump io.Reader) error {
	_, err := io.Copy(io.Discard, dump)
	return err
}

func TestConvertKeyAndWords(t *testing.T) {
	if got := ConvertKey("db_a", time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)); got != "db_a/20261015T090000Z.converted.dump" {
		t.Errorf("key = %s", got)
	}
	if _, _, _, kind, ok := Parse("db_a/20261015T090000Z.converted.dump"); !ok || kind != KindConverted {
		t.Errorf("parse = %v %s", ok, kind)
	}
	for n, want := range map[int64]string{512: "512 bytes", 4096: "4 kB", 10 << 20: "10 MB", 3 << 30: "3.0 GB"} {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %s, want %s", n, got, want)
		}
	}
}

// A table with row-level security forced hides its rows from its owner:
// the target's own count reads zero where the copy landed whole. With a
// counter that sees every row (the server's superuser) the conversion
// verifies and completes; without one it would fall back.
func TestConvertCountsTheTargetThroughTheCounterWhenGiven(t *testing.T) {
	ctx := context.Background()
	source := &fakeDatabase{rows: map[string]int64{"public.audit_entries": 6, "public.posts": 2}}
	target := &fakeDatabase{rows: map[string]int64{}}
	objects := &fakeObjects{objects: map[string]string{}}
	c := Convert{Source: source, Target: target, Objects: objects, Getter: objects, Key: "db_x/2026.converted.dump"}
	// The owner's view of the target: the policy hides audit_entries.
	hidden := &rlsView{Database: target, hidden: "public.audit_entries"}
	c.Target = hidden
	if _, err := c.Do(ctx); err == nil || !strings.Contains(err.Error(), "public.audit_entries has 6 rows at the source and 0 at the target") {
		t.Fatalf("without a counter the owner's zero fails the copy: %v", err)
	}
	target.rows = map[string]int64{}
	c.TargetCounter = target // the superuser sees the rows the policy hides
	moved, err := c.Do(ctx)
	if err != nil {
		t.Fatalf("with the superuser's count: %v", err)
	}
	if moved.Rows != 8 || moved.Tables != 2 {
		t.Errorf("moved = %+v, want 8 rows in 2 tables", moved)
	}
}

// rlsView is a target whose owner cannot see one table's rows.
type rlsView struct {
	Database
	hidden string
}

func (v *rlsView) Count(ctx context.Context, table string) (int64, error) {
	if table == v.hidden {
		return 0, nil
	}
	return v.Database.Count(ctx, table)
}
