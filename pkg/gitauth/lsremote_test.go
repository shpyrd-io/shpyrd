package gitauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	corev1 "k8s.io/api/core/v1"
)

const (
	mainSHA = "0123456789abcdef0123456789abcdef01234567"
	tagObj  = "f00df00df00df00df00df00df00df00df00df00d"
	tagSHA  = "89abcdef0123456789abcdef0123456789abcdef"
)

// pkt is one pkt-line: four hex digits of length, then the data.
func pkt(s string) string { return fmt.Sprintf("%04x%s", len(s)+4, s) }

// refLines are the refs a server advertises, the first with capabilities,
// and the flush that ends them.
func refLines(refs ...string) string {
	var b strings.Builder
	for i, r := range refs {
		if i == 0 {
			r += "\x00multi_ack thin-pack side-band-64k ofs-delta agent=git/2.47.0"
		}
		b.WriteString(pkt(r + "\n"))
	}
	b.WriteString("0000")
	return b.String()
}

var sampleRefs = []string{
	mainSHA + " HEAD",
	mainSHA + " refs/heads/main",
	tagSHA + " refs/heads/release",
	"1111111111111111111111111111111111111111 refs/pull/7/head",
	tagObj + " refs/tags/v1.2",
	tagSHA + " refs/tags/v1.2^{}",
}

// smartHTTP serves an advertisement the way a git host does.
func smartHTTP(t *testing.T, check func(*http.Request), body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLsRemoteHTTPWithAToken(t *testing.T) {
	srv := smartHTTP(t, func(r *http.Request) {
		if r.URL.Path != "/acme/shop.git/info/refs" || r.URL.Query().Get("service") != "git-upload-pack" {
			t.Errorf("asked %s", r.URL)
		}
		if r.Header.Get("Git-Protocol") != "" {
			t.Error("no protocol v2 header: the answer must be v0/v1")
		}
		if u, p, ok := r.BasicAuth(); !ok || u != "x-access-token" || p != "tok" {
			t.Errorf("basic auth %q %q %v", u, p, ok)
		}
	}, pkt("# service=git-upload-pack\n")+"0000"+refLines(sampleRefs...))
	b := Build{Kind: KindToken, Secret: &corev1.Secret{Data: map[string][]byte{
		corev1.BasicAuthUsernameKey: []byte("x-access-token"), corev1.BasicAuthPasswordKey: []byte("tok"),
	}}}
	refs, err := LsRemote(context.Background(), srv.URL+"/acme/shop", b)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"refs/heads/main": mainSHA, "refs/heads/release": tagSHA,
		"refs/tags/v1.2": tagObj, "refs/tags/v1.2^{}": tagSHA,
	}
	if len(refs) != len(want) {
		t.Fatalf("refs = %v", refs)
	}
	for k, v := range want {
		if refs[k] != v {
			t.Errorf("%s = %q, want %q", k, refs[k], v)
		}
	}
}

func TestLsRemoteHTTPRefused(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"401": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
		"403": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) },
		"404": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
		"sign-in page": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, "<html>Sign in</html>")
		},
	} {
		srv := httptest.NewServer(h)
		_, err := LsRemote(context.Background(), srv.URL+"/acme/shop", Build{})
		srv.Close()
		if !errors.Is(err, ErrNoAccess) {
			t.Errorf("%s: err = %v, want ErrNoAccess", name, err)
		}
	}
}

func TestLsRemoteRefusesWhatIsNotAnAdvertisement(t *testing.T) {
	for name, body := range map[string]string{
		"garbage":     "zzzz not git",
		"no service":  refLines(sampleRefs...),
		"bad sha":     pkt("# service=git-upload-pack\n") + "0000" + refLines("nothex refs/heads/main"),
		"cut short":   pkt("# service=git-upload-pack\n") + "0000" + pkt(mainSHA+" refs/heads/main\n"),
		"protocol v2": pkt("# service=git-upload-pack\n") + "0000" + pkt("version 2\n") + "0000",
	} {
		srv := smartHTTP(t, nil, body)
		_, err := LsRemote(context.Background(), srv.URL+"/acme/shop", Build{})
		if err == nil || errors.Is(err, ErrNoAccess) {
			t.Errorf("%s: err = %v, want a malformed answer", name, err)
		}
	}
}

func TestLsRemoteOfAnEmptyRepository(t *testing.T) {
	srv := smartHTTP(t, nil, pkt("# service=git-upload-pack\n")+"0000"+refLines("0000000000000000000000000000000000000000 capabilities^{}"))
	refs, err := LsRemote(context.Background(), srv.URL+"/acme/empty", Build{})
	if err != nil || len(refs) != 0 {
		t.Errorf("refs %v err %v", refs, err)
	}
}

