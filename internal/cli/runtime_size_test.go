package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
)

func TestSourceRuntime(t *testing.T) {
	for _, tc := range []struct{ file, runtime string }{
		{"package.json", "Node.js"}, {"Gemfile", "Ruby"}, {"pyproject.toml", "Python"}, {"build.gradle.kts", "JVM"},
		{"go.mod", "Go"}, {"Cargo.toml", "Rust"}, {"index.html", "static"},
	} {
		t.Run(tc.runtime, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, tc.file), []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			if got := sourceRuntime(dir, nil); got != tc.runtime {
				t.Fatalf("runtime = %q", got)
			}
			if tc.runtime == "Node.js" {
				build := &shpyrdv1.Build{Env: []corev1.EnvVar{{Name: "BP_WEB_SERVER", Value: "nginx"}}}
				if got := sourceRuntime(dir, build); got != "static" {
					t.Fatalf("static asset build = %q", got)
				}
			}
		})
	}
}

func TestPrepareRuntimeSize(t *testing.T) {
	for _, tc := range []struct {
		name, runtime string
		web           shpyrdv1.Process
		wantSize      string
		warn          bool
	}{
		{name: "Node default", runtime: "Node.js", wantSize: "shared-m"},
		{name: "Go default", runtime: "Go", wantSize: "shared-s"},
		{name: "explicit small", runtime: "Node.js", web: shpyrdv1.Process{Size: "shared-s"}, wantSize: "shared-s", warn: true},
		{name: "explicit large", runtime: "Ruby", web: shpyrdv1.Process{Size: "shared-l"}, wantSize: "shared-l"},
		{name: "custom memory", runtime: "Python", web: shpyrdv1.Process{Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")}}}, warn: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := api.AppDetailSpec{Processes: map[string]shpyrdv1.Process{"web": tc.web, "release": {Command: []string{"migrate"}}}}
			req := api.DeployRequest{}
			var out bytes.Buffer
			if err := prepareRuntimeSize(&req, current, tc.runtime, sizes.Defaults(), &out); err != nil {
				t.Fatal(err)
			}
			procs := req.Processes
			if procs == nil {
				procs = current.Processes
			}
			if procs["web"].Size != tc.wantSize {
				t.Fatalf("web size = %q", procs["web"].Size)
			}
			if procs["release"].Size != "" {
				t.Fatal("release must inherit web")
			}
			if strings.Contains(out.String(), "Warning:") != tc.warn {
				t.Fatalf("output = %s", out.String())
			}
			if tc.web.Size == "" && len(tc.web.Resources.Limits) == 0 && !strings.Contains(out.String(), "because the detected runtime is "+tc.runtime) {
				t.Fatalf("no explanation: %s", out.String())
			}
			if current.Processes["web"].Size != tc.web.Size {
				t.Fatal("changed existing config")
			}
		})
	}
}
