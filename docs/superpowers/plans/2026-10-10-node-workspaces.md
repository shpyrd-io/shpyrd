# JavaScript workspace builds: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** an app inside an npm, pnpm or Yarn workspace deploys with buildpacks from its own folder (`cd apps/web && shpyrd deploy`), and a project's first deploy no longer fails while its own builder is being made (#145, #143).

**Architecture:**
- **The build:** a new shell buildpack, `buildpacks/node-workspace`, runs after Paketo's `node-engine` in a builder of the project's own. It installs and builds one package in place in `/workspace` and records its `start` script as `web`.
- **The setting:** `build.workspace` (the package's path from the workspace's root) travels from `shpyrd.yaml` or CLI detection, through the deploy API and the App spec, to the controller. The controller composes the builder and passes the path to the build as `BP_NODE_WORKSPACE`.
- **The source:** the CLI archives the workspace's root instead of the current folder.

**Tech Stack:**
- the controller, API and CLI in Go;
- the buildpack in Bash plus one dependency-free Node script (CNB buildpack API 0.9);
- kpack `v1alpha2` (Builder, ClusterBuildpack);
- GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-10-10-node-workspaces-design.md`

## Global Constraints

- **Package managers:** npm, pnpm and Yarn (1 and 2+), all now.
- **The buildpack:** id `shpyrd/node-workspace`, `api = "0.9"`, stacks `*`, version `0.1.0`.
- **Its image:** `ghcr.io/shpyrd-io/buildpack-node-workspace:0.1.0`, published by tag `buildpack-node-workspace/v0.1.0`.
- **The project builder's group, in order:**
  1. `shpyrd-build-env`, only when the platform has it, as today;
  2. `heroku-deb-packages`, when `systemPackages`, as today;
  3. `paketo-node-engine`;
  4. `shpyrd-node-workspace`;
  5. `paketo-procfile`, optional.
- **The build variable:** `BP_NODE_WORKSPACE=<build.workspace>`. A value the project writes in `build.env` wins.
- **The path's form:** `build.workspace` is relative and slash-separated, with no empty, `.` or `..` segments and no leading `/`.
- **Refusals:** the API refuses `build.workspace` together with `strategy: dockerfile`, and together with `build.buildpacks`. `build.stack` stays allowed.
- **The CLI's output on detection:**
  - `==> Detected <a|an> <manager> workspace at <where>; building <path> in it`
  - then `    build.workspace=<path> (shpyrd.yaml values win; --save writes it there)`
- **The #143 waiting message:** exactly `preparing the project's builder`.
- **Install:** always with dev dependencies, whatever `NODE_ENV` says.
- **Launch:** `NODE_ENV=production` by default at launch. A `Procfile` at the workspace's root replaces `web`.
- **Brand:** always lowercase `shpyrd` in copy and messages.

## Review Focus

1. **`NODE_ENV=production` during the build.** The Next.js build profile sets it, and so may `build.env`. The install must still bring dev dependencies, or every Next build breaks. Pinned in Task 4, `TestNodeWorkspaceNpm`, which builds with `NODE_ENV=production` and a dev-only workspace dependency.
2. **The path written loosely.** A `build.workspace` like `./apps/web/` should be read as `apps/web`, not refused as an unknown package. Pinned in Task 4, `TestNodeWorkspaceReadsThePath`.
3. **A Dockerfile project inside a workspace.** It must deploy exactly as before: the website today, with `strategy: dockerfile` written. Pinned in Task 5, `TestWorkspaceForLeavesDockerfileBuildsAlone`.
4. **Deploying from the workspace's root, or from a folder the workspace doesn't list.** Nothing is detected; the deploy is unchanged. Pinned in Task 5, `TestFindWorkspace`.
5. **A `build.workspace` written in `shpyrd.yaml` that matches neither the current folder nor a folder below it.** This must fail with a sentence, not upload the wrong tree. Pinned in Task 5, `TestWorkspaceForWrittenPath`.

## Decisions this plan takes beyond the spec

