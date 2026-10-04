//go:build !foss

package licensing

import (
	"context"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Report is what the cluster used and cost over a window, summed: sent
// with every online renewal, for the contracts priced on it (a share of
// the cloud's cost, say).
type Report struct {
	Cluster    string    `json:"cluster"`
	From       time.Time `json:"from"`
	To         time.Time `json:"to"`
	Workspaces int       `json:"workspaces"`
	Usage      UsageSum  `json:"usage"`
	// Costs are summed by kind (estimated: OpenCost's; real: the
	// provider's bill) and currency, as the costs collector wrote them;
	// none without it.
	Costs []CostSum `json:"costs"`
}

// UsageSum is the platform's metering, in billed units.
type UsageSum struct {
	CPUCoreHours     float64 `json:"cpuCoreHours"`
	MemoryGiBHours   float64 `json:"memoryGibHours"`
	StorageGiBMonths float64 `json:"storageGibMonths"`
	EgressGiB        float64 `json:"egressGib"`
}

// CostSum is the cost lines of one kind and currency, added up.
type CostSum struct {
	Kind     string  `json:"kind"`
	Currency string  `json:"currency"`
	Amount   float64 `json:"amount"`
	Lines    int     `json:"lines"`
}

// The units of the sums: a GiB-month is 720 GiB-hours, as billing counts.
const (
	secondsPerHour = 3600
	hoursPerMonth  = 720
	bytesPerGiB    = 1 << 30
	reportWindow   = 30 * 24 * time.Hour
)

// BuildReport sums what every workspace used and what the cluster cost
// between from and to.
func BuildReport(ctx context.Context, st store.Store, cluster string, from, to time.Time) (Report, error) {
	r := Report{Cluster: cluster, From: from.UTC(), To: to.UTC(), Costs: []CostSum{}}
	all, err := st.ListWorkspaces(ctx)
	if err != nil {
		return r, err
	}
	r.Workspaces = len(all)
	for _, w := range all {
		buckets, err := st.QueryBuckets(ctx, w.Slug, "", from, to)
		if err != nil {
			return r, err
		}
		for _, b := range buckets {
			if b.Quantity == nil || b.PeriodStart.Before(from) || !b.PeriodStart.Before(to) {
				continue
			}
			q := *b.Quantity
			switch b.Metric {
			case store.MetricCPUUsed:
				r.Usage.CPUCoreHours += q / secondsPerHour
			case store.MetricMemoryReserved, store.MetricMemoryUsed:
				r.Usage.MemoryGiBHours += q / secondsPerHour
			case store.MetricStorage:
				r.Usage.StorageGiBMonths += q / secondsPerHour / hoursPerMonth
			case store.MetricEgressHTTP:
				r.Usage.EgressGiB += q / bytesPerGiB
			}
		}
	}
	type key struct{ kind, currency string }
	sums := map[key]*CostSum{}
	var order []key
	for _, kind := range []string{store.CostEstimated, store.CostReal} {
		lines, err := st.QueryCostLines(ctx, store.CostQuery{From: from, To: to, Kind: kind})
		if err != nil {
			return r, err
		}
		for _, l := range lines {
			if l.Cost == nil {
				continue
			}
			k := key{kind, l.Currency}
			if sums[k] == nil {
				sums[k] = &CostSum{Kind: kind, Currency: l.Currency}
				order = append(order, k)
			}
			sums[k].Amount += *l.Cost
			sums[k].Lines++
		}
	}
	for _, k := range order {
		r.Costs = append(r.Costs, *sums[k])
	}
	return r, nil
}
