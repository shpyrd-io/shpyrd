package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/api"
)

// shpyrd-ctl console-users: who may open the operator's console, by email,
// independent of every workspace. The first one is added over the
// kubeconfig, which is the operator's own credential.
func newConsoleUsersCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "console-users",
		Short: "Who may open the console",
		Long: `The console's administrators are selected by principal ID, independently
of workspace permissions. Sign in once through the console's identity provider,
then select that principal. Credentials stay at the identity provider.`,
	}
	cmd.AddCommand(newConsoleUsersListCmd(g), newConsoleUsersAddCmd(g), newConsoleUsersRemoveCmd(g))
	return cmd
}

func newConsoleUsersListCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the console's users",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			raw, err := serverRequest(ctx, ac.k, "GET", "api/cluster/console-users", nil, "")
			if err != nil {
				return err
			}
			list := []api.ConsoleUserView{}
			if err := json.Unmarshal(raw, &list); err != nil {
				return err
			}
			return g.print(cmd, list, func(out io.Writer) {
				if len(list) == 0 {
					fmt.Fprintln(out, "No console users: only the kubeconfig and the admin token open the console.")
					return
				}
				w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "PRINCIPAL\tEMAIL\tADDED\tBY")
				for _, u := range list {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", u.PrincipalID, u.Email, u.AddedAt.Format("2006-01-02"), firstNonEmpty(u.AddedBy, "-"))
				}
				w.Flush()
			})
		},
	}
}

func newConsoleUsersAddCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <principal-id>",
		Short: "Let someone open the console",
		Long:  "Authorize an existing console principal by its immutable ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			body, _ := json.Marshal(api.AddConsoleUserRequest{PrincipalID: args[0]})
			raw, err := serverRequest(ctx, ac.k, "POST", "api/cluster/console-users", body, "application/json")
			if err != nil {
				return err
			}
			var u api.ConsoleUserView
			if err := json.Unmarshal(raw, &u); err != nil {
				return err
			}
			return g.print(cmd, u, func(w io.Writer) {
				fmt.Fprintf(w, "%s may open the console.\n", u.Email)

			})
		},
	}
	return cmd
}

func newConsoleUsersRemoveCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <email>",
		Aliases: []string{"rm"},
		Short:   "Take someone off the console",
		Long:    "Take an email off the console's list. Their account, if any, stays: it may still open workspaces.",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			if _, err := serverRequest(ctx, ac.k, "DELETE", "api/cluster/console-users/"+url.PathEscape(strings.ToLower(args[0])), nil, ""); err != nil {
				return err
			}
			return g.print(cmd, map[string]string{"removed": args[0]}, func(w io.Writer) {
				fmt.Fprintf(w, "%s may no longer open the console.\n", args[0])
			})
		},
	}
}
