package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// #119: the kills the controller recorded come with the metrics, as
// markers for the Memory chart, within the range and never with a pod name.
func TestMetricsCarryTheOOMKills(t *testing.T) {
	prom, _ := newFakeProm(t)
	app := metricsApp()
	s, _ := newTestServer(t, prom, []client.Object{app})
	now := time.Now()
	kills := []store.OOMKill{
		{Namespace: app.Namespace, Pod: "shop-web-7d9f-l8tzg", Container: "app", Process: "web", Instance: "web.1", At: now.Add(-17 * time.Minute), LimitBytes: 64 << 20, Release: 3},
		{Namespace: app.Namespace, Pod: "shop-worker-5c2a-p0q1", Container: "app", Process: "worker", Instance: "worker.1", At: now.Add(-20 * time.Minute), LimitBytes: 128 << 20, Release: 3},
		{Namespace: app.Namespace, Pod: "shop-release-v2-a1b2", Container: "app", Process: "release", At: now.Add(-50 * time.Minute), LimitBytes: 64 << 20, Release: 2},
		{Namespace: app.Namespace, Pod: "shop-web-1111-old", Container: "app", Process: "web", Instance: "web.2", At: now.Add(-3 * time.Hour), LimitBytes: 64 << 20, Release: 1},
		{Namespace: "app-other", Pod: "other-web-2222-x", Container: "app", Process: "web", Instance: "web.1", At: now.Add(-5 * time.Minute), LimitBytes: 64 << 20},
	}
	if _, err := s.store.RecordOOMKills(context.Background(), kills); err != nil {
		t.Fatal(err)
	}

	rec := do(t, s, "GET", "/api/projects/shop/metrics?range=1h", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics: %d %s", rec.Code, rec.Body.String())
	}
	for _, k := range kills {
		if strings.Contains(rec.Body.String(), k.Pod) {
			t.Fatalf("a pod name left the server: %s", k.Pod)
		}
	}
	var out MetricsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	// This hour's three, oldest first, the other project's and the old one left out.
	if len(out.OOMs) != 3 || out.OOMs[0].Process != "release" || out.OOMs[1].Process != "worker" || out.OOMs[2].Process != "web" {
		t.Fatalf("ooms = %+v", out.OOMs)
	}
	web := out.OOMs[2]
	// shared-s is 64Mi, which the web process still runs at: the size is named.
	if web.Instance != "web.1" || web.Limit != 64<<20 || web.Size != "shared-s" || web.Release != 3 || web.Time != float64(kills[0].At.Unix()) {
		t.Errorf("web kill = %+v", web)
	}
	// The worker died at a limit that is not its size today: the limit alone.
	if out.OOMs[1].Size != "" || out.OOMs[1].Limit != 128<<20 {
		t.Errorf("worker kill = %+v", out.OOMs[1])
	}
	// The release step is no process of the spec: no size, no instance.
	if out.OOMs[0].Size != "" || out.OOMs[0].Instance != "" || out.OOMs[0].Release != 2 {
		t.Errorf("release kill = %+v", out.OOMs[0])
	}

	// A wider range reaches the old one; a process filter keeps that process's.
	if out = getMetrics(t, s, "?range=6h"); len(out.OOMs) != 4 {
		t.Errorf("6h ooms = %+v", out.OOMs)
	}
	if out = getMetrics(t, s, "?range=6h&process=web"); len(out.OOMs) != 2 || out.OOMs[0].Instance != "web.2" {
		t.Errorf("web ooms = %+v", out.OOMs)
	}
	// Without kills the list is there and empty, as releases is.
	if rec := do(t, s, "GET", "/api/projects/shop/metrics?range=15m", "", true); !strings.Contains(rec.Body.String(), `"ooms":[]`) {
		t.Errorf("no kills: %s", rec.Body.String())
	}
}

// The operator sees every kill of the cluster: the projects' and the
// platform's own components.
func TestClusterOOMKillsListsEveryNamespace(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	now := time.Now()
	if _, err := s.store.RecordOOMKills(context.Background(), []store.OOMKill{
		{Namespace: "app-shop", Pod: "shop-web-1", Container: "app", Process: "web", Instance: "web.1", At: now.Add(-time.Hour), LimitBytes: 64 << 20},
		{Namespace: "shpyrd-system", Pod: "platform-backup-29330123-q7x", Container: "backup", Process: "platform-backup", At: now.Add(-2 * time.Hour), LimitBytes: 512 << 20},
		{Namespace: "app-shop", Pod: "shop-web-0", Container: "app", Process: "web", At: now.Add(-48 * time.Hour), LimitBytes: 64 << 20},
	}); err != nil {
		t.Fatal(err)
	}
	rec := do(t, s, "GET", "/api/cluster/oom-kills", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("cluster oom kills: %d %s", rec.Code, rec.Body.String())
	}
	var out ClusterOOMKillsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Range != "24h" || len(out.Kills) != 2 || out.Kills[0].Process != "web" || out.Kills[1].Process != "platform-backup" || out.Kills[1].Pod == "" {
		t.Fatalf("kills = %+v", out)
	}
	if rec := do(t, s, "GET", "/api/cluster/oom-kills?range=7d", "", true); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "shop-web-0") {
		t.Errorf("7d: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "GET", "/api/cluster/oom-kills?range=2h", "", true); rec.Code != http.StatusBadRequest {
		t.Errorf("bad range: %d", rec.Code)
	}
}
