package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/api"
)

// ---- shpyrd billing --------------------------------------------------------

// newBillingCmd returns `shpyrd billing`: the workspace's usage and
// estimated invoice at plan prices (RFC-0075). No money changes hands.
func newBillingCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "billing",
		Short: "Usage and estimated cost at plan prices for this workspace",
		Long: `Month-to-date usage and the estimated invoice at your plan's prices.
No money changes hands through this command — billing is informational until
a payment provider is connected.

  shpyrd billing              # current month
  shpyrd billing --month 2026-09   # past month`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			month, _ := cmd.Flags().GetString("month")
			path := "api/workspace/billing/current"
			if month != "" {
				path = "api/workspace/billing/invoices?" + url.Values{"month": {month}}.Encode()
			}
			var raw json.RawMessage
			if err := t.call(ctx, "GET", path, nil, &raw); err != nil {
				return err
			}
			if month != "" {
				lines := []api.InvoiceLine{}
				if err := json.Unmarshal(raw, &lines); err != nil {
					return err
				}
				return g.print(cmd, lines, func(w io.Writer) { printInvoiceLines(w, lines) })
			}
			var view api.WorkspaceBillingView
			if err := json.Unmarshal(raw, &view); err != nil {
				return err
			}
			return g.print(cmd, view, func(w io.Writer) { printBillingView(w, view) })
		},
	}
	cmd.Flags().String("month", "", "show a past month (YYYY-MM)")
	return cmd
}

func printBillingView(out io.Writer, v api.WorkspaceBillingView) {
	planName := "(no plan)"
	if v.Plan != nil {
		planName = v.Plan.Name
	}
	fmt.Fprintf(out, "Workspace:  %s\nPeriod:     %s\nPlan:       %s\nQuality:    %s\n\n", v.Workspace, v.Period, planName, v.Quality)
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PROJECT\tCOMPONENT\tMETRIC\tQUANTITY\tUNIT\tUNIT PRICE\tESTIMATED")
	// Lines arrive sorted by project; a subtotal closes each project.
	project, subtotal := "", 0.0
	flush := func() {
		if project != "" {
			fmt.Fprintf(tw, "\t\t\t\t\t%s\t%.4f %s\n", project, subtotal, v.Currency)
		}
	}
	for _, l := range v.Lines {
		if l.Project != project {
			flush()
			project, subtotal = l.Project, 0
		}
		name := l.Project
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%.4f\t%s\t%.6f\t%.4f %s\n", name, l.Component, l.Metric, l.Quantity, l.Unit, l.UnitPrice, l.GrossAmount, v.Currency)
		subtotal += l.GrossAmount
	}
	flush()
	_ = tw.Flush()
	fmt.Fprintf(out, "\nEstimated this month:  %.4f %s\n", v.Total, v.Currency)
	fmt.Fprintf(out, "Projected month total: %.4f %s\n", v.Projection, v.Currency)
	fmt.Fprintln(out, "\n(Estimate only; no money is owed. Billing is activated when a payment provider is configured.)")
}

func printInvoiceLines(w io.Writer, lines []api.InvoiceLine) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PERIOD\tCOMPONENT\tMETRIC\tQUANTITY\tAMOUNT\tFINALIZED")
	for _, l := range lines {
		fin := "no"
		if l.Finalized {
			fin = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%.4f\t%.4f\t%s\n", l.PeriodStart.Format("2006-01"), l.Component, l.Metric, l.Quantity, l.GrossAmount, fin)
	}
	_ = tw.Flush()
}

// ---- shpyrd-ctl plans (operator) -------------------------------------------

// newPlansCmd returns `shpyrd-ctl plans`: create and assign billing plans.
func newPlansCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plans",
		Short: "Billing plans: price per CPU-hour, memory, storage and egress",
		Long: `Billing plans define the per-unit prices a workspace is charged at.
Plans are platform-level; the cloud assigns them to workspaces.

  shpyrd-ctl plans list
  shpyrd-ctl plans create starter --cpu-hour 0.02 --memory-gib-hour 0.003 \
      --storage-gib-month 0.10 --egress-gib 0.05 --min-monthly 5.00
  shpyrd-ctl plans assign starter --workspace demo`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return plansList(cmd, g) },
	}
	cmd.AddCommand(newPlansListCmd(g), newPlansCreateCmd(g), newPlansUpdateCmd(g), newPlansHistoryCmd(g), newPlansAssignCmd(g))
	return cmd
}

