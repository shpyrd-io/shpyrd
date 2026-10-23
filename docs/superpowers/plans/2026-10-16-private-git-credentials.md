# Private git repositories, PR 1: git credentials (RFC-0017) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A project deploys from a private git repository on any host, with a
workspace token for the host or a deploy key shpyrd generates for the project,
for buildpack and Dockerfile builds alike.

**Architecture:** A new pure package `pkg/gitauth` parses git URLs, generates
deploy keys, records SSH host keys and decides which credential a build uses.
The API stores credentials as Secrets: tokens in `shpyrd-system`, labelled by
workspace; the deploy key in the project namespace. Before each build the App
controller writes the Secret `<app>-git` into the project namespace. kpack
reads it through the `shpyrd-builder` ServiceAccount and the BuildKit fetch
container through `.netrc` or `GIT_SSH_COMMAND`. A clone refused for lack of
access fails the build with a reason that names the fix. `shpyrd git-auth`
drives it from the CLI.

**Tech Stack:** Go, controller-runtime, gin, cobra, `golang.org/x/crypto/ssh`
(already in `go.mod`, v0.55.0), kpack, rootless BuildKit.

**Spec:** `docs/superpowers/specs/2026-10-16-private-git-repositories-design.md`
(this plan is delivery item 1 of 4; the watcher, the GitHub App and its events
get plans of their own when their turn comes).

## Global Constraints

- The brand is written `shpyrd`, lowercase, everywhere, including docs and CLI
  output.
- Credential values are write-only: no API response, CLI output, log line or
  audit detail carries a token or a private key.
- Lookup order, from the spec: GitHub App installation (not in this PR), then
  the project's deploy key, then the workspace's token for the host.
- Workspace-scoped objects live in the system namespace, labelled
  `shpyrd.io/workspace` and named with the workspace in them (the drains
  pattern, `pkg/api/drains.go`).
- Token routes need `authz.ClusterAdmin` (the workspace admin, as
  `/workspace/drains`); deploy-key routes need `authz.ProjectConfig`; reads
  need `authz.ProjectView`.
- The CLI's `git-auth` works signed in (`shpyrd login`) through the API; over a
  kubeconfig it says to sign in.
- Messages are plain sentences that name the fix, in the style of
  `internal/controller/build_failure.go`.
- Every commit ends with:
  ```
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb
  ```
- The PR carries screenshots of the feature in use (Task 8).

## Deviations from the spec, deliberate

- The spec has a SQL table `git_credentials`. This plan keeps credentials as
  labelled Secrets instead, as log drains and global config vars do. The store
  would hold only a name, a host and dates, which the Secret's labels and
  annotations already carry. That saves a migration and both store backends,
  and the controller reads Secrets anyway. `github_installations` (PR 3) still
  goes in the SQL store, because it is looked up by installation id across
  workspaces.
- `SourceReady=False` with a reason arrives with the watcher (PR 2). In this
  PR a clone without access fails the build, and the build's failure message
  carries the same reason.
- The host key of a deploy key's host is recorded when the key is generated,
  trust on first use, and shown with its fingerprint to compare. No host keys
  are compiled in.
- kpack checks no SSH host key (its git client does not). The Dockerfile path
  pins it. Noted in the docs.

## Review Focus

1. **A URL in another form than the credential needs.** An `https://` URL with
   only a deploy key must clone over SSH, and a `git@host:org/repo` URL with
   only a token must clone over HTTPS. Test: `TestResolveConvertsURLForm`
   (Task 1).
2. **A token or key containing shell-special characters** (`'`, `$`, newline)
   reaching the fetch script. Test: `TestFetchScriptNeverInlinesSecrets`
   (Task 5). The script reads files and never interpolates values.
3. **Two workspaces with a token for the same host**, and hosts that
   differ only in punctuation (`a-b.com` and `a.b-com`). Test:
   `TestTokenSecretNameDistinct` (Task 1) and the workspace check in
   `TestReconcileGitCredentialsPicksWorkspaceToken` (Task 4).
4. **A credential removed after a build used it.** The `<app>-git` Secret
   and its ServiceAccount reference must go, or kpack keeps a stale reference.
   Test: `TestReconcileGitCredentialsCleansUp` (Task 4).
5. **The SSH private key's file mode.** ssh refuses a group-readable key, and
   Secret volumes with `fsGroup` are group-readable. The fetch script copies
   the key to `0600`. Test: an assertion in
   `TestDockerfileFetchWithDeployKey` (Task 5).

---

## File structure

| File | Responsibility |
|---|---|
| `pkg/gitauth/repo.go` (new) | Parse git URLs; render them as HTTPS or SSH |
| `pkg/gitauth/keys.go` (new) | Generate ed25519 deploy keys; record a host's SSH key |
| `pkg/gitauth/resolve.go` (new) | Names, labels, and choosing the credential a build uses (renders `<app>-git`) |
| `pkg/gitauth/*_test.go` (new) | Unit tests for the above |
| `pkg/api/gitauth.go` (new) | Routes for workspace tokens and project deploy keys |
| `pkg/api/gitauth_test.go` (new) | API tests |
| `pkg/api/server.go` (modify, near line 655 and 784) | Register the routes |
| `internal/controller/gitcreds.go` (new) | `reconcileGitCredentials`, `ensureBuildServiceAccount` |
| `internal/controller/gitcreds_test.go` (new) | Controller tests |
| `internal/controller/registry.go` (modify) | Drop the ServiceAccount from the registry mirror |
| `internal/controller/desired.go` (modify, 168-179, 573-610) | The SA name and the kpack source take the resolved credential |
| `internal/controller/dockerfile.go` (modify, 52-80, 342-458, 476-500) | Mount `<app>-git` in the fetch container; the script uses it |
| `internal/controller/app_controller.go` (modify, 346, 377, 401, 659-666) | Call the new reconcile step; pass the credential |
| `internal/controller/globals.go` (modify, 137-159) | Credential Secret events reach their Apps |
| `internal/controller/build_failure.go` (modify, 58-116) | A refused clone gets a reason that names the fix |
| `internal/cli/gitauth.go` (new), `internal/cli/gitauth_test.go` (new) | `shpyrd git-auth` |
| `internal/cli/root.go` (modify, line 130) | Register the command |
| `content/docs/deploying.mdx`, `content/docs/cli.mdx` (modify) | Docs |
| `rfcs/0017-git-credentials.md`, `rfcs/README.md` (modify) | Status and history |

---

### Task 1: `pkg/gitauth`: URLs, names, and choosing a credential

**Files:**
- Create: `pkg/gitauth/repo.go`, `pkg/gitauth/resolve.go`
- Test: `pkg/gitauth/repo_test.go`, `pkg/gitauth/resolve_test.go`

**Interfaces:**
- Produces:
  - `type Repo struct { Scheme, Host, Port, Path string }`
  - `func Parse(raw string) (Repo, error)`
  - `func (r Repo) HTTPS() string`
  - `func (r Repo) SSH() string`
  - `func (r Repo) Display() string`
  - `func TokenSecretName(workspace, host string) string`
  - `func DefaultUsername(host string) string`
  - `const KindToken = "token"`, `KindDeployKey = "deploy-key"`
  - `const LabelCredential = "shpyrd.io/git-credential"`, `AnnotationHost = "shpyrd.io/git-host"`, `AnnotationPublicKey = "shpyrd.io/git-public-key"`, `AnnotationFingerprint = "shpyrd.io/git-key-fingerprint"`, `AnnotationHostKeyFingerprint = "shpyrd.io/git-host-key-fingerprint"`, `AnnotationUpdatedBy = "shpyrd.io/updated-by"`
  - `const DeployKeySecretName = "shpyrd-git-deploy-key"`, `KeyToken = "token"`, `KeyUsername = "username"`, `KeyKnownHosts = "known_hosts"`
  - `func BuildSecretName(app string) string` (returns `app + "-git"`)
  - `type Build struct { Kind, CloneURL, Host string; Secret *corev1.Secret }`
  - `func Resolve(gitURL, app, namespace string, deployKey, token *corev1.Secret) (Build, error)`

- [ ] **Step 1: Write the failing tests**

`pkg/gitauth/repo_test.go`:

```go
package gitauth

import "testing"

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		in, scheme, host, port, path, https, ssh string
	}{
		{"https://github.com/acme/api", "https", "github.com", "", "acme/api", "https://github.com/acme/api.git", "git@github.com:acme/api.git"},
		{"https://github.com/acme/api.git", "https", "github.com", "", "acme/api", "https://github.com/acme/api.git", "git@github.com:acme/api.git"},
		{"git@github.com:acme/api.git", "ssh", "github.com", "", "acme/api", "https://github.com/acme/api.git", "git@github.com:acme/api.git"},
		{"ssh://git@git.example.com:2222/team/app.git", "ssh", "git.example.com", "2222", "team/app", "https://git.example.com/team/app.git", "ssh://git@git.example.com:2222/team/app.git"},
		{"http://gitea.local:3000/me/site", "http", "gitea.local", "3000", "me/site", "http://gitea.local:3000/me/site.git", "git@gitea.local:me/site.git"},
		{"https://gitlab.com/group/sub/project", "https", "gitlab.com", "", "group/sub/project", "https://gitlab.com/group/sub/project.git", "git@gitlab.com:group/sub/project.git"},
	} {
		r, err := Parse(tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if r.Scheme != tc.scheme || r.Host != tc.host || r.Port != tc.port || r.Path != tc.path {
			t.Errorf("%s: got %+v", tc.in, r)
		}
		if got := r.HTTPS(); got != tc.https {
			t.Errorf("%s: HTTPS() = %s, want %s", tc.in, got, tc.https)
		}
		if got := r.SSH(); got != tc.ssh {
			t.Errorf("%s: SSH() = %s, want %s", tc.in, got, tc.ssh)
		}
	}
}

func TestParseRefuses(t *testing.T) {
	for _, in := range []string{"", "github.com/acme/api", "ftp://x/y", "https://github.com/", "git@github.com"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("%q: want an error", in)
		}
	}
}
```

`pkg/gitauth/resolve_test.go`:

