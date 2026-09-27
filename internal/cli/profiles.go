package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// Build profiles (RFC-0067). The buildpacks detect the language; a few
// patterns need hints they cannot guess: a static site needs BP_WEB_SERVER,
// a Vite app must be built with the web-servers buildpack or the Node one
// wins detection and ships an image with nothing to run, a Rack app only
// permits real hostnames in production. The CLI recognises those patterns
// in the source directory and fills in what shpyrd.yaml leaves out, saying
// what it inferred and why. Explicit shpyrd.yaml values always win.

// profile is one recognised pattern and the settings it implies.
type profile struct {
	// Name is short, for the message: "Rails application".
	Name string
	// match inspects the directory and reports what it saw when the
	// profile applies ("config/application.rb").
	match func(dir string) (reason string, ok bool)
	// BuildEnv are BP_* (and build-time) variables, set unless present.
	BuildEnv map[string]string
	// Buildpacks composes the build (RFC-0065), set unless present.
	Buildpacks []string
	// Stack is base or full, set unless present.
	Stack string
	// Env are plain runtime variables, set unless present.
	Env map[string]string
	// HealthPath is the web process's health check path, set unless the
	// web process declares a health check.
	HealthPath string
}

// languageProfiles are exclusive: the first that matches wins.
var languageProfiles = []profile{
	{
		Name: "Rails application",
		match: func(dir string) (string, bool) {
			if !fileExists(dir, "config/application.rb") {
				return "", false
			}
			return "config/application.rb", true
		},
		Env:        map[string]string{"RAILS_ENV": "production", "RAILS_LOG_TO_STDOUT": "1"},
		HealthPath: railsHealthPath,
	},
	{
		Name: "Rack application",
		match: func(dir string) (string, bool) {
			if !fileExists(dir, "config.ru") {
				return "", false
			}
			return "config.ru", true
		},
		Env: map[string]string{"RACK_ENV": "production"},
	},
	{
		Name: "Next.js application",
		match: func(dir string) (string, bool) {
			pkg := readPackageJSON(dir)
			if pkg == nil || !pkg.depends("next") {
				return "", false
			}
			return "next in package.json", true
		},
		BuildEnv: map[string]string{"BP_NODE_RUN_SCRIPTS": "build", "NODE_ENV": "production"},
	},
	{
		Name: "Vite single-page app",
		match: func(dir string) (string, bool) {
			pkg := readPackageJSON(dir)
			if pkg == nil || !pkg.depends("vite") || pkg.Scripts["build"] == "" || pkg.Scripts["start"] != "" {
				return "", false
			}
			return "vite in package.json, a build script and no start script", true
		},
		Buildpacks: []string{"web-servers"},
		BuildEnv: map[string]string{
			"BP_NODE_RUN_SCRIPTS":             "build",
			"BP_WEB_SERVER":                   "nginx",
			"BP_WEB_SERVER_ROOT":              "dist",
			"BP_WEB_SERVER_ENABLE_PUSH_STATE": "true",
		},
	},
	{
		Name: "PHP application",
		match: func(dir string) (string, bool) {
			if !fileExists(dir, "public/index.php") {
				return "", false
			}
			return "public/index.php", true
		},
		BuildEnv: map[string]string{"BP_PHP_SERVER": "nginx", "BP_PHP_WEB_DIR": "public"},
	},
	{
		Name: "static site",
		match: func(dir string) (string, bool) {
			if !fileExists(dir, "public/index.html") {
				return "", false
			}
			// A language would take the build somewhere else.
			for _, f := range []string{"package.json", "Gemfile", "go.mod", "requirements.txt", "pom.xml", "build.gradle", "composer.json", "Procfile"} {
				if fileExists(dir, f) {
					return "", false
				}
			}
			if hasGlob(dir, "*.csproj") {
				return "", false
			}
			return "public/index.html and no language files", true
		},
		Buildpacks: []string{"web-servers"},
		BuildEnv:   map[string]string{"BP_WEB_SERVER": "nginx", "BP_WEB_SERVER_ROOT": "public"},
	},
}

// railsHealthPath is set only when the app routes Rails' health check.
const railsHealthPath = "/up"

// heavyPackages have dependency trees (glib, cairo, curl) the base run
// image lacks; the full stack carries them.
var heavyPackages = []string{"libvips", "imagemagick", "libmagick", "ffmpeg", "libgdal", "libopencv", "tesseract", "chromium", "wkhtmltopdf", "libreoffice", "poppler", "graphviz"}

// addonProfiles apply on top of a language profile (or none).
var addonProfiles = []profile{
	{
		Name: "system packages with deep dependencies",
		match: func(dir string) (string, bool) {
			b, err := os.ReadFile(filepath.Join(dir, "Aptfile"))
			if err != nil {
				return "", false
			}
			pkgs, _ := aptfilePackages(string(b))
			for _, p := range pkgs {
				for _, heavy := range heavyPackages {
					if strings.HasPrefix(p, heavy) {
						return p + " in the Aptfile", true
					}
				}
			}
			return "", false
		},
		Stack: "full",
	},
}

