package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ext/resources"
	"github.com/shpyrd-io/shpyrd/pkg/kexec"
)

func newPgCmd(g ext.CLIGlobals) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pg",
		Aliases: []string{"postgres"},
		Short:   "PostgreSQL databases of a project (extension postgres)",
		Long: `Create a PostgreSQL database in a project and attach it to the app:

  shpyrd pg create db --project shop --size shared-m --storage 10Gi
  shpyrd attach db --project shop        # DATABASE_URL, DATABASE_HOST, ... in the app
  shpyrd pg psql db --project shop       # a psql session on the primary

Databases run on CloudNativePG; one cluster per database, 1 instance by
default (2-3 for high availability with --instances).`,
	}
	cmd.AddCommand(newCreateCmd(g), newListCmd(g), newInfoCmd(g), newPsqlCmd(g), newDeleteCmd(g), newBackupsCmd(g), newBackupCmd(g), newRestoreCmd(g), newPgSleepCmd(g), newPgSuspendCmd(g), newPgResumeCmd(g))
	return cmd
}

// Database sleep (RFC-0075 section 5) is not complete yet: bindings still
// point apps at "<name>-rw", no activity signal keeps a busy database
// awake, and the wake proxy image is not shipped. The commands exist so the
// shape is settled, but they are hidden and refuse until then.
const pgSleepNotYet = "database sleep is not available in this release yet (RFC-0075 section 5 is in progress); HTTP sleep for apps is: shpyrd sleep <project> --after 15m"

func pgSleepPlaceholder(use, short string) *cobra.Command {
	return &cobra.Command{
		Use: use, Short: short, Hidden: true,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New(pgSleepNotYet)
		},
	}
}

// newPgSleepCmd configures automatic hibernation for a database (RFC-0075).
func newPgSleepCmd(ext.CLIGlobals) *cobra.Command {
	cmd := pgSleepPlaceholder("sleep <name>", "Configure automatic hibernation for a database")
	cmd.Flags().String("after", "", "idle window before hibernating, e.g. 30m; 'off' disables")
	cmd.Flags().String("project", "", "project slug")
	return cmd
}

// newPgSuspendCmd suspends a database (explicit, no auto-wake).
func newPgSuspendCmd(ext.CLIGlobals) *cobra.Command {
	cmd := pgSleepPlaceholder("suspend <name>", "Suspend a database (no automatic wake; data kept)")
	cmd.Flags().String("project", "", "project slug")
	return cmd
}

// newPgResumeCmd resumes a suspended database.
func newPgResumeCmd(ext.CLIGlobals) *cobra.Command {
	cmd := pgSleepPlaceholder("resume <name>", "Resume a suspended database")
	cmd.Flags().String("project", "", "project slug")
	return cmd
}
func cliContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		cancel()
	}()
	return ctx
}

func projectFlag(cmd *cobra.Command, dst *string) {
	cmd.Flags().StringVar(dst, "project", "", "project name (required)")
	_ = cmd.MarkFlagRequired("project")
}

