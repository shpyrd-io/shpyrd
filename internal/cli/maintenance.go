package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/api"
)

// shpyrd maintenance [on|off] (RFC-0020): a page in front of the app.
func newMaintenanceCmd(g *globalFlags) *cobra.Command {
	var appName, message string
	cmd := &cobra.Command{
		Use:   "maintenance",
		Short: "Put a maintenance page in front of the app, or take it away",
		Long: `While maintenance is on, every request at the app's addresses gets a 503
and a page with the project's name and your message; programs get a JSON
answer with Retry-After. Nobody is let through, the team included. The
processes keep running, and releases, scaling, the shell and one-off
commands go on underneath: a migration runs with shpyrd run or shpyrd shell.

  shpyrd maintenance --project shop                     # on or off, since when, by whom
  shpyrd maintenance on --project shop --message "Back at 15:00 UTC"
  shpyrd maintenance off --project shop`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			name, err := resolveAppName(appName)
			if err != nil {
				return err
			}
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			raw, err := serverRequest(ctx, ac.k, "GET", "api/projects/"+name, nil, "")
			if err != nil {
				return err
			}
			var d api.AppDetail
			_ = json.Unmarshal(raw, &d)
			m := d.Spec.Maintenance
			if m == nil {
				m = &shpyrdv1.Maintenance{}
			}
			return g.print(cmd, map[string]any{"project": name, "maintenance": m}, func(w io.Writer) {
				printMaintenance(w, name, m)
			})
		},
	}
	set := func(enabled bool) func(cmd *cobra.Command, args []string) error {
		return func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			name, err := resolveAppName(appName)
			if err != nil {
				return err
			}
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			body, _ := json.Marshal(map[string]any{"enabled": enabled, "message": message})
			raw, err := serverRequest(ctx, ac.k, "PUT", "api/projects/"+name+"/maintenance", body, "application/json")
			if err != nil {
				return err
			}
			var s api.AppSummary
			_ = json.Unmarshal(raw, &s)
			m := s.Maintenance
			if m == nil {
				m = &shpyrdv1.Maintenance{}
			}
			return g.print(cmd, map[string]any{"project": name, "maintenance": m}, func(w io.Writer) {
				printMaintenance(w, name, m)
			})
		}
	}
	on := &cobra.Command{Use: "on", Short: "Serve the maintenance page instead of the app", Args: cobra.NoArgs, RunE: set(true)}
	on.Flags().StringVar(&message, "message", "", "what the page says under the project's name: plain text, up to 500 characters")
	off := &cobra.Command{Use: "off", Short: "Serve the app again", Args: cobra.NoArgs, RunE: set(false)}
	for _, c := range []*cobra.Command{cmd, on, off} {
		appFlag(c, &appName)
	}
	cmd.AddCommand(on, off)
	return cmd
}

// printMaintenance says how the project answers: the page, since when and
// by whom, or the app.
func printMaintenance(w io.Writer, name string, m *shpyrdv1.Maintenance) {
	if m == nil || !m.Enabled {
		fmt.Fprintf(w, "%s serves the app: maintenance is off.\n", name)
		return
	}
	fmt.Fprintf(w, "%s serves its maintenance page%s.\n", name, maintenanceSince(m))
	if m.Message != "" {
		fmt.Fprintf(w, "  %q\n", m.Message)
	}
	fmt.Fprintln(w, "  Visitors get a 503; the processes keep running, and shell, run and deploys go on.")
}

// maintenanceSince is " since <when> by <whom>", what is known of it.
func maintenanceSince(m *shpyrdv1.Maintenance) string {
	s := ""
	if m.Since != nil {
		s += " since " + m.Since.Local().Format("Jan 2 15:04")
	}
	if m.By != "" {
		s += " by " + m.By
	}
	return s
}
