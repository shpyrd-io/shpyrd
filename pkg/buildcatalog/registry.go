package buildcatalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Registry resolves a tag against the registry that holds it, by the
// distribution API: one request for the manifest, with the anonymous
// bearer token the registry asks for (Docker Hub, GitHub's) when it does.
// The images are the public buildpacks and stacks: no credential.
type Registry struct {
	// Client is the HTTP client; nil is one with a 30 s timeout.
	Client *http.Client
	// Scheme is https unless set (tests).
	Scheme string
}

// manifestTypes are the manifest kinds the registry may answer with: a
// multi-architecture index first (the buildpackages are amd64 + arm64).
const manifestTypes = "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"

// Digest is the digest ref's tag points at now.
func (r Registry) Digest(ctx context.Context, ref string) (string, error) {
	host, repo, tag := Split(ref)
	if IsDigest(ref) {
		return ref[strings.Index(ref, "@")+1:], nil
	}
	scheme := r.Scheme
	if scheme == "" {
		scheme = "https"
	}
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	manifest := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", scheme, apiHost(host), repo, tag)
	resp, err := r.get(ctx, client, manifest, "")
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		resp.Body.Close()
		token, err := r.token(ctx, client, challenge)
		if err != nil {
			return "", err
		}
		if resp, err = r.get(ctx, client, manifest, token); err != nil {
			return "", err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: HTTP %d", ref, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if d := resp.Header.Get("Docker-Content-Digest"); d != "" {
		return d, nil
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (r Registry) get(ctx context.Context, client *http.Client, url, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", manifestTypes)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return client.Do(req)
}

var challengeParam = regexp.MustCompile(`(\w+)="([^"]*)"`)

// ErrCredential: the registry wants a credential the resolver has none of
// (the platform's own registry, say). The field is left as a tag, which
// kpack follows with the credential it has.
var ErrCredential = errors.New("the registry asks for a credential")

// token answers a registry's bearer challenge (WWW-Authenticate: Bearer
// realm="...",service="...",scope="...") with an anonymous token request.
func (r Registry) token(ctx context.Context, client *http.Client, challenge string) (string, error) {
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
		return "", fmt.Errorf("%w (%q)", ErrCredential, challenge)
	}
	params := map[string]string{}
	for _, m := range challengeParam.FindAllStringSubmatch(challenge, -1) {
		params[m[1]] = m[2]
	}
	realm, err := url.Parse(params["realm"])
	if err != nil || realm.Host == "" {
		return "", fmt.Errorf("the registry's token realm is unreadable (%q)", challenge)
	}
	q := realm.Query()
	if s := params["service"]; s != "" {
		q.Set("service", s)
	}
	if s := params["scope"]; s != "" {
		q.Set("scope", s)
	}
	realm.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return "", fmt.Errorf("token: %w", err)
	}
	if out.Token == "" {
		out.Token = out.AccessToken
	}
	if out.Token == "" {
		return "", fmt.Errorf("token: the registry returned none")
	}
	return out.Token, nil
}

// Split takes a reference apart: host (index.docker.io when the reference
// names none), repository and tag ("latest" when none; "" for a digest
// reference).
func Split(ref string) (host, repo, tag string) {
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i]
		defer func() { tag = "" }()
	}
	host, repo = "index.docker.io", ref
	if i := strings.Index(ref, "/"); i >= 0 && (strings.ContainsAny(ref[:i], ".:") || ref[:i] == "localhost") {
		host, repo = ref[:i], ref[i+1:]
	}
	tag = "latest"
	if colon := strings.LastIndex(repo, ":"); colon >= 0 {
		repo, tag = repo[:colon], repo[colon+1:]
	}
	if host == "index.docker.io" && !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}
	return host, repo, tag
}

// apiHost is where a registry's API answers: Docker Hub's is not its
// image host.
func apiHost(host string) string {
	switch host {
	case "docker.io", "index.docker.io":
		return "registry-1.docker.io"
	}
	return host
}
