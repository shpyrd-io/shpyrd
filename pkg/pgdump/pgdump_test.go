package pgdump

import (
	"bytes"
	"context"
	"errors"
	"io"
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
		objects.objects[Key("db_a", tonight.AddDate(0, 0, -day))] = "old"
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
