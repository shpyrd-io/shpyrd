//go:build !foss

package licensing

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// The files in testdata are copies of shpyrd-license's testdata: the same
// bytes, signed by Node, verified here.

func testdata(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func testKeys(t *testing.T) map[string]ed25519.PublicKey {
	t.Helper()
	k, err := ParsePublicKey([]byte(testdata(t, "test.public.pem")))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]ed25519.PublicKey{"test": k}
}

// withTestKeys makes Parse verify against the test key and puts the
// clock and the state back afterwards.
func withTestKeys(t *testing.T, at time.Time) {
	t.Helper()
	ks := testKeys(t)
	prevKeys, prevNow := publicKeys, now
	publicKeys = func() (map[string]ed25519.PublicKey, error) { return ks, nil }
	now = func() time.Time { return at }
	t.Cleanup(func() {
		publicKeys, now = prevKeys, prevNow
		current.Store(nil)
		unlocked.Store(false)
	})
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var oct10 = time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)

func TestTheLicensesSignedByNodeVerify(t *testing.T) {
	ks := testKeys(t)
	l, err := ParseWith(testdata(t, "valid.jwt"), ks)
	if err != nil {
		t.Fatal(err)
	}
	want := License{
		ID:        "lic_test_valid",
		Customer:  "Test Customer",
		IssuedAt:  time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if l != want {
		t.Fatalf("got %+v, want %+v", l, want)
	}
	if !l.ActiveAt(oct10) {
		t.Fatal("the valid license is not in force")
	}
	expired, err := ParseWith(testdata(t, "expired.jwt"), ks)
	if err != nil {
		t.Fatalf("an expired license still parses: %v", err)
	}
	if expired.ActiveAt(oct10) || expired.Customer != "Test Customer" {
		t.Fatalf("expired: %+v", expired)
	}
}

func TestALicenseIsOffFromItsExpiry(t *testing.T) {
	l := License{ExpiresAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}
	if !l.ActiveAt(time.Date(2029, 12, 31, 23, 59, 59, 0, time.UTC)) {
		t.Fatal("off before its expiry")
	}
	if l.ActiveAt(l.ExpiresAt) {
		t.Fatal("still on at its expiry")
	}
}

func TestRefusedLicenses(t *testing.T) {
	ks := testKeys(t)
	for name, want := range map[string]error{
		"tampered.jwt":    ErrSignature,
		"unknown-kid.jwt": ErrKid,
		"alg-none.jwt":    ErrAlg,
	} {
		if _, err := ParseWith(testdata(t, name), ks); !errors.Is(err, want) {
			t.Errorf("%s: got %v, want %v", name, err, want)
		}
	}
	for _, bad := range []string{"", "a.b", "a.b.c.d", "!!.e30.", "W10.e30.", "e30.e30.not base64"} {
		if _, err := ParseWith(bad, ks); !errors.Is(err, ErrMalformed) {
			t.Errorf("%q: got %v, want ErrMalformed", bad, err)
		}
	}
}

// The keys built into the binary parse, and the production key that signs
// the licenses sold since 2026-10 is among them: without it no license
// issued would verify.
func TestTheKeysOfThisBuildParse(t *testing.T) {
	ks, err := loadKeys(keysFS, "keys")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ks["2026-10"]; !ok {
		t.Errorf("the production key 2026-10 is not built in: %v", ks)
	}
}

func TestActive(t *testing.T) {
	withTestKeys(t, oct10)
	if Active() {
		t.Fatal("on with no license read")
	}
	Load(testdata(t, "valid.jwt"))
	if s := Current(); !s.Active || s.License == nil || s.License.Customer != "Test Customer" || s.Error != "" {
		t.Fatalf("valid: %+v", s)
	}
	Load(testdata(t, "expired.jwt"))
	if s := Current(); s.Active || s.License == nil || s.Error != "" {
		t.Fatalf("expired: %+v", s)
	}
	Load(testdata(t, "tampered.jwt"))
	if s := Current(); s.Active || s.License != nil || s.Error == "" {
		t.Fatalf("tampered: %+v", s)
	}
	Load("")
	if s := Current(); s.Active || s.License != nil || s.Error != "" {
		t.Fatalf("none: %+v", s)
	}
	Unlock()
	if s := Current(); !s.Active || !s.Unlocked {
		t.Fatalf("unlocked: %+v", s)
	}
}

func TestTheLicenseGoesOffOnItsDayWithoutBeingReadAgain(t *testing.T) {
	withTestKeys(t, oct10)
	Load(testdata(t, "valid.jwt"))
	now = func() time.Time { return time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC) }
	if Active() {
		t.Fatal("still on at its expiry")
	}
}

