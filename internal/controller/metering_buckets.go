package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Object storage and the dedicated databases' sizes (storage plan step 4).

// bucketBuckets meters what every project bucket holds for the window:
// the bytes its status reports (measured by the bucket controller) times
// seconds, one bucket per bucket, from the objects themselves. The bucket
// a database's backups go to is the platform's and is not metered.
func (m *MeteringLoop) bucketBuckets(ctx context.Context, start, end time.Time, nsMap map[string][2]string) []store.UsageBucket {
	if m.Client == nil {
		return nil
	}
	var list shpyrdv1.ObjectBucketList
	if err := m.Client.List(ctx, &list); err != nil {
		log.FromContext(ctx).Error(err, "buckets for metering")
		return nil
	}
	dur := end.Sub(start).Seconds()
	var out []store.UsageBucket
	for _, b := range list.Items {
		meta, ok := nsMap[b.Namespace]
		if !ok || b.DeletionTimestamp != nil || b.Labels["shpyrd.io/postgres"] != "" {
			continue
		}
		gib := float64(b.Status.UsedBytes) / (1024 * 1024 * 1024)
		out = append(out, store.UsageBucket{
			WorkspaceID: meta[0], Project: meta[1], Component: "bucket/" + b.Name,
			Metric: store.MetricObjectStorage, PeriodStart: start, PeriodEnd: end,
			Quantity: ptr.To(gib * dur), Unit: store.UnitGiBSeconds, Quality: store.QualityComplete, Revision: 1, Source: "usedbytes_v1",
		})
	}
	return out
}

// AnnotationDataBytes is what a dedicated database's data weighs, as the
// metering loop last measured it: what a conversion to shared compares
// with the cap before it starts.
const AnnotationDataBytes = "shpyrd.io/data-bytes"

// dedicatedSizes stamps every Ready dedicated database with its data's
// size, from the exporter's per-database size by pod (a cluster's pods
// are named after it).
func (m *MeteringLoop) dedicatedSizes(ctx context.Context, at time.Time) error {
	if m.Client == nil {
		return nil
	}
	var list shpyrdv1.PostgresList
	if err := m.Client.List(ctx, &list); err != nil {
		return err
	}
	var dedicated []shpyrdv1.Postgres
	for _, pg := range list.Items {
		if pg.Spec.Type != shpyrdv1.PostgresTypeShared && pg.Status.Phase == shpyrdv1.ResourceReady {
			dedicated = append(dedicated, pg)
		}
	}
	if len(dedicated) == 0 {
		return nil
	}
	sizes, err := m.Prom.QueryInstantSeriesAt(ctx,
		fmt.Sprintf(`max by (namespace, pod) (cnpg_pg_database_size_bytes{namespace!=%q,datname!~"template.*|postgres"})`, SharedPostgresNamespace), at)
	if err != nil {
		return fmt.Errorf("dedicated database sizes: %w", err)
	}
	for i := range dedicated {
		pg := &dedicated[i]
		var bytes int64
		measured := false
		for _, s := range sizes {
			if s.Labels["namespace"] == pg.Namespace && strings.HasPrefix(s.Labels["pod"], pg.Name+"-") {
				if int64(s.Value) > bytes {
					bytes = int64(s.Value)
				}
				measured = true
			}
		}
		if !measured || pg.Annotations[AnnotationDataBytes] == strconv.FormatInt(bytes, 10) {
			continue
		}
		patch := client.MergeFrom(pg.DeepCopy())
		if pg.Annotations == nil {
			pg.Annotations = map[string]string{}
		}
		pg.Annotations[AnnotationDataBytes] = strconv.FormatInt(bytes, 10)
		if err := m.Client.Patch(ctx, pg, patch); err != nil {
			log.FromContext(ctx).Error(err, "data size stamp failed", "postgres", pg.Namespace+"/"+pg.Name)
		}
	}
	return nil
}

// DataBytes is what a database's data weighed when last measured, 0 when
// never.
func DataBytes(pg *shpyrdv1.Postgres) int64 {
	n, _ := strconv.ParseInt(pg.Annotations[AnnotationDataBytes], 10, 64)
	return n
}