func plansList(cmd *cobra.Command, g *globalFlags) error {
	ctx := signalContext()
	t, err := newTeamsAPI(g)
	if err != nil {
		return err
	}
	var plans []api.PlanView
	if err := t.call(ctx, "GET", "api/cluster/plans", nil, &plans); err != nil {
		return err
	}
	if plans == nil {
		plans = []api.PlanView{}
	}
	return g.print(cmd, plans, func(w io.Writer) {
		if len(plans) == 0 {
			fmt.Fprintln(w, "No plans. Create one: shpyrd-ctl plans create starter --cpu-hour 0.02")
			return
		}
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tFREE\tCPU/CORE-H\tMEM/GIB-H\tSTORAGE/GIB-MO\tEGRESS/GIB\tMIN/MO\tBUDGET/MO\tCOST BUDGET/MO\tCURRENCY\tSLEEP DEFAULT\tDB SLEEP\tSELF-SERVE\tLIMITS")
		for _, p := range plans {
			sleep := "-"
			if p.SleepAfter != "" {
				sleep = p.SleepAfter + " " + firstNonEmpty(p.SleepResuming, "wait")
			}
			budget, costBudget, selfServe, free, limits := "-", "-", "no", "no", "-"
			if p.MonthlyBudget > 0 {
				budget = fmt.Sprintf("%.2f", p.MonthlyBudget)
			}
			if p.CostBudget > 0 {
				costBudget = fmt.Sprintf("%.2f", p.CostBudget)
			}
			if p.Free {
				free = "yes"
			}
			if p.SelfServe {
				selfServe = "yes"
			}
			if p.Limits != nil {
				limits = limitsString(p.Limits)
			}
			fmt.Fprintf(tw, "%s\t%s\t%.6f\t%.6f\t%.6f\t%.6f\t%.2f\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p.Name, free, p.CPUHour, p.MemoryGiBHour, p.StorageGiBMonth, p.EgressGiB, p.MinMonthly, budget, costBudget, p.Currency, sleep, firstNonEmpty(p.PostgresSleepAfter, "-"), selfServe, limits)
		}
		_ = tw.Flush()
	})
}

func newPlansListCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use: "list", Aliases: []string{"ls"}, Short: "List billing plans",
		RunE: func(cmd *cobra.Command, args []string) error { return plansList(cmd, g) },
	}
}

