package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// A workspace owner signed in with a token and no kubeconfig manages the
// backups of a database and the sizes of databases and stores (#74, #57):
// every command speaks the API, none asks for a cluster.
func TestDatabaseCommandsWithATokenAndNoKubeconfig(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	answers := map[string]string{
		"PUT /api/projects/shop/resources/postgres/db/backups":    `{"name":"db","policy":{"retention":"7d"},"backups":[]}`,
		"DELETE /api/projects/shop/resources/postgres/db/backups": `{"name":"db","policy":null,"backups":[]}`,
		"GET /api/projects/shop/resources/postgres/db/backups":    `{"name":"db","policy":{},"lastBackup":"2026-10-05T02:00:00Z","backups":[{"name":"db-20261005-100000","started":"2026-10-05T10:00:00Z","phase":"completed","kind":"on demand"}]}`,
		"POST /api/projects/shop/resources/postgres/db/backups":   `{"name":"db-20261005-100000","phase":"pending","kind":"on demand"}`,
		"POST /api/projects/shop/resources/postgres/db/restore":   `{"kind":"Postgres","name":"db2","phase":"Pending","attachedTo":[]}`,
		"GET /api/projects/shop/resources":                        `[{"kind":"Postgres","name":"db2","phase":"Ready","endpoint":"db2-rw.app-shop.svc:5432","attachedTo":[]}]`,
		"POST /api/projects/shop/resources/Postgres/db/resize":    `{"kind":"Postgres","name":"db","phase":"Ready","attachedTo":[],"note":"db now has the size shared-m."}`,
		"POST /api/projects/shop/resources/Redis/cache/resize":    `{"kind":"Redis","name":"cache","phase":"Ready","attachedTo":[],"note":"cache now has the size shared-m."}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, `{"error":"sign in"}`, http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		key := r.Method + " " + r.URL.Path
		mu.Lock()
		calls = append(calls, strings.TrimSpace(key+" "+string(body)))
		mu.Unlock()
		answer, ok := answers[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, answer)
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("SHPYRD_URL", srv.URL)
	t.Setenv("SHPYRD_TOKEN", "test-token")
	defer func(prev bool) { preferKubeconfig = prev }(preferKubeconfig)

	run := func(args ...string) string {
		t.Helper()
		var out, errOut bytes.Buffer
		root := New()
		root.SetOut(&out)
		root.SetErr(&errOut)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, errOut.String())
		}
		return out.String()
	}
	for _, c := range []struct {
		args []string
		says string
		sent string
	}{
		{[]string{"pg", "backups", "enable", "db", "--project", "shop", "--retention", "7d"}, "Backups of db: on, daily at 02:00 UTC, kept 7d", `PUT /api/projects/shop/resources/postgres/db/backups {"retention":"7d"}`},
		{[]string{"pg", "backups", "list", "db", "--project", "shop"}, "db-20261005-100000", "GET /api/projects/shop/resources/postgres/db/backups"},
		{[]string{"pg", "backup", "db", "--project", "shop"}, "Backup db-20261005-100000 completed.", "POST /api/projects/shop/resources/postgres/db/backups"},
		{[]string{"pg", "restore", "db", "--as", "db2", "--to", "2026-10-05T09:00:00Z", "--project", "shop"}, "Database db2 is ready", `POST /api/projects/shop/resources/postgres/db/restore {"as":"db2","size":"","storage":"","to":"2026-10-05T09:00:00Z"}`},
		{[]string{"pg", "backups", "disable", "db", "--yes", "--project", "shop"}, "Backups of db are off.", "DELETE /api/projects/shop/resources/postgres/db/backups"},
		{[]string{"pg", "resize", "db", "shared-m", "--project", "shop"}, "db now has the size shared-m.", `POST /api/projects/shop/resources/Postgres/db/resize {"size":"shared-m"}`},
		{[]string{"redis", "resize", "cache", "shared-m", "--project", "shop"}, "cache now has the size shared-m.", `POST /api/projects/shop/resources/Redis/cache/resize {"size":"shared-m"}`},
	} {
		mu.Lock()
		calls = nil
		mu.Unlock()
		if out := run(c.args...); !strings.Contains(out, c.says) {
			t.Errorf("%v said:\n%s", c.args, out)
		}
		mu.Lock()
		if len(calls) == 0 || calls[0] != c.sent {
			t.Errorf("%v sent %q, want first %q", c.args, calls, c.sent)
		}
		mu.Unlock()
	}
}
