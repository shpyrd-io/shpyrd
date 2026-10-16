package objectgateway

import (
	"container/list"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v3"
	"github.com/versity/versitygw/s3err"
	"golang.org/x/time/rate"
)

// A byte cap bounds what a bucket holds, not what it is asked (storage
// plan step 5): every logical bucket gets a token bucket of its own, and
// a request past it is answered SlowDown, which every SDK retries with
// backoff. The limiters are kept for the buckets seen most recently, a
// bounded number of them.
const limiterCacheSize = 8192

type bucketLimiters struct {
	rate  rate.Limit
	burst int

	mu      sync.Mutex
	entries map[string]*list.Element
	recent  *list.List
}

type limiterEntry struct {
	name    string
	limiter *rate.Limiter
}

func newBucketLimiters(perSecond float64, burst int) *bucketLimiters {
	return &bucketLimiters{rate: rate.Limit(perSecond), burst: burst, entries: map[string]*list.Element{}, recent: list.New()}
}

func (l *bucketLimiters) allow(name string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	el, ok := l.entries[name]
	if ok {
		l.recent.MoveToFront(el)
	} else {
		el = l.recent.PushFront(&limiterEntry{name: name, limiter: rate.NewLimiter(l.rate, l.burst)})
		l.entries[name] = el
		for l.recent.Len() > limiterCacheSize {
			last := l.recent.Back()
			l.recent.Remove(last)
			delete(l.entries, last.Value.(*limiterEntry).name)
		}
	}
	return el.Value.(*limiterEntry).limiter.Allow()
}

// middleware refuses a request past its bucket's rate. The bucket is the
// first segment of the path (path style); a request without one (the
// listing of buckets) is not limited here.
func (l *bucketLimiters) middleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		name, _, _ := strings.Cut(strings.TrimPrefix(c.Path(), "/"), "/")
		if name == "" || l.allow(name) {
			return c.Next()
		}
		slowDowns.Inc()
		return s3err.GetAPIError(s3err.ErrSlowDown)
	}
}
