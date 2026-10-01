package api

import (
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The projection extrapolates usage, not the floor: a workspace that used
// a cent on the first day of a month with a 5.00 floor is projected at
// 5.00, not at the floor times thirty.
func TestProjectionLeavesTheFloorOutOfThePace(t *testing.T) {
	plan := &store.Plan{MinMonthly: 5}
	lines := []BillingLineView{
		{Component: "web", Metric: store.MetricCPUUsed, GrossAmount: 0.0084},
		{Component: "minimum", Metric: "min_monthly", GrossAmount: 4.9916},
	}
	day := 24 * time.Hour
	month := 31 * day
	if got := projectMonth(lines, 5, plan, 19*time.Hour, month); got != 5 {
		t.Errorf("first day with a floor: projected %.4f, want the floor 5.00", got)
	}
	// Usage above the floor projects on its own pace.
	lines = []BillingLineView{{Component: "web", Metric: store.MetricCPUUsed, GrossAmount: 6}}
	if got := projectMonth(lines, 6, plan, 10*day, month); got < 18.5 || got > 18.7 {
		t.Errorf("6.00 in ten days: projected %.2f, want about 18.60", got)
	}
	// Without a plan the pace alone counts; a free plan projects nothing.
	if got := projectMonth(lines, 6, nil, 10*day, month); got < 18.5 || got > 18.7 {
		t.Errorf("no plan: %.2f", got)
	}
	if got := projectMonth(lines, 0, &store.Plan{Free: true}, 10*day, month); got != 0 {
		t.Errorf("free plan: %.2f", got)
	}
}

// A price change is a new version from a date: buckets before it keep the
// old price, buckets after take the new one, and the floor is the version
// in force at the period's end.
func TestInvoicePreviewPricesEachBucketAtItsVersion(t *testing.T) {
	mar := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	versions := []store.Plan{
		{Name: "starter", CPUHour: 0.02, MinMonthly: 5, EffectiveFrom: mar.AddDate(0, -2, 0)},
		{Name: "starter", CPUHour: 0.04, MinMonthly: 0, EffectiveFrom: mar.AddDate(0, 0, 10)},
	}
	q := func(coreSeconds float64) *float64 { return &coreSeconds }
	buckets := []store.UsageBucket{
		{Project: "p", Component: "web", Metric: store.MetricCPUUsed, PeriodStart: mar.AddDate(0, 0, 5), Quantity: q(100 * 3600), Quality: store.QualityComplete},  // old price: 100 h × 0.02 = 2.00
		{Project: "p", Component: "web", Metric: store.MetricCPUUsed, PeriodStart: mar.AddDate(0, 0, 20), Quantity: q(100 * 3600), Quality: store.QualityComplete}, // new price: 100 h × 0.04 = 4.00
	}
	lines, total, _ := InvoicePreview(buckets, versions, mar, mar.AddDate(0, 1, 0))
	if len(lines) != 1 || lines[0].Quantity != 200 || lines[0].GrossAmount != 6 || lines[0].UnitPrice != 0.03 {
		t.Fatalf("lines = %+v", lines)
	}
	// The floor of the version in force at the end (none) applies: 6.00.
	if total != 6 {
		t.Errorf("total = %.2f, want 6.00", total)
	}
	// Priced to a date before the change, the floor of the first version holds.
	_, total, _ = InvoicePreview(buckets[:1], versions, mar, mar.AddDate(0, 0, 8))
	if total != 5 {
		t.Errorf("before the change the 5.00 floor holds over 2.00 of use: %.2f", total)
	}
	// No versions, no prices.
	if _, total, _ = InvoicePreview(buckets, nil, mar, mar.AddDate(0, 1, 0)); total != 0 {
		t.Errorf("no plan: %.2f", total)
	}
}

// Where the ledger carries both the working set and the reservation for a
// component, memory is priced once: the working set until the first
// reserved bucket, the reservation from then on.
func TestInvoicePreviewPricesMemoryOnce(t *testing.T) {
	oct := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	plan := []store.Plan{{Name: "starter", MemoryGiBHour: 1, EffectiveFrom: oct.AddDate(0, -1, 0)}}
	q := func(gibSeconds float64) *float64 { return &gibSeconds }
	buckets := []store.UsageBucket{
		{Project: "p", Component: "web", Metric: store.MetricMemoryUsed, PeriodStart: oct.Add(1 * time.Hour), Quantity: q(3600), Quality: store.QualityComplete},         // before the switch: priced, 1 GiB-hour
		{Project: "p", Component: "web", Metric: store.MetricMemoryUsed, PeriodStart: oct.Add(3 * time.Hour), Quantity: q(3600), Quality: store.QualityComplete},         // beside a reserved bucket: not priced
		{Project: "p", Component: "web", Metric: store.MetricMemoryReserved, PeriodStart: oct.Add(2 * time.Hour), Quantity: q(2 * 3600), Quality: store.QualityComplete}, // 2 GiB-hours
		{Project: "p", Component: "web", Metric: store.MetricMemoryReserved, PeriodStart: oct.Add(3 * time.Hour), Quantity: q(2 * 3600), Quality: store.QualityComplete}, // 2 GiB-hours
		{Project: "p", Component: "worker", Metric: store.MetricMemoryUsed, PeriodStart: oct.Add(3 * time.Hour), Quantity: q(3600), Quality: store.QualityComplete},      // a component never switched: priced
	}
	lines, total, _ := InvoicePreview(buckets, plan, oct, oct.AddDate(0, 1, 0))
	if total != 6 {
		t.Fatalf("total = %.2f, want 6.00 (1 used before the switch + 4 reserved + 1 of the worker); lines %+v", total, lines)
	}
}
