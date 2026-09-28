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
	// Project is the slug the usage belongs to; "minimum" lines have none.
	Project     string  `json:"project,omitempty"`
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

	if lines == nil {
		lines = []BillingLineView{}
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
	rows := []wsEcon{}
	var totRevenue, totCOGS, totDirect, totShared, totIdle float64
	now := time.Now().UTC()
	for _, ws := range workspaces {
		lines, _ := s.store.QueryInvoiceLines(ctx, ws.Slug, from, to, nil)
		cogs, _ := s.store.QueryCOGSBuckets(ctx, ws.Slug, from, to)
		rev := 0.0
		for _, l := range lines {
			rev += l.GrossAmount
		}
		if len(lines) == 0 {
			// No finalised invoice lines for the month (the finalisation
			// job is not written yet): revenue is the same preview the
			// workspace's Billing card shows, from the ledger at plan prices.
			end := to
			if now.Before(end) {
				end = now
			}
			var plan *store.Plan
			if wp, err := s.store.WorkspacePlan(ctx, ws.Slug); err == nil && wp != nil {
				plan, _ = s.store.GetPlan(ctx, wp.PlanID)
			}
			if buckets, err := s.store.QueryBuckets(ctx, ws.Slug, "", from, end); err == nil {
				_, rev, _ = computeInvoicePreview(buckets, plan, from, end)
			}
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
		totDirect += direct
		totShared += shared
		totIdle += idle
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Workspace < rows[j].Workspace })
	totMargin := totRevenue - totCOGS
	totMarginPct := 0.0
	if totRevenue > 0 {
		totMarginPct = totMargin / totRevenue * 100
	}
	// Totals carry the same field names as rows so clients render both alike.
	c.JSON(http.StatusOK, gin.H{
		"month": month, "workspaces": rows,
		"totals": wsEcon{
			Revenue: totRevenue, DirectCOGS: totDirect, SharedCOGS: totShared, IdleCOGS: totIdle,
			TotalCOGS: totCOGS, GrossMargin: totMargin, MarginPct: totMarginPct,
		},
	})
}

// ---- computeInvoicePreview -----------------------------------------------

// computeInvoicePreview sums usage buckets at plan prices for a month-to-date
// preview, one line per (project, component, metric) — the shape a customer
// reads: their project, then what in it (web, postgres/db, build-cache, …).
// Zero-quantity lines are dropped; a missing bucket still lowers the
// quality. When there is no plan, amounts and unit prices are zero.
func computeInvoicePreview(buckets []store.UsageBucket, plan *store.Plan, from, to time.Time) ([]BillingLineView, float64, string) {
	type key struct{ project, component, metric string }
	totals := map[key]float64{}
	qualities := map[key]string{}
	for _, b := range buckets {
		k := key{b.Project, b.Component, b.Metric}
		if b.Quantity == nil {
			qualities[k] = store.QualityMissing
			continue
		}
		totals[k] += *b.Quantity
		if qualities[k] != store.QualityMissing {
			qualities[k] = b.Quality
		}
	}

	var lines []BillingLineView
	totalAmount := 0.0
	worstQuality := store.QualityComplete
	for _, q := range qualities {
		if q == store.QualityMissing || (q == store.QualityPartial && worstQuality == store.QualityComplete) {
			worstQuality = q
		}
	}
	for k, qty := range totals {
		if qty <= 0 {
			continue
		}
		up, unit := unitPriceFor(k.metric, plan)
		qty = convertUnits(qty, k.metric)
		amount := qty * up
		lines = append(lines, BillingLineView{Project: k.project, Component: k.component, Metric: k.metric, Quantity: qty, Unit: unit, UnitPrice: up, GrossAmount: amount})
		totalAmount += amount
	}
	// Apply the plan's minimum monthly (workspace level, RFC-0075 open question 3).
	if plan != nil && plan.MinMonthly > 0 && totalAmount < plan.MinMonthly {
		diff := plan.MinMonthly - totalAmount
		lines = append(lines, BillingLineView{Component: "minimum", Metric: "min_monthly", Quantity: 1, Unit: "month", UnitPrice: diff, GrossAmount: diff})
		totalAmount = plan.MinMonthly
	}
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].Project != lines[j].Project {
			return lines[i].Project < lines[j].Project
		}
		if lines[i].Component != lines[j].Component {
			return lines[i].Component < lines[j].Component
		}
		return lines[i].Metric < lines[j].Metric
	})
	return lines, totalAmount, worstQuality
}

// hoursPerMonth is the billing month used to turn a GiB-month price into
// the metered GiB-seconds (30-day month, the industry convention).
const hoursPerMonth = 30 * 24

// unitPriceFor returns the price per billing unit and the billing unit's
// name. The unit is named even without a plan, so a preview reads well.
func unitPriceFor(metric string, plan *store.Plan) (float64, string) {
	var price float64
	unit := ""
	switch metric {
	case store.MetricCPUUsed, store.MetricCPUReserved:
		unit = "core-hours"
		if plan != nil {
			price = plan.CPUHour
		}
	case store.MetricMemoryUsed:
		unit = "GiB-hours"
		if plan != nil {
			price = plan.MemoryGiBHour
		}
	case store.MetricStorage:
		unit = "GiB-months"
		if plan != nil {
			price = plan.StorageGiBMonth
		}
	case store.MetricEgressHTTP:
		unit = "GiB"
		if plan != nil {
			price = plan.EgressGiB
		}
	case store.MetricInstanceSec:
		unit = "instance-hours"
	}
	return price, unit
}

// convertUnits converts from the ledger unit to the billing unit named by
// unitPriceFor.
func convertUnits(qty float64, metric string) float64 {
	switch metric {
	case store.MetricCPUUsed, store.MetricCPUReserved, store.MetricMemoryUsed, store.MetricInstanceSec:
		return qty / 3600 // core-seconds, GiB-seconds, seconds → hours
	case store.MetricStorage:
		return qty / 3600 / hoursPerMonth // GiB-seconds → GiB-months
	case store.MetricEgressHTTP:
		return qty / (1024 * 1024 * 1024) // bytes → GiB
	}
	return qty
}

// ---- usage write (internal) ------------------------------------------------

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