func TestWriteRefusesWhatIsNotInForce(t *testing.T) {
	withTestKeys(t, oct10)
	k := fake.NewSimpleClientset()
	ctx := context.Background()
	if _, err := Write(ctx, k, "shpyrd-system", testdata(t, "expired.jwt")); err == nil {
		t.Fatal("an expired license was written")
	}
	if _, err := Write(ctx, k, "shpyrd-system", testdata(t, "tampered.jwt")); !errors.Is(err, ErrSignature) {
		t.Fatalf("tampered: %v", err)
	}
	if tok, _ := Read(ctx, k, "shpyrd-system"); tok != "" {
		t.Fatal("a refused license reached the Secret")
	}
}

func TestWriteThenWatchPutsTheLicenseInForce(t *testing.T) {
	withTestKeys(t, oct10)
	k := fake.NewSimpleClientset()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := Write(ctx, k, "shpyrd-system", testdata(t, "valid.jwt")); err != nil {
		t.Fatal(err)
	}
	// A second write replaces the first.
	if _, err := Write(ctx, k, "shpyrd-system", testdata(t, "valid.jwt")); err != nil {
		t.Fatal(err)
	}
	sec, err := k.CoreV1().Secrets("shpyrd-system").Get(ctx, SecretName, metav1.GetOptions{})
	if err != nil || len(sec.Data[SecretKey]) == 0 {
		t.Fatalf("secret: %v %v", sec, err)
	}
	done := make(chan struct{})
	go func() { Watch(ctx, k, "shpyrd-system", time.Hour, discard()); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for !Active() {
		if time.Now().After(deadline) {
			t.Fatal("the watch never read the license")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestRequireAnswers402WithoutALicense(t *testing.T) {
	withTestKeys(t, oct10)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", Require(), func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	get := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
		return w
	}
	w := get()
	var body map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusPaymentRequired || body["error"] != "available with a license" {
		t.Fatalf("without a license: %d %s", w.Code, w.Body)
	}
	Load(testdata(t, "valid.jwt"))
	if w := get(); w.Code != http.StatusOK {
		t.Fatalf("with a license: %d", w.Code)
	}
}

func TestStatusOfWhatTheSecretHolds(t *testing.T) {
	withTestKeys(t, oct10)
	if s := statusOf("", oct10); s.Active || s.License != nil {
		t.Fatalf("none: %+v", s)
	}
	if s := statusOf(testdata(t, "expired.jwt"), oct10); s.Active || s.License == nil {
		t.Fatalf("expired: %+v", s)
	}
	if s := statusOf(testdata(t, "unknown-kid.jwt"), oct10); s.Error == "" {
		t.Fatalf("unknown kid: %+v", s)
	}
}

// router is an ext.Router whose groups are one gin engine.
type router struct{ e *gin.Engine }

func (r router) Public() gin.IRouter         { return r.e }
func (r router) Protected() gin.IRouter      { return r.e }
func (r router) WorkspaceAdmin() gin.IRouter { return r.e }
func (r router) Admin() gin.IRouter          { return r.e }
func (r router) Platform() gin.IRouter       { return r.e }

func TestTheConsoleReadsTheLicenseState(t *testing.T) {
	withTestKeys(t, oct10)
	gin.SetMode(gin.TestMode)
	e := gin.New()
	if err := New().Routes(router{e}, ext.Deps{}); err != nil {
		t.Fatal(err)
	}
	get := func() Status {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/cluster/license", nil))
		var s Status
		if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil || w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		return s
	}
	if s := get(); s.Active || s.License != nil {
		t.Fatalf("none: %+v", s)
	}
	Load(testdata(t, "valid.jwt"))
	if s := get(); !s.Active || s.License.ID != "lic_test_valid" {
		t.Fatalf("valid: %+v", s)
	}
	Load("")
	Unlock()
	if s := get(); !s.Active || !s.Unlocked {
		t.Fatalf("unlocked: %+v", s)
	}
}
