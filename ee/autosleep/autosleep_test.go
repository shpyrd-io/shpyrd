//go:build !foss

package autosleep

import (
	"testing"

	"github.com/shpyrd-io/shpyrd/ee/licensing"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

func TestThingsSleepByThemselvesOnlyWithALicense(t *testing.T) {
	t.Cleanup(licensing.ResetForTest)
	g := New().(ext.SleepGate)
	if g.SleepAllowed() {
		t.Fatal("asleep without a license")
	}
	licensing.Unlock()
	if !g.SleepAllowed() {
		t.Fatal("awake with the license")
	}
}
