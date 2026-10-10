package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// People and workspace roles (RFC-0033): who is in the workspace, as
// owner, admin or member, and the invitations that bring new people in.
// The CLI speaks the same API as the dashboard.

func newPeopleCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "people",
		Aliases: []string{"person"},
		Short:   "The workspace's people and their roles",
		Long: `The people of the workspace: everyone who has signed in or holds a role.

Roles: owners and admins administer the workspace and every project; only
owners name owners, and the last owner stays. Members may create projects
and administer the ones they create; teams and project grants give
everything else. A person without a role has what grants give them.

  shpyrd people
  shpyrd people role ada@example.com admin
  shpyrd people role bob@example.com none
  shpyrd people suspend eve@example.com`,
		Args: cobra.NoArgs,
		RunE: newPeopleListCmd(g).RunE, // bare `shpyrd people` lists
	}
	cmd.AddCommand(newPeopleListCmd(g), newPeopleRoleCmd(g), newPeopleStatusCmd(g, store.StatusSuspended), newPeopleStatusCmd(g, store.StatusActive), newPeopleForgetCmd(g))
	return cmd
}

func (t *teamsAPI) people(ctx context.Context) ([]api.PersonView, error) {
	var out []api.PersonView
	err := t.call(ctx, "GET", "api/workspace/people", nil, &out)
	return out, err
}

func newPeopleListCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the workspace's people",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			people, err := t.people(ctx)
			if err != nil {
				return err
			}
			if people == nil {
				people = []api.PersonView{}
			}
			return g.print(cmd, people, func(w io.Writer) {
				if len(people) == 0 {
					fmt.Fprintln(w, "Nobody has signed in through an account yet (the admin token is not a person). Invite someone: `shpyrd invite ada@example.com`.")
					return
				}
				tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "EMAIL\tROLE\tNAME\tSIGNED IN WITH\tLAST SEEN\tSTATUS")
				for _, p := range people {
					role := firstNonEmpty(p.Role, "-")
					if p.Role == "" && p.PlatformRole != "" {
						role = p.PlatformRole + " (team)"
					}
					seen := "never"
					if p.LastSeenAt != nil {
						seen = ago(*p.LastSeenAt)
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", p.Email, role, firstNonEmpty(p.Name, "-"), firstNonEmpty(p.Provider, "-"), seen, p.Status)
				}
				_ = tw.Flush()
			})
		},
	}
}

func newPeopleRoleCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "role <email> <owner|admin|member|none>",
		Short: "Set or remove someone's workspace role",
		Long: `Set someone's workspace role, or remove it with "none". The person need not
have signed in yet: the role waits for them. Naming or demoting an owner
takes an owner; the last owner cannot step down.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			email, role := strings.ToLower(strings.TrimSpace(args[0])), strings.ToLower(strings.TrimSpace(args[1]))
			if role == "none" || role == "-" {
				role = ""
			} else if !store.ValidWorkspaceRole(role) {
				return errors.New("the role is owner, admin, member or none")
			}
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			var out api.PersonView
			if err := t.call(ctx, "PATCH", "api/workspace/people/"+url.PathEscape(email), map[string]string{"role": role}, &out); err != nil {
				return err
			}
			return g.print(cmd, out, func(w io.Writer) {
				if out.Role == "" {
					fmt.Fprintf(w, "%s has no workspace role now (grants and teams still apply).\n", out.Email)
				} else {
					fmt.Fprintf(w, "%s is %s %s of the workspace.\n", out.Email, article(out.Role), out.Role)
				}
			})
		},
	}
}

func newPeopleStatusCmd(g *globalFlags, status string) *cobra.Command {
	use, short := "suspend <email>", "Switch someone's access off, everywhere, at once"
	if status == store.StatusActive {
		use, short = "reactivate <email>", "Switch a suspended person's access back on"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			var out api.PersonView
			if err := t.call(ctx, "PATCH", "api/workspace/people/"+url.PathEscape(strings.ToLower(strings.TrimSpace(args[0]))), map[string]string{"status": status}, &out); err != nil {
				return err
			}
			return g.print(cmd, out, func(w io.Writer) {
				if out.Status == store.StatusSuspended {
					fmt.Fprintf(w, "%s is suspended: no role anywhere, no app opens, until reactivated.\n", out.Email)
				} else {
					fmt.Fprintf(w, "%s is active again.\n", out.Email)
				}
			})
		},
	}
}

func newPeopleForgetCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "forget <email>",
		Short: "Remove someone's sign-in record (their role and grants stay)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			email := strings.ToLower(strings.TrimSpace(args[0]))
			if err := t.call(ctx, "DELETE", "api/workspace/people/"+url.PathEscape(email), nil, nil); err != nil {
				return err
			}
			return g.print(cmd, map[string]any{"email": email, "forgotten": true}, func(w io.Writer) {
				fmt.Fprintf(w, "Forgot %s; the next sign-in creates the record again.\n", email)
			})
		},
	}
}

// ---- invitations ---------------------------------------------------------------

func newInviteCmd(g *globalFlags) *cobra.Command {
	var role, team string
	cmd := &cobra.Command{
		Use:   "invite <email>",
		Short: "Invite someone to the workspace",
		Long: `Invite someone by email with a workspace role (member by default) and,
