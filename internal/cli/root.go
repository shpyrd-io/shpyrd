// Package cli implements the shpyrd command tree.
package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ext/all"
	"github.com/shpyrd-io/shpyrd/pkg/version"
)

// Version of the CLI (see pkg/version).
var Version = version.Version

type globalFlags struct {
	kubeconfig string
	kubeCtx    string
	verbose    bool
}

// API implements ext.CLIGlobals: the workspace API over the login session
// or the kubeconfig proxy, decided the same way every core command does.
func (g *globalFlags) API() ext.APIClient { return &apiTransport{g: g} }

type apiTransport struct {
	g  *globalFlags
	ac *appClient
}

func (t *apiTransport) client() (*appClient, error) {
	if t.ac == nil {
		ac, err := newAppClient(t.g, io.Discard)
		if err != nil {
			return nil, err
		}
		t.ac = ac
	}
	return t.ac, nil
}

func (t *apiTransport) Request(ctx context.Context, method, path string, body []byte, contentType string) ([]byte, error) {
	ac, err := t.client()
	if err != nil {
		return nil, err
	}
	return ac.serverRequest(ctx, method, path, body, contentType)
}

func (t *apiTransport) Session() bool {
	ac, err := t.client()
	return err == nil && ac.session
}

// New builds the root command.
func New() *cobra.Command {
	g := &globalFlags{}
	root := &cobra.Command{
		Use:           "shpyrd",
		Short:         "Opensource Cloud PaaS",
		Long:          "shpyrd manages applications and agents from one place, from deploy to monitoring: cluster bootstrap, buildpack builds, releases, config vars, logs and metrics on Kubernetes.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			level := slog.LevelInfo
			if g.verbose {
				level = slog.LevelDebug
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
			// An explicit --context or --kubeconfig names the cluster to
			// talk to: it wins over a saved login session.
			preferKubeconfig = cmd.Flags().Changed("context") || cmd.Flags().Changed("kubeconfig")
		},
	}
	root.PersistentFlags().StringVar(&g.kubeconfig, "kubeconfig", os.Getenv("KUBECONFIG"), "path to the kubeconfig file")
	root.PersistentFlags().StringVar(&g.kubeCtx, "context", "", "kubeconfig context to use")
	root.PersistentFlags().BoolVarP(&g.verbose, "verbose", "v", false, "verbose output")
	root.CompletionOptions.HiddenDefaultCmd = true

	root.AddCommand(newClusterCmd(g))
	root.AddCommand(newAppsCmd(g))
	root.AddCommand(newDeployCmd(g))
	root.AddCommand(newSecretsCmd(g))
	root.AddCommand(newGlobalsCmd(g))
	root.AddCommand(newDrainsCmd(g))
	root.AddCommand(newScaleCmd(g))
	root.AddCommand(newResizeCmd(g))
	root.AddCommand(newSizesCmd(g))
	root.AddCommand(newVolumesCmd(g))
	root.AddCommand(newAttachCmd(g))
	root.AddCommand(newDetachCmd(g))
	root.AddCommand(newExtensionsCmd(g))
	root.AddCommand(newTeamsCmd(g))
	root.AddCommand(newMembersCmd(g))
	root.AddCommand(newPeopleCmd(g))
	root.AddCommand(newInviteCmd(g))
	root.AddCommand(newInvitationsCmd(g))
	// Commands contributed by extensions (they explain themselves when the
	// extension is not enabled on the cluster): the developer's here (pg,
	// redis), the operator's in shpyrd-ctl (users, auth, object-storage).
	addExtensionCommands(root, g, ext.AudienceDeveloper)
	root.AddCommand(newLogsCmd(g))
	root.AddCommand(newShellCmd(g))
	root.AddCommand(newRunCmd(g))
	root.AddCommand(newReleasesCmd(g))
	root.AddCommand(newRollbackCmd(g))
	root.AddCommand(newExposureCmd(g))
	root.AddCommand(newAccessCmd(g))
	root.AddCommand(newAllowCmd(g))
	root.AddCommand(newDomainsCmd(g))
	root.AddCommand(newLoginCmd(g))
	root.AddCommand(newUseCmd(g))
	root.AddCommand(newTokensCmd(g))
	root.AddCommand(newLogoutCmd(g))
	root.AddCommand(newWhoAmICmd(g))
	root.AddCommand(newRedeployCmd(g))
	root.AddCommand(newOpenCmd(g))
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the CLI version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), Version)
		},
	})
	return root
}

func findCommand(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// addExtensionCommands mounts the extension commands of one audience.
// Extensions may share a top-level command (`shpyrd-ctl auth`): the later
// ones add their subcommands to the first.
func addExtensionCommands(root *cobra.Command, g *globalFlags, audience string) {
	for _, x := range all.All() {
		for _, c := range x.CLI(g) {
			if ext.Audience(c) != audience {
				continue
			}
			if existing := findCommand(root, c.Name()); existing != nil {
				existing.AddCommand(c.Commands()...)
				continue
			}
			root.AddCommand(c)
		}
	}
}
