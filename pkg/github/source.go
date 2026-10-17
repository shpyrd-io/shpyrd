package github

import (
	"context"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Recheck is how long the Secret read last stands: a new App, a removed
// one or a broken one is seen within it.
const Recheck = 30 * time.Second

// Source is the App as its Secret says, for the server and the
// controllers: the Secret read at most every 30 s, and one Client per
// version of it, so its tokens are kept.
type Source struct {
	Reader    client.Reader
	Namespace string
	Options   Options

	mu      sync.Mutex
	client  *Client
	version string
	missing bool
	// broken is why the Secret read last makes no App: callers get it
	// until the next read.
	broken  error
	checked time.Time
}

// Client is the App's client; ErrNotConfigured while there is no usable
// Secret.
func (s *Source) Client(ctx context.Context) (*Client, error) {
	if s == nil || s.Reader == nil {
		return nil, ErrNotConfigured
	}
	now := time.Now()
	if s.Options.Now != nil {
		now = s.Options.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.checked.IsZero() && now.Sub(s.checked) < Recheck {
		if s.missing {
			return nil, ErrNotConfigured
		}
		if s.broken != nil {
			return nil, s.broken
		}
		if s.client != nil {
			return s.client, nil
		}
	}
	sec := &corev1.Secret{}
	err := s.Reader.Get(ctx, types.NamespacedName{Namespace: s.Namespace, Name: SecretName}, sec)
	if apierrors.IsNotFound(err) {
		s.client, s.version, s.missing, s.broken, s.checked = nil, "", true, nil, now
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, err
	}
	s.missing, s.broken, s.checked = false, nil, now
	if s.client != nil && sec.ResourceVersion == s.version {
		return s.client, nil
	}
	// A Secret that no longer makes an App stops the old client: callers get
	// the error, never the previous App's credentials.
	app, err := AppFromSecret(sec)
	if err != nil {
		s.client, s.version, s.broken = nil, "", err
		return nil, err
	}
	c, err := New(app, s.Options)
	if err != nil {
		s.client, s.version, s.broken = nil, "", err
		return nil, err
	}
	s.client, s.version = c, sec.ResourceVersion
	return c, nil
}

// Invalidate drops the last read, so the next Client reads the Secret
// again: the Secret's own event says it changed, and the 30 s must not
// hide that from the reconcile it queues.
func (s *Source) Invalidate() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.checked = time.Time{}
	s.mu.Unlock()
}
