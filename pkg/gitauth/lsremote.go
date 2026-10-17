package gitauth

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	corev1 "k8s.io/api/core/v1"
)

// Reading a branch's commit (RFC-0018). The server image has no git, so
// the ref advertisement a clone starts with is read here, over smart HTTP
// or SSH, in protocol v0/v1: no Git-Protocol header is sent.

var (
	// ErrNoAccess: the host refused the listing, or has no repository of
	// that name.
	ErrNoAccess = errors.New("the git host refused access to the repository, or it does not exist under that name")
	// ErrHostKeyChanged: the SSH host presented a key other than the one
	// recorded with the deploy key.
	ErrHostKeyChanged = errors.New("the git host's SSH host key does not match the one recorded with the deploy key")
	// ErrTooManyRefs: the advertisement passed MaxAdvertisement. The refs
	// read until then come with it; branches sort before pull requests'
	// and tags, so a branch is usually among them.
	ErrTooManyRefs = errors.New("the repository lists more refs than shpyrd reads")

	errMalformed = errors.New("the git host's answer is not a ref advertisement shpyrd can read")
	// errHostError: the host answered with a status other than refusal.
	errHostError = errors.New("the git host answered with an error")
)

// notFound is the host's 404: no such repository, or one this credential
// may not see (hosts answer 404 for both). It counts as ErrNoAccess.
type notFound struct{}

func (notFound) Error() string        { return "the repository was not found" }
func (notFound) Is(target error) bool { return target == ErrNoAccess }

const (
	// MaxAdvertisement caps what one listing reads.
	MaxAdvertisement = 4 << 20
	lsRemoteTimeout  = 20 * time.Second
)

var httpClient = newHTTPClient()

// LsRemote lists the branches and tags of the repository at cloneURL, as
// `git ls-remote` does, with the credential b carries: over SSH with a
// deploy key, else over HTTP(S) with the token or none. It returns ref
// name to commit, for refs/heads/ and refs/tags/ only.
func LsRemote(ctx context.Context, cloneURL string, b Build) (map[string]string, error) {
	r, err := Parse(cloneURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, lsRemoteTimeout)
	defer cancel()
	if r.Scheme == "ssh" && b.Kind == KindDeployKey && b.Secret != nil {
		return lsRemoteSSH(ctx, strings.TrimSpace(cloneURL), r, b.Secret)
	}
	// A token (the workspace's or the GitHub App's), or nothing (a public
	// repository): HTTPS, whatever form the
	// URL was given in.
	return lsRemoteHTTP(ctx, strings.TrimSpace(cloneURL), r, b)
}

func lsRemoteHTTP(ctx context.Context, cloneURL string, r Repo, b Build) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.HTTPS()+"/info/refs?service=git-upload-pack", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "git/2.47.0 (shpyrd)")
	if (b.Kind == KindToken || b.Kind == KindGitHubApp) && b.Secret != nil {
		req.SetBasicAuth(string(b.Secret.Data[corev1.BasicAuthUsernameKey]), string(b.Secret.Data[corev1.BasicAuthPasswordKey]))
	} else if user, pass, ok := urlCredentials(cloneURL); b.Kind == "" && ok {
		// Credentials written into the app's URL, as builds used them
		// before shpyrd kept tokens. The request's URL never carries
		// them, so no error names them.
		req.SetBasicAuth(user, pass)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		if refused := refusal(r.Host, err); refused != nil {
			return nil, refused
		}
		if errors.Is(err, errRedirect) {
			// A sign-in page on another host, most often.
			return nil, ErrNoAccess
		}
		return nil, fmt.Errorf("could not reach %s: %w", r.Host, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return nil, ErrNoAccess
	case resp.StatusCode == http.StatusNotFound:
		return nil, notFound{}
	case resp.StatusCode != http.StatusOK:
		return nil, errHostError
	case strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html"):
		// A sign-in page the host redirected to.
		return nil, ErrNoAccess
	}
	return readAdvertisement(resp.Body, true)
}

