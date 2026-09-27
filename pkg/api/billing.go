package api

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/authz"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Billing API (RFC-0075): plans (operator), usage (workspace admins),
// invoice preview (workspace owners).  No money changes hands here;
// billing is the ledger layer that a payment provider will read later.

// ---- Plans (operator-level) -----------------------------------------------

// PlanView is a plan as the API returns it.
type PlanView struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	CPUHour         float64   `json:"cpuHour"`
	MemoryGiBHour   float64   `json:"memoryGibHour"`
	StorageGiBMonth float64   `json:"storageGibMonth"`
	EgressGiB       float64   `json:"egressGib"`
	MinMonthly      float64   `json:"minMonthly"`
	Currency        string    `json:"currency"`
	EffectiveFrom   time.Time `json:"effectiveFrom"`
}

func planView(p store.Plan) PlanView {
	return PlanView{ID: p.ID, Name: p.Name, CPUHour: p.CPUHour, MemoryGiBHour: p.MemoryGiBHour, StorageGiBMonth: p.StorageGiBMonth, EgressGiB: p.EgressGiB, MinMonthly: p.MinMonthly, Currency: firstNonEmpty(p.Currency, "USD"), EffectiveFrom: p.EffectiveFrom}
}

// listPlans is GET /api/cluster/plans (cluster admins).
func (s *Server) listPlans(c *gin.Context) {
	plans, err := s.store.ListPlans(c.Request.Context())
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	out := make([]PlanView, len(plans))
	for i, p := range plans {
		out[i] = planView(p)
	}
	c.JSON(http.StatusOK, out)
}

