package objectstorage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/resource"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ext/resources"
)

// The developer's buckets (storage plan step 5): a bucket of a project,
// attached to its app as BUCKET_* config vars. The CRD is ObjectBucket;
// the word people read is "bucket".
const kind = "ObjectBucket"

func newBucketsCmd(g ext.CLIGlobals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "buckets",
		Short: "Buckets of a project: files for an app, with an S3 key of their own",
		Long: `Create a bucket in a project and attach it to the app. The app reads
BUCKET_ACCESS_KEY_ID, BUCKET_SECRET_ACCESS_KEY, BUCKET_ENDPOINT_URL,
BUCKET_REGION, BUCKET_BUCKET and BUCKET_URL; with --prefix AWS the SDKs
find their own names and need no code.

  shpyrd buckets create uploads --project shop             # up to the plan's cap per bucket
  shpyrd buckets create uploads --project shop --size 5Gi  # a cap of its own, within the plan
  shpyrd attach uploads --project shop --prefix AWS        # AWS_ACCESS_KEY_ID, AWS_ENDPOINT_URL, ...
  shpyrd buckets rotate uploads --project shop             # a new key; the app restarts with it`,
	}
	cmd.AddCommand(newCreateCmd(g), newListCmd(g), newInfoCmd(g), newRotateCmd(g), newDeleteCmd(g))
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

func newCreateCmd(g ext.CLIGlobals) *cobra.Command {
	var (
		project   string
		size      string
		public    bool
		retention int32
	)
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a bucket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cliContext()
			spec := map[string]any{}
			if size != "" {
				qty, err := resource.ParseQuantity(size)
				if err != nil || qty.Sign() <= 0 {
					return fmt.Errorf("invalid --size %q (use e.g. 1Gi)", size)
				}
				spec["maxBytes"] = qty.String()
			}
			if public {
				spec["public"] = true
			}
			if retention > 0 {
				spec["retentionDays"] = retention
			}
			created, err := resources.CreateAPI(ctx, g.API(), project, kind, args[0], spec)
			if err != nil {
				return err
			}
			out := ext.Progress(g, cmd)
			fmt.Fprintf(out, "Creating bucket %s...\n", args[0])
			v, err := resources.WaitReadyAPI(ctx, g.API(), out, project, kind, args[0], 2*time.Minute)
			if err != nil {
				return err
			}
			switch {
			case v == nil:
				return ext.Print(g, cmd, map[string]any{"project": project, "name": args[0], "ready": false}, func(w io.Writer) {
					fmt.Fprintln(w, "Still provisioning; check with `shpyrd buckets list`.")
				})
			case v.Phase == shpyrdv1.BucketFailed:
				if strings.Contains(v.Message, "extension is not installed") {
					return errors.New(resources.ExtensionHint(Name))
				}
				return errors.New(v.Message)
			}
			v.Note = created.Note
			return ext.Print(g, cmd, v, func(w io.Writer) {
				fmt.Fprintf(w, "Bucket %s is ready. %sAttach it with `shpyrd attach %s --project %s`.\n", args[0], withSpace(created.Note), args[0], project)
			})
		},
	}
	ext.ProjectFlag(g, cmd, &project)
	cmd.Flags().StringVar(&size, "size", "", "what the bucket may hold (1Gi, 10Gi); default: the plan's cap per bucket")
	cmd.Flags().BoolVar(&public, "public", false, "let anyone read an object by its address (listing and writing still need the key)")
	cmd.Flags().Int32Var(&retention, "retention", 0, "days an object is kept before it expires (0: forever)")
	return cmd
}

func newListCmd(g ext.CLIGlobals) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the buckets of a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := resources.ListAPI(cliContext(), g.API(), project, kind)
			if err != nil {
				return err
			}
			if list == nil {
				list = []resources.View{}
			}
			return ext.Print(g, cmd, list, func(w io.Writer) {
				if len(list) == 0 {
					fmt.Fprintf(w, "No buckets in project %s. Create one with `shpyrd buckets create uploads --project %s`.\n", project, project)
					return
				}
				tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "NAME\tUSED\tCAP\tPUBLIC\tSTATUS\tATTACHED TO")
				for _, v := range list {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", v.Name, humanBytes(bytesOf(v.Details["usedBytes"])), capWords(v.Details["capBytes"]), yesNo(v.Details["public"] == "true"), firstNonEmpty(v.Phase, "Pending"), firstNonEmpty(strings.Join(v.AttachedTo, ", "), "-"))
				}
				_ = tw.Flush()
			})
		},
	}
	ext.ProjectFlag(g, cmd, &project)
	return cmd
}

