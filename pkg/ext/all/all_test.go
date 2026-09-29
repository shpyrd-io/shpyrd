package all

import (
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// An install record written before an extension was retired must still
// resolve: the retired name is dropped, the others load, and only names
// nobody ever knew come back as unknown.
func TestEnabledDropsRetiredNamesWithoutCallingThemUnknown(t *testing.T) {
	enabled, unknown := Enabled("auth-local, auth-oidc ,postgres,made-up")
	if got := ext.Names(enabled); len(got) != 2 || got[0] != "auth-local" || got[1] != "postgres" {
		t.Fatalf("enabled = %v, want [auth-local postgres]", got)
	}
	if len(unknown) != 1 || unknown[0] != "made-up" {
		t.Fatalf("unknown = %v, want [made-up]", unknown)
	}
	for name := range Retired {
		if ext.Find(All(), name) != nil {
			t.Fatalf("%s is both retired and registered", name)
		}
	}
}
