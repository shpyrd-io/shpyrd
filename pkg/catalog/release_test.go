package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	commitA = "0123456789abcdef0123456789abcdef01234567"
	commitB = "89abcdef0123456789abcdef0123456789abcdef"
)

func entryAt(revision string) string {
	return strings.Replace(fluxyr, "revision: main", "revision: "+revision, 1)
}

func fakeRefs(refs map[string]string, calls *int) RefLister {
	return func(_ context.Context, url string) (map[string]string, error) {
		*calls++
		if url != "https://github.com/shpyrd-io/fluxyr" {
			return nil, errors.New("no such repository")
		}
		return refs, nil
	}
}

func readIndex(t *testing.T, out string) *Index {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(out, IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	ix, err := ParseIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func TestPublishWritesTheCommitTheRevisionNames(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	writeEntry(t, dir, "tagged", entryAt("v1.2"), nil)
	writeEntry(t, dir, "branched", entryAt("main"), nil)
	writeEntry(t, dir, "pinned", entryAt(commitB), nil)
	calls := 0
	list := fakeRefs(map[string]string{
		"refs/heads/main": commitA,
		"refs/tags/v1.2":  commitB,
	}, &calls)
	ix, resolved, err := PublishResolved(context.Background(), dir, out, time.Now(), list)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("the repository is listed once for all its entries, got %d", calls)
	}
	got := readIndex(t, out)
	want := map[string]string{"tagged": commitB, "branched": commitA, "pinned": commitB}
	for name, commit := range want {
		e := got.Find(name)
		if e == nil || e.Commit != commit || ix.Find(name).Commit != commit {
			t.Fatalf("%s: %+v", name, e)
		}
	}
	branch := map[string]bool{}
	for _, r := range resolved {
		branch[r.Name] = r.Branch
	}
	if !branch["branched"] || branch["tagged"] || branch["pinned"] {
		t.Fatalf("only the entry on a branch is marked: %v", branch)
	}
}

func TestAFullCommitRevisionNeedsNoLookup(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	writeEntry(t, dir, "pinned", entryAt(commitA), nil)
	list := func(context.Context, string) (map[string]string, error) {
		t.Fatal("a full commit is not looked up")
		return nil, nil
	}
	if _, _, err := PublishResolved(context.Background(), dir, out, time.Now(), list); err != nil {
		t.Fatal(err)
	}
	if c := readIndex(t, out).Find("pinned").Commit; c != commitA {
		t.Fatalf("commit %q", c)
	}
}

func TestPublishFailsWritingNothingWhenARevisionCannotBeResolved(t *testing.T) {
	calls := 0
	for name, list := range map[string]RefLister{
		"missing revision": fakeRefs(map[string]string{"refs/heads/main": commitA}, &calls),
		"unreadable repository": func(context.Context, string) (map[string]string, error) {
			return nil, errors.New("the git host refused access")
		},
	} {
		dir, out := t.TempDir(), t.TempDir()
		writeEntry(t, dir, "fluxyr", entryAt("v9"), nil)
		_, _, err := PublishResolved(context.Background(), dir, out, time.Now(), list)
		if err == nil || !strings.Contains(err.Error(), "fluxyr") || !strings.Contains(err.Error(), `"v9"`) {
			t.Fatalf("%s: the error names the entry and the revision: %v", name, err)
		}
		if names, _ := os.ReadDir(out); len(names) != 0 {
			t.Fatalf("%s: nothing is written, found %v", name, names)
		}
	}
}

func TestAnIndexWithoutCommitIsStillRead(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	writeEntry(t, dir, "fluxyr", fluxyr, nil)
	if _, err := Publish(dir, out, time.Now()); err != nil {
		t.Fatal(err)
	}
	if e := readIndex(t, out).Find("fluxyr"); e == nil || e.Commit != "" {
		t.Fatalf("%+v", e)
	}
}

func TestAnIndexWithAPartialCommitIsRefused(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	writeEntry(t, dir, "fluxyr", fluxyr, nil)
	ix, err := Publish(dir, out, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"main", commitA[:7], strings.ToUpper(commitA), commitA + "0"} {
		ix.Entries[0].Commit = bad
		raw, _ := json.Marshal(ix)
		if _, err := ParseIndex(raw); err == nil || !strings.Contains(err.Error(), "fluxyr") || !strings.Contains(err.Error(), "not a full commit name") {
			t.Fatalf("%q: %v", bad, err)
		}
	}
	ix.Entries[0].Commit = strings.Repeat("a", 64)
	raw, _ := json.Marshal(ix)
	if _, err := ParseIndex(raw); err != nil {
		t.Fatalf("a SHA-256 commit is a full commit: %v", err)
	}
}

func TestTheCommitIsNotPartOfTheDigest(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	writeEntry(t, dir, "fluxyr", fluxyr, nil)
	plain, err := Publish(dir, out, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	resolved, _, err := PublishResolved(context.Background(), dir, t.TempDir(), time.Now(), fakeRefs(map[string]string{"refs/heads/main": commitA}, &calls))
	if err != nil {
		t.Fatal(err)
	}
	if plain.Entries[0].Digest != resolved.Entries[0].Digest {
		t.Fatal("the digest is the folder's, whatever commit is resolved")
	}
}
