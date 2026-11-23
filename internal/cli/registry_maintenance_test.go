package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// buildListEntry is what the build list reports for one build.
type buildListEntry struct {
	number          int
	status, message string
}

// refusedBuildClient serves the project's answers in turn (the last one from
// then on), each build's log stream ("<name> output") and the build list, and
// returns a client on that server, its output and the names of the builds
// whose stream was requested.
func refusedBuildClient(t *testing.T, list map[string]buildListEntry, answers ...projectAnswer) (*appClient, *bytes.Buffer, func() []string) {
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
	var streamed []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/api/projects/shop"):
			mu.Lock()
			a := answers[min(reads, len(answers)-1)]
			reads++
			mu.Unlock()
			status := map[string]any{"phase": a.phase, "message": a.message, "latestBuild": a.build, "generation": 3, "observedGeneration": 3}
			if a.reason != "" {
				status["conditions"] = []map[string]any{{"type": "Built", "status": "Unknown", "reason": a.reason, "message": a.message, "lastTransitionTime": time.Now().Format(time.RFC3339)}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"slug": "shop", "status": status})
		case strings.HasSuffix(p, "/builds"):
			var out []map[string]any
			for name, b := range list {
				out = append(out, map[string]any{"name": name, "number": b.number, "status": b.status, "message": b.message})
			}
			_ = json.NewEncoder(w).Encode(out)
		case strings.HasSuffix(p, "/logs") && strings.Contains(p, "/builds/"):
			name := strings.TrimSuffix(p[strings.Index(p, "/builds/")+len("/builds/"):], "/logs")
			mu.Lock()
			streamed = append(streamed, name)
			mu.Unlock()
			fmt.Fprintf(w, "%s output\n", name)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SHPYRD_URL", srv.URL)
	out := &bytes.Buffer{}
	ac, err := newAppClient(&globalFlags{}, out)
	if err != nil {
		t.Fatal(err)
	}
	return ac, out, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), streamed...)
	}
}

const buildTwoFailure = "The build failed on a step of its own."

// repeat returns n copies of a.
func repeat(a projectAnswer, n int) []projectAnswer {
	out := make([]projectAnswer, n)
	for i := range out {
		out[i] = a
	}
	return out
}

// refusedRun runs the waits of `shpyrd deploy`: the build wait, then the
// release wait. The limits are short (20ms at a 1ms poll), so the reads given
// to the reason outlive them.
func refusedRun(ac *appClient) error {
	if err := ac.followBuildAPI(context.Background(), "shop", "shop-build-1", 20*time.Millisecond); err != nil {
		return err
	}
	_, err := ac.waitRunningAPI(context.Background(), "shop", 3, 20*time.Millisecond, nil)
	return err
}

var (
	twoBuilding   = projectAnswer{phase: "Building", message: "Building", build: "shop-build-2"}
	twoHeld       = projectAnswer{phase: "Building", message: maintenanceMessage, reason: "RegistryMaintenance", build: "shop-build-2"}
	threeBuilding = projectAnswer{phase: "Building", message: "Building", build: "shop-build-3"}
	refusedList   = map[string]buildListEntry{
		"shop-build-2": {2, "Failed", buildTwoFailure},
		"shop-build-3": {3, "Succeeded", ""},
	}
)

func assertRefusedThenRerun(t *testing.T, err error, out string, streamed []string) {
	t.Helper()
	if err != nil {
		t.Fatalf("the waits returned %v", err)
	}
	for _, bad := range []string{"no build started within", "timed out waiting", "build 2:"} {
		if strings.Contains(out, bad) {
			t.Errorf("the output has %q:\n%s", bad, out)
		}
	}
	if n := strings.Count(out, maintenanceMessage); n != 1 {
		t.Errorf("the maintenance message was printed %d times, want once:\n%s", n, out)
	}
	i2, im, i3 := strings.Index(out, "shop-build-2 output"), strings.Index(out, maintenanceMessage), strings.Index(out, "shop-build-3 output")
	if i2 < 0 || im < i2 || i3 < im {
		t.Errorf("want build 2's output, the maintenance message, build 3's output, in that order:\n%s", out)
	}
	if len(streamed) != 2 || streamed[0] != "shop-build-2" || streamed[1] != "shop-build-3" {
		t.Errorf("streams requested = %v", streamed)
	}
}

