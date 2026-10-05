package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ext/resources"
)

// Backups (RFC-0038): `shpyrd pg backups enable|disable|list`, `shpyrd pg
// backup` for one now, `shpyrd pg restore` into a new database. All go
// through the API (#74): signed in with `shpyrd login` they need no
// kubeconfig, and with a kubeconfig they reach the same routes through
// its proxy.

// backup is one base backup as the API lists it (api.BackupView).
type backup struct {
	Name    string `json:"name"`
	Started string `json:"started,omitempty"`
	Phase   string `json:"phase"`
	Kind    string `json:"kind"`
	Error   string `json:"error,omitempty"`
}

// backups is a database's backups as the API gives them (api.BackupsView).
type backups struct {
	Name            string                    `json:"name"`
	Policy          *shpyrdv1.PostgresBackups `json:"policy"`
	LastBackup      *time.Time                `json:"lastBackup,omitempty"`
	RecoverableFrom *time.Time                `json:"recoverableFrom,omitempty"`
	Backups         []backup                  `json:"backups"`
}

func backupsPath(project, name string) string {
	return "api/projects/" + url.PathEscape(project) + "/resources/postgres/" + url.PathEscape(name)
}

// backupsCall sends one request about a database's backups and decodes
// the answer into out (when not nil).
func backupsCall(ctx context.Context, g ext.CLIGlobals, method, path string, body any, out any) error {
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		raw = b
	}
	resp, err := g.API().Request(ctx, method, path, raw, "application/json")
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp, out); err != nil {
		return fmt.Errorf("unexpected response: %s", resp)
	}
	return nil
}

func backupsLine(b backups) string {
	if b.Policy == nil {
		return "off (shpyrd pg backups enable " + b.Name + ")"
	}
	schedule, retention := "daily at 02:00 UTC", "14d"
	if b.Policy.Schedule != "" {
		schedule = "cron " + b.Policy.Schedule
	}
	if b.Policy.Retention != "" {
		retention = b.Policy.Retention
	}
	out := fmt.Sprintf("on, %s, kept %s", schedule, retention)
	if b.LastBackup != nil {
		out += ", last " + b.LastBackup.UTC().Format(time.RFC3339)
	} else {
		out += ", no backup completed yet"
	}
	if b.RecoverableFrom != nil {
		out += ", recoverable from " + b.RecoverableFrom.UTC().Format(time.RFC3339)
	}
	return out
}

func newBackupsCmd(g ext.CLIGlobals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backups",
		Short: "Backups of a database: turn them on or off, list them",
		Long: `Backups go to the platform's object store (extension object-storage):
continuous WAL archiving plus a base backup on a schedule, kept for the
retention period. With them, "shpyrd pg restore" recovers any point in time
inside that window into a new database.`,
	}
	var (
		project   string
		retention string
		schedule  string
	)
	enable := &cobra.Command{
		Use:   "enable <name>",
		Short: "Turn backups on (or change retention and schedule)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var b backups
			body := shpyrdv1.PostgresBackups{Retention: retention, Schedule: schedule}
			if err := backupsCall(cliContext(), g, "PUT", backupsPath(project, args[0])+"/backups", body, &b); err != nil {
				return err
			}
			return ext.Print(g, cmd, map[string]any{"project": project, "name": b.Name, "backups": b.Policy}, func(w io.Writer) {
				fmt.Fprintf(w, "Backups of %s: %s\n", b.Name, backupsLine(b))
				fmt.Fprintln(w, "The first base backup starts now; follow it with `shpyrd pg backups list "+b.Name+"`.")
			})
		},
	}
	projectFlag(enable, &project)
	enable.Flags().StringVar(&retention, "retention", "", "how long backups are kept, e.g. 14d")
	enable.Flags().StringVar(&schedule, "schedule", "", "cron of the base backup in UTC, e.g. \"0 2 * * *\"")

	var yes bool
	disable := &cobra.Command{
		Use:   "disable <name>",
		Short: "Turn backups off (existing backups stay until the database is deleted)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("this stops WAL archiving and scheduled backups of %q; re-run with --yes to confirm", args[0])
			}
			var b backups
			if err := backupsCall(cliContext(), g, "DELETE", backupsPath(project, args[0])+"/backups", nil, &b); err != nil {
				return err
			}
			return ext.Print(g, cmd, map[string]any{"project": project, "name": b.Name, "backups": nil}, func(w io.Writer) {
				fmt.Fprintf(w, "Backups of %s are off. Existing backups remain restorable until the database is deleted.\n", b.Name)
			})
		},
	}
	projectFlag(disable, &project)
	disable.Flags().BoolVar(&yes, "yes", false, "confirm")

	list := &cobra.Command{
		Use:     "list <name>",
		Aliases: []string{"ls"},
		Short:   "List the base backups of a database",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var b backups
			if err := backupsCall(cliContext(), g, "GET", backupsPath(project, args[0])+"/backups", nil, &b); err != nil {
				return err
			}
			if b.Backups == nil {
				b.Backups = []backup{}
			}
			return ext.Print(g, cmd, map[string]any{"project": project, "name": b.Name, "policy": b.Policy, "backups": b.Backups}, func(out io.Writer) {
				fmt.Fprintf(out, "Backups:    %s\n\n", backupsLine(b))
				if len(b.Backups) == 0 {
					fmt.Fprintln(out, "No base backups yet.")
					return
				}
				tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "NAME\tSTARTED\tSTATUS\tKIND")
				for _, x := range b.Backups {
					phase := x.Phase
					if x.Error != "" {
						phase += ": " + x.Error
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", x.Name, firstNonEmpty(x.Started, "-"), phase, x.Kind)
				}
				_ = tw.Flush()
			})
		},
	}
	projectFlag(list, &project)
	cmd.AddCommand(enable, disable, list)
	return cmd
}

