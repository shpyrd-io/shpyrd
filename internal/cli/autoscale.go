package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/api"
)

// ---- autoscale --------------------------------------------------------------

func newAutoscaleCmd(g *globalFlags) *cobra.Command {
	var min, max, cpu, concurrency int32
	var off bool
	cmd := &cobra.Command{
		Use:   "autoscale <project> [--min N --max M] [--cpu PERCENT] [--concurrency N] [--off]",
		Short: "Show or set the bounds the web process follows its load between",
		Long: `Let the web process follow its load: another instance when one passes the
CPU target or carries more requests in flight than the target, one fewer when
the load has been lower for five minutes, always between the two bounds.

  shpyrd autoscale shop                                 # what applies now
  shpyrd autoscale shop --min 2 --max 10                # 70% of the size's CPU, 20 requests in flight
  shpyrd autoscale shop --min 2 --max 10 --cpu 50       # another instance sooner
  shpyrd autoscale shop --min 1 --max 4 --concurrency 8 # for slow requests
  shpyrd autoscale shop --off                           # a fixed count again

shpyrd scale web=N on a process with bounds sets the minimum. A sleep policy
and bounds go together: asleep at zero, the minimum on a wake, the bounds
under load. Needs the autoscale extension on the cluster (shpyrd-ctl
extensions enable autoscale).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			changed := cmd.Flags().Changed
			if !changed("min") && !changed("max") && !off {
				var d api.AppDetail
				if err := t.call(ctx, "GET", "api/projects/"+args[0], nil, &d); err != nil {
					return err
				}
				spec := d.Spec.Processes["web"].Autoscale
				st := d.Processes["web"].Autoscale
				return g.print(cmd, map[string]any{"project": args[0], "autoscale": spec, "status": st}, func(w io.Writer) {
					printAutoscaleReadBack(w, args[0], spec, st)
				})
			}
			var bounds map[string]any
			switch {
			case off:
				if changed("min") || changed("max") {
					return fmt.Errorf("--off takes no bounds")
				}
				bounds = map[string]any{"off": true}
			case !changed("min") || !changed("max"):
				return fmt.Errorf("bounds need both --min and --max (e.g. --min 2 --max 10)")
			default:
				bounds = map[string]any{"min": min, "max": max}
				if changed("cpu") {
					bounds["cpu"] = cpu
				}
				if changed("concurrency") {
					bounds["concurrency"] = concurrency
				}
			}
			body := map[string]any{"processes": map[string]any{"web": map[string]any{"autoscale": bounds}}}
			var out any
			if err := t.call(ctx, "POST", "api/projects/"+args[0]+"/processes", body, &out); err != nil {
				return err
			}
			return g.print(cmd, out, func(w io.Writer) {
				if off {
					fmt.Fprintf(w, "Autoscaling is off for %s: web keeps a fixed count (shpyrd scale web=N).\n", args[0])
					return
				}
				as := &shpyrdv1.AutoscaleSpec{Min: min, Max: max}
				if changed("cpu") {
					as.CPU = &cpu
				}
				if changed("concurrency") {
					as.Concurrency = &concurrency
				}
				fmt.Fprintf(w, "%s follows its load: %s\n", args[0], boundsWords(as))
			})
		},
	}
	cmd.Flags().Int32Var(&min, "min", 0, "the fewest instances, 1 or more")
	cmd.Flags().Int32Var(&max, "max", 0, "the most instances")
	cmd.Flags().Int32Var(&cpu, "cpu", shpyrdv1.DefaultAutoscaleCPU, "the CPU an instance may use before another is added, in percent of the size's CPU")
	cmd.Flags().Int32Var(&concurrency, "concurrency", shpyrdv1.DefaultAutoscaleConcurrency, "the requests an instance may have in flight before another is added")
	cmd.Flags().BoolVar(&off, "off", false, "a fixed count again: the process keeps the count it last asked for")
	return cmd
}

// boundsWords says what bounds mean, in one line.
func boundsWords(as *shpyrdv1.AutoscaleSpec) string {
	return fmt.Sprintf("web runs between %d and %d instances, another when an instance passes %d%% of its CPU or %d requests in flight.", as.Min, as.Max, as.TargetCPU(), as.TargetConcurrency())
}

// printAutoscaleReadBack says what bounds apply to a project's web process
// and whether the cluster follows them.
func printAutoscaleReadBack(w io.Writer, project string, spec *shpyrdv1.AutoscaleSpec, st *shpyrdv1.AutoscaleStatus) {
	if spec == nil {
		fmt.Fprintf(w, "%s keeps a fixed count of web instances (shpyrd scale web=N).\n", project)
		fmt.Fprintf(w, "Let it follow its load: shpyrd autoscale %s --min 2 --max 10\n", project)
		return
	}
	fmt.Fprintf(w, "%s follows its load: %s\n", project, boundsWords(spec))
	switch {
	case st == nil:
		fmt.Fprintln(w, "Not reconciled yet.")
	case st.State == "unavailable":
		fmt.Fprintf(w, "Not following it now: %s\n", firstNonEmpty(st.Message, "the cluster cannot follow the bounds"))
	case st.Message != "":
		fmt.Fprintf(w, "Note: %s\n", st.Message)
	}
	fmt.Fprintf(w, "Change it: shpyrd autoscale %s --min N --max M [--cpu PERCENT] [--concurrency N], or --off\n", project)
}
