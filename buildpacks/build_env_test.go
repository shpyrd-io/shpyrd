package buildpacks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The build-env buildpack's build script (shpyrd #54), run on a layout like
// the one kpack gives it: the project's variables, from a binding of type
// shpyrd-env, become build-time variables of the buildpacks after it, in a
// layer for the build only; build.env (the platform's environment) keeps
// its value; other bindings and the binding's own files are left alone.
func TestBuildEnvBuildpack(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
	dir := t.TempDir()
	layers, platform := filepath.Join(dir, "layers"), filepath.Join(dir, "platform")
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(platform, "env", "NODE_ENV"), "production")
	vars := filepath.Join(platform, "bindings", "web-build-env")
	write(filepath.Join(vars, "type"), "shpyrd-env\n")
	write(filepath.Join(vars, "DATABASE_URL"), "postgres://app:pw@db/app")
	write(filepath.Join(vars, "MULTI"), "line one\nline two\n")
	write(filepath.Join(vars, "NODE_ENV"), "development") // build.env wins
	write(filepath.Join(vars, "provider"), "shpyrd")
	other := filepath.Join(platform, "bindings", "npmrc")
	write(filepath.Join(other, "type"), "npmrc")
	write(filepath.Join(other, ".npmrc"), "//registry/:_authToken=x")
	write(filepath.Join(other, "TOKEN"), "not ours")

	cmd := exec.Command("bash", "build-env/bin/build", layers, platform, filepath.Join(dir, "plan.toml"))
	cmd.Env = append(os.Environ(), "CNB_LAYERS_DIR="+layers, "CNB_PLATFORM_DIR="+platform, "SERVICE_BINDING_ROOT="+filepath.Join(platform, "bindings"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "sees 2 variable(s)") {
		t.Errorf("output = %s", out)
	}
	env := filepath.Join(layers, "build-env", "env.build")
	entries, _ := os.ReadDir(env)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, " ") != "DATABASE_URL.override MULTI.override" {
		t.Errorf("build variables = %v", names)
	}
	if b, _ := os.ReadFile(filepath.Join(env, "MULTI.override")); string(b) != "line one\nline two\n" {
		t.Errorf("a value is copied as it is: %q", b)
	}
	meta, err := os.ReadFile(filepath.Join(layers, "build-env.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"build = true", "launch = false", "cache = false"} {
		if !strings.Contains(string(meta), want) {
			t.Errorf("layer metadata lacks %q: %s", want, meta)
		}
	}

	// No binding of its type: nothing, and no layer.
	empty := t.TempDir()
	cmd = exec.Command("bash", "build-env/bin/build", filepath.Join(empty, "layers"), filepath.Join(empty, "platform"), filepath.Join(empty, "plan.toml"))
	cmd.Env = append(os.Environ(), "CNB_LAYERS_DIR="+filepath.Join(empty, "layers"), "CNB_PLATFORM_DIR="+filepath.Join(empty, "platform"), "SERVICE_BINDING_ROOT="+filepath.Join(empty, "platform", "bindings"))
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "sees 0 variable(s)") {
		t.Errorf("without a binding: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(empty, "layers", "build-env.toml")); !os.IsNotExist(err) {
		t.Error("a layer was written without variables")
	}
}
