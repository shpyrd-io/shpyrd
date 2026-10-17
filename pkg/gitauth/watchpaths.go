package gitauth

import (
	"fmt"
	"path"
	"strings"
)

// Watch paths (RFC-0054): gitignore-style globs from the repository root
// that say which changes deploy a project. Patterns run in order and the
// last one that matches a file decides it; a leading ! excludes. `*`
// matches within one part of a path, `**` any number of parts, and a
// pattern ending in / everything below it.

const (
	MaxWatchPaths      = 20
	MaxWatchPathLength = 200
)

// watchPattern is one pattern, read: whether it excludes, and its parts.
type watchPattern struct {
	exclude  bool
	segments []string
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
		if len(p) > MaxWatchPathLength {
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
			if matchSegments(w.segments, name) {
				in = !w.exclude
			}
		}
		if in {
			return true
		}
	}
	return false
}

// matchSegments matches a pattern's parts against a path's: ** takes any
// number of parts, and at the end at least one (a folder's contents, not
// a file of that name).
func matchSegments(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			if len(rest) == 0 {
				return len(name) > 0
			}
			for i := 0; i <= len(name); i++ {
				if matchSegments(rest, name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], name[0]); !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}