- **The lockfile check moves from detect to build.** The spec has detection pass only when there is a lockfile. Without one, the group would then fail with kpack's "no buildpack group passed detection", while the spec asks for the sentence "no lockfile at the workspace's root…". So detect passes on `BP_NODE_WORKSPACE` alone, and build fails with that sentence.
- **pnpm and Yarn select the package by its `name`**, not its path (`--filter <name>...`, `yarn workspace <name>`), so a package used with them must have a `name`. A missing name fails with a sentence.
- **Yarn 2 and 3 run `yarn install`.** Yarn 4 has `workspaces focus` built in; Yarn 2 and 3 need a plugin for it, so they install the whole workspace, as Yarn 1 does.
- **pnpm and Yarn come from `corepack enable --install-directory <layer>/bin`.** The plain `pnpm` and `yarn` commands are then on `PATH` for build scripts (`pnpm --filter … run build` inside `build`) and at launch. The layer is build, launch and cache.
- **When a project's own Builder isn't ready (#143):** the project waits up to two minutes from the Builder's creation, then fails with kpack's reason.
- **CLI detection skips `--path`, `--git` and `--image` deploys.** Those don't archive the current folder.

## Files

| File | What it does |
|---|---|
| `api/v1alpha1/app_types.go` | `Build.Workspace` |
| `deploy/components/shpyrd/base/crds/shpyrd.io_apps.yaml` | regenerated |
| `pkg/api/apps.go` | the refusals in `validateDeployRequest`; `validateWorkspacePath` |
| `pkg/api/workspace_validate_test.go` | new |
| `internal/controller/builder.go` | composing for a workspace; the group; the constants |
| `internal/controller/builder_test.go` | `TestWorkspaceBuilder` |
| `internal/controller/desired.go` | `BP_NODE_WORKSPACE` in the kpack Image |
| `internal/controller/desired_test.go` | `TestWorkspaceBuildVariable` |
| `internal/controller/status.go` | `buildState.Reason`, `BuilderReady`, `builderNotReady()` |
| `internal/controller/app_controller.go` | #143: wait for the project's Builder |
| `internal/controller/project_builder.go` | new: `projectBuilderFailure` |
| `internal/controller/project_builder_test.go` | new |
| `buildpacks/node-workspace/{buildpack.toml,bin/detect,bin/build,lib/workspace.js}` | new: the buildpack |
| `buildpacks/node_workspace_test.go` | new |
| `deploy/components/kpack/base/builder.yaml` | two ClusterBuildpacks |
| `.github/workflows/buildpacks.yml` | the package's description names the buildpack |
| `internal/cli/workspace.go` | new: detection and `workspaceFor` |
| `internal/cli/workspace_test.go` | new |
| `internal/cli/appclient.go` | `projectBuild.Workspace`, `applyTo` |
| `internal/cli/projectconfig_save.go` | `--save` writes `build.workspace` |
| `internal/cli/deploy.go` | wiring; `archiveSource` takes a folder |
| `examples/monorepo/**` | new: a pnpm workspace |
| `.github/workflows/ci.yml` | the end-to-end job deploys it |
| `content/docs/{deploying,shpyrd-yaml,cli}.md` | docs |

---

### Task 1: `build.workspace` in the API

**Files:**
- Modify: `api/v1alpha1/app_types.go` (the `Build` struct, after `SystemPackages`)
- Modify: `pkg/api/apps.go` (`validateDeployRequest`, about line 813)
- Generate: `deploy/components/shpyrd/base/crds/shpyrd.io_apps.yaml`, `api/v1alpha1/zz_generated.deepcopy.go`
- Test: `pkg/api/workspace_validate_test.go`

**Interfaces:**
- Produces: `shpyrdv1.Build.Workspace string` (json `workspace`), and `validateWorkspacePath(p string) error` in `pkg/api`.

- [ ] **Step 1: Write the failing test**

`pkg/api/workspace_validate_test.go`:

```go
package api

import (
	"strings"
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// build.workspace names a package inside the source; with a Dockerfile or
// a buildpack list it means nothing, and the deploy says so (#145).
func TestValidateDeployRequestWorkspace(t *testing.T) {
	ok := []string{"apps/web", "web", "packages/a/b"}
	for _, p := range ok {
		req := &DeployRequest{Build: &shpyrdv1.Build{Workspace: p}}
		if err := validateDeployRequest(req); err != nil {
			t.Errorf("%q refused: %v", p, err)
		}
	}
	bad := []string{"/apps/web", "../web", "apps/../web", "apps//web", "./apps/web", "apps/web/", `apps\web`}
	for _, p := range bad {
		req := &DeployRequest{Build: &shpyrdv1.Build{Workspace: p}}
		err := validateDeployRequest(req)
		if err == nil || !strings.Contains(err.Error(), "build.workspace") {
			t.Errorf("%q accepted or unclear: %v", p, err)
		}
	}
	docker := &DeployRequest{Build: &shpyrdv1.Build{Workspace: "apps/web", Strategy: shpyrdv1.StrategyDockerfile}}
	if err := validateDeployRequest(docker); err == nil || !strings.Contains(err.Error(), "dockerfile") {
		t.Errorf("with a Dockerfile: %v", err)
	}
	listed := &DeployRequest{Build: &shpyrdv1.Build{Workspace: "apps/web", Buildpacks: []string{"node"}}}
	if err := validateDeployRequest(listed); err == nil || !strings.Contains(err.Error(), "build.buildpacks") {
		t.Errorf("with a buildpack list: %v", err)
	}
	full := &DeployRequest{Build: &shpyrdv1.Build{Workspace: "apps/web", Stack: "full"}}
	if err := validateDeployRequest(full); err != nil {
		t.Errorf("with the full stack: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to see it fail**

Run: `go test ./pkg/api -run TestValidateDeployRequestWorkspace`
Expected: a compile error, `unknown field Workspace in struct literal`.

- [ ] **Step 3: Add the field**

In `api/v1alpha1/app_types.go`, after the `SystemPackages` field of `Build`:

```go
	// Workspace is the path, from the source's root, of the package to
	// build inside a JavaScript workspace (npm, pnpm or Yarn): "apps/web"
	// (shpyrd #145). The project gets a builder of its own that installs
	// and builds that package in place. Buildpacks only.
	// +optional
	Workspace string `json:"workspace,omitempty"`
```

- [ ] **Step 4: Add the refusals**

In `pkg/api/apps.go`, inside `validateDeployRequest`'s `if req.Build != nil {` block, after the `for _, name := range req.Build.Buildpacks` loop:

```go
		if w := req.Build.Workspace; w != "" {
			if err := validateWorkspacePath(w); err != nil {
				return err
			}
			if req.Build.Strategy == shpyrdv1.StrategyDockerfile || req.Strategy == shpyrdv1.StrategyDockerfile {
				return errors.New("build.workspace builds with buildpacks; it cannot be used with strategy: dockerfile")
			}
			if len(req.Build.Buildpacks) > 0 {
				return errors.New("build.workspace chooses its own buildpacks; remove build.buildpacks")
			}
		}
```

And after the function:

```go
// validateWorkspacePath accepts a package's path inside the source, as
// the CLI writes it: relative, slash-separated, every segment a name.
func validateWorkspacePath(p string) error {
	bad := strings.HasPrefix(p, "/") || strings.Contains(p, `\`)
	for _, s := range strings.Split(p, "/") {
		if s == "" || s == "." || s == ".." {
			bad = true
		}
	}
	if bad {
		return fmt.Errorf("build.workspace must be a package's path inside the source, like apps/web; got %q", p)
	}
	return nil
}
```

If `errors` isn't yet imported in `apps.go`, add it.

- [ ] **Step 5: Regenerate, and run the test**

Run: `make generate && go test ./pkg/api -run TestValidateDeployRequestWorkspace && git diff --stat deploy/components/shpyrd/base/crds`
Expected: `ok`; the CRD gains `workspace` under `build`.

- [ ] **Step 6: Commit**

```bash
git add api/v1alpha1 deploy/components/shpyrd/base/crds pkg/api/apps.go pkg/api/workspace_validate_test.go
git commit -m "feat(api): build.workspace, the package of a JavaScript workspace to build (#145)"
```

---

### Task 2: The controller composes a workspace's builder

**Files:**
- Modify: `internal/controller/builder.go` (`composesBuild`, `desiredBuilder`)
- Modify: `internal/controller/desired.go` (`desiredKpackImage`, the `build` env block about line 608)
- Test: `internal/controller/builder_test.go`, `internal/controller/desired_test.go`

**Interfaces:**
- Consumes: `shpyrdv1.Build.Workspace` (Task 1).
- Produces:
  - the constants `NodeEngineBuildpack = "paketo-node-engine"`, `NodeWorkspaceBuildpack = "shpyrd-node-workspace"` and `NodeWorkspaceEnv = "BP_NODE_WORKSPACE"`;
  - `composesBuild(app)` is true for a workspace, which Task 3 relies on.

- [ ] **Step 1: Write the failing builder test**

Append to `internal/controller/builder_test.go`:

```go
// A package of a JavaScript workspace (#145) gets a builder of its own:
// Node from node-engine, then the workspace buildpack, then a Procfile if
// there is one; with a buildpack list as well, it is refused.
func TestWorkspaceBuilder(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "site", Namespace: "app-site"},
		Spec:       shpyrdv1.AppSpec{Build: &shpyrdv1.Build{Workspace: "apps/website"}},
	}
	r, c := newTestReconciler(t, app)
	r.Config.RegistryHost = "10.96.0.50:5000"
	ref, err := r.reconcileBuilder(ctx, app)
	if err != nil || ref["kind"] != "Builder" || ref["name"] != "site-builder" {
		t.Fatalf("ref = %v %v", ref, err)
	}
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(BuilderGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-site", Name: "site-builder"}, b); err != nil {
		t.Fatal(err)
	}
	order, _, _ := unstructured.NestedSlice(b.Object, "spec", "order")
	if len(order) != 1 {
		t.Fatalf("groups = %d", len(order))
	}
	var names []string
	for _, e := range order[0].(map[string]interface{})["group"].([]interface{}) {
		m := e.(map[string]interface{})
		n := m["name"].(string)
		if m["optional"] == true {
			n += "?"
		}
		names = append(names, n)
	}
	if got := strings.Join(names, ","); got != "paketo-node-engine,shpyrd-node-workspace,paketo-procfile?" {
		t.Errorf("group = %s", got)
	}

	both := app.DeepCopy()
	both.Spec.Build.Buildpacks = []string{"node"}
	if _, err := r.reconcileBuilder(ctx, both); err == nil || !strings.Contains(err.Error(), "build.buildpacks") {
		t.Errorf("workspace and buildpacks: %v", err)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/controller -run TestWorkspaceBuilder`
Expected: FAIL. `ref = map[kind:ClusterBuilder name:shpyrd]`, because a workspace doesn't compose yet.

- [ ] **Step 3: Compose for a workspace**

In `internal/controller/builder.go`, add after `var Stacks = …`:

```go
// A package of a JavaScript workspace (#145) builds with Node alone from
// Paketo, then shpyrd's workspace buildpack (buildpacks/node-workspace),
// which reads the package's path from NodeWorkspaceEnv.
const (
	NodeEngineBuildpack    = "paketo-node-engine"
	NodeWorkspaceBuildpack = "shpyrd-node-workspace"
	NodeWorkspaceEnv       = "BP_NODE_WORKSPACE"
)
```

Replace `composesBuild`:

```go
// composesBuild says the app asked for a builder of its own.
func composesBuild(app *shpyrdv1.App) bool {
	b := app.Spec.Build
	return b != nil && (len(b.Buildpacks) > 0 || b.Stack != "" || b.SystemPackages || b.Workspace != "")
}
```

In `desiredBuilder`, replace the line `names := app.Spec.Build.Buildpacks` and the `if len(names) == 0 {` that follows with:

```go
	names := app.Spec.Build.Buildpacks
	if app.Spec.Build.Workspace != "" {
		if len(names) > 0 {
			return nil, fmt.Errorf("build.workspace chooses its own buildpacks; remove build.buildpacks")
		}
		g := append(append([]interface{}{}, prefix...), entry(NodeEngineBuildpack), entry(NodeWorkspaceBuildpack))
		procfile := entry(BuildpackCatalog["procfile"])
		procfile["optional"] = true
		group = []interface{}{map[string]interface{}{"group": append(g, procfile)}}
	} else if len(names) == 0 {
```

The existing `} else {` branch for an explicit list stays as it is.

- [ ] **Step 4: Run it to see it pass**

Run: `go test ./internal/controller -run 'TestWorkspaceBuilder|TestBuildComposition|TestComposedBuilder'`
Expected: `ok`.

- [ ] **Step 5: Write the failing build-variable test**

Append to `internal/controller/desired_test.go` (it already imports `unstructured`, `metav1` and `shpyrdv1`; add `corev1 "k8s.io/api/core/v1"` if it doesn't):

```go
// The workspace's package reaches the build as BP_NODE_WORKSPACE (#145);
// a value the project writes in build.env wins.
func TestWorkspaceBuildVariable(t *testing.T) {
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "site", Namespace: "app-site"},
		Spec: shpyrdv1.AppSpec{
			Source: &shpyrdv1.Source{Blob: &shpyrdv1.BlobSource{URL: "http://blobs/a.tgz", SHA256: "abc"}},
			Build:  &shpyrdv1.Build{Workspace: "apps/website"},
		},
	}
	r, _ := newTestReconciler(t, app)
	r.Config.RegistryHost = "10.96.0.50:5000"
	env := func() map[string]string {
		img, err := r.Config.desiredKpackImage(app)
		if err != nil {
			t.Fatal(err)
		}
		list, _, _ := unstructured.NestedSlice(img.Object, "spec", "build", "env")
		out := map[string]string{}
		for _, e := range list {
			m := e.(map[string]interface{})
			if _, seen := out[m["name"].(string)]; seen {
				t.Errorf("%s twice", m["name"])
			}
			out[m["name"].(string)] = m["value"].(string)
		}
		return out
	}
	if got := env()["BP_NODE_WORKSPACE"]; got != "apps/website" {
		t.Errorf("BP_NODE_WORKSPACE = %q", got)
	}
	app.Spec.Build.Env = []corev1.EnvVar{{Name: "BP_NODE_WORKSPACE", Value: "apps/other"}}
	if got := env()["BP_NODE_WORKSPACE"]; got != "apps/other" {
		t.Errorf("written value lost: %q", got)
	}
}
```

- [ ] **Step 6: Run it to see it fail**

Run: `go test ./internal/controller -run TestWorkspaceBuildVariable`
Expected: FAIL, `BP_NODE_WORKSPACE = ""`.

- [ ] **Step 7: Pass the variable**

In `internal/controller/desired.go`, replace:

```go
	if app.Spec.Build != nil {
		for _, e := range app.Spec.Build.Env {
			env = append(env, map[string]interface{}{"name": e.Name, "value": e.Value})
		}
	}
```

with:

```go
	if app.Spec.Build != nil {
		// The package of a JavaScript workspace (#145), unless build.env
		// names one itself.
		if w := app.Spec.Build.Workspace; w != "" {
			written := false
			for _, e := range app.Spec.Build.Env {
				written = written || e.Name == NodeWorkspaceEnv
			}
			if !written {
				env = append(env, map[string]interface{}{"name": NodeWorkspaceEnv, "value": w})
			}
		}
		for _, e := range app.Spec.Build.Env {
			env = append(env, map[string]interface{}{"name": e.Name, "value": e.Value})
		}
	}
```

- [ ] **Step 8: Run the controller's tests**

Run: `go test ./internal/controller`
Expected: `ok`.

- [ ] **Step 9: Commit**

```bash
git add internal/controller/builder.go internal/controller/builder_test.go internal/controller/desired.go internal/controller/desired_test.go
git commit -m "feat(controller): a workspace package's builder and BP_NODE_WORKSPACE (#145)"
```

---

### Task 3: Wait for the project's own builder (#143)

**Files:**
- Modify: `internal/controller/status.go` (`buildState`, `readBuildState`)
- Create: `internal/controller/project_builder.go`
- Modify: `internal/controller/app_controller.go:409-415`
- Test: `internal/controller/project_builder_test.go`

**Interfaces:**
- Consumes: `composesBuild`, `builderName`, `BuilderGVK` (`builder.go`); `runReconcile` and `newTestReconciler` (`app_controller_test.go`).
- Produces:
  - `buildState.Reason` and `buildState.BuilderReady`, both strings;
  - `(buildState).builderNotReady() bool`;
  - `(*AppReconciler).projectBuilderFailure(ctx, app) string`;
  - `builderGrace = 2 * time.Minute`.

- [ ] **Step 1: Write the failing test**

`internal/controller/project_builder_test.go`:

```go
package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// A project's first deploy with a builder of its own (#143): kpack says
// the Image's builder is not ready while the Builder is being made. The
// project waits, Building, until the Builder has had two minutes; then
// kpack's reason is the build's failure.
func TestFirstDeployWaitsForTheProjectsBuilder(t *testing.T) {
	for _, tc := range []struct {
		name     string
		age      time.Duration
		builder  []interface{}
		phase    string
		contains string
	}{
		{"being made", 10 * time.Second, nil, shpyrdv1.PhaseBuilding, "preparing the project's builder"},
		{"failed for good", 5 * time.Minute, []interface{}{map[string]interface{}{"type": "Ready", "status": "False", "message": "buildpack image not found"}}, shpyrdv1.PhaseFailed, "buildpack image not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			app := &shpyrdv1.App{
				ObjectMeta: metav1.ObjectMeta{Name: "site", Namespace: "app-site", Generation: 1},
				Spec: shpyrdv1.AppSpec{
					Source: &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://github.com/example/repo", Revision: "main"}},
					Build:  &shpyrdv1.Build{Workspace: "apps/web"},
				},
			}
			b := &unstructured.Unstructured{}
			b.SetGroupVersionKind(BuilderGVK)
			b.SetNamespace("app-site")
			b.SetName("site-builder")
			b.SetCreationTimestamp(metav1.NewTime(time.Now().Add(-tc.age)))
			if tc.builder != nil {
				_ = unstructured.SetNestedSlice(b.Object, tc.builder, "status", "conditions")
			}
			r, c := newTestReconciler(t, app, b)
			runReconcile(t, r, app) // makes the kpack Image

			img := &unstructured.Unstructured{}
			img.SetGroupVersionKind(KpackImageGVK)
			if err := c.Get(ctx, types.NamespacedName{Namespace: "app-site", Name: "site"}, img); err != nil {
				t.Fatal(err)
			}
			_ = unstructured.SetNestedField(img.Object, int64(img.GetGeneration()), "status", "observedGeneration")
			_ = unstructured.SetNestedSlice(img.Object, []interface{}{
				map[string]interface{}{"type": "Ready", "status": "False", "reason": "BuilderNotReady", "message": "Builder site-builder is not ready"},
				map[string]interface{}{"type": "BuilderReady", "status": "False", "reason": "BuilderNotReady"},
			}, "status", "conditions")
			if err := c.Update(ctx, img); err != nil {
				t.Fatal(err)
			}
			got := runReconcile(t, r, app)
			if got.Status.Phase != tc.phase || !strings.Contains(got.Status.Message, tc.contains) {
				t.Errorf("phase = %s, message = %q", got.Status.Phase, got.Status.Message)
			}
			if strings.Contains(got.Status.Message, "could not start") {
				t.Errorf("still the platform-fault sentence: %q", got.Status.Message)
			}
		})
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/controller -run TestFirstDeployWaitsForTheProjectsBuilder`
Expected: both subtests FAIL. `being made` gets `phase = Failed, message = "The build could not start: a fault of the platform…"`.

If the fake client rejects the pre-made Builder in `newTestReconciler`, because it re-creates it in `reconcileBuilder`: `CreateOrUpdate` reads it first, so it updates it. Confirm the error isn't that before going on.

- [ ] **Step 3: Read the reason and the builder's condition**

In `internal/controller/status.go`, add to `buildState` after `Message string`:

```go
	// Reason is kpack's reason for Ready; BuilderReady mirrors the Image's
	// BuilderReady condition ("" when kpack has not set it).
	Reason       string
	BuilderReady string
```

In `readBuildState`, replace the loop body's `if !ok || m["type"] != "Ready" { continue }` and what follows it, up to the loop's end, with:

```go
		if !ok {
			continue
		}
		if m["type"] == "BuilderReady" {
			st.BuilderReady, _ = m["status"].(string)
			continue
		}
		if m["type"] != "Ready" {
			continue
		}
		if s, ok := m["status"].(string); ok && s != "" {
			st.Ready = s
		}
		if msg, ok := m["message"].(string); ok {
			st.Message = msg
		}
		st.Reason, _ = m["reason"].(string)
```

Then after `readBuildState`:

```go
// builderNotReady says kpack has not started the build because the
// Image's builder is not ready yet.
func (s buildState) builderNotReady() bool {
	return s.Reason == "BuilderNotReady" || s.BuilderReady == "False"
}
```

- [ ] **Step 4: The project's Builder's failure**

`internal/controller/project_builder.go`:

```go
package controller

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// builderGrace is how long a project's own Builder may take to become
// ready (#143): kpack makes it a few seconds after the first deploy, and
// retries while the buildpacks it names are still being pulled.
const builderGrace = 2 * time.Minute

// projectBuilderFailure is the sentence for a project's Builder that is
// not ready builderGrace after it was made, or "" while it may still be.
func (r *AppReconciler) projectBuilderFailure(ctx context.Context, app *shpyrdv1.App) string {
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(BuilderGVK)
	if err := r.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: builderName(app)}, b); err != nil {
		return ""
	}
	if time.Since(b.GetCreationTimestamp().Time) < builderGrace {
		return ""
	}
	reason := "it is still not ready"
	conds, _, _ := unstructured.NestedSlice(b.Object, "status", "conditions")
	for _, raw := range conds {
		m, ok := raw.(map[string]interface{})
		if !ok || m["type"] != "Ready" {
			continue
		}
		if m["status"] == "True" {
			return ""
		}
		if msg, _ := m["message"].(string); msg != "" {
			reason = msg
		}
	}
	return "The project's builder could not be made: " + reason + ". A fault of the platform, not of the app; deploying again later usually passes."
}
```

- [ ] **Step 5: Use it in the reconcile**

In `internal/controller/app_controller.go`, replace:

```go
			if build.Ready == "False" {
				if kpackBuild != nil && kpackMessage(kpackBuild) != "" && buildFailed(kpackBuild) {
					build.Message = r.kpackBuildFailure(ctx, kpackBuild)
				} else {
					build.Message = "The build could not start: a fault of the platform, not of the app. Deploying again later usually passes."
				}
			}
```

with:

```go
			if build.Ready == "False" {
				switch {
				case kpackBuild != nil && kpackMessage(kpackBuild) != "" && buildFailed(kpackBuild):
					build.Message = r.kpackBuildFailure(ctx, kpackBuild)
				case composesBuild(app) && build.builderNotReady():
					// A builder of the project's own is made on its first
					// deploy, and kpack waits for it (#143).
					if msg := r.projectBuilderFailure(ctx, app); msg != "" {
						build.Message = msg
					} else {
						build.Ready = "Unknown"
						build.Message = "preparing the project's builder"
					}
				default:
					build.Message = "The build could not start: a fault of the platform, not of the app. Deploying again later usually passes."
				}
			}
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/controller`
Expected: `ok`. Both subtests pass, and `TestReconcileSourceBuild` still sees `detect failed` as Failed.

- [ ] **Step 7: Commit**

```bash
git add internal/controller/status.go internal/controller/project_builder.go internal/controller/project_builder_test.go internal/controller/app_controller.go
git commit -m "fix(controller): a project's first build waits for its own builder (#143)"
```

---

### Task 4: The `node-workspace` buildpack

**Files:**
- Create: `buildpacks/node-workspace/buildpack.toml`
- Create: `buildpacks/node-workspace/bin/detect` (executable)
- Create: `buildpacks/node-workspace/bin/build` (executable)
- Create: `buildpacks/node-workspace/lib/workspace.js`
- Test: `buildpacks/node_workspace_test.go`
- Modify: `deploy/components/kpack/base/builder.yaml`, `.github/workflows/buildpacks.yml`

**Interfaces:**
- Consumes: `BP_NODE_WORKSPACE` in the build's environment (Task 2), and Node on `PATH` (from `paketo-node-engine`).
- Produces:
  - the ClusterBuildpacks `paketo-node-engine` and `shpyrd-node-workspace` that Task 2's group names;
  - `lib/workspace.js <path>`: shell assignments for `manager`, `path`, `name`, `has_build`, `has_start`, `package_manager`; or a sentence on stderr and exit 1.

- [ ] **Step 1: Write the failing tests**

`buildpacks/node_workspace_test.go`:

```go
package buildpacks

import (
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
func greetingWorkspace(root map[string]string) map[string]string {
	files := map[string]string{
		"packages/greeting/package.json": `{"name":"greeting","version":"1.0.0","main":"index.js"}`,
		"packages/greeting/index.js":     `module.exports = "hello from greeting";`,
		"apps/web/package.json": `{"name":"web","version":"1.0.0","private":true,
			"devDependencies":{"greeting":"1.0.0"},
			"scripts":{"build":"node -e \"require('fs').writeFileSync('built.txt', require('greeting'))\"","start":"node server.js"}}`,
		"apps/web/server.js": `console.log("up")`,
	}
	for k, v := range root {
		files[k] = v
	}
	return files
}

func TestNodeWorkspaceNpm(t *testing.T) {
	needNode(t)
	app := t.TempDir()
	writeFiles(t, app, greetingWorkspace(map[string]string{
		"package.json": `{"name":"root","private":true,"workspaces":["apps/*","packages/*"]}`,
	}))
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
		"package.json":      `{"name":"root","private":true,"workspaces":["apps/*","packages/*","!packages/legacy"]}`,
		"package-lock.json": `{}`,
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
	for _, tc := range []struct{ name, pm, lock string }{
		{"pnpm", "pnpm@9.15.0", "pnpm"},
		{"yarn 4", "yarn@4.5.3", "yarn"},
		{"yarn 1", "yarn@1.22.22", "yarn"},
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
			launch, _ := os.ReadFile(filepath.Join(layers, "launch.toml"))
			if !strings.Contains(string(launch), `type = "web"`) || !strings.Contains(string(launch), `"start"`) {
				t.Errorf("launch.toml:\n%s", launch)
			}
		})
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./buildpacks -run NodeWorkspace -short -v 2>&1 | tail -20`
Expected: FAIL, with `node-workspace/bin/build: No such file or directory` and the like. Without Node the tests SKIP: install Node before going on, since a skipped test proves nothing.

- [ ] **Step 3: The descriptor and detect**

`buildpacks/node-workspace/buildpack.toml`:

```toml
# One package of a JavaScript workspace (npm, pnpm or Yarn), built in place
# (shpyrd #145). Paketo's npm-install and yarn-install move workspace
# packages into layers of their own; this installs the package and what it
# needs in /workspace, runs its build script, and records its start script
# as the web process. Node comes from node-engine, before it in the group
# the controller composes for a project with build.workspace.
api = "0.9"

[buildpack]
  id = "shpyrd/node-workspace"
  version = "0.1.0"
  name = "shpyrd JavaScript workspace"
  homepage = "https://github.com/shpyrd-io/shpyrd/tree/main/buildpacks/node-workspace"

[[stacks]]
  id = "*"
```

`buildpacks/node-workspace/bin/detect`:

```bash
#!/usr/bin/env bash
# Takes part when the build names a package of a workspace (BP_NODE_WORKSPACE,
# which the controller sets from build.workspace). It needs Node, from
# node-engine, to build and to run. A missing lockfile fails the build, with
# a sentence, rather than detection, with none.
set -euo pipefail
plan="${CNB_BUILD_PLAN_PATH:-$2}"
[ -n "${BP_NODE_WORKSPACE:-}" ] || exit 100
printf '[[requires]]\n  name = "node"\n  [requires.metadata]\n    build = true\n    launch = true\n' > "$plan"
```

- [ ] **Step 4: The workspace reader**

`buildpacks/node-workspace/lib/workspace.js`:

```js
// Reads the workspace for bin/build: its package manager, by lockfile, and
// the package BP_NODE_WORKSPACE names. Prints shell assignments; on a
// mistake, a sentence on stderr and exit 1. Node's own modules only.
"use strict";
const fs = require("node:fs");
const path = require("node:path");

function fail(message) {
  process.stderr.write(message + "\n");
  process.exit(1);
}
function readJSON(file) {
  try {
    return JSON.parse(fs.readFileSync(file, "utf8"));
  } catch {
    return null;
  }
}
function quote(value) {
  return "'" + String(value).replace(/'/g, "'\\''") + "'";
}

const asked = process.argv[2] || "";
const want = asked.replace(/^(\.\/)+/, "").replace(/\/+$/, "");
if (!want || want.startsWith("/") || want.split("/").some((s) => s === "" || s === "." || s === ".."))
  fail(`build.workspace must be a package's path inside the repository, like apps/web; got "${asked}"`);

let manager;
if (fs.existsSync("pnpm-lock.yaml")) manager = "pnpm";
else if (fs.existsSync("yarn.lock")) manager = /^__metadata:/m.test(fs.readFileSync("yarn.lock", "utf8")) ? "yarn" : "yarn1";
else if (fs.existsSync("package-lock.json")) manager = "npm";
else fail("no lockfile at the workspace's root; commit the one your package manager writes (package-lock.json, pnpm-lock.yaml or yarn.lock)");

const root = readJSON("package.json") || {};
const patterns =
  manager === "pnpm"
    ? pnpmPackages()
    : Array.isArray(root.workspaces)
      ? root.workspaces
      : (root.workspaces && root.workspaces.packages) || [];

// pnpm-workspace.yaml's packages, as a block list or a flow list.
function pnpmPackages() {
  let text;
  try {
    text = fs.readFileSync("pnpm-workspace.yaml", "utf8");
  } catch {
    return [];
  }
  const unquote = (s) => s.trim().replace(/^['"]|['"]$/g, "");
  const out = [];
  let inList = false;
  for (const raw of text.split("\n")) {
    const line = raw.replace(/\s+#.*$/, "");
    const flow = line.match(/^packages:\s*\[(.*)\]\s*$/);
    if (flow) return flow[1].split(",").map(unquote).filter(Boolean);
    if (/^packages:\s*$/.test(line)) {
      inList = true;
      continue;
    }
    if (!inList) continue;
    const item = line.match(/^\s+-\s+(.+)$/);
    if (item) out.push(unquote(item[1]));
    else if (/^\S/.test(line)) inList = false;
  }
  return out;
}

// The managers' globs: "*" and "?" within a segment, "**" any number of
// segments, a leading "!" excludes. The CLI matches the same way
// (internal/cli/workspace.go).
function matches(pattern, p) {
  const pat = pattern.replace(/^(\.\/)+/, "").replace(/\/+$/, "").split("/");
  return segments(pat, p.split("/"));
}
function segments(pat, segs) {
  if (pat.length === 0) return segs.length === 0;
  if (pat[0] === "**") {
    for (let i = 0; i <= segs.length; i++) if (segments(pat.slice(1), segs.slice(i))) return true;
    return false;
  }
  return segs.length > 0 && one(pat[0], segs[0]) && segments(pat.slice(1), segs.slice(1));
}
function one(glob, name) {
  let re = "^";
  for (const ch of glob) re += ch === "*" ? "[^/]*" : ch === "?" ? "[^/]" : ch.replace(/[.+^${}()|[\]\\]/g, "\\$&");
  return new RegExp(re + "$").test(name);
}
function listed(p) {
  let yes = false;
  for (const pattern of patterns) {
    if (pattern.startsWith("!")) {
      if (matches(pattern.slice(1), p)) return false;
    } else if (matches(pattern, p)) yes = true;
  }
  return yes;
}

// Every folder of the workspace with a package.json that it lists, for
// the error message.
function packages(dir = ".", depth = 0, out = []) {
  if (depth > 4) return out;
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (!entry.isDirectory() || entry.name === "node_modules" || entry.name.startsWith(".")) continue;
    const sub = dir === "." ? entry.name : `${dir}/${entry.name}`;
    if (fs.existsSync(path.join(sub, "package.json")) && listed(sub)) out.push(sub);
    packages(sub, depth + 1, out);
  }
  return out;
}

if (!listed(want)) {
  const there = packages();
  fail(`${want} is not a package of this workspace; its packages are: ${there.length ? there.join(", ") : "none"}`);
}
const pkg = readJSON(path.join(want, "package.json"));
if (!pkg) fail(`${want} has no package.json`);
if (manager !== "npm" && !pkg.name) fail(`${want}/package.json has no name, which ${manager.replace("1", "")} needs to select it`);
const scripts = pkg.scripts || {};

console.log(`manager=${quote(manager)}`);
console.log(`path=${quote(want)}`);
console.log(`name=${quote(pkg.name || "")}`);
console.log(`has_build=${scripts.build ? 1 : 0}`);
console.log(`has_start=${scripts.start ? 1 : 0}`);
console.log(`package_manager=${quote(root.packageManager || "")}`);
```

- [ ] **Step 5: The build**

`buildpacks/node-workspace/bin/build`:

```bash
#!/usr/bin/env bash
# Installs and builds one package of a JavaScript workspace in place, in the
# app's directory, so node_modules and the build's output stay where the
# package expects them, and records its start script as the web process
# (shpyrd #145). The install keeps dev dependencies whatever NODE_ENV says:
# the build needs them. pnpm and Yarn come through corepack, at the version
# packageManager names; Node from 25 on has no corepack, and then it is
# installed. The managers' downloads are cached between builds.
set -euo pipefail
layers="${CNB_LAYERS_DIR:-$1}"
bp="${CNB_BUILDPACK_DIR:-$(cd "$(dirname "$0")/.." && pwd)}"

vars="$(node "$bp/lib/workspace.js" "${BP_NODE_WORKSPACE:-}")"
eval "$vars"

if [ "$has_start" = 0 ] && [ ! -f Procfile ]; then
  echo "shpyrd: add a start script to $path/package.json, or a Procfile at the workspace's root" >&2
  exit 1
fi

# Downloads, kept between builds and out of the image.
cache="$layers/cache"
mkdir -p "$cache"
printf '[types]\n  build = false\n  launch = false\n  cache = true\n' > "$layers/cache.toml"
export npm_config_cache="$cache/npm"
export npm_config_store_dir="$cache/pnpm-store"
export YARN_CACHE_FOLDER="$cache/yarn"
export YARN_ENABLE_GLOBAL_CACHE=false
export COREPACK_ENABLE_DOWNLOAD_PROMPT=0
export COREPACK_ENABLE_STRICT=0

# pnpm and Yarn: corepack's shims in a layer of their own, on PATH here,
# for the build scripts and at launch, with the managers it downloads.
if [ "$manager" != npm ]; then
  tools="$layers/corepack"
  mkdir -p "$tools/bin" "$tools/env"
  export COREPACK_HOME="$tools/home"
  printf '%s' "$COREPACK_HOME" > "$tools/env/COREPACK_HOME.override"
  printf '[types]\n  build = true\n  launch = true\n  cache = true\n' > "$layers/corepack.toml"
  corepack="corepack"
  if ! command -v corepack > /dev/null; then
    echo "shpyrd: this Node has no corepack; installing it"
    npm install --global --prefix "$tools/corepack" --no-audit --no-fund corepack > /dev/null
    corepack="$tools/corepack/bin/corepack"
  fi
  "$corepack" enable --install-directory "$tools/bin" pnpm yarn
  export PATH="$tools/bin:$PATH"
  # Where the shims find corepack itself, at launch too.
  if [ "$corepack" != corepack ]; then
    ln -sf "$corepack" "$tools/bin/corepack"
  fi
fi

echo "shpyrd: installing $path with ${package_manager:-$manager}"
case "$manager" in
  npm) npm ci --workspace "$path" --include=dev --no-audit --no-fund ;;
  pnpm) pnpm install --frozen-lockfile --prod=false --filter "$name..." ;;
  yarn)
    if yarn workspaces focus --help > /dev/null 2>&1; then
      YARN_ENABLE_IMMUTABLE_INSTALLS=true yarn workspaces focus "$name"
    else
      yarn install --immutable
    fi
    ;;
  yarn1) yarn install --frozen-lockfile --production=false --non-interactive ;;
esac

if [ "$has_build" = 1 ]; then
  echo "shpyrd: building $path"
  case "$manager" in
    npm) npm run build --workspace "$path" ;;
    pnpm) pnpm --filter "$name" run build ;;
    yarn | yarn1) yarn workspace "$name" run build ;;
  esac
