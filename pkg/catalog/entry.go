// Package catalog is the catalog of applications a project can be made
// from: an agent, a CRM, any open-source project that runs on the
// platform. An entry is a folder in the catalog repository, read from its
// shpyrd.yaml: the keys of a project's own shpyrd.yaml (processes, build,
// env) under a `catalog` block that says what the entry is, where its
// code lives, and what it needs before it runs. The repository's CI
// publishes the entries as an index over HTTPS (Publish); the server reads
// the index (Fetcher) and installs an entry as a project.
//
// An entry's source is the upstream repository, followed on a branch or
// pinned to a tag: the project auto-deploys when upstream moves, as any
// git project does. The files beside the entry's shpyrd.yaml (a
// Dockerfile, a config file) are an overlay, laid over the checkout before
// the build; an overlay goes with a Dockerfile build, since the buildpacks
// builder reads one source.
package catalog

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/configvars"
	"github.com/shpyrd-io/shpyrd/pkg/project"
)

// EntryFile is the file an entry is read from, in its folder.
const EntryFile = "shpyrd.yaml"

// Entry is one application of the catalog, as its shpyrd.yaml says it.
type Entry struct {
	Catalog Meta `json:"catalog"`
	// Processes, Build and Env are the keys of a project's shpyrd.yaml,
	// with the same meaning: the entry says how the upstream code runs.
	Processes map[string]Process `json:"processes,omitempty"`
	Build     *Build             `json:"build,omitempty"`
	Env       map[string]string  `json:"env,omitempty"`
}

// Meta is the `catalog` block: what the entry is and what it needs.
type Meta struct {
	// Name is the application's name as people read it ("Fluxyr");
	// Description the one line under it, as the launcher shows one.
	Name        string `json:"name"`
	Description string `json:"description"`
	// Category groups the entries (agents, crm, automation); Tags are
	// words a search finds the entry by.
	Category string   `json:"category"`
	Tags     []string `json:"tags,omitempty"`
	// Icon is the symbol of the project's card, by the name lucide gives
	// it, and Color one of the colours the cards take.
	Icon  string `json:"icon,omitempty"`
	Color string `json:"color,omitempty"`
	// Publisher is who maintains the entry; License the upstream
	// project's; Homepage where to read about it.
	Publisher string `json:"publisher,omitempty"`
	License   string `json:"license,omitempty"`
	Homepage  string `json:"homepage,omitempty"`
	// Source is the upstream code.
	Source Source `json:"source"`
	// Inputs are what the person is asked at install: a key, a model, a
	// choice. Their values become the project's config vars.
	Inputs []Input `json:"inputs,omitempty"`
	// Volumes are the disks the entry needs, mounted by its processes.
	Volumes []Volume `json:"volumes,omitempty"`
	// Resources are the databases and stores the entry needs, created in
	// the project and attached to it.
	Resources []Resource `json:"resources,omitempty"`
}

// Source is where the entry's code is: a git repository, followed on a
// branch (revision "main" when unset) or pinned to a tag, and the folder
// in it when the application is not at the root.
type Source struct {
	Git      string `json:"git"`
	Revision string `json:"revision,omitempty"`
	SubPath  string `json:"subPath,omitempty"`
}

// Input is one thing the person gives at install.
type Input struct {
	// Name is the config var the value is set as.
	Name string `json:"name"`
	// Label is what the form calls it; Help a sentence under it, where
	// to get the value from.
	Label string `json:"label"`
	Help  string `json:"help,omitempty"`
	// Secret keeps the value out of every listing (a key); Required
	// refuses an install without it; Default fills the field.
	Secret   bool   `json:"secret,omitempty"`
	Required bool   `json:"required,omitempty"`
	Default  string `json:"default,omitempty"`
	// Generate makes the value at install, that many random bytes as hex
	// (an encryption key the application must keep): nothing is asked.
	Generate int `json:"generate,omitempty"`
}

// Volume is a disk the entry needs: created at install, mounted where a
// process says.
type Volume struct {
	Name string `json:"name"`
	Size string `json:"size"`
}

