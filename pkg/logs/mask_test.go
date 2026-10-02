package logs

import (
	"bytes"
	"strings"
	"testing"
	"testing/iotest"

	corev1 "k8s.io/api/core/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// A build's log is served masked (#54): the project's secret values,
// whole, line by line, and a URL's password on its own; short values and
// plain variables stay readable; a value cut between two reads of the
// stream is still found.
func TestMasker(t *testing.T) {
	app := &shpyrdv1.App{Spec: shpyrdv1.AppSpec{Env: []corev1.EnvVar{{Name: "NODE_ENV", Value: "production"}}}}
	key := "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASC\n-----END PRIVATE KEY-----\n"
	m := NewMasker(BuildSecrets(app, map[string][]byte{
		"type":         []byte("shpyrd-env"),
		"NODE_ENV":     []byte("production"), // plain env: of shpyrd.yaml
		"DATABASE_URL": []byte("postgres://app:Zq9marked%2Fpw@db-rw:5432/app"),
		"API_KEY":      []byte("sk-live-0123456789abcdef"),
		"PRIVATE_KEY":  []byte(key),
		"FEATURE":      []byte("true"), // too short to mask
	}))
	for in, want := range map[string]string{
		"connecting to postgres://app:Zq9marked%2Fpw@db-rw:5432/app\n":      "connecting to ***\n",
		"password=Zq9marked/pw host=db-rw\n":                                "password=*** host=db-rw\n",
		"password=Zq9marked%2Fpw\n":                                         "password=***\n",
		"key sk-live-0123456789abcdef and again sk-live-0123456789abcdef\n": "key *** and again ***\n",
		"MIIEvQIBADANBgkqhkiG9w0BAQEFAASC\n":                                "***\n",
		"NODE_ENV=production FEATURE=true\n":                                "NODE_ENV=production FEATURE=true\n",
	} {
		if got := m.Line(in); got != want {
			t.Errorf("Line(%q) = %q, want %q", in, got, want)
		}
	}

	// A stream read a byte at a time: the value is split between reads.
	var out bytes.Buffer
	src := iotest.OneByteReader(strings.NewReader("one sk-live-0123456789abcdef\ntwo, no newline at the end: sk-live-0123456789abcdef"))
	if err := m.Copy(&out, src); err != nil {
		t.Fatal(err)
	}
	if out.String() != "one ***\ntwo, no newline at the end: ***" {
		t.Errorf("Copy = %q", out.String())
	}

	// Nothing to mask: a nil Masker passes everything through.
	var none *Masker = NewMasker([]string{"short", "", "true"})
	if none != nil || none.Line("short true") != "short true" {
		t.Error("values under the minimum length must not be masked")
	}
	out.Reset()
	if err := none.Copy(&out, strings.NewReader("as it is")); err != nil || out.String() != "as it is" {
		t.Errorf("nil Copy = %q %v", out.String(), err)
	}
}
