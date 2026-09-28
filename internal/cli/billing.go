package cli

import (
	"encoding/json"
	"fmt"
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
				var lines []api.InvoiceLine
				if err := json.Unmarshal(raw, &lines); err != nil {
					return err
				}
				printInvoiceLines(cmd, lines)
				return nil
			}
			var view api.WorkspaceBillingView
			if err := json.Unmarshal(raw, &view); err != nil {
				return err
			}
			printBillingView(cmd, view)
			return nil
		},
	}
	cmd.Flags().String("month", "", "show a past month (YYYY-MM)")
	return cmd
}

func printBillingView(cmd *cobra.Command, v api.WorkspaceBillingView) {
	out := cmd.OutOrStdout()
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

func printInvoiceLines(cmd *cobra.Command, lines []api.InvoiceLine) {
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
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
	cmd.AddCommand(newPlansListCmd(g), newPlansCreateCmd(g), newPlansAssignCmd(g))
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
	if len(plans) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No plans. Create one: shpyrd-ctl plans create starter --cpu-hour 0.02")
		return nil
	}
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tCPU/CORE-H\tMEM/GIB-H\tSTORAGE/GIB-MO\tEGRESS/GIB\tMIN/MO\tCURRENCY")
	for _, p := range plans {
		fmt.Fprintf(tw, "%s\t%.6f\t%.6f\t%.6f\t%.6f\t%.2f\t%s\n", p.Name, p.CPUHour, p.MemoryGiBHour, p.StorageGiBMonth, p.EgressGiB, p.MinMonthly, p.Currency)
	}
	return tw.Flush()
}

func newPlansListCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use: "list", Aliases: []string{"ls"}, Short: "List billing plans",
		RunE: func(cmd *cobra.Command, args []string) error { return plansList(cmd, g) },
	}
}

func newPlansCreateCmd(g *globalFlags) *cobra.Command {
	var cpuHour, memGiBHour, storageGiBMonth, egressGiB, minMonthly float64
	var currency, effectiveFrom string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a billing plan",
		Args:  cobra.ExactArgs(1),
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
			fmt.Fprintf(cmd.OutOrStdout(), "Plan %s created.\n", p.Name)
			return nil
		},
	}
	cmd.Flags().Float64Var(&cpuHour, "cpu-hour", 0, "price per core-hour of actual CPU use")
	cmd.Flags().Float64Var(&memGiBHour, "memory-gib-hour", 0, "price per GiB-hour of working-set memory")
	cmd.Flags().Float64Var(&storageGiBMonth, "storage-gib-month", 0, "price per GiB-month of provisioned storage")
	cmd.Flags().Float64Var(&egressGiB, "egress-gib", 0, "price per GiB of HTTP egress")
	cmd.Flags().Float64Var(&minMonthly, "min-monthly", 0, "minimum charge per month (workspace floor)")
	cmd.Flags().StringVar(&currency, "currency", "USD", "three-letter currency code")
	cmd.Flags().StringVar(&effectiveFrom, "effective-from", "", "date the plan is effective from (YYYY-MM-DD)")
	return cmd
}

func newPlansAssignCmd(g *globalFlags) *cobra.Command {
	var ws string
	cmd := &cobra.Command{
		Use:   "assign <plan-name>",
		Short: "Assign a plan to a workspace",
		Args:  cobra.ExactArgs(1),
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
			if err := t.call(ctx, "POST", path, nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Plan %s assigned to %s.\n", args[0], ws)
			return nil
		},
	}
	cmd.Flags().StringVar(&ws, "workspace", "", "workspace slug to assign the plan to (required)")
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
			out := cmd.OutOrStdout()
			if m, ok := raw["month"]; ok {
				var s string
				_ = json.Unmarshal(m, &s)
				fmt.Fprintf(out, "Month: %s\n\n", s)
			}
			type row struct {
				Workspace   string  `json:"workspace"`
				Revenue     float64 `json:"revenue"`
				TotalCOGS   float64 `json:"totalCogs"`
				GrossMargin float64 `json:"grossMargin"`
				MarginPct   float64 `json:"marginPct"`
			}
			if ws, ok := raw["workspaces"]; ok {
				var rows []row
				_ = json.Unmarshal(ws, &rows)
				tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "WORKSPACE\tREVENUE\tCOGS\tMARGIN\tMARGIN%")
				for _, r := range rows {
					fmt.Fprintf(tw, "%s\t%.2f\t%.2f\t%.2f\t%.1f%%\n", r.Workspace, r.Revenue, r.TotalCOGS, r.GrossMargin, r.MarginPct)
				}
				_ = tw.Flush()
			}
			if tot, ok := raw["totals"]; ok {
				var t row
				_ = json.Unmarshal(tot, &t)
				fmt.Fprintf(out, "\nTotals: revenue %.2f  COGS %.2f  margin %.2f (%.1f%%)\n", t.Revenue, t.TotalCOGS, t.GrossMargin, t.MarginPct)
			}
			return nil
		},
	}
}