```go
package gitauth

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func tokenSecret(host, token, user string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: TokenSecretName("default", host), Namespace: "shpyrd-system",
			Labels: map[string]string{LabelCredential: KindToken}, Annotations: map[string]string{AnnotationHost: host}},
		Data: map[string][]byte{KeyToken: []byte(token), KeyUsername: []byte(user)},
	}
}

func deployKeySecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: DeployKeySecretName, Namespace: "app-shop", Labels: map[string]string{LabelCredential: KindDeployKey}},
		Type:       corev1.SecretTypeSSHAuth,
		Data:       map[string][]byte{corev1.SSHAuthPrivateKey: []byte("PRIVATE"), KeyKnownHosts: []byte("github.com ssh-ed25519 AAAA")},
	}
}

func TestResolveNothing(t *testing.T) {
	b, err := Resolve("https://github.com/acme/api", "shop", "app-shop", nil, nil)
	if err != nil || b.Kind != "" || b.Secret != nil || b.CloneURL != "https://github.com/acme/api" {
		t.Fatalf("got %+v, %v", b, err)
	}
}

func TestResolveDeployKeyWinsOverToken(t *testing.T) {
	b, err := Resolve("https://github.com/acme/api", "shop", "app-shop", deployKeySecret(), tokenSecret("github.com", "ghp_x", ""))
	if err != nil {
		t.Fatal(err)
	}
	if b.Kind != KindDeployKey || b.CloneURL != "git@github.com:acme/api.git" {
		t.Fatalf("got %+v", b)
	}
	s := b.Secret
	if s.Name != "shop-git" || s.Namespace != "app-shop" || s.Type != corev1.SecretTypeSSHAuth {
		t.Fatalf("secret = %+v", s.ObjectMeta)
	}
	if s.Annotations["kpack.io/git"] != "git@github.com" || string(s.Data[corev1.SSHAuthPrivateKey]) != "PRIVATE" || string(s.Data[KeyKnownHosts]) == "" {
		t.Errorf("secret = %+v", s)
	}
}

func TestResolveToken(t *testing.T) {
	b, err := Resolve("https://github.com/acme/api", "shop", "app-shop", nil, tokenSecret("github.com", "ghp_x", ""))
	if err != nil {
		t.Fatal(err)
	}
	s := b.Secret
	if b.Kind != KindToken || b.CloneURL != "https://github.com/acme/api.git" || s.Type != corev1.SecretTypeBasicAuth {
		t.Fatalf("got %+v", b)
	}
	if s.Annotations["kpack.io/git"] != "https://github.com" || string(s.Data[corev1.BasicAuthUsernameKey]) != "x-access-token" || string(s.Data[corev1.BasicAuthPasswordKey]) != "ghp_x" {
		t.Errorf("secret = %+v", s)
	}
}

func TestResolveConvertsURLForm(t *testing.T) {
	b, _ := Resolve("git@gitlab.com:group/app.git", "shop", "app-shop", nil, tokenSecret("gitlab.com", "glpat", ""))
	if b.CloneURL != "https://gitlab.com/group/app.git" || string(b.Secret.Data[corev1.BasicAuthUsernameKey]) != "oauth2" {
		t.Errorf("ssh URL with a token must clone over https as oauth2: %+v", b)
	}
	b, _ = Resolve("http://gitea.local:3000/me/site", "shop", "app-shop", nil, tokenSecret("gitea.local", "t", "me"))
	if b.CloneURL != "http://gitea.local:3000/me/site.git" || b.Secret.Annotations["kpack.io/git"] != "http://gitea.local:3000" || string(b.Secret.Data[corev1.BasicAuthUsernameKey]) != "me" {
		t.Errorf("http with port keeps both: %+v", b)
	}
}

func TestResolveRefusesABadURL(t *testing.T) {
	if _, err := Resolve("not a url", "shop", "app-shop", nil, nil); err == nil || !strings.Contains(err.Error(), "not a git URL") {
		t.Errorf("err = %v", err)
	}
}

func TestTokenSecretNameDistinct(t *testing.T) {
	a, b := TokenSecretName("acme", "a-b.com"), TokenSecretName("acme", "a.b-com")
	if a == b {
		t.Fatalf("hosts that differ in punctuation share %s", a)
	}
	if TokenSecretName("acme", "github.com") == TokenSecretName("other", "github.com") {
		t.Fatal("workspaces must not share a token Secret")
	}
	long := TokenSecretName(strings.Repeat("w", 63), strings.Repeat("h", 250)+".com")
	if len(long) > 253 || strings.ToLower(long) != long {
		t.Errorf("not a DNS-1123 name: %s", long)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./pkg/gitauth/...`
Expected: FAIL, `undefined: Parse` and the rest.

- [ ] **Step 3: Write `pkg/gitauth/repo.go`**

```go
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
```

- [ ] **Step 4: Write `pkg/gitauth/resolve.go`**

```go
package gitauth

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Credential kinds.
const (
	KindToken     = "token"
	KindDeployKey = "deploy-key"
)

// Labels, annotations and keys of the credential Secrets.
const (
	// LabelCredential marks a Secret as a git credential, by kind.
	LabelCredential = "shpyrd.io/git-credential"
	// AnnotationHost is the host a token is for, or a deploy key's host.
	AnnotationHost = "shpyrd.io/git-host"
	// AnnotationPublicKey is a deploy key's public half, authorized_keys form.
	AnnotationPublicKey = "shpyrd.io/git-public-key"
	// AnnotationFingerprint is the deploy key's SHA256 fingerprint.
	AnnotationFingerprint = "shpyrd.io/git-key-fingerprint"
	// AnnotationHostKeyFingerprint is the host key recorded with a deploy key.
	AnnotationHostKeyFingerprint = "shpyrd.io/git-host-key-fingerprint"
	// AnnotationUpdatedBy is who set the credential, for people to read.
	AnnotationUpdatedBy = "shpyrd.io/updated-by"

	// DeployKeySecretName is a project's deploy key, in its namespace.
	DeployKeySecretName = "shpyrd-git-deploy-key"

	KeyToken      = "token"
	KeyUsername   = "username"
	KeyKnownHosts = "known_hosts"

	// kpackGitAnnotation tells kpack which URLs a Secret is for.
	kpackGitAnnotation = "kpack.io/git"
)

var notName = regexp.MustCompile(`[^a-z0-9]+`)

// TokenSecretName is the Secret in the system namespace holding a
// workspace's token for a host. A hash of the host keeps a-b.com and
// a.b-com apart.
func TokenSecretName(workspace, host string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(host)))
	slug := strings.Trim(notName.ReplaceAllString(strings.ToLower(host), "-"), "-")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	return "shpyrd-git-token-" + workspace + "-" + slug + "-" + hex.EncodeToString(sum[:4])
}

// BuildSecretName is the Secret a project's builds clone with.
func BuildSecretName(app string) string { return app + "-git" }

// DefaultUsername is the user name a host expects beside a token.
func DefaultUsername(host string) string {
	switch host {
	case "gitlab.com":
		return "oauth2"
	case "bitbucket.org":
		return "x-token-auth"
	}
	return "x-access-token"
}

// Build is what a build needs to clone a project's repository.
type Build struct {
	// Kind is KindDeployKey, KindToken, or "" when the clone goes without
	// credentials (a public repository).
	Kind string
	// CloneURL is the URL in the form the credential needs.
	CloneURL string
	// Host is the repository's host.
	Host string
	// Secret is <app>-git for the project namespace; nil without a credential.
	Secret *corev1.Secret
}

// Resolve chooses the credential a build of gitURL clones with: the
// project's deploy key, else the workspace's token for the host, else none.
// deployKey and token may be nil.
func Resolve(gitURL, app, namespace string, deployKey, token *corev1.Secret) (Build, error) {
	r, err := Parse(gitURL)
	if err != nil {
		return Build{}, err
	}
	meta := metav1.ObjectMeta{
		Name:      BuildSecretName(app),
		Namespace: namespace,
		Labels:    map[string]string{"app.kubernetes.io/managed-by": "shpyrd", "shpyrd.io/project": app},
	}
	switch {
	case deployKey != nil:
		meta.Annotations = map[string]string{kpackGitAnnotation: "git@" + r.Host}
		return Build{Kind: KindDeployKey, CloneURL: r.SSH(), Host: r.Host, Secret: &corev1.Secret{
			ObjectMeta: meta,
			Type:       corev1.SecretTypeSSHAuth,
			Data: map[string][]byte{
				corev1.SSHAuthPrivateKey: deployKey.Data[corev1.SSHAuthPrivateKey],
				KeyKnownHosts:            deployKey.Data[KeyKnownHosts],
			},
		}}, nil
	case token != nil:
		prefix := strings.TrimSuffix(r.HTTPS(), "/"+r.Path+".git")
		meta.Annotations = map[string]string{kpackGitAnnotation: prefix}
		user := string(token.Data[KeyUsername])
		if user == "" {
			user = DefaultUsername(r.Host)
		}
		return Build{Kind: KindToken, CloneURL: r.HTTPS(), Host: r.Host, Secret: &corev1.Secret{
			ObjectMeta: meta,
			Type:       corev1.SecretTypeBasicAuth,
			Data: map[string][]byte{
				corev1.BasicAuthUsernameKey: []byte(user),
				corev1.BasicAuthPasswordKey: token.Data[KeyToken],
			},
		}}, nil
	}
	return Build{CloneURL: gitURL, Host: r.Host}, nil
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./pkg/gitauth/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/gitauth
git commit -m "feat(gitauth): git URLs, credential names, and the choice a build clones with (RFC-0017)"
```

---

### Task 2: `pkg/gitauth`: deploy keys and host keys

**Files:**
- Create: `pkg/gitauth/keys.go`
- Test: `pkg/gitauth/keys_test.go`

**Interfaces:**
- Produces:
  - `type DeployKey struct { PrivatePEM []byte; Public, Fingerprint string }`
  - `func GenerateDeployKey(comment string) (DeployKey, error)`
  - `func ScanHostKey(ctx context.Context, host, port string) (knownHostsLine, fingerprint string, err error)`

- [ ] **Step 1: Write the failing tests**

```go
package gitauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestGenerateDeployKey(t *testing.T) {
	k, err := GenerateDeployKey("shpyrd shop")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(k.Public, "ssh-ed25519 ") || !strings.HasSuffix(k.Public, " shpyrd shop") {
		t.Errorf("public = %q", k.Public)
	}
	if !strings.HasPrefix(k.Fingerprint, "SHA256:") {
		t.Errorf("fingerprint = %q", k.Fingerprint)
	}
	signer, err := ssh.ParsePrivateKey(k.PrivatePEM)
	if err != nil {
		t.Fatalf("private key does not parse: %v", err)
	}
	if ssh.FingerprintSHA256(signer.PublicKey()) != k.Fingerprint {
		t.Error("private and public halves differ")
	}
}

// An SSH server on localhost: ScanHostKey records its key without logging in.
func TestScanHostKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{NoClientAuth: false, PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil }}
	cfg.AddHostKey(signer)
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
			go func() { _, _, _, _ = ssh.NewServerConn(c, cfg); c.Close() }()
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	line, fp, err := ScanHostKey(ctx, host, port)
	if err != nil {
		t.Fatal(err)
	}
	if fp != ssh.FingerprintSHA256(signer.PublicKey()) {
		t.Errorf("fingerprint %s, want %s", fp, ssh.FingerprintSHA256(signer.PublicKey()))
	}
	if !strings.HasPrefix(line, "[127.0.0.1]:"+port+" ssh-ed25519 ") {
		t.Errorf("known_hosts line = %q", line)
	}
}

func TestScanHostKeyUnreachable(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()
	if _, _, err := ScanHostKey(context.Background(), "127.0.0.1", port); err == nil || !strings.Contains(err.Error(), "could not reach") {
		t.Errorf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./pkg/gitauth/ -run 'DeployKey|HostKey'`
Expected: FAIL, `undefined: GenerateDeployKey`.

- [ ] **Step 3: Write `pkg/gitauth/keys.go`**