func lsRemoteSSH(ctx context.Context, cloneURL string, r Repo, sec *corev1.Secret) (map[string]string, error) {
	signer, err := ssh.ParsePrivateKey(sec.Data[corev1.SSHAuthPrivateKey])
	if err != nil {
		return nil, fmt.Errorf("the deploy key does not parse: make a new one with shpyrd git-auth deploy-key: %w", err)
	}
	check, algorithms, err := knownHostsCheck(sec.Data[KeyKnownHosts])
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(r.Host, firstNonEmpty(r.Port, "22"))
	conn, err := newDialer().DialContext(ctx, "tcp", addr)
	if err != nil {
		if refused := refusal(r.Host, err); refused != nil {
			return nil, refused
		}
		return nil, fmt.Errorf("could not reach %s: %w", addr, err)
	}
	defer conn.Close()
	// The deadline bounds each read; a cancelled context closes the
	// connection, which ends any read at once.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	cfg := &ssh.ClientConfig{
		User:              sshUser(cloneURL),
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback:   check,
		HostKeyAlgorithms: algorithms,
	}
	cc, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		switch {
		case errors.Is(err, ErrHostKeyChanged) || strings.Contains(err.Error(), ErrHostKeyChanged.Error()):
			return nil, ErrHostKeyChanged
		case strings.Contains(err.Error(), "unable to authenticate"):
			return nil, ErrNoAccess
		}
		return nil, fmt.Errorf("could not reach %s: %w", addr, err)
	}
	client := ssh.NewClient(cc, chans, reqs)
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("could not open a session on %s: %w", addr, err)
	}
	defer sess.Close()
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := sess.Start("git-upload-pack " + quote(sshPath(cloneURL, r))); err != nil {
		return nil, fmt.Errorf("could not list the repository on %s: %w", addr, err)
	}
	refs, err := readAdvertisement(stdout, false)
	if err != nil && !errors.Is(err, ErrTooManyRefs) {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			// The host hung up without a ref: no such repository, or this
			// key may not read it.
			return nil, ErrNoAccess
		}
		return nil, err
	}
	// A flush: this client wants nothing, and the host hangs up.
	_, _ = io.WriteString(stdin, "0000")
	_ = stdin.Close()
	return refs, err
}

// knownHostsCheck accepts the host keys recorded with the deploy key, and
// asks the host for their algorithms so it presents one of them. The
// lines are parsed here: there is no known_hosts file.
func knownHostsCheck(data []byte) (ssh.HostKeyCallback, []string, error) {
	type entry struct {
		hosts []string
		key   ssh.PublicKey
	}
	var entries []entry
	var algorithms []string
	rest := data
	for {
		marker, hosts, key, _, next, err := ssh.ParseKnownHosts(rest)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("the host key recorded with the deploy key does not parse: make a new deploy key with shpyrd git-auth deploy-key: %w", err)
		}
		if marker != "" {
			// @revoked or @cert-authority: not a key to trust as the host's.
			rest = next
			continue
		}
		entries = append(entries, entry{hosts, key})
		algorithms = append(algorithms, algorithmsFor(key.Type())...)
		rest = next
	}
	if len(entries) == 0 {
		return nil, nil, errors.New("no host key was recorded with the deploy key: make a new one with shpyrd git-auth deploy-key")
	}
	check := func(hostname string, _ net.Addr, key ssh.PublicKey) error {
		want := knownhosts.Normalize(hostname)
		for _, e := range entries {
			for _, h := range e.hosts {
				if knownhosts.Normalize(h) == want && bytes.Equal(e.key.Marshal(), key.Marshal()) {
					return nil
				}
			}
		}
		return ErrHostKeyChanged
	}
	return check, algorithms, nil
}

func algorithmsFor(keyType string) []string {
	if keyType == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	}
	return []string{keyType}
}

// urlCredentials is the user and password an http(s) URL carries.
func urlCredentials(cloneURL string) (string, string, bool) {
	u, err := url.Parse(cloneURL)
	if err != nil || u.User == nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", "", false
	}
	pass, ok := u.User.Password()
	if !ok {
		return "", "", false
	}
	return u.User.Username(), pass, true
}

// sshPath is the repository's path as upload-pack wants it: org/repo.git
// for the scp form, /org/repo.git for an ssh:// URL.
func sshPath(cloneURL string, r Repo) string {
	if strings.Contains(cloneURL, "://") {
		return "/" + r.Path + ".git"
	}
	return r.Path + ".git"
}

