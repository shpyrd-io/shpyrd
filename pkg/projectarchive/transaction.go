package projectarchive

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Participant stages data without changing the active copy. Commit and
// Rollback are retryable after a process restart; Finish removes recovery
// data. The caller holds a durable exclusive project maintenance lock.
type Participant struct {
	Name     string
	Stage    func(context.Context) error
	Commit   func(context.Context) error
	Rollback func(context.Context) error
	Finish   func(context.Context) error
}

type TransactionState struct {
	Phase    string `json:"phase"`
	Resource string `json:"resource,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Journal must persist outside the API process and outside the uploaded tgz.
// Failed operations retain this record so a later request can recover them.
type Journal func(context.Context, TransactionState) error

// RestoreTransaction deliberately does not remove maintenance or recovery
// copies. The caller verifies application health and durably commits the
// project's new configuration before allowing external traffic again.
func RestoreTransaction(ctx context.Context, resources []Participant, save Journal) error {
	seen := map[string]bool{}
	for _, r := range resources {
		if r.Name == "" || seen[r.Name] || r.Stage == nil || r.Commit == nil || r.Rollback == nil || r.Finish == nil {
			return errors.New("invalid restore participant")
		}
		seen[r.Name] = true
	}
	for _, phase := range []string{"staging", "committing"} {
		for _, r := range resources {
			if err := save(ctx, TransactionState{Phase: phase, Resource: r.Name}); err != nil {
				return fmt.Errorf("persist restore intent: %w", err)
			}
			fn := r.Stage
			if phase == "committing" {
				fn = r.Commit
			}
			if err := fn(ctx); err != nil {
				cause := fmt.Errorf("%s %s: %w", phase, r.Name, err)
				// Request cancellation must not cancel recovery. If cleanup
				// itself fails, maintenance remains and recovery is explicit.
				recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
				defer cancel()
				return errors.Join(cause, RollbackTransaction(recovery, resources, save))
			}
		}
	}
	return save(ctx, TransactionState{Phase: "verifying"})
}

func RollbackTransaction(ctx context.Context, resources []Participant, save Journal) error {
	if err := save(ctx, TransactionState{Phase: "rolling-back"}); err != nil {
		return err
	}
	var failures []error
	for i := len(resources) - 1; i >= 0; i-- {
		r := resources[i]
		if err := r.Rollback(ctx); err != nil {
			failures = append(failures, fmt.Errorf("recover %s: %w", r.Name, err))
		}
	}
	if err := errors.Join(failures...); err != nil {
		return errors.Join(err, save(ctx, TransactionState{Phase: "recovery-required", Error: err.Error()}))
	}
	return save(ctx, TransactionState{Phase: "rolled-back"})
}

// FinalizeTransaction is called only after recording whether restoration or
// rollback won. Retrying finalization must never undo newly accepted writes.
func FinalizeTransaction(ctx context.Context, resources []Participant, save Journal) error {
	var failures []error
	for _, r := range resources {
		if err := r.Finish(ctx); err != nil {
			failures = append(failures, fmt.Errorf("clean up %s: %w", r.Name, err))
		}
	}
	return errors.Join(failures...)
}
