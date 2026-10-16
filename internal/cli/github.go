package cli

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/api"
)

// Connect GitHub (RFC-0054): `shpyrd github`. A workspace admin connects
// an account or organisation where the cluster's GitHub App is installed;
// then whoever makes projects lists its repositories. Signed in only.

var (
	// githubPollEvery and githubConnectWait pace `shpyrd github connect`.
	githubPollEvery   = 3 * time.Second
	githubConnectWait = 10 * time.Minute
)

func githubRequest(g *globalFlags, cmd *cobra.Command, method, path string, out any) error {
	return signedInRequest(g, cmd, "GitHub is connected signed in: run shpyrd login first", method, path, nil, out)
}

// kindOf says GitHub's account type in words.
func kindOf(accountType string) string {
	if accountType == "Organization" {
		return "organisation"
	}
	return "personal account"
}

func newGitHubCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github",
		Short: "Connect GitHub: pick repositories, private ones build, every push deploys",
		Long: `Connect an account or organisation once (workspace admins). Then anyone who
makes projects picks one of its repositories: private ones build without keys
or tokens, and every push to a project's branch deploys.

  shpyrd github connect               open the dashboard to connect, and wait for it
  shpyrd github list                  the workspace's connections
  shpyrd github disconnect <account>  forget one (the App stays installed on GitHub)
  shpyrd github repos [--filter TEXT] the repositories the connections reach

Then: shpyrd deploy --git https://github.com/<owner>/<repo> --ref <branch>`,
	}
	var noBrowser bool
	connect := &cobra.Command{
		Use:   "connect",
		Short: "Open the dashboard's Git page to connect an account or organisation, and wait for it",
		RunE: func(cmd *cobra.Command, args []string) error {
			var before api.GitHubView
			if err := githubRequest(g, cmd, "GET", "api/workspace/github", &before); err != nil {
				return err
			}
			if !before.Configured {
				return errors.New(firstNonEmpty(before.Note, "GitHub is not set up on this platform."))
			}
			// The connect finishes on the workspace's own page, for the
			// identity that started it; a CLI token is another identity, so
			// the page starts it and the CLI only waits.
			page := workspaceURL() + "/workspace/git"
			out := g.progress(cmd)
			if !noBrowser && browse(page) == nil {
				fmt.Fprintf(out, "Opened %s (if nothing opened, open it yourself).\n", page)
			} else {
				fmt.Fprintf(out, "Open %s\n", page)
			}
			fmt.Fprintln(out, "Connect GitHub there; waiting for the connection…")
			known := map[int64]bool{}
			for _, i := range before.Installations {
				known[i.ID] = true
			}
			ctx := signalContext()
			deadline := time.Now().Add(githubConnectWait)
			for {
				select {
				case <-ctx.Done():
					return errors.New("cancelled; a connection made on the page still counts: shpyrd github list")
				case <-time.After(githubPollEvery):
				}
				var now api.GitHubView
				if err := githubRequest(g, cmd, "GET", "api/workspace/github", &now); err != nil {
					return err
				}
				for _, inst := range now.Installations {
					if known[inst.ID] {
						continue
					}
					return g.print(cmd, inst, func(w io.Writer) {
						fmt.Fprintf(w, "Connected %s (%s), installation %d. Its repositories: shpyrd github repos\n", inst.Account, kindOf(inst.Type), inst.ID)
					})
				}
				if time.Now().After(deadline) {
					return errors.New("no new connection after 10 minutes: run shpyrd github connect again")
				}
			}
		},
	}
	connect.Flags().BoolVar(&noBrowser, "no-browser", false, "print the page link instead of opening it")

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the workspace's GitHub connections",
		RunE: func(cmd *cobra.Command, args []string) error {
			var v api.GitHubView
			if err := githubRequest(g, cmd, "GET", "api/workspace/github", &v); err != nil {
				return err
			}
			return g.print(cmd, v, func(w io.Writer) {
				switch {
				case !v.Configured:
					fmt.Fprintln(w, v.Note)
				case len(v.Installations) == 0:
					fmt.Fprintln(w, "No GitHub connections. A workspace admin connects one: shpyrd github connect")
				default:
					tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
					fmt.Fprintln(tw, "ACCOUNT\tKIND\tID\tCONNECTED BY\tSTATE")
					for _, i := range v.Installations {
						state := "ok"
						if i.Suspended {
							state = "suspended on GitHub"
						}
						fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n", i.Account, kindOf(i.Type), i.ID, i.ConnectedBy, state)
					}
					_ = tw.Flush()
				}
			})
		},
	}

	disconnect := &cobra.Command{
		Use:   "disconnect <account|id>",
		Short: "Forget a connection; the App stays installed on GitHub",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var v api.GitHubView
			if err := githubRequest(g, cmd, "GET", "api/workspace/github", &v); err != nil {
				return err
			}
			var found *api.GitHubInstallationView
			for i := range v.Installations {
				inst := &v.Installations[i]
				if strings.EqualFold(inst.Account, args[0]) || strconv.FormatInt(inst.ID, 10) == args[0] {
					found = inst
				}
			}
			if found == nil {
				return fmt.Errorf("this workspace has no GitHub connection named %s: shpyrd github list", args[0])
			}
			if err := githubRequest(g, cmd, "DELETE", "api/workspace/github/installations/"+url.PathEscape(strconv.FormatInt(found.ID, 10)), nil); err != nil {
				return err
			}
			return g.print(cmd, map[string]any{"disconnected": found.ID}, func(w io.Writer) {
				fmt.Fprintf(w, "Disconnected %s: its repositories no longer build through the App and its pushes no longer deploy. The App stays installed on GitHub; remove it at %s\n", found.Account, found.ManageURL)
			})
		},
	}

	var filter string
	repos := &cobra.Command{
		Use:   "repos [--filter TEXT]",
		Short: "List the repositories the workspace's connections reach",
		RunE: func(cmd *cobra.Command, args []string) error {
			var all []api.GitHubRepositoryView
			if err := githubRequest(g, cmd, "GET", "api/workspace/github/repositories", &all); err != nil {
				return err
			}
			shown := []api.GitHubRepositoryView{}
			for _, r := range all {
				if strings.Contains(strings.ToLower(r.FullName), strings.ToLower(filter)) {
					shown = append(shown, r)
				}
			}
			return g.print(cmd, map[string]any{"repositories": shown}, func(w io.Writer) {
				if len(shown) == 0 {
					fmt.Fprintln(w, "No repository matches. A connection reaches what its installation includes: change it on GitHub.")
					return
				}
				tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "REPOSITORY\tVISIBILITY\tDEFAULT BRANCH")
				for _, r := range shown {
					visibility := "public"
					if r.Private {
						visibility = "private"
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\n", r.FullName, visibility, r.DefaultBranch)
				}
				_ = tw.Flush()
				fmt.Fprintln(w, "\nDeploy one: shpyrd deploy --git https://github.com/<repository> --ref <branch> --project <name>")
			})
		},
	}
	repos.Flags().StringVar(&filter, "filter", "", "only repositories whose name contains this")

	cmd.AddCommand(connect, list, disconnect, repos)
	return cmd
}

// workspaceURL is the workspace the CLI talks to, as serverRequest picks it.
func workspaceURL() string {
	u := os.Getenv("SHPYRD_URL")
	if u == "" {
		u = loadSessions().activeURL()
	}
	return strings.TrimRight(u, "/")
}
