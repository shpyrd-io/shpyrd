package objectgateway

import (
	"container/list"
	"sync"
	"time"
)

// Authentication keeps a short memory of descriptors so that one GET to the
// provider serves a minute of a consumer's requests instead of one GET per
// request. The memory is bounded: a flood of unknown access keys evicts the
// oldest entries and costs, at worst, what uncached authentication cost.
//
// A replica forgets a descriptor the moment it changes it (new credential,
// revocation, deletion). The other replica still answers from its memory
// until the entry ages out, so a revocation is in force everywhere within
// descriptorTTL of the change.
const (
	descriptorTTL       = time.Minute
	descriptorCacheSize = 4096
)

type descriptorCache struct {
	ttl  time.Duration
	size int
	now  func() time.Time

	mu      sync.Mutex
	entries map[string]*list.Element
	recent  *list.List // front is the most recently used
}

type descriptorEntry struct {
	name    string
	rec     Record
	err     error // a cached "no such bucket"; transient errors are never kept
	expires time.Time
}

func newDescriptorCache(size int, ttl time.Duration, now func() time.Time) *descriptorCache {
	if now == nil {
		now = time.Now
	}
	return &descriptorCache{ttl: ttl, size: size, now: now, entries: map[string]*list.Element{}, recent: list.New()}
}

// lookup returns the remembered outcome for name while it is fresh.
func (c *descriptorCache) lookup(name string) (Record, error, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[name]
	if !ok {
		return Record{}, nil, false
	}
	e := el.Value.(*descriptorEntry)
	if !c.now().Before(e.expires) {
		c.recent.Remove(el)
		delete(c.entries, name)
		return Record{}, nil, false
	}
	c.recent.MoveToFront(el)
	return e.rec, e.err, true
}

// remember stores an outcome, evicting the least recently used entry when
// full.
func (c *descriptorCache) remember(name string, rec Record, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := &descriptorEntry{name: name, rec: rec, err: err, expires: c.now().Add(c.ttl)}
	if el, ok := c.entries[name]; ok {
		el.Value = e
		c.recent.MoveToFront(el)
		return
	}
	for c.recent.Len() >= c.size && c.recent.Len() > 0 {
		last := c.recent.Back()
		delete(c.entries, last.Value.(*descriptorEntry).name)
		c.recent.Remove(last)
	}
	c.entries[name] = c.recent.PushFront(e)
}

// forget drops what is remembered about name; the next lookup misses.
func (c *descriptorCache) forget(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[name]; ok {
		c.recent.Remove(el)
		delete(c.entries, name)
	}
}
