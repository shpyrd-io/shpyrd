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
