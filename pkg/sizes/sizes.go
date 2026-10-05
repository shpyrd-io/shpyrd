// Package sizes defines instance sizes: named CPU/memory allocations a
// process runs with, the equivalent of Fly machine sizes or Render instance
// types. The catalog is cluster-wide (ConfigMap shpyrd-system/shpyrd-sizes),
// seeded with defaults at install time and editable afterwards.
//
// Databases and stores have lists of their own, as Heroku's add-ons have
// their own plans (#57): the same names (shared-s ... dedicated-2xl), each
// list with its own memory, connections and default, so a Postgres
// shared-s is not a process shared-s.
//
// Two kinds exist:
//
//   - shared: cpu is the ceiling the process may use (the Kubernetes
//     limit); it is guaranteed a ShareFactor-th of it (the request) and
//     borrows the rest from idle neighbours, the way Fly's shared-cpu
//     machines and Heroku's standard dynos work. Kubernetes schedules by
//     requests, so this is what lets a node hold many small instances.
//   - dedicated: requests equal limits (Guaranteed QoS), whole cores.
//
// Memory is never overcommitted: request equals limit for both kinds.
package sizes

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"
)

// ConfigMap holding the catalog and its key.
const (
	ConfigMapName = "shpyrd-sizes"
	ConfigMapKey  = "sizes.yaml"
)

// Kinds of sizes.
const (
	Shared    = "shared"
	Dedicated = "dedicated"
)

// ShareFactor is the share of its CPU a shared size is guaranteed: the
// request is cpu/ShareFactor. 1/8 of half a core is the slice Fly
// guarantees a shared-cpu-1x machine.
const ShareFactor = 8

var nameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,30}[a-z0-9])?$`)

// Size is one catalog entry.
type Size struct {
	Name string `json:"name"`
	// Kind is shared or dedicated.
	Kind string `json:"kind"`
	// CPU in cores, e.g. "0.25", "500m", "2".
	CPU string `json:"cpu"`
	// Memory, e.g. "64Mi", "1Gi".
	Memory string `json:"memory"`
	// Description is free text for the dashboard.
	Description string `json:"description,omitempty"`
	// Connections is how many clients a database or store of this size
	// takes (PostgreSQL's max_connections, Redis's maxclients); 0 for a
	// process.
	Connections int `json:"connections,omitempty"`
}

// List is the sizes of one kind of thing and the one it gets when it
// names none.
type List struct {
	Default string `json:"default"`
	Sizes   []Size `json:"sizes"`
}

// Catalog is the cluster's sizes: the processes' list, and the lists of
// databases and stores.
type Catalog struct {
	List
	Postgres *List `json:"postgres,omitempty"`
	Redis    *List `json:"redis,omitempty"`
}

// What a list is for: processes, or the resource kind it sizes.
const (
	ForProcesses = ""
	ForPostgres  = "postgres"
	ForRedis     = "redis"
)

// PostgresMinMemory is the least a Postgres size has: under it PostgreSQL
// and CloudNativePG's instance manager do not fit in the pod.
const PostgresMinMemory = "128Mi"

// Defaults is the catalog seeded at install time. Memory steps follow Fly
// and Render from 64 MiB up: the smallest entry suits Go services and
// static sites, JVM and Node apps usually need shared-m or larger. Memory
// is billed as reserved while an instance is awake (RFC-0075), so the
// smallest size is also the smallest bill.
//
// A database or store gets the smallest size of its list unless it names
// one, as a process does. Postgres starts at 128Mi, where PostgreSQL is
// tuned for about 20 connections (#53); Redis follows the processes.
func Defaults() Catalog {
	return Catalog{
		List: List{
			Default: "shared-s",
			Sizes: []Size{
				{Name: "shared-s", Kind: Shared, CPU: "0.5", Memory: "64Mi", Description: "Default: static sites, Go services"},
				{Name: "shared-m", Kind: Shared, CPU: "0.5", Memory: "256Mi", Description: "Node.js, Python, Ruby"},
				{Name: "shared-l", Kind: Shared, CPU: "1", Memory: "512Mi", Description: "JVM, heavier web apps"},
				{Name: "shared-xl", Kind: Shared, CPU: "2", Memory: "1Gi"},
				{Name: "dedicated-s", Kind: Dedicated, CPU: "1", Memory: "1Gi", Description: "Guaranteed CPU"},
				{Name: "dedicated-m", Kind: Dedicated, CPU: "2", Memory: "4Gi"},
				{Name: "dedicated-l", Kind: Dedicated, CPU: "4", Memory: "8Gi"},
				{Name: "dedicated-xl", Kind: Dedicated, CPU: "8", Memory: "16Gi"},
				{Name: "dedicated-2xl", Kind: Dedicated, CPU: "16", Memory: "32Gi"},
			},
		},
		Postgres: &List{
			Default: "shared-s",
			Sizes: []Size{
				{Name: "shared-s", Kind: Shared, CPU: "0.5", Memory: "128Mi", Connections: 20, Description: "Default: a small app's database; not for reporting"},
				{Name: "shared-m", Kind: Shared, CPU: "0.5", Memory: "256Mi", Connections: 40},
				{Name: "shared-l", Kind: Shared, CPU: "1", Memory: "512Mi", Connections: 80},
				{Name: "shared-xl", Kind: Shared, CPU: "2", Memory: "1Gi", Connections: 120},
				{Name: "dedicated-s", Kind: Dedicated, CPU: "1", Memory: "1Gi", Connections: 120, Description: "Guaranteed CPU"},
				{Name: "dedicated-m", Kind: Dedicated, CPU: "2", Memory: "4Gi", Connections: 200},
				{Name: "dedicated-l", Kind: Dedicated, CPU: "4", Memory: "8Gi", Connections: 300},
				{Name: "dedicated-xl", Kind: Dedicated, CPU: "8", Memory: "16Gi", Connections: 400},
				{Name: "dedicated-2xl", Kind: Dedicated, CPU: "16", Memory: "32Gi", Connections: 500},
			},
		},
		Redis: &List{
			Default: "shared-s",
			Sizes: []Size{
				{Name: "shared-s", Kind: Shared, CPU: "0.5", Memory: "64Mi", Connections: 100, Description: "Default: a cache or a small queue"},
				{Name: "shared-m", Kind: Shared, CPU: "0.5", Memory: "256Mi", Connections: 400},
				{Name: "shared-l", Kind: Shared, CPU: "1", Memory: "512Mi", Connections: 1000},
				{Name: "shared-xl", Kind: Shared, CPU: "2", Memory: "1Gi", Connections: 2000},
				{Name: "dedicated-s", Kind: Dedicated, CPU: "1", Memory: "1Gi", Connections: 2000, Description: "Guaranteed CPU"},
				{Name: "dedicated-m", Kind: Dedicated, CPU: "2", Memory: "4Gi", Connections: 5000},
				{Name: "dedicated-l", Kind: Dedicated, CPU: "4", Memory: "8Gi", Connections: 10000},
			},
		},
	}
}

// Parse reads a catalog from its YAML form and validates it. A catalog
// saved before databases and stores had sizes of their own gets the
// built-in lists for them.
func Parse(data []byte) (*Catalog, error) {
	var c Catalog
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("sizes: %w", err)
	}
	d := Defaults()
	if c.Postgres == nil || len(c.Postgres.Sizes) == 0 {
		c.Postgres = d.Postgres
	}
	if c.Redis == nil || len(c.Redis.Sizes) == 0 {
		c.Redis = d.Redis
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// For is the list of sizes of processes (ForProcesses), databases
// (ForPostgres) or stores (ForRedis); nil for anything else.
func (c *Catalog) For(what string) *List {
	switch what {
	case ForProcesses:
		return &c.List
	case ForPostgres:
		return c.Postgres
	case ForRedis:
		return c.Redis
	}
	return nil
}

// Of names what a list is for in a sentence: "a process", "a Postgres".
func Of(what string) string {
	switch what {
	case ForPostgres:
		return "a Postgres database"
	case ForRedis:
		return "a Redis store"
	}
	return "a process"
}

// Marshal renders the catalog as YAML for the ConfigMap.
func (c Catalog) Marshal() ([]byte, error) {
	return yaml.Marshal(c)
}

// Validate checks the three lists.
func (c Catalog) Validate() error {
	for _, what := range []string{ForProcesses, ForPostgres, ForRedis} {
		l := c.For(what)
		if l == nil {
			return fmt.Errorf("sizes: the catalog has no sizes for %s", Of(what))
		}
		if err := l.Validate(what); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks names, kinds, quantities and the default of the sizes
// of what; a Postgres size has at least PostgresMinMemory.
func (l List) Validate(what string) error {
	if len(l.Sizes) == 0 {
		return fmt.Errorf("sizes: %s needs at least one size", Of(what))
	}
	seen := map[string]bool{}
	for _, s := range l.Sizes {
		if err := s.Validate(); err != nil {
			return err
		}
		if seen[s.Name] {
			return fmt.Errorf("sizes: duplicate size %q for %s", s.Name, Of(what))
		}
		seen[s.Name] = true
		if mem := resource.MustParse(s.Memory); what == ForPostgres && mem.Cmp(resource.MustParse(PostgresMinMemory)) < 0 {
			return fmt.Errorf("sizes: %s has %s of memory; a Postgres size has at least %s", s.Name, s.Memory, PostgresMinMemory)
		}
		if what == ForProcesses && s.Connections != 0 {
			return fmt.Errorf("sizes: %s: connections are for databases and stores, not processes", s.Name)
		}
	}
	if l.Default == "" {
		return fmt.Errorf("sizes: the default size for %s must be set", Of(what))
	}
	if !seen[l.Default] {
		return fmt.Errorf("sizes: default %q for %s is not one of its sizes", l.Default, Of(what))
	}
	return nil
}

// Validate checks one size.
func (s Size) Validate() error {
	if !nameRe.MatchString(s.Name) {
		return fmt.Errorf("sizes: invalid name %q (lowercase letters, digits, dashes)", s.Name)
	}
	if s.Kind != Shared && s.Kind != Dedicated {
		return fmt.Errorf("sizes: %s: kind must be %s or %s", s.Name, Shared, Dedicated)
	}
	cpu, err := resource.ParseQuantity(s.CPU)
	if err != nil || cpu.Sign() <= 0 {
		return fmt.Errorf("sizes: %s: invalid cpu %q", s.Name, s.CPU)
	}
	mem, err := resource.ParseQuantity(s.Memory)
	if err != nil || mem.Sign() <= 0 {
		return fmt.Errorf("sizes: %s: invalid memory %q", s.Name, s.Memory)
	}
	if s.Connections < 0 {
		return fmt.Errorf("sizes: %s: connections cannot be negative", s.Name)
	}
	return nil
}

// Get returns a size by name.
func (l List) Get(name string) (Size, bool) {
	for _, s := range l.Sizes {
		if s.Name == name {
			return s, true
		}
	}
	return Size{}, false
}

// Pick is the size named, or the default when name is ""; an unknown name
// is refused with the names there are.
func (l List) Pick(what, name string) (Size, error) {
	if name == "" {
		name = l.Default
	}
	if s, ok := l.Get(name); ok {
		return s, nil
	}
	names := make([]string, 0, len(l.Sizes))
	for _, s := range l.Sorted() {
		names = append(names, s.Name)
	}
	return Size{}, fmt.Errorf("unknown size %q for %s (sizes: %s)", name, Of(what), strings.Join(names, ", "))
}

// Sorted returns the sizes ordered by kind (shared first) then CPU then memory.
func (l List) Sorted() []Size {
	out := append([]Size(nil), l.Sizes...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == Shared
		}
		ci, cj := resource.MustParse(out[i].CPU), resource.MustParse(out[j].CPU)
		if ci.Cmp(cj) != 0 {
			return ci.Cmp(cj) < 0
		}
		mi, mj := resource.MustParse(out[i].Memory), resource.MustParse(out[j].Memory)
		return mi.Cmp(mj) < 0
	})
	return out
}

// Upsert adds or replaces a size.
func (l *List) Upsert(s Size) error {
	if err := s.Validate(); err != nil {
		return err
	}
	for i := range l.Sizes {
		if l.Sizes[i].Name == s.Name {
			l.Sizes[i] = s
			return nil
		}
	}
	l.Sizes = append(l.Sizes, s)
	return nil
}

// Remove deletes a size; the default cannot be removed.
func (l *List) Remove(name string) error {
	if name == l.Default {
		return fmt.Errorf("sizes: %q is the default size; pick another default first", name)
	}
	for i := range l.Sizes {
		if l.Sizes[i].Name == name {
			l.Sizes = append(l.Sizes[:i], l.Sizes[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("sizes: %q not found", name)
}

// Resources turns a size into Kubernetes requests and limits: the CPU is
// the limit for both kinds; a shared size requests a ShareFactor-th of it,
// a dedicated one all of it.
func (s Size) Resources() corev1.ResourceRequirements {
	cpu := resource.MustParse(s.CPU)
	mem := resource.MustParse(s.Memory)
	out := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: cpu, corev1.ResourceMemory: mem},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: cpu, corev1.ResourceMemory: mem},
	}
	if s.Kind != Dedicated {
		out.Requests[corev1.ResourceCPU] = SharedRequest(cpu)
	}
	return out
}

// SharedRequest is the CPU a shared process is guaranteed for a ceiling.
func SharedRequest(cpu resource.Quantity) resource.Quantity {
	return *resource.NewMilliQuantity(cpu.MilliValue()/ShareFactor, resource.DecimalSI)
}

// Resolve picks the resources for a process: an explicit override wins
// (missing halves are filled from the size), then the named size, then the
// catalog default. It returns the size name used ("" when fully explicit).
func (c Catalog) Resolve(sizeName string, override corev1.ResourceRequirements) (corev1.ResourceRequirements, string, error) {
	name := sizeName
	if name == "" {
		name = c.Default
	}
	size, ok := c.Get(name)
	if !ok {
		return corev1.ResourceRequirements{}, "", fmt.Errorf("unknown size %q: it is not in this platform's catalog of sizes", name)
	}
	base := size.Resources()
	if len(override.Requests) == 0 && len(override.Limits) == 0 {
		return base, size.Name, nil
	}
	// Explicit cpu/memory limits replace the size's; requests follow the
	// size's kind (dedicated: equal to limits; shared: the guaranteed share
	// of the new CPU ceiling).
	merged := base
	for k, v := range override.Limits {
		merged.Limits[k] = v
		if size.Kind == Dedicated || k == corev1.ResourceMemory {
			merged.Requests[k] = v
		} else {
			merged.Requests[k] = SharedRequest(v)
		}
	}
	for k, v := range override.Requests {
		merged.Requests[k] = v
	}
	return merged, "", nil
}
