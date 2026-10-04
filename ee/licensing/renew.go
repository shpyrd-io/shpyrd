//go:build !foss

package licensing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Online renewal, for a license the billing app issued (iss): before it
// expires the cluster sends it there, with what the cluster used and cost
// over the last 30 days (Report), and gets the next one. The leader tries
// every hour from renewBefore ahead of the expiry and notes the outcome on
// the Secret, for the console and `license status`. An expired license
// does not renew: the customer asks for a new one.
//
// The same billing app is where the customer pays: the server sends it the
// license and gets back a link of one use that opens the customer's own
// account (BillingLink). The license never leaves the cluster otherwise.

const (
	renewBefore = 7 * 24 * time.Hour
	renewEvery  = time.Hour
	// The license Secret's notes on the last renewal tried.
	annRenewalAt    = "shpyrd.io/license-renewal-at"
	annRenewalError = "shpyrd.io/license-renewal-error"
)

// The billing app's routes, under the license's issuer.
const (
	renewPath   = "/api/licenses/renew"
	sessionPath = "/api/licenses/session"
)

// Why a license does not renew online.
var (
	ErrNoLicense     = errors.New("no license is installed")
	ErrOffline       = errors.New("this license was issued by hand: it renews offline, with a new one (shpyrd-ctl license set)")
	ErrExpiredOnline = errors.New("the license has expired: it no longer renews online; ask for a new one")
)

// Renewal is the last online renewal tried.
type Renewal struct {
	At    time.Time `json:"at"`
	Error string    `json:"error,omitempty"`
}

// Renewer renews the cluster's license online.
type Renewer struct {
	Kube      kubernetes.Interface
	Namespace string
	Store     store.Store
	// Cluster names the cluster in the reports.
	Cluster string
	HTTP    *http.Client
	Logger  *slog.Logger
}

// Start renews while the manager runs: on the leader only.
func (r *Renewer) Start(ctx context.Context) error {
	t := time.NewTicker(renewEvery)
	defer t.Stop()
	for {
		r.tick(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func (r *Renewer) tick(ctx context.Context) {
	l, err := r.Renew(ctx, false)
	if l == nil && err == nil {
		return // nothing due
	}
	if err != nil && r.Logger != nil {
		r.Logger.Warn("license: online renewal", "error", err)
	}
	r.note(ctx, err)
}

// Renew renews the license when it is due (renewBefore ahead of its
// expiry) or, forced, now. It returns the new license; nil and no error
// when nothing was due (or, unforced, there is nothing to renew online).
func (r *Renewer) Renew(ctx context.Context, force bool) (*License, error) {
	token, _, err := ReadSecret(ctx, r.Kube, r.Namespace)
	if err != nil {
		return nil, err
	}
	if token == "" {
		if force {
			return nil, ErrNoLicense
		}
		return nil, nil
	}
	cur, err := Parse(token)
	if err != nil {
		if force {
			return nil, err
		}
		return nil, nil
	}
	if cur.Issuer == "" {
		if force {
			return nil, ErrOffline
		}
		return nil, nil
	}
	at := now()
	if !cur.ActiveAt(at) {
		return nil, ErrExpiredOnline
	}
	if !force && at.Before(cur.ExpiresAt.Add(-renewBefore)) {
		return nil, nil
	}
	rep, err := BuildReport(ctx, r.Store, r.Cluster, at.Add(-reportWindow), at)
	if err != nil {
		return nil, fmt.Errorf("the usage report: %w", err)
	}
	var out struct {
		License string `json:"license"`
	}
	if err := post(ctx, r.HTTP, cur.Issuer, renewPath, map[string]any{"license": token, "report": rep}, &out); err != nil {
		return nil, err
	}
	next, err := Parse(out.License)
	if err != nil {
		return nil, fmt.Errorf("the license %s sent: %w", cur.Issuer, err)
	}
	if next.Customer != cur.Customer || !next.ExpiresAt.After(cur.ExpiresAt) {
		return nil, fmt.Errorf("%s sent a license for another customer, or one that expires no later", cur.Issuer)
	}
	if _, err := Write(ctx, r.Kube, r.Namespace, out.License); err != nil {
		return nil, err
	}
	Load(out.License)
	return &next, nil
}

// note writes the outcome of a renewal on the Secret.
func (r *Renewer) note(ctx context.Context, failed error) {
	secrets := r.Kube.CoreV1().Secrets(r.Namespace)
	sec, err := secrets.Get(ctx, SecretName, metav1.GetOptions{})
	if err != nil {
		return
	}
	if sec.Annotations == nil {
		sec.Annotations = map[string]string{}
	}
	sec.Annotations[annRenewalAt] = now().UTC().Format(time.RFC3339)
	if failed != nil {
		sec.Annotations[annRenewalError] = failed.Error()
	} else {
		delete(sec.Annotations, annRenewalError)
	}
	if _, err := secrets.Update(ctx, sec, metav1.UpdateOptions{}); err != nil && !apierrors.IsConflict(err) && r.Logger != nil {
		r.Logger.Warn("license: noting the renewal", "error", err)
	}
}

// BillingLink asks the license's billing app for a link of one use to the
// customer's account. An expired license still opens it: a customer pays
// to get the next one.
func BillingLink(ctx context.Context, client *http.Client, k kubernetes.Interface, namespace string) (string, error) {
	token, _, err := ReadSecret(ctx, k, namespace)
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", ErrNoLicense
	}
	l, err := Parse(token)
	if err != nil {
		return "", err
	}
	if l.Issuer == "" {
		return "", errors.New("this license was issued by hand: it names no billing app")
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := post(ctx, client, l.Issuer, sessionPath, map[string]any{"license": token}, &out); err != nil {
		return "", err
	}
	if !strings.HasPrefix(out.URL, l.Issuer+"/") {
		return "", fmt.Errorf("%s sent a link elsewhere", l.Issuer)
	}
	return out.URL, nil
}

// post sends JSON to the issuer and reads JSON back; an answer that is not
// a success carries the issuer's message.
func post(ctx context.Context, client *http.Client, issuer, path string, in, out any) error {
	if err := safeIssuer(issuer); err != nil {
		return err
	}
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, issuer+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", issuer, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		return fmt.Errorf("%s answered %d: %s", issuer, res.StatusCode, firstNonEmpty(e.Error, strings.TrimSpace(string(raw))))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s answered something else than JSON", issuer)
	}
	return nil
}

// safeIssuer takes only https, but on the machine itself and the local
// names development uses.
func safeIssuer(issuer string) error {
	u, err := url.Parse(issuer)
	if err != nil || u.Host == "" {
		return fmt.Errorf("the license names no usable billing app (%q)", issuer)
	}
	if u.Scheme == "https" {
		return nil
	}
	host := u.Hostname()
	if u.Scheme == "http" && (host == "localhost" || net.ParseIP(host).IsLoopback() || strings.HasSuffix(host, ".test") || strings.HasSuffix(host, ".localhost")) {
		return nil
	}
	return fmt.Errorf("the license's billing app must be https (%q)", issuer)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