func newPlansCreateCmd(g *globalFlags) *cobra.Command {
	var cpuHour, memGiBHour, storageGiBMonth, egressGiB, minMonthly, monthlyBudget, costBudget float64
	var currency, effectiveFrom, sleepAfter, sleepResuming, pgSleepAfter string
	var selfServe, free bool
	var limits limitFlags
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a billing plan",
		Long: `Create a billing plan: the unit prices a workspace is charged at, the plan's
default sleep for HTTP apps and for databases (RFC-0075), a monthly budget, the
ceilings a workspace starts with, and whether people may pick the plan when
they sign up. Projects on a plan with --sleep-after inherit it unless they set
their own policy ('shpyrd sleep <project> --after off' opts out); databases
inherit --postgres-sleep-after the same way ('shpyrd pg sleep'). With a
budget, the workspace's owners are warned at 80% of it and the workspace is
paused at 100% until the month ends.

  shpyrd-ctl plans create starter --cpu-hour 0.02 --memory-gib-hour 0.005 \
      --storage-gib-month 0.10 --egress-gib 0.05 \
      --sleep-after 15m --sleep-resuming page
  shpyrd-ctl plans create free --free --cost-budget 0.50 --self-serve \
      --projects 1 --instances 1 --memory 256Mi \
      --sleep-after 10m --sleep-resuming page --postgres-sleep-after 10m

A free plan (--free) charges nothing: the Billing card shows the consumption
with nothing to pay. --cost-budget caps what a workspace on the plan may cost
the platform in a month (OpenCost's cost, the operator's number, shown
nowhere); --monthly-budget caps the month at the plan's prices. Either cap
warns the owners at 80% and pauses the workspace at 100% until the month ends.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			body := map[string]any{
				"name":    strings.TrimSpace(args[0]),
				"cpuHour": cpuHour, "memoryGibHour": memGiBHour,
				"storageGibMonth": storageGiBMonth, "egressGib": egressGiB,
				"minMonthly": minMonthly, "currency": firstNonEmpty(currency, "USD"),
				"sleepAfter": sleepAfter, "sleepResuming": sleepResuming,
				"postgresSleepAfter": pgSleepAfter, "monthlyBudget": monthlyBudget, "costBudget": costBudget, "selfServe": selfServe, "free": free,
			}
			if l := limits.limits(); l != nil {
				body["limits"] = l
			}
			if effectiveFrom != "" {
				t, err := time.Parse("2006-01-02", effectiveFrom)
				if err != nil {
					return fmt.Errorf("--effective-from: %w", err)
				}
				body["effectiveFrom"] = t
			}
			var p api.PlanView
			if err := t.call(ctx, "POST", "api/cluster/plans", body, &p); err != nil {
				return err
			}
			return g.print(cmd, p, func(w io.Writer) {
				fmt.Fprintf(w, "Plan %s created.\n", p.Name)
				if p.SleepAfter != "" {
					fmt.Fprintf(w, "Its projects sleep after %s (%s mode) unless they say otherwise.\n", p.SleepAfter, firstNonEmpty(p.SleepResuming, "wait"))
				}
				if p.PostgresSleepAfter != "" {
					fmt.Fprintf(w, "Its databases sleep after %s idle unless they say otherwise.\n", p.PostgresSleepAfter)
				}
				if p.Free {
					fmt.Fprintln(w, "A free plan: nothing to pay; the Billing card shows the consumption alone.")
				}
				if p.MonthlyBudget > 0 {
					fmt.Fprintf(w, "A month is capped at %.2f %s at the plan's prices: owners are warned at 80%%, the workspace is paused at 100%% until the month ends.\n", p.MonthlyBudget, p.Currency)
				}
				if p.CostBudget > 0 {
					fmt.Fprintf(w, "A month is capped at a cost of %.2f %s to the platform (OpenCost); the same warning and pause. Shown nowhere.\n", p.CostBudget, p.Currency)
				}
				if p.SelfServe {
					fmt.Fprintln(w, "People may pick it when they sign up.")
				}
				if p.Limits != nil {
					fmt.Fprintf(w, "Ceilings: %s, for every workspace on it without ceilings of its own.\n", limitsString(p.Limits))
				}
			})
		},
	}
	cmd.Flags().Float64Var(&cpuHour, "cpu-hour", 0, "price per core-hour of actual CPU use")
	cmd.Flags().Float64Var(&memGiBHour, "memory-gib-hour", 0, "price per GiB-hour of working-set memory")
	cmd.Flags().Float64Var(&storageGiBMonth, "storage-gib-month", 0, "price per GiB-month of provisioned storage")
	cmd.Flags().Float64Var(&egressGiB, "egress-gib", 0, "price per GiB of HTTP egress")
	cmd.Flags().Float64Var(&minMonthly, "min-monthly", 0, "minimum charge per month (workspace floor)")
	cmd.Flags().StringVar(&currency, "currency", "USD", "three-letter currency code")
	cmd.Flags().StringVar(&effectiveFrom, "effective-from", "", "date the plan is effective from (YYYY-MM-DD)")
	cmd.Flags().StringVar(&sleepAfter, "sleep-after", "", "default quiet period before projects on this plan sleep, 5m to 24h (empty: no default)")
	cmd.Flags().StringVar(&sleepResuming, "sleep-resuming", "page", "default resuming mode for the plan's projects: page or wait")
	cmd.Flags().StringVar(&pgSleepAfter, "postgres-sleep-after", "", "default idle period before databases on this plan hibernate, 5m to 24h (empty: no default)")
	cmd.Flags().Float64Var(&monthlyBudget, "monthly-budget", 0, "cap for a month at plan prices: warn owners at 80%, pause the workspace at 100% until the month ends (0: none)")
	cmd.Flags().BoolVar(&selfServe, "self-serve", false, "people may pick this plan for themselves when they sign up")
	cmd.Flags().BoolVar(&free, "free", false, "a plan that charges nothing: the bill shows consumption with nothing to pay")
	cmd.Flags().Float64Var(&costBudget, "cost-budget", 0, "cap for a month on what a workspace may cost the platform (OpenCost's cost): warn owners at 80%, pause at 100% until the month ends; shown nowhere (0: none)")
	limits.bind(cmd)
	return cmd
}

func newPlansAssignCmd(g *globalFlags) *cobra.Command {
	var ws string
	var keepLimits bool
	cmd := &cobra.Command{
		Use:   "assign <plan-name>",
		Short: "Assign a plan to a workspace (it follows the plan's ceilings)",
		Long: `Assign a billing plan to a workspace. The workspace is held to the plan's
ceilings from then on: ceilings of its own ('shpyrd-ctl workspaces limits')
are removed, unless --keep-limits.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if ws == "" {
				return fmt.Errorf("--workspace is required")
			}
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			path := "api/cluster/plans/" + url.PathEscape(args[0]) + "/assign?workspace=" + url.QueryEscape(ws)
			if keepLimits {
				path += "&keepLimits=true"
			}
			if err := t.call(ctx, "POST", path, nil, nil); err != nil {
				return err
			}
			return g.print(cmd, map[string]any{"plan": args[0], "workspace": ws, "keepLimits": keepLimits}, func(w io.Writer) {
				if keepLimits {
					fmt.Fprintf(w, "Plan %s assigned to %s; its own ceilings, if it has some, stay.\n", args[0], ws)
					return
				}
				fmt.Fprintf(w, "Plan %s assigned to %s; the workspace follows the plan's ceilings.\n", args[0], ws)
			})
		},
	}
	cmd.Flags().StringVar(&ws, "workspace", "", "workspace slug to assign the plan to (required)")
	cmd.Flags().BoolVar(&keepLimits, "keep-limits", false, "keep the workspace's own ceilings instead of following the plan's")
	_ = cmd.MarkFlagRequired("workspace")
	return cmd
}

