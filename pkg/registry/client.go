// Package registry is a small client for the OCI distribution API of the
// platform's image registry: what the dashboard needs to show the images
// it holds and what the controller needs to delete the ones no release
// references (RFC-0059). TLS trust comes from the process (SSL_CERT_FILE
// points at the cluster's trust bundle in the server pod).
package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

// Client talks to one registry with one credential.
type Client struct {
	// Host is the registry host[:port]; the base URL is https://Host.
	Host     string
	Username string
	Password string
	// HTTP defaults to a client with a 30 s timeout.
	HTTP *http.Client
}

// FromDockerConfig builds a client for host from a dockerconfigjson document
// (the shpyrd-registry Secret). The credential may be absent.
func FromDockerConfig(host string, dockerconfig []byte) *Client {
	c := &Client{Host: host}
	var cfg struct {
		Auths map[string]struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Auth     string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(dockerconfig, &cfg); err == nil {
		if a, ok := cfg.Auths[host]; ok {
			c.Username, c.Password = a.Username, a.Password
			if c.Username == "" && a.Auth != "" {
				if dec, err := base64.StdEncoding.DecodeString(a.Auth); err == nil {
					if u, p, ok := strings.Cut(string(dec), ":"); ok {
						c.Username, c.Password = u, p
					}
				}
			}
		}
	}
	return c
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) do(ctx context.Context, method, path string, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "https://"+c.Host+path, nil)
	if err != nil {
		return nil, err
	}
	if c.Username != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		return nil, &Error{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	return resp, nil
}

// Error is a non-2xx answer.
type Error struct {
	Status int
	Body   string
}

func (e *Error) Error() string { return fmt.Sprintf("registry: HTTP %d: %s", e.Status, e.Body) }

// IsNotFound says the error is a 404.
func IsNotFound(err error) bool {
	e, ok := err.(*Error)
	return ok && e.Status == http.StatusNotFound
}

// Ping checks the API is reachable and the credential accepted.
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "/v2/", "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Catalog lists the repositories (paginated by the registry; up to limit).
func (c *Client) Catalog(ctx context.Context, limit int) ([]string, error) {
	var out []string
	path := fmt.Sprintf("/v2/_catalog?n=%d", min(limit, 1000))
	for path != "" && len(out) < limit {
		resp, err := c.do(ctx, http.MethodGet, path, "")
		if err != nil {
			return nil, err
		}
		var page struct {
			Repositories []string `json:"repositories"`
		}
		err = json.NewDecoder(resp.Body).Decode(&page)
		next := nextLink(resp.Header.Get("Link"))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, page.Repositories...)
		path = next
	}
	return out, nil
}

// Tags lists the tags of a repository.
func (c *Client) Tags(ctx context.Context, repo string) ([]string, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v2/"+repo+"/tags/list", "")
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Tags, nil
}

// DeleteManifest removes a manifest by digest (every tag pointing at it goes
// with it; blobs are reclaimed by garbage collection). A missing manifest is
// not an error.
func (c *Client) DeleteManifest(ctx context.Context, repo, digest string) error {
	resp, err := c.do(ctx, http.MethodDelete, "/v2/"+repo+"/manifests/"+digest, "")
	if err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	resp.Body.Close()
	return nil
}

// SplitReference splits "host[:port]/repo[:tag][@digest]" into its parts.
func SplitReference(ref string) (host, repo, digest string) {
	if i := strings.Index(ref, "@"); i >= 0 {
		digest = ref[i+1:]
		ref = ref[:i]
	}
	host, repo, _ = strings.Cut(ref, "/")
	if i := strings.LastIndex(repo, ":"); i >= 0 && !strings.Contains(repo[i:], "/") {
		repo = repo[:i]
	}
	return host, repo, digest
}

// nextLink extracts the path of an RFC 5988 Link header with rel="next".
func nextLink(h string) string {
	for _, part := range strings.Split(h, ",") {
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start, end := strings.Index(part, "<"), strings.Index(part, ">")
		if start < 0 || end <= start {
			continue
		}
		u, err := url.Parse(part[start+1 : end])
		if err != nil {
			continue
		}
		return u.RequestURI()
	}
	return ""
}

// ProcessTypes reads the process types a Cloud Native Buildpacks image
// declares (web, worker, release, ...) from its config's
// io.buildpacks.build.metadata label. ref is a digest or a tag; an image
// index resolves to the entry for this platform. Images not built by
// buildpacks have no label: an empty list, no error.
func (c *Client) ProcessTypes(ctx context.Context, repo, ref string) ([]string, error) {
	const accept = "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"
	resp, err := c.do(ctx, "GET", "/v2/"+repo+"/manifests/"+ref, accept)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	var manifest struct {
		MediaType string `json:"mediaType"`
		Config    struct {
			Digest string `json:"digest"`
		} `json:"config"`
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				OS           string `json:"os"`
				Architecture string `json:"architecture"`
			} `json:"platform"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if manifest.Config.Digest == "" && len(manifest.Manifests) > 0 {
		// An index: this platform's image, else the first.
		pick := manifest.Manifests[0].Digest
		for _, m := range manifest.Manifests {
			if m.Platform.OS == "linux" && m.Platform.Architecture == runtime.GOARCH {
				pick = m.Digest
				break
			}
		}
		return c.ProcessTypes(ctx, repo, pick)
	}
	if manifest.Config.Digest == "" {
		return nil, errors.New("manifest has no config")
	}
	resp, err = c.do(ctx, "GET", "/v2/"+repo+"/blobs/"+manifest.Config.Digest, "")
	if err != nil {
		return nil, err
	}
	cfgBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if err := json.Unmarshal(cfgBody, &cfg); err != nil {
		return nil, fmt.Errorf("image config: %w", err)
	}
	raw := cfg.Config.Labels["io.buildpacks.build.metadata"]
	if raw == "" {
		return []string{}, nil
	}
	var meta struct {
		Processes []struct {
			Type string `json:"type"`
		} `json:"processes"`
	}
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, fmt.Errorf("buildpack metadata: %w", err)
	}
	out := make([]string, 0, len(meta.Processes))
	for _, p := range meta.Processes {
		if p.Type != "" {
			out = append(out, p.Type)
		}
	}
	return out, nil
}
