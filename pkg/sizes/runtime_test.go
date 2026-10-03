package sizes

import "testing"

func TestRuntimeDefaults(t *testing.T) {
	for _, tc := range []struct{ id, runtime, size string }{
		{"node-engine", "Node.js", "shared-m"}, {"mri", "Ruby", "shared-m"},
		{"cpython", "Python", "shared-m"}, {"bellsoft-liberica", "JVM", "shared-m"},
		{"go-build", "Go", "shared-s"}, {"rust", "Rust", "shared-s"}, {"nginx", "static", "shared-s"},
		{"unknown", "", "shared-s"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			runtime := RuntimeFromBuildpacks([]string{"paketo-buildpacks/" + tc.id})
			if runtime != tc.runtime {
				t.Fatalf("runtime = %q", runtime)
			}
			if got := Defaults().DefaultForRuntime(runtime); got != tc.size {
				t.Fatalf("size = %q", got)
			}
		})
	}
	if got := RuntimeFromBuildpacks([]string{"paketo-buildpacks/node-engine", "paketo-buildpacks/nginx"}); got != "static" {
		t.Fatalf("asset build runtime = %q", got)
	}
	cat := Defaults()
	cat.Default = "shared-xl"
	if got := cat.DefaultForRuntime("Node.js"); got != "shared-xl" {
		t.Fatalf("larger operator default shrank to %q", got)
	}
	cat = Catalog{Default: "custom", Sizes: []Size{{Name: "custom", Kind: Shared, CPU: "1", Memory: "128Mi"}}}
	if got := cat.DefaultForRuntime("Node.js"); got != "custom" {
		t.Fatalf("custom catalog selected nonexistent size %q", got)
	}
}