// ---- shpyrd-ctl economics ---------------------------------------------------

// newEconomicsCmd returns `shpyrd-ctl economics`: operator margin view.
func newEconomicsCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "economics",
		Short: "Revenue, COGS and gross margin per workspace (requires OpenCost)",
		Long: `Platform economics: what each workspace is billed at plan prices (revenue),
what it costs us to run it (COGS from OpenCost), and the gross margin.
Requires the opencost extension to be enabled for the COGS column.

  shpyrd-ctl economics
  shpyrd-ctl economics --month 2026-09`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			month, _ := cmd.Flags().GetString("month")
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			path := "api/cluster/economics"
			if month != "" {
				path += "?month=" + url.QueryEscape(month)
			}
			var raw map[string]json.RawMessage
			if err := t.call(ctx, "GET", path, nil, &raw); err != nil {
				return err
			}
			if g.out.Machine() {
				return g.print(cmd, raw, nil)
			}
			out := cmd.OutOrStdout()
			if m, ok := raw["month"]; ok {
				var s string
				_ = json.Unmarshal(m, &s)
				fmt.Fprintf(out, "Month: %s\n\n", s)
			}
			// Three COGS columns let finance separate what is directly
			// attributable to the customer (direct), overhead allocated
			// proportionally (shared), and wasted capacity that shrinks
			// as the cluster fills or autoscales (idle).
			type row struct {
				Workspace   string  `json:"workspace"`
				Owner       string  `json:"owner"`
				Revenue     float64 `json:"revenue"`
				DirectCOGS  float64 `json:"directCogs"`
				SharedCOGS  float64 `json:"sharedCogs"`
				IdleCOGS    float64 `json:"idleCogs"`
				TotalCOGS   float64 `json:"totalCogs"`
				GrossMargin float64 `json:"grossMargin"`
				MarginPct   float64 `json:"marginPct"`
			}
			if ws, ok := raw["workspaces"]; ok {
				var rows []row
				_ = json.Unmarshal(ws, &rows)
				tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				// OWN: C = customer, O = operator.
				// R/E = Revenue (customer) or - (operator, no billing).
				// D/S/I = Direct / Shared / Idle expenses.
				fmt.Fprintln(tw, "WORKSPACE\tOWN\tR/E\tD\tS\tI\tTOTAL\tM\tM%")
				var custDirect, custShared, custIdle, custTotal, custRev, custMargin float64
				var opDirect, opShared, opIdle, opTotal float64
				for _, r := range rows {
					own := "C"
					if r.Owner == "operator" {
						own = "O"
					}
					if r.Owner == "operator" {
						fmt.Fprintf(tw, "%s\t%s\t-\t%.2f\t%.2f\t%.2f\t%.2f\t-\t-\n",
							r.Workspace, own,
							r.DirectCOGS, r.SharedCOGS, r.IdleCOGS, r.TotalCOGS)
						opDirect += r.DirectCOGS
						opShared += r.SharedCOGS
						opIdle += r.IdleCOGS
						opTotal += r.TotalCOGS
					} else {
						fmt.Fprintf(tw, "%s\t%s\t%.2f\t%.2f\t%.2f\t%.2f\t%.2f\t%.2f\t%.1f%%\n",
							r.Workspace, own,
							r.Revenue, r.DirectCOGS, r.SharedCOGS, r.IdleCOGS,
							r.TotalCOGS, r.GrossMargin, r.MarginPct)
						custRev += r.Revenue
						custDirect += r.DirectCOGS
						custShared += r.SharedCOGS
						custIdle += r.IdleCOGS
						custTotal += r.TotalCOGS
						custMargin += r.GrossMargin
					}
				}
				fmt.Fprintln(tw)
				custPct := 0.0
				if custRev > 0 {
					custPct = custMargin / custRev * 100
				}
				fmt.Fprintf(tw, "Customer totals\t\t%.2f\t%.2f\t%.2f\t%.2f\t%.2f\t%.2f\t%.1f%%\n",
					custRev, custDirect, custShared, custIdle, custTotal, custMargin, custPct)
				if opTotal > 0 {
					fmt.Fprintf(tw, "Operating expenses\t\t-\t%.2f\t%.2f\t%.2f\t%.2f\t-\t-\n",
						opDirect, opShared, opIdle, opTotal)
				}
				_ = tw.Flush()
			}
			return nil
		},
	}
}

