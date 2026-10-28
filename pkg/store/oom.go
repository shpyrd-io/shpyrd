package store

import (
	"context"
	"strconv"
	"time"
)

// OOMKill is one container the kernel killed for crossing its memory limit
// (#119). Kubernetes keeps only the last death of each container, and the
// node's events expire within the hour, so the controller writes every one
// here as it sees it; the project's Memory chart marks them, and the
// operator reads the platform's own.
type OOMKill struct {
	// WorkspaceID and Project (the project's short ID) say whose process
	// died. Both are empty for a platform component, such as the backup
	// CronJob in the system namespace.
	WorkspaceID string `json:"workspaceId,omitempty"`
	Project     string `json:"project,omitempty"`
	Namespace   string `json:"namespace"`
	Pod         string `json:"pod"`
	Container   string `json:"container"`
	// Process is the process type (web, worker) or what a Job pod was for
	// (release, build, run); a platform component's is its workload's name.
	Process string `json:"process"`
	// Instance is the pod's human name when it died (web.1); "" when it had
	// none, as a Job pod has not.
	Instance string `json:"instance,omitempty"`
	// At is when the container died: the kubelet's finishedAt.
	At time.Time `json:"at"`
	// LimitBytes is the memory limit the container crossed; 0 when it ran
	// without one.
	LimitBytes int64 `json:"limitBytes"`
	// Size is the process's size at the time, when the pod said it.
	Size string `json:"size,omitempty"`
	// Release is the release the instance ran, 0 when the pod did not say.
	Release int `json:"release,omitempty"`
}

// sameKill says two records are the one death: Kubernetes reports it
// again on every status change until the container dies once more.
func sameKill(a, b OOMKill) bool {
	return a.Namespace == b.Namespace && a.Pod == b.Pod && a.Container == b.Container && a.At.Equal(b.At)
}

// OOMKillQuery selects kills: those of one namespace, or every one, in
// [From, To).
type OOMKillQuery struct {
	Namespace string
	From, To  time.Time
}

// OOMKills is the part of the Store that remembers out-of-memory kills.
type OOMKills interface {
	// RecordOOMKills stores the kills not stored yet and returns how many
	// were new. A kill is the same when its namespace, pod, container and
	// time are, so reporting one twice changes nothing.
	RecordOOMKills(ctx context.Context, kills []OOMKill) (int, error)
	// QueryOOMKills lists the kills the query selects, newest first.
	QueryOOMKills(ctx context.Context, q OOMKillQuery) ([]OOMKill, error)
}

// ---- memory -----------------------------------------------------------------

func (m *Memory) RecordOOMKills(_ context.Context, kills []OOMKill) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	added := 0
	for _, k := range kills {
		k.At = k.At.UTC()
		dup := false
		for _, have := range m.oomKills {
			if sameKill(have, k) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		m.oomKills = append(m.oomKills, k)
		added++
	}
	return added, nil
}

func (m *Memory) QueryOOMKills(_ context.Context, q OOMKillQuery) ([]OOMKill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []OOMKill{}
	for _, k := range m.oomKills {
		if q.Namespace != "" && k.Namespace != q.Namespace {
			continue
		}
		if (!q.From.IsZero() && k.At.Before(q.From)) || (!q.To.IsZero() && !k.At.Before(q.To)) {
			continue
		}
		out = append(out, k)
	}
	sortOOMKills(out)
	return out, nil
}

// sortOOMKills orders kills newest first, then by pod and container so
// two deaths at one second have one order.
func sortOOMKills(kills []OOMKill) {
	for i := 1; i < len(kills); i++ {
		for j := i; j > 0 && oomBefore(kills[j], kills[j-1]); j-- {
			kills[j], kills[j-1] = kills[j-1], kills[j]
		}
	}
}

func oomBefore(a, b OOMKill) bool {
	if !a.At.Equal(b.At) {
		return a.At.After(b.At)
	}
	if a.Pod != b.Pod {
		return a.Pod < b.Pod
	}
	return a.Container < b.Container
}

// ---- postgres ---------------------------------------------------------------

const oomKillColumns = `namespace, pod, container, at, workspace_id, project, process, instance, limit_bytes, size_name, release_number`

func (p *Postgres) RecordOOMKills(ctx context.Context, kills []OOMKill) (int, error) {
	if len(kills) == 0 {
		return 0, nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	added := 0
	for _, k := range kills {
		tag, err := tx.Exec(ctx, `INSERT INTO oom_kills (`+oomKillColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING`,
			k.Namespace, k.Pod, k.Container, k.At.UTC(), k.WorkspaceID, k.Project, k.Process, k.Instance, k.LimitBytes, k.Size, k.Release)
		if err != nil {
			return 0, err
		}
		added += int(tag.RowsAffected())
	}
	return added, tx.Commit(ctx)
}

func (p *Postgres) QueryOOMKills(ctx context.Context, q OOMKillQuery) ([]OOMKill, error) {
	sql := `SELECT ` + oomKillColumns + ` FROM oom_kills WHERE true`
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if q.Namespace != "" {
		sql += ` AND namespace = ` + arg(q.Namespace)
	}
	if !q.From.IsZero() {
		sql += ` AND at >= ` + arg(q.From.UTC())
	}
	if !q.To.IsZero() {
		sql += ` AND at < ` + arg(q.To.UTC())
	}
	rows, err := p.pool.Query(ctx, sql+` ORDER BY at DESC, pod, container`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OOMKill{}
	for rows.Next() {
		var k OOMKill
		if err := rows.Scan(&k.Namespace, &k.Pod, &k.Container, &k.At, &k.WorkspaceID, &k.Project, &k.Process, &k.Instance, &k.LimitBytes, &k.Size, &k.Release); err != nil {
			return nil, err
		}
		k.At = k.At.UTC()
		out = append(out, k)
	}
	return out, rows.Err()
}
