package install

import (
	"reflect"
	"testing"

	"github.com/shpyrd-io/shpyrd/deploy"
)

// The cloud profiles retired storage-local: not installed any more, removed
// from a cluster that still records it; kind keeps it.
func TestCloudProfilesRetireStorageLocal(t *testing.T) {
	for _, name := range []string{"oci", "aws"} {
		p, err := LoadProfile(deploy.FS, name)
		if err != nil {
			t.Fatal(err)
		}
		if p.HasComponent("storage-local") || !contains(p.Retired, "storage-local") {
			t.Errorf("%s: storage-local must be retired, not installed: runlevels %v retired %v", name, p.Runlevels, p.Retired)
		}
	}
	local, err := LoadProfile(deploy.FS, "local")
	if err != nil {
		t.Fatal(err)
	}
	if !local.HasComponent("storage-local") || len(local.Retired) != 0 {
		t.Errorf("kind keeps storage-local: retired %v", local.Retired)
	}
}

func TestRetiredToRemove(t *testing.T) {
	recorded := []string{"cert-manager", "shpyrd", "storage-local"}
	if got := retiredToRemove([]string{"storage-local", "garage"}, recorded); !reflect.DeepEqual(got, []string{"storage-local"}) {
		t.Errorf("only what the record still lists is removed: %v", got)
	}
	if got := retiredToRemove([]string{"storage-local"}, []string{"shpyrd"}); got != nil {
		t.Errorf("a cluster that never had it: %v", got)
	}
}
