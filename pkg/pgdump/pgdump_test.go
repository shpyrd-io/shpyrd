package pgdump

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

type fakeSource struct {
	dbs  []string
	fail map[string]error
}

func (s fakeSource) Databases(context.Context) ([]string, error) { return s.dbs, nil }

func (s fakeSource) Dump(_ context.Context, db string, w io.Writer) error {
	if _, err := io.WriteString(w, "dump of "+db+" "); err != nil {
		return err
	}
	if err := s.fail[db]; err != nil {
		return err
	}
	_, err := io.WriteString(w, "complete")
	return err
}

type fakeObjects struct {
	objects map[string]string
	putErr  error
}

func (o *fakeObjects) Put(_ context.Context, key string, r io.Reader) error {
	if o.putErr != nil {
		return o.putErr
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		return err
	}
	o.objects[key] = buf.String()
	return nil
}

func (o *fakeObjects) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range o.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (o *fakeObjects) Remove(_ context.Context, key string) error {
	delete(o.objects, key)
	return nil
}

var tonight = time.Date(2026, 10, 14, 3, 7, 0, 0, time.UTC)

func TestDoDumpsEveryDatabaseIntoItsOwnFolder(t *testing.T) {
	objects := &fakeObjects{objects: map[string]string{}}
	run := Run{Source: fakeSource{dbs: []string{"db_a1b2c3d4", "db_e5f6a7b8"}}, Objects: objects, Keep: 7, Now: func() time.Time { return tonight }}
	rep, err := run.Do(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Dumped) != 2 || len(rep.Failed) != 0 || rep.Removed != 0 {
		t.Errorf("report = %+v", rep)
	}
	if got := objects.objects["db_a1b2c3d4/20261014T030700Z.dump"]; got != "dump of db_a1b2c3d4 complete" {
		t.Errorf("object = %q in %v", got, objects.objects)
	}
	if _, ok := objects.objects["db_e5f6a7b8/20261014T030700Z.dump"]; !ok {
		t.Errorf("second database missing: %v", objects.objects)
	}
}

func TestDoKeepsGoingWhenOneDumpFails(t *testing.T) {
	objects := &fakeObjects{objects: map[string]string{}}
	run := Run{Source: fakeSource{dbs: []string{"db_bad", "db_good"}, fail: map[string]error{"db_bad": errors.New("pg_dump db_bad: exit status 1: permission denied for table secrets")}}, Objects: objects, Now: func() time.Time { return tonight }}
	rep, err := run.Do(context.Background())
	if err == nil || !strings.Contains(err.Error(), "1 of 2 databases were not dumped: db_bad") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(rep.Failed["db_bad"].Error(), "permission denied") {
		t.Errorf("failure = %v", rep.Failed["db_bad"])
	}
	if _, ok := objects.objects["db_good/20261014T030700Z.dump"]; !ok || len(objects.objects) != 1 {
		t.Errorf("a failed dump left an object, or the good one is missing: %v", objects.objects)
	}
}

func TestDoReportsAFailedUpload(t *testing.T) {
	objects := &fakeObjects{objects: map[string]string{}, putErr: errors.New("connection refused")}
	run := Run{Source: fakeSource{dbs: []string{"db_a"}}, Objects: objects, Now: func() time.Time { return tonight }}
	rep, err := run.Do(context.Background())
	if err == nil || !strings.Contains(rep.Failed["db_a"].Error(), "upload db_a/20261014T030700Z.dump: connection refused") {
		t.Errorf("err = %v failure = %v", err, rep.Failed["db_a"])
	}
}