// createPlan is POST /api/cluster/plans (cluster admins).
func (s *Server) createPlan(c *gin.Context) {
	var req struct {
		Name            string     `json:"name" binding:"required"`
		CPUHour         float64    `json:"cpuHour"`
		MemoryGiBHour   float64    `json:"memoryGibHour"`
		StorageGiBMonth float64    `json:"storageGibMonth"`
		EgressGiB       float64    `json:"egressGib"`
		MinMonthly      float64    `json:"minMonthly"`
		Currency        string     `json:"currency"`
		EffectiveFrom   *time.Time `json:"effectiveFrom"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 64 {
		abort(c, http.StatusBadRequest, errors.New("name must be 1 to 64 characters"))
		return
	}
	ef := time.Now().UTC()
	if req.EffectiveFrom != nil {
		ef = *req.EffectiveFrom
	}
	p := store.Plan{Name: req.Name, CPUHour: req.CPUHour, MemoryGiBHour: req.MemoryGiBHour, StorageGiBMonth: req.StorageGiBMonth, EgressGiB: req.EgressGiB, MinMonthly: req.MinMonthly, Currency: firstNonEmpty(req.Currency, "USD"), EffectiveFrom: ef}
	created, err := s.store.CreatePlan(c.Request.Context(), p)
	if errors.Is(err, store.ErrConflict) {
		abort(c, http.StatusConflict, errors.New("a plan with that name already exists"))
		return
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	s.audit(c, "", "plan.create", created.Name, "")
	c.JSON(http.StatusCreated, planView(*created))
}

// assignPlan is POST /api/cluster/plans/:name/assign?workspace=<slug>
// (cluster admins; assigns a plan to a workspace).
func (s *Server) assignPlan(c *gin.Context) {
	ws := c.Query("workspace")
	if ws == "" {
		abort(c, http.StatusBadRequest, errors.New("workspace is required"))
		return
	}
	if _, err := s.store.Workspace(c.Request.Context(), ws); err != nil {
		storeErr(c, err, "workspace")
		return
	}
	name := c.Param("name")
	wp, err := s.store.AssignPlan(c.Request.Context(), ws, name)
	if errors.Is(err, store.ErrNotFound) {
		abort(c, http.StatusNotFound, errors.New("plan not found"))
		return
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	s.audit(c, "", "plan.assign", ws, wp.PlanName)
	c.JSON(http.StatusOK, wp)
}

// ---- Workspace billing (workspace admins) ----------------------------------

// WorkspaceBillingView is GET /api/workspace/billing/current.
type WorkspaceBillingView struct {
	Workspace  string            `json:"workspace"`
	Period     string            `json:"period"` // "2026-10" (current month)
	Plan       *PlanView         `json:"plan,omitempty"`
	Lines      []BillingLineView `json:"lines"`
	Total      float64           `json:"total"`
	Currency   string            `json:"currency"`
	Projection float64           `json:"projection"` // run-rate to month end
	Quality    string            `json:"quality"`    // complete | partial | missing
}

type BillingLineView struct {
	Component   string  `json:"component"`
	Metric      string  `json:"metric"`
	Quantity    float64 `json:"quantity"`
	Unit        string  `json:"unit"`
	UnitPrice   float64 `json:"unitPrice"`
	GrossAmount float64 `json:"grossAmount"`
}

// workspaceBillingCurrent is GET /api/workspace/billing/current.
func (s *Server) workspaceBillingCurrent(c *gin.Context) {
	ws := s.workspace(c)
	ctx := c.Request.Context()
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)

	wp, _ := s.store.WorkspacePlan(ctx, ws)
	var plan *store.Plan
	if wp != nil {
		if p, err := s.store.GetPlan(ctx, wp.PlanID); err == nil {
			plan = p
		}
	}

	buckets, err := s.store.QueryBuckets(ctx, ws, "", monthStart, now)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}

	lines, total, quality := computeInvoicePreview(buckets, plan, monthStart, now)
	// Simple run-rate projection.
	elapsed := now.Sub(monthStart).Hours()
	total_h := monthEnd.Sub(monthStart).Hours()
	projection := 0.0
	if elapsed > 0 {
		projection = total / elapsed * total_h
	}

	out := WorkspaceBillingView{
		Workspace: ws, Period: now.Format("2006-01"), Total: total,
		Currency: "USD", Projection: projection, Quality: quality, Lines: lines,
	}
	if plan != nil {
		pv := planView(*plan)
		out.Plan = &pv
	}
	c.JSON(http.StatusOK, out)
}

// workspaceBillingInvoices is GET /api/workspace/billing/invoices?month=2026-09.
func (s *Server) workspaceBillingInvoices(c *gin.Context) {
	ws := s.workspace(c)
	ctx := c.Request.Context()
	month := c.Query("month")
	var from, to time.Time
	if month == "" {
		now := time.Now().UTC()
		to = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		from = to.AddDate(0, -6, 0)
	} else {
		t, err := time.Parse("2006-01", month)
		if err != nil {
			abort(c, http.StatusBadRequest, errors.New("month must be YYYY-MM"))
			return
		}
		from = t
		to = t.AddDate(0, 1, 0)
	}
	lines, err := s.store.QueryInvoiceLines(ctx, ws, from, to, nil)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, lines)
}

// workspaceUsage is GET /api/workspace/usage (and /api/projects/:slug/usage).
func (s *Server) workspaceUsage(c *gin.Context) {
	ws := s.workspace(c)
	project := c.Query("project")
	ctx := c.Request.Context()

	to := time.Now().UTC()
	from := to.AddDate(0, -1, 0)
	if f := c.Query("from"); f != "" {
		if t, err := time.Parse(time.RFC3339, f); err == nil {
			from = t
		}
	}
	if t := c.Query("to"); t != "" {
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			to = parsed
		}
	}

	buckets, err := s.store.QueryBuckets(ctx, ws, project, from, to)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, buckets)
}

// projectUsage is GET /api/projects/:slug/usage.
func (s *Server) projectUsage(c *gin.Context) {
	c.Request.URL.RawQuery += "&project=" + c.Param("slug")
	s.workspaceUsage(c)
}

// ---- Operator economics (cluster admins only) ------------------------------

// clusterEconomics is GET /api/cluster/economics?month=2026-10.
func (s *Server) clusterEconomics(c *gin.Context) {
	ctx := c.Request.Context()
	month := c.DefaultQuery("month", time.Now().UTC().Format("2006-01"))
	t, err := time.Parse("2006-01", month)
	if err != nil {
		abort(c, http.StatusBadRequest, errors.New("month must be YYYY-MM"))
		return
	}
	from := t
	to := t.AddDate(0, 1, 0)

	workspaces, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}

	type wsEcon struct {
		Workspace   string  `json:"workspace"`
		Revenue     float64 `json:"revenue"` // billed to the customer
		DirectCOGS  float64 `json:"directCogs"`
		SharedCOGS  float64 `json:"sharedCogs"`
		IdleCOGS    float64 `json:"idleCogs"`
		TotalCOGS   float64 `json:"totalCogs"`
		GrossMargin float64 `json:"grossMargin"`
		MarginPct   float64 `json:"marginPct"`
	}
	var rows []wsEcon
	var totRevenue, totCOGS float64
	for _, ws := range workspaces {
		lines, _ := s.store.QueryInvoiceLines(ctx, ws.Slug, from, to, nil)
		cogs, _ := s.store.QueryCOGSBuckets(ctx, ws.Slug, from, to)
		rev := 0.0
		for _, l := range lines {
			rev += l.GrossAmount
		}
		direct, shared, idle, total := 0.0, 0.0, 0.0, 0.0
		for _, b := range cogs {
			direct += b.CPUCost + b.MemoryCost + b.StorageCost + b.NetworkCost
			shared += b.SharedCost
			idle += b.IdleCost
			total += b.TotalCost
		}
		margin := rev - total
		pct := 0.0
		if rev > 0 {
			pct = margin / rev * 100
		}
		rows = append(rows, wsEcon{Workspace: ws.Slug, Revenue: rev, DirectCOGS: direct, SharedCOGS: shared, IdleCOGS: idle, TotalCOGS: total, GrossMargin: margin, MarginPct: pct})
		totRevenue += rev
		totCOGS += total
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Workspace < rows[j].Workspace })
	totMargin := totRevenue - totCOGS
	totMarginPct := 0.0
	if totRevenue > 0 {
		totMarginPct = totMargin / totRevenue * 100
	}
	c.JSON(http.StatusOK, gin.H{
		"month": month, "workspaces": rows,
		"totals": gin.H{"revenue": totRevenue, "cogs": totCOGS, "grossMargin": totMargin, "marginPct": totMarginPct},
	})
}

// ---- computeInvoicePreview -----------------------------------------------

// computeInvoicePreview sums usage buckets at plan prices for a month-to-date
// preview. When there is no plan, amounts are zero and unit prices are zero.
func computeInvoicePreview(buckets []store.UsageBucket, plan *store.Plan, from, to time.Time) ([]BillingLineView, float64, string) {
	// Aggregate by (component, metric).
	type key struct{ component, metric string }
	totals := map[key]float64{}
	qualities := map[key]string{}
	for _, b := range buckets {
		if b.Quantity == nil {
			qualities[key{b.Component, b.Metric}] = store.QualityMissing
			continue
		}
		totals[key{b.Component, b.Metric}] += *b.Quantity
		if qualities[key{b.Component, b.Metric}] != store.QualityMissing {
			qualities[key{b.Component, b.Metric}] = b.Quality
		}
	}

	// Compute amounts.
	var lines []BillingLineView
	totalAmount := 0.0
	worstQuality := store.QualityComplete
	for k, qty := range totals {
		up, unit := unitPriceFor(k.metric, plan)
		// Convert raw units to billing units.
		qty = convertUnits(qty, k.metric)
		amount := qty * up
		lines = append(lines, BillingLineView{Component: k.component, Metric: k.metric, Quantity: qty, Unit: unit, UnitPrice: up, GrossAmount: amount})
		totalAmount += amount
		q := qualities[k]
		if q == store.QualityMissing || (q == store.QualityPartial && worstQuality == store.QualityComplete) {
			worstQuality = q
		}
	}
	// Apply minimum monthly.
	if plan != nil && plan.MinMonthly > 0 && totalAmount < plan.MinMonthly {
		diff := plan.MinMonthly - totalAmount
		lines = append(lines, BillingLineView{Component: "minimum", Metric: "min_monthly", Quantity: 1, Unit: "month", UnitPrice: diff, GrossAmount: diff})
		totalAmount = plan.MinMonthly
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].Component+lines[i].Metric < lines[j].Component+lines[j].Metric })
	return lines, totalAmount, worstQuality
}

// unitPriceFor returns the price per billing unit and the billing unit name.
func unitPriceFor(metric string, plan *store.Plan) (float64, string) {
	if plan == nil {
		return 0, ""
	}
	switch metric {
	case store.MetricCPUUsed:
		return plan.CPUHour, "core-hours"
	case store.MetricMemoryUsed:
		return plan.MemoryGiBHour, "GiB-hours"
	case store.MetricStorage:
		return plan.StorageGiBMonth / (30 * 24), "GiB-hours" // per-hour storage
	case store.MetricEgressHTTP:
		return plan.EgressGiB / (1024 * 1024 * 1024), "bytes"
	}
	return 0, ""
}

// convertUnits converts from the ledger unit to the billing unit.
func convertUnits(qty float64, metric string) float64 {
	switch metric {
	case store.MetricCPUUsed, store.MetricCPUReserved:
		return qty / 3600 // core-seconds → core-hours
	case store.MetricMemoryUsed, store.MetricStorage:
		return qty / 3600 // GiB-seconds → GiB-hours
	}
	return qty
}

// ---- usage write (internal) ------------------------------------------------

// WriteSleepEvent records a sleep or wake event from the controller.
// This is called by the sleep reconciler, not by an API handler.
func (s *Server) recordSleepEvent(ws, project, component, event string, durationSec *int) {
	if s.store == nil {
		return
	}
	_ = s.store.WriteSleepEvent(nil, store.SleepEvent{WorkspaceID: ws, Project: project, Component: component, Event: event, DurationSeconds: durationSec})
}

// ---- authorization guard ---------------------------------------------------

// requireOwnerOrAdmin is used for billing endpoints: workspace admins can
// see usage, owners see invoice preview.
func (s *Server) billingAdmin(c *gin.Context) {
	roles, err := s.rolesOf(c)
	if err != nil || !roles.Can(authz.ClusterAdmin, "") {
		id, _ := ext.IdentityFrom(c)
		if id.Email == "" {
			abort(c, http.StatusForbidden, errors.New("sign in to view billing"))
			return
		}
		abort(c, http.StatusForbidden, errors.New("workspace admins can view billing"))
		return
	}
	c.Next()
}