func newCreateCmd(g ext.CLIGlobals) *cobra.Command {
	var (
		project   string
		version   string
		size      string
		storage   string
		instances int32
		backups   bool
		retention string
		schedule  string
	)
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a database",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cliContext()
			qty, err := resource.ParseQuantity(storage)
			if err != nil || qty.Sign() <= 0 {
				return fmt.Errorf("invalid --storage %q (use e.g. 10Gi)", storage)
			}
			// Through the API (RFC-0052): the same for a login session and a
			// kubeconfig; the server validates the spec against the CRD.
			spec := shpyrdv1.PostgresSpec{Version: version, Size: size, Storage: &qty, Instances: ptr.To(instances)}
			if backups || retention != "" || schedule != "" {
				spec.Backups = &shpyrdv1.PostgresBackups{Retention: retention, Schedule: schedule}
			}
			if _, err := resources.CreateAPI(ctx, g.API(), project, "Postgres", args[0], spec); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Creating PostgreSQL %s database %s (%s, %d instance(s))...\n", version, args[0], qty.String(), instances)
			v, err := resources.WaitReadyAPI(ctx, g.API(), cmd.OutOrStdout(), project, "Postgres", args[0], 5*time.Minute)
			if err != nil {
				return err
			}
			switch {
			case v == nil:
				fmt.Fprintln(cmd.OutOrStdout(), "Still provisioning; check with `shpyrd pg list`.")
			case v.Phase == shpyrdv1.ResourceFailed:
				if strings.Contains(v.Message, "extension is not installed") {
					return errors.New(resources.ExtensionHint(Name))
				}
				return errors.New(v.Message)
			default:
				fmt.Fprintf(cmd.OutOrStdout(), "Database %s is ready at %s. Attach it with `shpyrd attach %s --project %s`.\n", args[0], v.Endpoint, args[0], project)
			}
			return nil
		},
	}
	projectFlag(cmd, &project)
	cmd.Flags().StringVar(&version, "version", "17", "PostgreSQL major version")
	cmd.Flags().StringVar(&size, "size", "", "instance size from the catalog (default: the catalog default)")
	cmd.Flags().StringVar(&storage, "storage", "5Gi", "data volume size")
	cmd.Flags().Int32Var(&instances, "instances", 1, "number of instances (2-3 for high availability)")
	cmd.Flags().BoolVar(&backups, "backups", false, "back up to the platform's object store: continuous WAL archiving and a daily base backup (needs the object-storage extension)")
	cmd.Flags().StringVar(&retention, "retention", "", "how long backups are kept, e.g. 14d (default 14d; implies --backups)")
	cmd.Flags().StringVar(&schedule, "backup-schedule", "", "cron of the base backup in UTC, e.g. \"0 2 * * *\" (default daily at 02:00; implies --backups)")
	return cmd
}

func newListCmd(g ext.CLIGlobals) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the databases of a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cliContext()
			list, err := resources.ListAPI(ctx, g.API(), project, "Postgres")
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No databases in project %s. Create one with `shpyrd pg create db --project %s`.\n", project, project)
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tVERSION\tSIZE\tSTORAGE\tINSTANCES\tSTATUS\tATTACHED TO")
			for _, v := range list {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", v.Name, firstNonEmpty(v.Details["version"], "17"), firstNonEmpty(v.Details["size"], "default"), firstNonEmpty(v.Details["storage"], "5Gi"), firstNonEmpty(v.Details["instances"], "1"), firstNonEmpty(v.Phase, "Pending"), firstNonEmpty(strings.Join(v.AttachedTo, ", "), "-"))
			}
			return tw.Flush()
		},
	}
	projectFlag(cmd, &project)
	return cmd
}

func newInfoCmd(g ext.CLIGlobals) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "info <name>",
		Short: "Show a database",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cliContext()
			v, err := resources.GetAPI(ctx, g.API(), project, "Postgres", args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Database:   %s (project %s)\n", v.Name, project)
			fmt.Fprintf(out, "Status:     %s%s\n", firstNonEmpty(v.Phase, "Pending"), suffix(v.Message))
			fmt.Fprintf(out, "Endpoint:   %s\n", firstNonEmpty(v.Endpoint, "-"))
			fmt.Fprintf(out, "Version:    PostgreSQL %s\n", firstNonEmpty(v.Details["version"], "17"))
			fmt.Fprintf(out, "Storage:    %s, %s instance(s)\n", firstNonEmpty(v.Details["storage"], "5Gi"), firstNonEmpty(v.Details["instances"], "1"))
			fmt.Fprintf(out, "Attached:   %s\n", firstNonEmpty(strings.Join(v.AttachedTo, ", "), "- (shpyrd attach "+v.Name+" --project "+project+")"))
			backups := "off (create with --backups, or restore from another database's backup)"
			if b := v.Details["backups"]; b != "" {
				backups = b
				if last := v.Details["lastBackup"]; last != "" {
					backups += "; last " + last
				}
				if from := v.Details["recoverableFrom"]; from != "" {
					backups += "; recoverable from " + from
				}
			}
			fmt.Fprintf(out, "Backups:    %s\n", backups)
			if from := v.Details["restoredFrom"]; from != "" {
				fmt.Fprintf(out, "Restored:   from %s\n", from)
			}
			fmt.Fprintf(out, "Config vars: DATABASE_URL, DATABASE_HOST, DATABASE_PORT, DATABASE_USER, DATABASE_PASSWORD, DATABASE_NAME (values are never shown)\n")
			return nil
		},
	}
	projectFlag(cmd, &project)
	return cmd
}