fi

# Production at launch, unless the project says otherwise.
mkdir -p "$layers/launch-env/env.launch"
printf 'production' > "$layers/launch-env/env.launch/NODE_ENV.default"
printf '[types]\n  build = false\n  launch = true\n  cache = false\n' > "$layers/launch-env.toml"

if [ "$has_start" = 1 ]; then
  case "$manager" in
    npm) command='"npm", "run", "start", "--workspace", "'"$path"'"' ;;
    pnpm) command='"pnpm", "--filter", "'"$name"'", "run", "start"' ;;
    yarn | yarn1) command='"yarn", "workspace", "'"$name"'", "run", "start"' ;;
  esac
  printf '[[processes]]\n  type = "web"\n  command = [%s]\n  default = true\n' "$command" > "$layers/launch.toml"
fi
echo "shpyrd: $path is built"
```

Then: `chmod +x buildpacks/node-workspace/bin/detect buildpacks/node-workspace/bin/build`.

- [ ] **Step 6: Run the fast tests**

Run: `go test ./buildpacks -run NodeWorkspace -short -v 2>&1 | tail -20`
Expected: `TestNodeWorkspaceNpm`, `ReadsThePath`, `Failures` and `Detect` PASS; `OtherManagers` SKIPs.

- [ ] **Step 7: Run the slow ones**

Run: `go test ./buildpacks -run TestNodeWorkspaceOtherManagers -v 2>&1 | tail -30`
Expected: PASS for pnpm, yarn 4 and yarn 1. This machine's Node 26 has no corepack, so this also covers the install of corepack. Where a manager fails, read its message before changing anything (systematic-debugging). The command lines in Step 5 are the parts most likely to need adjusting, and any change to them must keep dev dependencies installed.

- [ ] **Step 8: Register the buildpacks with kpack**

In `deploy/components/kpack/base/builder.yaml`, add before the `---` that precedes `kind: ClusterBuilder`:

```yaml
---
# A package of a JavaScript workspace (shpyrd #145) builds with Node alone
# from Paketo, then shpyrd's workspace buildpack, in a builder of the
# project's own (internal/controller/builder.go).
apiVersion: kpack.io/v1alpha2
kind: ClusterBuildpack
metadata:
  name: paketo-node-engine
  labels:
    app.kubernetes.io/part-of: shpyrd
