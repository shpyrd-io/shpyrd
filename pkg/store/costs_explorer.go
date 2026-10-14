package store

import (
	"context"
	"encoding/json"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/ids"
)

// CostNames is display metadata only; grouping and filters always use stable IDs.
type CostNames struct {
	Workspaces map[string]string `json:"workspaces"`
	Projects   map[string]string `json:"projects"`
}

// CostExploreQuery uses UTC day boundaries. Empty filters mean all values;
// __none__ matches a missing dimension (or an unspecified sorting currency).
type CostExploreQuery struct {
	From, To                        time.Time
	Kind, Group                     string
	Workspace, Project, Category    string
	Workload, Process               string
	ResourceType, Resource, Service string
	Search, Currency, Sort          string
	Limit, Offset                   int
	Names                           CostNames
}
type CostGroupRow struct {
	Key           string             `json:"key"`
	Name          string             `json:"name"`
	Workspace     string             `json:"workspace,omitempty"`
	WorkspaceName string             `json:"workspaceName,omitempty"`
	Project       string             `json:"project,omitempty"`
	Slug          string             `json:"slug,omitempty"`
	Category      string             `json:"category,omitempty"`
	Workload      string             `json:"workload,omitempty"`
	Process       string             `json:"process,omitempty"`
	ResourceType  string             `json:"resourceType,omitempty"`
	Resource      string             `json:"resource,omitempty"`
	Service       string             `json:"service,omitempty"`
	Cost          map[string]float64 `json:"cost"`
	Lines         int64              `json:"lines"`
}
type CostPage struct {
	Rows       []CostGroupRow                `json:"rows"`
	Total      map[string]float64            `json:"total"`
	Categories map[string]map[string]float64 `json:"categories"`
	Count      int                           `json:"count"`
	Limit      int                           `json:"limit"`
	Offset     int                           `json:"offset"`
	Currency   string                        `json:"currency"`
	Currencies []string                      `json:"currencies"`
}
type CostCollectionStatus struct {
	Through     *time.Time `json:"through,omitempty"`
	CollectedAt *time.Time `json:"collectedAt,omitempty"`
}

