package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
)

// The usage ledger, read by the workspace (storage plan step 4): what the
// metering loop measured, summed per project, component and metric over
// a period, in the units the billing drain receives, so the two can be
// held against each other.

// UsageLine is one sum of the ledger.
type UsageLine struct {
	Project   string  `json:"project"`
	Component string  `json:"component"`
	Metric    string  `json:"metric"`
	Unit      string  `json:"unit"`
	Quantity  float64 `json:"quantity"`
	// Buckets is how many ledger rows the sum covers; Partial says some
	// of them were incomplete.
	Buckets int  `json:"buckets"`
	Partial bool `json:"partial,omitempty"`
}

// UsageView is GET /api/workspace/usage.
type UsageView struct {
	From  time.Time   `json:"from"`
	To    time.Time   `json:"to"`
	Lines []UsageLine `json:"lines"`
}

// workspaceUsageLedger is GET /api/workspace/usage?from=&to=: the last
// 24 hours when no period is given; at most 31 days.
func (s *Server) workspaceUsageLedger(c *gin.Context) {
	to := time.Now().UTC()
	from := to.Add(-24 * time.Hour)
	if raw := c.Query("to"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			abort(c, http.StatusBadRequest, errBadPeriod)
			return
		}
		to = t
	}
	if raw := c.Query("from"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			abort(c, http.StatusBadRequest, errBadPeriod)
			return
		}
		from = t
	}
	if !from.Before(to) || to.Sub(from) > 31*24*time.Hour {
		abort(c, http.StatusBadRequest, errBadPeriod)
		return
	}
	buckets, err := s.store.QueryBuckets(c.Request.Context(), s.workspace(c), c.Query("project"), from, to)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	type key struct{ project, component, metric, unit string }
	sums := map[key]*UsageLine{}
	for _, b := range buckets {
		k := key{b.Project, b.Component, b.Metric, b.Unit}
		line := sums[k]
		if line == nil {
			line = &UsageLine{Project: b.Project, Component: b.Component, Metric: b.Metric, Unit: b.Unit}
			sums[k] = line
		}
		line.Buckets++
		if b.Quantity != nil {
			line.Quantity += *b.Quantity
		}
		if b.Quality != "complete" {
			line.Partial = true
		}
	}
	out := UsageView{From: from, To: to, Lines: []UsageLine{}}
	for _, line := range sums {
		out.Lines = append(out.Lines, *line)
	}
	sort.Slice(out.Lines, func(i, j int) bool {
		a, b := out.Lines[i], out.Lines[j]
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		if a.Component != b.Component {
			return a.Component < b.Component
		}
		return a.Metric < b.Metric
	})
	c.JSON(http.StatusOK, out)
}

var errBadPeriod = &userError{errorString("from and to must be RFC 3339 times, from before to, at most 31 days apart")}

type errorString string

func (e errorString) Error() string { return string(e) }
