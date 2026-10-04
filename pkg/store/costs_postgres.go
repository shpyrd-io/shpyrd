package store

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- costs (ee/costs) --------------------------------------------------------

const costLineColumns = `id, kind, source, period_start, period_end, workspace_id, project, process, metric, quantity, unit, cost, currency, resource, resource_type, service, sku, tags, changed_at`

func scanCostLine(row pgx.Row) (CostLine, error) {
	var l CostLine
	var tags []byte
	err := row.Scan(&l.ID, &l.Kind, &l.Source, &l.Start, &l.End, &l.Workspace, &l.Project, &l.Process, &l.Metric, &l.Quantity, &l.Unit, &l.Cost, &l.Currency, &l.Resource, &l.ResourceType, &l.Service, &l.SKU, &tags, &l.ChangedAt)
	if err != nil {
		return l, err
	}
	if len(tags) > 0 && string(tags) != "{}" {
		_ = json.Unmarshal(tags, &l.Tags)
	}
	return l, nil
}

func (p *Postgres) UpsertCostLines(ctx context.Context, lines []CostLine) (int, error) {
	if len(lines) == 0 {
		return 0, nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	changed := 0
	for _, l := range lines {
		tags := []byte("{}")
		if len(l.Tags) > 0 {
			if tags, err = json.Marshal(l.Tags); err != nil {
				return 0, err
			}
		}
		tag, err := tx.Exec(ctx, `INSERT INTO cost_lines (`+costLineColumns+`)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,clock_timestamp())
			ON CONFLICT (id) DO UPDATE SET quantity = EXCLUDED.quantity, unit = EXCLUDED.unit, cost = EXCLUDED.cost,
				currency = EXCLUDED.currency, resource_type = EXCLUDED.resource_type, tags = EXCLUDED.tags, changed_at = clock_timestamp()
			WHERE cost_lines.quantity IS DISTINCT FROM EXCLUDED.quantity OR cost_lines.cost IS DISTINCT FROM EXCLUDED.cost
				OR cost_lines.unit IS DISTINCT FROM EXCLUDED.unit OR cost_lines.currency IS DISTINCT FROM EXCLUDED.currency
				OR cost_lines.resource_type IS DISTINCT FROM EXCLUDED.resource_type OR cost_lines.tags IS DISTINCT FROM EXCLUDED.tags`,
			CostLineID(l), l.Kind, l.Source, l.Start.UTC(), l.End.UTC(), l.Workspace, l.Project, l.Process, l.Metric, l.Quantity, l.Unit, l.Cost, l.Currency, l.Resource, l.ResourceType, l.Service, l.SKU, tags)
		if err != nil {
			return 0, err
		}
		changed += int(tag.RowsAffected())
	}
	return changed, tx.Commit(ctx)
}

func (p *Postgres) QueryCostLines(ctx context.Context, q CostQuery) ([]CostLine, error) {
	sql := `SELECT ` + costLineColumns + ` FROM cost_lines WHERE period_start >= $1 AND period_start < $2`
	args := []any{q.From.UTC(), q.To.UTC()}
	if q.Kind != "" {
		args = append(args, q.Kind)
		sql += ` AND kind = $3`
	}
	if q.Workspace != "" {
		args = append(args, q.Workspace)
		sql += ` AND workspace_id = $` + strconv.Itoa(len(args))
	}
	rows, err := p.pool.Query(ctx, sql+` ORDER BY period_start, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CostLine{}
	for rows.Next() {
		l, err := scanCostLine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (p *Postgres) CostLinesChangedSince(ctx context.Context, at time.Time, id string, limit int) ([]CostLine, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+costLineColumns+` FROM cost_lines
		WHERE (changed_at, id) > ($1, $2) ORDER BY changed_at, id LIMIT $3`, at.UTC(), id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CostLine{}
	for rows.Next() {
		l, err := scanCostLine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

const costDrainColumns = `id, name, url, header_names, cursor_at, cursor_id, last_delivery_at, sent, errors, message, created_at`

func scanCostDrain(row pgx.Row) (CostDrain, error) {
	var d CostDrain
	var headers []byte
	err := row.Scan(&d.ID, &d.Name, &d.URL, &headers, &d.CursorAt, &d.CursorID, &d.LastDeliveryAt, &d.Sent, &d.Errors, &d.Message, &d.CreatedAt)
	if err == nil && len(headers) > 0 {
		_ = json.Unmarshal(headers, &d.Headers)
	}
	return d, err
}

func (p *Postgres) ListCostDrains(ctx context.Context) ([]CostDrain, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+costDrainColumns+` FROM cost_drains ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CostDrain{}
	for rows.Next() {
		d, err := scanCostDrain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (p *Postgres) CreateCostDrain(ctx context.Context, d CostDrain) (*CostDrain, error) {
	if d.ID == "" {
		d.ID = newID()
	}
	headers, err := json.Marshal(d.Headers)
	if err != nil {
		return nil, err
	}
	if d.Headers == nil {
		headers = []byte("[]")
	}
	row := p.pool.QueryRow(ctx, `INSERT INTO cost_drains (id, name, url, header_names, cursor_at)
		VALUES ($1,$2,$3,$4,$5) RETURNING `+costDrainColumns, d.ID, d.Name, d.URL, headers, d.CursorAt)
	out, err := scanCostDrain(row)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return nil, ErrConflict
		}
		return nil, err
	}
	return &out, nil
}

func (p *Postgres) DeleteCostDrain(ctx context.Context, name string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM cost_drains WHERE name = $1`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) RecordCostDelivery(ctx context.Context, id string, d CostDelivery) error {
	var tag interface{ RowsAffected() int64 }
	var err error
	if d.Err != "" {
		tag, err = p.pool.Exec(ctx, `UPDATE cost_drains SET errors = errors + 1, message = $2 WHERE id = $1`, id, d.Err)
	} else {
		tag, err = p.pool.Exec(ctx, `UPDATE cost_drains SET sent = sent + $2, last_delivery_at = $3, message = '',
			cursor_at = COALESCE($4, cursor_at), cursor_id = CASE WHEN $4::timestamptz IS NULL THEN cursor_id ELSE $5 END WHERE id = $1`,
			id, d.Sent, d.At.UTC(), d.CursorAt, d.CursorID)
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
