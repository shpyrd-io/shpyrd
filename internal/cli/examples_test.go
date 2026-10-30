package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
)

// buildSpec is the build the CLI would send for a project config: what
// sourceRuntime reads (the web-servers buildpack or BP_WEB_SERVER make a
// Node build a static site).
func buildSpec(pc *projectConfig) *shpyrdv1.Build {
	if pc == nil || pc.Build == nil {
		return nil
	}
	b := &shpyrdv1.Build{Buildpacks: pc.Build.Buildpacks}
	for k, v := range pc.Build.Env {
		b.Env = append(b.Env, corev1.EnvVar{Name: k, Value: v})
	}
	return b
}

// examplesDir is the checkout of github.com/shpyrd-io/shpyrd-examples the
// test reads: $SHPYRD_EXAMPLES, else ../shpyrd-examples beside this
// repository, else ~/dev/shpyrd-examples. Skipped when none exists.
func examplesDir(t *testing.T) string {
	t.Helper()
	candidates := []string{os.Getenv("SHPYRD_EXAMPLES")}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "..", "..", "..", "shpyrd-examples"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "dev", "shpyrd-examples"))
	}
	for _, c := range candidates {
		if c != "" && fileExists(c, "README.md") {
			return c
		}
	}
	t.Skip("shpyrd-examples checkout not found (set SHPYRD_EXAMPLES)")
	return ""
}

// Every example deploys with `shpyrd deploy` and nothing else: what the
// CLI infers from each directory (RFC-0067) and the runtime it detects,
// against what the example's README says it relies on. The example's own
// shpyrd.yaml is not read here: the question is what a person gets who
// runs `shpyrd deploy` in a directory like it without one.
func TestExamplesDeployWithoutConfiguration(t *testing.T) {
	root := examplesDir(t)
	cases := map[string]struct {
		profiles string            // detected profiles, "" for none
		inferred map[string]string // values the CLI fills in
		runtime  string            // what sourceRuntime sees
		size     string            // the size the CLI would choose with no shpyrd.yaml
	}{
		"static-nginx": {"static site", map[string]string{"build.buildpacks": "[web-servers]", "BP_WEB_SERVER": "nginx", "BP_WEB_SERVER_ROOT": "public"}, "static", "shared-s"},
		"static-httpd": {"static site", map[string]string{"build.buildpacks": "[web-servers]", "BP_WEB_SERVER": "nginx", "BP_WEB_SERVER_ROOT": "public"}, "static", "shared-s"},
		// Node builds the assets, nginx serves them: the runtime that stays
		// in the image is the static server, so the small size is right.
		"react":    {"Vite single-page app", map[string]string{"build.buildpacks": "[web-servers]", "BP_NODE_RUN_SCRIPTS": "build", "BP_WEB_SERVER": "nginx", "BP_WEB_SERVER_ROOT": "dist", "BP_WEB_SERVER_ENABLE_PUSH_STATE": "true"}, "static", "shared-s"},
		"nextjs":   {"Next.js application", map[string]string{"BP_NODE_RUN_SCRIPTS": "build", "NODE_ENV": "production"}, "Node.js", "shared-m"},
		"node":     {"", map[string]string{}, "Node.js", "shared-m"},
		"go":       {"", map[string]string{}, "Go", "shared-s"},
		"revision": {"", map[string]string{}, "Go", "shared-s"},
		// PHP (nginx and PHP-FPM) and .NET do not fit the 64Mi default;
		// both examples ask for shared-m, and the CLI chooses it.
		"php":      {"PHP application", map[string]string{"BP_PHP_SERVER": "nginx", "BP_PHP_WEB_DIR": "public"}, "PHP", "shared-m"},
		"sinatra":  {"Rack application", map[string]string{"env.RACK_ENV": "production"}, "Ruby", "shared-m"},
		"rails":    {"Rails application", map[string]string{"env.RAILS_ENV": "production", "env.RAILS_LOG_TO_STDOUT": "1", "processes.web.healthCheck.path": "/up"}, "Ruby", "shared-m"},
		"ruby-apt": {"Rack application + system packages with deep dependencies", map[string]string{"env.RACK_ENV": "production", "build.stack": "full"}, "Ruby", "shared-m"},
		"python":   {"", map[string]string{}, "Python", "shared-m"},
		"java":     {"", map[string]string{}, "JVM", "shared-m"},
		"dotnet":   {"", map[string]string{}, ".NET", "shared-m"},
	}
	catalog := sizes.Defaults()
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(root, name)
			if !fileExists(dir, "README.md") {
				t.Skipf("%s not in the checkout", name)
			}
			pc, det := applyProfiles(nil, dir)
			if got := strings.Join(det.profiles, " + "); got != c.profiles {
				t.Errorf("profiles = %q, want %q", got, c.profiles)
			}
			got := inferredMap(det)
			if len(got) != len(c.inferred) {
				t.Errorf("inferred %v, want %v", got, c.inferred)
			}
			for k, v := range c.inferred {
				if got[k] != v {
					t.Errorf("%s = %q, want %q", k, got[k], v)
				}
			}
			runtime := sourceRuntime(dir, buildSpec(pc))
			if runtime != c.runtime {
				t.Errorf("runtime = %q, want %q", runtime, c.runtime)
			}
			size := catalog.Default
			if runtime != "" {
				size = catalog.DefaultForRuntime(runtime)
			}
			if size != c.size {
				t.Errorf("size with no shpyrd.yaml = %q, want %q", size, c.size)
			}
		})
	}
}

