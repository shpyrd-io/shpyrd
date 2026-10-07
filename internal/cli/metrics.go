package cli

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/shpyrd-io/shpyrd/pkg/ext/metrics"
	"github.com/spf13/cobra"
)

func newMetricsCmd(g *globalFlags) *cobra.Command {
	var project, process, mode, by, agg string
	var replaced bool
	var opts metrics.Options
	cmd := &cobra.Command{Use: "metrics", Short: "Show project or process metrics", Args: cobra.NoArgs,
		Long: `Show the latest sample for each metric series. Use --json for the full time series.

  shpyrd metrics --project shop --process web
  shpyrd metrics --project shop --by instance --range 6h --json
  shpyrd pg metrics db --project shop
  shpyrd redis metrics cache --project shop

Throughput and latency describe the project's HTTP traffic; --process filters
CPU, memory, instance counts and network metrics.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			q, err := opts.Query()
			if err != nil {
				return err
			}
			if mode != "percent" && mode != "total" {
				return fmt.Errorf("--mode must be percent or total")
			}
			if by != "process" && by != "instance" {
				return fmt.Errorf("--by must be process or instance")
			}
			switch agg {
			case "none", "sum", "avg", "max":
			default:
				return fmt.Errorf("--agg must be none, sum, avg or max")
			}
			if agg != "none" && by != "instance" {
				return fmt.Errorf("--agg requires --by instance")
			}
			if replaced && by != "instance" {
				return fmt.Errorf("--replaced requires --by instance")
			}
			name, err := resolveAppName(project)
			if err != nil {
				return err
			}
			q.Set("process", process)
			q.Set("mode", mode)
			q.Set("by", by)
			q.Set("agg", agg)
			q.Set("replaced", strconv.FormatBool(replaced))
			return metrics.Print(g, cmd, "api/projects/"+url.PathEscape(name)+"/metrics?"+q.Encode())
		},
	}
	appFlag(cmd, &project)
	opts.Flags(cmd)
	cmd.Flags().StringVarP(&process, "process", "p", "", "filter to one process type, e.g. web or worker")
	cmd.Flags().StringVar(&mode, "mode", "percent", "CPU/memory units: percent or total")
	cmd.Flags().StringVar(&by, "by", "process", "break down by process or instance")
	cmd.Flags().StringVar(&agg, "agg", "none", "aggregate instances: none, sum, avg, max (requires --by instance)")
	cmd.Flags().BoolVar(&replaced, "replaced", false, "include replaced instances (requires --by instance)")
	return cmd
}
