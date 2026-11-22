package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const maintenanceMessage = "The platform is in scheduled maintenance of its image storage, which takes under an hour. This build starts by itself when the maintenance ends; nothing needs doing."

// projectAnswer is one answer of GET /api/projects/shop.
type projectAnswer struct {
	phase, message, reason, build string
}

// maintenanceClient serves the answers in turn (the last one from then on)
// and returns a client on that server and its output.
func maintenanceClient(t *testing.T, answers ...projectAnswer) (*appClient, *bytes.Buffer) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("SHPYRD_TOKEN", "test-token")
	defer func(prev bool) { preferKubeconfig = prev }(preferKubeconfig)
	prevPoll := waitPoll
	waitPoll = time.Millisecond
	t.Cleanup(func() { waitPoll = prevPoll })
	var mu sync.Mutex
	reads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/projects/shop") {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		a := answers[min(reads, len(answers)-1)]
		reads++
		mu.Unlock()
		status := map[string]any{"phase": a.phase, "message": a.message, "latestBuild": a.build, "generation": 3, "observedGeneration": 3}
		if a.reason != "" {
			status["conditions"] = []map[string]any{{"type": "Built", "status": "Unknown", "reason": a.reason, "message": a.message, "lastTransitionTime": time.Now().Format(time.RFC3339)}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"slug": "shop", "status": status})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SHPYRD_URL", srv.URL)
	out := &bytes.Buffer{}
	ac, err := newAppClient(&globalFlags{}, out)
	if err != nil {
		t.Fatal(err)
	}
	return ac, out
}

func TestADeployHeldForMaintenanceKeepsWaitingForTheBuild(t *testing.T) {
	held := projectAnswer{phase: "Building", message: maintenanceMessage, reason: "RegistryMaintenance", build: "shop-build-1"}
	// Far more reads than the limit allows at 1ms: the wait outlives it.
	answers := make([]projectAnswer, 0, 100)
	for range 99 {
		answers = append(answers, held)
	}
	answers = append(answers, projectAnswer{phase: "Building", build: "shop-build-2"})
	ac, out := maintenanceClient(t, answers...)
	ac.out = out

	// The build's log stream is not served: the wait ends there, after the
	// new build was seen, which is all this test needs.
	err := ac.followBuildAPI(context.Background(), "shop", "shop-build-1", 20*time.Millisecond)
	if err != nil && strings.Contains(err.Error(), "no build started") {
		t.Fatalf("a held build timed out: %v", err)
	}
	if n := strings.Count(out.String(), maintenanceMessage); n != 1 {
		t.Errorf("the maintenance message was printed %d times, want once:\n%s", n, out.String())
	}
}

func TestADeployHeldForMaintenanceIsNotTimedOutWhileReleasing(t *testing.T) {
	held := projectAnswer{phase: "Building", message: maintenanceMessage, reason: "RegistryMaintenance"}
	answers := make([]projectAnswer, 0, 100)
	for range 99 {
		answers = append(answers, held)
	}
	answers = append(answers, projectAnswer{phase: "Running"})
	ac, _ := maintenanceClient(t, answers...)
	d, err := ac.waitRunningAPI(context.Background(), "shop", 3, 20*time.Millisecond, nil)
	if err != nil {
		t.Fatalf("a held project then running = %v", err)
	}
	if d.Status.Phase != "Running" {
		t.Errorf("phase = %s", d.Status.Phase)
	}
}

func TestABuildingProjectWithoutTheReasonStillTimesOut(t *testing.T) {
	building := projectAnswer{phase: "Building", message: "Building", build: "shop-build-1"}
	ac, _ := maintenanceClient(t, building)
	err := ac.followBuildAPI(context.Background(), "shop", "shop-build-1", 30*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "no build started within") {
		t.Errorf("followBuildAPI = %v, want no build started", err)
	}
	ac, _ = maintenanceClient(t, building)
	_, err = ac.waitRunningAPI(context.Background(), "shop", 3, 30*time.Millisecond, nil)
	if err == nil || !strings.Contains(err.Error(), "timed out waiting") {
		t.Errorf("waitRunningAPI = %v, want timed out", err)
	}
}

var _ = io.Discard
