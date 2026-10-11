package api

import (
	"testing"
)

// Dex finds its OAuth2Client by the object name its storage derives from
// the id; the same derivation authlocal uses for passwords.
func TestDexObjectName(t *testing.T) {
	if got, want := dexObjectName("shpyrd"), "onuha6lsmtf7fhheqqrcgji"; got != want {
		t.Errorf("dexObjectName(shpyrd) = %s, want %s (Dex's idToName)", got, want)
	}
	if dexObjectName("shpyrd") == "shpyrd" {
		t.Error("the object name must not be the id itself")
	}
}
