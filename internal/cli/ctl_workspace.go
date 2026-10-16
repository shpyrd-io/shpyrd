package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// shpyrd-ctl workspace: the open-source platform's one workspace, as its
// operator sees it: what it may use (limits), what its projects and
// databases do when nobody uses them (sleep), and how it stands.

// workspaceSettingsAnswer is GET/PUT /api/cluster/workspace-settings.
type workspaceSettingsAnswer struct {
	Workspace string               `json:"workspace"`
	Limits    *store.Limits        `json:"limits"`
	Sleep     *store.SleepDefaults `json:"sleep"`
	Usage     *api.Usage           `json:"usage"`
}

func newCtlWorkspaceCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "The platform's workspace: its status, limits and sleep defaults",
		Long: `The open-source platform hosts one workspace. Its limits bound what its
projects may use together; its sleep defaults say when the projects and
databases that set none of their own go to sleep.

  shpyrd-ctl workspace status
  shpyrd-ctl workspace limits --projects 10 --memory 8Gi
  shpyrd-ctl workspace sleep --apps 15m --databases 30m`,
	}
	cmd.AddCommand(newCtlWorkspaceStatusCmd(g), newCtlWorkspaceLimitsCmd(g), newCtlWorkspaceSleepCmd(g))
	return cmd
}

func readWorkspaceSettings(cmd *cobra.Command, g *globalFlags) (*appClient, *workspaceSettingsAnswer, error) {
	ac, err := newAppClient(g, g.progress(cmd))
	if err != nil {
		return nil, nil, err
	}
	raw, err := serverRequest(signalContext(), ac.k, "GET", "api/cluster/workspace-settings", nil, "")
	if err != nil {
		return nil, nil, err
	}
	var a workspaceSettingsAnswer
	return ac, &a, json.Unmarshal(raw, &a)
}

func writeWorkspaceSettings(cmd *cobra.Command, g *globalFlags, ac *appClient, limits *store.Limits, sleep *store.SleepDefaults) error {
	body, _ := json.Marshal(api.WorkspaceSettings{Limits: limits, Sleep: sleep})
	raw, err := serverRequest(signalContext(), ac.k, "PUT", "api/cluster/workspace-settings", body, "application/json")
	if err != nil {
		return err
	}
	var a workspaceSettingsAnswer
	if err := json.Unmarshal(raw, &a); err != nil {
		return err
	}
	return g.print(cmd, a, func(w io.Writer) { printWorkspaceSettings(w, &a) })
}

func printWorkspaceSettings(w io.Writer, a *workspaceSettingsAnswer) {
	fmt.Fprintf(w, "Workspace %s\n", a.Workspace)
	if a.Limits == nil {
		fmt.Fprintln(w, "  limits: none")
	} else {
		fmt.Fprintf(w, "  limits: %s\n", limitsLine(a.Limits))
		if a.Usage != nil {
			fmt.Fprintf(w, "  in use: %d projects, %d instances, %s CPU, %s memory, %s storage, %d buckets, %d databases (%d shared)\n", a.Usage.Projects, a.Usage.Instances, a.Usage.CPU, a.Usage.Memory, a.Usage.Storage, a.Usage.Buckets, a.Usage.Databases, a.Usage.SharedDatabases)
		}
	}
	if a.Sleep == nil {
		fmt.Fprintln(w, "  sleep:  none by default")
	} else {
		apps, dbs := "never", "never"
		if a.Sleep.AppsAfter != "" {
			apps = "after " + a.Sleep.AppsAfter + " (" + firstNonEmpty(a.Sleep.AppsResuming, "wait") + ")"
		}
		if a.Sleep.DatabasesAfter != "" {
			dbs = "after " + a.Sleep.DatabasesAfter
		}
		fmt.Fprintf(w, "  sleep:  apps %s, databases %s\n", apps, dbs)
	}
}

func limitsLine(l *store.Limits) string {
	out := ""
	add := func(s string) {
		if out != "" {
			out += ", "
		}
		out += s
	}
	if l.Projects > 0 {
		add(fmt.Sprintf("%d projects", l.Projects))
	}
	if l.Instances > 0 {
		add(fmt.Sprintf("%d instances", l.Instances))
	}
	if l.CPU != "" {
		add(l.CPU + " CPU")
	}
	if l.Memory != "" {
		add(l.Memory + " memory")
	}
	if l.Storage != "" {
		add(l.Storage + " storage")
	}
	if !l.AllowsDisks() {
		add("no disks")
	}
	if o := l.ObjectStorage; o != nil {
		if o.Buckets > 0 {
			add(fmt.Sprintf("%d buckets", o.Buckets))
		}
		if o.BucketCap != "" {
			add(o.BucketCap + " per bucket")
		}
		if o.Total != "" {
			add(o.Total + " object storage")
		}
	}
	if pg := l.Postgres; pg != nil {
		if pg.Databases > 0 {
			add(fmt.Sprintf("%d databases", pg.Databases))
		}
		if len(pg.Types) > 0 {
			add(strings.Join(pg.Types, " and ") + " databases")
		}
		if pg.DefaultType != "" {
			add(pg.DefaultType + " by default")
		}
		if pg.SharedCap != "" {
			add(pg.SharedCap + " shared cap")
		}
		if pg.MaxInstances > 0 {
			add(fmt.Sprintf("up to %d instances", pg.MaxInstances))
		}
	}
	return firstNonEmpty(out, "none")
}

func newCtlWorkspaceStatusCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the workspace's limits, what it uses and its sleep defaults",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, a, err := readWorkspaceSettings(cmd, g)
			if err != nil {
				return err
			}
			return g.print(cmd, a, func(w io.Writer) { printWorkspaceSettings(w, a) })
		},
	}
}

func newCtlWorkspaceLimitsCmd(g *globalFlags) *cobra.Command {
	var patch store.Limits
	var buckets store.ObjectStorageLimits
	var pg store.PostgresLimits
	var disks, none bool
	cmd := &cobra.Command{
		Use:   "limits",
		Short: "Change what the workspace's projects may use together",
		Long: `Change the workspace's limits: the flags given change, the others stay.
--none removes every limit.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ac, cur, err := readWorkspaceSettings(cmd, g)
			if err != nil {
				return err
			}
			if none {
				return writeWorkspaceSettings(cmd, g, ac, nil, cur.Sleep)
			}
			if cmd.Flags().Changed("disks") {
				patch.AllowDisks = &disks
			}
			if buckets != (store.ObjectStorageLimits{}) {
				patch.ObjectStorage = &buckets
			}
			if pg.Databases != 0 || pg.MaxInstances != 0 || pg.SharedCap != "" || pg.DefaultType != "" || len(pg.Types) != 0 {
				patch.Postgres = &pg
			}
			if patch.Projects == 0 && patch.Instances == 0 && patch.CPU == "" && patch.Memory == "" && patch.Storage == "" && patch.AllowDisks == nil && patch.ObjectStorage == nil && patch.Postgres == nil {
				return fmt.Errorf("give a limit to change (--projects, --instances, --cpu, --memory, --storage, --disks, --buckets, --bucket-cap, --object-storage, --databases, --database-kinds, --database-default, --shared-cap, --database-instances) or --none")
			}
			return writeWorkspaceSettings(cmd, g, ac, store.MergeLimits(cur.Limits, &patch), cur.Sleep)
		},
	}
	cmd.Flags().IntVar(&patch.Projects, "projects", 0, "how many projects")
	cmd.Flags().IntVar(&patch.Instances, "instances", 0, "how many instances, every process of every project")
	cmd.Flags().StringVar(&patch.CPU, "cpu", "", "CPU of their instance sizes together (2, 500m)")
	cmd.Flags().StringVar(&patch.Memory, "memory", "", "memory of their instance sizes together (512Mi, 8Gi)")
	cmd.Flags().StringVar(&patch.Storage, "storage", "", "block storage their disks and databases claim together (10Gi)")
	cmd.Flags().BoolVar(&disks, "disks", true, "whether disks are included at all (--disks=false: files go in a bucket; disks that exist keep working)")
	cmd.Flags().IntVar(&buckets.Buckets, "buckets", 0, "how many buckets")
	cmd.Flags().StringVar(&buckets.BucketCap, "bucket-cap", "", "what one bucket may hold (1Gi)")
	cmd.Flags().StringVar(&buckets.Total, "object-storage", "", "what the buckets may hold together (10Gi)")
	cmd.Flags().IntVar(&pg.Databases, "databases", 0, "how many databases, shared and dedicated together")
	cmd.Flags().StringSliceVar(&pg.Types, "database-kinds", nil, "the kinds of database allowed: shared, dedicated (both when unset)")
	cmd.Flags().StringVar(&pg.DefaultType, "database-default", "", "the kind a database gets when none is asked (dedicated when unset)")
	cmd.Flags().StringVar(&pg.SharedCap, "shared-cap", "", "what a shared database may hold (1Gi)")
	cmd.Flags().IntVar(&pg.MaxInstances, "database-instances", 0, "how many instances a dedicated database may run")
	cmd.Flags().BoolVar(&none, "none", false, "remove every limit")
	return cmd
}

func newCtlWorkspaceSleepCmd(g *globalFlags) *cobra.Command {
	var apps, resuming, databases string
	var none bool
	cmd := &cobra.Command{
		Use:   "sleep",
		Short: "Change when the workspace's projects and databases sleep by default",
		Long: `Change the sleep defaults: projects and databases with no policy of their
own sleep after this quiet period. A duration from 5m to 24h, or off; the
flags given change, the others stay. --none removes the defaults.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ac, cur, err := readWorkspaceSettings(cmd, g)
			if err != nil {
				return err
			}
			if none {
				return writeWorkspaceSettings(cmd, g, ac, cur.Limits, nil)
			}
			next := store.SleepDefaults{}
			if cur.Sleep != nil {
				next = *cur.Sleep
			}
			changed := false
			if cmd.Flags().Changed("apps") {
				next.AppsAfter, changed = apps, true
				if apps == "off" {
					next.AppsAfter, next.AppsResuming = "", ""
				}
			}
			if cmd.Flags().Changed("resuming") {
				next.AppsResuming, changed = resuming, true
			}
			if cmd.Flags().Changed("databases") {
				next.DatabasesAfter, changed = databases, true
				if databases == "off" {
					next.DatabasesAfter = ""
				}
			}
			if !changed {
				return fmt.Errorf("give a default to change (--apps, --resuming, --databases) or --none")
			}
			return writeWorkspaceSettings(cmd, g, ac, cur.Limits, &next)
		},
	}
	cmd.Flags().StringVar(&apps, "apps", "", "quiet period before a web process sleeps (15m), or off")
	cmd.Flags().StringVar(&resuming, "resuming", "", "how a sleeping app wakes: page (a waiting page) or wait (the request is held)")
	cmd.Flags().StringVar(&databases, "databases", "", "idle period before a database hibernates (30m), or off")
	cmd.Flags().BoolVar(&none, "none", false, "remove the sleep defaults")
	return cmd
}
