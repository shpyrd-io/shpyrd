package configvars

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func TestVisibilityRequiresExplicitConsent(t *testing.T) {
	sec := &corev1.Secret{Data: map[string][]byte{"OLD": []byte("private")}}
	if len(Public(sec)) != 0 {
		t.Fatal("legacy secret became visible")
	}
	if err := ApplyKind(sec, map[string]string{"APP_URL": "https://example.test"}, nil, nil, true, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(Secrets(sec))
	if strings.Contains(string(raw), "private") {
		t.Fatal("secret leaked")
	}
	vars := Public(sec)
	if len(vars) != 1 || vars[0].Value == nil || *vars[0].Value != "https://example.test" {
		t.Fatalf("variables = %+v", vars)
	}
	for _, name := range []string{"OLD", "API_KEY", "AUTH_TOKEN", "DATABASE_PASSWORD", "CLIENT_SECRET"} {
		if err := ApplyKind(sec, map[string]string{name: "value"}, nil, nil, true, false, time.Now()); err == nil {
			t.Fatalf("published %s without consent", name)
		}
	}
	if err := ApplyKind(sec, nil, nil, []string{"OLD"}, true, false, time.Now()); err == nil {
		t.Fatal("converted without consent")
	}
	if err := ApplyKind(sec, nil, nil, []string{"OLD"}, true, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(Public(sec)) != 2 {
		t.Fatal("confirmed conversion missing")
	}
	if err := ApplyKind(sec, nil, nil, []string{"OLD"}, false, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if Plain(sec, "OLD") || string(sec.Data["OLD"]) != "private" {
		t.Fatal("make secret changed value or kept it visible")
	}
	if err := ApplyKind(sec, nil, []string{"APP_URL"}, nil, false, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, ok := sec.Data["APP_URL"]; !ok {
		t.Fatal("secret unset removed a variable")
	}
}
