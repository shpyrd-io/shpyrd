package cli

import (
	"context"
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
	"github.com/shpyrd-io/shpyrd/pkg/ext"
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
  shpyrd-ctl workspaces status acme --wait 15m   # the checks behind the READY column
  shpyrd-ctl workspaces list
  shpyrd-ctl workspaces limits acme --projects 10 --instances 20 --cpu 8 --memory 16Gi --storage 100Gi
  shpyrd-ctl workspaces suspend acme
  shpyrd-ctl workspaces resume acme`,
	}
	cmd.AddCommand(newWorkspacesCreateCmd(g), newWorkspacesStatusCmd(g), newWorkspacesListCmd(g), newWorkspacesInviteCmd(g), newWorkspacesLimitsCmd(g), newWorkspacesSuspendResumeCmd(g, "suspend", store.WorkspaceSuspended), newWorkspacesSuspendResumeCmd(g, "resume", store.WorkspaceActive))
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
	var name, address, owner, plan string
	var operator bool
	var wait time.Duration
	var limits limitFlags
	cmd := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create a workspace with its first owner",
		Long: `Create a workspace: the row, its address and its first owner. The front door,
the certificate and the public name follow through the controller, which records
what it sees on the workspace (` + "`workspaces status`" + `). When the platform sends mail,
the owner's invitation goes out once the door answers, so its link leads somewhere;
otherwise the link is printed here, for you to pass on once the door answers.
--wait stays until it does.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			slug := args[0]
			if err := project.ValidateWorkspaceSlug(slug); err != nil {
				return err
			}
			if owner == "" && !operator {
				return errors.New("--owner <email> is required: a workspace is enforced from birth and needs a first owner (or --operator: the platform admins own it)")
			}
			if plan != "" && operator {
				return errors.New("--plan does not apply to an operator workspace: the operator's own are never invoiced")
			}
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			if err := requireWorkspaces(ctx, ac); err != nil {
				return err
			}
			req := api.CreateWorkspaceRequest{Slug: slug, Name: firstNonEmpty(name, slug), Address: address, Owner: owner, OperatorOwned: operator, Plan: plan, Limits: limits.limits()}
			body, _ := json.Marshal(req)
			raw, err := serverRequest(ctx, ac.k, "POST", "api/workspaces", body, "application/json")
			if err != nil {
				return err
			}
			var created api.CreatedWorkspace
			_ = json.Unmarshal(raw, &created)
			ws := created.WorkspaceSummary
			if err := g.print(cmd, created, func(out io.Writer) {
				fmt.Fprintf(out, "Created workspace %s (%s) at %s\n", ws.Slug, ws.Name, ws.URL)
				if operator {
					fmt.Fprintf(out, "An operator workspace: every platform admin owns it. The door follows in a few minutes: `shpyrd-ctl workspaces status %s`.\n", ws.Slug)
					return
				}
				fmt.Fprintf(out, "%s is its first owner. The door follows in a few minutes: `shpyrd-ctl workspaces status %s`.\n", owner, ws.Slug)
				if created.OwnerInvitationPending {
					fmt.Fprintf(out, "The invitation to %s goes out by email once the door answers.\n", owner)
				} else {
					printOwnerInvitation(out, owner, ws.URL, created.OwnerInvitation)
				}
				if ws.Plan != "" {
					fmt.Fprintf(out, "Metered against the %s plan from now on.\n", ws.Plan)
				} else {
					fmt.Fprintf(out, "No billing plan yet: its usage is not priced until `shpyrd-ctl plans assign <plan> --workspace %s`.\n", ws.Slug)
				}
			}); err != nil {
				return err
			}
			if wait > 0 {
				return waitForWorkspace(ctx, cmd, g, ac, ws.Slug, wait)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "display name (default: the slug)")
	cmd.Flags().StringVar(&plan, "plan", "", "billing plan the workspace is metered against from birth (one of: shpyrd-ctl plans list); not for --operator")
	cmd.Flags().StringVar(&address, "address", "", "host of the workspace's dashboard; apps live one label under it (default: <slug>.<the platform's workspaces domain>)")
	cmd.Flags().StringVar(&owner, "owner", "", "email of the workspace's first owner")
	cmd.Flags().BoolVar(&operator, "operator", false, "one of the platform operator's own workspaces: never invoiced, owned by every platform admin (RFC-0078)")
	cmd.Flags().DurationVar(&wait, "wait", 0, "stay until the door answers, at most this long (e.g. 15m); the exit code says whether it did")
	limits.bind(cmd)
	return cmd
}