```go
package gitauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// DeployKey is a key pair shpyrd generates for a project: the private half
// stays in a Secret, the public half is added to the repository.
type DeployKey struct {
	PrivatePEM  []byte
	Public      string // authorized_keys form, with the comment
	Fingerprint string // SHA256:...
}

// GenerateDeployKey makes an ed25519 key pair.
func GenerateDeployKey(comment string) (DeployKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return DeployKey{}, err
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return DeployKey{}, err
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		return DeployKey{}, err
	}
	return DeployKey{
		PrivatePEM:  pem.EncodeToMemory(block),
		Public:      strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))) + " " + comment,
		Fingerprint: ssh.FingerprintSHA256(sp),
	}, nil
}

var errScanned = errors.New("host key recorded")

// ScanHostKey connects to host's SSH port, records the key it presents and
// hangs up before logging in. The line is in known_hosts form.
func ScanHostKey(ctx context.Context, host, port string) (string, string, error) {
	if port == "" {
		port = "22"
	}
	addr := net.JoinHostPort(host, port)
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", "", fmt.Errorf("could not reach %s to record its SSH host key: %v", addr, err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	}
	var key ssh.PublicKey
	cfg := &ssh.ClientConfig{
		User: "git",
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			key = k
			return errScanned
		},
	}
	_, _, _, err = ssh.NewClientConn(conn, addr, cfg)
	if key == nil {
		return "", "", fmt.Errorf("could not reach %s to record its SSH host key: %v", addr, err)
	}
	return knownhosts.Line([]string{knownhosts.Normalize(addr)}, key), ssh.FingerprintSHA256(key), nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./pkg/gitauth/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/gitauth/keys.go pkg/gitauth/keys_test.go
git commit -m "feat(gitauth): deploy keys and recorded SSH host keys (RFC-0017)"
```

---

### Task 3: API for workspace tokens and project deploy keys

**Files:**
- Create: `pkg/api/gitauth.go`
- Modify: `pkg/api/server.go`. Add next to the `/workspace/drains` routes (around line 643) and the `/projects/:slug/drains` routes (line 782).
- Test: `pkg/api/gitauth_test.go`

**Interfaces:**
- Consumes: Task 1 and Task 2 (`gitauth.*`); `s.loadApp(c)`, `s.doorWorkspace(c)`, `s.audit(c, project, action, target, detail)`, `abort(c, code, err)`, `project.SlugOf(app)`; `s.apps` (the controller-runtime client the tests read back through); `s.kube.Namespace` (the system namespace).
- Produces (JSON for the CLI in Task 7):
  ```go
  type GitTokenView struct {
  	Host      string    `json:"host"`
  	Username  string    `json:"username"`
  	UpdatedBy string    `json:"updatedBy,omitempty"`
  	UpdatedAt time.Time `json:"updatedAt"`
  }
  type GitTokenRequest struct {
  	Token    string `json:"token" binding:"required"`
  	Username string `json:"username,omitempty"`
  }
  type DeployKeyRequest struct {
  	Host string `json:"host,omitempty"` // default: the project's git host
  }
  type DeployKeyView struct {
  	Host               string    `json:"host"`
  	PublicKey          string    `json:"publicKey"`
  	Fingerprint        string    `json:"fingerprint"`
  	HostKeyFingerprint string    `json:"hostKeyFingerprint"`
  	CreatedAt          time.Time `json:"createdAt"`
  }
  type ProjectGitAuthView struct {
  	URL       string         `json:"url,omitempty"`
  	Using     string         `json:"using"` // "deploy-key", "token" or "none"
  	DeployKey *DeployKeyView `json:"deployKey,omitempty"`
  	Token     *GitTokenView  `json:"token,omitempty"`
  }
  ```
  Routes:
  - `GET /api/workspace/git-auth` → `[]GitTokenView` (ClusterAdmin)
  - `PUT /api/workspace/git-auth/:host` → `GitTokenView` (ClusterAdmin)
  - `DELETE /api/workspace/git-auth/:host` → 204 (ClusterAdmin)
  - `GET /api/projects/:slug/git-auth` → `ProjectGitAuthView` (ProjectView)
  - `POST /api/projects/:slug/git-auth/deploy-key` → 201 `DeployKeyView` (ProjectConfig)
  - `DELETE /api/projects/:slug/git-auth/deploy-key` → 204 (ProjectConfig)

- [ ] **Step 1: Write the failing tests**

`pkg/api/gitauth_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

func TestWorkspaceGitTokens(t *testing.T) {
	s, k := newTestServer(t, nil, nil)
	rec := do(t, s, "PUT", "/api/workspace/git-auth/gitlab.example.com", `{"token":"glpat-s3cret"}`, true)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "s3cret") {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}
	sec := &corev1.Secret{}
	key := types.NamespacedName{Namespace: "shpyrd-system", Name: gitauth.TokenSecretName("default", "gitlab.example.com")}
	if err := k.Get(context.Background(), key, sec); err != nil {
		t.Fatal(err)
	}
	if string(sec.Data[gitauth.KeyToken]) != "glpat-s3cret" || sec.Labels[gitauth.LabelCredential] != gitauth.KindToken ||
		sec.Labels[shpyrdv1.LabelWorkspace] != "default" || sec.Annotations[gitauth.AnnotationHost] != "gitlab.example.com" {
		t.Errorf("secret = %+v", sec)
	}
	rec = do(t, s, "GET", "/api/workspace/git-auth", "", true)
	var list []GitTokenView
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != http.StatusOK || len(list) != 1 || list[0].Host != "gitlab.example.com" || list[0].Username != "x-access-token" || strings.Contains(rec.Body.String(), "s3cret") {
		t.Errorf("list: %d %s", rec.Code, rec.Body.String())
	}
	for _, bad := range []string{"not_a_host", "gitea.local:3000", "-x.com"} {
		if rec := do(t, s, "PUT", "/api/workspace/git-auth/"+bad, `{"token":"x"}`, true); rec.Code != http.StatusBadRequest {
			t.Errorf("bad host %s: %d", bad, rec.Code)
		}
	}
	if rec := do(t, s, "PUT", "/api/workspace/git-auth/github.com", `{}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("no token: %d", rec.Code)
	}
	if rec := do(t, s, "DELETE", "/api/workspace/git-auth/gitlab.example.com", "", true); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d", rec.Code)
	}
	if err := k.Get(context.Background(), key, sec); err == nil {
		t.Error("the token Secret must be gone")
	}
}

func TestProjectDeployKey(t *testing.T) {
	app := sampleApp("shop", shpyrdv1.PhaseRunning)
	app.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://github.com/acme/shop"}}
	s, k := newTestServer(t, nil, []client.Object{app})
	s.scanHostKey = func(_ context.Context, host, port string) (string, string, error) {
		if host != "github.com" || port != "" {
			t.Errorf("scanned %s:%s", host, port)
		}
		return "github.com ssh-ed25519 AAAAhost", "SHA256:host", nil
	}
	rec := do(t, s, "POST", "/api/projects/shop/git-auth/deploy-key", `{}`, true)
	if rec.Code != http.StatusCreated || strings.Contains(rec.Body.String(), "PRIVATE KEY") {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var v DeployKeyView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	if v.Host != "github.com" || !strings.HasPrefix(v.PublicKey, "ssh-ed25519 ") || v.HostKeyFingerprint != "SHA256:host" {
		t.Errorf("view = %+v", v)
	}
	sec := &corev1.Secret{}
	if err := k.Get(context.Background(), types.NamespacedName{Namespace: app.Namespace, Name: gitauth.DeployKeySecretName}, sec); err != nil {
		t.Fatal(err)
	}
	if sec.Type != corev1.SecretTypeSSHAuth || !strings.Contains(string(sec.Data[corev1.SSHAuthPrivateKey]), "PRIVATE KEY") || string(sec.Data[gitauth.KeyKnownHosts]) != "github.com ssh-ed25519 AAAAhost" {
		t.Errorf("secret = %+v", sec)
	}
	rec = do(t, s, "GET", "/api/projects/shop/git-auth", "", true)
	var pv ProjectGitAuthView
	_ = json.Unmarshal(rec.Body.Bytes(), &pv)
	if rec.Code != http.StatusOK || pv.Using != gitauth.KindDeployKey || pv.DeployKey == nil || pv.DeployKey.PublicKey != v.PublicKey {
		t.Errorf("show: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "DELETE", "/api/projects/shop/git-auth/deploy-key", "", true); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d", rec.Code)
	}
	rec = do(t, s, "GET", "/api/projects/shop/git-auth", "", true)
	_ = json.Unmarshal(rec.Body.Bytes(), &pv)
	if pv.Using != "none" || pv.DeployKey != nil {
		t.Errorf("after delete: %s", rec.Body.String())
	}
}

func TestProjectGitAuthUsesTheWorkspaceToken(t *testing.T) {
	app := sampleApp("shop", shpyrdv1.PhaseRunning)
	app.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "git@gitlab.example.com:team/shop.git"}}
	s, _ := newTestServer(t, nil, []client.Object{app})
	if rec := do(t, s, "PUT", "/api/workspace/git-auth/gitlab.example.com", `{"token":"t","username":"me"}`, true); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	rec := do(t, s, "GET", "/api/projects/shop/git-auth", "", true)
	var pv ProjectGitAuthView
	_ = json.Unmarshal(rec.Body.Bytes(), &pv)
	if pv.Using != gitauth.KindToken || pv.Token == nil || pv.Token.Username != "me" {
		t.Errorf("show: %s", rec.Body.String())
	}
}

