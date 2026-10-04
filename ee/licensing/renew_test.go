//go:build !foss

package licensing

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// A billing app of the test's own: a key, licenses it signs, and what the
// cluster sent it.
type fakeBilling struct {
	t        *testing.T
	priv     ed25519.PrivateKey
	srv      *httptest.Server
	reports  []Report
	sessions int
	// expires is when the licenses it renews expire.
	expires time.Time
}

func newFakeBilling(t *testing.T, at time.Time) *fakeBilling {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b := &fakeBilling{t: t, priv: priv, expires: at.Add(37 * 24 * time.Hour)}
	prevKeys, prevNow := publicKeys, now
	publicKeys = func() (map[string]ed25519.PublicKey, error) { return map[string]ed25519.PublicKey{"bill": pub}, nil }
	now = func() time.Time { return at }
	t.Cleanup(func() {
		publicKeys, now = prevKeys, prevNow
		current.Store(nil)
	})
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			License string `json:"license"`
			Report  Report `json:"report"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if _, err := Parse(in.License); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not a license of ours"})
			return
		}
		switch r.URL.Path {
		case renewPath:
			b.reports = append(b.reports, in.Report)
			_ = json.NewEncoder(w).Encode(map[string]string{"license": b.sign("lic_next", b.expires)})
		case sessionPath:
			b.sessions++
			_ = json.NewEncoder(w).Encode(map[string]string{"url": b.srv.URL + "/c/session?t=once"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *fakeBilling) sign(id string, exp time.Time) string {
	return b.signAs(id, exp, b.srv.URL)
}

func (b *fakeBilling) signAs(id string, exp time.Time, issuer string) string {
	enc := func(v any) string {
		raw, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	claims := map[string]any{"jti": id, "sub": "Acme Corp", "iat": exp.Add(-37 * 24 * time.Hour).Unix(), "exp": exp.Unix()}
	if issuer != "" {
		claims["iss"] = issuer
	}
	head := enc(map[string]string{"alg": "EdDSA", "typ": "JWT", "kid": "bill"}) + "." + enc(claims)
	return head + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(b.priv, []byte(head)))
}

// installed is a cluster with a license in its Secret, a day of usage
// and of cost, and a renewer.
func installed(t *testing.T, token string, at time.Time) (*Renewer, *fake.Clientset) {
	t.Helper()
	k := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: "shpyrd-system"}, Data: map[string][]byte{SecretKey: []byte(token)}})
	st := store.NewMemory()
	ctx := context.Background()
	ws, err := st.Workspace(ctx, store.DefaultWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	q := func(v float64) *float64 { return &v }
	start := at.Add(-24 * time.Hour).Truncate(time.Hour)
	if err := st.WriteBuckets(ctx, []store.UsageBucket{
		{WorkspaceID: ws.ID, Project: "p1", Component: "web", Metric: store.MetricCPUUsed, PeriodStart: start, PeriodEnd: start.Add(5 * time.Minute), Quantity: q(7200), Unit: store.UnitCoreSeconds, Quality: "complete", Revision: 1},
		{WorkspaceID: ws.ID, Project: "p1", Component: "web", Metric: store.MetricMemoryReserved, PeriodStart: start, PeriodEnd: start.Add(5 * time.Minute), Quantity: q(3600), Unit: store.UnitGiBSeconds, Quality: "complete", Revision: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertCostLines(ctx, []store.CostLine{
		{Kind: store.CostEstimated, Source: "opencost", Start: start, End: start.Add(time.Hour), Workspace: ws.ID, Cost: q(12.5), Currency: "USD"},
		{Kind: store.CostReal, Source: "oci", Start: start, End: start.Add(24 * time.Hour), Resource: "ocid1.instance", Cost: q(80), Currency: "BRL"},
	}); err != nil {
		t.Fatal(err)
	}
	return &Renewer{Kube: k, Namespace: "shpyrd-system", Store: st, Cluster: "acme-prod"}, k
}

// A week before it expires, the license goes to the billing app that
// issued it with the last 30 days' usage and cost, and the next one is
// installed and in force; before that week nothing is sent.
func TestALicenseRenewsOnlineAWeekBeforeItExpires(t *testing.T) {
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	b := newFakeBilling(t, at)
	ctx := context.Background()

	early, _ := installed(t, b.sign("lic_early", at.Add(20*24*time.Hour)), at)
	if l, err := early.Renew(ctx, false); l != nil || err != nil || len(b.reports) != 0 {
		t.Fatalf("20 days ahead: %v %v, %d reports sent", l, err, len(b.reports))
	}

	r, k := installed(t, b.sign("lic_now", at.Add(3*24*time.Hour)), at)
	l, err := r.Renew(ctx, false)
	if err != nil || l == nil || l.ID != "lic_next" || !l.ExpiresAt.Equal(b.expires.Truncate(time.Second)) {
		t.Fatalf("renewal: %+v %v", l, err)
	}
	token, _ := Read(ctx, k, "shpyrd-system")
	if got, _ := Parse(token); got.ID != "lic_next" || !Active() {
		t.Errorf("installed: %+v, active %v", got, Active())
	}
	rep := b.reports[0]
	if rep.Cluster != "acme-prod" || rep.Usage.CPUCoreHours != 2 || rep.Usage.MemoryGiBHours != 1 || !rep.To.Equal(at) || !rep.From.Equal(at.Add(-30*24*time.Hour)) {
		t.Errorf("report: %+v", rep)
	}
	if len(rep.Costs) != 2 || rep.Costs[0] != (CostSum{Kind: store.CostEstimated, Currency: "USD", Amount: 12.5, Lines: 1}) || rep.Costs[1].Currency != "BRL" {
		t.Errorf("costs: %+v", rep.Costs)
	}
}

// An expired license does not renew online, a license issued by hand never
// does, and the outcome of a renewal is noted on the Secret.
func TestWhatDoesNotRenewOnline(t *testing.T) {
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	b := newFakeBilling(t, at)
	ctx := context.Background()

	r, k := installed(t, b.sign("lic_old", at.Add(-time.Hour)), at)
	if _, err := r.Renew(ctx, false); !errors.Is(err, ErrExpiredOnline) {
		t.Errorf("expired: %v", err)
	}
	r.tick(ctx)
	if _, renewal, _ := ReadSecret(ctx, k, "shpyrd-system"); renewal == nil || !strings.Contains(renewal.Error, "expired") {
		t.Errorf("the outcome noted: %+v", renewal)
	}

	byHand, _ := installed(t, b.signAs("lic_hand", at.Add(24*time.Hour), ""), at)
	if l, err := byHand.Renew(ctx, false); l != nil || err != nil {
		t.Errorf("by hand, unforced: %v %v", l, err)
	}
	if _, err := byHand.Renew(ctx, true); !errors.Is(err, ErrOffline) {
		t.Errorf("by hand, forced: %v", err)
	}
	if len(b.reports) != 0 {
		t.Errorf("%d reports sent", len(b.reports))
	}
}

// The billing link works with an expired license too: a customer pays to
// get the next one.
func TestTheBillingLinkOpensWithAnExpiredLicense(t *testing.T) {
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	b := newFakeBilling(t, at)
	_, k := installed(t, b.sign("lic_old", at.Add(-48*time.Hour)), at)
	link, err := BillingLink(context.Background(), nil, k, "shpyrd-system")
	if err != nil || link != b.srv.URL+"/c/session?t=once" || b.sessions != 1 {
		t.Fatalf("link: %q %v", link, err)
	}
}

// Only https, but on the machine itself and the local names.
func TestTheBillingAppMustBeHTTPS(t *testing.T) {
	for issuer, ok := range map[string]bool{
		"https://billing.example.com": true, "http://127.0.0.1:8080": true, "http://localhost:4329": true, "http://billing.example.test": true,
		"http://billing.example.com": false, "ftp://billing.example.com": false, "billing": false,
	} {
		if err := safeIssuer(issuer); (err == nil) != ok {
			t.Errorf("%s: %v", issuer, err)
		}
	}
}
