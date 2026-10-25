package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/api"
)

// A workspace's own application (RFC-0084): `shpyrd workspace ui` sets
// the reference of a built workspace application the door serves in
// place of the platform's own; `shpyrd registry-auth` holds the read
// credential of the registry it is pulled from. Signed in only.

func newWorkspaceUICmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "The application this workspace's door serves (admins)",
		Long: `A workspace may run a workspace application of its own: a build of the
platform's apps/workspace, published to a registry as an artifact or an
image, served at the workspace's door in place of the platform's own.

  shpyrd workspace ui                                              # what the door serves, and how it is doing
  shpyrd workspace ui set oci://ghcr.io/acme/workspace-ui:2.3.1   # a tag is settled to its digest now
  shpyrd workspace ui set oci://ghcr.io/acme/workspace-ui@sha256:...
  shpyrd workspace ui clear                                        # back to the platform's own
  shpyrd workspace ui reload                                       # load it again (a rebuilt directory, a failed pull)

A private repository needs a read credential first: shpyrd registry-auth set.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var ws api.WorkspaceView
			if err := signedInRequest(g, cmd, "the workspace's application is managed signed in: run shpyrd login first", "GET", "api/workspace", nil, &ws); err != nil {
				return err
			}
			return printWorkspaceUI(g, cmd, ws)
		},
	}
	set := &cobra.Command{
		Use:   "set <reference>",
		Short: "Serve an application at the door: oci://host/repository[:tag][@sha256:digest]",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"ui": strings.TrimSpace(args[0])})
			var ws api.WorkspaceView
			if err := signedInRequest(g, cmd, "the workspace's application is managed signed in: run shpyrd login first", "PATCH", "api/workspace", body, &ws); err != nil {
				return err
			}
			return printWorkspaceUI(g, cmd, ws)
		},
	}
	clear := &cobra.Command{
		Use:   "clear",
		Short: "Serve the platform's own application again",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"ui": ""})
			var ws api.WorkspaceView
			if err := signedInRequest(g, cmd, "the workspace's application is managed signed in: run shpyrd login first", "PATCH", "api/workspace", body, &ws); err != nil {
				return err
			}
			return g.print(cmd, ws, func(w io.Writer) {
				fmt.Fprintln(w, "The door serves the platform's own application.")
			})
		},
	}
	reload := &cobra.Command{
		Use:   "reload",
		Short: "Load the application again on the next request",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var ws api.WorkspaceView
			if err := signedInRequest(g, cmd, "the workspace's application is managed signed in: run shpyrd login first", "POST", "api/workspace/ui/reload", nil, &ws); err != nil {
				return err
			}
			return g.print(cmd, ws, func(w io.Writer) {
				fmt.Fprintln(w, "The application is loaded again on the next request.")
			})
		},
	}
	cmd.AddCommand(set, clear, reload)
	return cmd
}

func printWorkspaceUI(g *globalFlags, cmd *cobra.Command, ws api.WorkspaceView) error {
	return g.print(cmd, ws, func(w io.Writer) {
		if ws.UI == nil {
			fmt.Fprintln(w, "The door serves the platform's own application.")
			return
		}
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "REFERENCE\t%s\n", ws.UI.Ref)
		if ws.UI.SetBy != "" {
			fmt.Fprintf(tw, "SET BY\t%s\n", ws.UI.SetBy)
		}
		if st := ws.UI.Status; st != nil {
			fmt.Fprintf(tw, "STATUS\t%s\n", st.State)
			if st.Name != "" || st.Version != "" {
				fmt.Fprintf(tw, "PACKAGE\t%s\n", strings.TrimSpace(st.Name+" "+st.Version))
			}
			if st.Error != "" {
				fmt.Fprintf(tw, "WHY\t%s\n", st.Error)
			}
		}
		_ = tw.Flush()
	})
}

func newRegistryAuthCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry-auth",
		Short: "Read credentials for the registries the workspace's application is pulled from (admins)",
		Long: `The workspace's application (shpyrd workspace ui) is pulled from a registry.
For a private repository, give the workspace a read-only credential for that
host, one per host. Values are written, never shown.

  shpyrd registry-auth set --host ghcr.io --username acme-bot --token -    (reads stdin)
  shpyrd registry-auth list
  shpyrd registry-auth unset --host ghcr.io`,
	}
	var host, username, token string
	set := &cobra.Command{
		Use:   "set --host <host> --username <user> --token <token|->",
		Short: "Set the workspace's credential for a registry host",
		RunE: func(cmd *cobra.Command, args []string) error {
			if host == "" || username == "" || token == "" {
				return errors.New("give --host, --username and --token (- reads it from stdin)")
			}
			if token == "-" {
				line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) {
					return err
				}
				token = strings.TrimSpace(line)
				if token == "" {
					return errors.New("no token on stdin")
				}
			}
			body, _ := json.Marshal(api.RegistryTokenRequest{Username: username, Token: token})
			var v api.RegistryTokenView
			if err := signedInRequest(g, cmd, "registry credentials are managed signed in: run shpyrd login first", "PUT", "api/workspace/registry-auth/"+url.PathEscape(host), body, &v); err != nil {
				return err
			}
			return g.print(cmd, v, func(w io.Writer) {
				fmt.Fprintf(w, "Credential for %s set (user %s). The workspace's application is pulled from %s with it.\n", v.Host, v.Username, v.Host)
			})
		},
	}
	set.Flags().StringVar(&host, "host", "", "registry host, such as ghcr.io")
	set.Flags().StringVar(&username, "username", "", "user name sent with the token")
	set.Flags().StringVar(&token, "token", "", "a read-only token; - reads it from stdin (keeps it out of shell history)")

	var unsetHost string
	unset := &cobra.Command{
		Use:   "unset --host <host>",
		Short: "Remove the workspace's credential for a registry host",
		RunE: func(cmd *cobra.Command, args []string) error {
			if unsetHost == "" {
				return errors.New("give --host")
			}
			if err := signedInRequest(g, cmd, "registry credentials are managed signed in: run shpyrd login first", "DELETE", "api/workspace/registry-auth/"+url.PathEscape(unsetHost), nil, nil); err != nil {
				return err
			}
			return g.print(cmd, map[string]string{"host": unsetHost}, func(w io.Writer) {
				fmt.Fprintf(w, "Credential for %s removed.\n", unsetHost)
			})
		},
	}
	unset.Flags().StringVar(&unsetHost, "host", "", "registry host")

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the workspace's registry credentials (hosts, never values)",
		RunE: func(cmd *cobra.Command, args []string) error {
			var vs []api.RegistryTokenView
			if err := signedInRequest(g, cmd, "registry credentials are managed signed in: run shpyrd login first", "GET", "api/workspace/registry-auth", nil, &vs); err != nil {
				return err
			}
			return g.print(cmd, map[string]any{"credentials": vs}, func(w io.Writer) {
				if len(vs) == 0 {
					fmt.Fprintln(w, "no registry credentials set")
					return
				}
				tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "HOST\tUSER\tSET BY")
				for _, v := range vs {
					fmt.Fprintf(tw, "%s\t%s\t%s\n", v.Host, v.Username, firstNonEmpty(v.UpdatedBy, "-"))
				}
				_ = tw.Flush()
			})
		},
	}
	cmd.AddCommand(set, unset, list)
	return cmd
}
