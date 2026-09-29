package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// newWorkspacesCmd is `shpyrd-ctl workspaces`: the console's view of the
// workspaces a platform hosts (RFC-0033 phase 8). The open-source platform
// has one implicit workspace and offers none of this: the commands work
// against a server that reports the "workspaces" capability and say so
// otherwise. One CLI, adapting to what the server offers.
func newWorkspacesCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspaces",
		Short: "Manage the workspaces this platform hosts (servers with the workspaces capability)",
		Long: `Workspaces are tenants: each has its own people, teams, projects and address.
The open-source platform has exactly one, implicit, and these commands do not apply
to it. On a platform that hosts many (the "workspaces" capability), they create,
list, suspend and resume workspaces and set their plans.

  shpyrd-ctl workspaces create acme --name "Acme Corp" --owner ana@acme.com --address acme.shpyrd.app
  shpyrd-ctl workspaces list
  shpyrd-ctl workspaces plan acme --projects 10 --instances 20 --cpu 8 --memory 16Gi --storage 100Gi
  shpyrd-ctl workspaces suspend acme
  shpyrd-ctl workspaces resume acme`,
	}
	cmd.AddCommand(newWorkspacesCreateCmd(g), newWorkspacesListCmd(g), newWorkspacesPlanCmd(g), newWorkspacesStatusCmd(g, "suspend", store.WorkspaceSuspended), newWorkspacesStatusCmd(g, "resume", store.WorkspaceActive))
	return cmd
}

// requireWorkspaces checks the server offers the capability before any
// workspace command runs, with a plain explanation when it does not.
func requireWorkspaces(ctx context.Context, ac *appClient) error {
	raw, err := serverRequest(ctx, ac.k, "GET", "api/config", nil, "")
	if err != nil {
		return err
	}
	var cfg struct {
		Capabilities []string `json:"capabilities"`
	}
	_ = json.Unmarshal(raw, &cfg)
	for _, c := range cfg.Capabilities {
		if c == "workspaces" {
			return nil
		}
	}
	return errors.New("this platform has one workspace: creating more is not part of the open-source platform")
}

func newWorkspacesCreateCmd(g *globalFlags) *cobra.Command {
	var name, address, owner string
	var operator bool
	var plan planFlags
	cmd := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create a workspace with its first owner",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			slug := args[0]
			if err := project.ValidateWorkspaceSlug(slug); err != nil {
				return err
			}
			if owner == "" && !operator {
				return errors.New("--owner <email> is required: a workspace is enforced from birth and needs a first owner (or --operator: the platform admins own it)")
			}
			ac, err := newAppClient(g, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			if err := requireWorkspaces(ctx, ac); err != nil {
				return err
			}
			req := api.CreateWorkspaceRequest{Slug: slug, Name: firstNonEmpty(name, slug), Address: address, Owner: owner, OperatorOwned: operator, Plan: plan.limits()}
			body, _ := json.Marshal(req)
			raw, err := serverRequest(ctx, ac.k, "POST", "api/workspaces", body, "application/json")
			if err != nil {
				return err
			}
			var ws api.WorkspaceSummary
			_ = json.Unmarshal(raw, &ws)
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Created workspace %s (%s) at %s\n", ws.Slug, ws.Name, ws.URL)
			if operator {
				fmt.Fprintln(out, "An operator workspace: every platform admin owns it. The front door and certificate follow within a minute.")
			} else {
				fmt.Fprintf(out, "%s is its first owner; the front door and certificate follow within a minute.\n", owner)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "display name (default: the slug)")
	cmd.Flags().StringVar(&address, "address", "", "host of the workspace's dashboard; apps live one label under it (default: <slug>.<the platform's workspaces domain>)")
	cmd.Flags().StringVar(&owner, "owner", "", "email of the workspace's first owner")
	cmd.Flags().BoolVar(&operator, "operator", false, "one of the platform operator's own workspaces: never invoiced, owned by every platform admin (RFC-0078)")
	plan.bind(cmd)
	return cmd
}

func newWorkspacesListCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the workspaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			ac, err := newAppClient(g, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			if err := requireWorkspaces(ctx, ac); err != nil {
				return err
			}
			raw, err := serverRequest(ctx, ac.k, "GET", "api/workspaces", nil, "")
			if err != nil {
				return err
			}
			var list []api.WorkspaceSummary
			if err := json.Unmarshal(raw, &list); err != nil {
				return err
			}
			printWorkspaces(cmd.OutOrStdout(), list)
			return nil
		},
	}
}

