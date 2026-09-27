package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// writeTree lays out files under a temp dir: path -> content.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func inferredMap(det *detection) map[string]string {
	out := map[string]string{}
	for _, i := range det.inferred {
		out[i.what] = i.value
	}
	return out
}

// Each pattern the CLI recognises and what it fills in (RFC-0067).
func TestProfilesDetect(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  map[string]string // inferred what -> value
		names string
	}{
		{
			name:  "static site under public",
			files: map[string]string{"public/index.html": "<h1>hi</h1>"},
			want:  map[string]string{"build.buildpacks": "[web-servers]", "BP_WEB_SERVER": "nginx", "BP_WEB_SERVER_ROOT": "public"},
			names: "static site",
		},
		{
			name:  "public/index.html next to a language is not a static site",
			files: map[string]string{"public/index.html": "", "Gemfile": "gem 'sinatra'", "config.ru": "run App"},
			want:  map[string]string{"env.RACK_ENV": "production"},
			names: "Rack application",
		},
		{
			name:  "vite single-page app",
			files: map[string]string{"package.json": `{"scripts":{"build":"vite build"},"devDependencies":{"vite":"^7"}}`},
			want: map[string]string{"build.buildpacks": "[web-servers]", "BP_NODE_RUN_SCRIPTS": "build", "BP_WEB_SERVER": "nginx",
				"BP_WEB_SERVER_ROOT": "dist", "BP_WEB_SERVER_ENABLE_PUSH_STATE": "true"},
			names: "Vite single-page app",
		},
		{
			name:  "vite with a start script is a server, not a static site",
			files: map[string]string{"package.json": `{"scripts":{"build":"vite build","start":"node server.js"},"devDependencies":{"vite":"^7"}}`},
			want:  map[string]string{},
		},
		{
			name:  "next.js",
			files: map[string]string{"package.json": `{"scripts":{"build":"next build","start":"next start"},"dependencies":{"next":"^15"}}`},
			want:  map[string]string{"BP_NODE_RUN_SCRIPTS": "build", "NODE_ENV": "production"},
			names: "Next.js application",
		},
		{
			name:  "rack",
			files: map[string]string{"config.ru": "run App", "Gemfile": ""},
			want:  map[string]string{"env.RACK_ENV": "production"},
			names: "Rack application",
		},
		{
			name:  "rails with the health route",
			files: map[string]string{"config/application.rb": "", "config.ru": "", "config/routes.rb": `get "up" => "rails/health#show"`},
			want:  map[string]string{"env.RAILS_ENV": "production", "env.RAILS_LOG_TO_STDOUT": "1", "processes.web.healthCheck.path": "/up"},
			names: "Rails application",
		},
		{
			name:  "rails without the health route",
			files: map[string]string{"config/application.rb": "", "config.ru": "", "config/routes.rb": "root 'home#index'"},
			want:  map[string]string{"env.RAILS_ENV": "production", "env.RAILS_LOG_TO_STDOUT": "1"},
			names: "Rails application",
		},
		{
			name:  "php under public",
			files: map[string]string{"public/index.php": "<?php echo 1;"},
			want:  map[string]string{"BP_PHP_SERVER": "nginx", "BP_PHP_WEB_DIR": "public"},
			names: "PHP application",
		},
		{
			name:  "aptfile with libvips wants the full stack, on top of rack",
			files: map[string]string{"config.ru": "", "Aptfile": "# libs\nlibvips42\n"},
			want:  map[string]string{"env.RACK_ENV": "production", "build.stack": "full"},
			names: "Rack application + system packages with deep dependencies",
		},
		{
			name:  "aptfile with a light package changes nothing",
			files: map[string]string{"go.mod": "module x", "Aptfile": "jq\n"},
			want:  map[string]string{},
		},
		{
			name:  "a go service needs nothing",
			files: map[string]string{"go.mod": "module x", "main.go": "package main"},
			want:  map[string]string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeTree(t, tc.files)
			_, det := applyProfiles(nil, dir)
			got := inferredMap(det)
			if len(got) != len(tc.want) {
				t.Fatalf("inferred %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("%s = %q, want %q (all: %v)", k, got[k], v, got)
				}
			}
			if tc.names != "" && strings.Join(det.profiles, " + ") != tc.names {
				t.Errorf("profiles = %v, want %q", det.profiles, tc.names)
			}
		})
	}
}

