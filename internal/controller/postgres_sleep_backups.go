package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/robfig/cron/v3"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// Backups around sleep (RFC-0075, storage plan step 6). A hibernated
// cluster runs no ScheduledBackup and archives no WAL, so a database with
// backups is copied before it sleeps when its last backup is older than a
// schedule period, its schedule is suspended while it sleeps, and a copy
// is taken after a wake when the last one is older than a period again.
// LastBackup is derived from the completed Backups, these included, so it
// stays truthful.

const (
	// backupBeforeSleepWait is how long hibernation waits for the backup
	// before sleeping anyway, with a visible warning: the volume keeps the
	// data; only the copy is late.
	backupBeforeSleepWait = 60 * time.Minute
	// backupBeforeSleep and backupAfterWake name the two on-demand backups
	// (<database>-<purpose>), one of each at a time per database.
	backupBeforeSleep = "before-sleep"
	backupAfterWake   = "after-wake"
	// annotationBackupRequestedAt is when the controller asked for an
	// on-demand backup, on the Backup object.
	annotationBackupRequestedAt = "shpyrd.io/requested-at"
	// labelBackupPurpose marks the controller's on-demand backups.
	labelBackupPurpose = "shpyrd.io/backup"
	// noFreshBackupNote opens the warning in the sleep status message of a
	// database that hibernated without a fresh copy.
	noFreshBackupNote = "no fresh backup: "
)

// schedulePeriod is the time between two runs of the backup schedule, the
// freshness a backup must have: one younger than it needs no copy before
// sleep. A day when the schedule cannot be read (CheckBackups refuses
// those before they are stored).
func schedulePeriod(spec *shpyrdv1.PostgresBackups) time.Duration {
	s, err := backupSchedule(spec)
	if err != nil {
		return 24 * time.Hour
	}
	sched, err := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(s)
	if err != nil {
		return 24 * time.Hour
	}
	first := sched.Next(time.Now())
	if d := sched.Next(first).Sub(first); d > 0 {
		return d
	}
	return 24 * time.Hour
}

// backupFresh says the last completed backup is younger than a schedule
// period, so sleep needs no copy first.
func backupFresh(pg *shpyrdv1.Postgres, now time.Time) bool {
	return pg.Status.LastBackup != nil && now.Sub(pg.Status.LastBackup.Time) < schedulePeriod(pg.Spec.Backups)
}

// backupTerminal says the Backup's phase is final.
func backupTerminal(phase string) bool { return phase == "completed" || phase == "failed" }

// backupInFlight finds a Backup of the database that is not finished yet,
// whoever asked for it (the schedule, or the controller), with when it
// started: a second copy while one runs is wasted load.
func (r *PostgresReconciler) backupInFlight(ctx context.Context, pg *shpyrdv1.Postgres) (name string, since time.Time, ok bool) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(CNPGBackupGVK.GroupVersion().WithKind("BackupList"))
	if err := r.List(ctx, list, client.InNamespace(pg.Namespace)); err != nil {
		return "", time.Time{}, false
	}
	for _, b := range list.Items {
		if cl, _, _ := unstructured.NestedString(b.Object, "spec", "cluster", "name"); cl != pg.Name {
			continue
		}
		phase, _, _ := unstructured.NestedString(b.Object, "status", "phase")
		if backupTerminal(phase) {
			continue
		}
		return b.GetName(), backupRequestedAt(&b), true
	}
	return "", time.Time{}, false
}

// backupRequestedAt is when a Backup was asked for: the controller's
// annotation on its own, the creation time on the schedule's.
func backupRequestedAt(b *unstructured.Unstructured) time.Time {
	if s := b.GetAnnotations()[annotationBackupRequestedAt]; s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return b.GetCreationTimestamp().Time
}

