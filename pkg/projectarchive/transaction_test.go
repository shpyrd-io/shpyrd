package projectarchive

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRestoreTransactionOrderAndFailure(t *testing.T) {
	for _, failure := range []string{"", "stage/volume-b", "commit/database-b", "cancel/volume-b", "rollback/volume-a"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var events []string
			var state TransactionState
			save := func(_ context.Context, s TransactionState) error { state = s; return nil }
			var resources []Participant
			for _, name := range []string{"volume-a", "volume-b", "database-a", "database-b"} {
				fn := func(phase string) func(context.Context) error {
					return func(c context.Context) error {
						if err := c.Err(); err != nil {
							return err
						}
						events = append(events, phase+"/"+name)
						if failure == "cancel/"+name && phase == "stage" {
							cancel()
							return context.Canceled
						}
						if failure == phase+"/"+name || (strings.HasPrefix(failure, "rollback/") && phase == "commit" && name == "database-b") {
							return errors.New("injected failure")
						}
						return nil
					}
				}
				resources = append(resources, Participant{Name: name, Stage: fn("stage"), Commit: fn("commit"), Rollback: fn("rollback"), Finish: fn("finish")})
			}
			err := RestoreTransaction(ctx, resources, save)
			if failure == "" {
				if err != nil || state.Phase != "verifying" {
					t.Fatalf("%v %+v", err, state)
				}
				want := []string{"stage/volume-a", "stage/volume-b", "stage/database-a", "stage/database-b", "commit/volume-a", "commit/volume-b", "commit/database-a", "commit/database-b"}
				if !reflect.DeepEqual(events, want) {
					t.Fatal(events)
				}
			} else {
				want := "rolled-back"
				if strings.HasPrefix(failure, "rollback/") {
					want = "recovery-required"
				}
				if err == nil || state.Phase != want {
					t.Fatalf("%v %+v %v", err, state, events)
				}
				if !reflect.DeepEqual(events[len(events)-4:], []string{"rollback/database-b", "rollback/database-a", "rollback/volume-b", "rollback/volume-a"}) {
					t.Fatal(events)
				}
			}
		})
	}
}

func TestRestoreTransactionZeroResources(t *testing.T) {
	var state TransactionState
	err := RestoreTransaction(context.Background(), nil, func(_ context.Context, s TransactionState) error { state = s; return nil })
	if err != nil || state.Phase != "verifying" {
		t.Fatalf("%v %+v", err, state)
	}
}

func TestRestoreTransactionJournalFailureDoesNotAdvance(t *testing.T) {
	called := false
	fn := func(context.Context) error { called = true; return nil }
	err := RestoreTransaction(context.Background(), []Participant{{Name: "db", Stage: fn, Commit: fn, Rollback: fn, Finish: fn}}, func(context.Context, TransactionState) error { return errors.New("durable storage unavailable") })
	if err == nil || called {
		t.Fatalf("resource changed without saved intent: %v", err)
	}
}

func TestRestoreTransactionTwoRealDatabasesAndVolumes(t *testing.T) {
	ctx := context.Background()
	var resources []Participant
	var databases []PostgreSQL
	var directories []string
	for _, name := range []string{"uploads", "documents"} {
		dir := t.TempDir()
		directories = append(directories, dir)
		if err := os.WriteFile(filepath.Join(dir, "original"), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
		v := openTransaction(t, dir)
		data := tarEntries(t, tar.Header{Name: "restored", Typeflag: tar.TypeReg, Mode: 0600, Size: 5})
		resources = append(resources, Participant{Name: name, Stage: func(c context.Context) error { return v.Stage(c, bytes.NewReader(data), 0) }, Commit: func(context.Context) error { return v.Commit() }, Rollback: func(context.Context) error { return v.Rollback() }, Finish: func(context.Context) error { return v.Finish() }})
	}
	for _, name := range []string{"primary", "events"} {
		p := localPostgres(t)
		databases = append(databases, p)
		appSQL(t, p, "SET ROLE app; CREATE TABLE data(value text); INSERT INTO data VALUES ('backup')")
		if err := p.Fence(ctx); err != nil {
			t.Fatal(err)
		}
		var dump bytes.Buffer
		if err := p.Export(ctx, &dump); err != nil {
			t.Fatal(err)
		}
		appSQL(t, p, "UPDATE data SET value='original'")
		resources = append(resources, Participant{Name: name, Stage: func(c context.Context) error { return p.Stage(c, testOperation, bytes.NewReader(dump.Bytes())) }, Commit: func(c context.Context) error { return p.Commit(c, testOperation) }, Rollback: func(c context.Context) error { return p.Rollback(c, testOperation) }, Finish: func(c context.Context) error { return p.Finish(c, testOperation) }})
	}
	last := resources[len(resources)-1].Commit
	resources[len(resources)-1].Commit = func(context.Context) error { return errors.New("second database unavailable") }
	var state TransactionState
	save := func(_ context.Context, s TransactionState) error { state = s; return nil }
	if err := RestoreTransaction(ctx, resources, save); err == nil || state.Phase != "rolled-back" {
		t.Fatalf("%v %+v", err, state)
	}
	for _, p := range databases {
		if got := appSQL(t, p, "SELECT value FROM data"); got != "original" {
			t.Fatalf("rollback lost DB data: %s", got)
		}
	}
	for _, dir := range directories {
		if _, err := os.Stat(filepath.Join(dir, "original")); err != nil {
			t.Fatal("rollback lost volume data", err)
		}
	}
	if err := FinalizeTransaction(ctx, resources, save); err != nil {
		t.Fatal(err)
	}
	resources[len(resources)-1].Commit = last
	if err := RestoreTransaction(ctx, resources, save); err != nil || state.Phase != "verifying" {
		t.Fatalf("retry: %v %+v", err, state)
	}
	for _, p := range databases {
		if got := appSQL(t, p, "SELECT value FROM data"); got != "backup" {
			t.Fatalf("restore lost DB data: %s", got)
		}
	}
	for _, dir := range directories {
		if got, err := os.ReadFile(filepath.Join(dir, "restored")); err != nil || string(got) != "xxxxx" {
			t.Fatalf("restore lost volume data: %s %v", got, err)
		}
	}
	if err := FinalizeTransaction(ctx, resources, save); err != nil {
		t.Fatal(err)
	}
}
