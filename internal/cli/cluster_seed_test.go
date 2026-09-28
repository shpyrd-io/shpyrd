package cli

import (
	"reflect"
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/install"
)

// A recorded --set is carried over, unless this run decides the value
// itself: an explicit --set, --registry-host, or the --vars-file the
// infrastructure wrote (RFC-0077: Terraform moves the autoscaled pool).
func TestSeedOverridesPrecedence(t *testing.T) {
	recorded := map[string]string{
		install.VarNodePoolID:   "ocid1.nodepool...old",
		install.VarNodeMinCount: "1",
		install.VarRegistryHost: "old.registry",
		"SHPYRD_ACME_EMAIL":     "ops@example.com",
	}
	fromFile := map[string]string{install.VarNodePoolID: "ocid1.nodepool...apps"}
	set := []string{install.VarNodeMinCount + "=0"}

	got := seedOverrides(set, recorded, fromFile, true)
	want := []string{
		install.VarNodeMinCount + "=0",      // explicit --set kept as given
		"SHPYRD_ACME_EMAIL=ops@example.com", // remembered
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("seedOverrides = %v, want %v", got, want)
	}

	// Without a vars file and without --registry-host the record wins.
	got = seedOverrides(nil, recorded, nil, false)
	if !hasSet(got, install.VarNodePoolID) || !hasSet(got, install.VarRegistryHost) {
		t.Fatalf("recorded overrides not carried over: %v", got)
	}
}
