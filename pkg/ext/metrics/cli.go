// Package metrics provides the shared CLI presentation of metrics API responses.
package metrics

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"text/tabwriter"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/resource"
)

type Options struct{ Range string }

func (o *Options) Flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.Range, "range", "1h", "time window: 15m, 1h, 6h, 24h, 7d")
}

func (o Options) Query() (url.Values, error) {
	switch o.Range {
	case "15m", "1h", "6h", "24h", "7d":
	default:
		return nil, fmt.Errorf("--range must be one of 15m, 1h, 6h, 24h, 7d")
	}
	return url.Values{"range": {o.Range}}, nil
}

// Print keeps the API document intact for JSON consumers, and shows the most
// recent sample of each series (with its timestamp) for terminal readers.
func Print(g ext.CLIGlobals, cmd *cobra.Command, path string) error {
	raw, err := g.API().Request(cmd.Context(), "GET", path, nil, "")
	if err != nil {
		return err
	}
	var response struct {
		Range  string `json:"range"`
		Charts []struct {
			Title  string `json:"title"`
			Unit   string `json:"unit"`
			Error  string `json:"error"`
			Note   string `json:"note"`
			Series []struct {
				Name   string       `json:"name"`
				Points [][2]float64 `json:"points"`
			} `json:"series"`
		} `json:"charts"`
		// OOMs are the out-of-memory kills of a project's processes (#119);
		// a datastore's metrics carry none.
		OOMs []struct {
			Time     float64 `json:"time"`
			Process  string  `json:"process"`
			Instance string  `json:"instance"`
			Limit    int64   `json:"limit"`
			Size     string  `json:"size"`
			Release  int     `json:"release"`
		} `json:"ooms"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return fmt.Errorf("decode metrics: %w", err)
	}
	return ext.Print(g, cmd, json.RawMessage(raw), func(w io.Writer) {
		fmt.Fprintf(w, "Latest samples over %s (timestamps in UTC)\n", response.Range)
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "METRIC\tSERIES\tVALUE\tUNIT\tSAMPLED AT")
		for _, chart := range response.Charts {
			if chart.Error != "" {
				fmt.Fprintf(tw, "%s\tERROR: %s\n", chart.Title, chart.Error)
				continue
			}
			if len(chart.Series) == 0 {
				fmt.Fprintf(tw, "%s\tNo data\n", chart.Title)
			}
			for _, series := range chart.Series {
				if len(series.Points) == 0 {
					fmt.Fprintf(tw, "%s\t%s\tNo data\n", chart.Title, series.Name)
					continue
				}
				p := series.Points[len(series.Points)-1]
				fmt.Fprintf(tw, "%s\t%s\t%.6g\t%s\t%s\n", chart.Title, series.Name, p[1], chart.Unit, time.Unix(int64(p[0]), 0).UTC().Format(time.RFC3339))
			}
			if chart.Note != "" {
				fmt.Fprintf(tw, "%s\t%s\n", chart.Title, chart.Note)
			}
		}
		_ = tw.Flush()
		if len(response.OOMs) == 0 {
			return
		}
		fmt.Fprintf(w, "\nKilled for running out of memory over %s: %d\n", response.Range, len(response.OOMs))
		tw = tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "PROCESS\tINSTANCE\tLIMIT\tRELEASE\tAT")
		for _, k := range response.OOMs {
			limit := "-"
			if k.Limit > 0 {
				limit = resource.NewQuantity(k.Limit, resource.BinarySI).String()
				if k.Size != "" {
					limit += " (" + k.Size + ")"
				}
			}
			release := "-"
			if k.Release > 0 {
				release = fmt.Sprintf("v%d", k.Release)
			}
			instance := k.Instance
			if instance == "" {
				instance = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", k.Process, instance, limit, release, time.Unix(int64(k.Time), 0).UTC().Format(time.RFC3339))
		}
		_ = tw.Flush()
	})
}

func ResourceCommand(g ext.CLIGlobals, kind string) *cobra.Command {
	var project string
	var opts Options
	cmd := &cobra.Command{Use: "metrics <name>", Short: "Show CPU, memory, network and storage metrics", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q, err := opts.Query()
			if err != nil {
				return err
			}
			return Print(g, cmd, "api/projects/"+url.PathEscape(project)+"/resources/"+kind+"/"+url.PathEscape(args[0])+"/metrics?"+q.Encode())
		},
	}
	ext.ProjectFlag(g, cmd, &project)
	opts.Flags(cmd)
	return cmd
}
