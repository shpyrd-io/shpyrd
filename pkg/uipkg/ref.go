// Package uipkg fetches the application a workspace chose for its door
// (RFC-0084): a build of the workspace application, the folder `next
// build` exports, published to a registry as an artifact or an image, or
// lying in a directory while developing. A reference names where it is;
// the fetcher brings it to a directory the server serves like its own.
package uipkg

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// The kinds of reference.
const (
	KindDir = "dir" // dir:/absolute/path, a directory on the server
	KindOCI = "oci" // oci://host/repository[:tag][@sha256:digest]
)

// Ref is a parsed reference.
type Ref struct {
	Kind string
	// Dir is the directory, for KindDir.
	Dir string
	// Host, Repository, Tag and Digest are the parts of an OCI reference.
	// Digest is "sha256:<hex>"; Tag is what the reference named when it
	// had no digest.
	Host, Repository, Tag, Digest string
}

var (
	repositoryRe = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)*$`)
	tagRe        = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
	digestRe     = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

// Parse reads a reference as the API receives it.
func Parse(s string) (Ref, error) {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(s, KindDir+":"):
		dir := strings.TrimPrefix(s, KindDir+":")
		if !filepath.IsAbs(dir) {
			return Ref{}, errors.New("dir: takes an absolute path")
		}
		return Ref{Kind: KindDir, Dir: filepath.Clean(dir)}, nil
	case strings.HasPrefix(s, KindOCI+"://"):
		return parseOCI(strings.TrimPrefix(s, KindOCI+"://"))
	case s == "":
		return Ref{}, errors.New("empty reference")
	}
	return Ref{}, fmt.Errorf("unknown source in %q: oci://host/repository@sha256:... or dir:/path", s)
}

func parseOCI(s string) (Ref, error) {
	r := Ref{Kind: KindOCI}
	if i := strings.Index(s, "@"); i >= 0 {
		r.Digest = s[i+1:]
		s = s[:i]
		if !digestRe.MatchString(r.Digest) {
			return Ref{}, errors.New("the digest is sha256: followed by 64 hex digits")
		}
	}
	host, rest, ok := strings.Cut(s, "/")
	if !ok || host == "" || rest == "" {
		return Ref{}, errors.New("an OCI reference is oci://host/repository[:tag][@sha256:digest]")
	}
	if u, err := url.Parse("https://" + host); err != nil || u.Host != host || u.Hostname() == "" {
		return Ref{}, fmt.Errorf("%q is not a registry host", host)
	}
	r.Host = host
	// A tag is after the last colon of the path part, if that colon comes
	// after the last slash (a port lives in the host, already cut).
	if i := strings.LastIndex(rest, ":"); i > strings.LastIndex(rest, "/") {
		r.Tag = rest[i+1:]
		rest = rest[:i]
		if !tagRe.MatchString(r.Tag) {
			return Ref{}, fmt.Errorf("%q is not a tag", r.Tag)
		}
	}
	if !repositoryRe.MatchString(rest) {
		return Ref{}, fmt.Errorf("%q is not a repository name", rest)
	}
	r.Repository = rest
	if r.Tag == "" && r.Digest == "" {
		r.Tag = "latest"
	}
	return r, nil
}

// String writes the reference back: by digest when it has one.
func (r Ref) String() string {
	switch r.Kind {
	case KindDir:
		return KindDir + ":" + r.Dir
	case KindOCI:
		s := KindOCI + "://" + r.Host + "/" + r.Repository
		if r.Digest != "" {
			return s + "@" + r.Digest
		}
		return s + ":" + r.Tag
	}
	return ""
}

// Sources are the kinds of reference a server accepts: SHPYRD_UI_SOURCES,
// a comma-separated list ("oci", "oci,dir"), or "off", under which
// workspaces keep their references and every door serves the platform's
// own.
type Sources struct {
	Off   bool
	kinds map[string]bool
}

// ParseSources reads the setting; "" means "oci".
func ParseSources(s string) (Sources, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		s = KindOCI
	}
	if s == "off" {
		return Sources{Off: true}, nil
	}
	out := Sources{kinds: map[string]bool{}}
	for _, k := range strings.Split(s, ",") {
		k = strings.TrimSpace(k)
		switch k {
		case KindOCI, KindDir:
			out.kinds[k] = true
		case "":
		default:
			return Sources{}, fmt.Errorf("SHPYRD_UI_SOURCES: unknown source %q (oci, dir, or off)", k)
		}
	}
	return out, nil
}

// Allows says a reference of this kind may be set and served.
func (s Sources) Allows(kind string) bool {
	return !s.Off && s.kinds[kind]
}

// String lists the sources as the setting would.
func (s Sources) String() string {
	if s.Off {
		return "off"
	}
	var out []string
	for _, k := range []string{KindOCI, KindDir} {
		if s.kinds[k] {
			out = append(out, k)
		}
	}
	return strings.Join(out, ",")
}