func printWorkspaces(out io.Writer, list []api.WorkspaceSummary) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "WORKSPACE\tNAME\tADDRESS\tSTATUS\tPLAN\tOWNERS")
	for _, ws := range list {
		plan := "-"
		if ws.Plan != nil {
			plan = planString(ws.Plan)
		}
		owners := strings.Join(ws.Owners, ",")
		if ws.Owner == store.WorkspaceOwnerOperator {
			owners = firstNonEmpty(owners, "(platform admins)")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", ws.Slug, ws.Name, ws.Address, ws.Status, plan, owners)
	}
	w.Flush()
}

func planString(l *store.Limits) string {
	var parts []string
	if l.Projects > 0 {
		parts = append(parts, fmt.Sprintf("%d projects", l.Projects))
	}
	if l.Instances > 0 {
		parts = append(parts, fmt.Sprintf("%d instances", l.Instances))
	}
	if l.CPU != "" {
		parts = append(parts, l.CPU+" cpu")
	}
	if l.Memory != "" {
		parts = append(parts, l.Memory+" mem")
	}
	if l.Storage != "" {
		parts = append(parts, l.Storage+" storage")
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

// planFlags are the ceilings of a plan as flags.
type planFlags struct {
	projects, instances  int
	cpu, memory, storage string
}

func (p *planFlags) bind(cmd *cobra.Command) {
	cmd.Flags().IntVar(&p.projects, "projects", 0, "plan: maximum projects (0 = no ceiling)")
	cmd.Flags().IntVar(&p.instances, "instances", 0, "plan: maximum instances across projects")
	cmd.Flags().StringVar(&p.cpu, "cpu", "", "plan: maximum CPU requests, e.g. 8")
	cmd.Flags().StringVar(&p.memory, "memory", "", "plan: maximum memory requests, e.g. 16Gi")
	cmd.Flags().StringVar(&p.storage, "storage", "", "plan: maximum storage, e.g. 100Gi")
}

func (p *planFlags) set() bool {
	return p.projects > 0 || p.instances > 0 || p.cpu != "" || p.memory != "" || p.storage != ""
}

func (p *planFlags) limits() *store.Limits {
	if !p.set() {
		return nil
	}
	return &store.Limits{Projects: p.projects, Instances: p.instances, CPU: p.cpu, Memory: p.memory, Storage: p.storage}
}

func newWorkspacesPlanCmd(g *globalFlags) *cobra.Command {
	var plan planFlags
	var clear bool
	cmd := &cobra.Command{
		Use:   "plan <slug>",
		Short: "Set a workspace's ceilings (those not given keep their value; --clear removes all)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			if !clear && !plan.set() {
				return errors.New("give at least one ceiling (--projects, --instances, --cpu, --memory, --storage) or --clear")
			}
			ac, err := newAppClient(g, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			if err := requireWorkspaces(ctx, ac); err != nil {
				return err
			}
			req := api.UpdateWorkspaceRequest{Plan: plan.limits(), ClearPlan: clear}
			body, _ := json.Marshal(req)
			raw, err := serverRequest(ctx, ac.k, "PATCH", "api/workspaces/"+args[0], body, "application/json")
			if err != nil {
				return err
			}
			var ws api.WorkspaceSummary
			_ = json.Unmarshal(raw, &ws)
			if ws.Plan == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Workspace %s has no ceilings.\n", ws.Slug)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Workspace %s: %s\n", ws.Slug, planString(ws.Plan))
			}
			return nil
		},
	}
	plan.bind(cmd)
	cmd.Flags().BoolVar(&clear, "clear", false, "remove every ceiling")
	return cmd
}

func newWorkspacesStatusCmd(g *globalFlags, verb, status string) *cobra.Command {
	short := "Suspend a workspace: its hosts answer only a 'suspended' page, nothing is deleted"
	if status == store.WorkspaceActive {
		short = "Resume a suspended workspace"
	}
	return &cobra.Command{
		Use:   verb + " <slug>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			ac, err := newAppClient(g, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			if err := requireWorkspaces(ctx, ac); err != nil {
				return err
			}
			st := status
			body, _ := json.Marshal(api.UpdateWorkspaceRequest{Status: &st})
			raw, err := serverRequest(ctx, ac.k, "PATCH", "api/workspaces/"+args[0], body, "application/json")
			if err != nil {
				return err
			}
			var ws api.WorkspaceSummary
			_ = json.Unmarshal(raw, &ws)
			fmt.Fprintf(cmd.OutOrStdout(), "Workspace %s is %s.\n", ws.Slug, ws.Status)
			return nil
		},
	}
}