spec:
  serviceAccountRef:
    name: kpack-builder
    namespace: ${SHPYRD_SYSTEM_NS}
  image: index.docker.io/paketobuildpacks/node-engine:latest
---
apiVersion: kpack.io/v1alpha2
kind: ClusterBuildpack
metadata:
  name: shpyrd-node-workspace
  labels:
    app.kubernetes.io/part-of: shpyrd
spec:
  serviceAccountRef:
    name: kpack-builder
    namespace: ${SHPYRD_SYSTEM_NS}
  image: ghcr.io/shpyrd-io/buildpack-node-workspace:0.1.0
```

The surrounding `---` separators must stay exactly one between documents. Check with `grep -c '^---$'` before and after: the count goes up by 2.

In `.github/workflows/buildpacks.yml`:
- add `echo "name=${name}" >> "$GITHUB_OUTPUT"` to the tag step;
- change `--label org.opencontainers.image.description="shpyrd's build-env buildpack"` to `--label org.opencontainers.image.description="shpyrd's ${{ steps.tag.outputs.name }} buildpack"`;
- add the node-workspace tag to the comment's example: `git tag buildpack-node-workspace/v0.1.0`.

- [ ] **Step 9: Check the component still renders**

Run: `go test ./... 2>&1 | grep -v '^ok' | head`
Expected: nothing printed. Every package passes, including any test that loads the kpack manifests.

- [ ] **Step 10: Commit**

```bash
git add buildpacks/node-workspace buildpacks/node_workspace_test.go deploy/components/kpack/base/builder.yaml .github/workflows/buildpacks.yml
git commit -m "feat(buildpacks): node-workspace, one package of an npm, pnpm or Yarn workspace built in place (#145)"
```

---

### Task 5: The CLI finds the workspace and archives its root

**Files:**
- Create: `internal/cli/workspace.go`
- Test: `internal/cli/workspace_test.go`
- Modify: `internal/cli/appclient.go` (`projectBuild`, and `applyTo` about line 303)
- Modify: `internal/cli/projectconfig_save.go` (the `switch` about line 51)
- Modify: `internal/cli/deploy.go` (the wiring at lines 103-122 and 178; `archiveSource`, `gitOutput`)

**Interfaces:**
- Consumes: `shpyrdv1.Build.Workspace` (Task 1).
- Produces, all in `internal/cli/workspace.go`:
  - `type jsWorkspace struct{ Root, Package, Manager string }`;
  - `findWorkspace(dir, stop string) (*jsWorkspace, error)`;
  - `matchWorkspaceGlob(pattern, p string) bool`;
  - `workspaceFor(project *projectConfig, stop string) (root string, found *jsWorkspace, err error)`;
  - `reportWorkspace(out io.Writer, ws *jsWorkspace, top string)`.

  Also `archiveSource(out io.Writer, workingTree bool, dir string)`.

- [ ] **Step 1: Write the failing tests**

`internal/cli/workspace_test.go`:

```go
package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

