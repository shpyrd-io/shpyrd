//go:build !foss

package costs

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

func newCostsCmd(g ext.CLIGlobals) *cobra.Command {
	var from, to, kind, by string
	cmd := &cobra.Command{
		Use:   "costs",
		Short: "What the cluster uses and costs (enterprise)",
		Long: `What the cluster uses and costs, summed by project, process, resource or
service, for a period (this month by default). Three kinds: estimated
(OpenCost, at list prices), real (the provider's bill, from OCI) and usage
(what the platform measured).

  shpyrd-ctl costs --by process
  shpyrd-ctl costs --kind real --by service --from 2026-10-01
  shpyrd-ctl costs drains add finance https://finance.example.com/costs --header "Authorization=Bearer …"
  shpyrd-ctl costs oci set --tenancy … --user … --fingerprint … --region us-ashburn-1 --key @oci.pem`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			q := url.Values{}
			for k, v := range map[string]string{"from": from, "to": to, "kind": kind, "group": by} {
				if v != "" {
					q.Set(k, v)
				}
			}
			raw, err := g.API().Request(cmd.Context(), "GET", "api/cluster/costs?"+q.Encode(), nil, "")
			if err != nil {
				return err
			}
			var s Summary
			if err := json.Unmarshal(raw, &s); err != nil {
				return err
			}
			return ext.Print(g, cmd, s, func(w io.Writer) { printSummary(w, s) })
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "first day (YYYY-MM-DD); the month's first by default")
	cmd.Flags().StringVar(&to, "to", "", "the day after the last (YYYY-MM-DD); tomorrow by default")
	cmd.Flags().StringVar(&kind, "kind", "", "estimated (default), real or usage")
	cmd.Flags().StringVar(&by, "by", "", "project (default), process, resource or service")
	cmd.AddCommand(newDrainsCmd(g), newOCICmd(g))
	return cmd
}

func money(m map[string]float64) string {
	if len(m) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%.2f %s", m[k], k))
	}
	return strings.Join(parts, " + ")
}

func printSummary(out io.Writer, s Summary) {
	fmt.Fprintf(out, "%s costs from %s to %s, by %s\n\n", s.Kind, s.From.Format("2006-01-02"), s.To.Format("2006-01-02"), s.Group)
	if len(s.Rows) == 0 {
		fmt.Fprintln(out, "Nothing recorded for this period.")
		return
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, strings.ToUpper(s.Group)+"\tCOST\tLINES")
	for _, r := range s.Rows {
		name := r.Key
		switch s.Group {
		case "project":
			name = firstNonEmpty(r.Slug, r.Project, "(platform)")
		case "process":
			name = firstNonEmpty(r.Slug, r.Project, "(platform)") + " " + r.Process
		}
		fmt.Fprintf(w, "%s\t%s\t%d\n", name, money(r.Cost), r.Lines)
	}
	fmt.Fprintf(w, "TOTAL\t%s\t\n", money(s.Total))
	w.Flush()
}

