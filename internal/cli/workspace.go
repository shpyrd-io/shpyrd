package cli

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/api"
)

// The workspace's names (RFC-0033): its address and its custom domains,
// for the people who run it, over the API like the Overview page.

func newWorkspaceCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "This workspace: its address and custom domains",
		Long: `The workspace you are signed in to: its name, address and domains.

  shpyrd workspace                                  # what it is and where it answers
  shpyrd workspace address acme-corp                # move to acme-corp.<parent>; the old address redirects for 30 days
  shpyrd workspace domains add intranet.acme.com    # a name your company owns (CNAME mode)
  shpyrd workspace domains verify intranet.acme.com
  shpyrd workspace domains primary intranet.acme.com`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			var ws api.WorkspaceView
			if err := t.call(ctx, "GET", "api/workspace", nil, &ws); err != nil {
				return err
			}
			return g.print(cmd, ws, func(w io.Writer) {
				tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
				fmt.Fprintf(tw, "NAME\t%s\n", ws.Name)
				fmt.Fprintf(tw, "IDENTIFIER\t%s\n", ws.Slug)
				if ws.Address != "" {
					fmt.Fprintf(tw, "ADDRESS\t%s\n", ws.Address)
				}
				fmt.Fprintf(tw, "DASHBOARD\t%s\n", ws.URL)
				if ws.Domain != "" {
					fmt.Fprintf(tw, "APPS\t<app>.%s\n", ws.Domain)
				}
				fmt.Fprintf(tw, "OWNERS\t%s\n", firstNonEmpty(strings.Join(ws.Owners, ", "), "-"))
				fmt.Fprintf(tw, "JOIN POLICY\t%s\n", ws.JoinPolicy)
				_ = tw.Flush()
			})
		},
	}
	cmd.AddCommand(newWorkspaceAddressCmd(g), newWorkspaceDomainsCmd(g))
	return cmd
}

func newWorkspaceAddressCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "address <label>",
		Short: "Move the workspace to another address under the same parent domain (owners)",
		Long: `Move the workspace: demo.shpyrd.app becomes <label>.shpyrd.app, and every app
<app>.<label>.shpyrd.app. The label cannot be another workspace's name or
address, or a reserved word. The old address redirects to the new one for
thirty days. People signed in sign in again at the new address.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			var ws api.WorkspaceView
			if err := t.call(ctx, "PATCH", "api/workspace", map[string]string{"address": strings.TrimSpace(args[0])}, &ws); err != nil {
				return err
			}
			return g.print(cmd, ws, func(w io.Writer) {
				fmt.Fprintf(w, "The workspace answers at %s now; apps at <app>.%s. The old address redirects for 30 days.\nSign in again: shpyrd login --url %s\n", ws.Address, ws.Domain, ws.URL)
			})
		},
	}
}

func newWorkspaceDomainsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domains",
		Short: "Custom domains: serve the workspace under a name your company owns",
		Long: `Custom domains (CNAME mode): point the name and its wildcard at the
workspace address, publish the TXT record to prove it, verify, and the
dashboard and every app answer there with certificates of their own. A
primary domain is what the dashboard and app links use; the address keeps
answering.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return workspaceDomainsList(cmd, g) },
	}
	cmd.AddCommand(
		&cobra.Command{Use: "list", Aliases: []string{"ls"}, Short: "List custom domains", RunE: func(cmd *cobra.Command, args []string) error { return workspaceDomainsList(cmd, g) }},
		newWorkspaceDomainActionCmd(g, "add", "Add a domain and print the DNS records to publish"),
		newWorkspaceDomainActionCmd(g, "verify", "Check the DNS records; a verified domain is served"),
		newWorkspaceDomainActionCmd(g, "primary", "Make a verified domain the one the workspace's URLs use (owners)"),
		newWorkspaceDomainActionCmd(g, "alias", "Make the primary domain an alias again (owners)"),
		newWorkspaceDomainActionCmd(g, "remove", "Remove a domain; its hosts stop answering"),
	)
	return cmd
}

func workspaceDomainsList(cmd *cobra.Command, g *globalFlags) error {
	ctx := signalContext()
	t, err := newTeamsAPI(g)
	if err != nil {
		return err
	}
	var list []api.DomainView
	if err := t.call(ctx, "GET", "api/workspace/domains", nil, &list); err != nil {
		return err
	}
	if list == nil {
		list = []api.DomainView{}
	}
	return g.print(cmd, list, func(w io.Writer) {
		if len(list) == 0 {
			fmt.Fprintln(w, "No custom domains. Add one: shpyrd workspace domains add intranet.acme.com")
			return
		}
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "HOST\tSTATUS\tPRIMARY")
		for _, d := range list {
			status := "waiting for DNS (shpyrd workspace domains verify " + d.Host + ")"
			if d.Verified {
				status = "verified"
			}
			fmt.Fprintf(tw, "%s\t%s\t%v\n", d.Host, status, d.Primary)
		}
		_ = tw.Flush()
	})
}

func printDomain(out io.Writer, d api.DomainView) {
	state := "waiting for DNS"
	if d.Verified {
		state = "verified"
	}
	if d.Primary {
		state += ", primary"
	}
	fmt.Fprintf(out, "%s: %s\n", d.Host, state)
	if !d.Verified {
		fmt.Fprintln(out, "Publish these records, then run: shpyrd workspace domains verify "+d.Host)
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TYPE\tNAME\tVALUE")
	for _, r := range d.Records {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Type, r.Name, r.Value)
	}
	_ = tw.Flush()
}

func newWorkspaceDomainActionCmd(g *globalFlags, action, short string) *cobra.Command {
	return &cobra.Command{
		Use:   action + " <host>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			t, err := newTeamsAPI(g)
			if err != nil {
				return err
			}
			host := strings.ToLower(strings.TrimSpace(args[0]))
			path := "api/workspace/domains/" + url.PathEscape(host)
			var d api.DomainView
			switch action {
			case "add":
				if err := t.call(ctx, "POST", "api/workspace/domains", map[string]string{"host": host}, &d); err != nil {
					return err
				}
			case "verify":
				if err := t.call(ctx, "POST", path+"/verify", nil, &d); err != nil {
					return err
				}
			case "primary", "alias":
				if err := t.call(ctx, "PATCH", path, map[string]bool{"primary": action == "primary"}, &d); err != nil {
					return err
				}
			case "remove":
				if err := t.call(ctx, "DELETE", path, nil, nil); err != nil {
					return err
				}
				return g.print(cmd, map[string]any{"host": host, "removed": true}, func(w io.Writer) {
					fmt.Fprintf(w, "Removed %s.\n", host)
				})
			}
			return g.print(cmd, d, func(w io.Writer) { printDomain(w, d) })
		},
	}
}