// backupPoll is how often `shpyrd pg backup` asks how its backup goes.
var backupPoll = 3 * time.Second

func newBackupCmd(g ext.CLIGlobals) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "backup <name>",
		Short: "Take a base backup now",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cliContext()
			path := backupsPath(project, args[0]) + "/backups"
			var started backup
			if err := backupsCall(ctx, g, "POST", path, nil, &started); err != nil {
				return err
			}
			fmt.Fprintf(ext.Progress(g, cmd), "Backup %s started...\n", started.Name)
			for deadline := time.Now().Add(10 * time.Minute); time.Now().Before(deadline); {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(backupPoll):
				}
				var b backups
				if err := backupsCall(ctx, g, "GET", path, nil, &b); err != nil {
					return err
				}
				for _, x := range b.Backups {
					if x.Name != started.Name {
						continue
					}
					switch x.Phase {
					case "completed":
						return ext.Print(g, cmd, map[string]any{"project": project, "name": args[0], "backup": x.Name, "phase": x.Phase}, func(w io.Writer) {
							fmt.Fprintf(w, "Backup %s completed.\n", x.Name)
						})
					case "failed":
						return fmt.Errorf("backup failed: %s", firstNonEmpty(x.Error, "no reason given"))
					}
				}
			}
			return ext.Print(g, cmd, map[string]any{"project": project, "name": args[0], "backup": started.Name, "phase": "running"}, func(w io.Writer) {
				fmt.Fprintln(w, "Still running; check with `shpyrd pg backups list "+args[0]+"`.")
			})
		},
	}
	projectFlag(cmd, &project)
	return cmd
}

func newRestoreCmd(g ext.CLIGlobals) *cobra.Command {
	var (
		project string
		to      string
		as      string
		size    string
		storage string
	)
	cmd := &cobra.Command{
		Use:   "restore <name> --as <new-name> [--to <time>]",
		Short: "Restore a database's backups into a new database, at a point in time",
		Long: `Creates a new database from the backups of an existing one, recovered to
the given moment (RFC 3339, e.g. 2026-09-25T10:00:00Z; the latest possible
when omitted), which must lie inside the window its backups cover. The
source keeps running; attach the app to the new database with
"shpyrd attach" when it is ready.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cliContext()
			if as == "" {
				return errors.New("--as names the new database")
			}
			if to != "" {
				if _, err := time.Parse(time.RFC3339, to); err != nil {
					return fmt.Errorf("--to %q: use RFC 3339, e.g. 2026-09-25T10:00:00Z", to)
				}
			}
			body := map[string]string{"as": as, "to": to, "size": size, "storage": storage}
			var created resources.View
			if err := backupsCall(ctx, g, "POST", backupsPath(project, args[0])+"/restore", body, &created); err != nil {
				return err
			}
			when := "the latest point"
			if to != "" {
				when = to
			}
			out := ext.Progress(g, cmd)
			fmt.Fprintf(out, "Restoring %s from the backups of %s to %s...\n", as, args[0], when)
			v, err := resources.WaitReadyAPI(ctx, g.API(), out, project, "Postgres", as, 15*time.Minute)
			if err != nil {
				return err
			}
			switch {
			case v == nil:
				return ext.Print(g, cmd, map[string]any{"project": project, "name": as, "ready": false}, func(w io.Writer) {
					fmt.Fprintln(w, "Still restoring; check with `shpyrd pg list`.")
				})
			case v.Phase == shpyrdv1.ResourceFailed:
				if strings.Contains(v.Message, "extension is not installed") {
					return errors.New(resources.ExtensionHint(Name))
				}
				return errors.New(v.Message)
			}
			return ext.Print(g, cmd, v, func(w io.Writer) {
				fmt.Fprintf(w, "Database %s is ready at %s. Attach it with `shpyrd attach %s --project %s`.\n", as, v.Endpoint, as, project)
			})
		},
	}
	projectFlag(cmd, &project)
	cmd.Flags().StringVar(&as, "as", "", "name of the new database (required)")
	cmd.Flags().StringVar(&to, "to", "", "point in time to recover to, RFC 3339 (default: latest)")
	cmd.Flags().StringVar(&size, "size", "", "a Postgres size for the new database (default: the source's)")
	cmd.Flags().StringVar(&storage, "storage", "", "data volume of the new database (default: the source's)")
	return cmd
}
