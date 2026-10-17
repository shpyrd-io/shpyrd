package gitauth

import (
	"net"
	"testing"
)

// It takes a test, nothing else that has a Cleanup.
var _ func(testing.TB) = AllowLoopbackForTests

func TestAllowLoopbackForTestsLastsUntilTheTestEnds(t *testing.T) {
	refuseLoopback(t)
	loopback := net.ParseIP("127.0.0.1")
	t.Run("allowed", func(t *testing.T) {
		AllowLoopbackForTests(t)
		if refusedIP(loopback) {
			t.Error("allowed within the test")
		}
	})
	if !refusedIP(loopback) {
		t.Error("refused again once the test ended")
	}
	if allowLoopbackForTests.Load() {
		t.Error("the flag is back to false")
	}
}
