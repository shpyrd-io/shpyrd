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

func TestEngineLoadsRetiredComponentsWithoutSelectingThem(t *testing.T) {
	for _, profile := range []string{"oci", "aws"} {
		t.Run(profile, func(t *testing.T) {
			e, err := New(nil, Options{Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			c := e.components["storage-local"]
			if c == nil || c.Kustomize == nil {
				t.Fatal("retired component manifests must be available for removal")
			}
			for _, rl := range e.profile.Runlevels {
				for _, selected := range e.selected(rl) {
					if selected.Name == "storage-local" {
						t.Fatal("retired component must not be installed")
					}
				}
			}
			if _, err := e.renderComponent(c); err != nil {
				t.Fatal(err)
			}
		})
	}
}
