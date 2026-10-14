package store

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type CostExport struct {
	Chunks    int       `json:"chunks"`
	Bytes     int64     `json:"bytes"`
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Export pages are written incrementally. Each currency gets its own row; no
// conversion or cross-currency sum is implicit. Neutralize spreadsheet formulas.
func costCSV(s string) string {
	trimmed := strings.TrimLeft(s, " \t\r\n")
	if len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + s
	}
	return s
}

func exportCostPages(ctx context.Context, q CostExploreQuery, read func(context.Context, CostExploreQuery) (CostPage, error), write func(int, []byte) error) error {
	q.Offset = 0
	q.Limit = 200
	for sequence := 0; ; sequence++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, err := read(ctx, q)
		if err != nil {
			return err
		}
		// Pin the currency selected on page one for stable sorting across pages.
		q.Currency = page.Currency
		if q.Currency == "" {
			q.Currency = "__none__"
		}
		var b bytes.Buffer
		w := csv.NewWriter(&b)
		if sequence == 0 {
			_ = w.Write([]string{"Name", "Workspace", "Workspace ID", "Project ID", "Category", "Workload", "Process", "Resource type", "Resource ID", "Provider service", "Currency", "Cost", "Cost records"})
		}
		for _, r := range page.Rows {
			currencies := []string{}
			for c := range r.Cost {
				currencies = append(currencies, c)
			}
			sort.Strings(currencies)
			if len(currencies) == 0 {
				currencies = []string{""}
			}
			for _, c := range currencies {
				amount := ""
				if v, ok := r.Cost[c]; ok {
					amount = strconv.FormatFloat(v, 'f', 8, 64)
				}
				row := []string{r.Name, r.WorkspaceName, r.Workspace, r.Project, r.Category, r.Workload, r.Process, r.ResourceType, r.Resource, r.Service, c}
				for i := range row {
					row[i] = costCSV(row[i])
				}
				row = append(row, amount, strconv.FormatInt(r.Lines, 10))
				_ = w.Write(row)
			}
		}
		w.Flush()
		if err = w.Error(); err != nil {
			return err
		}
		if err = write(sequence, b.Bytes()); err != nil {
			return err
		}
		q.Offset += len(page.Rows)
		if q.Offset >= page.Count || len(page.Rows) == 0 {
			return nil
		}
	}
}
func (p *Postgres) CreateCostExport(ctx context.Context, q CostExploreQuery) (CostExport, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return CostExport{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(82734612)`); err != nil {
		return CostExport{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM cost_exports WHERE expires_at<now()`); err != nil {
		return CostExport{}, err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM cost_exports WHERE status='queued'`).Scan(&count); err != nil {
		return CostExport{}, err
	}
	if count >= 5 {
		return CostExport{}, ErrConflict
	}
	data, err := json.Marshal(q)
	if err != nil {
		return CostExport{}, err
	}
	job := CostExport{ID: newID(), Status: "queued"}
	err = tx.QueryRow(ctx, `INSERT INTO cost_exports(id,query) VALUES($1,$2) RETURNING expires_at`, job.ID, data).Scan(&job.ExpiresAt)
	if err != nil {
		return job, err
	}
	return job, tx.Commit(ctx)
}
func (p *Postgres) CostExport(ctx context.Context, id string) (CostExport, error) {
	var job CostExport
	err := p.pool.QueryRow(ctx, `SELECT id,status,expires_at,(SELECT count(*) FROM cost_export_chunks WHERE export_id=id),(SELECT coalesce(sum(octet_length(data)),0) FROM cost_export_chunks WHERE export_id=id) FROM cost_exports WHERE id=$1 AND expires_at>now()`, id).Scan(&job.ID, &job.Status, &job.ExpiresAt, &job.Chunks, &job.Bytes)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return job, err
}
func (p *Postgres) CostExportChunk(ctx context.Context, id string, sequence int) ([]byte, error) {
	var b []byte
	err := p.pool.QueryRow(ctx, `SELECT data FROM cost_export_chunks JOIN cost_exports ON id=export_id WHERE export_id=$1 AND sequence=$2 AND status='ready' AND expires_at>now()`, id, sequence).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return b, err
}
func (p *Postgres) BuildCostExport(ctx context.Context) (bool, error) {
	// All pages come from one repeatable snapshot; collection can continue while
	// an export runs. A crash rolls back chunks and leaves the job queued to retry.
	if _, err := p.pool.Exec(ctx, `DELETE FROM cost_exports WHERE expires_at<now()`); err != nil {
		return false, err
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var locked bool
	if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(82734613)`).Scan(&locked); err != nil || !locked {
		return false, err
	}
	var id string
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT id,query FROM cost_exports WHERE status='queued' AND expires_at>now() ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var q CostExploreQuery
	err = json.Unmarshal(raw, &q)
	if err == nil {
		err = exportCostPages(ctx, q, func(ctx context.Context, q CostExploreQuery) (CostPage, error) { return exploreCostsSQL(ctx, tx, q) }, func(seq int, b []byte) error {
			_, err := tx.Exec(ctx, `INSERT INTO cost_export_chunks VALUES($1,$2,$3)`, id, seq, b)
			return err
		})
	}
	if err != nil {
		_ = tx.Rollback(context.Background())
		if errors.Is(err, context.Canceled) {
			return true, err
		}
		failureCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = p.pool.Exec(failureCtx, `UPDATE cost_exports SET status='failed' WHERE id=$1`, id)
		return true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE cost_exports SET status='ready' WHERE id=$1`, id); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}

type memoryCostExport struct {
	job    CostExport
	query  CostExploreQuery
	chunks [][]byte
}

func (m *Memory) CreateCostExport(_ context.Context, q CostExploreQuery) (CostExport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.costs()
	if c.exports == nil {
		c.exports = map[string]*memoryCostExport{}
	}
	queued := 0
	for id, e := range c.exports {
		if e.job.ExpiresAt.Before(m.now()) {
			delete(c.exports, id)
		} else if e.job.Status == "queued" {
			queued++
		}
	}
	if queued >= 5 {
		return CostExport{}, ErrConflict
	}
	job := CostExport{ID: newID(), Status: "queued", ExpiresAt: m.now().Add(24 * time.Hour)}
	c.exports[job.ID] = &memoryCostExport{job: job, query: q}
	return job, nil
}
func (m *Memory) CostExport(_ context.Context, id string) (CostExport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.costs().exports[id]
	if e == nil || e.job.ExpiresAt.Before(m.now()) {
		return CostExport{}, ErrNotFound
	}
	return e.job, nil
}
func (m *Memory) CostExportChunk(_ context.Context, id string, seq int) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.costs().exports[id]
	if e == nil || e.job.Status != "ready" || e.job.ExpiresAt.Before(m.now()) || seq >= len(e.chunks) {
		return nil, ErrNotFound
	}
	return append([]byte(nil), e.chunks[seq]...), nil
}
func (m *Memory) BuildCostExport(ctx context.Context) (bool, error) {
	m.mu.Lock()
	var selected *memoryCostExport
	for _, e := range m.costs().exports {
		if e.job.Status == "queued" && e.job.ExpiresAt.After(m.now()) {
			selected = e
			break
		}
	}
	if selected == nil {
		m.mu.Unlock()
		return false, nil
	}
	selected.job.Status = "preparing"
	snapshot := NewMemory()
	snapshot.costs()
	snapshot.projects = append([]Project(nil), m.projects...)
	for slug, w := range m.workspaces {
		copy := *w
		snapshot.workspaces[slug] = &copy
	}
	for id, l := range m.costs().lines {
		snapshot.cost.lines[id] = l
	}
	m.mu.Unlock()
	chunks := [][]byte{}
	err := exportCostPages(ctx, selected.query, snapshot.ExploreCosts, func(_ int, b []byte) error { chunks = append(chunks, append([]byte(nil), b...)); return nil })
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		selected.job.Status = "failed"
	} else {
		selected.chunks = chunks
		selected.job.Chunks = len(chunks)
		for _, b := range chunks {
			selected.job.Bytes += int64(len(b))
		}
		selected.job.Status = "ready"
	}
	return true, err
}