// Review Focus 5: GitHub advertises every pull request's refs. Branches
// sort before refs/pull/, so a branch is found in a listing cut at the cap.
func TestLsRemoteFindsTheBranchPastTheCap(t *testing.T) {
	lines := []string{mainSHA + " HEAD", mainSHA + " refs/heads/main"}
	for n := 0; len(lines)*60 < MaxAdvertisement+(1<<20); n++ {
		lines = append(lines, fmt.Sprintf("%s refs/pull/%d/head", tagSHA, n))
	}
	lines = append(lines, tagObj+" refs/tags/v9")
	srv := smartHTTP(t, nil, pkt("# service=git-upload-pack\n")+"0000"+refLines(lines...))
	refs, err := LsRemote(context.Background(), srv.URL+"/acme/monorepo", Build{})
	if !errors.Is(err, ErrTooManyRefs) {
		t.Fatalf("err = %v, want ErrTooManyRefs", err)
	}
	if sha, ok := ResolveBranch(refs, "main"); !ok || sha != mainSHA {
		t.Errorf("main = %q %v", sha, ok)
	}
	if _, ok := ResolveBranch(refs, "v9"); ok {
		t.Error("a tag past the cap cannot be found")
	}
}

// Review Focus 4: an annotated tag's ref names the tag object; the commit
// is its peeled ^{} entry.
func TestResolveBranchPrefersTheBranchThenThePeeledTag(t *testing.T) {
	refs := map[string]string{
		"refs/heads/main": mainSHA, "refs/tags/main": tagSHA,
		"refs/tags/v1.2": tagObj, "refs/tags/v1.2^{}": tagSHA,
		"refs/tags/light": mainSHA,
	}
	for rev, want := range map[string]string{
		"main": mainSHA, "v1.2": tagSHA, "light": mainSHA,
		"refs/tags/v1.2": tagSHA, "refs/heads/main": mainSHA,
	} {
		if got, ok := ResolveBranch(refs, rev); !ok || got != want {
			t.Errorf("%s = %q %v, want %q", rev, got, ok, want)
		}
	}
	if _, ok := ResolveBranch(refs, "gone"); ok {
		t.Error("gone must not resolve")
	}
}

// sshGitServer answers `git-upload-pack` with adv, as a git host does, to
// the one client key it knows. It returns its address, its host key and
// the commands it was asked to run.
func sshGitServer(t *testing.T, client ssh.PublicKey, adv string) (string, ssh.Signer, chan string) {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	host, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if bytes.Equal(k.Marshal(), client.Marshal()) {
			return nil, nil
		}
		return nil, errors.New("unknown key")
	}}
	cfg.AddHostKey(host)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	cmds := make(chan string, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveUploadPack(c, cfg, adv, cmds)
		}
	}()
	return ln.Addr().String(), host, cmds
}

func serveUploadPack(c net.Conn, cfg *ssh.ServerConfig, adv string, cmds chan<- string) {
	defer c.Close()
	_, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "sessions only")
			continue
		}
		ch, chReqs, err := nc.Accept()
		if err != nil {
			return
		}
		go func() {
			for req := range chReqs {
				if req.Type != "exec" {
					_ = req.Reply(false, nil)
					continue
				}
				var p struct{ Command string }
				_ = ssh.Unmarshal(req.Payload, &p)
				cmds <- p.Command
				_ = req.Reply(true, nil)
				_, _ = io.WriteString(ch, adv)
				flush := make([]byte, 4)
				_, _ = io.ReadFull(ch, flush) // the client's "0000": it wants nothing
				_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				_ = ch.Close()
			}
		}()
	}
}

func deployKeyBuild(t *testing.T, knownHosts string, key DeployKey) Build {
	t.Helper()
	return Build{Kind: KindDeployKey, Secret: &corev1.Secret{Data: map[string][]byte{
		corev1.SSHAuthPrivateKey: key.PrivatePEM, KeyKnownHosts: []byte(knownHosts),
	}}}
}

func TestLsRemoteSSHWithADeployKey(t *testing.T) {
	key, err := GenerateDeployKey("shpyrd shop")
	if err != nil {
		t.Fatal(err)
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key.Public))
	if err != nil {
		t.Fatal(err)
	}
	addr, host, cmds := sshGitServer(t, pub, refLines(sampleRefs...))
	line := knownhosts.Line([]string{knownhosts.Normalize(addr)}, host.PublicKey())
	refs, err := LsRemote(context.Background(), "ssh://git@"+addr+"/acme/shop.git", deployKeyBuild(t, line, key))
	if err != nil {
		t.Fatal(err)
	}
	if refs["refs/heads/main"] != mainSHA {
		t.Errorf("refs = %v", refs)
	}
	if cmd := <-cmds; cmd != "git-upload-pack '/acme/shop.git'" {
		t.Errorf("ran %q", cmd)
	}
}

func TestLsRemoteSSHRefusesAnotherHostKey(t *testing.T) {
	key, _ := GenerateDeployKey("shpyrd shop")
	pub, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(key.Public))
	addr, _, _ := sshGitServer(t, pub, refLines(sampleRefs...))
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	otherSigner, _ := ssh.NewSignerFromKey(other)
	line := knownhosts.Line([]string{knownhosts.Normalize(addr)}, otherSigner.PublicKey())
	_, err := LsRemote(context.Background(), "ssh://git@"+addr+"/acme/shop.git", deployKeyBuild(t, line, key))
	if !errors.Is(err, ErrHostKeyChanged) {
		t.Errorf("err = %v, want ErrHostKeyChanged", err)
	}
}