func newInfoCmd(g ext.CLIGlobals) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "info <name>",
		Short: "Show a bucket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := resources.GetAPI(cliContext(), g.API(), project, kind, args[0])
			if err != nil {
				return err
			}
			return ext.Print(g, cmd, v, func(out io.Writer) {
				fmt.Fprintf(out, "Bucket:      %s (project %s)\n", v.Name, project)
				fmt.Fprintf(out, "Status:      %s%s\n", firstNonEmpty(v.Phase, "Pending"), suffix(v.Message))
				fmt.Fprintf(out, "Holds:       %s of %s, %s objects\n", humanBytes(bytesOf(v.Details["usedBytes"])), capWords(v.Details["capBytes"]), firstNonEmpty(v.Details["objects"], "0"))
				fmt.Fprintf(out, "Name in S3:  %s\n", firstNonEmpty(v.Details["bucket"], "-"))
				fmt.Fprintf(out, "Endpoint:    %s (region %s)\n", firstNonEmpty(v.Endpoint, "-"), firstNonEmpty(v.Details["region"], "-"))
				if public := v.Details["publicEndpoint"]; public != "" {
					fmt.Fprintf(out, "From outside: %s (presigned addresses)\n", public)
				}
				fmt.Fprintf(out, "Public read: %s\n", yesNo(v.Details["public"] == "true"))
				if r := v.Details["retentionDays"]; r != "" && r != "0" {
					fmt.Fprintf(out, "Retention:   %s days\n", r)
				}
				fmt.Fprintf(out, "Attached:    %s\n", firstNonEmpty(strings.Join(v.AttachedTo, ", "), "- (shpyrd attach "+v.Name+" --project "+project+")"))
				fmt.Fprintf(out, "Config vars: BUCKET_ACCESS_KEY_ID, BUCKET_SECRET_ACCESS_KEY, BUCKET_ENDPOINT_URL, BUCKET_REGION, BUCKET_BUCKET, BUCKET_URL (values are never shown)\n")
			})
		},
	}
	ext.ProjectFlag(g, cmd, &project)
	return cmd
}

func newRotateCmd(g ext.CLIGlobals) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "rotate <name>",
		Short: "Issue a new key for a bucket (the apps bound to it restart with it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := g.API().Request(cliContext(), "POST", "api/projects/"+project+"/resources/objectbucket/"+url.PathEscape(args[0])+"/rotate", nil, "")
			if err != nil {
				return err
			}
			var answer map[string]any
			if err := json.Unmarshal(raw, &answer); err != nil {
				return fmt.Errorf("unexpected response: %s", raw)
			}
			return ext.Print(g, cmd, answer, func(w io.Writer) {
				note, _ := answer["note"].(string)
				fmt.Fprintln(w, firstNonEmpty(note, "A new key for "+args[0]+" is being written."))
			})
		},
	}
	ext.ProjectFlag(g, cmd, &project)
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
		Short:   "Delete a bucket and its objects",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cliContext()
			v, err := resources.GetAPI(ctx, g.API(), project, kind, args[0])
			if err != nil {
				return err
			}
			if len(v.AttachedTo) > 0 && !force {
				return fmt.Errorf("bucket %q is attached to %s: detach it first (shpyrd detach %s --project %s) or pass --force", v.Name, strings.Join(v.AttachedTo, ", "), v.Name, project)
			}
			if !yes {
				return fmt.Errorf("this deletes bucket %q and every object in it; re-run with --yes to confirm", v.Name)
			}
			if err := resources.DeleteAPI(ctx, g.API(), project, kind, v.Name, force); err != nil {
				return err
			}
			return ext.Print(g, cmd, map[string]any{"project": project, "name": v.Name, "deleted": true}, func(w io.Writer) {
				fmt.Fprintf(w, "Deleted bucket %s from project %s\n", v.Name, project)
			})
		},
	}
	ext.ProjectFlag(g, cmd, &project)
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm")
	cmd.Flags().BoolVar(&force, "force", false, "delete even while attached to an app")
	return cmd
}

func bytesOf(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// capWords is a cap in words: "no cap" when the bucket has none.
func capWords(capBytes string) string {
	n := bytesOf(capBytes)
	if n <= 0 {
		return "no cap"
	}
	return humanBytes(n)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func suffix(msg string) string {
	if msg == "" {
		return ""
	}
	return " (" + msg + ")"
}

func withSpace(s string) string {
	if s == "" {
		return ""
	}
	return s + " "
}
