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
	// KindGitHubApp is a token the GitHub App minted for the one
	// repository (RFC-0054).
	KindGitHubApp = "github-app"
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
	// AnnotationGitHubAccount is, on <app>-git, the account whose GitHub
	// App installation minted its token, and AnnotationTokenMintedAt when
	// (RFC3339). Only a GitHub App credential carries them (RFC-0054).
	AnnotationGitHubAccount = "shpyrd.io/github-account"
	AnnotationTokenMintedAt = "shpyrd.io/github-token-minted-at"

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
	// Kind is KindGitHubApp, KindDeployKey, KindToken, or "" when the clone
	// goes without credentials (a public repository).
	Kind string
	// CloneURL is the URL the build clones: the app's own URL when its form
	// fits the credential, converted only when it does not (an ssh URL with
	// a token, an https URL with a deploy key). kpack rebuilds when the URL
	// changes, so a form that fits is never rewritten.
	CloneURL string
	// Host is the repository's host.
	Host string
	// Secret is <app>-git for the project namespace; nil without a credential.
	Secret *corev1.Secret
	// AppRefused says why the GitHub App could not give access, when it
	// was tried and the build fell through to another credential.
	AppRefused string
	// AppRetry says the GitHub App failed for a reason that may pass (GitHub
	// down, slow or rate-limiting): an App token already written that has
	// not expired may be kept, and the App is tried again soon.
	AppRetry bool
}

// Resolve chooses the credential a build of gitURL clones with: the
// project's deploy key when it is for the repository's host, else the
// workspace's token for the host, else none. deployKey and token may be nil.
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
	given := strings.TrimSpace(gitURL)
	switch {
	case DeployKeyFits(deployKey, r.Host):
		cloneURL := given
		if r.Scheme != "ssh" {
			cloneURL = r.SSH()
		}
		// kpack matches the clone URL's host with its port (url.Host)
		// against this annotation: an ssh:// URL that names a port needs
		// it here, or kpack clones without the key. Only an ssh URL is
		// kept with its port; a converted one is the scp form, port 22.
		matches := "git@" + r.Host
		if r.Scheme == "ssh" && r.Port != "" {
			matches += ":" + r.Port
		}
		meta.Annotations = map[string]string{kpackGitAnnotation: matches}
		return Build{Kind: KindDeployKey, CloneURL: cloneURL, Host: r.Host, Secret: &corev1.Secret{
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
		cloneURL := given
		if r.Scheme == "ssh" {
			cloneURL = r.HTTPS()
		}
		return Build{Kind: KindToken, CloneURL: cloneURL, Host: r.Host, Secret: &corev1.Secret{
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

// DeployKeyFits says a project's deploy key applies to a repository on
// host: a key is for the one host it was made for.
func DeployKeyFits(deployKey *corev1.Secret, host string) bool {
	return deployKey != nil && strings.EqualFold(deployKey.Annotations[AnnotationHost], host)
}