// inference is one value a profile filled in, for the message.
type inference struct{ what, value string }

// detection is what applyProfiles found and did.
type detection struct {
	profiles  []string
	reasons   []string
	inferred  []inference
	healthSet bool
}

func (d *detection) empty() bool { return len(d.inferred) == 0 }

// applyProfiles fills the project config with what the matching profiles
// imply and shpyrd.yaml does not say. dir is the directory to be deployed.
func applyProfiles(pc *projectConfig, dir string) (*projectConfig, *detection) {
	det := &detection{}
	if pc == nil {
		pc = &projectConfig{}
	}
	var matched []profile
	for _, p := range languageProfiles {
		if reason, ok := p.match(dir); ok {
			matched = append(matched, p)
			det.profiles = append(det.profiles, p.Name)
			det.reasons = append(det.reasons, reason)
			break
		}
	}
	for _, p := range addonProfiles {
		if reason, ok := p.match(dir); ok {
			matched = append(matched, p)
			det.profiles = append(det.profiles, p.Name)
			det.reasons = append(det.reasons, reason)
		}
	}
	if len(matched) == 0 {
		return pc, det
	}
	// A Dockerfile build takes none of the buildpack hints.
	dockerfile := pc.Build != nil && pc.Build.Strategy == shpyrdv1.StrategyDockerfile
	for _, p := range matched {
		if !dockerfile && (len(p.Buildpacks) > 0 || len(p.BuildEnv) > 0 || p.Stack != "") {
			if pc.Build == nil {
				pc.Build = &projectBuild{}
			}
			if len(p.Buildpacks) > 0 && len(pc.Build.Buildpacks) == 0 {
				pc.Build.Buildpacks = append([]string(nil), p.Buildpacks...)
				det.inferred = append(det.inferred, inference{"build.buildpacks", "[" + strings.Join(p.Buildpacks, ", ") + "]"})
			}
			if p.Stack != "" && pc.Build.Stack == "" {
				pc.Build.Stack = p.Stack
				det.inferred = append(det.inferred, inference{"build.stack", p.Stack})
			}
			if len(p.BuildEnv) > 0 {
				if pc.Build.Env == nil {
					pc.Build.Env = map[string]string{}
				}
				for _, k := range sortedKeys(p.BuildEnv) {
					if _, set := pc.Build.Env[k]; set {
						continue
					}
					pc.Build.Env[k] = p.BuildEnv[k]
					det.inferred = append(det.inferred, inference{k, p.BuildEnv[k]})
				}
			}
		}
		if len(p.Env) > 0 {
			if pc.Env == nil {
				pc.Env = map[string]string{}
			}
			for _, k := range sortedKeys(p.Env) {
				if _, set := pc.Env[k]; set {
					continue
				}
				pc.Env[k] = p.Env[k]
				det.inferred = append(det.inferred, inference{"env." + k, p.Env[k]})
			}
		}
		if p.HealthPath != "" && (p.HealthPath != railsHealthPath || railsRoutesHealth(dir)) {
			web := pc.Processes["web"]
			if web.HealthCheck == nil {
				if pc.Processes == nil {
					pc.Processes = map[string]projectProcess{}
				}
				web.HealthCheck = &shpyrdv1.HealthCheck{Path: p.HealthPath}
				pc.Processes["web"] = web
				det.inferred = append(det.inferred, inference{"processes.web.healthCheck.path", p.HealthPath})
				det.healthSet = true
			}
		}
	}
	return pc, det
}

// report prints what the profiles inferred, nothing when shpyrd.yaml
// already said it all.
func (d *detection) report(out io.Writer) {
	if d.empty() {
		return
	}
	fmt.Fprintf(out, "==> Detected %s (%s)\n", strings.Join(d.profiles, " + "), strings.Join(d.reasons, "; "))
	parts := make([]string, 0, len(d.inferred))
	for _, i := range d.inferred {
		parts = append(parts, i.what+"="+i.value)
	}
	fmt.Fprintf(out, "    %s\n", strings.Join(parts, ", "))
	fmt.Fprintln(out, "    (shpyrd.yaml values win; --save writes these there)")
}

// railsRoutesHealth says whether config/routes.rb keeps Rails' health
// endpoint (rails/health#show, present since 7.1).
func railsRoutesHealth(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "config/routes.rb"))
	return err == nil && strings.Contains(string(b), "rails/health#show")
}

type packageJSON struct {
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func (p *packageJSON) depends(name string) bool {
	_, a := p.Dependencies[name]
	_, b := p.DevDependencies[name]
	return a || b
}

func readPackageJSON(dir string) *packageJSON {
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil
	}
	var pkg packageJSON
	if err := json.Unmarshal(b, &pkg); err != nil {
		return nil
	}
	return &pkg
}

func fileExists(dir, rel string) bool {
	st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil && !st.IsDir()
}

func hasGlob(dir, pattern string) bool {
	m, _ := filepath.Glob(filepath.Join(dir, pattern))
	return len(m) > 0
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
