package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/ids"
)

func TestCostExplorer(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			st := open(t)
			ctx := context.Background()
			day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			q := CostExploreQuery{From: day, To: day.Add(24 * time.Hour), Kind: CostEstimated, Group: "project", Limit: 50, Names: CostNames{Projects: map[string]string{}, Workspaces: map[string]string{"w1": "Production", "w2": "Staging"}}}
			lines := []CostLine{}
			add := func(ws, project, process, metric, currency string, cost float64) {
				v := cost
				lines = append(lines, CostLine{Kind: CostEstimated, Source: "opencost", Start: day, End: day.Add(time.Hour), Workspace: ws, Project: project, Process: process, Metric: metric, Currency: currency, Cost: &v, Resource: "node1", ResourceType: "node"})
			}
			for i := 0; i < 221; i++ {
				id := fmt.Sprintf("p%03d", i)
				q.Names.Projects[id] = "shop"
				add("w1", id, "web", "cpu", "USD", float64(i+1))
			}
			add("w2", "p000", "web", "cpu", "USD", 4)
			add("", "", "idle", "cpu", "USD", 10)
			add("", "", "kube-system", "cpu", "USD", 5)
			add("w2", "", "unknown", "cpu", "USD", 2)
			add("w1", "p000", "web", "memory", "BRL", 1000)
			if _, err := st.UpsertCostLines(ctx, lines); err != nil {
				t.Fatal(err)
			}
			q.Currency = "USD"
			p, err := st.ExploreCosts(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if p.Count != 225 || len(p.Rows) != 50 || p.Rows[0].Project != "p220" || p.Total["BRL"] != 1000 || p.Total["USD"] != 24552 {
				t.Fatalf("bad first page: %+v", p)
			}
			if p.Categories["idle"]["USD"] != 10 || p.Categories["system"]["USD"] != 5 || p.Categories["unattributed"]["USD"] != 2 {
				t.Fatalf("categories: %+v", p.Categories)
			}
			q.Offset = 50
			p2, err := st.ExploreCosts(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if p2.Total["USD"] != p.Total["USD"] || p2.Count != p.Count || p2.Rows[0].Key == p.Rows[0].Key {
				t.Fatal("pagination changes totals or repeats first row")
			}
			q.Offset = 0
			q.Search = "Staging"
			p, err = st.ExploreCosts(ctx, q)
			if err != nil || p.Count != 2 || p.Total["USD"] != 6 {
				t.Fatalf("workspace search: %+v %v", p, err)
			}
			q.Search = ""
			q.Workspace = "w2"
			q.Project = "__none__"
			p, err = st.ExploreCosts(ctx, q)
			if err != nil || p.Count != 1 || p.Rows[0].Category != "unattributed" || p.Rows[0].Workspace != "w2" {
				t.Fatalf("workspace without project: %+v %v", p, err)
			}
			q.Workspace = "w1"
			q.Project = "p000"
			q.Group = "process"
			p, err = st.ExploreCosts(ctx, q)
			if err != nil || p.Count != 1 || p.Rows[0].Lines != 2 {
				t.Fatalf("drilldown: %+v %v", p, err)
			}
			// A correction changes amount, currency, resource type and display name;
			// the daily aggregate must subtract the old dimensions and keep record count.
			corrected := lines[0]
			amount := 9.0
			corrected.Cost = &amount
			corrected.Currency = "EUR"
			corrected.ResourceType = "instance"
			corrected.Tags = map[string]string{"cost.resourceName": "worker-a"}
			if _, err = st.UpsertCostLines(ctx, []CostLine{corrected}); err != nil {
				t.Fatal(err)
			}
			p, err = st.ExploreCosts(ctx, q)
			if err != nil || p.Total["EUR"] != 9 || p.Total["USD"] != 0 || p.Rows[0].Lines != 2 {
				t.Fatalf("correction: %+v %v", p, err)
			}
			q.Group = "resource"
			q.Search = "worker-a"
			p, err = st.ExploreCosts(ctx, q)
			if err != nil || p.Count != 1 || p.Rows[0].Name != "worker-a" {
				t.Fatalf("resource name: %+v %v", p, err)
			}
			if err = st.RecordCostCollection(ctx, CostEstimated, day.Add(time.Hour), day.Add(2*time.Hour)); err != nil {
				t.Fatal(err)
			}
			status, err := st.CostCollection(ctx, CostEstimated)
			if err != nil || status.Through == nil || !status.Through.Equal(day.Add(time.Hour)) {
				t.Fatalf("status: %+v %v", status, err)
			}
			q.Project = ""
			q.Workspace = ""
			q.Search = ""
			q.Group = "project"
			q.Offset = 50
			job, err := st.CreateCostExport(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if ok, err := st.BuildCostExport(ctx); !ok || err != nil {
				t.Fatalf("export: %v %v", ok, err)
			}
			job, err = st.CostExport(ctx, job.ID)
			if err != nil || job.Status != "ready" {
				t.Fatalf("export status: %+v %v", job, err)
			}
			csv := ""
			for seq := 0; ; seq++ {
				b, e := st.CostExportChunk(ctx, job.ID, seq)
				if e == ErrNotFound {
					break
				}
				if e != nil {
					t.Fatal(e)
				}
				csv += string(b)
			}
			if !strings.Contains(csv, "Production") || !strings.Contains(csv, "EUR") || strings.Count(csv, "\n") != 227 {
				t.Fatalf("export rows %d", strings.Count(csv, "\n"))
			}

		})
	}
}

