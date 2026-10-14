package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/configvars"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestConfigEditsArePendingAndOnlyVariablesAreVisible(t *testing.T) {
	app := sampleApp("web1", shpyrdv1.PhaseRunning)
	active := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: app.EnvSecretName(), Namespace: app.Namespace}, Data: map[string][]byte{"PASSWORD": []byte("original-private")}}
	s, cr := newTestServer(t, nil, []client.Object{app, active})
	for _, body := range []string{`{"set":{"PASSWORD":"new-private"}}`, `{"kind":"variable","set":{"APP_URL":"https://example.test","EMPTY":""}}`} {
		rec := do(t, s, "PUT", "/api/projects/web1/secrets", body, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("save: %d %s", rec.Code, rec.Body)
		}
	}
	if err := cr.Get(context.Background(), client.ObjectKeyFromObject(active), active); err != nil {
		t.Fatal(err)
	}
	if string(active.Data["PASSWORD"]) != "original-private" || len(active.Data) != 1 {
		t.Fatal("saving changed the active environment")
	}
	rec := do(t, s, "GET", "/api/projects/web1/secrets", "", true)
	var got ConfigVarsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Pending || len(got.Variables) != 2 || len(got.Vars) != 1 || strings.Contains(rec.Body.String(), "private") {
		t.Fatalf("response: %s", rec.Body)
	}
	if got.Variables[1].Value == nil || *got.Variables[1].Value != "" {
		t.Fatal("empty value omitted")
	}
	rec = do(t, s, "PUT", "/api/projects/web1/secrets", `{"kind":"variable","convert":["PASSWORD"]}`, true)
	if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), "new-private") {
		t.Fatalf("unconfirmed conversion: %d %s", rec.Code, rec.Body)
	}
	rec = do(t, s, "PUT", "/api/projects/web1/secrets", `{"kind":"variable","convert":["PASSWORD"],"confirmPlain":true}`, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "new-private") {
		t.Fatalf("confirmed conversion: %d %s", rec.Code, rec.Body)
	}
	rec = do(t, s, "PUT", "/api/projects/web1/secrets", `{"kind":"secret","convert":["PASSWORD"]}`, true)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "new-private") {
		t.Fatalf("make secret: %d %s", rec.Code, rec.Body)
	}
	pending := &corev1.Secret{}
	if err := cr.Get(context.Background(), client.ObjectKey{Namespace: app.Namespace, Name: configvars.PendingName(app)}, pending); err != nil {
		t.Fatal(err)
	}
	if string(pending.Data["PASSWORD"]) != "new-private" || configvars.Plain(pending, "PASSWORD") {
		t.Fatal("conversion lost the value")
	}
}