func CostCategory(l CostLine) string {
	if l.Project != "" {
		return "project"
	}
	if l.Source == "opencost" && l.Process == "idle" {
		return "idle"
	}
	if l.Source == "opencost" && (l.Tags["cost.category"] == "system" || l.Process == "kube-system") {
		return "system"
	}
	return "unattributed"
}
func CostWorkload(l CostLine) string {
	if l.Metric == "storage" || l.Process == "volumes" {
		return "storage"
	}
	if strings.HasPrefix(l.Process, "postgres/") || strings.HasPrefix(l.Process, "redis/") {
		return "databases"
	}
	if l.Process == "build" {
		return "builds"
	}
	if l.Project != "" && l.Process != "" && l.Process != "other" {
		return "applications"
	}
	return "other"
}
func CostCategoryName(c string) string {
	switch c {
	case "project":
		return "Project-attributed"
	case "system":
		return "System services"
	case "idle":
		return "Idle capacity"
	default:
		return "Unattributed costs"
	}
}
func costName(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
func costDimension(l CostLine, q CostExploreQuery) CostGroupRow {
	c := CostCategory(l)
	r := CostGroupRow{Category: c}
	switch q.Group {
	case "resource":
		r.Resource, r.ResourceType = l.Resource, l.ResourceType
		r.Name = costName(l.Tags["cost.resourceName"], costName(l.Resource, "Unknown resource"))
		r.Category = ""
	case "resourceType":
		r.ResourceType = l.ResourceType
		r.Name = costName(l.ResourceType, "Unknown resource type")
		r.Category = ""
	case "service":
		r.Service = l.Service
		r.Name = costName(l.Service, "Unspecified provider service")
		r.Category = ""
	default:
		r.Workspace = l.Workspace
		r.WorkspaceName = costName(q.Names.Workspaces[l.Workspace], l.Workspace)
		r.Name = costName(r.WorkspaceName, CostCategoryName(c))
		if q.Group != "workspace" {
			r.Project = l.Project
			r.Slug = q.Names.Projects[l.Project]
			r.Name = costName(r.Slug, costName(r.Project, CostCategoryName(c)))
		}
		if q.Group == "workload" || q.Group == "process" {
			r.Workload = CostWorkload(l)
			r.Name = r.Workload
		}
		if q.Group == "process" {
			r.Process = l.Process
			r.Name = costName(l.Process, "Unspecified process")
		}
	}
	// Names are not identity: renaming a project/resource never merges groups.
	key, _ := json.Marshal([]string{r.Workspace, r.Project, r.Category, r.Workload, r.Process, r.ResourceType, r.Resource, r.Service})
	r.Key = string(key)
	return r
}
func costMatches(l CostLine, q CostExploreQuery) bool {
	if l.Start.Before(q.From) || !l.Start.Before(q.To) || l.Kind != q.Kind {
		return false
	}
	for _, p := range [][2]string{{q.Workspace, l.Workspace}, {q.Project, l.Project}, {q.Category, CostCategory(l)}, {q.Workload, CostWorkload(l)}, {q.Process, l.Process}, {q.ResourceType, l.ResourceType}, {q.Resource, l.Resource}, {q.Service, l.Service}} {
		if p[0] != "" && ((p[0] == "__none__" && p[1] != "") || (p[0] != "__none__" && p[0] != p[1])) {
			return false
		}
	}
	hay := strings.Join([]string{l.Workspace, l.Project, q.Names.Workspaces[l.Workspace], q.Names.Projects[l.Project], l.Process, l.Resource, l.ResourceType, l.Service, l.Tags["cost.resourceName"]}, " ")
	return strings.Contains(strings.ToLower(hay), strings.ToLower(q.Search))
}
func (m *Memory) ExploreCosts(_ context.Context, q CostExploreQuery) (CostPage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := CostPage{Rows: []CostGroupRow{}, Total: map[string]float64{}, Categories: map[string]map[string]float64{}, Limit: q.Limit, Offset: q.Offset, Currencies: []string{}}
	groups := map[string]*CostGroupRow{}
	resourceNames := map[string]string{}
	q.Names.Workspaces = maps.Clone(q.Names.Workspaces)
	q.Names.Projects = maps.Clone(q.Names.Projects)
	// The memory implementation resolves the same names as SQL, including deleted projects.
	if q.Names.Workspaces == nil {
		q.Names.Workspaces = map[string]string{}
	}
	if q.Names.Projects == nil {
		q.Names.Projects = map[string]string{}
	}
	for _, w := range m.workspaces {
		if q.Names.Workspaces[w.ID] == "" {
			q.Names.Workspaces[w.ID] = w.Name + " (" + w.Slug + ")"
		}
	}
	for _, p := range m.projects {
		if q.Names.Projects[ids.Short(p.ID)] == "" {
			q.Names.Projects[ids.Short(p.ID)] = p.Slug
		}
	}
	for _, l := range m.costs().lines {
		if !costMatches(l, q) {
			continue
		}
		r := costDimension(l, q)
		cur := groups[r.Key]
		if cur == nil {
			r.Cost = map[string]float64{}
			cur = &r
			groups[r.Key] = cur
		}
		// A deterministic resource name if older records carry a different name.
		if q.Group == "resource" && l.Tags["cost.resourceName"] > resourceNames[r.Key] {
			resourceNames[r.Key] = l.Tags["cost.resourceName"]
		}
		if r.Name > cur.Name {
			cur.Name = r.Name
		}
		cur.Lines++
		if l.Cost != nil {
			cur.Cost[l.Currency] += *l.Cost
			p.Total[l.Currency] += *l.Cost
			c := CostCategory(l)
			if p.Categories[c] == nil {
				p.Categories[c] = map[string]float64{}
			}
			p.Categories[c][l.Currency] += *l.Cost
		}
	}
	for c := range p.Total {
		p.Currencies = append(p.Currencies, c)
	}
	sort.Strings(p.Currencies)
	p.Currency = q.Currency
	if p.Currency == "__none__" {
		p.Currency = ""
	} else if p.Currency == "" && len(p.Currencies) > 0 {
		p.Currency = p.Currencies[0]
		if _, ok := p.Total["USD"]; ok {
			p.Currency = "USD"
		}
	}
	for _, r := range groups {
		if n := resourceNames[r.Key]; n != "" {
			r.Name = n
		}
		p.Rows = append(p.Rows, *r)
	}
	sort.Slice(p.Rows, func(i, j int) bool {
		a, b := p.Rows[i], p.Rows[j]
		if q.Sort == "name" {
			if a.Name != b.Name {
				return a.Name < b.Name
			}
		} else if a.Cost[p.Currency] != b.Cost[p.Currency] {
			if q.Sort == "cost_asc" {
				return a.Cost[p.Currency] < b.Cost[p.Currency]
			}
			return a.Cost[p.Currency] > b.Cost[p.Currency]
		}
		return a.Key < b.Key
	})
	p.Count = len(p.Rows)
	start := min(q.Offset, p.Count)
	end := min(start+q.Limit, p.Count)
	p.Rows = p.Rows[start:end]
	return p, nil
}
func (m *Memory) RecordCostCollection(_ context.Context, kind string, through, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.costs()
	if c.collections == nil {
		c.collections = map[string]CostCollectionStatus{}
	}
	c.collections[kind] = CostCollectionStatus{Through: &through, CollectedAt: &at}
	return nil
}
func (m *Memory) CostCollection(_ context.Context, kind string) (CostCollectionStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.costs().collections[kind], nil
}
