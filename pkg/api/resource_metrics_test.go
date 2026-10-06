package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext/all"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestResourceMetrics(t *testing.T) {
	for _, kind := range []string{"postgres", "redis"} {
		t.Run(kind, func(t *testing.T) {
			prom, queries := newFakeProm(t, fakeAnswer{Match: "container_cpu", Series: []fakeSeries{{Values: []Point{{1700000000, 0.5}}}}})
			objects := []client.Object{sampleApp("shop", "Running")}
			meta := metav1.ObjectMeta{Name: "db", Namespace: "app-shop"}
			owner := "Cluster"
			if kind == "postgres" {
				objects = append(objects, &shpyrdv1.Postgres{ObjectMeta: meta})
			} else {
				objects = append(objects, &shpyrdv1.Redis{ObjectMeta: meta})
				owner = "StatefulSet"
			}
			s, _ := newTestServer(t, prom, objects)
			s.opts.Extensions = all.All()
			path := "/api/projects/shop/resources/" + kind + "/db/metrics"
			rec := do(t, s, "GET", path+"?range=6h", "", true)
			if rec.Code != http.StatusOK {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
			var response MetricsResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Range != "6h" || response.Step != 360 || len(response.Charts) != 4 || len(response.Charts[0].Series) != 1 {
				t.Fatalf("%+v", response)
			}
			for _, q := range queries.Queries() {
				for _, want := range []string{`namespace="app-shop"`, `owner_name="db"`, `owner_kind="` + owner + `"`} {
					if !strings.Contains(q, want) {
						t.Fatalf("unscoped query %s", q)
					}
				}
			}
			for _, tc := range []struct {
				path   string
				auth   bool
				status int
			}{
				{path, false, 401}, {path + "?range=2h", true, 400},
				{strings.Replace(path, "/db/", "/missing/", 1), true, 404},
				{strings.Replace(path, "/shop/", "/missing/", 1), true, 404},
			} {
				if r := do(t, s, "GET", tc.path, "", tc.auth); r.Code != tc.status {
					t.Fatalf("%s: %d %s", tc.path, r.Code, r.Body.String())
				}
			}
			s.prom = nil
			if r := do(t, s, "GET", path, "", true); r.Code != 501 {
				t.Fatalf("without monitoring: %d", r.Code)
			}
		})
	}
}