func newPsqlCmd(g ext.CLIGlobals) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "psql <name> [-- psql args...]",
		Short: "Open psql on the database's primary instance",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cliContext()
			k, c, err := resources.Connect(g)
			if err != nil {
				return err
			}
			pg, err := get(ctx, c, project, args[0])
			if err != nil {
				return err
			}
			var pods corev1.PodList
			if err := c.List(ctx, &pods, client.InNamespace(pg.Namespace), client.MatchingLabels{"cnpg.io/cluster": pg.Name, "cnpg.io/instanceRole": "primary"}); err != nil {
				return err
			}
			if len(pods.Items) == 0 {
				return fmt.Errorf("database %s has no primary instance yet (%s)", pg.Name, firstNonEmpty(pg.Status.Message, pg.Status.Phase))
			}
			command := append([]string{"psql", "-d", "app"}, args[1:]...)
			fmt.Fprintf(cmd.ErrOrStderr(), "Connecting to %s (primary)...\n", pg.Name)
			return kexec.RemoteExit(kexec.Exec(ctx, k, pg.Namespace, pods.Items[0].Name, "postgres", command, kexec.StdinIsTerminal()))
		},
	}
	projectFlag(cmd, &project)
	return cmd
}

func newDeleteCmd(g ext.CLIGlobals) *cobra.Command {
	var (
		project string
		yes     bool
		force   bool
	)
	cmd := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"rm", "destroy"},
		Short:   "Delete a database and its data",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cliContext()
			v, err := resources.GetAPI(ctx, g.API(), project, "Postgres", args[0])
			if err != nil {
				return err
			}
			if len(v.AttachedTo) > 0 && !force {
				return fmt.Errorf("database %q is attached to %s: detach it first (shpyrd detach %s --project %s) or pass --force", v.Name, strings.Join(v.AttachedTo, ", "), v.Name, project)
			}
			if !yes {
				return fmt.Errorf("this deletes database %q and all its data; re-run with --yes to confirm", v.Name)
			}
			if err := resources.DeleteAPI(ctx, g.API(), project, "Postgres", v.Name, force); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted database %s from project %s\n", v.Name, project)
			return nil
		},
	}
	projectFlag(cmd, &project)
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm")
	cmd.Flags().BoolVar(&force, "force", false, "delete even while attached to an app")
	return cmd
}

func get(ctx context.Context, c client.Client, project, name string) (*shpyrdv1.Postgres, error) {
	pg := &shpyrdv1.Postgres{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: resources.Namespace(project), Name: name}, pg); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("database %q not found in project %s (see `shpyrd pg list --project %s`)", name, project, project)
		}
		return nil, err
	}
	return pg, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func suffix(msg string) string {
	if msg == "" {
		return ""
	}
	return " (" + msg + ")"
}

// waitReady follows a Postgres until it is Ready or Failed, through the
// cluster (restore still runs that way).
func waitReady(ctx context.Context, cmd *cobra.Command, c client.Client, pg *shpyrdv1.Postgres, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	last := ""
	for time.Now().Before(deadline) {
		cur := &shpyrdv1.Postgres{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(pg), cur); err != nil {
			return err
		}
		if msg := cur.Status.Phase + " " + cur.Status.Message; msg != last && cur.Status.Phase != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", strings.TrimSpace(msg))
			last = msg
		}
		switch cur.Status.Phase {
		case shpyrdv1.ResourceReady:
			fmt.Fprintf(cmd.OutOrStdout(), "Database %s is ready at %s. Attach it with `shpyrd attach %s --project %s`.\n", pg.Name, cur.Status.Endpoint, pg.Name, strings.TrimPrefix(pg.Namespace, "app-"))
			return nil
		case shpyrdv1.ResourceFailed:
			if strings.Contains(cur.Status.Message, "extension is not installed") {
				return errors.New(resources.ExtensionHint(Name))
			}
			return errors.New(cur.Status.Message)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Still provisioning; check with `shpyrd pg list`.")
	return nil
}
