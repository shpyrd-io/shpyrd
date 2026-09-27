package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/project"
)

func newAppsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "projects",
		Short:   "Create, list and inspect projects",
		Aliases: []string{"project", "apps", "app"},
	}
	cmd.AddCommand(newAppsCreateCmd(g), newAppsListCmd(g), newAppsInfoCmd(g), newAppsRenameCmd(g), newAppsDescribeCmd(g), newAppsDestroyCmd(g))
	return cmd
}

func newAppsCreateCmd(g *globalFlags) *cobra.Command {
	var (
		domains []string
		save    bool
		slug    string
		public  bool
	)
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a project",
		Long: `Create a project. The name is free text ("My Shop"); its slug (my-shop) is
derived from it and identifies the project in the CLI, in URLs and in the
hostname <slug>.<cluster domain>. Pass --slug to choose it.

New projects ask visitors to sign in: only people with a role on the project
(teams with the user role, its developers and admins) can open the app, and
the app receives who they are. --public makes it a site anyone can open;
` + "`shpyrd access`" + ` changes it later.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if slug == "" {
				var err error
				if slug, err = project.Slug(name); err != nil {
					return err
				}
			} else if err := project.ValidateSlug(slug); err != nil {
				return err
			}
			ctx := signalContext()
			ac, err := newAppClient(g, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			access := shpyrdv1.AccessAuthenticated
			if public {
				access = shpyrdv1.AccessPublic
			}
			out := cmd.OutOrStdout()
			var label string
			if ac.session {
				// Signed in through the API (shpyrd login): the server creates
				// the project in the workspace the session belongs to.
				body, _ := json.Marshal(api.CreateAppRequest{Name: name, Slug: slug, Domains: domains, Access: access})
				raw, err := ac.serverRequest(ctx, "POST", "api/projects", body, "application/json")
				if err != nil {
					return err
				}
				var created struct {
					Slug        string `json:"slug"`
					DisplayName string `json:"displayName"`
				}
				_ = json.Unmarshal(raw, &created)
				slug = firstNonEmpty(created.Slug, slug)
				label = firstNonEmpty(created.DisplayName, name)
				if label != slug {
					label += " (" + slug + ")"
				}
			} else {
				ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
					Name:   appNamespace(slug),
					Labels: project.NamespaceLabels(project.DefaultWorkspace, slug),
				}}
				if err := ac.c.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
					return fmt.Errorf("create namespace: %w", err)
				}
				app := &shpyrdv1.App{
					ObjectMeta: metav1.ObjectMeta{Name: slug, Namespace: ns.Name, Labels: project.NamespaceLabels(project.DefaultWorkspace, slug)},
					Spec:       shpyrdv1.AppSpec{Domains: domains, Access: access},
				}
				project.SetDisplayName(app, name)
				if err := ac.c.Create(ctx, app); err != nil {
					if apierrors.IsAlreadyExists(err) {
						return fmt.Errorf("project %q already exists; choose another slug, for example --slug %s-2", slug, slug)
					}
					return fmt.Errorf("create project: %w", err)
				}
				ac.audit(ctx, slug, "project.create", project.Label(app), "")
				label = project.Label(app)
			}
			fmt.Fprintf(out, "Created project %s\n", label)
			if public {
				fmt.Fprintln(out, "Anyone on the internet can open it (public). `shpyrd access set authenticated` closes it.")
			} else {
				fmt.Fprintln(out, "Visitors must sign in; grant a team the user role to let it in (`shpyrd members add`), or `shpyrd access set public` for a site.")
			}
			if save {
				// The file starts with the project and whatever the
				// directory's build profile implies (RFC-0067).
				_, det := applyProfiles(&projectConfig{Project: slug}, ".")
				det.report(out)
				path, created, err := saveInferences(".", slug, det, true)
				if err != nil {
					return err
				}
				if created {
					fmt.Fprintf(out, "Wrote %s\n", path)
				} else {
					fmt.Fprintf(out, "Added to %s\n", path)
				}
				fmt.Fprintln(out, "Next: shpyrd deploy")
			} else {
				fmt.Fprintf(out, "Next: shpyrd deploy --project %s   (or add `project: %s` to shpyrd.yaml)\n", slug, slug)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&slug, "slug", "", "identifier to use instead of the one derived from the name")
	cmd.Flags().StringSliceVar(&domains, "domain", nil, "custom domains served in addition to <slug>.<cluster domain> (see `shpyrd domains`)")
	cmd.Flags().BoolVar(&save, "save", false, "write shpyrd.yaml in the current directory")
	cmd.Flags().BoolVar(&public, "public", false, "anyone can open the app (a site); the default asks visitors to sign in")
	return cmd
}

func newAppsRenameCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <project> <new name>",
		Short: "Change the display name of a project (the slug never changes)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateAppName(args[0]); err != nil {
				return err
			}
			name := strings.TrimSpace(args[1])
			if name == "" {
				return errors.New("the new name must not be empty")
			}
			ctx := signalContext()
			ac, err := newAppClient(g, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			app, err := ac.getApp(ctx, args[0])
			if err != nil {
				return err
			}
			project.SetDisplayName(app, name)
			if ac.session {
				body, _ := json.Marshal(map[string]string{"name": name})
				if _, err := ac.serverRequest(ctx, "PATCH", "api/projects/"+app.Name, body, "application/json"); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Renamed project %s\n", project.Label(app))
				return nil
			}
			if err := ac.c.Update(ctx, app); err != nil {
				return err
			}
			ac.audit(ctx, app.Name, "project.rename", project.Label(app), "")
			fmt.Fprintf(cmd.OutOrStdout(), "Renamed project %s\n", project.Label(app))
			return nil
		},
	}
}

// newAppsDescribeCmd sets what the launcher shows for a project (RFC-0033):
// the one-line description and whether it is featured.
func newAppsDescribeCmd(g *globalFlags) *cobra.Command {
	var description string
	var featured, unfeatured bool
	cmd := &cobra.Command{
		Use:   "describe <project> [--description text] [--featured|--unfeatured]",
		Short: "Set the launcher's description of a project, and whether it is featured",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateAppName(args[0]); err != nil {
				return err
			}
			if !cmd.Flags().Changed("description") && !featured && !unfeatured {
				return errors.New("give --description, --featured or --unfeatured")
			}
			if featured && unfeatured {
				return errors.New("--featured and --unfeatured exclude each other")
			}
			body := map[string]any{}
			if cmd.Flags().Changed("description") {
				body["description"] = strings.TrimSpace(description)
			}
			if featured || unfeatured {
				body["featured"] = featured
			}
			ctx := signalContext()
			ac, err := newAppClient(g, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			raw, _ := json.Marshal(body)
			if _, err := ac.serverRequest(ctx, "PATCH", "api/projects/"+args[0], raw, "application/json"); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Updated what the launcher shows for %s.\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&description, "description", "", "one line under the name in the launcher (empty removes it)")
	cmd.Flags().BoolVar(&featured, "featured", false, "show the app first, and larger, in the launcher")
	cmd.Flags().BoolVar(&unfeatured, "unfeatured", false, "stop featuring the app")
	return cmd
}

func newAppsListCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List projects",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			ac, err := newAppClient(g, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			raw, err := serverRequest(ctx, ac.k, "GET", "api/projects", nil, "")
			if err != nil {
				return err
			}
			var items []api.AppSummary
			if err2 := json.Unmarshal(raw, &items); err2 != nil {
				return fmt.Errorf("unexpected response: %s", truncate(string(raw), 200))
			}
			sort.Slice(items, func(i, j int) bool { return items[i].Slug < items[j].Slug })
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "PROJECT\tNAME\tPHASE\tRELEASE\tURL\tAGE")
			for _, a := range items {
				rel := "-"
				if a.Release > 0 {
					rel = fmt.Sprintf("v%d", a.Release)
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", a.Slug, a.DisplayName, firstNonEmpty(a.Phase, "Pending"), rel, a.URL, a.CreatedAt.Local().Format("Jan 2"))
			}
			return tw.Flush()
		},
	}
}

func newAppsInfoCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "info <project>",
		Short: "Show a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			ac, err := newAppClient(g, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			app, err := ac.getApp(ctx, args[0])
			if err != nil {
				return err
			}
			printAppInfo(cmd, app)
			var vols []shpyrdv1.Volume
			if ac.session {
				vols, _ = ac.listVolumesAPI(ctx, app.Name)
			} else {
				var list shpyrdv1.VolumeList
				_ = ac.c.List(ctx, &list, client.InNamespace(app.Namespace))
				vols = list.Items
			}
			printResources(cmd, app, vols)
			return nil
		},
	}
}

// printResources lists every resource of the project (RFC-0003): the app
// itself, its attached resources and the volumes.
func printResources(cmd *cobra.Command, app *shpyrdv1.App, vols []shpyrdv1.Volume) {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Resources:")
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", "App", app.Name, firstNonEmpty(app.Status.Phase, "Pending"), firstNonEmpty(app.Status.URL, "-"))
	for _, b := range app.Spec.Bindings {
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", b.Kind, b.Name, "attached", "config vars "+strings.ToUpper(firstNonEmpty(b.Prefix, defaultPrefix(b.Kind)))+"_*")
	}
	for _, v := range vols {
		mode := "single-instance"
		if v.Shared() {
			mode = "shared"
		}
		detail := fmt.Sprintf("%s %s", v.Spec.Size.String(), mode)
		if len(v.Status.MountedBy) > 0 {
			detail += ", mounted by " + strings.Join(v.Status.MountedBy, ", ")
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", "Volume", v.Name, firstNonEmpty(v.Status.Phase, "Pending"), detail)
	}
	_ = tw.Flush()
}

func printAppInfo(cmd *cobra.Command, app *shpyrdv1.App) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Project:    %s\n", project.Label(app))
	fmt.Fprintf(out, "Phase:      %s\n", firstNonEmpty(app.Status.Phase, "Pending"))
	if app.Status.Message != "" {
		fmt.Fprintf(out, "Message:    %s\n", app.Status.Message)
	}
	if app.Status.URL != "" {
		fmt.Fprintf(out, "URL:        %s\n", app.Status.URL)
	}
	switch {
	case app.Spec.Image != "":
		fmt.Fprintf(out, "Digest:     %s (pinned)\n", digest(app.Spec.Image))
	case app.Status.Image != "":
		fmt.Fprintf(out, "Digest:     %s\n", digest(app.Status.Image))
	}
	if app.Spec.Source != nil {
		switch {
		case app.Spec.Source.Git != nil:
			fmt.Fprintf(out, "Source:     git %s @ %s\n", app.Spec.Source.Git.URL, firstNonEmpty(app.Spec.Source.Git.Revision, "main"))
		case app.Spec.Source.Blob != nil:
			fmt.Fprintf(out, "Source:     archive %s (%s)\n", short(app.Spec.Source.Blob.SHA256), firstNonEmpty(app.Spec.Source.Blob.Ref, "local"))
		}
		if app.Spec.Source.SubPath != "" {
			fmt.Fprintf(out, "Path:       %s\n", app.Spec.Source.SubPath)
		}
	}
	if len(app.Status.ProcessTypes) > 0 {
		types := strings.Join(app.Status.ProcessTypes, ", ")
		for _, t := range app.Status.ProcessTypes {
			if t == "release" {
				types += "   (release runs before every release)"
			}
		}
		fmt.Fprintf(out, "Types:      %s\n", types)
	}
	if rel := app.Status.Release; rel != nil && rel.State != shpyrdv1.ReleaseSucceeded {
		fmt.Fprintf(out, "Release:    %s: %s\n", strings.ToLower(rel.State), rel.Message)
	}
	// Health check summary per process (RFC-0019).
	if len(app.Spec.Processes) > 0 {
		pnames := make([]string, 0, len(app.Spec.Processes))
		for n := range app.Spec.Processes {
			pnames = append(pnames, n)
		}
		sort.Strings(pnames)
		for _, n := range pnames {
			p := app.Spec.Processes[n]
			probe := healthLabel(n, p)
			if probe != "" {
				fmt.Fprintf(out, "Health:     %s: %s\n", n, probe)
			}
		}
	}
	if len(app.Status.Processes) > 0 {
		names := make([]string, 0, len(app.Status.Processes))
		for n := range app.Status.Processes {
			names = append(names, n)
		}
		sort.Strings(names)
		var parts []string
		for _, n := range names {
			p := app.Status.Processes[n]
			part := fmt.Sprintf("%s %d/%d", n, p.Ready, p.Desired)
			if p.Failing > 0 {
				part += fmt.Sprintf(" (%d failing: %s)", p.Failing, p.Reason)
			}
			parts = append(parts, part)
		}
		fmt.Fprintf(out, "Processes:  %s\n", strings.Join(parts, ", "))
	}
	if n := len(app.Status.Releases); n > 0 {
		fmt.Fprintln(out, "Releases:")
		start := n - 5
		if start < 0 {
			start = 0
		}
		for _, r := range app.Status.Releases[start:] {
			fmt.Fprintf(out, "  v%-3d %-9s %s\n", r.Number, age(r.CreatedAt)+" ago", r.Description)
		}
	}
}

func newAppsDestroyCmd(g *globalFlags) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "destroy <project>",
		Short: "Delete a project and everything in it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			ctx := signalContext()
			ac, err := newAppClient(g, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			app, err := ac.getApp(ctx, name)
			if err != nil {
				return err
			}
			var vols shpyrdv1.VolumeList
			if ac.session {
				items, _ := ac.listVolumesAPI(ctx, name)
				vols.Items = items
			} else {
				_ = ac.c.List(ctx, &vols, client.InNamespace(appNamespace(name)))
			}
			if len(vols.Items) > 0 {
				var names []string
				for _, v := range vols.Items {
					names = append(names, fmt.Sprintf("%s (%s)", v.Name, v.Spec.Size.String()))
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Warning: this deletes the data on volume(s) %s.\n", strings.Join(names, ", "))
			}
			if !confirm(cmd, yes, fmt.Sprintf("Delete project %s with all its resources?", project.Label(app)), false) {
				return fmt.Errorf("aborted")
			}
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: appNamespace(name)}}
			if ac.session {
				if _, err := ac.serverRequest(ctx, "DELETE", "api/projects/"+name, nil, ""); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Deleting project %s...\n", name)
				return nil
			}
			if err := ac.c.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			ac.auditCluster(ctx, "project.destroy", project.Label(app), "")
			fmt.Fprintf(cmd.OutOrStdout(), "Deleting project %s...\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