// newPlansUpdateCmd is `shpyrd-ctl plans update <name>`: the plan's prices
// and settings from a date on. A new version is written and the months
// before it keep the version they were priced at; only the flags given
// change, the rest stays as in the version in force.
func newPlansUpdateCmd(g *globalFlags) *cobra.Command {
	var cpuHour, memGiBHour, storageGiBMonth, egressGiB, minMonthly, monthlyBudget, costBudget float64
	var currency, effectiveFrom, sleepAfter, sleepResuming, pgSleepAfter string
	var selfServe, free, clearLimits bool
	var limits limitFlags
	cmd := &cobra.Command{
		Use:   "update <name> [--effective-from YYYY-MM-DD] [the flags of create]",
		Short: "Change a plan's prices or settings from a date on (a new version; past months keep theirs)",
		Long: `Add a version of a plan: its prices and settings from --effective-from
(today when not given) on. The ledger prices every five minutes of use at the
version in force at that time, so an old month is never rewritten. Only the
flags given change; everything else stays as in the version in force, the
ceilings too (--projects alone leaves --memory as it was).

The plan's ceilings apply to every workspace on it without ceilings of its
own ('shpyrd-ctl workspaces limits'): raising them raises those workspaces.

  shpyrd-ctl plans update starter --cpu-hour 0.04 --memory-gib-hour 0.03 --min-monthly 0 --effective-from 2026-11-01
  shpyrd-ctl plans history starter`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			fields := map[string]string{
				"cpu-hour": "cpuHour", "memory-gib-hour": "memoryGibHour", "storage-gib-month": "storageGibMonth",
				"egress-gib": "egressGib", "min-monthly": "minMonthly", "currency": "currency",
				"sleep-after": "sleepAfter", "sleep-resuming": "sleepResuming", "postgres-sleep-after": "postgresSleepAfter",
				"monthly-budget": "monthlyBudget", "cost-budget": "costBudget", "self-serve": "selfServe", "free": "free",
			}
			values := map[string]any{
				"cpu-hour": cpuHour, "memory-gib-hour": memGiBHour, "storage-gib-month": storageGiBMonth,
				"egress-gib": egressGiB, "min-monthly": minMonthly, "currency": currency,
				"sleep-after": sleepAfter, "sleep-resuming": sleepResuming, "postgres-sleep-after": pgSleepAfter,
				"monthly-budget": monthlyBudget, "cost-budget": costBudget, "self-serve": selfServe, "free": free,
			}
			body := map[string]any{}
			for flag, field := range fields {
				if cmd.Flags().Changed(flag) {
					body[field] = values[flag]
				}
			}
			switch l := limits.limits(); {
			case clearLimits && l != nil:
				return errors.New("--clear-limits or the limits, not both: the limits given change and the others stay; --clear-limits removes them all")
			case clearLimits:
				body["clearLimits"] = true
			case l != nil:
				body["limits"] = l
			}
			if len(body) == 0 {
				return errors.New("give what changes: a price, a budget, a sleep default, --free, --self-serve or the limits")
			}
			if effectiveFrom != "" {
				day, err := time.Parse("2006-01-02", effectiveFrom)
				if err != nil {
					return fmt.Errorf("--effective-from: %w", err)
				}
				body["effectiveFrom"] = day
			}
			var p api.PlanView
			if err := t.call(ctx, "POST", "api/cluster/plans/"+url.PathEscape(args[0])+"/versions", body, &p); err != nil {
				return err
			}
			return g.print(cmd, p, func(w io.Writer) {
				fmt.Fprintf(w, "Plan %s changes from %s: %.6f per core-hour, %.6f per GiB-hour reserved, %.6f per GiB-month, %.6f per GiB egress, floor %.2f %s.\n",
					p.Name, p.EffectiveFrom.Format("2006-01-02"), p.CPUHour, p.MemoryGiBHour, p.StorageGiBMonth, p.EgressGiB, p.MinMonthly, p.Currency)
				fmt.Fprintln(w, "Months before that date keep the prices they were billed at.")
				if p.Limits != nil {
					fmt.Fprintf(w, "Ceilings: %s, for every workspace on the plan without ceilings of its own.\n", limitsString(p.Limits))
				} else if clearLimits {
					fmt.Fprintln(w, "No ceilings: workspaces on the plan without ceilings of their own have none.")
				}
			})
		},
	}
	cmd.Flags().Float64Var(&cpuHour, "cpu-hour", 0, "price per core-hour of actual CPU use")
	cmd.Flags().Float64Var(&memGiBHour, "memory-gib-hour", 0, "price per GiB-hour of reserved memory while awake")
	cmd.Flags().Float64Var(&storageGiBMonth, "storage-gib-month", 0, "price per GiB-month of provisioned storage")
	cmd.Flags().Float64Var(&egressGiB, "egress-gib", 0, "price per GiB of HTTP egress")
	cmd.Flags().Float64Var(&minMonthly, "min-monthly", 0, "minimum charge per month (workspace floor); 0 removes it")
	cmd.Flags().StringVar(&currency, "currency", "", "three-letter currency code")
	cmd.Flags().StringVar(&effectiveFrom, "effective-from", "", "the date the version takes effect (YYYY-MM-DD; default today)")
	cmd.Flags().StringVar(&sleepAfter, "sleep-after", "", "default quiet period before projects sleep, 5m to 24h; off removes the default")
	cmd.Flags().StringVar(&sleepResuming, "sleep-resuming", "", "default resuming mode: page or wait")
	cmd.Flags().StringVar(&pgSleepAfter, "postgres-sleep-after", "", "default idle period before databases hibernate, 5m to 24h; off removes it")
	cmd.Flags().Float64Var(&monthlyBudget, "monthly-budget", 0, "cap for a month at plan prices (0: none)")
	cmd.Flags().Float64Var(&costBudget, "cost-budget", 0, "cap for a month on what a workspace may cost the platform (0: none); shown nowhere")
	cmd.Flags().BoolVar(&selfServe, "self-serve", false, "people may pick the plan when they sign up")
	cmd.Flags().BoolVar(&free, "free", false, "the plan charges nothing")
	limits.bind(cmd)
	cmd.Flags().BoolVar(&clearLimits, "clear-limits", false, "remove the plan's ceilings (not with the limit flags)")
	return cmd
}