func TestLsRemoteSSHWithAKeyTheHostDoesNotKnow(t *testing.T) {
	known, _ := GenerateDeployKey("known")
	pub, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(known.Public))
	addr, host, _ := sshGitServer(t, pub, refLines(sampleRefs...))
	stranger, _ := GenerateDeployKey("stranger")
	line := knownhosts.Line([]string{knownhosts.Normalize(addr)}, host.PublicKey())
	_, err := LsRemote(context.Background(), "ssh://git@"+addr+"/acme/shop.git", deployKeyBuild(t, line, stranger))
	if !errors.Is(err, ErrNoAccess) {
		t.Errorf("err = %v, want ErrNoAccess", err)
	}
}

func TestSSHPathAndUser(t *testing.T) {
	for _, tc := range []struct{ url, path, user string }{
		{"git@github.com:acme/shop.git", "acme/shop.git", "git"},
		{"ssh://git@git.example.com:2222/team/app.git", "/team/app.git", "git"},
		{"ssh://gitea@git.example.com/team/app", "/team/app.git", "gitea"},
		{"forge@host.example:me/site", "me/site.git", "forge"},
	} {
		r, err := Parse(tc.url)
		if err != nil {
			t.Fatal(err)
		}
		if got := sshPath(tc.url, r); got != tc.path {
			t.Errorf("%s: path %q, want %q", tc.url, got, tc.path)
		}
		if got := sshUser(tc.url); got != tc.user {
			t.Errorf("%s: user %q, want %q", tc.url, got, tc.user)
		}
	}
}

// An app whose URL carries its credentials (https://user:pass@host/...)
// kept working before shpyrd read the branch: the listing sends them.
func TestLsRemoteHTTPWithCredentialsInTheURL(t *testing.T) {
	srv := smartHTTP(t, func(r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "u" || p != "p" {
			t.Errorf("basic auth %q %q %v", u, p, ok)
		}
	}, pkt("# service=git-upload-pack\n")+"0000"+refLines(sampleRefs...))
	host := strings.TrimPrefix(srv.URL, "http://")
	refs, err := LsRemote(context.Background(), "http://u:p@"+host+"/org/repo", Build{})
	if err != nil || refs["refs/heads/main"] != mainSHA {
		t.Fatalf("refs %v err %v", refs, err)
	}
}

func TestLsRemoteNeverSaysTheURLsPassword(t *testing.T) {
	const pass = "hunter2secret"
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer failing.Close()
	gone := httptest.NewServer(http.NotFoundHandler())
	goneHost := strings.TrimPrefix(gone.URL, "http://")
	gone.Close()
	for _, host := range []string{strings.TrimPrefix(failing.URL, "http://"), goneHost} {
		_, err := LsRemote(context.Background(), "http://u:"+pass+"@"+host+"/org/repo", Build{})
		if err == nil || strings.Contains(err.Error(), pass) {
			t.Errorf("err = %v", err)
		}
	}
}

// Lines with a marker are not host keys to trust: a revoked key, or a
// certificate authority shpyrd does not check certificates against.
func TestKnownHostsSkipsMarkedLines(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	other, _ := ssh.NewSignerFromKey(otherPriv)
	good := knownhosts.Line([]string{"git.example.com"}, signer.PublicKey())
	revoked := "@revoked " + knownhosts.Line([]string{"git.example.com"}, other.PublicKey())
	ca := "@cert-authority " + knownhosts.Line([]string{"*"}, other.PublicKey())
	check, algos, err := knownHostsCheck([]byte(revoked + "\n" + ca + "\n" + good + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(algos) != 1 || algos[0] != ssh.KeyAlgoED25519 {
		t.Errorf("algorithms %v", algos)
	}
	if err := check("git.example.com:22", nil, other.PublicKey()); !errors.Is(err, ErrHostKeyChanged) {
		t.Errorf("a revoked key was accepted: %v", err)
	}
	if err := check("git.example.com:22", nil, signer.PublicKey()); err != nil {
		t.Errorf("the recorded key: %v", err)
	}
	if _, _, err := knownHostsCheck([]byte(revoked + "\n" + ca + "\n")); err == nil {
		t.Error("only marked lines: no host key was recorded")
	}
}

// A listing over SSH stops when its context does, even while the host
// says nothing.
func TestLsRemoteSSHStopsWithItsContext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close() // accepts, and never answers
		}
	}()
	key, _ := GenerateDeployKey("shpyrd shop")
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	host, _ := ssh.NewSignerFromKey(priv)
	line := knownhosts.Line([]string{knownhosts.Normalize(ln.Addr().String())}, host.PublicKey())
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	start := time.Now()
	if _, err := LsRemote(ctx, "ssh://git@"+ln.Addr().String()+"/acme/shop.git", deployKeyBuild(t, line, key)); err == nil {
		t.Error("want an error")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("returned after %v", d)
	}
}
