package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

// githubNotLinked starts the server's 404 when no GitHub account is linked.
const githubNotLinked = "no GitHub account is linked"

// githubLinkRequired turns the server's github_link_required answer into
// what to run; nil for any other answer.
func githubLinkRequired(status int, raw []byte) error {
	if status != http.StatusConflict {
		return nil
	}
	var body struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Code != "github_link_required" {
		return nil
	}
	return errors.New("link your GitHub account first: run shpyrd github link")
}

// openOrPrint opens a page in the browser, or prints its address when
// asked not to or when nothing opens.
func openOrPrint(out io.Writer, page string, noBrowser bool) {
	if !noBrowser && browse(page) == nil {
		fmt.Fprintf(out, "Opened %s (if nothing opened, open it yourself).\n", page)
	} else {
		fmt.Fprintf(out, "Open %s\n", page)
	}
}

// waitForBrowser polls step every githubPollEvery until it reports done,
// for up to githubConnectWait. A step error is a blip: three in a row end
// the wait. On done, the error is the step's own (what printing returned).
func waitForBrowser(cancelled, expired string, step func() (done bool, err error)) error {
	ctx := signalContext()
	deadline := time.Now().Add(githubConnectWait)
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return errors.New(cancelled)
		case <-time.After(githubPollEvery):
		}
		done, err := step()
		if done {
			return err
		}
		if err != nil {
			if failures++; failures > 3 {
				return err
			}
		} else {
			failures = 0
		}
		if time.Now().After(deadline) {
			return errors.New(expired)
		}
	}
}

func githubRequest(g *globalFlags, cmd *cobra.Command, method, path string, out any) error {
	return signedInRequest(g, cmd, "GitHub connections are managed signed in: run shpyrd login first.", method, path, nil, out)
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
		Short: "GitHub: link your account, connect accounts, pick repositories; every push deploys",
		Long: `Link your own GitHub account once: New project, the CLI and your agents then
list and use the repositories you can read on GitHub. A workspace admin
connects an account or organisation once; private repositories then build
without keys or tokens, and every push to a project's branch deploys.

  shpyrd github link                  open the browser to link your GitHub account, and wait for it
  shpyrd github unlink                unlink it (your projects keep deploying)
  shpyrd github connect               open the dashboard to connect, and wait for it
  shpyrd github list                  the workspace's connections
  shpyrd github disconnect <account>  forget one (the App stays installed on GitHub)
  shpyrd github repos [--filter TEXT] the repositories you can read through the connections

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
			openOrPrint(out, page, noBrowser)
			fmt.Fprintln(out, "Connect GitHub there; waiting for the connection…")
			known := map[int64]bool{}
			for _, i := range before.Installations {
				known[i.ID] = true
			}
			return waitForBrowser(
				"cancelled; a connection made on the page still counts: shpyrd github list",
				"no new connection after 10 minutes: run shpyrd github connect again",
				func() (bool, error) {
					var now api.GitHubView
					if err := githubRequest(g, cmd, "GET", "api/workspace/github", &now); err != nil {
						return false, err
					}
					for _, inst := range now.Installations {
						if known[inst.ID] {
							continue
						}
						return true, g.print(cmd, inst, func(w io.Writer) {
							fmt.Fprintf(w, "Connected %s (%s), installation %d. Its repositories: shpyrd github repos\n", inst.Account, kindOf(inst.Type), inst.ID)
						})
					}
					return false, nil
				})
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
			// A number equal to an id names that connection; otherwise the
			// account, which must be one connection only.
			var matches []*api.GitHubInstallationView
			for i := range v.Installations {
				if strconv.FormatInt(v.Installations[i].ID, 10) == args[0] {
					matches = []*api.GitHubInstallationView{&v.Installations[i]}
					break
				}
				if strings.EqualFold(v.Installations[i].Account, args[0]) {
					matches = append(matches, &v.Installations[i])
				}
			}
			if len(matches) == 0 {
				return fmt.Errorf("this workspace has no GitHub connection named %s: shpyrd github list", args[0])
			}
			if len(matches) > 1 {
				ids := make([]string, len(matches))
				for i, m := range matches {
					ids[i] = strconv.FormatInt(m.ID, 10)
				}
				return fmt.Errorf("%s matches several connections (ids %s): give the id", args[0], strings.Join(ids, ", "))
			}
			found := matches[0]
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
		Short: "List the repositories you can read through the workspace's connections",
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
					fmt.Fprintln(w, "No repository matches. You see what your GitHub account can read in the connected accounts: shpyrd github link, or ask for access on GitHub.")
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

	var linkNoBrowser bool
	link := &cobra.Command{
		Use:   "link",
		Short: "Link your GitHub account in the browser, and wait for it",
		RunE: func(cmd *cobra.Command, args []string) error {
			var me api.GitHubLinkView
			err := githubRequest(g, cmd, "GET", "api/me/github", &me)
			if err == nil {
				return g.print(cmd, me, func(w io.Writer) {
					fmt.Fprintf(w, "Your GitHub account is linked: %s. To link another, run shpyrd github unlink first.\n", me.Login)
				})
			}
			if !strings.Contains(err.Error(), githubNotLinked) {
				return err
			}
			// The link finishes in the browser, signed in there: the page
			// goes on to GitHub, and the CLI waits for the link to appear.
			page := workspaceURL() + "/api/me/github/link"
			out := g.progress(cmd)
			openOrPrint(out, page, linkNoBrowser)
			fmt.Fprintln(out, "Continue with GitHub there; waiting for the link…")
			return waitForBrowser(
				"cancelled; a link made in the browser still counts: shpyrd whoami",
				"not linked after 10 minutes: run shpyrd github link again",
				func() (bool, error) {
					var now api.GitHubLinkView
					err := githubRequest(g, cmd, "GET", "api/me/github", &now)
					switch {
					case err == nil:
						return true, g.print(cmd, now, func(w io.Writer) {
							fmt.Fprintf(w, "Linked GitHub account %s. The repositories it can read: shpyrd github repos\n", now.Login)
						})
					case strings.Contains(err.Error(), githubNotLinked):
						return false, nil
					}
					return false, err
				})
		},
	}
	link.Flags().BoolVar(&linkNoBrowser, "no-browser", false, "print the page link instead of opening it")

	unlink := &cobra.Command{
		Use:   "unlink",
		Short: "Unlink your GitHub account; your projects keep deploying",
		RunE: func(cmd *cobra.Command, args []string) error {
			var me api.GitHubLinkView
			if err := githubRequest(g, cmd, "GET", "api/me/github", &me); err != nil {
				if strings.Contains(err.Error(), githubNotLinked) {
					return errors.New(githubNotLinked)
				}
				return err
			}
			if err := githubRequest(g, cmd, "DELETE", "api/me/github", nil); err != nil {
				return err
			}
			return g.print(cmd, map[string]string{"unlinked": me.Login}, func(w io.Writer) {
				fmt.Fprintf(w, "Unlinked %s; its token is revoked on GitHub. Your projects keep deploying; picking a private repository needs a link again.\n", me.Login)
			})
		},
	}

	cmd.AddCommand(link, unlink, connect, list, disconnect, repos)
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
