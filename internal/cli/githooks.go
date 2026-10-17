package cli

import (
	"fmt"
	"io"
	"net/url"

	"github.com/spf13/cobra"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

// Deploy on push (RFC-0018): `shpyrd git-hooks`, the webhook a git host
// calls when the project's branch moves. Signed in only.

func newGitHooksCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "git-hooks",
		Short: "Deploy on push: the webhook a git host calls",
		Long: `A project that deploys from a git branch deploys every push to it. shpyrd
checks the branch every few minutes by itself; a webhook makes it seconds.

  shpyrd git-hooks show --project shop      the webhook's URL and secret, and how to add them
  shpyrd git-hooks rotate --project shop    a new URL and secret; the old ones are refused

A commit whose message says [skip deploy] is recorded, not deployed. To record
pushes without deploying them: shpyrd deploy --git <url> --no-auto-deploy.`,
	}
	var showApp, rotateApp string
	show := &cobra.Command{
		Use:   "show [--project <name>]",
		Short: "Print the webhook's URL and secret, and how to add them",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveAppName(showApp)
			if err != nil {
				return err
			}
			var auth api.ProjectGitAuthView
			if err := gitHooksRequest(g, cmd, "GET", "api/projects/"+url.PathEscape(name)+"/git-auth", &auth); err != nil {
				return err
			}
			var d api.AppDetail
			if err := gitHooksRequest(g, cmd, "GET", "api/projects/"+url.PathEscape(name), &d); err != nil {
				return err
			}
			if auth.Using == gitauth.KindGitHubApp {
				// The App's own webhook brings the pushes (RFC-0054): no
				// hook Secret is made for this project.
				return g.print(cmd, map[string]string{"through": "github-app", "account": auth.GitHubAccount}, func(w io.Writer) {
					fmt.Fprintf(w, "Pushes arrive through the GitHub App (%s); nothing to set up.\n", auth.GitHubAccount)
					printAutoDeploy(w, name, d)
				})
			}
			var v api.GitHooksView
			if err := gitHooksRequest(g, cmd, "GET", "api/projects/"+url.PathEscape(name)+"/git-hooks", &v); err != nil {
				return err
			}
			return g.print(cmd, v, func(w io.Writer) { printGitHooks(w, name, v, d) })
		},
	}
	appFlag(show, &showApp)
	rotate := &cobra.Command{
		Use:   "rotate [--project <name>]",
		Short: "Make a new webhook URL and secret; the old ones stop working",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveAppName(rotateApp)
			if err != nil {
				return err
			}
			var v api.GitHooksView
			if err := gitHooksRequest(g, cmd, "POST", "api/projects/"+url.PathEscape(name)+"/git-hooks/rotate", &v); err != nil {
				return err
			}
			return g.print(cmd, v, func(w io.Writer) {
				fmt.Fprintf(w, "New webhook for %s. Change it on the git host: the old URL and secret are refused from now on.\n\n  URL     %s\n  Secret  %s\n", name, v.URL, v.Secret)
			})
		},
	}
	appFlag(rotate, &rotateApp)
	cmd.AddCommand(show, rotate)
	return cmd
}

func gitHooksRequest(g *globalFlags, cmd *cobra.Command, method, path string, out any) error {
	return signedInRequest(g, cmd, "webhooks are managed signed in: run shpyrd login first", method, path, nil, out)
}

// printGitHooks says where the webhook goes on each host, and what a push
// does now.
func printGitHooks(w io.Writer, name string, v api.GitHooksView, d api.AppDetail) {
	fmt.Fprintf(w, "Deploy %s on push: add this webhook to its repository.\n\n  URL     %s\n  Secret  %s\n\n", name, v.URL, v.Secret)
	fmt.Fprintln(w, `GitHub: the repository's Settings, Webhooks, Add webhook. Payload URL: the URL; Content type: application/json; Secret: the secret; "Just the push event".`)
	fmt.Fprintln(w, "GitLab: the project's Settings, Webhooks, Add new webhook. URL: the URL; Secret token: the secret; Trigger: Push events.")
	fmt.Fprintln(w, "Gitea: the repository's Settings, Webhooks, Add webhook, Gitea. Target URL: the URL; Content type: application/json; Secret: the secret; Trigger on: Push events.")
	fmt.Fprintln(w)
	printAutoDeploy(w, name, d)
}

// printAutoDeploy says what a push to the project's branch does now.
func printAutoDeploy(w io.Writer, name string, d api.AppDetail) {
	var git *shpyrdv1.GitSource
	if d.Spec.Source != nil {
		git = d.Spec.Source.Git
	}
	switch {
	case git == nil:
		fmt.Fprintf(w, "%s does not deploy from a git repository yet: shpyrd deploy --git <url> --project %s\n", name, name)
	case shpyrdv1.IsCommit(git.Revision):
		fmt.Fprintf(w, "%s is pinned to commit %s: a push deploys nothing. Deploy a branch to follow it.\n", name, git.Revision)
	case git.AutoDeploy != nil && !*git.AutoDeploy:
		branch := firstNonEmpty(git.Revision, "main")
		fmt.Fprintf(w, "Auto-deploy is off: pushes to %s are recorded, not deployed. Switch it on in the dashboard, or: shpyrd deploy --git %s --ref %s --auto-deploy --project %s\n", branch, git.URL, branch, name)
	default:
		fmt.Fprintf(w, "Auto-deploy is on: every push to %s deploys. Without the webhook, shpyrd checks the branch every few minutes.\n", firstNonEmpty(git.Revision, "main"))
	}
}