// newWorkspacesStatusCmd is `shpyrd-ctl workspaces status <slug>`: what the
// controller last saw at the workspace's door, one line per check, and an
// exit code that says whether the door answers (for scripts). --wait polls
// until it does or the time is up.
func newWorkspacesStatusCmd(g *globalFlags) *cobra.Command {
	var wait time.Duration
	cmd := &cobra.Command{
		Use:   "status <slug>",
		Short: "Show whether a workspace's door answers: front door, certificate, public name, HTTPS",
		Long: `A workspace exists the moment it is created; its address answers once the
front door has an Ingress, the certificate is issued, the name is on public DNS
and the door answers over HTTPS. The controller looks every few seconds while
it is not ready and records what it saw. This shows that record, and exits 1
while the door does not answer.

  shpyrd-ctl workspaces status acme
  shpyrd-ctl workspaces status acme --wait 15m
  shpyrd-ctl workspaces status acme --json | jq .readiness`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			if err := requireWorkspaces(ctx, ac); err != nil {
				return err
			}
			if wait > 0 {
				return waitForWorkspace(ctx, cmd, g, ac, args[0], wait)
			}
			ws, err := getWorkspace(ctx, ac, args[0])
			if err != nil {
				return err
			}
			if err := g.print(cmd, ws, func(out io.Writer) { printReadiness(out, ws) }); err != nil {
				return err
			}
			if ws.Readiness == nil || !ws.Readiness.Ready {
				return errors.New("the door does not answer yet")
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&wait, "wait", 0, "poll until the door answers, at most this long (e.g. 15m)")
	return cmd
}

func getWorkspace(ctx context.Context, ac *appClient, slug string) (*api.WorkspaceSummary, error) {
	raw, err := serverRequest(ctx, ac.k, "GET", "api/workspaces/"+url.PathEscape(slug), nil, "")
	if err != nil {
		return nil, err
	}
	var ws api.WorkspaceSummary
	if err := json.Unmarshal(raw, &ws); err != nil {
		return nil, err
	}
	return &ws, nil
}

// waitForWorkspace polls the workspace until its door answers or the time
// is up, narrating each change of what the controller sees. Under --json
// the final record is printed, nothing else.
func waitForWorkspace(ctx context.Context, cmd *cobra.Command, g *globalFlags, ac *appClient, slug string, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	progress := g.progress(cmd)
	last := ""
	for {
		ws, err := getWorkspace(ctx, ac, slug)
		if err != nil {
			return err
		}
		if line := readinessLine(ws); line != last {
			fmt.Fprintln(progress, line)
			last = line
		}
		if ws.Readiness != nil && ws.Readiness.Ready {
			return g.print(cmd, ws, func(out io.Writer) { printReadiness(out, ws) })
		}
		if time.Now().After(deadline) {
			_ = g.print(cmd, ws, func(out io.Writer) { printReadiness(out, ws) })
			return fmt.Errorf("the door of %s does not answer after %s", slug, wait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
}

// readyColumn is the READY column of the list: "yes", or "not yet" with
// the first check that does not pass.
func readyColumn(ws *api.WorkspaceSummary) string {
	if ws.Readiness != nil && ws.Readiness.Ready {
		return "yes"
	}
	if ws.Readiness == nil {
		return "not yet"
	}
	for _, c := range ws.Readiness.Checks {
		if !c.OK {
			return "not yet: " + c.Name
		}
	}
	return "not yet"
}

// readinessLine is the one-line state of a door: "ready", or the first
// check that does not pass and why.
func readinessLine(ws *api.WorkspaceSummary) string {
	if ws.Readiness == nil {
		return "not looked at yet"
	}
	if ws.Readiness.Ready {
		return "ready"
	}
	for _, c := range ws.Readiness.Checks {
		if !c.OK {
			return c.Name + ": " + firstNonEmpty(c.Detail, "not yet")
		}
	}
	return "not ready"
}

func printReadiness(out io.Writer, ws *api.WorkspaceSummary) {
	fmt.Fprintf(out, "Workspace %s at %s: %s\n", ws.Slug, firstNonEmpty(ws.URL, ws.Address), readinessLine(ws))
	if ws.Readiness == nil {
		fmt.Fprintln(out, "The controller has not looked at its door yet; it does within seconds of creation.")
		return
	}
	if ws.ReadyAt != nil {
		fmt.Fprintf(out, "The door first answered on %s.\n", ws.ReadyAt.Local().Format("Jan 2, 15:04"))
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "CHECK\tSTATE\tDETAIL")
	for _, c := range ws.Readiness.Checks {
		state := "ok"
		if !c.OK {
			state = "not yet"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", c.Name, state, c.Detail)
	}
	_ = tw.Flush()
	fmt.Fprintf(out, "Looked at %s.\n", ws.Readiness.CheckedAt.Local().Format("Jan 2, 15:04:05"))
}

// printOwnerInvitation says how the first owner gets in: by the email
// the platform sent, or by a link the operator passes on (shown once).
func printOwnerInvitation(out io.Writer, owner, door string, inv *ext.InviteOutcome) {
	switch {
	case inv == nil:
		fmt.Fprintf(out, "No invitation was sent: %s holds the owner role and signs in at %s with a method the workspace offers.\n", owner, door)
	case inv.Error != "":
		fmt.Fprintf(out, "Could not invite %s: %s. They hold the owner role; invite them again from the workspace's People page.\n", owner, inv.Error)
	case inv.Applied:
		fmt.Fprintf(out, "%s is known to the platform already and can sign in at %s now.\n", owner, door)
	case inv.Emailed:
		fmt.Fprintf(out, "Invitation emailed to %s: the link opens %s, where they set a password (or sign in with a method the workspace offers).\n", owner, door)
	default:
		fmt.Fprintf(out, "Mail is not configured, so nothing was sent. Pass this invitation link on to %s (shown once, valid until %s):\n  %s\n", owner, inv.ExpiresAt.Local().Format("Jan 2, 15:04"), inv.Link)
		if inv.SetPasswordLink != "" {
			fmt.Fprintf(out, "They have no password yet; this link lets them choose one (24 hours):\n  %s\n", inv.SetPasswordLink)
		}
		if inv.MailError != "" {
			fmt.Fprintf(out, "  (the email failed: %s)\n", inv.MailError)
		}
	}
}

func newWorkspacesInviteCmd(g *globalFlags) *cobra.Command {
	var role string
	cmd := &cobra.Command{
		Use:   "invite <slug> <email>",
		Short: "Invite a person into a workspace (the role is theirs at once; the link stands on the workspace's door and is emailed when mail is configured)",
		Long: `Invite a person into a workspace: as when the first owner's email never
arrived, or a second owner is named. The role is granted at once; the
invitation is how the person learns of it and gets a way in — a link on
the workspace's own door, emailed when the mail extension is configured,
printed once otherwise.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			slug, email := args[0], strings.ToLower(strings.TrimSpace(args[1]))
			if !strings.Contains(email, "@") {
				return errors.New("give the person's email address")
			}
			if !store.ValidWorkspaceRole(role) {
				return errors.New("--role must be owner, admin or member")
			}
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			if err := requireWorkspaces(ctx, ac); err != nil {
				return err
			}
			body, _ := json.Marshal(api.InviteRequest{Email: email, Role: role})
			raw, err := serverRequest(ctx, ac.k, "POST", "api/workspaces/"+url.PathEscape(slug)+"/invitations", body, "application/json")
			if err != nil {
				return err
			}
			var inv ext.InviteOutcome
			_ = json.Unmarshal(raw, &inv)
			return g.print(cmd, map[string]any{"workspace": slug, "email": email, "role": role, "invitation": inv}, func(out io.Writer) {
				fmt.Fprintf(out, "%s is %s %s of workspace %s.\n", email, article(role), role, slug)
				printOwnerInvitation(out, email, "the workspace's door", &inv)
			})
		},
	}
	cmd.Flags().StringVar(&role, "role", store.WorkspaceRoleOwner, "workspace role: owner, admin or member")
	return cmd
}

func newWorkspacesListCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the workspaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			ac, err := newAppClient(g, g.progress(cmd))
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
			list := []api.WorkspaceSummary{}
			if err := json.Unmarshal(raw, &list); err != nil {
				return err
			}
			return g.print(cmd, list, func(w io.Writer) { printWorkspaces(w, list) })
		},
	}
}

func printWorkspaces(out io.Writer, list []api.WorkspaceSummary) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "WORKSPACE\tNAME\tADDRESS\tSTATUS\tREADY\tPLAN\tLIMITS\tOWNERS")
	for _, ws := range list {
		limits := "-"
		if ws.Limits != nil {
			limits = limitsString(ws.Limits)
		}
		plan := firstNonEmpty(ws.Plan, "-")
		owners := strings.Join(ws.Owners, ",")
		if ws.Owner == store.WorkspaceOwnerOperator {
			owners = firstNonEmpty(owners, "(platform admins)")
			plan = "(operator)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", ws.Slug, ws.Name, ws.Address, ws.Status, readyColumn(&ws), plan, limits, owners)
	}
	w.Flush()
}

func limitsString(l *store.Limits) string {
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

// limitFlags are a workspace's ceilings as flags. Not the billing plan:
// that is `shpyrd-ctl plans`, and --plan on create.
type limitFlags struct {
	projects, instances  int
	cpu, memory, storage string
}

func (p *limitFlags) bind(cmd *cobra.Command) {
	cmd.Flags().IntVar(&p.projects, "projects", 0, "limit: maximum projects (0 = no ceiling)")
	cmd.Flags().IntVar(&p.instances, "instances", 0, "limit: maximum instances across projects")
	cmd.Flags().StringVar(&p.cpu, "cpu", "", "limit: maximum CPU requests, e.g. 8")
	cmd.Flags().StringVar(&p.memory, "memory", "", "limit: maximum memory requests, e.g. 16Gi")
	cmd.Flags().StringVar(&p.storage, "storage", "", "limit: maximum storage, e.g. 100Gi")
}

func (p *limitFlags) set() bool {
	return p.projects > 0 || p.instances > 0 || p.cpu != "" || p.memory != "" || p.storage != ""
}

func (p *limitFlags) limits() *store.Limits {
	if !p.set() {
		return nil
	}
	return &store.Limits{Projects: p.projects, Instances: p.instances, CPU: p.cpu, Memory: p.memory, Storage: p.storage}
}

func newWorkspacesLimitsCmd(g *globalFlags) *cobra.Command {
	var limits limitFlags
	var clear bool
	cmd := &cobra.Command{
		Use:     "limits <slug>",
		Aliases: []string{"plan"}, // the name until v0.9.56; plan is the billing plan now
		Short:   "Set a workspace's ceilings (those not given keep their value; --clear removes all)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			if cmd.CalledAs() == "plan" {
				fmt.Fprintln(cmd.ErrOrStderr(), "note: `workspaces plan` is now `workspaces limits`; the billing plan is `shpyrd-ctl plans`.")
			}
			if !clear && !limits.set() {
				return errors.New("give at least one ceiling (--projects, --instances, --cpu, --memory, --storage) or --clear")
			}
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			if err := requireWorkspaces(ctx, ac); err != nil {
				return err
			}
			req := api.UpdateWorkspaceRequest{Limits: limits.limits(), ClearLimits: clear}
			body, _ := json.Marshal(req)
			raw, err := serverRequest(ctx, ac.k, "PATCH", "api/workspaces/"+args[0], body, "application/json")
			if err != nil {
				return err
			}
			var ws api.WorkspaceSummary
			_ = json.Unmarshal(raw, &ws)
			return g.print(cmd, ws, func(w io.Writer) {
				if ws.Limits == nil {
					fmt.Fprintf(w, "Workspace %s has no ceilings.\n", ws.Slug)
				} else {
					fmt.Fprintf(w, "Workspace %s: %s\n", ws.Slug, limitsString(ws.Limits))
				}
			})
		},
	}
	limits.bind(cmd)
	cmd.Flags().BoolVar(&clear, "clear", false, "remove every ceiling")
	return cmd
}

func newWorkspacesSuspendResumeCmd(g *globalFlags, verb, status string) *cobra.Command {
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
			ac, err := newAppClient(g, g.progress(cmd))
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
			return g.print(cmd, ws, func(w io.Writer) {
				fmt.Fprintf(w, "Workspace %s is %s.\n", ws.Slug, ws.Status)
			})
		},
	}
}