// onDemandBackup finds or creates the controller's Backup of a purpose and
// reports its phase ("" when just asked for) and when it was asked. One
// requested before `since` (an earlier sleep or wake) is replaced, unless it
// still runs; a finished one of this cycle is reported as it is.
func (r *PostgresReconciler) onDemandBackup(ctx context.Context, pg *shpyrdv1.Postgres, purpose string, since time.Time) (phase string, requested time.Time, err error) {
	name := pg.Name + "-" + purpose
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(CNPGBackupGVK)
	err = r.Get(ctx, types.NamespacedName{Namespace: pg.Namespace, Name: name}, b)
	switch {
	case err == nil:
		phase, _, _ = unstructured.NestedString(b.Object, "status", "phase")
		requested = backupRequestedAt(b)
		if !backupTerminal(phase) || !requested.Before(since) {
			return phase, requested, nil
		}
		// An earlier cycle's: make room for this one's.
		if err := r.Delete(ctx, b); err != nil && !apierrors.IsNotFound(err) {
			return "", time.Time{}, fmt.Errorf("replace backup %s: %w", name, err)
		}
	case !apierrors.IsNotFound(err):
		return "", time.Time{}, err
	}
	now := time.Now().UTC()
	b = &unstructured.Unstructured{}
	b.SetGroupVersionKind(CNPGBackupGVK)
	b.SetName(name)
	b.SetNamespace(pg.Namespace)
	b.SetLabels(map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", "shpyrd.io/postgres": pg.Name, labelBackupPurpose: purpose})
	b.SetAnnotations(map[string]string{annotationBackupRequestedAt: now.Format(time.RFC3339)})
	b.Object["spec"] = map[string]interface{}{
		"cluster":             map[string]interface{}{"name": pg.Name},
		"method":              "plugin",
		"pluginConfiguration": map[string]interface{}{"name": BarmanPluginName},
	}
	if err := controllerutil.SetControllerReference(pg, b, r.Scheme); err != nil {
		return "", time.Time{}, err
	}
	if err := r.Create(ctx, b); err != nil && !apierrors.IsAlreadyExists(err) {
		return "", time.Time{}, fmt.Errorf("backup %s: %w", name, err)
	}
	log.FromContext(ctx).Info("backup requested", "postgres", pg.Name, "purpose", purpose)
	return "", now, nil
}

// backupBeforeSleepReady says the database may hibernate as far as its
// backups go: it has none, or a fresh one, or the copy taken for this sleep
// finished. Otherwise it asks for the copy and says what the status should
// read while waiting; past the wait, or after a failed copy, it lets the
// database sleep with the warning it returns.
func (r *PostgresReconciler) backupBeforeSleepReady(ctx context.Context, pg *shpyrdv1.Postgres, now time.Time) (ready bool, waiting, warning string, err error) {
	if pg.Spec.Backups == nil || backupFresh(pg, now) {
		return true, "", "", nil
	}
	idleSince := now
	if pg.Status.Sleep != nil && pg.Status.Sleep.LastActivityAt != nil {
		idleSince = pg.Status.Sleep.LastActivityAt.Time
	}
	name, since, running := r.backupInFlight(ctx, pg)
	if !running || name == pg.Name+"-"+backupBeforeSleep {
		var phase string
		phase, since, err = r.onDemandBackup(ctx, pg, backupBeforeSleep, idleSince)
		if err != nil {
			return false, "", "", err
		}
		switch phase {
		case "completed":
			return true, "", "", nil
		case "failed":
			return true, "", "the backup before sleeping failed", nil
		}
	}
	if now.Sub(since) > backupBeforeSleepWait {
		return true, "", fmt.Sprintf("the backup before sleeping did not finish in %s", backupBeforeSleepWait), nil
	}
	return false, fmt.Sprintf("awake: idle, taking a backup before sleeping (%s so far)", now.Sub(since).Truncate(time.Second)), "", nil
}

// backupAfterWakeIfDue takes a copy after a wake when the last one is older
// than a schedule period and none is running (the schedule may have started
// its own missed run when it was resumed).
func (r *PostgresReconciler) backupAfterWakeIfDue(ctx context.Context, pg *shpyrdv1.Postgres, now time.Time) error {
	if pg.Spec.Backups == nil || backupFresh(pg, now) {
		return nil
	}
	if _, _, running := r.backupInFlight(ctx, pg); running {
		return nil
	}
	_, _, err := r.onDemandBackup(ctx, pg, backupAfterWake, now.Add(-time.Minute))
	return err
}

// backupsSuspended says the ScheduledBackup is suspended for the database's
// state: a hibernated cluster has no instance to back up.
func backupsSuspended(pg *shpyrdv1.Postgres) bool {
	return pg.Status.Sleep != nil && (pg.Status.Sleep.State == pgSleeping || pg.Status.Sleep.State == pgSuspended)
}

// syncBackupSchedule sets the ScheduledBackup's suspend flag to the sleep
// state at once, not at the next rendering of the schedule.
func (r *PostgresReconciler) syncBackupSchedule(ctx context.Context, pg *shpyrdv1.Postgres) error {
	if pg.Spec.Backups == nil {
		return nil
	}
	schedule := &unstructured.Unstructured{}
	schedule.SetGroupVersionKind(CNPGScheduledBackupGVK)
	if err := r.Get(ctx, types.NamespacedName{Namespace: pg.Namespace, Name: pg.Name + "-scheduled"}, schedule); err != nil {
		if apierrors.IsNotFound(err) || isNoKind(err) {
			return nil
		}
		return err
	}
	want := backupsSuspended(pg)
	if cur, _, _ := unstructured.NestedBool(schedule.Object, "spec", "suspend"); cur == want {
		return nil
	}
	_ = unstructured.SetNestedField(schedule.Object, want, "spec", "suspend")
	return r.Update(ctx, schedule)
}

// sleepBackupWarning is the warning a sleeping database's status carries
// when it hibernated without a fresh copy, "" otherwise.
func sleepBackupWarning(pg *shpyrdv1.Postgres) string {
	if pg.Status.Sleep == nil {
		return ""
	}
	if i := strings.Index(pg.Status.Sleep.Message, noFreshBackupNote); i >= 0 {
		return pg.Status.Sleep.Message[i:]
	}
	return ""
}

var _ = metav1.Now