func files(t *testing.T, root string, m map[string]string) {
	t.Helper()
	for name, content := range m {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMatchWorkspaceGlob(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		want          bool
	}{
		{"apps/*", "apps/web", true},
		{"./apps/*/", "apps/web", true},
		{"apps/*", "apps/web/sub", false},
		{"packages/**", "packages/a/b", true},
		{"**/web", "apps/web", true},
		{"apps/w?b", "apps/web", true},
		{"apps/web", "apps/website", false},
	} {
		if got := matchWorkspaceGlob(tc.pattern, tc.path); got != tc.want {
			t.Errorf("%s ~ %s = %v", tc.pattern, tc.path, got)
		}
	}
}

// The nearest folder above that lists this one, for each manager's way of
// listing; nothing for the root itself or a folder no workspace lists.
func TestFindWorkspace(t *testing.T) {
	top := t.TempDir()
	files(t, top, map[string]string{
		"package.json":                       `{"name":"r","workspaces":["apps/*","!apps/old"]}`,
		"package-lock.json":                  `{}`,
		"apps/web/package.json":              `{"name":"web"}`,
		"apps/old/package.json":              `{"name":"old"}`,
		"tools/x/package.json":               `{"name":"x"}`,
		"yarn1/package.json":                 `{"workspaces":{"packages":["pkgs/*"]}}`,
		"yarn1/yarn.lock":                    "",
		"yarn1/pkgs/a/package.json":          `{"name":"a"}`,
		"examples/mono/pnpm-workspace.yaml":  "packages:\n  - 'apps/*'\n",
		"examples/mono/package.json":         `{"name":"mono"}`,
		"examples/mono/apps/web/package.json": `{"name":"w"}`,
	})
	for _, tc := range []struct{ dir, root, pkg, manager string }{
		{"apps/web", ".", "apps/web", "npm"},
		{"yarn1/pkgs/a", "yarn1", "pkgs/a", "yarn"},
		{"examples/mono/apps/web", "examples/mono", "apps/web", "pnpm"},
		{"apps/old", "", "", ""},
		{"tools/x", "", "", ""},
		{".", "", "", ""},
	} {
		ws, err := findWorkspace(filepath.Join(top, tc.dir), top)
		if err != nil {
			t.Fatalf("%s: %v", tc.dir, err)
		}
		if tc.root == "" {
			if ws != nil {
				t.Errorf("%s: found %+v", tc.dir, ws)
			}
			continue
		}
		if ws == nil || ws.Root != filepath.Join(top, tc.root) || ws.Package != tc.pkg || ws.Manager != tc.manager {
			t.Errorf("%s: %+v", tc.dir, ws)
		}
	}
}

func TestWorkspaceForLeavesDockerfileBuildsAlone(t *testing.T) {
	top := t.TempDir()
	files(t, top, map[string]string{
		"package.json":          `{"workspaces":["apps/*"]}`,
		"apps/web/package.json": `{"name":"web"}`,
	})
	t.Chdir(filepath.Join(top, "apps/web"))
	pc := &projectConfig{Build: &projectBuild{Strategy: shpyrdv1.StrategyDockerfile}}
	root, ws, err := workspaceFor(pc, top)
	if err != nil || root != "" || ws != nil || pc.Build.Workspace != "" {
		t.Errorf("a Dockerfile build changed: %q %+v %v %+v", root, ws, err, pc.Build)
	}
	// Without a written strategy: the workspace, built with buildpacks.
	pc2 := &projectConfig{}
	root, ws, err = workspaceFor(pc2, top)
	if err != nil || ws == nil || root != top || pc2.Build.Workspace != "apps/web" || pc2.Build.Strategy != shpyrdv1.StrategyBuildpacks {
		t.Errorf("detection: %q %+v %v %+v", root, ws, err, pc2.Build)
	}
}

func TestWorkspaceForWrittenPath(t *testing.T) {
	top := t.TempDir()
	files(t, top, map[string]string{
		"package.json":          `{"workspaces":["apps/*"]}`,
		"apps/web/package.json": `{"name":"web"}`,
	})
	// Written in the package's own shpyrd.yaml: the root is above.
	t.Chdir(filepath.Join(top, "apps/web"))
	root, _, err := workspaceFor(&projectConfig{Build: &projectBuild{Workspace: "apps/web"}}, top)
	if err != nil || root != top {
		t.Errorf("from the package: %q %v", root, err)
	}
	// Written at the root: the root is here.
	t.Chdir(top)
	root, _, err = workspaceFor(&projectConfig{Build: &projectBuild{Workspace: "apps/web"}}, top)
	if err != nil || root != top {
		t.Errorf("from the root: %q %v", root, err)
	}
	// Neither: a sentence, not the wrong tree.
	t.Chdir(filepath.Join(top, "apps/web"))
	_, _, err = workspaceFor(&projectConfig{Build: &projectBuild{Workspace: "apps/api"}}, top)
	if err == nil || !strings.Contains(err.Error(), "build.workspace") {
		t.Errorf("a path that is neither: %v", err)
	}
}

func TestReportWorkspace(t *testing.T) {
	var b bytes.Buffer
	reportWorkspace(&b, &jsWorkspace{Root: "/r/examples/mono", Package: "apps/web", Manager: "pnpm"}, "/r")
	reportWorkspace(&b, &jsWorkspace{Root: "/r", Package: "apps/site", Manager: "npm"}, "/r")
	want := "==> Detected a pnpm workspace at examples/mono; building apps/web in it\n" +
		"    build.workspace=apps/web (shpyrd.yaml values win; --save writes it there)\n" +
		"==> Detected an npm workspace at the repository's root; building apps/site in it\n" +
		"    build.workspace=apps/site (shpyrd.yaml values win; --save writes it there)\n"
	if b.String() != want {
		t.Errorf("got:\n%s", b.String())
	}
}

// The archive is the workspace's root, committed, with the package in it.
func TestArchiveSourceOfAWorkspaceRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	top := t.TempDir()
	files(t, top, map[string]string{
		"package.json":          `{"workspaces":["apps/*"]}`,
		"apps/web/package.json": `{"name":"web"}`,
	})
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "x"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = top
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	t.Chdir(filepath.Join(top, "apps/web"))
	data, _, err := archiveSource(io.Discard, false, top)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			names = append(names, h.Name)
		}
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "apps/web/package.json,package.json" {
		t.Errorf("archive = %v", names)
	}
}
```

`go.mod` says Go 1.27, so `t.Chdir` is available.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/cli -run 'Workspace|ArchiveSourceOf'`
Expected: compile errors: `undefined: matchWorkspaceGlob`, `findWorkspace`, `workspaceFor`, `reportWorkspace`, `projectBuild has no field Workspace`, and `archiveSource` given too many arguments.