func newDrainsCmd(g ext.CLIGlobals) *cobra.Command {
	cmd := &cobra.Command{Use: "drains", Short: "Where the cost lines are sent"}
	list := &cobra.Command{
		Use: "list", Aliases: []string{"ls"}, Short: "List the cost drains", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := g.API().Request(cmd.Context(), "GET", "api/cluster/cost-drains", nil, "")
			if err != nil {
				return err
			}
			var drains []store.CostDrain
			if err := json.Unmarshal(raw, &drains); err != nil {
				return err
			}
			return ext.Print(g, cmd, drains, func(out io.Writer) {
				if len(drains) == 0 {
					fmt.Fprintln(out, "No cost drain.")
					return
				}
				w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "NAME\tURL\tSENT\tERRORS\tLAST DELIVERY\tMESSAGE")
				for _, d := range drains {
					last := "-"
					if d.LastDeliveryAt != nil {
						last = d.LastDeliveryAt.Format("2006-01-02 15:04")
					}
					fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\t%s\n", d.Name, d.URL, d.Sent, d.Errors, last, d.Message)
				}
				w.Flush()
			})
		},
	}
	var headers []string
	add := &cobra.Command{
		Use: "add <name> <url>", Short: "Send the cost lines to a URL", Args: cobra.ExactArgs(2),
		Long: "Send the cost lines to a URL, from now on: one POST of JSON at a time, with the headers given (kept in a Secret).",
		RunE: func(cmd *cobra.Command, args []string) error {
			req := NewDrain{Name: args[0], URL: args[1], Headers: map[string]string{}}
			for _, h := range headers {
				k, v, ok := strings.Cut(h, "=")
				if !ok {
					return fmt.Errorf("--header %q: write it as Name=value", h)
				}
				req.Headers[strings.TrimSpace(k)] = v
			}
			if err := ValidateDrain(req); err != nil {
				return err
			}
			body, _ := json.Marshal(req)
			raw, err := g.API().Request(cmd.Context(), "POST", "api/cluster/cost-drains", body, "application/json")
			if err != nil {
				return err
			}
			var d store.CostDrain
			if err := json.Unmarshal(raw, &d); err != nil {
				return err
			}
			return ext.Print(g, cmd, d, func(w io.Writer) { fmt.Fprintf(w, "Cost drain %s sends to %s from now on.\n", d.Name, d.URL) })
		},
	}
	add.Flags().StringArrayVar(&headers, "header", nil, "a header to send, Name=value (repeatable)")
	remove := &cobra.Command{
		Use: "remove <name>", Aliases: []string{"rm"}, Short: "Stop sending to a drain", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := g.API().Request(cmd.Context(), "DELETE", "api/cluster/cost-drains/"+url.PathEscape(args[0]), nil, ""); err != nil {
				return err
			}
			return ext.Print(g, cmd, map[string]string{"removed": args[0]}, func(w io.Writer) { fmt.Fprintf(w, "Cost drain %s removed.\n", args[0]) })
		},
	}
	cmd.AddCommand(list, add, remove)
	return cmd
}

func newOCICmd(g ext.CLIGlobals) *cobra.Command {
	cmd := &cobra.Command{Use: "oci", Short: "Read the bill from OCI (the real costs)"}
	var s OCISettings
	set := &cobra.Command{
		Use: "set", Short: "Give the API key of an OCI user who may read usage and search resources", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.HasPrefix(s.Key, "@") {
				raw, err := os.ReadFile(strings.TrimPrefix(s.Key, "@"))
				if err != nil {
					return err
				}
				s.Key = string(raw)
			}
			body, _ := json.Marshal(s)
			raw, err := g.API().Request(cmd.Context(), "PUT", "api/cluster/costs/oci", body, "application/json")
			if err != nil {
				return err
			}
			var st OCIStatus
			_ = json.Unmarshal(raw, &st)
			return ext.Print(g, cmd, st, func(w io.Writer) {
				fmt.Fprintf(w, "The bill of %s (%s) is read once a day.\n", st.Tenancy, st.Region)
			})
		},
	}
	set.Flags().StringVar(&s.Tenancy, "tenancy", "", "tenancy OCID")
	set.Flags().StringVar(&s.User, "user", "", "user OCID")
	set.Flags().StringVar(&s.Fingerprint, "fingerprint", "", "the API key's fingerprint")
	set.Flags().StringVar(&s.Region, "region", "", "home region (us-ashburn-1)")
	set.Flags().StringVar(&s.Key, "key", "", "the API key's private key, PEM; @path reads a file")
	status := &cobra.Command{
		Use: "status", Short: "Whether the bill is read", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := g.API().Request(cmd.Context(), "GET", "api/cluster/costs/oci", nil, "")
			if err != nil {
				return err
			}
			var st OCIStatus
			_ = json.Unmarshal(raw, &st)
			return ext.Print(g, cmd, st, func(w io.Writer) {
				if !st.Configured {
					fmt.Fprintln(w, "No OCI key: only estimates, no bill.")
					return
				}
				fmt.Fprintf(w, "The bill of %s (%s) is read once a day.\n", st.Tenancy, st.Region)
			})
		},
	}
	remove := &cobra.Command{
		Use: "remove", Short: "Stop reading the bill", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := g.API().Request(cmd.Context(), "DELETE", "api/cluster/costs/oci", nil, "")
			return err
		},
	}
	cmd.AddCommand(set, status, remove)
	return cmd
}
