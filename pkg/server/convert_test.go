package server

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// A server that is not there is asked again until the patience runs out,
// then the error is the connection's.
func TestConnectWithPatienceGivesUpAfterTheDeadline(t *testing.T) {
	previous, previousRetry := connectPatience, connectRetry
	connectPatience, connectRetry = 300*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { connectPatience, connectRetry = previous, previousRetry })
	start := time.Now()
	_, err := connectWithPatience(context.Background(), "host=127.0.0.1 port=1 user=x dbname=x connect_timeout=1", slog.New(slog.NewTextHandler(discard{}, nil)), "target")
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("err = %v", err)
	}
	if took := time.Since(start); took < 250*time.Millisecond || took > 5*time.Second {
		t.Errorf("took %v", took)
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
