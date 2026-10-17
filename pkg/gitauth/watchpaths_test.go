package gitauth

import (
	"fmt"
	"strings"
	"testing"
)

func TestWatchPathsMatchFromTheRepositoryRoot(t *testing.T) {
	for _, c := range []struct {
		patterns []string
		file     string
		want     bool
	}{
		{[]string{"services/api/**"}, "services/api/main.go", true},
		{[]string{"services/api/**"}, "services/api/internal/db/db.go", true},
		{[]string{"services/api/**"}, "services/web/main.go", false},
		{[]string{"services/api/**"}, "services/api", false},
		{[]string{"*.md"}, "README.md", true},
		{[]string{"*.md"}, "docs/guide.md", false},
		{[]string{"**/*.md"}, "docs/guide.md", true},
		{[]string{"**/*.md"}, "README.md", true},
		{[]string{"**"}, "anything/at/all.txt", true},
		{[]string{"/web/**"}, "web/app.ts", true},
		{[]string{"docs/"}, "docs/a/b.md", true},
		{[]string{"docs/"}, "docsite/index.md", false},
		{[]string{"a/**/b.txt"}, "a/b.txt", true},
		{[]string{"a/**/b.txt"}, "a/x/y/b.txt", true},
		{[]string{"v[0-9].txt"}, "v7.txt", true},
		{[]string{"v[0-9].txt"}, "vx.txt", false},
		{[]string{"src/?.go"}, "src/a.go", true},
		{[]string{"src/*.go"}, "src/pkg/a.go", false},
	} {
		if got := MatchWatchPaths(c.patterns, []string{c.file}); got != c.want {
			t.Errorf("%v on %s: %v", c.patterns, c.file, got)
		}
	}
}

func TestTheLastWatchPathThatMatchesDecides(t *testing.T) {
	everythingButDocs := []string{"**", "!**/*.md"}
	if MatchWatchPaths(everythingButDocs, []string{"README.md", "docs/guide.md"}) {
		t.Error("only Markdown changed: nothing deploys")
	}
	if !MatchWatchPaths(everythingButDocs, []string{"README.md", "main.go"}) {
		t.Error("a Go file changed with the docs: it deploys")
	}
	if !MatchWatchPaths([]string{"!**/*.md", "**"}, []string{"README.md"}) {
		t.Error("** comes last and matches: it decides")
	}
	if MatchWatchPaths([]string{"!services/**"}, []string{"main.go"}) {
		t.Error("only an exclusion: nothing is included")
	}
	if MatchWatchPaths([]string{"services/api/**", "!services/api/docs/**"}, []string{"services/api/docs/x.md"}) {
		t.Error("an exclusion after an inclusion wins for the files it matches")
	}
}

func TestARenamedFileCountsUnderBothNames(t *testing.T) {
	// The compare lists a rename as its new name and its old one.
	if !MatchWatchPaths([]string{"services/api/**"}, []string{"services/shared/util.go", "services/api/util.go"}) {
		t.Error("moved out of the watch path: its old name matches")
	}
}

func TestWatchPathsThatCannotBeReadAreRefusedWhenSet(t *testing.T) {
	for _, p := range []string{"", "  ", "!", "/", "a//b", "a**", "**b/c", "src/[a-", `x\`} {
		if err := ValidWatchPaths([]string{p}); err == nil {
			t.Errorf("%q is taken", p)
		}
	}
	if err := ValidWatchPaths([]string{"services/api/**", "!**/*.md", "/web/", "docs/"}); err != nil {
		t.Errorf("good patterns: %v", err)
	}
	var many []string
	for i := range MaxWatchPaths + 1 {
		many = append(many, fmt.Sprintf("dir%d/**", i))
	}
	if err := ValidWatchPaths(many); err == nil || !strings.Contains(err.Error(), "at most 20") {
		t.Errorf("21 patterns: %v", err)
	}
	if err := ValidWatchPaths([]string{strings.Repeat("a", MaxWatchPathLength+1)}); err == nil || !strings.Contains(err.Error(), "200 characters") {
		t.Errorf("a long one: %v", err)
	}
	if !MatchWatchPaths([]string{"a**", "**"}, []string{"x"}) {
		t.Error("an unreadable pattern that slipped through matches nothing; the others still count")
	}
}

func TestCleanWatchPathsDropsBlankLines(t *testing.T) {
	got := CleanWatchPaths([]string{" services/api/** ", "", "  ", "!**/*.md"})
	if strings.Join(got, "|") != "services/api/**|!**/*.md" {
		t.Errorf("got %q", got)
	}
	if CleanWatchPaths([]string{"", " "}) != nil {
		t.Error("nothing left is nil, so the field is removed")
	}
}