func TestDeployKeyNeedsAHost(t *testing.T) {
	s, _ := newTestServer(t, nil, []client.Object{sampleApp("shop", shpyrdv1.PhaseRunning)}) // no git source
	rec := do(t, s, "POST", "/api/projects/shop/git-auth/deploy-key", `{}`, true)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "--host") {
		t.Errorf("no source, no host: %d %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./pkg/api/ -run 'GitToken|DeployKey|GitAuth'`
Expected: FAIL to compile, `s.scanHostKey undefined`.

- [ ] **Step 3: Add the hook to `Server`**

In the `Server` struct in `pkg/api/server.go`, next to `sleepAvailable`:

```go
	// scanHostKey records a git host's SSH key for a deploy key
	// (RFC-0017); a field so tests need no network.
	scanHostKey func(ctx context.Context, host, port string) (line, fingerprint string, err error)
```

In `newServer`, where the other defaults are set:

```go
	s.scanHostKey = gitauth.ScanHostKey
```

- [ ] **Step 4: Write `pkg/api/gitauth.go`**

```go
package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/project"
)

// Git credentials (RFC-0017): a workspace's tokens, one per host, live in
// the system namespace labelled with the workspace; a project's deploy key
// lives in its namespace. Values are written, never returned.

type GitTokenView struct {
	Host      string    `json:"host"`
	Username  string    `json:"username"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type GitTokenRequest struct {
	Token    string `json:"token" binding:"required"`
	Username string `json:"username,omitempty"`
}

type DeployKeyRequest struct {
	// Host defaults to the project's git host.
	Host string `json:"host,omitempty"`
}

type DeployKeyView struct {
	Host               string    `json:"host"`
	PublicKey          string    `json:"publicKey"`
	Fingerprint        string    `json:"fingerprint"`
	HostKeyFingerprint string    `json:"hostKeyFingerprint"`
	CreatedAt          time.Time `json:"createdAt"`
}

type ProjectGitAuthView struct {
	URL       string         `json:"url,omitempty"`
	Using     string         `json:"using"`
	DeployKey *DeployKeyView `json:"deployKey,omitempty"`
	Token     *GitTokenView  `json:"token,omitempty"`
}

// A token is for a host name, without a port: the controller looks it up
// by the repository URL's host name, whatever port the URL names.
var gitHostRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)

func gitHostParam(c *gin.Context) (string, bool) {
	host := c.Param("host")
	if !gitHostRe.MatchString(host) {
		abort(c, http.StatusBadRequest, errors.New("give a host name such as github.com or gitlab.example.com, without a port"))
		return "", false
	}
	return host, true
}

func actorOf(c *gin.Context) string {
	if id, ok := ext.IdentityFrom(c); ok {
		return firstNonEmpty(id.Email, id.Name, id.Subject)
	}
	return ""
}

func tokenView(sec *corev1.Secret) GitTokenView {
	host := sec.Annotations[gitauth.AnnotationHost]
	return GitTokenView{
		Host:      host,
		Username:  firstNonEmpty(string(sec.Data[gitauth.KeyUsername]), gitauth.DefaultUsername(host)),
		UpdatedBy: sec.Annotations[gitauth.AnnotationUpdatedBy],
		UpdatedAt: sec.CreationTimestamp.Time,
	}
}

func (s *Server) listGitTokens(c *gin.Context) {
	var list corev1.SecretList
	if err := s.apps.List(c.Request.Context(), &list, client.InNamespace(s.kube.Namespace),
		client.MatchingLabels{gitauth.LabelCredential: gitauth.KindToken, shpyrdv1.LabelWorkspace: s.doorWorkspace(c)}); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	out := make([]GitTokenView, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, tokenView(&list.Items[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	c.JSON(http.StatusOK, out)
}

func (s *Server) putGitToken(c *gin.Context) {
	host, ok := gitHostParam(c)
	if !ok {
		return
	}
	var req GitTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, errors.New("give the token"))
		return
	}
	ws := s.doorWorkspace(c)
	ctx := c.Request.Context()
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: gitauth.TokenSecretName(ws, host), Namespace: s.kube.Namespace}}
	data := map[string][]byte{gitauth.KeyToken: []byte(req.Token)}
	if req.Username != "" {
		data[gitauth.KeyUsername] = []byte(req.Username)
	}
	err := s.apps.Get(ctx, client.ObjectKeyFromObject(sec), sec)
	switch {
	case apierrors.IsNotFound(err):
		sec.Labels = map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", gitauth.LabelCredential: gitauth.KindToken, shpyrdv1.LabelWorkspace: ws}
		sec.Annotations = map[string]string{gitauth.AnnotationHost: host, gitauth.AnnotationUpdatedBy: actorOf(c)}
		sec.Type = corev1.SecretTypeOpaque
		sec.Data = data
		err = s.apps.Create(ctx, sec)
	case err == nil:
		sec.Annotations = mergeStringMaps(sec.Annotations, map[string]string{gitauth.AnnotationUpdatedBy: actorOf(c)})
		sec.Data = data
		err = s.apps.Update(ctx, sec)
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	s.audit(c, "", "git-auth.set", host, "token for "+host)
	c.JSON(http.StatusOK, tokenView(sec))
}

func (s *Server) deleteGitToken(c *gin.Context) {
	host, ok := gitHostParam(c)
	if !ok {
		return
	}
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: gitauth.TokenSecretName(s.doorWorkspace(c), host), Namespace: s.kube.Namespace}}
	if err := s.apps.Delete(c.Request.Context(), sec); err != nil && !apierrors.IsNotFound(err) {
		abort(c, http.StatusBadGateway, err)
		return
	}
	s.audit(c, "", "git-auth.unset", host, "token for "+host)
	c.Status(http.StatusNoContent)
}

func deployKeyView(sec *corev1.Secret) *DeployKeyView {
	return &DeployKeyView{
		Host:               sec.Annotations[gitauth.AnnotationHost],
		PublicKey:          sec.Annotations[gitauth.AnnotationPublicKey],
		Fingerprint:        sec.Annotations[gitauth.AnnotationFingerprint],
		HostKeyFingerprint: sec.Annotations[gitauth.AnnotationHostKeyFingerprint],
		CreatedAt:          sec.CreationTimestamp.Time,
	}
}

// projectGitSecrets reads the project's deploy key and the workspace's
// token for its git host; either may be nil.
func (s *Server) projectGitSecrets(ctx context.Context, app *shpyrdv1.App, host string) (deployKey, token *corev1.Secret, err error) {
	dk := &corev1.Secret{}
	if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: gitauth.DeployKeySecretName}, dk); err == nil {
		deployKey = dk
	} else if !apierrors.IsNotFound(err) {
		return nil, nil, err
	}
	if host != "" {
		tk := &corev1.Secret{}
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: s.kube.Namespace, Name: gitauth.TokenSecretName(workspaceOf(app), host)}, tk); err == nil {
			token = tk
		} else if !apierrors.IsNotFound(err) {
			return nil, nil, err
		}
	}
	return deployKey, token, nil
}

func (s *Server) showProjectGitAuth(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	view := ProjectGitAuthView{Using: "none"}
	host := ""
	if app.Spec.Source != nil && app.Spec.Source.Git != nil {
		view.URL = app.Spec.Source.Git.URL
		if r, err := gitauth.Parse(view.URL); err == nil {
			host = r.Host
		}
	}
	dk, tk, err := s.projectGitSecrets(c.Request.Context(), app, host)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	if dk != nil {
		view.DeployKey = deployKeyView(dk)
	}
	if tk != nil {
		tv := tokenView(tk)
		view.Token = &tv
	}
	switch {
	case dk != nil:
		view.Using = gitauth.KindDeployKey
	case tk != nil:
		view.Using = gitauth.KindToken
	}
	c.JSON(http.StatusOK, view)
}

func (s *Server) createDeployKey(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	var req DeployKeyRequest
	_ = c.ShouldBindJSON(&req)
	host, port := req.Host, ""
	if host == "" && app.Spec.Source != nil && app.Spec.Source.Git != nil {
		if r, err := gitauth.Parse(app.Spec.Source.Git.URL); err == nil {
			host = r.Host
			if r.Scheme == "ssh" {
				port = r.Port
			}
		}
	}
	if host == "" {
		abort(c, http.StatusBadRequest, errors.New("the project has no git repository yet: name its host with --host (github.com, gitlab.example.com)"))
		return
	}
	ctx := c.Request.Context()
	line, hostFP, err := s.scanHostKey(ctx, host, port)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	slug := project.SlugOf(app)
	key, err := gitauth.GenerateDeployKey("shpyrd " + slug)
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: gitauth.DeployKeySecretName, Namespace: app.Namespace,
			Labels: map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", gitauth.LabelCredential: gitauth.KindDeployKey, shpyrdv1.LabelProject: app.Name},
			Annotations: map[string]string{
				gitauth.AnnotationHost: host, gitauth.AnnotationPublicKey: key.Public, gitauth.AnnotationFingerprint: key.Fingerprint,
				gitauth.AnnotationHostKeyFingerprint: hostFP, gitauth.AnnotationUpdatedBy: actorOf(c),
			},
		},
		Type: corev1.SecretTypeSSHAuth,
		Data: map[string][]byte{corev1.SSHAuthPrivateKey: key.PrivatePEM, gitauth.KeyKnownHosts: []byte(line + "\n")},
	}
	// A new key replaces the old one: the old public half stops working
	// once removed from the repository.
	if err := s.apps.Delete(ctx, sec.DeepCopy()); err != nil && !apierrors.IsNotFound(err) {
		abort(c, http.StatusBadGateway, err)
		return
	}
	if err := s.apps.Create(ctx, sec); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	s.audit(c, slug, "git-auth.set", "deploy key", "deploy key "+key.Fingerprint+" for "+host)
	c.JSON(http.StatusCreated, deployKeyView(sec))
}

