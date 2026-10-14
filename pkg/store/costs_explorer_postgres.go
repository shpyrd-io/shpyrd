package store

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

// ExploreCosts groups in PostgreSQL over daily aggregates, never loading raw
// records. One statement gives rows, count and totals from the same snapshot.
func (p *Postgres) ExploreCosts(ctx context.Context, q CostExploreQuery) (CostPage, error) {
	return exploreCostsSQL(ctx, p.pool, q)
}

type costQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func exploreCostsSQL(ctx context.Context, db costQueryer, q CostExploreQuery) (CostPage, error) {
	names, err := json.Marshal(q.Names)
	if err != nil {
		return CostPage{}, err
	}
	args := []any{q.From.UTC(), q.To.UTC(), q.Kind, names}
	where := `day >= ($1::timestamptz AT TIME ZONE 'UTC')::date AND day < ($2::timestamptz AT TIME ZONE 'UTC')::date AND kind=$3 AND records>0`
	for _, f := range [][2]string{{"workspace_id", q.Workspace}, {"project", q.Project}, {"category", q.Category}, {"workload", q.Workload}, {"process", q.Process}, {"resource_type", q.ResourceType}, {"resource", q.Resource}, {"service", q.Service}} {
		if f[1] != "" {
			value := f[1]
			if value == "__none__" {
				value = ""
			}
			args = append(args, value)
			where += fmt.Sprintf(" AND %s=$%d", f[0], len(args))
		}
	}
	if q.Search != "" {
		args = append(args, q.Search)
		where += fmt.Sprintf(` AND strpos(lower(concat_ws(' ',workspace_id,project,workspace_name,project_slug,process,resource,resource_type,resource_name,service)),lower($%d))>0`, len(args))
	}
	// Only these fixed expressions become SQL; all user values are parameters.
	dims := map[string]string{"workspace": "''", "project": "''", "category": "''", "workload": "''", "process": "''", "resourceType": "''", "resource": "''", "service": "''"}
	name := `''`
	categoryName := `CASE category WHEN 'project' THEN 'Project-attributed' WHEN 'system' THEN 'System services' WHEN 'idle' THEN 'Idle capacity' ELSE 'Unattributed costs' END`
	switch q.Group {
	case "resource":
		dims["resource"], dims["resourceType"] = "resource", "resource_type"
		name = `nullif(resource_name,'')`
	case "resourceType":
		dims["resourceType"] = "resource_type"
		name = `coalesce(nullif(resource_type,''),'Unknown resource type')`
	case "service":
		dims["service"] = "service"
		name = `coalesce(nullif(service,''),'Unspecified provider service')`
	default:
		dims["workspace"], dims["category"] = "workspace_id", "category"
		name = `coalesce(nullif(workspace_name,''),nullif(workspace_id,''),` + categoryName + `)`
		if q.Group != "workspace" {
			dims["project"] = "project"
			name = `coalesce(nullif(project_slug,''),nullif(project,''),` + categoryName + `)`
		}
		if q.Group == "workload" || q.Group == "process" {
			dims["workload"] = "workload"
			name = "workload"
		}
		if q.Group == "process" {
			dims["process"] = "process"
			name = `coalesce(nullif(process,''),'Unspecified process')`
		}
	}
	columns := []string{"workspace", "project", "category", "workload", "process", "resourceType", "resource", "service"}
	dimJSON := "jsonb_build_object("
	keyJSON := "jsonb_build_array("
	for i, col := range columns {
		if i > 0 {
			dimJSON += ","
			keyJSON += ","
		}
		dimJSON += "'" + col + "'," + dims[col]
		keyJSON += dims[col]
	}
	if dims["workspace"] != "''" {
		dimJSON += ",'workspaceName',workspace_name,'slug',"
		if dims["project"] != "''" {
			dimJSON += "project_slug"
		} else {
			dimJSON += "''"
		}
	}
	dimJSON += ")"
	keyJSON += ")::text"
	args = append(args, q.Currency)
	currencyArg := fmt.Sprintf("$%d::text", len(args))
	args = append(args, q.Limit, q.Offset)
	limitArg, offsetArg := fmt.Sprintf("$%d", len(args)-1), fmt.Sprintf("$%d", len(args))
	order := `coalesce((cost->>(SELECT currency FROM chosen))::numeric,0) DESC,key`
	if q.Sort == "cost_asc" {
		order = `coalesce((cost->>(SELECT currency FROM chosen))::numeric,0) ASC,key`
	} else if q.Sort == "name" {
		order = `name COLLATE "C",key`
	}
	sql := `WITH enriched AS NOT MATERIALIZED (
 SELECT d.*,coalesce($4::jsonb->'workspaces'->>d.workspace_id,w.name || ' (' || w.slug || ')',d.workspace_id) workspace_name,
 coalesce($4::jsonb->'projects'->>d.project,p.slug,'') project_slug
 FROM cost_daily d LEFT JOIN workspaces w ON w.id::text=d.workspace_id
 LEFT JOIN projects p ON cost_project_short(p.id)=d.project
 ), filtered AS MATERIALIZED (SELECT * FROM enriched WHERE ` + where + `),
 totals AS (SELECT currency,sum(amount) amount FROM filtered WHERE priced_records>0 GROUP BY currency),
 chosen AS (SELECT CASE WHEN ` + currencyArg + `='__none__' THEN '' ELSE coalesce(nullif(` + currencyArg + `,''),(SELECT coalesce(max(currency) FILTER (WHERE currency='USD'),min(currency)) FROM totals),'') END currency),
 categories AS (SELECT category,currency,sum(amount) amount FROM filtered WHERE priced_records>0 GROUP BY category,currency),
 category_totals AS (SELECT category,jsonb_object_agg(currency,amount) amounts FROM categories GROUP BY category),
 dimensions AS (SELECT ` + keyJSON + ` key,` + dimJSON + ` dims,` + name + ` name,currency,records,priced_records,amount FROM filtered),
 per_currency AS (SELECT key,dims,max(name) name,currency,sum(records) records,sum(priced_records) priced_records,sum(amount) amount FROM dimensions GROUP BY key,dims,currency),
 grouped AS (SELECT key,dims,coalesce(max(name),nullif(dims->>'resource',''),'Unknown resource') name,sum(records) lines,coalesce(jsonb_object_agg(currency,amount) FILTER(WHERE priced_records>0),'{}'::jsonb) cost FROM per_currency GROUP BY key,dims),
 page AS (SELECT *,row_number() OVER(ORDER BY ` + order + `) position FROM grouped ORDER BY ` + order + ` LIMIT ` + limitArg + ` OFFSET ` + offsetArg + `)
 SELECT jsonb_build_object(
 'rows',coalesce((SELECT jsonb_agg(dims || jsonb_build_object('key',key,'name',name,'cost',cost,'lines',lines) ORDER BY position) FROM page),'[]'::jsonb),
 'total',coalesce((SELECT jsonb_object_agg(currency,amount) FROM totals),'{}'::jsonb),
 'categories',coalesce((SELECT jsonb_object_agg(category,amounts) FROM category_totals),'{}'::jsonb),
 'count',(SELECT count(*) FROM grouped),'limit',` + limitArg + `::integer,'offset',` + offsetArg + `::integer,
 'currency',(SELECT currency FROM chosen),'currencies',coalesce((SELECT jsonb_agg(currency ORDER BY currency) FROM totals),'[]'::jsonb))`
	var raw []byte
	if err = db.QueryRow(ctx, sql, args...).Scan(&raw); err != nil {
		return CostPage{}, err
	}
	var out CostPage
	err = json.Unmarshal(raw, &out)
	return out, err
}
func (p *Postgres) RecordCostCollection(ctx context.Context, kind string, through, at time.Time) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO cost_collections VALUES($1,$2,$3) ON CONFLICT(kind) DO UPDATE SET through=EXCLUDED.through,collected_at=EXCLUDED.collected_at`, kind, through.UTC(), at.UTC())
	return err
}
func (p *Postgres) CostCollection(ctx context.Context, kind string) (CostCollectionStatus, error) {
	var out CostCollectionStatus
	err := p.pool.QueryRow(ctx, `SELECT max(through),max(collected_at) FROM cost_collections WHERE kind=$1`, kind).Scan(&out.Through, &out.CollectedAt)
	return out, err
}
