package cli

import (
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
)

// sourceRuntime predicts the launch runtime before uploading local source.
// The controller also reads the built image for Git and non-CLI deploys.
func sourceRuntime(dir string, build *shpyrdv1.Build) string {
	if build != nil {
		for _, e := range build.Env {
			if e.Name == "BP_WEB_SERVER" && e.Value != "" {
				return "static"
			}
		}
		for _, b := range build.Buildpacks {
			if b == "web-servers" || b == "shpyrd-web-servers" {
				return "static"
			}
		}
	}
	for _, candidate := range []struct {
		runtime string
		files   []string
	}{
		{"Go", []string{"go.mod"}}, {"Rust", []string{"Cargo.toml"}},
		{"Ruby", []string{"Gemfile", "config.ru"}},
		{"Python", []string{"requirements.txt", "pyproject.toml", "Pipfile", "setup.py"}},
		{"JVM", []string{"pom.xml", "build.gradle", "build.gradle.kts"}},
		{"Node.js", []string{"package.json"}},
		{"static", []string{"index.html", "public/index.html"}},
	} {
		for _, f := range candidate.files {
			if fileExists(dir, f) {
				return candidate.runtime
			}
		}
	}
	return ""
}

// prepareRuntimeSize operates on the merged deploy request: a size already
// chosen in shpyrd.yaml or on the server always wins over inference.
func prepareRuntimeSize(req *api.DeployRequest, current api.AppDetailSpec, runtime string, catalog sizes.Catalog, out io.Writer) error {
	if runtime == "" {
		return nil
	}
	procs := req.Processes
	if procs == nil {
		procs = current.Processes
	}
	copy := make(map[string]shpyrdv1.Process, len(procs)+1)
	for name, p := range procs {
		copy[name] = *p.DeepCopy()
	}
	if len(copy) == 0 {
		copy["web"] = shpyrdv1.Process{}
	}
	changed := false
	for name, p := range copy {
		if p.Size != "" || len(p.Resources.Limits) > 0 || len(p.Resources.Requests) > 0 {
			continue
		}
		// A release command without its own allocation inherits web below on
		// the server; choosing it here would override an explicit web size.
		if name == "release" {
			continue
		}
		p.Size = catalog.DefaultForRuntime(runtime)
		copy[name] = p
		changed = true
		size, _ := catalog.Get(p.Size)
		fmt.Fprintf(out, "==> Choosing %s (%s) for %s because the detected runtime is %s; no size was configured.\n", p.Size, size.Memory, name, runtime)
	}
	if changed {
		req.Processes = copy
	}
	return warnRuntimeMemory(copy, runtime, catalog, out)
}

func warnRuntimeMemory(procs map[string]shpyrdv1.Process, runtime string, catalog sizes.Catalog, out io.Writer) error {
	if sizes.RuntimeNeedsMemory(runtime) {
		web := procs["web"]
		if web.Size == "" {
			web.Size = catalog.DefaultForRuntime(runtime)
		}
		res, _, err := catalog.Resolve(web.Size, web.Resources)
		if err != nil {
			return err
		}
		minimum := resource.MustParse("256Mi")
		if memory := res.Limits[corev1.ResourceMemory]; memory.Cmp(minimum) < 0 {
			fmt.Fprintf(out, "Warning: %s web has only %s of memory, below 256Mi. The app or its release migration may run out of memory. Give web or processes.release a larger size, or check the migration.\n", runtime, memory.String())
		}
	}
	return nil
}