- [ ] **Step 3: Detection**

`internal/cli/workspace.go`:

```go
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// JavaScript workspaces (shpyrd #145): a package of an npm, pnpm or Yarn
// workspace is deployed from its own folder. Its build needs the whole
// workspace, so the deploy archives the workspace's root and names the
// package with build.workspace.

type jsWorkspace struct {
	Root    string // the workspace's root, absolute
	Package string // the package's path from the root, slash-separated
	Manager string // npm, pnpm or yarn
}

// findWorkspace walks up from dir to stop (included; "" for the
// filesystem's root) to the nearest folder that declares a workspace
// listing dir; nil when none does.
func findWorkspace(dir, stop string) (*jsWorkspace, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for cur := abs; cur != stop && filepath.Dir(cur) != cur; {
		cur = filepath.Dir(cur)
		rel, err := filepath.Rel(cur, abs)
		if err != nil {
			return nil, err
		}
		if patterns, manager := workspacePatterns(cur); patterns != nil && workspaceListed(patterns, filepath.ToSlash(rel)) {
			return &jsWorkspace{Root: cur, Package: filepath.ToSlash(rel), Manager: manager}, nil
		}
	}
	return nil, nil
}

// workspacePatterns reads the folders a workspace root declares: pnpm's
// pnpm-workspace.yaml, or package.json's workspaces (a list, or Yarn 1's
// {packages: [...]}).
func workspacePatterns(dir string) ([]string, string) {
	if b, err := os.ReadFile(filepath.Join(dir, "pnpm-workspace.yaml")); err == nil {
		var w struct {
			Packages []string `yaml:"packages"`
		}
		if yaml.Unmarshal(b, &w) == nil && len(w.Packages) > 0 {
			return w.Packages, "pnpm"
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil, ""
	}
	var pkg struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}
	if json.Unmarshal(b, &pkg) != nil || len(pkg.Workspaces) == 0 {
		return nil, ""
	}
	var list []string
	if json.Unmarshal(pkg.Workspaces, &list) != nil {
		var yarn1 struct {
			Packages []string `json:"packages"`
		}
		if json.Unmarshal(pkg.Workspaces, &yarn1) != nil {
			return nil, ""
		}
		list = yarn1.Packages
	}
	manager := "npm"
	if _, err := os.Stat(filepath.Join(dir, "yarn.lock")); err == nil {
		manager = "yarn"
	}
	return list, manager
}

func workspaceListed(patterns []string, rel string) bool {
	listed := false
	for _, p := range patterns {
		if neg := strings.TrimPrefix(p, "!"); neg != p {
			if matchWorkspaceGlob(neg, rel) {
				return false
			}
		} else if matchWorkspaceGlob(p, rel) {
			listed = true
		}
	}
	return listed
}

// matchWorkspaceGlob matches the managers' globs: path.Match within a
// segment, "**" for any number of segments. The buildpack matches the same
// way (buildpacks/node-workspace/lib/workspace.js).
func matchWorkspaceGlob(pattern, p string) bool {
	for strings.HasPrefix(pattern, "./") {
		pattern = pattern[2:]
	}
	pattern = strings.TrimRight(pattern, "/")
	return matchSegments(strings.Split(pattern, "/"), strings.Split(p, "/"))
}

func matchSegments(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(segs); i++ {
			if matchSegments(pat[1:], segs[i:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	ok, _ := path.Match(pat[0], segs[0])
	return ok && matchSegments(pat[1:], segs[1:])
}

// workspaceFor settles a local deploy's workspace: the one shpyrd.yaml
// names, or the one found above the current folder. It fills
// project.Build.Workspace and returns the folder to archive ("" for the
// current one), and what it found ("" and nil when shpyrd.yaml said it).
// A Dockerfile build written in shpyrd.yaml is left as it is.
func workspaceFor(project *projectConfig, stop string) (string, *jsWorkspace, error) {
	if project.Build != nil && project.Build.Strategy == shpyrdv1.StrategyDockerfile {
		return "", nil, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", nil, err
	}
	if project.Build != nil && project.Build.Workspace != "" {
		w := strings.Trim(project.Build.Workspace, "/")
		project.Build.Workspace = w
		if suffix := string(filepath.Separator) + filepath.FromSlash(w); strings.HasSuffix(cwd, suffix) {
			return strings.TrimSuffix(cwd, suffix), nil, nil
		}
		if _, err := os.Stat(filepath.Join(cwd, filepath.FromSlash(w), "package.json")); err == nil {
			return cwd, nil, nil
		}
		return "", nil, fmt.Errorf("shpyrd.yaml: build.workspace %s is neither this folder nor a package below it", w)
	}
	ws, err := findWorkspace(cwd, stop)
	if err != nil || ws == nil {
		return "", nil, err
	}
	if project.Build == nil {
		project.Build = &projectBuild{}
	}
	project.Build.Workspace = ws.Package
	if project.Build.Strategy == "" {
		project.Build.Strategy = shpyrdv1.StrategyBuildpacks
	}
	return ws.Root, ws, nil
}

// reportWorkspace says what detection found, as build profiles do; top is
// the repository's root ("" outside one).
func reportWorkspace(out io.Writer, ws *jsWorkspace, top string) {
	where := ws.Root
	if top != "" {
		if rel, err := filepath.Rel(top, ws.Root); err == nil {
			where = filepath.ToSlash(rel)
			if rel == "." {
				where = "the repository's root"
			}
		}
	}
	article := "a"
	if ws.Manager == "npm" {
		article = "an"
	}
	fmt.Fprintf(out, "==> Detected %s %s workspace at %s; building %s in it\n", article, ws.Manager, where, ws.Package)
	fmt.Fprintf(out, "    build.workspace=%s (shpyrd.yaml values win; --save writes it there)\n", ws.Package)
}
```

