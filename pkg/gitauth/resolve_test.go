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