// Resource is a database or store the entry needs: created in the
// project at install, and attached to it, so its variables reach the
// processes.
type Resource struct {
	// Kind as the resources API names it: postgres, redis, objectbucket.
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Spec is what the resource is created with, as the resources API
	// takes it (a size, a version); empty takes the defaults.
	Spec map[string]interface{} `json:"spec,omitempty"`
}

// Process is a process of the entry, with the keys of a project's
// shpyrd.yaml.
type Process struct {
	Port     *int32                 `json:"port,omitempty"`
	Replicas *int32                 `json:"replicas,omitempty"`
	Command  []string               `json:"command,omitempty"`
	Args     []string               `json:"args,omitempty"`
	Volumes  []shpyrdv1.VolumeMount `json:"volumes,omitempty"`
	Size     string                 `json:"size,omitempty"`
	// CPU and Memory override the size's limits; prefer a size.
	CPU         string                `json:"cpu,omitempty"`
	Memory      string                `json:"memory,omitempty"`
	HealthCheck *shpyrdv1.HealthCheck `json:"healthCheck,omitempty"`
}

// Build is the `build` key of a project's shpyrd.yaml.
type Build struct {
	Strategy   string            `json:"strategy,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Builder    string            `json:"builder,omitempty"`
	Dockerfile string            `json:"dockerfile,omitempty"`
	Target     string            `json:"target,omitempty"`
	Buildpacks []string          `json:"buildpacks,omitempty"`
	Stack      string            `json:"stack,omitempty"`
	Workspace  string            `json:"workspace,omitempty"`
}

var (
	nameRe     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)
	categoryRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	iconRe     = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	volumeRe   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)
)

// Parse reads an entry's shpyrd.yaml.
func Parse(b []byte) (*Entry, error) {
	var e Entry
	if err := yaml.UnmarshalStrict(b, &e); err != nil {
		return nil, fmt.Errorf("%s: %w", EntryFile, err)
	}
	return &e, nil
}

// ValidName says a string may name an entry: lowercase letters, digits
// and dashes, 40 at most, as a project's slug.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// Validate checks an entry named name, with the files beside its
// shpyrd.yaml (the overlay) given by name, and says what is wrong in
// words.
func (e *Entry) Validate(name string, overlay []string) error {
	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }
	if !ValidName(name) {
		add("the folder %q must be lowercase letters, digits and dashes (40 at most), as a project's slug", name)
	}
	m := &e.Catalog
	if strings.TrimSpace(m.Name) == "" {
		add("catalog.name is missing: the application's name, as people read it")
	} else if len(m.Name) > 60 {
		add("catalog.name is longer than 60 characters")
	}
	if strings.TrimSpace(m.Description) == "" {
		add("catalog.description is missing: one line that says what the application is")
	} else if len(m.Description) > 200 {
		add("catalog.description is longer than 200 characters")
	}
	if m.Category == "" {
		add("catalog.category is missing: the group the entry is listed under (agents, crm, automation)")
	} else if !categoryRe.MatchString(m.Category) {
		add("catalog.category %q must be lowercase words joined by dashes", m.Category)
	}
	for _, t := range m.Tags {
		if !categoryRe.MatchString(t) {
			add("catalog.tags: %q must be lowercase words joined by dashes", t)
		}
	}
	if m.Icon != "" && (len(m.Icon) > 40 || !iconRe.MatchString(m.Icon)) {
		add("catalog.icon %q is not the name of a symbol (lowercase words joined by dashes, like bot or briefcase)", m.Icon)
	}
	if m.Color != "" && !slices.Contains(project.IconColors, m.Color) {
		add("catalog.color %q is not one of %s", m.Color, strings.Join(project.IconColors, ", "))
	}
	if m.Homepage != "" && !httpURL(m.Homepage) {
		add("catalog.homepage must be an http(s) address")
	}
	switch {
	case m.Source.Git == "":
		add("catalog.source.git is missing: the repository the application is built from")
	case !httpURL(m.Source.Git):
		add("catalog.source.git must be an http(s) address of a repository")
	}
	if strings.ContainsAny(m.Source.Revision, " \t\n") {
		add("catalog.source.revision %q is not a branch, tag or commit", m.Source.Revision)
	}
	if strings.HasPrefix(m.Source.SubPath, "/") || strings.Contains(m.Source.SubPath, "..") {
		add("catalog.source.subPath must be a folder inside the repository")
	}
	seen := map[string]bool{}
	for i, in := range m.Inputs {
		at := fmt.Sprintf("catalog.inputs[%d]", i)
		if in.Name == "" {
			add("%s: name is missing: the config var the value is set as", at)
		} else if err := configvars.ValidateKey(in.Name); err != nil {
			add("%s: %v", at, err)
		} else if seen[in.Name] {
			add("%s: %s is asked twice", at, in.Name)
		}
		seen[in.Name] = true
		if in.Generate > 0 {
			if in.Default != "" || in.Required {
				add("%s: a generated value takes no default and is never required: it is made at install", at)
			}
			if in.Generate > 256 {
				add("%s: generate is in bytes, 256 at most", at)
			}
			continue
		}
		if strings.TrimSpace(in.Label) == "" {
			add("%s: label is missing: what the form calls it", at)
		}
		if in.Required && in.Default != "" {
			add("%s: required and default together: a default is an answer, so the input is not required", at)
		}
	}
	for k := range e.Env {
		if err := configvars.ValidateKey(k); err != nil {
			add("env: %v", err)
		}
		if seen[k] {
			add("env: %s is both an input and a variable of env", k)
		}
	}
	volumes := map[string]bool{}
	for i, v := range m.Volumes {
		at := fmt.Sprintf("catalog.volumes[%d]", i)
		if !volumeRe.MatchString(v.Name) {
			add("%s: name %q must be lowercase letters, digits and dashes", at, v.Name)
		}
		if volumes[v.Name] {
			add("%s: %s is declared twice", at, v.Name)
		}
		volumes[v.Name] = true
		if q, err := resource.ParseQuantity(v.Size); err != nil || q.Sign() <= 0 {
			add("%s: size %q is not a size (5Gi)", at, v.Size)
		}
	}
	mounted := map[string]bool{}
	for name, p := range e.Processes {
		if name == "" || !categoryRe.MatchString(name) {
			add("processes: %q is not a process name (web, worker)", name)
		}
		if p.Port != nil && (*p.Port < 1 || *p.Port > 65535) {
			add("processes.%s.port %d is not a port", name, *p.Port)
		}
		if p.CPU != "" {
			if _, err := resource.ParseQuantity(p.CPU); err != nil {
				add("processes.%s.cpu %q: %v", name, p.CPU, err)
			}
		}
		if p.Memory != "" {
			if _, err := resource.ParseQuantity(p.Memory); err != nil {
				add("processes.%s.memory %q: %v", name, p.Memory, err)
			}
		}
		for _, mnt := range p.Volumes {
			if mnt.Name == "" || mnt.Path == "" {
				add("processes.%s.volumes: a mount needs name and path", name)
				continue
			}
			if !volumes[mnt.Name] {
				add("processes.%s mounts volume %q, which catalog.volumes does not declare", name, mnt.Name)
			}
			if !strings.HasPrefix(mnt.Path, "/") {
				add("processes.%s.volumes: path %q must be absolute", name, mnt.Path)
			}
			mounted[mnt.Name] = true
		}
	}
	for _, v := range m.Volumes {
		if !mounted[v.Name] {
			add("catalog.volumes: %s is declared and no process mounts it", v.Name)
		}
	}
	resources := map[string]bool{}
	for i, r := range m.Resources {
		at := fmt.Sprintf("catalog.resources[%d]", i)
		if !categoryRe.MatchString(r.Kind) {
			add("%s: kind %q must be lowercase (postgres, redis, objectbucket)", at, r.Kind)
		}
		if !volumeRe.MatchString(r.Name) {
			add("%s: name %q must be lowercase letters, digits and dashes", at, r.Name)
		}
		if resources[r.Kind+"/"+r.Name] {
			add("%s: %s %s is declared twice", at, r.Kind, r.Name)
		}
		resources[r.Kind+"/"+r.Name] = true
	}
	strategy := ""
	if e.Build != nil {
		strategy = e.Build.Strategy
		switch strategy {
		case "", shpyrdv1.StrategyBuildpacks, shpyrdv1.StrategyDockerfile:
		default:
			add("build.strategy must be buildpacks or dockerfile, got %q", strategy)
		}
		if strategy == "" && (e.Build.Dockerfile != "" || e.Build.Target != "") {
			strategy = shpyrdv1.StrategyDockerfile
		}
		for k := range e.Build.Env {
			if err := configvars.ValidateKey(k); err != nil {
				add("build.env: %v", err)
			}
		}
	}
	if len(overlay) > 0 && strategy != shpyrdv1.StrategyDockerfile {
		sort.Strings(overlay)
		add("the files beside %s (%s) are laid over the checkout before the build, and that goes with a Dockerfile build: say build.strategy: dockerfile, or remove them", EntryFile, strings.Join(overlay, ", "))
	}
	if len(errs) == 0 {
		return nil
	}
	return &ValidationError{Name: name, Problems: errs}
}

// ValidationError is what Validate found wrong, one line each.
type ValidationError struct {
	Name     string
	Problems []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("entry %s: %s", e.Name, strings.Join(e.Problems, "; "))
}

func httpURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// Revision is the revision the entry follows: "main" when unset.
func (e *Entry) Revision() string {
	if e.Catalog.Source.Revision == "" {
		return "main"
	}
	return e.Catalog.Source.Revision
}

// Spec is what the entry writes into a project: its processes, build
// settings and plain variables, as the CLI writes a shpyrd.yaml's.
func (e *Entry) Spec() (processes map[string]shpyrdv1.Process, build *shpyrdv1.Build, env []corev1.EnvVar, err error) {
	if len(e.Processes) > 0 {
		processes = map[string]shpyrdv1.Process{}
		for name, p := range e.Processes {
			res := corev1.ResourceRequirements{}
			if p.CPU != "" || p.Memory != "" {
				res.Limits = corev1.ResourceList{}
				if p.CPU != "" {
					q, err := resource.ParseQuantity(p.CPU)
					if err != nil {
						return nil, nil, nil, fmt.Errorf("process %s: cpu %q: %w", name, p.CPU, err)
					}
					res.Limits[corev1.ResourceCPU] = q
				}
				if p.Memory != "" {
					q, err := resource.ParseQuantity(p.Memory)
					if err != nil {
						return nil, nil, nil, fmt.Errorf("process %s: memory %q: %w", name, p.Memory, err)
					}
					res.Limits[corev1.ResourceMemory] = q
				}
			}
			processes[name] = shpyrdv1.Process{Replicas: p.Replicas, Port: p.Port, Command: p.Command, Args: p.Args, Size: p.Size, Resources: res, Volumes: p.Volumes, HealthCheck: p.HealthCheck}
		}
	}
	if b := e.Build; b != nil {
		build = &shpyrdv1.Build{Strategy: b.Strategy, Builder: b.Builder, Dockerfile: b.Dockerfile, Target: b.Target, Buildpacks: b.Buildpacks, Stack: b.Stack, Workspace: b.Workspace}
		if build.Strategy == "" && (b.Dockerfile != "" || b.Target != "") {
			build.Strategy = shpyrdv1.StrategyDockerfile
		}
		for _, k := range sortedKeys(b.Env) {
			build.Env = append(build.Env, corev1.EnvVar{Name: k, Value: b.Env[k]})
		}
	}
	for _, k := range sortedKeys(e.Env) {
		env = append(env, corev1.EnvVar{Name: k, Value: e.Env[k]})
	}
	return processes, build, env, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