`TestFindWorkspace`'s `{".", …}` case passes `top` as `dir` with `stop = top`. The loop condition `cur != stop` stops before looking above it, so the root itself is never its own workspace.

- [ ] **Step 4: Carry the field, and let `--save` write it**

In `internal/cli/appclient.go`, add to `projectBuild` after `Stack`:

```go
	// Workspace is the package to build inside a JavaScript workspace, as
	// a path from the workspace's root (shpyrd #145). The CLI finds it when
	// deploying from a package's folder.
	Workspace string `json:"workspace,omitempty"`
```

In `applyTo`, in the `b := &shpyrdv1.Build{…}` literal, add `Workspace: pc.Build.Workspace` after `Stack: pc.Build.Stack`.

In `internal/cli/projectconfig_save.go`, add a case before `case inf.what == "build.stack":`:

```go
		case inf.what == "build.workspace":
			setPath(root, []string{"build", "workspace"}, scalar(inf.value))
```

- [ ] **Step 5: Archive a folder, and wire the deploy**

In `internal/cli/deploy.go`:

Replace `gitOutput` with:

```go
func gitOutput(args ...string) (string, error) { return gitOutputIn("", args...) }

// gitOutputIn runs git in dir ("" for the current folder).
func gitOutputIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stderr = io.Discard
	b, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
```

Change `archiveSource`'s signature and comment to:

```go
// archiveSource returns a tar.gz of dir ("" for the current folder; a
// JavaScript workspace's root, #145) and a reference for the release
// description. Inside a Git repository the committed HEAD tree of dir is
// used unless workingTree is set (or dir has no tracked files); elsewhere
// the folder is tarred.
func archiveSource(out io.Writer, workingTree bool, dir string) ([]byte, string, error) {
	tarDir := firstNonEmpty(dir, ".")
```

In its body:
- every `gitOutput(` becomes `gitOutputIn(dir, `;
- both `tarDirectory(".")` become `tarDirectory(tarDir)`;
- after `cmd := exec.Command("git", "archive", "--format=tar.gz", "HEAD")`, add `cmd.Dir = dir`;
- the messages `"==> Archiving current directory (not a git repository)"` and `"==> Archiving working tree (%s)\n"` stay.

In the deploy command, find:

```go
			if image == "" && gitURL == "" {
				project = detectDockerfile(project, subPath)
```

Replace those two lines with:

```go
			// A package of a JavaScript workspace (#145): the workspace's
			// root is what gets built.
			workspaceRoot := ""
			if image == "" && gitURL == "" && subPath == "" {
				if project == nil {
					project = &projectConfig{}
				}
				top, _ := gitOutput("rev-parse", "--show-toplevel")
				root, found, err := workspaceFor(project, top)
				if err != nil {
					return err
				}
				workspaceRoot = root
				if found != nil {
					reportWorkspace(out, found, top)
					if save {
						det := &detection{inferred: []inference{{what: "build.workspace", value: found.Package}}}
						if _, _, err := saveInferences(".", name, det, false); err != nil {
							return err
						}
					}
				}
			}
			if image == "" && gitURL == "" {
				project = detectDockerfile(project, subPath)
```

Then change `archive, ref, err := archiveSource(out, workingTree)` to `archive, ref, err := archiveSource(out, workingTree, workspaceRoot)`.

Also update the `--save` flag's help text, `"write what the build profile inferred into shpyrd.yaml"`, to `"write what the build profile or workspace detection inferred into shpyrd.yaml"`.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/cli`
Expected: `ok`. The new tests pass, and the existing deploy and profile tests still do.

- [ ] **Step 7: Try it by hand**

Run: `make cli && cd examples/hello && ../../bin/shpyrd deploy --project nope --no-wait 2>&1 | head -3; cd ../..`
Expected: no `Detected … workspace` line. `examples/hello` is a Go app in no workspace, so the deploy is unchanged; it fails later on the project lookup, which is fine here.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/workspace.go internal/cli/workspace_test.go internal/cli/appclient.go internal/cli/projectconfig_save.go internal/cli/deploy.go
git commit -m "feat(cli): deploying a JavaScript workspace's package archives the workspace (#145)"
```

---

### Task 6: An example monorepo, deployed in CI

**Files:**
- Create: `examples/monorepo/package.json`, `pnpm-workspace.yaml`, `pnpm-lock.yaml` (generated), `.gitignore`, `README.md`
- Create: `examples/monorepo/packages/greeting/{package.json,build.js}`
- Create: `examples/monorepo/apps/web/{package.json,server.js}`
- Modify: `.github/workflows/ci.yml` (the end-to-end job's deploy and check steps)

**Interfaces:**
- Consumes: everything above. The project name `monorepo` answers at `https://monorepo.127.0.0.1.nip.io:8443/`.

- [ ] **Step 1: The workspace**

