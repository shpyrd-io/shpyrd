package cli

import (
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

// status is one answer of GET /api/projects/shop while a deploy waits.
type status struct{ phase, message, digest string }

// waitWith runs waitRunningAPI against a server that answers the statuses
// in turn (the last one from then on), with the project failing as stale
// before the deploy.
func waitWith(t *testing.T, stale *staleFailure, answers ...status) error {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("SHPYRD_TOKEN", "test-token")
	defer func(prev bool) { preferKubeconfig = prev }(preferKubeconfig)
	defer func(p, g time.Duration) { waitPoll, staleGrace = p, g }(waitPoll, staleGrace)
	waitPoll = time.Millisecond
	var mu sync.Mutex
	reads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		a := answers[min(reads, len(answers)-1)]
		reads++
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"slug": "shop", "status": map[string]any{
			"phase": a.phase, "message": a.message, "digest": a.digest, "generation": 3, "observedGeneration": 3,
		}})
	}))
	defer srv.Close()
	t.Setenv("SHPYRD_URL", srv.URL)
	ac, err := newAppClient(&globalFlags{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ac.waitRunningAPI(context.Background(), "shop", 3, 5*time.Second, stale)
	return err
}

func TestADeployWaitsPastThePreviousReleasesFailure(t *testing.T) {
	old := &staleFailure{message: "the image runs as root", image: "sha256:old"}
	failed := status{"Failed", "the image runs as root", "sha256:old"}

	// The issue's case: right after the build the project still shows the
	// old failure; the new release then starts and runs.
	if err := waitWith(t, old, failed, failed, status{"Deploying", "Starting the instance", "sha256:old"}, status{"Running", "", "sha256:new"}); err != nil {
		t.Errorf("a deploy that succeeds after an old failure = %v", err)
	}
	// The new release fails for a reason of its own: that is the answer.
	if err := waitWith(t, old, failed, status{"Deploying", "", "sha256:old"}, status{"Failed", "the release step failed", "sha256:new"}); err == nil || !strings.Contains(err.Error(), "release step failed") {
		t.Errorf("a new failure = %v", err)
	}
	// The same message again after the project moved is the new release's.
	if err := waitWith(t, old, failed, status{"Deploying", "", "sha256:old"}, failed); err == nil || !strings.Contains(err.Error(), "runs as root") {
		t.Errorf("the same failure again after moving = %v", err)
	}
	// A failure on another image is new, whatever its message.
	if err := waitWith(t, old, status{"Failed", "the image runs as root", "sha256:new"}); err == nil {
		t.Error("a failure on the new image was taken for the old one")
	}
}

func TestADeployWithoutAnOldFailureReportsAFailureAtOnce(t *testing.T) {
	if err := waitWith(t, nil, status{"Failed", "the image runs as root", "sha256:old"}); err == nil || !strings.Contains(err.Error(), "runs as root") {
		t.Errorf("failure without an old one = %v", err)
	}
}

func TestAnOldFailureThatNeverMovesIsTheAnswerAfterAWhile(t *testing.T) {
	defer func(g time.Duration) { staleGrace = g }(staleGrace)
	staleGrace = 20 * time.Millisecond
	old := &staleFailure{message: "the image runs as root", image: "sha256:old"}
	start := time.Now()
	err := waitWith(t, old, status{"Failed", "the image runs as root", "sha256:old"})
	if err == nil || !strings.Contains(err.Error(), "runs as root") {
		t.Fatalf("an old failure that stays = %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("waited %s for an old failure that never moved", time.Since(start))
	}
}
