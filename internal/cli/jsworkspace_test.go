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
		"package.json":                        `{"name":"r","workspaces":["apps/*","!apps/old"]}`,
		"package-lock.json":                   `{}`,
		"apps/web/package.json":               `{"name":"web"}`,
		"apps/old/package.json":               `{"name":"old"}`,
		"tools/x/package.json":                `{"name":"x"}`,
		"yarn1/package.json":                  `{"workspaces":{"packages":["pkgs/*"]}}`,
		"yarn1/yarn.lock":                     "",
		"yarn1/pkgs/a/package.json":           `{"name":"a"}`,
		"examples/mono/pnpm-workspace.yaml":   "packages:\n  - 'apps/*'\n",
		"examples/mono/package.json":          `{"name":"mono"}`,
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

// A written build.workspace never makes the deploy upload a folder that is
// not a workspace listing it, nor one outside the repository: a cloned
// project's shpyrd.yaml cannot send the folders above it.
func TestWorkspaceForWrittenPathIsAWorkspace(t *testing.T) {
	top := t.TempDir()
	files(t, top, map[string]string{
		"home/project/package.json":  `{"name":"p"}`,
		"repo/package.json":          `{"workspaces":["apps/*"]}`,
		"repo/apps/web/package.json": `{"name":"web"}`,
	})
	// The folder's own name, written as a workspace: the parent is no
	// workspace, so nothing above is uploaded.
	t.Chdir(filepath.Join(top, "home/project"))
	if root, _, err := workspaceFor(&projectConfig{Build: &projectBuild{Workspace: "project"}}, ""); err == nil {
		t.Errorf("a parent that declares no workspace was accepted: %q", root)
	}
	t.Chdir(filepath.Join(top, "repo/apps/web"))
	if _, _, err := workspaceFor(&projectConfig{Build: &projectBuild{Workspace: "../web"}}, ""); err == nil || !strings.Contains(err.Error(), "build.workspace") {
		t.Errorf("a path with ..: %v", err)
	}
	// A real workspace, but above the repository's root: refused.
	if _, _, err := workspaceFor(&projectConfig{Build: &projectBuild{Workspace: "apps/web"}}, filepath.Join(top, "repo/apps")); err == nil {
		t.Error("a root outside the repository was accepted")
	}
}

// A path written loosely, as people write folders, is read as the package's
// path: in the deploy from the package, and in what goes to the API (a
// --git deploy too).
func TestWorkspaceWrittenLoosely(t *testing.T) {
	top := t.TempDir()
	files(t, top, map[string]string{
		"package.json":          `{"workspaces":["apps/*"]}`,
		"apps/web/package.json": `{"name":"web"}`,
	})
	t.Chdir(filepath.Join(top, "apps/web"))
	pc := &projectConfig{Build: &projectBuild{Workspace: "./apps/web/"}}
	root, _, err := workspaceFor(pc, top)
	if err != nil || root != top || pc.Build.Workspace != "apps/web" {
		t.Errorf("workspaceFor: %q %v %q", root, err, pc.Build.Workspace)
	}
	app := &shpyrdv1.App{}
	if err := (&projectConfig{Build: &projectBuild{Workspace: "./apps/web/"}}).applyTo(app); err != nil {
		t.Fatal(err)
	}
	if app.Spec.Build == nil || app.Spec.Build.Workspace != "apps/web" {
		t.Errorf("applyTo: %+v", app.Spec.Build)
	}
}