optionally, into a team. The link is printed once and emailed when the
platform sends mail; signing in with the invited address accepts it, link
or no link. Inviting someone who has signed in before applies the role at
once. Inviting the same address again makes a new link.

  shpyrd invite ada@example.com
  shpyrd invite bob@example.com --role admin --team web`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !store.ValidWorkspaceRole(role) {
				return errors.New("--role is owner, admin or member")
			}
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			var res api.InviteResult
			if err := t.call(ctx, "POST", "api/workspace/invitations", api.InviteRequest{Email: strings.TrimSpace(args[0]), Role: role, Team: team}, &res); err != nil {
				return err
			}
			where := ""
			if res.Team != "" {
				where = " and in team " + res.Team
			}
			return g.print(cmd, res, func(out io.Writer) {
				if res.Applied {
					fmt.Fprintf(out, "%s had signed in before: they are %s %s of the workspace now%s.\n", res.Email, article(res.Role), res.Role, where)
					return
				}
				fmt.Fprintf(out, "Invited %s as %s %s%s.\n", res.Email, article(res.Role), res.Role, where)
				switch {
				case res.Emailed:
					fmt.Fprintf(out, "The link was emailed to them; it also works from here (shown once):\n\n  %s\n", res.Link)
				case res.MailError != "":
					fmt.Fprintf(out, "The email could not be sent (%s). Send them this link yourself (shown once):\n\n  %s\n", res.MailError, res.Link)
				default:
					fmt.Fprintf(out, "This platform does not send email yet. Send them this link yourself (shown once):\n\n  %s\n", res.Link)
				}
				if res.Invitation != nil {
					fmt.Fprintf(out, "\nIt works until %s. Signing in with that address accepts it.\n", res.Invitation.ExpiresAt.Local().Format("Jan 2, 2006 15:04"))
				}
			})
		},
	}
	cmd.Flags().StringVar(&role, "role", store.WorkspaceRoleMember, "workspace role: member, admin or owner")
	cmd.Flags().StringVar(&team, "team", "", "add them to this team when they join")
	return cmd
}

func newInvitationsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "invitations",
		Aliases: []string{"invitation"},
		Short:   "Pending invitations to the workspace",
		Long: `Pending invitations: people invited who have not signed in yet. A new link