`examples/monorepo/package.json`:

```json
{
  "name": "monorepo",
  "private": true,
  "packageManager": "pnpm@9.15.0"
}
```

`examples/monorepo/pnpm-workspace.yaml`:

```yaml
packages:
  - apps/*
  - packages/*
```

`examples/monorepo/packages/greeting/package.json`:

```json
{
  "name": "@monorepo/greeting",
  "version": "1.0.0",
  "main": "dist/index.js",
  "scripts": { "build": "node build.js" }
}
```

`examples/monorepo/packages/greeting/build.js`:

```js
// The package's build: what apps/web imports exists only after it runs.
const fs = require("node:fs");
fs.mkdirSync("dist", { recursive: true });
fs.writeFileSync("dist/index.js", 'module.exports = "Hello from a workspace package";\n');
```

`examples/monorepo/apps/web/package.json`:

```json
{
  "name": "@monorepo/web",
  "private": true,
  "dependencies": { "@monorepo/greeting": "workspace:*" },
  "scripts": {
    "build": "pnpm --filter @monorepo/greeting run build",
    "start": "node server.js"
  }
}
```

`examples/monorepo/apps/web/server.js`:

```js
// A page that says what a sibling package of the workspace built.
const http = require("node:http");
const greeting = require("@monorepo/greeting");

http
  .createServer((_, res) => {
    res.setHeader("content-type", "text/html; charset=utf-8");
    res.end(`<h1>${greeting}</h1>\n`);
  })
  .listen(process.env.PORT || 8080);
```

`examples/monorepo/.gitignore`:

```
node_modules/
dist/
```

`examples/monorepo/README.md`:

```markdown
# A pnpm workspace

`apps/web` serves a page with what `packages/greeting` builds. Deploy the
app from its own folder; shpyrd finds the workspace above it, uploads the
whole of it, and builds `apps/web` there:

    cd apps/web && shpyrd deploy
```

- [ ] **Step 2: The lockfile**

Run: `cd examples/monorepo && npx -y pnpm@9.15.0 install --lockfile-only && cd ../.. && head -5 examples/monorepo/pnpm-lock.yaml`
Expected: `lockfileVersion: '9.0'`, and a lockfile with no registry packages.

- [ ] **Step 3: Check it works locally the way the buildpack runs it**

Run: `cd examples/monorepo && BP_NODE_WORKSPACE=apps/web CNB_LAYERS_DIR=$(mktemp -d) CNB_BUILDPACK_DIR=$PWD/../../buildpacks/node-workspace bash ../../buildpacks/node-workspace/bin/build && (cd apps/web && PORT=18080 timeout 3 node server.js & sleep 1; curl -s localhost:18080); git clean -fdxq . ; cd ../..`
Expected: the build ends with `shpyrd: apps/web is built`, and curl prints `<h1>Hello from a workspace package</h1>`. The `git clean` removes `node_modules` and `dist` from the example, and only there; the trailing `.` limits it to that folder.

- [ ] **Step 4: Deploy it in CI**

In `.github/workflows/ci.yml`, in the step `Deploy a buildpacks project and a Dockerfile project`, add before `./bin/shpyrd projects list --context kind-ci`:

```yaml
          # A package of a pnpm workspace, from its own folder (#145).
          ./bin/shpyrd projects create monorepo --public --context kind-ci
          (cd examples/monorepo/apps/web && ../../../../bin/shpyrd deploy --project monorepo --context kind-ci)
```

Rename that step to `Deploy a buildpacks project, a Dockerfile project and a workspace package`.

In the step `The projects answer on their URLs`, add at its end:

```yaml
          for i in $(seq 1 30); do
            if curl -fsk --max-time 5 "https://monorepo.127.0.0.1.nip.io:8443/" >/tmp/monorepo.html; then break; fi
            sleep 5
          done
          grep -q "Hello from a workspace package" /tmp/monorepo.html
```

- [ ] **Step 5: Commit**

```bash
git add examples/monorepo .github/workflows/ci.yml
git commit -m "test(e2e): a pnpm workspace's package deploys from its own folder (#145)"
```

---

### Task 7: Docs

**Files:**
- Modify: `content/docs/deploying.md` (a new section after `### System packages (Aptfile)`, before `## Release phase`)
- Modify: `content/docs/shpyrd-yaml.md` (a row after `build.stack`)
- Modify: `content/docs/cli.md` (the `shpyrd deploy` row, line 153)

- [ ] **Step 1: deploying.md**

Insert before `## Release phase`:

```markdown
### Monorepos and workspaces

An app that is one package of an npm, pnpm or Yarn workspace deploys from its own folder:

    cd apps/web && shpyrd deploy

`shpyrd deploy` finds the workspace's root above the folder (a `package.json` with `workspaces`, or a `pnpm-workspace.yaml`, that lists it), uploads the whole workspace and builds that package in it:

    ==> Detected a pnpm workspace at the repository's root; building apps/web in it
        build.workspace=apps/web (shpyrd.yaml values win; --save writes it there)

The build installs what the package needs (its workspace dependencies included), with the package manager the root's lockfile says and the version its `packageManager` names, runs the package's `build` script, and starts it with its `start` script. If the package needs its workspace dependencies built first, its `build` script says so, as it would locally (`pnpm --filter @acme/ui run build && next build`, or `turbo run build --filter web`). A `Procfile` at the workspace's root replaces the `start` script.

A workspace package builds with buildpacks, even when the folder or the root has a `Dockerfile`; `build.strategy: dockerfile` in `shpyrd.yaml` keeps a Dockerfile build, and then no workspace is detected. `build.workspace` can be written by hand, as the package's path from the workspace's root.
```

- [ ] **Step 2: shpyrd-yaml.md**

After the `build.stack` row:

```markdown
| `build.workspace` | The package to build inside an npm, pnpm or Yarn workspace, as its path from the workspace's root (`apps/web`). `shpyrd deploy` finds it when run from the package's folder; see [Monorepos and workspaces](/docs/deploying#monorepos-and-workspaces). Buildpacks only; it cannot be combined with `build.buildpacks`. |
```

In the `build.builder` row, change `Ignored when \`build.buildpacks\`, \`build.stack\` or an \`Aptfile\`` to `Ignored when \`build.buildpacks\`, \`build.stack\`, \`build.workspace\` or an \`Aptfile\``.

- [ ] **Step 3: cli.md**

In the `shpyrd deploy` row, after `Archive the committed tree of the current directory, upload, build and release.`, add: `From a package of an npm, pnpm or Yarn workspace it archives the workspace's root and builds that package ([Monorepos and workspaces](/docs/deploying#monorepos-and-workspaces)).` In the same row, change `--save writes what the build profile inferred` to `--save writes what the build profile or workspace detection inferred`.

- [ ] **Step 4: Check the docs build**

Run: `npm run test --workspace apps/website 2>&1 | tail -5`
Expected: the website's tests pass, including any that render every docs page.

- [ ] **Step 5: Commit**

```bash
git add content/docs/deploying.md content/docs/shpyrd-yaml.md content/docs/cli.md
git commit -m "docs: monorepos and workspaces, build.workspace (#145)"
```

---

### Task 8: Publish the buildpack and open the pull request

These steps have effects outside the branch. **Ask before each push.**

- [ ] **Step 1: The whole suite**

Run: `go test ./... 2>&1 | grep -v '^ok' ; make generate && git status --short`
Expected: no failures, and no changes from `make generate`.

- [ ] **Step 2: Push the branch, then publish the image (ask first)**

```bash
git push -u origin feat/node-workspaces
git tag buildpack-node-workspace/v0.1.0 && git push origin buildpack-node-workspace/v0.1.0
```

Then check that the `buildpacks` workflow published `ghcr.io/shpyrd-io/buildpack-node-workspace:0.1.0`. Make the package public once, in the organisation's package settings, as for `build-env`: every cluster pulls it without credentials.

- [ ] **Step 3: Open the pull request (ask first)**

Title: `JavaScript workspaces build with buildpacks; a first build waits for its project's builder (#145, #143)`. The body:
- what a person does now, `cd apps/web && shpyrd deploy`;
- the new buildpack and image;
- #143's waiting;
- `Closes #145`, `Closes #143`.

Wait for the end-to-end job to deploy `examples/monorepo`.

---

### Task 9 (after the release): The website builds with buildpacks

This is a separate pull request, after a shpyrd release carries the above to platform.shpyrd.cloud, and after the gateway's upload fix that Patrick is doing. Today no website deploy succeeds.

- [ ] **Step 1:** In `apps/website/shpyrd.yaml`, remove the Dockerfile settings (`strategy`, `dockerfile`, `target`).
- [ ] **Step 2:** Delete `apps/website/Dockerfile` and its `.dockerignore`. Remove `output: "standalone"` from `apps/website/next.config.ts`, and make the package's `start` script `next start`.
- [ ] **Step 3:** In the website's deploy workflow, remove the step that copies `shpyrd.yaml` to the repository's root, and deploy with `cd apps/website && shpyrd deploy`.
- [ ] **Step 4:** Deploy once from a laptop to a preview project before merging, and open it.