// sshUser is the URL's user, git when it names none.
func sshUser(cloneURL string) string {
	if strings.Contains(cloneURL, "://") {
		if u, err := url.Parse(cloneURL); err == nil && u.User != nil && u.User.Username() != "" {
			return u.User.Username()
		}
		return "git"
	}
	if at := strings.Index(cloneURL, "@"); at > 0 {
		return cloneURL[:at]
	}
	return "git"
}

// quote single-quotes s for the remote shell. Parse already refused
// anything but [A-Za-z0-9._~-] and slashes in the path.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// readAdvertisement reads the refs a server advertises, up to the flush
// that ends them. Over smart HTTP they follow a "# service=" line and a
// flush.
func readAdvertisement(body io.Reader, smartHTTP bool) (map[string]string, error) {
	cr := &countingReader{r: io.LimitReader(body, MaxAdvertisement)}
	br := bufio.NewReader(cr)
	cut := func(err error) error {
		if cr.n >= MaxAdvertisement {
			return ErrTooManyRefs
		}
		return err
	}
	if smartHTTP {
		line, flush, err := readPkt(br)
		if err != nil {
			return nil, cut(err)
		}
		if flush || !strings.HasPrefix(string(line), "# service=git-upload-pack") {
			return nil, errMalformed
		}
		if _, flush, err = readPkt(br); err != nil || !flush {
			return nil, errMalformed
		}
	}
	refs := map[string]string{}
	seen := false
	for {
		line, flush, err := readPkt(br)
		switch {
		case err != nil && errors.Is(cut(err), ErrTooManyRefs):
			return refs, ErrTooManyRefs
		case err != nil && !seen && !smartHTTP:
			return nil, err // nothing at all: the SSH host hung up
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			return nil, errMalformed // cut short, without the flush
		case err != nil:
			return nil, err
		case flush:
			return refs, nil
		}
		seen = true
		s := strings.TrimSuffix(string(line), "\n")
		if i := strings.IndexByte(s, 0); i >= 0 {
			s = s[:i] // the capabilities, after the first ref
		}
		switch {
		case strings.HasPrefix(s, "ERR "):
			return nil, fmt.Errorf("%w: %s", ErrNoAccess, strings.TrimPrefix(s, "ERR "))
		case s == "version 2":
			return nil, fmt.Errorf("%w: the host answered in protocol v2", errMalformed)
		case s == "version 1":
			continue
		}
		sha, name, ok := strings.Cut(s, " ")
		if !ok || !isHexSHA(sha) {
			return nil, errMalformed
		}
		if strings.HasPrefix(name, "refs/heads/") || strings.HasPrefix(name, "refs/tags/") {
			refs[name] = sha
		}
	}
}

// readPkt reads one pkt-line; flush is the "0000" that ends a section.
func readPkt(r *bufio.Reader) (line []byte, flush bool, err error) {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, false, err
	}
	n, err := strconv.ParseUint(string(head[:]), 16, 16)
	if err != nil {
		return nil, false, errMalformed
	}
	switch {
	case n == 0:
		return nil, true, nil
	case n < 4:
		// 0001 and 0002 are protocol v2's delimiters.
		return nil, false, errMalformed
	}
	line = make([]byte, n-4)
	if _, err := io.ReadFull(r, line); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, false, err
	}
	return line, false, nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// ResolveBranch finds the commit revision names among refs: the branch,
// else the tag, its peeled commit (^{}) when the tag is annotated. A full
// ref name (refs/...) is looked up as given.
func ResolveBranch(refs map[string]string, revision string) (string, bool) {
	names := []string{"refs/heads/" + revision, "refs/tags/" + revision + "^{}", "refs/tags/" + revision}
	if strings.HasPrefix(revision, "refs/") {
		names = []string{revision + "^{}", revision}
	}
	for _, n := range names {
		if sha, ok := refs[n]; ok {
			return sha, true
		}
	}
	return "", false
}

// isHexSHA says s is a SHA-1 or SHA-256 object name.
func isHexSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
