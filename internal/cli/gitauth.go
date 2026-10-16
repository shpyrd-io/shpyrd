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

// Git credentials (RFC-0017): `shpyrd git-auth`. A workspace admin sets a
// token per host; a project gets a deploy key shpyrd generates. Values are
// write-only. Signed in only: the server stores and checks them.

func newGitAuthCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "git-auth",
		Short: "Give builds access to private git repositories",
		Long: `Builds clone a project's git repository. For a private one, give shpyrd access
either way:

  a deploy key for the project (any git host; read-only, one repository):
    shpyrd git-auth deploy-key --project shop
    then add the printed key to the repository as a read-only deploy key

  a token for a host, for every project of the workspace (workspace admins):
    shpyrd git-auth set --host gitlab.example.com --token -    (reads stdin)

A project's deploy key wins over the workspace's token.

  shpyrd git-auth show --project shop     which credential the project's builds use
  shpyrd git-auth list                    the workspace's tokens (never their values)
  shpyrd git-auth unset --host gitlab.example.com
  shpyrd git-auth deploy-key --project shop --remove`,
	}
	var host, token, username string
	set := &cobra.Command{
		Use:   "set --host <host> --token <token|->",
		Short: "Set the workspace's token for a git host",
		RunE: func(cmd *cobra.Command, args []string) error {
			if host == "" || token == "" {
				return errors.New("give --host and --token (- reads it from stdin)")
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
			body, _ := json.Marshal(api.GitTokenRequest{Token: token, Username: username})
			var v api.GitTokenView
			if err := gitAuthRequest(g, cmd, "PUT", "api/workspace/git-auth/"+url.PathEscape(host), body, &v); err != nil {
				return err
			}
			return g.print(cmd, v, func(w io.Writer) {
				fmt.Fprintf(w, "Token for %s set (user %s). Builds of the workspace's projects on %s use it.\n", v.Host, v.Username, v.Host)
			})
		},
	}
	set.Flags().StringVar(&host, "host", "", "git host, such as gitlab.example.com")
	set.Flags().StringVar(&token, "token", "", "the token; - reads it from stdin (keeps it out of shell history)")
	set.Flags().StringVar(&username, "username", "", "user name sent with the token (default per host: x-access-token, oauth2 on gitlab.com)")

	var unsetHost string
	unset := &cobra.Command{
		Use:   "unset --host <host>",
		Short: "Remove the workspace's token for a git host",
		RunE: func(cmd *cobra.Command, args []string) error {
			if unsetHost == "" {
				return errors.New("give --host")
			}
			if err := gitAuthRequest(g, cmd, "DELETE", "api/workspace/git-auth/"+url.PathEscape(unsetHost), nil, nil); err != nil {
				return err
			}
			return g.print(cmd, map[string]string{"host": unsetHost}, func(w io.Writer) {
				fmt.Fprintf(w, "Token for %s removed.\n", unsetHost)
			})
		},
	}
	unset.Flags().StringVar(&unsetHost, "host", "", "git host")

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the workspace's git tokens (hosts, never values)",
		RunE: func(cmd *cobra.Command, args []string) error {
			var vs []api.GitTokenView
			if err := gitAuthRequest(g, cmd, "GET", "api/workspace/git-auth", nil, &vs); err != nil {
				return err
			}
			return g.print(cmd, map[string]any{"tokens": vs}, func(w io.Writer) {
				if len(vs) == 0 {
					fmt.Fprintln(w, "no git tokens set")
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

	var appName, keyHost string
	var remove bool
	deployKey := &cobra.Command{
		Use:   "deploy-key [--project <name>] [--host <host>] [--remove]",
		Short: "Make a deploy key for the project and print its public half",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveAppName(appName)
			if err != nil {
				return err
			}
			path := "api/projects/" + url.PathEscape(name) + "/git-auth/deploy-key"
			if remove {
				if err := gitAuthRequest(g, cmd, "DELETE", path, nil, nil); err != nil {
					return err
				}
				return g.print(cmd, map[string]string{"project": name}, func(w io.Writer) {
					fmt.Fprintf(w, "Deploy key of %s removed. Remove it from the repository too.\n", name)
				})
			}
			body, _ := json.Marshal(api.DeployKeyRequest{Host: keyHost})
			var v api.DeployKeyView
			if err := gitAuthRequest(g, cmd, "POST", path, body, &v); err != nil {
				return err
			}
			return g.print(cmd, v, func(w io.Writer) {
				fmt.Fprintf(w, "Add this key to the repository on %s as a read-only deploy key:\n\n  %s\n\n", v.Host, v.PublicKey)
				fmt.Fprintf(w, "On GitHub: the repository's Settings, Deploy keys, Add deploy key (leave write access off).\n")
				fmt.Fprintf(w, "%s's host key was recorded as %s; builds refuse a host that presents another.\n", v.Host, v.HostKeyFingerprint)
				fmt.Fprintf(w, "Then deploy again. A new deploy-key replaces this one.\n")
			})
		},
	}
	appFlag(deployKey, &appName)
	deployKey.Flags().StringVar(&keyHost, "host", "", "git host (default: the project's repository's)")
	deployKey.Flags().BoolVar(&remove, "remove", false, "remove the project's deploy key")

	var showApp string
	show := &cobra.Command{
		Use:   "show [--project <name>]",
		Short: "Say which credential the project's builds clone with",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveAppName(showApp)
			if err != nil {
				return err
			}
			var v api.ProjectGitAuthView
			if err := gitAuthRequest(g, cmd, "GET", "api/projects/"+url.PathEscape(name)+"/git-auth", nil, &v); err != nil {
				return err
			}
			return g.print(cmd, v, func(w io.Writer) {
				switch v.Using {
				case "deploy-key":
					fmt.Fprintf(w, "%s clones %s with its deploy key %s (for %s).\n", name, v.URL, v.DeployKey.Fingerprint, v.DeployKey.Host)
				case "token":
					fmt.Fprintf(w, "%s clones %s with the workspace's token for %s (user %s).\n", name, v.URL, v.Token.Host, v.Token.Username)
				default:
					fmt.Fprintf(w, "%s clones %s without credentials: fine for a public repository. For a private one: shpyrd git-auth --help\n", name, firstNonEmpty(v.URL, "(no git repository)"))
				}
			})
		},
	}
	appFlag(show, &showApp)

	cmd.AddCommand(set, unset, list, deployKey, show)
	return cmd
}

// gitAuthRequest calls the API signed in; git credentials are not managed
// over a kubeconfig.
func gitAuthRequest(g *globalFlags, cmd *cobra.Command, method, path string, body []byte, out any) error {
	ctx := signalContext()
	ac, err := newAppClient(g, g.progress(cmd))
	if err != nil {
		return err
	}
	if !ac.session {
		return errors.New("git credentials are managed signed in: run shpyrd login first")
	}
	ct := ""
	if body != nil {
		ct = "application/json"
	}
	raw, err := ac.serverRequest(ctx, method, path, body, ct)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("unexpected response: %s", truncate(string(raw), 200))
	}
	return nil
}
