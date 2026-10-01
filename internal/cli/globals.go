package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/configvars"
	"github.com/shpyrd-io/shpyrd/pkg/install"
)

// Global config vars (RFC-0016): `shpyrd globals set|unset|list`, a
// workspace admin's counterpart of `shpyrd secrets`. Signed in with
// `shpyrd login` they go through the workspace API (RFC-0052); over a
// kubeconfig they edit the Secret, and --workspace names which one.

func newGlobalsCmd(g *globalFlags) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "globals",
		Short: "Manage config vars every project of a workspace receives",
		Long: `Global config vars are set once by a workspace's admins and injected into
every process of every project of the workspace, first in the environment so a
project's own config var of the same name wins and attached resources win over
both. Changing them creates a "Global config change" release in every project
of the workspace that has not opted out (shpyrd.yaml: globals: false, or
globals: {exclude: [NAME]}). Values are write-only: they are never printed
back. Signed in with shpyrd login, the workspace is the one you are signed in
to; over a kubeconfig it is the default one unless --workspace names another.

  shpyrd globals set OPENAI_API_KEY=sk-... REGION=eu
  shpyrd globals set --workspace acme SENTRY_DSN=https://...
  shpyrd globals unset REGION
  shpyrd globals list`,
	}
	cmd.PersistentFlags().StringVar(&workspace, "workspace", "", "workspace slug (the default workspace when empty)")
	cmd.AddCommand(&cobra.Command{
		Use:   "set KEY=VALUE [KEY=VALUE...]",
		Short: "Set global config vars",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			set := map[string]string{}
			for _, kv := range args {
				k, v, ok := strings.Cut(kv, "=")
				if !ok || k == "" {
					return fmt.Errorf("expected KEY=VALUE, got %q", kv)
				}
				set[k] = v
			}
			return mutateGlobals(g, cmd, workspace, set, nil)
		},
	}, &cobra.Command{
		Use:   "unset KEY [KEY...]",
		Short: "Remove global config vars",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mutateGlobals(g, cmd, workspace, nil, args)
		},
	}, &cobra.Command{
		Use:     "list",
		Short:   "List global config var names (never values)",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			var vars []configvars.Var
			if ac.session {
				if workspace != "" {
					return errors.New("--workspace names a workspace over a kubeconfig; signed in, the globals are the current workspace's")
				}
				raw, err := ac.serverRequest(ctx, "GET", "api/workspace/globals", nil, "")
				if err != nil {
					return err
				}
				var res api.GlobalsResponse
				if err := json.Unmarshal(raw, &res); err != nil {
					return fmt.Errorf("unexpected response: %s", truncate(string(raw), 200))
				}
				vars = res.Vars
			} else {
				sec := &corev1.Secret{}
				if err := ac.c.Get(ctx, globalsKey(workspace), sec); err != nil {
					if apierrors.IsNotFound(err) {
						fmt.Fprintln(cmd.OutOrStdout(), "no global config vars set")
						return nil
					}
					return err
				}
				vars = configvars.List(sec)
			}
			if len(vars) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no global config vars set")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tUPDATED")
			for _, v := range vars {
				when := "-"
				if t, err := time.Parse(time.RFC3339, v.UpdatedAt); err == nil {
					when = age(metav1.NewTime(t))
				}
				fmt.Fprintf(tw, "%s\t%s\n", v.Name, when)
			}
			return tw.Flush()
		},
	})
	return cmd
}

func globalsKey(workspace string) types.NamespacedName {
	return types.NamespacedName{Namespace: install.DefaultSystemNamespace, Name: shpyrdv1.GlobalEnvSecretFor(workspace)}
}

// workspaceOfApp is the slug of the workspace an App belongs to: its label,
// or the default workspace.
func workspaceOfApp(a *shpyrdv1.App) string {
	if ws := a.Labels[shpyrdv1.LabelWorkspace]; ws != "" {
		return ws
	}
	return "default"
}

func mutateGlobals(g *globalFlags, cmd *cobra.Command, workspace string, set map[string]string, unset []string) error {
	ctx := signalContext()
	ac, err := newAppClient(g, g.progress(cmd))
	if err != nil {
		return err
	}
	if ac.session {
		// Over the API (RFC-0052): the server applies the change and
		// releases it to the workspace's projects.
		if workspace != "" {
			return errors.New("--workspace names a workspace over a kubeconfig; signed in, the globals are the current workspace's")
		}
		body, _ := json.Marshal(api.ConfigVarsUpdate{Set: set, Unset: unset})
		raw, err := ac.serverRequest(ctx, "PUT", "api/workspace/globals", body, "application/json")
		if err != nil {
			return err
		}
		var res api.GlobalsResponse
		_ = json.Unmarshal(raw, &res)
		names := make([]string, 0, len(res.Vars))
		for _, v := range res.Vars {
			names = append(names, v.Name)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Global config vars: %s\n", firstNonEmpty(strings.Join(names, ", "), "(none)"))
		if res.Projects > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "Releasing the change to %d project(s)...\n", res.Projects)
		}
		return nil
	}
	sec := &corev1.Secret{}
	create := false
	key := globalsKey(workspace)
	if err := ac.c.Get(ctx, key, sec); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		create = true
		sec = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace, Labels: map[string]string{shpyrdv1.LabelManagedBy: "shpyrd"}},
			Type:       corev1.SecretTypeOpaque,
		}
	}
	if err := configvars.Apply(sec, set, unset, time.Now()); err != nil {
		return err
	}
	if create {
		err = ac.c.Create(ctx, sec)
	} else {
		err = ac.c.Update(ctx, sec)
	}
	if err != nil {
		return err
	}
	if len(set) > 0 {
		ac.auditCluster(ctx, "globals.set", "global config vars", configDetail(set, nil))
	}
	if len(unset) > 0 {
		ac.auditCluster(ctx, "globals.unset", "global config vars", configDetail(nil, unset))
	}
	out := cmd.OutOrStdout()
	names := make([]string, 0, len(configvars.List(sec)))
	for _, v := range configvars.List(sec) {
		names = append(names, v.Name)
	}
	fmt.Fprintf(out, "Global config vars: %s\n", firstNonEmpty(strings.Join(names, ", "), "(none)"))
	var apps shpyrdv1.AppList
	if err := ac.c.List(ctx, &apps); err == nil {
		n := 0
		want := workspace
		if want == "" {
			want = "default"
		}
		for _, a := range apps.Items {
			if workspaceOfApp(&a) == want && (a.Spec.Globals == nil || !a.Spec.Globals.Disabled) {
				n++
			}
		}
		if n > 0 {
			fmt.Fprintf(out, "Releasing the change to %d project(s)...\n", n)
		}
	}
	return nil
}