// C13 (a) and (g): `shpyrd deploy` and `shpyrd redeploy` both run
// followBuildAPI, so the one wait is what is tested.
func TestARefusedBuildIsFollowedByTheRerun(t *testing.T) {
	answers := append([]projectAnswer{twoBuilding}, repeat(twoHeld, 100)...)
	answers = append(answers, threeBuilding, threeBuilding, projectAnswer{phase: "Running"})
	ac, out, streamed := refusedBuildClient(t, refusedList, answers...)
	err := refusedRun(ac)
	assertRefusedThenRerun(t, err, out.String(), streamed())
}

func TestARefusedBuildBeforeAndAfterTheControllersVerdict(t *testing.T) {
	answers := append([]projectAnswer{twoBuilding}, repeat(twoBuilding, 5)...)
	answers = append(answers, repeat(twoHeld, 100)...)
	answers = append(answers, repeat(twoBuilding, 5)...)
	answers = append(answers, threeBuilding, threeBuilding, projectAnswer{phase: "Running"})
	ac, out, streamed := refusedBuildClient(t, refusedList, answers...)
	err := refusedRun(ac)
	assertRefusedThenRerun(t, err, out.String(), streamed())
}

func TestAnotherCauseOfAFailedBuildIsStillAFailure(t *testing.T) {
	failed := projectAnswer{phase: "Failed", message: "The project failed.", build: "shop-build-2"}
	ac, out, streamed := refusedBuildClient(t, refusedList, twoBuilding, failed)
	err := ac.followBuildAPI(context.Background(), "shop", "shop-build-1", 20*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "build 2") || !strings.Contains(err.Error(), buildTwoFailure) {
		t.Fatalf("followBuildAPI = %v, want build 2 and the list's message", err)
	}
	if strings.Contains(out.String(), maintenanceMessage) || len(streamed()) != 1 {
		t.Errorf("maintenance message printed or another stream requested: %v\n%s", streamed(), out.String())
	}
}

func TestAFailedBuildWithoutTheReasonStillEndsAtTheLimit(t *testing.T) {
	ac, _, _ := refusedBuildClient(t, refusedList, twoBuilding)
	done := make(chan error, 1)
	go func() {
		done <- ac.followBuildAPI(context.Background(), "shop", "shop-build-1", 30*time.Millisecond)
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "build 2") {
			t.Errorf("followBuildAPI = %v, want an error naming build 2", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the wait did not end")
	}
}

func TestARerunThatNeverComesIsAnError(t *testing.T) {
	answers := append([]projectAnswer{twoBuilding}, repeat(twoHeld, 100)...)
	answers = append(answers, twoBuilding, projectAnswer{phase: "Failed", message: "The project failed.", build: "shop-build-2"})
	ac, _, streamed := refusedBuildClient(t, refusedList, answers...)
	err := refusedRun(ac)
	if err == nil {
		t.Fatal("want an error")
	}
	if len(streamed()) != 1 {
		t.Errorf("streams requested = %v", streamed())
	}
}

func TestARerunThatFailsNamesTheRerun(t *testing.T) {
	list := map[string]buildListEntry{
		"shop-build-2": {2, "Failed", buildTwoFailure},
		"shop-build-3": {3, "Failed", "The rerun failed too."},
	}
	answers := append([]projectAnswer{twoBuilding}, repeat(twoHeld, 100)...)
	answers = append(answers, threeBuilding, threeBuilding, projectAnswer{phase: "Failed", message: "The project failed.", build: "shop-build-3"})
	ac, _, _ := refusedBuildClient(t, list, answers...)
	err := refusedRun(ac)
	if err == nil || !strings.Contains(err.Error(), "build 3") {
		t.Errorf("waits = %v, want an error naming build 3", err)
	}
}
