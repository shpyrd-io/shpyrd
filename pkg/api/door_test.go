package api

import (
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/ext/authlocal"
)

// Dex finds its OAuth2Client by the object name its storage derives from
// the id; the same derivation authlocal uses for passwords.
func TestDexObjectName(t *testing.T) {
	if got, want := dexObjectName("shpyrd"), authlocal.PasswordName("shpyrd"); got != want {
		t.Errorf("dexObjectName(shpyrd) = %s, want %s (Dex's idToName)", got, want)
	}
	if dexObjectName("shpyrd") == "shpyrd" {
		t.Error("the object name must not be the id itself")
	}
}
