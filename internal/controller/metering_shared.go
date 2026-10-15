package controller

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The cap of a shared database (storage plan, step 3): its size is read
// from the shared servers' exporters every five minutes, with the other
// database metrics, and written into its status as "used of cap" with a
// condition that says where it stands against the cap. The ladder's
// later steps (the read-only nudge after seven days over, the fence at
// 150 %) act on this condition.

// ConditionCap is the condition of a shared database against its cap.
const ConditionCap = "Cap"

// Cap condition reasons, by what the measurement found.
const (
	CapWithin = "Within" // under 80 %
	CapNear   = "Near"   // 80 % and over
	CapOver   = "Over"   // 100 % and over
	CapFar    = "Far"    // 150 % and over: the fence applies
)

// sharedCaps measures every Ready shared database and writes what it found.
func (m *MeteringLoop) sharedCaps(ctx context.Context, at time.Time) error {
	if m.Client == nil {
		return nil
	}
	var list shpyrdv1.PostgresList
	if err := m.Client.List(ctx, &list); err != nil {
		return err
	}
	var shared []shpyrdv1.Postgres
	for _, pg := range list.Items {
		if pg.Spec.Type == shpyrdv1.PostgresTypeShared && pg.Status.Phase == shpyrdv1.ResourceReady {
			shared = append(shared, pg)
		}
	}
	if len(shared) == 0 {
		return nil
	}
	// One query for every tenant of every shared server: the exporter's
	// per-database size, by database name.
	sizes, err := m.Prom.QueryInstantSeriesAt(ctx,
		fmt.Sprintf(`max by (datname) (cnpg_pg_database_size_bytes{namespace=%q})`, SharedPostgresNamespace), at)
	if err != nil {
		return fmt.Errorf("shared database sizes: %w", err)
	}
	bytes := map[string]int64{}
	for _, s := range sizes {
		bytes[s.Labels["datname"]] = int64(s.Value)
	}
	for i := range shared {
		pg := &shared[i]
		used, measured := bytes[sharedDatabaseName(tenantHash(pg.UID))]
		if !measured {
			continue // not scraped yet; the last measurement stands
		}
		patch := client.MergeFrom(pg.DeepCopy())
		applyCap(&pg.Status, pg.Generation, used, pg.Spec.Storage, at)
		if err := m.Client.Status().Patch(ctx, pg, patch); err != nil {
			log.FromContext(ctx).Error(err, "shared cap patch failed", "postgres", pg.Namespace+"/"+pg.Name)
		}
	}
	return nil
}

// applyCap writes a measurement into a shared database's status: the
// storage line "used of cap" and the Cap condition.
func applyCap(st *shpyrdv1.ResourceStatus, generation int64, used int64, cap *resource.Quantity, at time.Time) {
	capBytes := int64(1 << 30)
	capText := "1Gi"
	if cap != nil && cap.Value() > 0 {
		capBytes, capText = cap.Value(), cap.String()
	}
	usedQ := resource.NewQuantity(used, resource.BinarySI)
	st.Storage = fmt.Sprintf("%s of %s", usedQ.String(), capText)
	ratio := float64(used) / float64(capBytes)
	percent := int(ratio*100 + 0.5)
	st.CapPercent = ptr.To(int32(percent))
	switch {
	case ratio >= 1.0 && st.CapOverSince == nil:
		st.CapOverSince = ptr.To(metav1.NewTime(at))
	case ratio < 1.0:
		st.CapOverSince = nil
	}
	cond := metav1.Condition{Type: ConditionCap, ObservedGeneration: generation, LastTransitionTime: metav1.NewTime(at)}
	switch {
	case ratio >= 1.5:
		cond.Status, cond.Reason = metav1.ConditionFalse, CapFar
		cond.Message = fmt.Sprintf("Your database is at %d%% of its %s cap.", percent, capText)
	case ratio >= 1.0:
		cond.Status, cond.Reason = metav1.ConditionFalse, CapOver
		cond.Message = fmt.Sprintf("Your database is over its %s cap (%d%%). Free space, or raise the limit.", capText, percent)
	case ratio >= 0.8:
		cond.Status, cond.Reason = metav1.ConditionTrue, CapNear
		cond.Message = fmt.Sprintf("Your database is at %d%% of its %s cap.", percent, capText)
	default:
		cond.Status, cond.Reason = metav1.ConditionTrue, CapWithin
		cond.Message = fmt.Sprintf("%s of %s used.", usedQ.String(), capText)
	}
	for i, c := range st.Conditions {
		if c.Type == ConditionCap {
			if c.Status == cond.Status && c.Reason == cond.Reason {
				cond.LastTransitionTime = c.LastTransitionTime
			}
			st.Conditions[i] = cond
			return
		}
	}
	st.Conditions = append(st.Conditions, cond)
}

// sharedBuckets meters every shared database's cap for the window (storage
// plan, step 3): cap GiB times seconds, one bucket per database, from the
// objects themselves, not from a scrape. A database with no cap yet is
// not metered.
func (m *MeteringLoop) sharedBuckets(ctx context.Context, start, end time.Time, nsMap map[string][2]string) []store.UsageBucket {
	if m.Client == nil {
		return nil
	}
	var list shpyrdv1.PostgresList
	if err := m.Client.List(ctx, &list); err != nil {
		log.FromContext(ctx).Error(err, "shared databases for metering")
		return nil
	}
	dur := end.Sub(start).Seconds()
	var out []store.UsageBucket
	for _, pg := range list.Items {
		meta, ok := nsMap[pg.Namespace]
		if !ok || pg.Spec.Type != shpyrdv1.PostgresTypeShared || pg.Spec.Storage == nil || pg.DeletionTimestamp != nil {
			continue
		}
		gib := float64(pg.Spec.Storage.Value()) / (1024 * 1024 * 1024)
		out = append(out, store.UsageBucket{
			WorkspaceID: meta[0], Project: meta[1], Component: "postgres/" + pg.Name,
			Metric: store.MetricPostgresShared, PeriodStart: start, PeriodEnd: end,
			Quantity: ptr.To(gib * dur), Unit: store.UnitGiBSeconds, Quality: store.QualityComplete, Revision: 1, Source: "cap_v1",
		})
	}
	return out
}
