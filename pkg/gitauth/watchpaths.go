package gitauth

import (
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

// Watch paths (RFC-0054): gitignore-style globs from the repository root
// that say which changes deploy a project. Patterns run in order and the
// last one that matches a file decides it; a leading ! excludes. `*`
// matches within one part of a path, `**` any number of parts, and a
// pattern ending in / everything below it. As in gitignore, a pattern that
// names a folder (services/api) matches the folder's contents too.

const (
	MaxWatchPaths      = 20
	MaxWatchPathLength = 200
)

// watchPattern is one pattern, read: whether it excludes, and its parts.
type watchPattern struct {
	exclude  bool
	segments []string
	// below: the pattern also matches everything under what it names.
	below bool
}

// parseWatchPath reads a pattern. A leading / is the repository root, as
// is none; a trailing / is everything below.
func parseWatchPath(p string) (watchPattern, error) {
	raw := strings.TrimSpace(p)
	var w watchPattern
	if rest, ok := strings.CutPrefix(raw, "!"); ok {
		w.exclude, raw = true, rest
	}
	raw = strings.TrimPrefix(raw, "/")
	if raw != "" && strings.HasSuffix(raw, "/") {
		raw += "**"
	} else {
		w.below = true
	}
	if raw == "" {
		return watchPattern{}, fmt.Errorf("the watch path %q names no file or folder", p)
	}
	for _, seg := range strings.Split(raw, "/") {
		switch {
		case seg == "":
			return watchPattern{}, fmt.Errorf("the watch path %q has an empty part (//)", p)
		case seg == "**":
		case strings.Contains(seg, "**"):
			return watchPattern{}, fmt.Errorf("in the watch path %q, ** stands alone between slashes, as in services/**/api", p)
		default:
			// path.Match checks the whole pattern even when the name does
			// not match.
			if _, err := path.Match(seg, ""); err != nil {
				return watchPattern{}, fmt.Errorf("the watch path %q is not a pattern shpyrd can read: check its [ ] and \\", p)
			}
		}
		// Consecutive ** mean what one does; collapsing them keeps the
		// match from backtracking over every way to split a path.
		if seg == "**" && len(w.segments) > 0 && w.segments[len(w.segments)-1] == "**" {
			continue
		}
		w.segments = append(w.segments, seg)
	}
	return w, nil
}

// CleanWatchPaths trims each pattern and drops the blank ones: what a
// person typed, one per line. Nil when none is left.
func CleanWatchPaths(patterns []string) []string {
	var out []string
	for _, p := range patterns {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ValidWatchPaths checks watch paths as they are set: at most 20 patterns
// of up to 200 characters, each one readable.
func ValidWatchPaths(patterns []string) error {
	if len(patterns) > MaxWatchPaths {
		return fmt.Errorf("a project takes at most %d watch paths; there are %d", MaxWatchPaths, len(patterns))
	}
	for _, p := range patterns {
		if utf8.RuneCountInString(p) > MaxWatchPathLength {
			return fmt.Errorf("a watch path is at most %d characters; %.40q… is longer", MaxWatchPathLength, p)
		}
		if _, err := parseWatchPath(p); err != nil {
			return err
		}
	}
	return nil
}

// MatchWatchPaths says a change deploys: one of the files is matched, and
// the last pattern that matches it does not exclude. A pattern that cannot
// be read matches nothing (patterns are checked when they are set).
func MatchWatchPaths(patterns, files []string) bool {
	var ps []watchPattern
	for _, p := range patterns {
		if w, err := parseWatchPath(p); err == nil {
			ps = append(ps, w)
		}
	}
	for _, f := range files {
		name := strings.Split(strings.TrimPrefix(f, "/"), "/")
		in := false
		for _, w := range ps {
			if w.matches(name) {
				in = !w.exclude
			}
		}
		if in {
			return true
		}
	}
	return false
}

// matches says the pattern covers the path.
func (w watchPattern) matches(name []string) bool {
	memo := make(map[[2]int]bool)
	seen := make(map[[2]int]bool)
	var match func(i, j int) bool
	match = func(i, j int) bool {
		k := [2]int{i, j}
		if seen[k] {
			return memo[k]
		}
		seen[k] = true
		memo[k] = func() bool {
			if i == len(w.segments) {
				return j == len(name) || w.below
			}
			if w.segments[i] == "**" {
				if i == len(w.segments)-1 {
					return j < len(name)
				}
				for n := j; n <= len(name); n++ {
					if match(i+1, n) {
						return true
					}
				}
				return false
			}
			if j == len(name) {
				return false
			}
			if ok, _ := path.Match(w.segments[i], name[j]); !ok {
				return false
			}
			return match(i+1, j+1)
		}()
		return memo[k]
	}
	return match(0, 0)
}
