package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DefaultURL is the catalog shpyrd publishes, read unless the operator
// names another (SHPYRD_CATALOG_URL) or none ("off").
const DefaultURL = "https://shpyrd-io.github.io/catalog/index.json"

// What a fetch reads at most.
const (
	maxIndexSize   = 4 << 20
	maxReadmeSize  = 256 << 10
	maxOverlaySize = 8 << 20
)

// Fetcher reads a published catalog and keeps it: the index is read once
// an hour (TTL), with the ETag the source gave, and served from memory in
// between; while the source cannot be read, the last index read is
// served, and the error is kept for the operator.
type Fetcher struct {
	// URL is the index's address; its files are resolved against it.
	URL string
	// Client is the HTTP client; nil is one with a 20 s timeout.
	Client *http.Client
	// TTL is how long an index is served before the source is asked again;
	// zero is an hour.
	TTL time.Duration

	mu        sync.Mutex
	index     *Index
	etag      string
	fetchedAt time.Time
	lastErr   error
	now       func() time.Time
}

// ErrNoCatalog says this server reads no catalog.
var ErrNoCatalog = errors.New("this server reads no catalog (SHPYRD_CATALOG_URL is off)")

// New is a fetcher of the catalog at u; "" is the default catalog, "off"
// none at all (nil).
func New(u string) *Fetcher {
	switch strings.TrimSpace(u) {
	case "off", "none":
		return nil
	case "":
		u = DefaultURL
	}
	return &Fetcher{URL: strings.TrimSpace(u)}
}

func (f *Fetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (f *Fetcher) ttl() time.Duration {
	if f.TTL > 0 {
		return f.TTL
	}
	return time.Hour
}

func (f *Fetcher) clock() time.Time {
	if f.now != nil {
		return f.now()
	}
	return time.Now()
}

// Index is the catalog: from memory while it is fresh, else read again.
// When the source cannot be read and an index was read before, that one
// is returned with no error; the error waits in Status.
func (f *Fetcher) Index(ctx context.Context) (*Index, error) {
	if f == nil {
		return nil, ErrNoCatalog
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.index != nil && f.clock().Sub(f.fetchedAt) < f.ttl() {
		return f.index, nil
	}
	ix, etag, err := f.read(ctx)
	switch {
	case err == nil:
		f.index, f.etag, f.fetchedAt, f.lastErr = ix, etag, f.clock(), nil
	case errors.Is(err, errNotModified):
		f.fetchedAt, f.lastErr = f.clock(), nil
	default:
		f.lastErr = err
		if f.index == nil {
			return nil, fmt.Errorf("the catalog at %s could not be read: %w", f.URL, err)
		}
		// Asked again in a minute, not on every request.
		f.fetchedAt = f.clock().Add(-f.ttl() + time.Minute)
	}
	return f.index, nil
}

// Refresh reads the index now, whatever its age.
func (f *Fetcher) Refresh(ctx context.Context) (*Index, error) {
	if f == nil {
		return nil, ErrNoCatalog
	}
	f.mu.Lock()
	f.fetchedAt = time.Time{}
	f.mu.Unlock()
	return f.Index(ctx)
}

// Status is where the catalog is read from, when it was last read, and
// what went wrong the last time it was asked, if anything.
type Status struct {
	URL       string    `json:"url"`
	FetchedAt time.Time `json:"fetchedAt,omitempty"`
	Entries   int       `json:"entries"`
	Error     string    `json:"error,omitempty"`
}

// Status reports the fetcher's state.
func (f *Fetcher) Status() Status {
	if f == nil {
		return Status{}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	st := Status{URL: f.URL, FetchedAt: f.fetchedAt}
	if f.index != nil {
		st.Entries = len(f.index.Entries)
	}
	if f.lastErr != nil {
		st.Error = f.lastErr.Error()
	}
	return st
}

var errNotModified = errors.New("not modified")

func (f *Fetcher) read(ctx context.Context) (*Index, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/json")
	if f.etag != "" && f.index != nil {
		req.Header.Set("If-None-Match", f.etag)
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return nil, "", errNotModified
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := readAll(resp.Body, maxIndexSize)
	if err != nil {
		return nil, "", err
	}
	ix, err := ParseIndex(b)
	if err != nil {
		return nil, "", err
	}
	return ix, resp.Header.Get("ETag"), nil
}

// Resolve is the address of a file of the catalog, given as a path from
// the index.
func (f *Fetcher) Resolve(rel string) (string, error) {
	base, err := url.Parse(f.URL)
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(rel)
	if err != nil {
		return "", err
	}
	if ref.IsAbs() || strings.HasPrefix(rel, "/") || strings.Contains(rel, "..") {
		return "", fmt.Errorf("%q is not a path inside the catalog", rel)
	}
	return base.ResolveReference(ref).String(), nil
}

// Readme is the entry's readme, as Markdown; "" when it has none.
func (f *Fetcher) Readme(ctx context.Context, e *IndexEntry) (string, error) {
	if e.Readme == "" {
		return "", nil
	}
	b, err := f.file(ctx, e.Readme, maxReadmeSize)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Overlay is the entry's overlay archive, checked against its digest.
func (f *Fetcher) Overlay(ctx context.Context, e *IndexEntry) ([]byte, error) {
	if e.Overlay == nil {
		return nil, nil
	}
	b, err := f.file(ctx, e.Overlay.Path, maxOverlaySize)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != e.Overlay.SHA256 {
		return nil, fmt.Errorf("the overlay of %s is not the one the index names: the catalog is being published, try again in a minute", e.Name)
	}
	if _, err := ReadOverlay(b); err != nil {
		return nil, fmt.Errorf("entry %s: %w", e.Name, err)
	}
	return b, nil
}

func (f *Fetcher) file(ctx context.Context, rel string, limit int64) ([]byte, error) {
	u, err := f.Resolve(rel)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", rel, resp.StatusCode)
	}
	return readAll(resp.Body, limit)
}

func readAll(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("larger than %d bytes", limit)
	}
	return b, nil
}