// shpyrd.yaml wins over every inferred value; a Dockerfile build takes no
// buildpack hints; the report stays quiet when nothing was added.
func TestProfilesExplicitWins(t *testing.T) {
	dir := writeTree(t, map[string]string{"public/index.html": ""})
	pc := &projectConfig{Build: &projectBuild{Env: map[string]string{"BP_WEB_SERVER": "httpd"}}}
	pc, det := applyProfiles(pc, dir)
	if pc.Build.Env["BP_WEB_SERVER"] != "httpd" {
		t.Errorf("explicit BP_WEB_SERVER lost: %v", pc.Build.Env)
	}
	got := inferredMap(det)
	if _, ok := got["BP_WEB_SERVER"]; ok {
		t.Errorf("an explicit value must not be reported as inferred: %v", got)
	}
	if got["BP_WEB_SERVER_ROOT"] != "public" || got["build.buildpacks"] != "[web-servers]" {
		t.Errorf("the rest must still be filled in: %v", got)
	}

	// Rails with its own health check keeps it.
	dir = writeTree(t, map[string]string{"config/application.rb": "", "config/routes.rb": `get "up" => "rails/health#show"`})
	pc = &projectConfig{Processes: map[string]projectProcess{"web": {HealthCheck: &shpyrdv1.HealthCheck{Path: "/healthz"}}}, Env: map[string]string{"RAILS_ENV": "staging"}}
	pc, det = applyProfiles(pc, dir)
	if pc.Processes["web"].HealthCheck.Path != "/healthz" || pc.Env["RAILS_ENV"] != "staging" {
		t.Errorf("explicit health check or env lost: %+v %v", pc.Processes["web"].HealthCheck, pc.Env)
	}
	if got := inferredMap(det); len(got) != 1 || got["env.RAILS_LOG_TO_STDOUT"] != "1" {
		t.Errorf("inferred = %v, want only RAILS_LOG_TO_STDOUT", got)
	}

	// A Dockerfile build: runtime env still applies, buildpack hints do not.
	dir = writeTree(t, map[string]string{"config.ru": "", "public/index.php": "", "Dockerfile": "FROM ruby"})
	pc = &projectConfig{Build: &projectBuild{Strategy: shpyrdv1.StrategyDockerfile}}
	pc, det = applyProfiles(pc, dir)
	if got := inferredMap(det); len(got) != 1 || got["env.RACK_ENV"] != "production" {
		t.Errorf("dockerfile inferred = %v, want only RACK_ENV", got)
	}
	if len(pc.Build.Env) != 0 || len(pc.Build.Buildpacks) != 0 {
		t.Errorf("a Dockerfile build took buildpack hints: %+v", pc.Build)
	}

	// Nothing to add: nothing said.
	var sb strings.Builder
	dir = writeTree(t, map[string]string{"go.mod": "module x"})
	_, det = applyProfiles(nil, dir)
	det.report(&sb)
	if sb.Len() != 0 {
		t.Errorf("report for nothing = %q", sb.String())
	}
	dir = writeTree(t, map[string]string{"public/index.html": ""})
	_, det = applyProfiles(nil, dir)
	sb.Reset()
	det.report(&sb)
	if !strings.Contains(sb.String(), "Detected static site (public/index.html") || !strings.Contains(sb.String(), "BP_WEB_SERVER=nginx") {
		t.Errorf("report = %q", sb.String())
	}
}

// --save writes a new shpyrd.yaml, or adds the inferred keys to the
// existing one without touching its comments, order or values.
func TestSaveInferences(t *testing.T) {
	dir := writeTree(t, map[string]string{"public/index.html": ""})
	_, det := applyProfiles(nil, dir)
	path, created, err := saveInferences(dir, "my-site", det, false)
	if err != nil || !created {
		t.Fatalf("save: %v created=%v", err, created)
	}
	b, _ := os.ReadFile(path)
	var pc projectConfig
	if err := yaml.Unmarshal(b, &pc); err != nil {
		t.Fatalf("written file does not parse: %v\n%s", err, b)
	}
	if pc.Project != "my-site" || pc.Build == nil || pc.Build.Env["BP_WEB_SERVER"] != "nginx" || pc.Build.Env["BP_WEB_SERVER_ROOT"] != "public" || len(pc.Build.Buildpacks) != 1 || pc.Build.Buildpacks[0] != "web-servers" {
		t.Errorf("written config = %+v build=%+v\n%s", pc, pc.Build, b)
	}

	// An existing file: comments kept, explicit values kept, keys added.
	dir = writeTree(t, map[string]string{
		"config/application.rb": "",
		"config/routes.rb":      `get "up" => "rails/health#show"`,
		"shpyrd.yaml": `# our app
project: shop   # the slug
env:
  RAILS_ENV: staging   # not production yet
processes:
  web:
    size: shared-m
`,
	})
	existing, _ := loadProjectConfigAt(dir)
	_, det = applyProfiles(existing, dir)
	path, created, err = saveInferences(dir, "other", det, false)
	if err != nil || created {
		t.Fatalf("merge: %v created=%v", err, created)
	}
	b, _ = os.ReadFile(path)
	s := string(b)
	// Comments survive (the encoder normalises their spacing).
	for _, want := range []string{"# our app", "project: shop # the slug", "RAILS_ENV: staging # not production yet", "RAILS_LOG_TO_STDOUT: \"1\"", "size: shared-m", "healthCheck:", "path: /up"} {
		if !strings.Contains(s, want) {
			t.Errorf("merged file lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "project: other") {
		t.Errorf("project must not change unless asked:\n%s", s)
	}
	if err := yaml.Unmarshal(b, &pc); err != nil {
		t.Fatalf("merged file does not parse: %v\n%s", err, b)
	}
	if pc.Env["RAILS_ENV"] != "staging" || pc.Env["RAILS_LOG_TO_STDOUT"] != "1" || pc.Processes["web"].Size != "shared-m" || pc.Processes["web"].HealthCheck == nil || pc.Processes["web"].HealthCheck.Path != "/up" {
		t.Errorf("merged config = %+v", pc)
	}

	// projects create --save on a directory with a file: the project is set.
	_, _, err = saveInferences(dir, "other", det, true)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if !strings.Contains(string(b), "project: other") {
		t.Errorf("setProject must replace the project:\n%s", b)
	}

	// Saving again adds nothing twice.
	before, _ := os.ReadFile(path)
	if _, _, err := saveInferences(dir, "other", det, true); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Errorf("second save changed the file:\n%s\n---\n%s", before, after)
	}
}
