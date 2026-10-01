package authlocal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// The workspace's own sign-in methods (RFC-0033 per-workspace SSO), for
// people who build: over the API, like the dashboard's Sign-in tab, so it
// works for a hosted workspace without any cluster access.

func newSSOCmd(g ext.CLIGlobals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sso",
		Short: "This workspace's sign-in methods: your company's Google, Microsoft, GitHub or OpenID Connect",
		Long: `The sign-in methods of this workspace. Add your company's identity provider
and it appears on this workspace's login page (only there); groups of the
provider map to teams. Register the callback URL printed by ` + "`shpyrd sso list`" + `
at the provider first.

  shpyrd sso add google --client-id ... --client-secret @secret.txt --hosted-domain acme.com
  shpyrd sso add oidc --issuer https://acme.okta.com --client-id ... --client-secret @secret.txt --label Okta
  shpyrd sso list
  shpyrd sso remove google

Once your method works, stop offering the platform's methods with
` + "`shpyrd sso platform-methods off`" + `.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return ssoList(cmd, g) },
	}
	cmd.AddCommand(newSSOListCmd(g), newSSOAddCmd(g), newSSORemoveCmd(g), newSSOPlatformCmd(g))
	return cmd
}

func ssoCall(ctx context.Context, g ext.CLIGlobals, method, path string, body any, out any) error {
	var raw []byte
	var ct string
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		raw, ct = b, "application/json"
	}
	resp, err := g.API().Request(ctx, method, path, raw, ct)
	if err != nil {
		return err
	}
	if out != nil && len(resp) > 0 {
		return json.Unmarshal(resp, out)
	}
	return nil
}

func ssoList(cmd *cobra.Command, g ext.CLIGlobals) error {
	ctx := cliContext()
	var out LoginMethods
	if err := ssoCall(ctx, g, "GET", "api/workspace/login-methods", nil, &out); err != nil {
		return err
	}
	if out.Connectors == nil {
		out.Connectors = []Connector{}
	}
	return ext.Print(g, cmd, out, func(w io.Writer) {
		if len(out.Connectors) == 0 {
			fmt.Fprintln(w, "This workspace has no sign-in method of its own yet; the platform's are offered.")
		} else {
			tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tTYPE\tLABEL\tRESTRICTION")
			for _, c := range out.Connectors {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.ID, c.Type, c.Name, firstNonEmpty(c.Detail, "-"))
			}
			_ = tw.Flush()
		}
		fmt.Fprintf(w, "\nCallback URL to register at the provider: %s\n", out.Callback)
	})
}

func newSSOListCmd(g ext.CLIGlobals) *cobra.Command {
	return &cobra.Command{Use: "list", Aliases: []string{"ls"}, Short: "List this workspace's sign-in methods", RunE: func(cmd *cobra.Command, args []string) error { return ssoList(cmd, g) }}
}

func newSSOAddCmd(g ext.CLIGlobals) *cobra.Command {
	var req connectorRequest
	cmd := &cobra.Command{
		Use:   "add <github|google|microsoft|oidc>",
		Short: "Add a sign-in method of this workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Type = args[0]
			if strings.HasPrefix(req.ClientSecret, "@") {
				raw, err := os.ReadFile(strings.TrimPrefix(req.ClientSecret, "@"))
				if err != nil {
					return err
				}
				req.ClientSecret = strings.TrimSpace(string(raw))
			}
			if req.ClientID == "" || req.ClientSecret == "" {
				return errors.New("--client-id and --client-secret are required")
			}
			ctx := cliContext()
			var out Connector
			if err := ssoCall(ctx, g, "POST", "api/workspace/login-methods", req, &out); err != nil {
				return err
			}
			return ext.Print(g, cmd, out, func(w io.Writer) {
				fmt.Fprintf(w, "Added %s (%s): the button is on this workspace's login page now.\n", out.Name, out.ID)
			})
		},
	}
	cmd.Flags().StringVar(&req.ID, "id", "", "method id within the workspace (default: the type)")
	cmd.Flags().StringVar(&req.Name, "label", "", "button text on the login page (default: the provider's name)")
	cmd.Flags().StringVar(&req.ClientID, "client-id", "", "OAuth application client id")
	cmd.Flags().StringVar(&req.ClientSecret, "client-secret", "", "OAuth application client secret, or @path to read it from a file")
	cmd.Flags().StringVar(&req.Org, "org", "", "GitHub: only members of this organisation may sign in; its teams become groups")
	cmd.Flags().StringVar(&req.HostedDomain, "hosted-domain", "", "Google: only accounts of this Workspace domain may sign in")
	cmd.Flags().StringVar(&req.Tenant, "tenant", "", "Microsoft: only accounts of this Entra tenant (id or domain) may sign in")
	cmd.Flags().StringVar(&req.Issuer, "issuer", "", "oidc: the provider's issuer URL (Okta, Keycloak, Auth0, ...)")
	return cmd
}

func newSSORemoveCmd(g ext.CLIGlobals) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <id>",
		Aliases: []string{"rm"},
		Short:   "Remove a sign-in method of this workspace",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cliContext()
			if err := ssoCall(ctx, g, "DELETE", "api/workspace/login-methods/"+url.PathEscape(args[0]), nil, nil); err != nil {
				return err
			}
			return ext.Print(g, cmd, map[string]any{"id": args[0], "removed": true}, func(w io.Writer) {
				fmt.Fprintf(w, "Removed %s; people signed in through it keep their sessions.\n", args[0])
			})
		},
	}
}

func newSSOPlatformCmd(g ext.CLIGlobals) *cobra.Command {
	return &cobra.Command{
		Use:   "platform-methods <on|off>",
		Short: "Offer the platform's sign-in methods on this workspace's login page, or only your own",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var own bool
			switch args[0] {
			case "on":
				own = false
			case "off":
				own = true
			default:
				return errors.New("say on or off")
			}
			ctx := cliContext()
			var out struct {
				OwnMethodsOnly bool `json:"ownMethodsOnly"`
			}
			if err := ssoCall(ctx, g, "PATCH", "api/workspace", map[string]bool{"ownMethodsOnly": own}, &out); err != nil {
				return err
			}
			return ext.Print(g, cmd, out, func(w io.Writer) {
				if out.OwnMethodsOnly {
					fmt.Fprintln(w, "Only this workspace's own methods are offered now.")
				} else {
					fmt.Fprintln(w, "The platform's methods are offered too.")
				}
			})
		},
	}
}
