package store

import (
	"context"
	"maps"
	"sort"
	"time"
)

// ---- costs (ee/costs) --------------------------------------------------------

type costsMemory struct {
	lines  map[string]CostLine
	drains []CostDrain
	clock  time.Time // changed_at only moves forward, as in Postgres
}

func (m *Memory) costs() *costsMemory {
	if m.cost == nil {
		m.cost = &costsMemory{lines: map[string]CostLine{}}
	}
	return m.cost
}

func sameNumbers(a, b CostLine) bool {
	eq := func(x, y *float64) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return eq(a.Quantity, b.Quantity) && eq(a.Cost, b.Cost) && a.Unit == b.Unit && a.Currency == b.Currency && a.ResourceType == b.ResourceType && maps.Equal(a.Tags, b.Tags)
}

func (m *Memory) UpsertCostLines(_ context.Context, lines []CostLine) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.costs()
	changed := 0
	for _, l := range lines {
		l.ID = CostLineID(l)
		l.Start, l.End = l.Start.UTC(), l.End.UTC()
		if cur, ok := c.lines[l.ID]; ok && sameNumbers(cur, l) {
			continue
		}
		now := m.now()
		if !now.After(c.clock) {
			now = c.clock.Add(time.Microsecond)
		}
		c.clock = now
		l.ChangedAt = now
		c.lines[l.ID] = l
		changed++
	}
	return changed, nil
}

func (m *Memory) QueryCostLines(_ context.Context, q CostQuery) ([]CostLine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []CostLine{}
	for _, l := range m.costs().lines {
		if l.Start.Before(q.From) || !l.Start.Before(q.To) || (q.Kind != "" && l.Kind != q.Kind) || (q.Workspace != "" && l.Workspace != q.Workspace) {
			continue
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Start.Equal(out[j].Start) {
			return out[i].Start.Before(out[j].Start)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (m *Memory) CostLinesChangedSince(_ context.Context, at time.Time, id string, limit int) ([]CostLine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []CostLine{}
	for _, l := range m.costs().lines {
		if l.ChangedAt.After(at) || (l.ChangedAt.Equal(at) && l.ID > id) {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ChangedAt.Equal(out[j].ChangedAt) {
			return out[i].ChangedAt.Before(out[j].ChangedAt)
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) ListCostDrains(_ context.Context) ([]CostDrain, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]CostDrain{}, m.costs().drains...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *Memory) CreateCostDrain(_ context.Context, d CostDrain) (*CostDrain, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.costs()
	for _, e := range c.drains {
		if e.Name == d.Name {
			return nil, ErrConflict
		}
	}
	if d.ID == "" {
		d.ID = newID()
	}
	d.CreatedAt = m.now()
	c.drains = append(c.drains, d)
	return &d, nil
}

func (m *Memory) DeleteCostDrain(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.costs()
	for i, e := range c.drains {
		if e.Name == name {
			c.drains = append(c.drains[:i], c.drains[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (m *Memory) RecordCostDelivery(_ context.Context, id string, d CostDelivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.costs()
	for i := range c.drains {
		e := &c.drains[i]
		if e.ID != id {
			continue
		}
		if d.Err != "" {
			e.Errors++
			e.Message = d.Err
			return nil
		}
		at := d.At.UTC()
		e.Sent += int64(d.Sent)
		e.LastDeliveryAt, e.Message = &at, ""
		if d.CursorAt != nil {
			cur := d.CursorAt.UTC()
			e.CursorAt, e.CursorID = &cur, d.CursorID
		}
		return nil
	}
	return ErrNotFound
}
