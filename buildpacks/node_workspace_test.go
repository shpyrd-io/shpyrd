package buildpacks

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The node-workspace buildpack (shpyrd #145), run as kpack would: in the
// workspace's root, with the package's path in BP_NODE_WORKSPACE and Node
// on PATH.

func needNode(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"bash", "node", "npm"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is not installed")
		}
	}
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func run(t *testing.T, dir string, env []string, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// buildIn runs bin/build in app with BP_NODE_WORKSPACE=pkg and returns its
// output, error and layers directory.
func buildIn(t *testing.T, app, pkg string, env ...string) (string, error, string) {
	t.Helper()
	bp, _ := filepath.Abs("node-workspace")
	layers := t.TempDir()
	env = append(env, "BP_NODE_WORKSPACE="+pkg, "CNB_LAYERS_DIR="+layers, "CNB_BUILDPACK_DIR="+bp, "CNB_PLATFORM_DIR="+t.TempDir())
	out, err := run(t, app, env, "bash", filepath.Join(bp, "bin", "build"), layers, "", "")
	return out, err, layers
}

// greetingWorkspace is a workspace whose web package needs greeting, a
// workspace package, as a dev dependency, at build time only.
// withRootTool makes the build need tool too, a dev dependency of the
// workspace's root, as turbo is in a Turborepo.
func greetingWorkspace(root map[string]string) map[string]string {
	files := map[string]string{
		"packages/greeting/package.json": `{"name":"greeting","version":"1.0.0","main":"index.js"}`,
		"packages/greeting/index.js":     `module.exports = "hello from greeting";`,
		"packages/lib/package.json":      `{"name":"lib","version":"1.0.0","main":"index.js"}`,
		"packages/lib/index.js":          `module.exports = "lib";`,
		"apps/web/package.json": `{"name":"web","version":"1.0.0","private":true,
			"dependencies":{"lib":"1.0.0","webprod":"file:../../vendor/webprod.tgz"},
			"devDependencies":{"greeting":"1.0.0","webdev":"file:../../vendor/webdev.tgz"},
			"scripts":{"build":"node -e \"require('fs').writeFileSync('built.txt', require('greeting'))\"","start":"node server.js"}}`,
		"apps/web/server.js": `console.log("up")`,
		"vendor/webprod.tgz": packageTarball("webprod"),
		"vendor/webdev.tgz":  packageTarball("webdev"),
	}
	for k, v := range root {
		files[k] = v
	}
	return files
}

// resolves says whether node, run in dir, finds module.
func resolves(t *testing.T, dir, module string) bool {
	t.Helper()
	_, err := run(t, dir, nil, "node", "-e", "require.resolve('"+module+"')")
	return err == nil
}

// packageTarball is a package as a registry serves it, for file:
// dependencies: workspace packages are always linked, whatever kind of
// dependency they are, so pruning shows only on packages from outside.
func packageTarball(name string) string {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for file, body := range map[string]string{
		"package/package.json": `{"name":"` + name + `","version":"1.0.0","main":"index.js"}`,
		"package/index.js":     `module.exports = "` + name + `";`,
	} {
		_ = tw.WriteHeader(&tar.Header{Name: file, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = zw.Close()
	return buf.String()
}

// pruned checks the image keeps the package's dependencies and drops the
// dev ones, the root's included.
func pruned(t *testing.T, app string) {
	t.Helper()
	web := filepath.Join(app, "apps/web")
	if !resolves(t, web, "webprod") {
		t.Error("a production dependency was pruned")
	}
	for _, dev := range []string{"webdev", "rootdev"} {
		if resolves(t, web, dev) {
			t.Errorf("dev dependency %s is still there", dev)
		}
	}
}

func withRootTool(files map[string]string, version string) map[string]string {
	files["packages/tool/package.json"] = `{"name":"tool","version":"1.0.0","main":"index.js"}`
	files["packages/tool/index.js"] = `module.exports = "tool";`
	files["vendor/rootdev.tgz"] = packageTarball("rootdev")
	files["package.json"] = strings.Replace(files["package.json"], `"private":true`, `"private":true,"devDependencies":{"tool":"`+version+`","rootdev":"file:vendor/rootdev.tgz"}`, 1)
	files["apps/web/package.json"] = strings.Replace(files["apps/web/package.json"], `\"require('fs')`, `\"require('tool'); require('fs')`, 1)
	return files
}

func TestNodeWorkspaceNpm(t *testing.T) {
	needNode(t)
	app := t.TempDir()
	writeFiles(t, app, withRootTool(greetingWorkspace(map[string]string{
		"package.json": `{"name":"root","private":true,"workspaces":["apps/*","packages/*"]}`,
	}), "1.0.0"))
	if out, err := run(t, app, nil, "npm", "install", "--package-lock-only", "--no-audit", "--no-fund"); err != nil {
		t.Fatalf("lockfile: %v %s", err, out)
	}
	// NODE_ENV=production, as the Next.js profile sets it: the dev
	// dependency the build needs is installed all the same.
	out, err, layers := buildIn(t, app, "apps/web", "NODE_ENV=production")
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(app, "apps/web/built.txt")); string(b) != "hello from greeting" {
		t.Errorf("the package's build did not run in place: %q\n%s", b, out)
	}
	pruned(t, app)
	launch, _ := os.ReadFile(filepath.Join(layers, "launch.toml"))
	for _, want := range []string{`type = "web"`, `"npm"`, `"start"`, `"apps/web"`, "default = true"} {
		if !strings.Contains(string(launch), want) {
			t.Errorf("launch.toml lacks %s:\n%s", want, launch)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(layers, "launch-env", "env.launch", "NODE_ENV.default")); string(b) != "production" {
		t.Errorf("NODE_ENV at launch = %q", b)
	}
}

func TestNodeWorkspaceReadsThePath(t *testing.T) {
	needNode(t)
	app := t.TempDir()
	writeFiles(t, app, greetingWorkspace(map[string]string{
		"package.json":                 `{"name":"root","private":true,"workspaces":["apps/*","packages/*","!packages/legacy"]}`,
		"package-lock.json":            `{}`,
		"packages/legacy/package.json": `{"name":"legacy"}`,
	}))
	bp, _ := filepath.Abs("node-workspace/lib/workspace.js")
	out, err := run(t, app, nil, "node", bp, "./apps/web/")
	if err != nil || !strings.Contains(out, "path='apps/web'") || !strings.Contains(out, "name='web'") || !strings.Contains(out, "manager='npm'") {
		t.Errorf("a loosely written path: %v\n%s", err, out)
	}
	// Not a package of the workspace: the ones there are, by name.
	out, err = run(t, app, nil, "node", bp, "packages/legacy")
	if err == nil || !strings.Contains(out, "packages/legacy is not a package of this workspace") || !strings.Contains(out, "apps/web") || strings.Contains(out, "packages/legacy, ") {
		t.Errorf("an excluded package: %v\n%s", err, out)
	}
	out, err = run(t, app, nil, "node", bp, "../elsewhere")
	if err == nil || !strings.Contains(out, "inside the repository") {
		t.Errorf("a path out of the source: %v\n%s", err, out)
	}
}

func TestNodeWorkspaceFailures(t *testing.T) {
	needNode(t)
	// No lockfile.
	app := t.TempDir()
	writeFiles(t, app, greetingWorkspace(map[string]string{
		"package.json": `{"name":"root","private":true,"workspaces":["apps/*","packages/*"]}`,
	}))
	if out, err, _ := buildIn(t, app, "apps/web"); err == nil || !strings.Contains(out, "no lockfile at the workspace's root") {
		t.Errorf("without a lockfile: %v\n%s", err, out)
	}
	// No start script and no Procfile.
	app = t.TempDir()
	files := greetingWorkspace(map[string]string{
		"package.json": `{"name":"root","private":true,"workspaces":["apps/*","packages/*"]}`,
	})
	files["apps/web/package.json"] = `{"name":"web","version":"1.0.0","private":true}`
	writeFiles(t, app, files)
	if out, err := run(t, app, nil, "npm", "install", "--package-lock-only", "--no-audit", "--no-fund"); err != nil {
		t.Fatalf("lockfile: %v %s", err, out)
	}
	if out, err, _ := buildIn(t, app, "apps/web"); err == nil || !strings.Contains(out, "add a start script to apps/web/package.json, or a Procfile") {
		t.Errorf("without a start script: %v\n%s", err, out)
	}
	// With a Procfile at the root, no start script is needed.
	writeFiles(t, app, map[string]string{"Procfile": "web: node apps/web/server.js\n"})
	if out, err, _ := buildIn(t, app, "apps/web"); err != nil {
		t.Errorf("with a Procfile: %v\n%s", err, out)
	}
}

func TestNodeWorkspaceDetect(t *testing.T) {
	needNode(t)
	bp, _ := filepath.Abs("node-workspace/bin/detect")
	app := t.TempDir()
	plan := filepath.Join(t.TempDir(), "plan.toml")
	if _, err := run(t, app, []string{"CNB_BUILD_PLAN_PATH=" + plan}, "bash", bp, "", plan); err == nil {
		t.Error("detected without BP_NODE_WORKSPACE")
	}
	if out, err := run(t, app, []string{"CNB_BUILD_PLAN_PATH=" + plan, "BP_NODE_WORKSPACE=apps/web"}, "bash", bp, "", plan); err != nil {
		t.Fatalf("detect: %v %s", err, out)
	}
	if b, _ := os.ReadFile(plan); !strings.Contains(string(b), `name = "node"`) || !strings.Contains(string(b), "launch = true") {
		t.Errorf("plan = %s", b)
	}
}

// pnpm and Yarn come through corepack, downloaded: the network, and time.
// The lockfiles are written with corepack too (npm's "yarn" package is
// Yarn 1 only).
func TestNodeWorkspaceOtherManagers(t *testing.T) {
	needNode(t)
	if testing.Short() {
		t.Skip("downloads pnpm and Yarn")
	}
	for _, tc := range []struct {
		name, pm, lock string
		rootTool       bool
	}{
		{"pnpm", "pnpm@9.15.0", "pnpm", true},
		{"yarn 4", "yarn@4.5.3", "yarn", true},
		{"yarn 4 pnp", "yarn@4.5.3", "yarn", false},
		{"yarn 1", "yarn@1.22.22", "yarn", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := t.TempDir()
			root := map[string]string{
				"package.json": `{"name":"root","private":true,"packageManager":"` + tc.pm + `","workspaces":["apps/*","packages/*"]}`,
			}
			if tc.lock == "pnpm" {
				root["pnpm-workspace.yaml"] = "packages:\n  - 'apps/*'\n  - packages/*\n"
			}
			files := greetingWorkspace(root)
			if tc.lock == "pnpm" {
				files["apps/web/package.json"] = strings.Replace(files["apps/web/package.json"], `"greeting":"1.0.0"`, `"greeting":"workspace:*"`, 1)
				files["apps/web/package.json"] = strings.Replace(files["apps/web/package.json"], `"lib":"1.0.0"`, `"lib":"workspace:*"`, 1)
			}
			if tc.rootTool {
				version := "1.0.0"
				if tc.lock == "pnpm" {
					version = "workspace:*"
				}
				files = withRootTool(files, version)
				// Under Plug'n'Play a package reads only what it declares:
				// the root's tools are reached through node_modules.
				if tc.pm == "yarn@4.5.3" {
					files[".yarnrc.yml"] = "nodeLinker: node-modules\n"
				}
			}
			writeFiles(t, app, files)
			lockArgs := map[string][]string{
				"pnpm@9.15.0":  {"install", "--lockfile-only"},
				"yarn@4.5.3":   {"install", "--mode=update-lockfile"},
				"yarn@1.22.22": {"install", "--ignore-scripts"},
			}[tc.pm]
			if out, err := run(t, app, []string{"COREPACK_ENABLE_DOWNLOAD_PROMPT=0", "YARN_ENABLE_IMMUTABLE_INSTALLS=false"}, "npx", append([]string{"-y", "corepack", tc.pm}, lockArgs...)...); err != nil {
				t.Fatalf("lockfile: %v\n%s", err, out)
			}
			_ = os.RemoveAll(filepath.Join(app, "node_modules"))
			out, err, layers := buildIn(t, app, "apps/web", "NODE_ENV=production")
			if err != nil {
				t.Fatalf("build: %v\n%s", err, out)
			}
			if b, _ := os.ReadFile(filepath.Join(app, "apps/web/built.txt")); string(b) != "hello from greeting" {
				t.Errorf("build output = %q\n%s", b, out)
			}
			// Under Plug'n'Play nothing resolves without Yarn's loader.
			if tc.rootTool {
				pruned(t, app)
			}
			launch, _ := os.ReadFile(filepath.Join(layers, "launch.toml"))
			if !strings.Contains(string(launch), `type = "web"`) || !strings.Contains(string(launch), `"start"`) {
				t.Errorf("launch.toml:\n%s", launch)
			}
		})
	}
}

// The Node version the workspace asks for, in engines.node (the package's
// own first, then the root's), goes to node-engine, as Paketo's npm-install
// would pass it.
func TestNodeWorkspaceDetectAsksForTheEnginesNode(t *testing.T) {
	needNode(t)
	bp, _ := filepath.Abs("node-workspace/bin/detect")
	app := t.TempDir()
	writeFiles(t, app, map[string]string{
		"package.json":          "{\n  \"name\": \"root\",\n  \"engines\": {\n    \"node\": \">=24 <25\"\n  }\n}\n",
		"apps/web/package.json": `{"name":"web"}`,
	})
	plan := filepath.Join(t.TempDir(), "plan.toml")
	env := []string{"CNB_BUILD_PLAN_PATH=" + plan, "BP_NODE_WORKSPACE=apps/web"}
	if out, err := run(t, app, env, "bash", bp, "", plan); err != nil {
		t.Fatalf("detect: %v %s", err, out)
	}
	b, _ := os.ReadFile(plan)
	if !strings.Contains(string(b), `version = ">=24 <25"`) || !strings.Contains(string(b), `version-source = "package.json"`) {
		t.Errorf("plan = %s", b)
	}
	writeFiles(t, app, map[string]string{"apps/web/package.json": `{"name":"web","engines":{"node":"22.x"}}`})
	if out, err := run(t, app, env, "bash", bp, "", plan); err != nil {
		t.Fatalf("detect: %v %s", err, out)
	}
	if b, _ := os.ReadFile(plan); !strings.Contains(string(b), `version = "22.x"`) {
		t.Errorf("the package's own engines: %s", b)
	}
}

// A Procfile in the package's own folder works as one at the root does:
// the procfile buildpack after this one reads it there.
func TestNodeWorkspacePackageProcfile(t *testing.T) {
	needNode(t)
	app := t.TempDir()
	files := greetingWorkspace(map[string]string{
		"package.json": `{"name":"root","private":true,"workspaces":["apps/*","packages/*"]}`,
	})
	files["apps/web/package.json"] = `{"name":"web","version":"1.0.0","private":true}`
	files["apps/web/Procfile"] = "web: node apps/web/server.js\n"
	writeFiles(t, app, files)
	if out, err := run(t, app, nil, "npm", "install", "--package-lock-only", "--no-audit", "--no-fund"); err != nil {
		t.Fatalf("lockfile: %v %s", err, out)
	}
	if out, err, _ := buildIn(t, app, "apps/web"); err != nil {
		t.Fatalf("with the package's Procfile: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(app, "Procfile")); string(b) != "web: node apps/web/server.js\n" {
		t.Errorf("root Procfile = %q", b)
	}
}

// A path with a quote in it still makes a launch.toml that says it.
func TestNodeWorkspaceQuotesTheStartCommand(t *testing.T) {
	needNode(t)
	app := t.TempDir()
	writeFiles(t, app, map[string]string{
		"package.json":            `{"name":"root","private":true,"workspaces":["apps/*"]}`,
		"apps/we\"b/package.json": `{"name":"web","version":"1.0.0","scripts":{"start":"node server.js"}}`,
	})
	if out, err := run(t, app, nil, "npm", "install", "--package-lock-only", "--no-audit", "--no-fund"); err != nil {
		t.Fatalf("lockfile: %v %s", err, out)
	}
	out, err, layers := buildIn(t, app, `apps/we"b`)
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	launch, _ := os.ReadFile(filepath.Join(layers, "launch.toml"))
	if !strings.Contains(string(launch), `"apps/we\"b"`) {
		t.Errorf("launch.toml:\n%s", launch)
	}
}

// BP_KEEP_DEV_DEPENDENCIES=true keeps them, as RFC-0079 names it.
func TestNodeWorkspaceKeepsDevDependenciesWhenAsked(t *testing.T) {
	needNode(t)
	app := t.TempDir()
	writeFiles(t, app, withRootTool(greetingWorkspace(map[string]string{
		"package.json": `{"name":"root","private":true,"workspaces":["apps/*","packages/*"]}`,
	}), "1.0.0"))
	if out, err := run(t, app, nil, "npm", "install", "--package-lock-only", "--no-audit", "--no-fund"); err != nil {
		t.Fatalf("lockfile: %v %s", err, out)
	}
	if out, err, _ := buildIn(t, app, "apps/web", "BP_KEEP_DEV_DEPENDENCIES=true"); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	if !resolves(t, filepath.Join(app, "apps/web"), "webdev") {
		t.Error("dev dependencies pruned though asked to keep them")
	}
}
