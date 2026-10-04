//go:build !foss

package licensing

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// SecretName is the Secret, in the system namespace, that holds the
// cluster's license under SecretKey.
const (
	SecretName = "shpyrd-license"
	SecretKey  = "license"
)

// The cluster's license, as the server last read it.
type loaded struct {
	license *License
	err     error
}

var (
	current  atomic.Pointer[loaded]
	unlocked atomic.Bool
	now      = time.Now
)

// Unlock switches every ee feature on without a license: the cloud's
// build, which runs the platform itself.
func Unlock() { unlocked.Store(true) }

// Active says whether ee features are on now: unlocked, or a license in
// force.
func Active() bool {
	if unlocked.Load() {
		return true
	}
	l := current.Load()
	return l != nil && l.license != nil && l.err == nil && l.license.ActiveAt(now())
}

// Load sets the cluster's license from a token; "" means none.
func Load(token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		current.Store(&loaded{})
		return
	}
	l, err := Parse(token)
	if err != nil {
		current.Store(&loaded{err: err})
		return
	}
	current.Store(&loaded{license: &l})
}

// Status is what the console and the CLI show.
type Status struct {
	// Active says ee features are on.
	Active bool `json:"active"`
	// Unlocked says they are on without a license (the cloud).
	Unlocked bool `json:"unlocked,omitempty"`
	// License is the license installed, expired or not; nil when none is.
	License *License `json:"license,omitempty"`
	// Error says why the license installed is refused.
	Error string `json:"error,omitempty"`
	// Renewal is the last online renewal tried.
	Renewal *Renewal `json:"renewal,omitempty"`
}

// Current is the license state now.
func Current() Status {
	s := Status{Active: Active(), Unlocked: unlocked.Load()}
	if l := current.Load(); l != nil {
		s.License = l.license
		if l.err != nil {
			s.Error = l.err.Error()
		}
	}
	return s
}

// Read returns the token the Secret holds, "" when there is none.
func Read(ctx context.Context, k kubernetes.Interface, namespace string) (string, error) {
	token, _, err := ReadSecret(ctx, k, namespace)
	return token, err
}

// ReadSecret returns the token the Secret holds and the last online
// renewal noted on it (nil when none was tried).
func ReadSecret(ctx context.Context, k kubernetes.Interface, namespace string) (string, *Renewal, error) {
	sec, err := k.CoreV1().Secrets(namespace).Get(ctx, SecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	var r *Renewal
	if at, err := time.Parse(time.RFC3339, sec.Annotations[annRenewalAt]); err == nil {
		r = &Renewal{At: at, Error: sec.Annotations[annRenewalError]}
	}
	return string(sec.Data[SecretKey]), r, nil
}

// Write stores a license in the Secret after checking it: a license that
// does not verify, or has expired, is refused.
func Write(ctx context.Context, k kubernetes.Interface, namespace, token string) (License, error) {
	l, err := Parse(token)
	if err != nil {
		return License{}, err
	}
	if !l.ActiveAt(now()) {
		return l, errors.New("the license expired on " + l.ExpiresAt.Format("2006-01-02"))
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: namespace, Labels: map[string]string{"app.kubernetes.io/managed-by": "shpyrd"}},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{SecretKey: []byte(strings.TrimSpace(token))},
	}
	secrets := k.CoreV1().Secrets(namespace)
	if _, err := secrets.Create(ctx, sec, metav1.CreateOptions{}); err == nil {
		return l, nil
	} else if !apierrors.IsAlreadyExists(err) {
		return l, err
	}
	cur, err := secrets.Get(ctx, SecretName, metav1.GetOptions{})
	if err != nil {
		return l, err
	}
	cur.Labels, cur.Data, cur.StringData = sec.Labels, sec.Data, nil
	_, err = secrets.Update(ctx, cur, metav1.UpdateOptions{})
	return l, err
}

// Watch reads the Secret now and then every interval until ctx ends, so a
// license set with the CLI is in force within that time. A failed read
// keeps the license read before.
func Watch(ctx context.Context, k kubernetes.Interface, namespace string, interval time.Duration, logger *slog.Logger) {
	read := func() {
		token, err := Read(ctx, k, namespace)
		if err != nil {
			logger.Warn("license: reading the Secret", "error", err)
			return
		}
		Load(token)
	}
	read()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			read()
		}
	}
}

// Require answers 402 to a request for an ee feature when ee features are
// off.
func Require() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !Active() {
			c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{"error": "available with a license"})
			return
		}
		c.Next()
	}
}

// ResetForTest forgets the license read and any Unlock: for tests of the
// features the license switches on.
func ResetForTest() {
	current.Store(nil)
	unlocked.Store(false)
}
