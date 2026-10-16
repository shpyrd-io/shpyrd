// Package gitauth is how builds reach private git repositories
// (RFC-0017): a repository's URL in the forms a credential needs, the
// deploy keys shpyrd generates, and the choice of credential a build
// clones with.
package gitauth

import (
	"fmt"
	"net/url"
	"strings"
)

// Repo is a git URL taken apart.
type Repo struct {
	// Scheme is how the URL was given: https, http or ssh.
	Scheme string
	// Host is the host name, without the port.
	Host string
	// Port is the port when the URL names one: the HTTP port for http(s),
	// the SSH port for ssh.
	Port string
	// Path is the repository's path, no leading slash and no .git.
	Path string
}

// Parse reads https://host/org/repo(.git), http://..., ssh://[user@]host[:port]/org/repo
// and the scp form user@host:org/repo.
func Parse(raw string) (Repo, error) {
	raw = strings.TrimSpace(raw)
	bad := fmt.Errorf("%q is not a git URL: give https://host/org/repo or git@host:org/repo", raw)
	if raw == "" {
		return Repo{}, bad
	}
	var r Repo
	if !strings.Contains(raw, "://") {
		// scp form: user@host:path
		at := strings.Index(raw, "@")
		colon := strings.Index(raw, ":")
		if at < 1 || colon < at {
			return Repo{}, bad
		}
		r = Repo{Scheme: "ssh", Host: raw[at+1 : colon], Path: raw[colon+1:]}
	} else {
		u, err := url.Parse(raw)
		if err != nil {
			return Repo{}, bad
		}
		switch u.Scheme {
		case "https", "http", "ssh":
		default:
			return Repo{}, bad
		}
		r = Repo{Scheme: u.Scheme, Host: u.Hostname(), Port: u.Port(), Path: u.Path}
	}
	r.Host = strings.ToLower(r.Host)
	r.Path = strings.TrimSuffix(strings.Trim(r.Path, "/"), ".git")
	if r.Host == "" || r.Path == "" {
		return Repo{}, bad
	}
	return r, nil
}

// HTTPS is the URL to clone with a token: https, or http when the URL was
// http. The port is kept only for http(s) URLs; an ssh port means nothing
// over HTTP.
func (r Repo) HTTPS() string {
	scheme, host := "https", r.Host
	if r.Scheme == "http" {
		scheme = "http"
	}
	if r.Scheme != "ssh" && r.Port != "" {
		host += ":" + r.Port
	}
	return scheme + "://" + host + "/" + r.Path + ".git"
}

// SSH is the URL to clone with a deploy key: the scp form, or ssh:// when
// the SSH port is not 22.
func (r Repo) SSH() string {
	if r.Scheme == "ssh" && r.Port != "" && r.Port != "22" {
		return "ssh://git@" + r.Host + ":" + r.Port + "/" + r.Path + ".git"
	}
	return "git@" + r.Host + ":" + r.Path + ".git"
}

// Display is host/path, for messages.
func (r Repo) Display() string { return r.Host + "/" + r.Path }