// newPlansHistoryCmd is `shpyrd-ctl plans history <name>`: its versions.
func newPlansHistoryCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "history <name>",
		Short: "List a plan's versions, oldest first",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			var versions []api.PlanView
			if err := t.call(ctx, "GET", "api/cluster/plans/"+url.PathEscape(args[0])+"/versions", nil, &versions); err != nil {
				return err
			}
			return g.print(cmd, versions, func(w io.Writer) {
				tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "FROM\tCPU/CORE-H\tMEM/GIB-H\tSTORAGE/GIB-MO\tEGRESS/GIB\tMIN/MO\tBUDGET/MO\tCOST BUDGET/MO\tFREE\tSLEEP DEFAULT\tDB SLEEP")
				for _, p := range versions {
					sleep := "-"
					if p.SleepAfter != "" {
						sleep = p.SleepAfter + " " + firstNonEmpty(p.SleepResuming, "wait")
					}
					free := "no"
					if p.Free {
						free = "yes"
					}
					fmt.Fprintf(tw, "%s\t%.6f\t%.6f\t%.6f\t%.6f\t%.2f\t%.2f\t%.2f\t%s\t%s\t%s\n", p.EffectiveFrom.Format("2006-01-02"), p.CPUHour, p.MemoryGiBHour, p.StorageGiBMonth, p.EgressGiB, p.MinMonthly, p.MonthlyBudget, p.CostBudget, free, sleep, firstNonEmpty(p.PostgresSleepAfter, "-"))
				}
				_ = tw.Flush()
			})
		},
	}
}