func (s *Server) deleteDeployKey(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: gitauth.DeployKeySecretName, Namespace: app.Namespace}}
	if err := s.apps.Delete(c.Request.Context(), sec); err != nil && !apierrors.IsNotFound(err) {
		abort(c, http.StatusBadGateway, err)
		return
	}
	s.audit(c, project.SlugOf(app), "git-auth.unset", "deploy key", "deploy key")
	c.Status(http.StatusNoContent)
}
```

Before writing, check two names against the code and use what is there:
- `workspaceOf` exists in `pkg/api/edge.go:906`.
- A string-map merge may already exist: `grep -n "func merge" pkg/api/*.go`. If none does, add this at the bottom of the file:
  ```go
  func mergeStringMaps(a, b map[string]string) map[string]string {
  	out := make(map[string]string, len(a)+len(b))
  	for k, v := range a {
  		out[k] = v
  	}
  	for k, v := range b {
  		out[k] = v
  	}
  	return out
  }
  ```
- `shpyrdv1.LabelManagedBy` and `shpyrdv1.LabelProject` are used in `internal/controller/registry.go`.

- [ ] **Step 5: Register the routes in `pkg/api/server.go`**

After `api.DELETE("/workspace/drains/:name", ...)`:

```go
	api.GET("/workspace/git-auth", s.require(authz.ClusterAdmin), s.listGitTokens) // RFC-0017
	api.PUT("/workspace/git-auth/:host", s.require(authz.ClusterAdmin), s.putGitToken)
	api.DELETE("/workspace/git-auth/:host", s.require(authz.ClusterAdmin), s.deleteGitToken)
```

After `api.DELETE("/projects/:slug/drains/:name", ...)`:

```go
	api.GET("/projects/:slug/git-auth", s.require(authz.ProjectView), s.showProjectGitAuth) // RFC-0017
	api.POST("/projects/:slug/git-auth/deploy-key", s.require(authz.ProjectConfig), s.createDeployKey)
	api.DELETE("/projects/:slug/git-auth/deploy-key", s.require(authz.ProjectConfig), s.deleteDeployKey)
```

- [ ] **Step 6: Run the tests**

Run: `go test ./pkg/api/ -run 'GitToken|DeployKey|GitAuth'`
Expected: PASS. Then `go test ./pkg/api/...`: everything passes.

- [ ] **Step 7: Make sure the server can manage these Secrets**

The server already writes Secrets in the system namespace (globals) and in
project namespaces (drain headers). Check that its RBAC covers create, update,
delete and list of Secrets in both:

Run: `grep -rn "secrets" deploy/ | grep -i -B2 -A6 "shpyrd-server" | head -40`

If `list` is missing in the system namespace, add it in the same file and
commit it with this task.

- [ ] **Step 8: Commit**

```bash
git add pkg/api/gitauth.go pkg/api/gitauth_test.go pkg/api/server.go
git commit -m "feat(api): workspace git tokens and project deploy keys (RFC-0017)"
```

---

### Task 4: The controller writes `<app>-git` and wires the build ServiceAccount

**Files:**
- Create: `internal/controller/gitcreds.go`, `internal/controller/gitcreds_test.go`
- Modify: `internal/controller/registry.go:27-58` (remove the ServiceAccount block), `internal/controller/desired.go:168-179` and `:573-610`, `internal/controller/app_controller.go:346`, `:377`, `:401`, `:659-666`, `internal/controller/dockerfile.go:52`, `internal/controller/globals.go:137-159`
- Test: `internal/controller/registry_test.go` (update the call sites), `internal/controller/gitcreds_test.go`

**Interfaces:**
- Consumes: `gitauth.Resolve`, `gitauth.Build`, `gitauth.DeployKeySecretName`, `gitauth.TokenSecretName`, `gitauth.LabelCredential`, `gitauth.BuildSecretName`, `gitauth.Parse`; `workspaceOf(app)` (desired.go:354).
- Produces:
  - `func (r *AppReconciler) reconcileGitCredentials(ctx context.Context, app *shpyrdv1.App) (gitauth.Build, error)`
  - `func (r *AppReconciler) ensureBuildServiceAccount(ctx context.Context, app *shpyrdv1.App, git gitauth.Build) error`
  - `func (c Config) buildServiceAccountName(git gitauth.Build) string` (signature change)
  - `func (c Config) desiredKpackImage(app *shpyrdv1.App, git gitauth.Build) (*unstructured.Unstructured, error)` (signature change)
  - `func (r *AppReconciler) reconcileKpackImage(ctx context.Context, app *shpyrdv1.App, git gitauth.Build)` (signature change)
  - `func (r *AppReconciler) reconcileDockerfileBuild(ctx context.Context, app *shpyrdv1.App, git gitauth.Build)` (signature change; Task 5 uses `git` inside)

- [ ] **Step 1: Write the failing tests**

`internal/controller/gitcreds_test.go`:

```go
package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

func gitApp(name, url string) *shpyrdv1.App {
	app := sampleApp(name)
	app.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: url}}
	return app
}

func workspaceToken(ws, host, token string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: gitauth.TokenSecretName(ws, host), Namespace: "shpyrd-system",
			Labels:      map[string]string{gitauth.LabelCredential: gitauth.KindToken, shpyrdv1.LabelWorkspace: ws},
			Annotations: map[string]string{gitauth.AnnotationHost: host}},
		Data: map[string][]byte{gitauth.KeyToken: []byte(token)},
	}
}

func TestReconcileGitCredentialsPicksWorkspaceToken(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	other := workspaceToken("other", "github.com", "not-ours")
	r, c := newTestReconciler(t, app, workspaceToken("default", "github.com", "ghp_ours"), other)
	got, err := r.reconcileGitCredentials(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != gitauth.KindToken || got.CloneURL != "https://github.com/acme/shop.git" {
		t.Fatalf("build = %+v", got)
	}
	sec := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop-git"}, sec); err != nil {
		t.Fatal(err)
	}
	if string(sec.Data[corev1.BasicAuthPasswordKey]) != "ghp_ours" {
		t.Errorf("the default workspace's token, not another's: %q", sec.Data[corev1.BasicAuthPasswordKey])
	}
	if err := r.ensureBuildServiceAccount(context.Background(), app, got); err != nil {
		t.Fatal(err)
	}
	sa := &corev1.ServiceAccount{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: BuildServiceAccount}, sa); err != nil {
		t.Fatal(err)
	}
	if len(sa.Secrets) != 1 || sa.Secrets[0].Name != "shop-git" || len(sa.ImagePullSecrets) != 0 {
		t.Errorf("SA = %+v", sa)
	}
	img, _ := r.Config.desiredKpackImage(app, got)
	if u, _, _ := unstructured.NestedString(img.Object, "spec", "source", "git", "url"); u != "https://github.com/acme/shop.git" {
		t.Errorf("kpack url = %s", u)
	}
	if n, _, _ := unstructured.NestedString(img.Object, "spec", "serviceAccountName"); n != BuildServiceAccount {
		t.Errorf("kpack SA = %s", n)
	}
}

func TestReconcileGitCredentialsDeployKeyAndRegistry(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	dk := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: gitauth.DeployKeySecretName, Namespace: "app-shop", Labels: map[string]string{gitauth.LabelCredential: gitauth.KindDeployKey}},
		Type:       corev1.SecretTypeSSHAuth,
		Data:       map[string][]byte{corev1.SSHAuthPrivateKey: []byte("K"), gitauth.KeyKnownHosts: []byte("github.com ssh-ed25519 A")},
	}
	reg := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shpyrd-registry", Namespace: "shpyrd-system"}, Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{}`)}}
	r, c := newTestReconciler(t, app, dk, reg)
	r.Config.RegistrySecret = "shpyrd-registry"
	runReconcile(t, r, app)
	sa := &corev1.ServiceAccount{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: BuildServiceAccount}, sa); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, s := range sa.Secrets {
		names[s.Name] = true
	}
	if !names["shpyrd-registry"] || !names["shop-git"] || len(sa.ImagePullSecrets) != 1 || sa.ImagePullSecrets[0].Name != "shpyrd-registry" {
		t.Errorf("SA = %+v", sa)
	}
	img := &unstructured.Unstructured{}
	img.SetGroupVersionKind(KpackImageGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop"}, img); err != nil {
		t.Fatal(err)
	}
	if u, _, _ := unstructured.NestedString(img.Object, "spec", "source", "git", "url"); u != "git@github.com:acme/shop.git" {
		t.Errorf("a deploy key clones over SSH: %s", u)
	}
}

func TestReconcileGitCredentialsCleansUp(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	tok := workspaceToken("default", "github.com", "ghp")
	r, c := newTestReconciler(t, app, tok)
	got, _ := r.reconcileGitCredentials(context.Background(), app)
	_ = r.ensureBuildServiceAccount(context.Background(), app, got)
	if err := c.Delete(context.Background(), tok); err != nil {
		t.Fatal(err)
	}
	got, err := r.reconcileGitCredentials(context.Background(), app)
	if err != nil || got.Kind != "" {
		t.Fatalf("build = %+v, %v", got, err)
	}
	_ = r.ensureBuildServiceAccount(context.Background(), app, got)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop-git"}, &corev1.Secret{}); err == nil {
		t.Error("shop-git must be deleted with the credential")
	}
	sa := &corev1.ServiceAccount{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: BuildServiceAccount}, sa); err == nil && len(sa.Secrets) != 0 {
		t.Errorf("the SA keeps a stale reference: %+v", sa.Secrets)
	}
	if n := r.Config.buildServiceAccountName(got); n != "default" {
		t.Errorf("no credentials, no registry: SA %s", n)
	}
}

func TestGitCredentialSecretsReachTheirApps(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	other := gitApp("blog", "https://github.com/acme/blog")
	other.Labels = map[string]string{shpyrdv1.LabelWorkspace: "other"}
	r, _ := newTestReconciler(t, app, other)
	reqs := r.secretToApps(context.Background(), workspaceToken("default", "github.com", "x"))
	if len(reqs) != 1 || reqs[0].Name != "shop" {
		t.Errorf("token -> %v", reqs)
	}
	dk := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: gitauth.DeployKeySecretName, Namespace: "app-shop", Labels: map[string]string{gitauth.LabelCredential: gitauth.KindDeployKey}}}
	if reqs := r.secretToApps(context.Background(), dk); len(reqs) != 1 || reqs[0].Name != "shop" {
		t.Errorf("deploy key -> %v", reqs)
	}
}
```

`TestReconcileGitCredentialsDeployKeyAndRegistry` reads back the kpack Image
the reconcile made. `TestKpackImageRecreatedWhenRegistryChanges`
(`app_controller_test.go:510`) shows how the fake client holds kpack Images;
copy its setup if this test needs more than it has.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/controller/ -run GitCredential`
Expected: FAIL to compile, `r.reconcileGitCredentials undefined`.

- [ ] **Step 3: Write `internal/controller/gitcreds.go`**

```go
package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

// Private git repositories (RFC-0017): before a build, the project's
// credential is written to <app>-git in its namespace, where kpack (through
// the build ServiceAccount) and the BuildKit fetch container read it. The
// project's deploy key wins over the workspace's token for the host.

// reconcileGitCredentials writes or removes <app>-git and says how the
// build clones.
func (r *AppReconciler) reconcileGitCredentials(ctx context.Context, app *shpyrdv1.App) (gitauth.Build, error) {
	name := gitauth.BuildSecretName(app.Name)
	if app.Spec.Source == nil || app.Spec.Source.Git == nil {
		return gitauth.Build{}, r.deleteSecret(ctx, app.Namespace, name)
	}
	url := app.Spec.Source.Git.URL
	repo, err := gitauth.Parse(url)
	if err != nil {
		return gitauth.Build{}, err
	}
	deployKey, err := r.optionalSecret(ctx, app.Namespace, gitauth.DeployKeySecretName)
	if err != nil {
		return gitauth.Build{}, err
	}
	token, err := r.optionalSecret(ctx, r.Config.SystemNamespace, gitauth.TokenSecretName(workspaceOf(app), repo.Host))
	if err != nil {
		return gitauth.Build{}, err
	}
	b, err := gitauth.Resolve(url, app.Name, app.Namespace, deployKey, token)
	if err != nil {
		return gitauth.Build{}, err
	}
	if b.Secret == nil {
		return b, r.deleteSecret(ctx, app.Namespace, name)
	}
	want := b.Secret
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: want.Name, Namespace: want.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, sec, func() error {
		sec.Labels = mergeMaps(sec.Labels, want.Labels)
		sec.Annotations = mergeMaps(sec.Annotations, want.Annotations)
		sec.Type = want.Type
		sec.Data = want.Data
		return nil
	}); err != nil {
		if apierrors.IsInvalid(err) {
			// The Secret's type is immutable: a deploy key replacing a
			// token needs a new Secret.
			if derr := r.deleteSecret(ctx, app.Namespace, name); derr != nil {
				return b, derr
			}
			return b, r.Create(ctx, want)
		}
		return b, fmt.Errorf("git credentials: %w", err)
	}
	return b, nil
}

// ensureBuildServiceAccount keeps shpyrd-builder listing the registry
// Secret and <app>-git, whichever the project needs. With neither, an
// existing account is emptied rather than left pointing at a Secret that
// is gone.
func (r *AppReconciler) ensureBuildServiceAccount(ctx context.Context, app *shpyrdv1.App, git gitauth.Build) error {
	var secrets []corev1.ObjectReference
	var pulls []corev1.LocalObjectReference
	if r.Config.RegistrySecret != "" {
		secrets = append(secrets, corev1.ObjectReference{Name: r.Config.RegistrySecret})
		pulls = append(pulls, corev1.LocalObjectReference{Name: r.Config.RegistrySecret})
	}
	if git.Secret != nil {
		secrets = append(secrets, corev1.ObjectReference{Name: git.Secret.Name})
	}
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: BuildServiceAccount, Namespace: app.Namespace}}
	if len(secrets) == 0 {
		if err := r.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: BuildServiceAccount}, sa); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		if len(sa.Secrets) == 0 && len(sa.ImagePullSecrets) == 0 {
			return nil
		}
		sa.Secrets, sa.ImagePullSecrets = nil, nil
		return r.Update(ctx, sa)
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error {
		sa.Labels = mergeMaps(sa.Labels, map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", shpyrdv1.LabelProject: app.Name})
		sa.Secrets = secrets
		sa.ImagePullSecrets = pulls
		return nil
	}); err != nil {
		return fmt.Errorf("build service account: %w", err)
	}
	return nil
}

func (r *AppReconciler) optionalSecret(ctx context.Context, ns, name string) (*corev1.Secret, error) {
	sec := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, sec); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return sec, nil
}

func (r *AppReconciler) deleteSecret(ctx context.Context, ns, name string) error {
	err := r.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
```

If `optionalSecret` or `deleteSecret` already exists in the package
(`grep -n "func (r \*AppReconciler) \(optionalSecret\|deleteSecret\)" internal/controller/*.go`),
use the existing one and drop the copy.

- [ ] **Step 4: Take the ServiceAccount out of `registry.go`**

In `reconcileRegistryCredentials` delete the block from
`sa := &corev1.ServiceAccount{...}` to its `return fmt.Errorf("build service account: %w", err)`,
and its closing brace, so the function ends after the mirror's
`CreateOrUpdate`. Update the comment above the function to: "mirrors the
registry Secret into the project namespace; ensureBuildServiceAccount links
it."

- [ ] **Step 5: Change `desired.go`**

Replace `buildServiceAccountName` (lines 168-179):

```go
// buildServiceAccountName is what builds run as: the credentialed account
// when the registry or the repository needs credentials, else the
// namespace default.
func (c Config) buildServiceAccountName(git gitauth.Build) string {
	if c.RegistrySecret != "" || git.Secret != nil {
		return BuildServiceAccount
	}
	return "default"
}
```

In `desiredKpackImage`, change the signature to
`func (c Config) desiredKpackImage(app *shpyrdv1.App, git gitauth.Build) (*unstructured.Unstructured, error)`,
and in the git case use the resolved URL:

```go
	case app.Spec.Source.Git != nil:
		url := app.Spec.Source.Git.URL
		if git.CloneURL != "" {
			url = git.CloneURL
		}
		gitSrc := map[string]interface{}{"url": url}
		rev := app.Spec.Source.Git.Revision
		if rev == "" {
			rev = "main"
		}
		gitSrc["revision"] = rev
		source["git"] = gitSrc
```

and `"serviceAccountName": c.buildServiceAccountName(git),`. Add the import
`"github.com/shpyrd-io/shpyrd/pkg/gitauth"`.

- [ ] **Step 6: Wire `app_controller.go` and `dockerfile.go`**

At `app_controller.go:346`, after `reconcileRegistryCredentials`:

```go
	gitBuild, err := r.reconcileGitCredentials(ctx, app)
	if err != nil {
		return outcome{}, err
	}
	if err := r.ensureBuildServiceAccount(ctx, app, gitBuild); err != nil {
		return outcome{}, err
	}
```

Pass `gitBuild` into `r.reconcileDockerfileBuild(ctx, app, gitBuild)` (line
377) and `r.reconcileKpackImage(ctx, app, gitBuild)` (line 401). Change
`reconcileKpackImage`'s signature (line 659) to take `git gitauth.Build` and
call `r.Config.desiredKpackImage(app, git)` (line 666). Change
`reconcileDockerfileBuild` (`dockerfile.go:52`) to take `git gitauth.Build`.
Task 5 passes it on to `desiredBuildJob`; for now leave it unused with
`_ = git`.

Fix the other callers of `desiredKpackImage`:
`grep -rn "desiredKpackImage(" internal/ | grep -v "func "`. In tests, pass
`gitauth.Build{}`. In `registry_test.go`,
`r.Config.desiredKpackImage(src, gitauth.Build{})` still expects
`BuildServiceAccount`, because `RegistrySecret` is set.

`TestPrivateRegistryCredentials` keeps passing: `runReconcile` now creates the
SA through `ensureBuildServiceAccount`.

- [ ] **Step 7: Credential Secret events reach their Apps (`globals.go:137`)**

At the top of `secretToApps`:

```go
	switch obj.GetLabels()[gitauth.LabelCredential] {
	case gitauth.KindDeployKey:
		var apps shpyrdv1.AppList
		if err := r.List(ctx, &apps, client.InNamespace(obj.GetNamespace())); err != nil {
			return nil
		}
		out := make([]reconcile.Request, 0, len(apps.Items))
		for _, a := range apps.Items {
			out = append(out, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: a.Namespace, Name: a.Name}})
		}
		return out
	case gitauth.KindToken:
		if obj.GetNamespace() != r.Config.SystemNamespace {
			return nil
		}
		ws := obj.GetLabels()[shpyrdv1.LabelWorkspace]
		var apps shpyrdv1.AppList
		if err := r.List(ctx, &apps); err != nil {
			return nil
		}
		var out []reconcile.Request
		for _, a := range apps.Items {
			if a.Spec.Source != nil && a.Spec.Source.Git != nil && workspaceOf(&a) == ws {
				out = append(out, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: a.Namespace, Name: a.Name}})
			}
		}
		return out
	}
```

Check that the Secret watch's predicate does not filter these Secrets out
before `secretToApps` runs: `grep -n "secretToApps" internal/controller/app_controller.go`
and read the `Watches(` call. If a predicate limits it to names ending in
`-env` or to the registry Secret, widen it to also pass Secrets carrying
`gitauth.LabelCredential`.

- [ ] **Step 8: Run the controller tests**

Run: `go test ./internal/controller/...`
Expected: PASS, including `TestPrivateRegistryCredentials` and `TestNoRegistryCredentials`.

- [ ] **Step 9: Commit**

```bash
git add internal/controller pkg/gitauth
git commit -m "feat(controller): builds clone private repositories with the project's credential (RFC-0017)"
```

---

### Task 5: The BuildKit fetch container clones with the credential

**Files:**
- Modify: `internal/controller/dockerfile.go:52-80` (pass `git` on), `:342` (`desiredBuildJob` signature), `:401-458` (fetch container volume and env), `:476-500` (`fetchScript`)
- Test: `internal/controller/dockerfile_test.go` (update callers), `internal/controller/gitcreds_test.go` (new tests)

**Interfaces:**
- Consumes: `gitauth.Build` from Task 4.
- Produces:
  - `func (c Config) desiredBuildJob(app *shpyrdv1.App, n int, key string, git gitauth.Build) (*batchv1.Job, error)`
  - `func (c Config) fetchScript(app *shpyrdv1.App, git gitauth.Build) string`
  - `const gitAuthDir = "/git-auth"`

- [ ] **Step 1: Write the failing tests** (append to `gitcreds_test.go`; add `"strings"` and `batchv1` imports as needed)

```go
func dockerfileGitApp(url string) *shpyrdv1.App {
	app := dockerfileApp()
	app.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: url, Revision: "main"}}
	return app
}

func TestDockerfileFetchWithToken(t *testing.T) {
	app := dockerfileGitApp("https://github.com/acme/dk")
	r, c := newTestReconciler(t, app, workspaceToken("default", "github.com", "ghp_s3cret"))
	runReconcile(t, r, app)
	job := getJob(t, c, "app-dk", "dk-build-1")
	init := job.Spec.Template.Spec.InitContainers[0]
	script := init.Command[2]
	if strings.Contains(script, "ghp_s3cret") {
		t.Fatal("the token must never be inlined in the script")
	}
	if !strings.Contains(script, "url='https://github.com/acme/dk.git'") || !strings.Contains(script, ".netrc") {
		t.Errorf("script: %s", script)
	}
	if !hasGitAuthMount(init) || !hasSecretVolume(job, "dk-git") {
		t.Errorf("fetch container lacks the credential: %+v", init.VolumeMounts)
	}
	if hasGitAuthMount(job.Spec.Template.Spec.Containers[0]) {
		t.Error("the build container must not see the credential")
	}
}

func TestDockerfileFetchWithDeployKey(t *testing.T) {
	app := dockerfileGitApp("https://github.com/acme/dk")
	dk := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: gitauth.DeployKeySecretName, Namespace: "app-dk", Labels: map[string]string{gitauth.LabelCredential: gitauth.KindDeployKey}},
		Type:       corev1.SecretTypeSSHAuth,
		Data:       map[string][]byte{corev1.SSHAuthPrivateKey: []byte("K"), gitauth.KeyKnownHosts: []byte("github.com ssh-ed25519 A")},
	}
	r, c := newTestReconciler(t, app, dk)
	runReconcile(t, r, app)
	script := getJob(t, c, "app-dk", "dk-build-1").Spec.Template.Spec.InitContainers[0].Command[2]
	for _, want := range []string{"url='git@github.com:acme/dk.git'", "chmod 600", "StrictHostKeyChecking=yes", "UserKnownHostsFile=" + gitAuthDir + "/known_hosts", "GIT_SSH_COMMAND"} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q:\n%s", want, script)
		}
	}
}

func TestFetchScriptNeverInlinesSecrets(t *testing.T) {
	app := dockerfileGitApp("https://github.com/acme/dk")
	b, _ := gitauth.Resolve(app.Spec.Source.Git.URL, "dk", "app-dk", nil, workspaceToken("default", "github.com", "a'b$(c)\nd"))
	script := Config{}.fetchScript(app, b)
	if strings.Contains(script, "a'b") || strings.Contains(script, "$(c)") {
		t.Errorf("secret material in the script:\n%s", script)
	}
}

func TestPublicGitFetchUnchanged(t *testing.T) {
	app := dockerfileGitApp("https://github.com/acme/dk")
	script := Config{}.fetchScript(app, gitauth.Build{})
	if strings.Contains(script, ".netrc") || strings.Contains(script, "GIT_SSH_COMMAND") || !strings.Contains(script, "url='https://github.com/acme/dk'") {
		t.Errorf("a public repository's fetch changed:\n%s", script)
	}
}

func hasGitAuthMount(c corev1.Container) bool {
	for _, m := range c.VolumeMounts {
		if m.MountPath == gitAuthDir {
			return true
		}
	}
	return false
}

func hasSecretVolume(job *batchv1.Job, name string) bool {
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Secret != nil && v.Secret.SecretName == name {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/controller/ -run 'DockerfileFetch|FetchScript|PublicGitFetch'`
Expected: FAIL, `undefined: gitAuthDir` and the arguments of `fetchScript`.

- [ ] **Step 3: Change `fetchScript`**

```go
// gitAuthDir is where the fetch container finds <app>-git.
const gitAuthDir = "/git-auth"

// fetchScript downloads the uploaded archive or clones the git revision
// into the workspace and records the resolved revision. With a credential
// it reads it from gitAuthDir: never inlined, so a value with quotes or a
// newline cannot change the script.
func (c Config) fetchScript(app *shpyrdv1.App, git gitauth.Build) string {
	src := app.Spec.Source
	dst := workspaceDir + "/src"
	switch {
	case src.Blob != nil:
		// unchanged
		return fmt.Sprintf(`mkdir -p %[1]s
wget -qO /tmp/source.tgz %[2]s
tar xzf /tmp/source.tgz -C %[1]s
printf '%%s' %[3]s > %[4]s/revision
echo "source ready"`, dst, shellQuote(c.sourceURL(src.Blob.URL)), shellQuote(src.Blob.Ref), workspaceDir)
	case src.Git != nil:
		url := src.Git.URL
		if git.CloneURL != "" {
			url = git.CloneURL
		}
		auth := ""
		switch git.Kind {
		case gitauth.KindToken:
			auth = fmt.Sprintf(`export HOME=/tmp
printf 'machine %%s login %%s password %%s\n' %[1]s "$(cat %[2]s/username)" "$(cat %[2]s/password)" > "$HOME/.netrc"
chmod 600 "$HOME/.netrc"
`, shellQuote(git.Host), gitAuthDir)
		case gitauth.KindDeployKey:
			auth = fmt.Sprintf(`cp %[1]s/ssh-privatekey /tmp/deploy-key
chmod 600 /tmp/deploy-key
export GIT_SSH_COMMAND="ssh -i /tmp/deploy-key -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=%[1]s/known_hosts"
`, gitAuthDir)
		}
		rev := firstNonEmpty(src.Git.Revision, "main")
		return auth + fmt.Sprintf(`url=%[1]s
rev=%[2]s
if git clone --quiet --depth 1 --branch "$rev" "$url" %[3]s 2>/tmp/clone.err; then :; else
  if grep -qiE 'authentication|could not read|permission denied|repository not found|host key verification' /tmp/clone.err; then
    cat /tmp/clone.err >&2
    exit 1
  fi
  # not a branch or tag: fetch the commit
  git init --quiet %[3]s
  git -C %[3]s fetch --quiet --depth 1 "$url" "$rev"
  git -C %[3]s checkout --quiet FETCH_HEAD
fi
git -C %[3]s rev-parse HEAD > %[4]s/revision
echo "source ready at $(cat %[4]s/revision)"`, shellQuote(url), shellQuote(rev), dst, workspaceDir)
	}
	return "echo 'no source'; exit 1"
}
```

The `grep` stops early on an access error, so the fetch fails with git's own
words and does not fall into the commit fetch, which would bury them. Task 6
turns those words into the reason.

The `.netrc` host has no port. A port in `git.Host`'s URL is fine because
curl matches `.netrc` by host name only.

- [ ] **Step 4: Mount the Secret in the fetch container (`desiredBuildJob`)**

Change the signature to `desiredBuildJob(app *shpyrdv1.App, n int, key string, git gitauth.Build)`
and call `c.fetchScript(app, git)`. On the fetch init container add:

```go
						VolumeMounts: append([]corev1.VolumeMount{{Name: "workspace", MountPath: workspaceDir}}, gitAuthMounts(git)...),
```

and to the pod's `Volumes` append `gitAuthVolumes(git)...`. Add the helpers
next to `buildKitAuthVolumes`:

```go
// The project's git credential for the fetch container only: the build's
// RUN steps never see it.
func gitAuthMounts(git gitauth.Build) []corev1.VolumeMount {
	if git.Secret == nil {
		return nil
	}
	return []corev1.VolumeMount{{Name: "git-auth", MountPath: gitAuthDir, ReadOnly: true}}
}

func gitAuthVolumes(git gitauth.Build) []corev1.Volume {
	if git.Secret == nil {
		return nil
	}
	return []corev1.Volume{{Name: "git-auth", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
		SecretName: git.Secret.Name, DefaultMode: ptr.To[int32](0o440),
	}}}}
}
```

In `reconcileDockerfileBuild` replace `_ = git` with the call
`r.Config.desiredBuildJob(app, n, key, git)` (line 69). Update the test
callers: `grep -rn "desiredBuildJob(\|fetchScript(" internal/controller/*_test.go`
and pass `gitauth.Build{}`.

- [ ] **Step 5: Check the BuildKit image has an SSH client**

Run: `docker run --rm --entrypoint sh docker.io/moby/buildkit:v0.32.2-rootless -c 'command -v ssh git'`
Expected: two paths. If `ssh` is missing, stop and report back: the deploy-key
path then needs a fetch image of its own, which is a design change.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/controller/...`
Expected: PASS, including `TestDockerfileGitFetchAndProcesses`.

- [ ] **Step 7: Commit**

```bash
git add internal/controller
git commit -m "feat(controller): Dockerfile builds fetch private repositories (RFC-0017)"
```

---

### Task 6: A refused clone says how to fix it

**Files:**
- Modify: `internal/controller/build_failure.go:58-116`
- Test: `internal/controller/build_failure_test.go`

**Interfaces:**
- Produces: `describeBuildFailure(step, out string) string` recognises refused clones. No signature change.

- [ ] **Step 1: Write the failing test** (append to `build_failure_test.go`)

```go
func TestRefusedCloneNamesTheFix(t *testing.T) {
	for _, out := range []string{
		"fatal: could not read Username for 'https://github.com': terminal prompts disabled",
		"remote: Repository not found.\nfatal: repository 'https://github.com/acme/api.git/' not found",
		"git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.",
		"Error: failed to fetch: authentication required",
	} {
		msg := describeBuildFailure("prepare", out)
		if !strings.Contains(msg, "private") || !strings.Contains(msg, "shpyrd git-auth deploy-key") || !strings.Contains(msg, "shpyrd git-auth set") {
			t.Errorf("%q -> %s", out, msg)
		}
	}
	if msg := describeBuildFailure("prepare", "Host key verification failed."); !strings.Contains(msg, "host key") {
		t.Errorf("host key -> %s", msg)
	}
	if msg := describeBuildFailure("prepare", "fatal: unable to access: Could not resolve host"); strings.Contains(msg, "git-auth") {
		t.Errorf("a network failure is not an access failure: %s", msg)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/controller/ -run RefusedClone`
Expected: FAIL.

- [ ] **Step 3: Add the case to `describeBuildFailure`**

Before the `for _, re := range missingVariable` loop:

```go
	if refusedClone.MatchString(text) {
		return "The build could not read the repository: it is private, or does not exist under that name. " +
			"Give shpyrd access: `shpyrd git-auth deploy-key` prints a key to add to the repository as a read-only deploy key, " +
			"or a workspace admin sets a token for the host with `shpyrd git-auth set --host <host> --token -`. Then deploy again. " + buildLogHint
	}
	if strings.Contains(text, "Host key verification failed") {
		return "The build could not check the git host's identity: its SSH host key is not the one recorded with the deploy key. " +
			"If the host changed its key, make a new deploy key with `shpyrd git-auth deploy-key` and add it to the repository. " + buildLogHint
	}
```

Next to the other regexps at the top of the file:

```go
// refusedClone is git (or kpack's git) refused access to a repository.
var refusedClone = regexp.MustCompile(`(?i)could not read Username|Repository not found|Permission denied \(publickey\)|authentication required|Authentication failed|could not read from remote repository`)
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/controller/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/controller/build_failure.go internal/controller/build_failure_test.go
git commit -m "feat(controller): a clone refused for access names how to give it (RFC-0017)"
```

---

### Task 7: `shpyrd git-auth`

**Files:**
- Create: `internal/cli/gitauth.go`, `internal/cli/gitauth_test.go`
- Modify: `internal/cli/root.go:130` (after `newDrainsCmd`)

**Interfaces:**
- Consumes: the API in Task 3 (routes and `api.GitTokenView`, `api.GitTokenRequest`, `api.DeployKeyRequest`, `api.DeployKeyView`, `api.ProjectGitAuthView`); `newAppClient(g, progress)`, `ac.session`, `ac.serverRequest(ctx, method, path, body, contentType)`, `appFlag`, `resolveAppName`, `g.print(cmd, v, human)`, `signalContext()`.
- Produces: commands `git-auth list | set | unset | deploy-key | show`.

- [ ] **Step 1: Write the failing test**

```go
package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func runGitAuth(t *testing.T, handler http.HandlerFunc, stdin string, args ...string) (string, error) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("SHPYRD_URL", srv.URL)
	t.Setenv("SHPYRD_TOKEN", "test-token")
	root := New()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(append([]string{"git-auth"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestGitAuthSetReadsTheTokenFromStdin(t *testing.T) {
	var body map[string]string
	out, err := runGitAuth(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/api/workspace/git-auth/gitlab.example.com" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"host":"gitlab.example.com","username":"oauth2","updatedAt":"2026-10-16T10:00:00Z"}`))
	}, "glpat-s3cret\n", "set", "--host", "gitlab.example.com", "--token", "-")
	if err != nil {
		t.Fatal(err)
	}
	if body["token"] != "glpat-s3cret" {
		t.Errorf("token sent = %q", body["token"])
	}
	if strings.Contains(out, "s3cret") || !strings.Contains(out, "gitlab.example.com") {
		t.Errorf("output: %s", out)
	}
}

func TestGitAuthDeployKeyPrintsThePublicHalf(t *testing.T) {
	out, err := runGitAuth(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/projects/shop/git-auth/deploy-key" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"host":"github.com","publicKey":"ssh-ed25519 AAAAkey shpyrd shop","fingerprint":"SHA256:k","hostKeyFingerprint":"SHA256:h"}`))
	}, "", "deploy-key", "--project", "shop")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ssh-ed25519 AAAAkey shpyrd shop", "read-only", "SHA256:h"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestGitAuthShow(t *testing.T) {
	out, err := runGitAuth(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"url":"https://github.com/acme/shop","using":"token","token":{"host":"github.com","username":"x-access-token","updatedAt":"2026-10-16T10:00:00Z"}}`))
	}, "", "show", "--project", "shop")
	if err != nil || !strings.Contains(out, "token for github.com") {
		t.Errorf("%v: %s", err, out)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/cli/ -run GitAuth`
Expected: FAIL, `unknown command "git-auth"`.

- [ ] **Step 3: Write `internal/cli/gitauth.go`**

```go
package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/api"
)

// Git credentials (RFC-0017): `shpyrd git-auth`. A workspace admin sets a
// token per host; a project gets a deploy key shpyrd generates. Values are
// write-only. Signed in only: the server stores and checks them.

func newGitAuthCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "git-auth",
		Short: "Give builds access to private git repositories",
		Long: `Builds clone a project's git repository. For a private one, give shpyrd access
either way:

  a deploy key for the project (any git host; read-only, one repository):
    shpyrd git-auth deploy-key --project shop
    then add the printed key to the repository as a read-only deploy key

  a token for a host, for every project of the workspace (workspace admins):
    shpyrd git-auth set --host gitlab.example.com --token -    (reads stdin)

A project's deploy key wins over the workspace's token.

  shpyrd git-auth show --project shop     which credential the project's builds use
  shpyrd git-auth list                    the workspace's tokens (never their values)
  shpyrd git-auth unset --host gitlab.example.com
  shpyrd git-auth deploy-key --project shop --remove`,
	}
	var host, token, username string
	set := &cobra.Command{
		Use:   "set --host <host> --token <token|->",
		Short: "Set the workspace's token for a git host",
		RunE: func(cmd *cobra.Command, args []string) error {
			if host == "" || token == "" {
				return errors.New("give --host and --token (- reads it from stdin)")
			}
			if token == "-" {
				line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) {
					return err
				}
				token = strings.TrimSpace(line)
				if token == "" {
					return errors.New("no token on stdin")
				}
			}
			body, _ := json.Marshal(api.GitTokenRequest{Token: token, Username: username})
			var v api.GitTokenView
			if err := gitAuthRequest(g, cmd, "PUT", "api/workspace/git-auth/"+url.PathEscape(host), body, &v); err != nil {
				return err
			}
			return g.print(cmd, v, func(w io.Writer) {
				fmt.Fprintf(w, "Token for %s set (user %s). Builds of the workspace's projects on %s use it.\n", v.Host, v.Username, v.Host)
			})
		},
	}
	set.Flags().StringVar(&host, "host", "", "git host, such as gitlab.example.com")
	set.Flags().StringVar(&token, "token", "", "the token; - reads it from stdin (keeps it out of shell history)")
	set.Flags().StringVar(&username, "username", "", "user name sent with the token (default per host: x-access-token, oauth2 on gitlab.com)")

	var unsetHost string
	unset := &cobra.Command{
		Use:   "unset --host <host>",
		Short: "Remove the workspace's token for a git host",
		RunE: func(cmd *cobra.Command, args []string) error {
			if unsetHost == "" {
				return errors.New("give --host")
			}
			if err := gitAuthRequest(g, cmd, "DELETE", "api/workspace/git-auth/"+url.PathEscape(unsetHost), nil, nil); err != nil {
				return err
			}
			return g.print(cmd, map[string]string{"host": unsetHost}, func(w io.Writer) {
				fmt.Fprintf(w, "Token for %s removed.\n", unsetHost)
			})
		},
	}
	unset.Flags().StringVar(&unsetHost, "host", "", "git host")

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the workspace's git tokens (hosts, never values)",
		RunE: func(cmd *cobra.Command, args []string) error {
			var vs []api.GitTokenView
			if err := gitAuthRequest(g, cmd, "GET", "api/workspace/git-auth", nil, &vs); err != nil {
				return err
			}
			return g.print(cmd, map[string]any{"tokens": vs}, func(w io.Writer) {
				if len(vs) == 0 {
					fmt.Fprintln(w, "no git tokens set")
					return
				}
				tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "HOST\tUSER\tSET BY")
				for _, v := range vs {
					fmt.Fprintf(tw, "%s\t%s\t%s\n", v.Host, v.Username, firstNonEmpty(v.UpdatedBy, "-"))
				}
				_ = tw.Flush()
			})
		},
	}

	var appName, keyHost string
	var remove bool
	deployKey := &cobra.Command{
		Use:   "deploy-key [--project <name>] [--host <host>] [--remove]",
		Short: "Make a deploy key for the project and print its public half",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveAppName(appName)
			if err != nil {
				return err
			}
			path := "api/projects/" + url.PathEscape(name) + "/git-auth/deploy-key"
			if remove {
				if err := gitAuthRequest(g, cmd, "DELETE", path, nil, nil); err != nil {
					return err
				}
				return g.print(cmd, map[string]string{"project": name}, func(w io.Writer) {
					fmt.Fprintf(w, "Deploy key of %s removed. Remove it from the repository too.\n", name)
				})
			}
			body, _ := json.Marshal(api.DeployKeyRequest{Host: keyHost})
			var v api.DeployKeyView
			if err := gitAuthRequest(g, cmd, "POST", path, body, &v); err != nil {
				return err
			}
			return g.print(cmd, v, func(w io.Writer) {
				fmt.Fprintf(w, "Add this key to the repository on %s as a read-only deploy key:\n\n  %s\n\n", v.Host, v.PublicKey)
				fmt.Fprintf(w, "On GitHub: the repository's Settings, Deploy keys, Add deploy key (leave write access off).\n")
				fmt.Fprintf(w, "%s's host key was recorded as %s; builds refuse a host that presents another.\n", v.Host, v.HostKeyFingerprint)
				fmt.Fprintf(w, "Then deploy again. A new deploy-key replaces this one.\n")
			})
		},
	}
	appFlag(deployKey, &appName)
	deployKey.Flags().StringVar(&keyHost, "host", "", "git host (default: the project's repository's)")
	deployKey.Flags().BoolVar(&remove, "remove", false, "remove the project's deploy key")

	var showApp string
	show := &cobra.Command{
		Use:   "show [--project <name>]",
		Short: "Say which credential the project's builds clone with",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveAppName(showApp)
			if err != nil {
				return err
			}
			var v api.ProjectGitAuthView
			if err := gitAuthRequest(g, cmd, "GET", "api/projects/"+url.PathEscape(name)+"/git-auth", nil, &v); err != nil {
				return err
			}
			return g.print(cmd, v, func(w io.Writer) {
				switch v.Using {
				case "deploy-key":
					fmt.Fprintf(w, "%s clones %s with its deploy key %s (for %s).\n", name, v.URL, v.DeployKey.Fingerprint, v.DeployKey.Host)
				case "token":
					fmt.Fprintf(w, "%s clones %s with the workspace's token for %s (user %s).\n", name, v.URL, v.Token.Host, v.Token.Username)
				default:
					fmt.Fprintf(w, "%s clones %s without credentials: fine for a public repository. For a private one: shpyrd git-auth --help\n", name, firstNonEmpty(v.URL, "(no git repository)"))
				}
			})
		},
	}
	appFlag(show, &showApp)

	cmd.AddCommand(set, unset, list, deployKey, show)
	return cmd
}

// gitAuthRequest calls the API signed in; git credentials are not managed
// over a kubeconfig.
func gitAuthRequest(g *globalFlags, cmd *cobra.Command, method, path string, body []byte, out any) error {
	ctx := signalContext()
	ac, err := newAppClient(g, g.progress(cmd))
	if err != nil {
		return err
	}
	if !ac.session {
		return errors.New("git credentials are managed signed in: run shpyrd login first")
	}
	ct := ""
	if body != nil {
		ct = "application/json"
	}
	raw, err := ac.serverRequest(ctx, method, path, body, ct)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("unexpected response: %s", truncate(string(raw), 200))
	}
	return nil
}
```

Register it in `root.go` after `newDrainsCmd`: `root.AddCommand(newGitAuthCmd(g))`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/cli/...`
Expected: PASS. If `TestGitAuthShow` reaches the kubeconfig path, check how
`build_logs_test.go` sets `preferKubeconfig` and do the same.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/gitauth.go internal/cli/gitauth_test.go internal/cli/root.go
git commit -m "feat(cli): shpyrd git-auth, tokens per host and deploy keys per project (RFC-0017)"
```

---

### Task 8: Docs, RFC, a real run on the local cluster, and the PR with screenshots

**Files:**
- Modify: `content/docs/deploying.mdx` (a "Private repositories" section next to the `--git` deploy section), `content/docs/cli.mdx` (a `shpyrd git-auth` entry in the style of the `drains` entry), `rfcs/0017-git-credentials.md`, `rfcs/README.md` (line 60)

- [ ] **Step 1: Docs**

In `deploying.mdx`, under the section on `shpyrd deploy --git`, add a
"Private repositories" section that says:
- the two ways (deploy key per project, token per host per workspace), with
  the commands from Task 7's long help;
- that the deploy key wins over the token;
- the token usernames per host;
- that the host key is recorded when the key is made and checked by
  Dockerfile builds;
- "Connect GitHub" is coming (RFC-0054), without promising dates.

In `cli.mdx`, add `shpyrd git-auth` with its five subcommands. Follow the
house style of the surrounding pages: short sentences, lowercase `shpyrd`.

- [ ] **Step 2: RFC status**

`rfcs/0017-git-credentials.md`:
- `**Status:** implemented`, `**Owner:**` the maintainer's name as in other
  RFCs, `**Last update:** <today>`.
- Under Implementation History:
  `- 2026-10-16: tokens per host per workspace and deploy keys per project, as labelled Secrets; <app>-git for kpack and BuildKit; shpyrd git-auth. A cluster-wide default token, from the Proposal, is not built: a workspace token covers it. See docs/superpowers/specs/2026-10-16-private-git-repositories-design.md.`

`rfcs/README.md` line 60: status `implemented`.

- [ ] **Step 3: Full test run**

Run: `go build ./... && go vet ./pkg/gitauth/... ./pkg/api/... ./internal/... && go test ./pkg/... ./internal/...`
Expected: all pass. If the repository has a lint target (`grep -n "^lint" Makefile`), run it too.

- [ ] **Step 4: A real run on the local kind cluster**

Follow the local cluster memory (kind on ports 80 and 443; load the built
server and controller images, not `extensions enable`). Build and load:
`grep -n "^kind\|^dev\|load" Makefile` names the targets. Then:

1. Make a **private** test repository the maintainer agrees to use (ask for
   it; do not create repositories on their account unasked). It holds a tiny
   Node app and a `Dockerfile`.
2. `shpyrd deploy --git <its https URL>` with no credentials. Expect the build
   to fail with Task 6's message. Capture the project page.
3. `shpyrd git-auth deploy-key --project <p>`, add the key on GitHub
   (read-only), `shpyrd deploy --git ...` again. Expect it to build and run.
4. Switch the project to the Dockerfile strategy and deploy again. Expect it
   to build and run.
5. `shpyrd git-auth deploy-key --remove`, then
   `shpyrd git-auth set --host github.com --token -` with a fine-grained token
   (contents: read on that one repository, given by the maintainer). Deploy
   again. Expect it to build and run, and `shpyrd git-auth show` to say
   "token".

- [ ] **Step 5: Screenshots**

With the Chrome tools, capture from the dashboard:
- the project page with the refused-clone message;
- the project page running from the private repository;
- the build log showing `source ready at <sha>`.

Capture the CLI transcripts of steps 2 to 5 as text. Save the images under
the scratchpad, not the repository.

- [ ] **Step 6: Commit the docs and RFC, push, open the PR**

```bash
git add content/docs rfcs
git commit -m "docs: private repositories with git-auth; RFC-0017 implemented"
git push -u origin feat/private-git-repositories
```

Open the PR with `gh pr create`:
- Title: `Private repositories, part 1: git credentials (RFC-0017)`.
- Body: what it does; the deliberate deviations listed at the top of this
  plan; how it was tested; the screenshots; the CLI transcripts in code
  blocks; and that PRs 2 to 4 follow (the watcher, then the GitHub App).
- End the body with:
  ```
  🤖 Generated with [Claude Code](https://claude.com/claude-code)

  https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb
  ```
- Images go in by uploading them to the PR (`gh` cannot attach images). Put
  the PR up first, then give the maintainer the image paths to drop into the
  description, or upload them through the browser tools on the PR page.