// The sizes the examples ask for in shpyrd.yaml against what the CLI would
// choose on its own: where the example asks for more, the default was not
// enough for a first deploy to run, and a person without the example's
// knowledge would see it crash or sleep forever. That gap is the cost of
// not knowing, and this test keeps the list of such examples explicit.
func TestExamplesSizesAboveTheDefault(t *testing.T) {
	root := examplesDir(t)
	catalog := sizes.Defaults()
	// Examples known to need more than the runtime default, and why.
	known := map[string]string{
		"java": "the JVM wants shared-l (512Mi) and a thread count for Paketo's memory calculator; the default for JVM is shared-m",
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() || !fileExists(filepath.Join(root, e.Name()), "shpyrd.yaml") {
			continue
		}
		dir := filepath.Join(root, e.Name())
		raw, err := os.ReadFile(filepath.Join(dir, "shpyrd.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var pc projectConfig
		if err := yaml.Unmarshal(raw, &pc); err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		web, ok := pc.Processes["web"]
		if !ok || web.Size == "" {
			continue
		}
		pcInferred, _ := applyProfiles(nil, dir)
		runtime := sourceRuntime(dir, buildSpec(pcInferred))
		def := catalog.Default
		if runtime != "" {
			def = catalog.DefaultForRuntime(runtime)
		}
		asked, ok := catalog.Get(web.Size)
		if !ok {
			// shared-xs exists on the cloud's catalog, not in the shipped
			// one: below the default either way.
			t.Logf("%s asks for %s, a size the shipped catalog lacks; skipped", e.Name(), web.Size)
			continue
		}
		chosen, _ := catalog.Get(def)
		chosenMem, askedMem := resource.MustParse(chosen.Memory), resource.MustParse(asked.Memory)
		if chosenMem.Cmp(askedMem) < 0 {
			if _, expected := known[e.Name()]; !expected {
				t.Errorf("%s asks for %s (%s) but the CLI would choose %s (%s) for runtime %q: an unknown gap", e.Name(), web.Size, asked.Memory, def, chosen.Memory, runtime)
			}
		} else if _, expected := known[e.Name()]; expected {
			t.Errorf("%s: the default %s now covers %s; drop it from the known list", e.Name(), def, web.Size)
		}
	}
}