func TestCostClassificationIsConservative(t *testing.T) {
	cases := []struct {
		line     CostLine
		category string
	}{{CostLine{Process: "kube-system", Source: "oci"}, "unattributed"}, {CostLine{Process: "idle", Source: "oci"}, "unattributed"}, {CostLine{Process: "custom-system", Source: "opencost", Tags: map[string]string{"cost.category": "system"}}, "system"}, {CostLine{Project: "project", Process: "idle", Source: "opencost"}, "project"}}
	for _, c := range cases {
		if got := CostCategory(c.line); got != c.category {
			t.Errorf("category %q, want %q", got, c.category)
		}
	}
	if costCSV("  =1+1") != "'  =1+1" || costCSV("hello") != "hello" {
		t.Fatal("CSV formula escaping")
	}
}

func TestCostExplorerResolvesRenamedAndDeletedProjects(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			st := open(t)
			ctx := context.Background()
			w, _ := st.Workspace(ctx, DefaultWorkspace)
			project := Project{ID: newID(), WorkspaceID: w.ID, Slug: "shop"}
			if _, err := st.UpsertProject(ctx, project); err != nil {
				t.Fatal(err)
			}
			day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			amount := 1.0
			line := CostLine{Kind: CostEstimated, Source: "opencost", Start: day, End: day.Add(time.Hour), Workspace: w.ID, Project: ids.Short(project.ID), Process: "web", Cost: &amount, Currency: "USD"}
			if _, err := st.UpsertCostLines(ctx, []CostLine{line}); err != nil {
				t.Fatal(err)
			}
			project.Slug = "renamed"
			if _, err := st.UpsertProject(ctx, project); err != nil {
				t.Fatal(err)
			}
			if err := st.DeleteProject(ctx, project.ID); err != nil {
				t.Fatal(err)
			}
			q := CostExploreQuery{From: day, To: day.Add(24 * time.Hour), Kind: CostEstimated, Group: "project", Search: "renamed", Limit: 50}
			page, err := st.ExploreCosts(ctx, q)
			if err != nil || page.Count != 1 || page.Rows[0].Name != "renamed" || !strings.Contains(page.Rows[0].WorkspaceName, w.Name) {
				t.Fatalf("names: %+v %v", page, err)
			}
			if changed, err := st.UpsertCostLines(ctx, []CostLine{line}); err != nil || changed != 0 {
				t.Fatalf("identical reread: %d %v", changed, err)
			}
		})
	}
}

func TestCostExplorerMigrationBackfillsHistoricalRecords(t *testing.T) {
	open := implementations(t)["postgres"]
	if open == nil {
		t.Skip("SHPYRD_TEST_DATABASE_URL is not set")
	}
	p := open(t).(*Postgres)
	ctx := context.Background()
	down, _ := migrationFiles.ReadFile("migrations/000027_cost_explorer.down.sql")
	if _, err := p.pool.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	amount := 3.0
	lines := []CostLine{{Kind: CostEstimated, Source: "opencost", Start: day, End: day.Add(time.Hour), Process: "idle", Cost: &amount, Currency: "USD"}, {Kind: CostEstimated, Source: "opencost", Start: day, End: day.Add(time.Hour), Process: "custom-system-without-evidence", Cost: &amount, Currency: "USD"}}
	if _, err := p.UpsertCostLines(ctx, lines); err != nil {
		t.Fatal(err)
	}
	up, _ := migrationFiles.ReadFile("migrations/000027_cost_explorer.up.sql")
	if _, err := p.pool.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	page, err := p.ExploreCosts(ctx, CostExploreQuery{From: day, To: day.Add(24 * time.Hour), Kind: CostEstimated, Group: "project", Limit: 50})
	if err != nil || page.Total["USD"] != 6 || page.Categories["idle"]["USD"] != 3 || page.Categories["unattributed"]["USD"] != 3 {
		t.Fatalf("backfill: %+v %v", page, err)
	}
	if _, err := p.pool.Exec(ctx, `DELETE FROM cost_lines WHERE process='idle'`); err != nil {
		t.Fatal(err)
	}
	page, err = p.ExploreCosts(ctx, CostExploreQuery{From: day, To: day.Add(24 * time.Hour), Kind: CostEstimated, Group: "project", Limit: 50})
	if err != nil || page.Total["USD"] != 3 || page.Count != 1 {
		t.Fatalf("delete correction: %+v %v", page, err)
	}
}