func TestDoKeepsTheNewestCopies(t *testing.T) {
	objects := &fakeObjects{objects: map[string]string{}}
	for day := 1; day <= 9; day++ {
		objects.objects[Key("db_a", tonight.AddDate(0, 0, -day), "")] = "old"
	}
	objects.objects["db_b/20261001T030700Z.dump"] = "another database's"
	run := Run{Source: fakeSource{dbs: []string{"db_a"}}, Objects: objects, Keep: 7, Now: func() time.Time { return tonight }}
	rep, err := run.Do(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	keys, _ := objects.List(context.Background(), "db_a/")
	if rep.Removed != 3 || len(keys) != 7 || keys[6] != "db_a/20261014T030700Z.dump" || keys[0] != "db_a/20261008T030700Z.dump" {
		t.Errorf("removed %d, kept %v", rep.Removed, keys)
	}
	if _, ok := objects.objects["db_b/20261001T030700Z.dump"]; !ok {
		t.Error("another database's copy was removed")
	}
}

// A database's own count, a file named after it in the directory, wins
// over the run's; a database without one, or with nonsense, has the run's.
func TestKeepFromDirReadsADatabasesOwnCount(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"db_a": "14\n", "db_c": "many"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	keep := KeepFromDir(dir)
	if keep("db_a") != 14 || keep("db_b") != 0 || keep("db_c") != 0 {
		t.Errorf("counts = %d %d %d", keep("db_a"), keep("db_b"), keep("db_c"))
	}
	objects := &fakeObjects{objects: map[string]string{}}
	for day := 1; day <= 9; day++ {
		objects.objects[Key("db_a", tonight.AddDate(0, 0, -day), "")] = "old"
		objects.objects[Key("db_b", tonight.AddDate(0, 0, -day), "")] = "old"
	}
	run := Run{Source: fakeSource{dbs: []string{"db_a", "db_b"}}, Objects: objects, Keep: 3, KeepFor: keep, Now: func() time.Time { return tonight }}
	if _, err := run.Do(context.Background()); err != nil {
		t.Fatal(err)
	}
	a, _ := objects.List(context.Background(), "db_a/")
	b, _ := objects.List(context.Background(), "db_b/")
	if len(a) != 10 || len(b) != 3 {
		t.Errorf("db_a keeps %d (its own 14), db_b keeps %d (the run's 3)", len(a), len(b))
	}
}

func TestDoDumpsOneDatabaseNowWhenAsked(t *testing.T) {
	objects := &fakeObjects{objects: map[string]string{}}
	run := Run{Source: fakeSource{dbs: []string{"db_a", "db_b"}}, Objects: objects, Only: "db_b", Kind: KindNow, Now: func() time.Time { return tonight }}
	if _, err := run.Do(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(objects.objects) != 1 || objects.objects["db_b/20261014T030700Z.now.dump"] == "" {
		t.Errorf("objects = %v", objects.objects)
	}
	run.Only = "db_c"
	if _, err := run.Do(context.Background()); err == nil || !strings.Contains(err.Error(), "db_c is not on this server") {
		t.Errorf("a database the role cannot see: %v", err)
	}
}

func TestParseReadsAKeyBack(t *testing.T) {
	for key, want := range map[string]string{
		"db_a/20261014T030700Z.dump":     "db_a 20261014T030700Z 2026-10-14T03:07:00Z kind=",
		"db_a/20261014T101500Z.now.dump": "db_a 20261014T101500Z.now 2026-10-14T10:15:00Z kind=now",
	} {
		db, name, at, kind, ok := Parse(key)
		if got := db + " " + name + " " + at.Format(time.RFC3339) + " kind=" + kind; !ok || got != want {
			t.Errorf("Parse(%s) = %q ok=%v, want %q", key, got, ok, want)
		}
	}
	for _, key := range []string{"db_a/notes.txt", "db_a", "db_a/2026.dump"} {
		if _, _, _, _, ok := Parse(key); ok {
			t.Errorf("Parse(%s) ok", key)
		}
	}
}

func TestOlderWithoutAKeepCount(t *testing.T) {
	objects := &fakeObjects{objects: map[string]string{"db_a/20261001T030700Z.dump": "old"}}
	run := Run{Source: fakeSource{dbs: []string{"db_a"}}, Objects: objects, Now: func() time.Time { return tonight }}
	if _, err := run.Do(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(objects.objects) != 2 {
		t.Errorf("Keep 0 removed something: %v", objects.objects)
	}
	if got := Older([]string{"a/2", "a/1", "a/3"}, 2); len(got) != 1 || got[0] != "a/1" {
		t.Errorf("Older = %v", got)
	}
}