for someone is another ` + "`shpyrd invite`" + `; revoking closes the door until they
are invited again.`,
		Args: cobra.NoArgs,
		RunE: newInvitationsListCmd(g).RunE, // bare `shpyrd invitations` lists
	}
	cmd.AddCommand(newInvitationsListCmd(g), newInvitationsRevokeCmd(g))
	return cmd
}

func (t *teamsAPI) invitations(ctx context.Context) ([]api.InvitationView, error) {
	var out []api.InvitationView
	err := t.call(ctx, "GET", "api/workspace/invitations", nil, &out)
	return out, err
}

func newInvitationsListCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List pending invitations",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			list, err := t.invitations(ctx)
			if err != nil {
				return err
			}
			if list == nil {
				list = []api.InvitationView{}
			}
			return g.print(cmd, list, func(w io.Writer) {
				if len(list) == 0 {
					fmt.Fprintln(w, "No pending invitations. Invite someone: `shpyrd invite ada@example.com`.")
					return
				}
				tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "EMAIL\tROLE\tTEAM\tINVITED BY\tEXPIRES")
				for _, inv := range list {
					expires := inv.ExpiresAt.Local().Format("Jan 2, 15:04")
					if inv.Expired {
						expires = "expired"
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", inv.Email, inv.Role, firstNonEmpty(inv.Team, "-"), firstNonEmpty(inv.InvitedBy, "-"), expires)
				}
				_ = tw.Flush()
			})
		},
	}
}

func newInvitationsRevokeCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <email>",
		Short: "Revoke someone's pending invitation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			email := strings.ToLower(strings.TrimSpace(args[0]))
			list, err := t.invitations(ctx)
			if err != nil {
				return err
			}
			for _, inv := range list {
				if inv.Email == email {
					if err := t.call(ctx, "DELETE", "api/workspace/invitations/"+url.PathEscape(inv.ID), nil, nil); err != nil {
						return err
					}
					return g.print(cmd, map[string]any{"email": email, "revoked": true}, func(w io.Writer) {
						fmt.Fprintf(w, "Revoked the invitation of %s.\n", email)
					})
				}
			}
			return fmt.Errorf("no pending invitation for %s (see `shpyrd invitations`)", email)
		},
	}
}

func article(word string) string {
	if word != "" && strings.ContainsAny(word[:1], "aeiou") {
		return "an"
	}
	return "a"
}

// ---- shpyrd sleep ----------------------------------------------------------

// newSleepCmd configures HTTP sleep for a project's web process: a quiet
// period of its own, off (awake even when the workspace has a default), or
// the workspace's default (#135).
func newSleepCmd(g *globalFlags) *cobra.Command {
	var after, resuming string
	cmd := &cobra.Command{
		Use:   "sleep <project>",
		Short: "Configure scale-to-zero for a project's web process",
		Long: `Scale the web process to zero when nobody has used the app for a while;
the first request wakes it. The person may choose:

  shpyrd sleep shop --after 15m --resuming page   # branded waking screen
  shpyrd sleep shop --after 30m --resuming wait   # hold the connection
  shpyrd sleep shop --after off                   # never sleeps, whatever the workspace's default
  shpyrd sleep shop --after default               # follow the workspace's default again

Needs the sleep extension on the cluster (shpyrd-ctl extensions enable sleep).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			// The batch process endpoint takes the sleep spec for web; the
			// server validates 5m..24h and page|wait, stores "off" as the
			// project's own policy and clears it on "default".
			body := map[string]any{"processes": map[string]any{
				"web": map[string]any{"sleep": map[string]any{"after": after, "resuming": resuming}},
			}}
			var out any
			if err := t.call(ctx, "POST", "api/projects/"+args[0]+"/processes", body, &out); err != nil {
				return err
			}
			return g.print(cmd, out, func(w io.Writer) {
				switch strings.ToLower(after) {
				case "off":
					fmt.Fprintf(w, "Sleep is off for %s: it stays awake, also when the workspace has a default.\n", args[0])
				case "", "default":
					fmt.Fprintf(w, "Sleep for %s follows the workspace's default.\n", args[0])
				default:
					fmt.Fprintf(w, "Sleep set: %s will scale to zero after %s of inactivity (%s mode).\n", args[0], after, firstNonEmpty(resuming, "wait"))
				}
			})
		},
	}
	cmd.Flags().StringVar(&after, "after", "", "quiet period before scaling to zero, e.g. 15m; 'off' keeps the app awake, 'default' follows the workspace")
	cmd.Flags().StringVar(&resuming, "resuming", "wait", "page (branded waking screen) or wait (hold the connection)")
	_ = cmd.MarkFlagRequired("after")
	return cmd
}
