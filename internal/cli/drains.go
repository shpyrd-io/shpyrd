package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/install"
)

// Log drains (RFC-0023): `shpyrd drains add|list|remove`, per project or,
// with --cluster, for every project.

func newDrainsCmd(g *globalFlags) *cobra.Command {
	var (
		appName   string
		cluster   bool
		workspace string
	)
	cmd := &cobra.Command{
		Use:   "drains",
		Short: "Forward logs to an external receiver (HTTPS or syslog)",
		Long: `Log drains forward a project's log lines to a receiver as they are written:
JSON over HTTPS (Datadog, Better Stack, Axiom, your own collector) or RFC 5424
syslog over TCP/TLS (Papertrail, rsyslog). A workspace drain (--workspace,
workspace admins) receives every project's lines of the workspace, labelled
with the project; a cluster drain (--cluster, the operator, over a kubeconfig)
receives every project's lines of the platform.

  shpyrd drains add https://in.logs.example.com/ingest --header "Authorization: Bearer ..." --project shop
  shpyrd drains add syslog+tls://logs.example.com:6514 --workspace acme
  shpyrd drains add syslog+tls://logs.example.com:6514 --cluster
  shpyrd drains list --project shop
  shpyrd drains remove in-logs-example-com --project shop

Header values are stored in the cluster and never printed again. Needs the
logs-agent extension (shpyrd extensions enable logs-agent).`,
	}
	add := &cobra.Command{
		Use:   "add <url>",
		Short: "Add a drain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			format, _ := cmd.Flags().GetString("format")
			rawHeaders, _ := cmd.Flags().GetStringArray("header")
			processes, _ := cmd.Flags().GetStringSlice("processes")
			ns, project, err := drainScope(cluster, appName, workspace)
			if err != nil {
				return err
			}
			headers := map[string]string{}
			for _, h := range rawHeaders {
				k, v, ok := strings.Cut(h, ":")
				if !ok || strings.TrimSpace(k) == "" {
					return fmt.Errorf("--header expects \"Name: value\", got %q", h)
				}
				headers[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
			name, err = api.DrainName(name, args[0])
			if err != nil {
				return err
			}
			// A workspace's drain in the system namespace is named and
			// labelled after the workspace, as the server does it.
			objectName := name
			labels := map[string]string{shpyrdv1.LabelManagedBy: "shpyrd"}
			if workspace != "" {
				objectName = workspace + "-" + name
				labels[shpyrdv1.LabelWorkspace] = workspace
			}
			d := &shpyrdv1.LogDrain{
				ObjectMeta: metav1.ObjectMeta{Name: objectName, Namespace: ns, Labels: labels},
				Spec:       shpyrdv1.LogDrainSpec{URL: strings.TrimSpace(args[0]), Format: format, Processes: processes},
			}
			if err := controller.ValidateDrainURL(d.Spec.URL, d.EffectiveFormat()); err != nil {
				return err
			}
			ctx := signalContext()
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			if ac.session {
				body, _ := json.Marshal(api.CreateDrainRequest{Name: name, URL: d.Spec.URL, Format: format, Headers: headers, Processes: processes})
				if workspace != "" {
					// Over a session, the workspace is the door's.
					if _, err := ac.serverRequest(ctx, "POST", "api/workspace/drains", body, "application/json"); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "Added drain %s to the workspace: every project's logs -> %s (%s)\n", name, d.Spec.URL, d.EffectiveFormat())
					return nil
				}
				if project == "" {
					return errors.New("cluster drains are the platform operator's: run this with --context (shpyrd-ctl)")
				}
				if _, err := ac.serverRequest(ctx, "POST", "api/projects/"+project+"/drains", body, "application/json"); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Added drain %s to project %s: logs -> %s (%s)\n", name, project, d.Spec.URL, d.EffectiveFormat())
				return nil
			}
			if project != "" {
				if _, err := ac.getApp(ctx, project); err != nil {
					return err
				}
			}
			if len(headers) > 0 {
				secName := "drain-" + objectName + "-headers"
				sec := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: secName, Namespace: ns, Labels: map[string]string{shpyrdv1.LabelManagedBy: "shpyrd"}},
					Type:       corev1.SecretTypeOpaque, Data: map[string][]byte{},
				}
				for k, v := range headers {
					sec.Data[k] = []byte(v)
				}
				if err := ac.c.Create(ctx, sec); err != nil {
					if !apierrors.IsAlreadyExists(err) {
						return err
					}
					if err := ac.c.Update(ctx, sec); err != nil {
						return err
					}
				}
				d.Spec.HeadersFrom = &corev1.LocalObjectReference{Name: secName}
			}
			if err := ac.c.Create(ctx, d); err != nil {
				if apierrors.IsAlreadyExists(err) {
					return fmt.Errorf("drain %q already exists", name)
				}
				return err
			}
			if workspace != "" {
				ac.auditCluster(ctx, "drain.add", name+" -> "+d.Spec.URL, "workspace "+workspace+" drain ("+d.EffectiveFormat()+")")
				fmt.Fprintf(cmd.OutOrStdout(), "Added drain %s to workspace %s: every project's logs -> %s (%s)\n", name, workspace, d.Spec.URL, d.EffectiveFormat())
			} else if project == "" {
				ac.auditCluster(ctx, "drain.add", name+" -> "+d.Spec.URL, "cluster drain ("+d.EffectiveFormat()+")")
				fmt.Fprintf(cmd.OutOrStdout(), "Added cluster drain %s: every project's logs -> %s (%s)\n", name, d.Spec.URL, d.EffectiveFormat())
			} else {
				ac.audit(ctx, project, "drain.add", name+" -> "+d.Spec.URL, d.EffectiveFormat())
				fmt.Fprintf(cmd.OutOrStdout(), "Added drain %s to project %s: logs -> %s (%s)\n", name, project, d.Spec.URL, d.EffectiveFormat())
			}
			if !contains(recordedExtensions(ctx, ac.k), "logs-agent") {
				fmt.Fprintln(cmd.OutOrStdout(), "The logs-agent extension is not enabled; nothing is forwarded until you run `shpyrd extensions enable logs-agent`.")
			}
			return nil
		},
	}
	add.Flags().String("name", "", "drain name (default: derived from the url host)")
	add.Flags().String("format", "", "json or syslog (default: from the url scheme)")
	add.Flags().StringArray("header", nil, `HTTP header for json drains, "Name: value" (repeatable; values are stored, never shown)`)
	add.Flags().StringSlice("processes", nil, "only these process types (default: all)")

	list := &cobra.Command{
		Use:     "list",
		Short:   "List drains and their delivery status",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, project, err := drainScope(cluster, appName, workspace)
			if err != nil {
				return err
			}
			ctx := signalContext()
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			if project != "" {
				if _, err := ac.getApp(ctx, project); err != nil {
					return err
				}
			}
			var drains shpyrdv1.LogDrainList
			if ac.session {
				path := "api/projects/" + project + "/drains"
				if workspace != "" {
					path = "api/workspace/drains"
				} else if project == "" {
					return errors.New("cluster drains are the platform operator's: run this with --context (shpyrd-ctl)")
				}
				raw, err := ac.serverRequest(ctx, "GET", path, nil, "")
				if err != nil {
					return err
				}
				var views []api.DrainView
				if err := json.Unmarshal(raw, &views); err != nil {
					return err
				}
				for _, v := range views {
					d := shpyrdv1.LogDrain{ObjectMeta: metav1.ObjectMeta{Name: v.Name}, Spec: shpyrdv1.LogDrainSpec{URL: v.URL, Format: v.Format, Processes: v.Processes}}
					d.Status.Phase, d.Status.Message = v.Phase, v.Message
					if v.LastDeliveryAt != nil {
						t := metav1.NewTime(*v.LastDeliveryAt)
						d.Status.LastDeliveryAt = &t
					}
					drains.Items = append(drains.Items, d)
				}
			} else if err := ac.c.List(ctx, &drains, client.InNamespace(ns)); err != nil {
				return err
			} else if ns == install.DefaultSystemNamespace {
				// The system namespace holds the cluster's drains and every
				// workspace's: keep the scope asked for, by its bare names.
				kept := drains.Items[:0]
				for _, d := range drains.Items {
					if d.Labels[shpyrdv1.LabelWorkspace] != workspace {
						continue
					}
					d.Name = strings.TrimPrefix(d.Name, workspace+"-")
					kept = append(kept, d)
				}
				drains.Items = kept
			}
			if len(drains.Items) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no drains; add one with `shpyrd drains add <url>`")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tURL\tFORMAT\tPROCESSES\tSTATUS\tLAST DELIVERY")
			for _, d := range drains.Items {
				procs := "all"
				if len(d.Spec.Processes) > 0 {
					procs = strings.Join(d.Spec.Processes, ",")
				}
				last := "-"
				if d.Status.LastDeliveryAt != nil {
					last = age(*d.Status.LastDeliveryAt) + " ago"
				}
				status := firstNonEmpty(d.Status.Phase, shpyrdv1.DrainPending)
				if d.Status.Message != "" && d.Status.Phase != shpyrdv1.DrainActive {
					status += " (" + d.Status.Message + ")"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", d.Name, d.Spec.URL, d.EffectiveFormat(), procs, status, last)
			}
			return tw.Flush()
		},
	}

	remove := &cobra.Command{
		Use:     "remove <name>",
		Short:   "Remove a drain",
		Aliases: []string{"rm"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, project, err := drainScope(cluster, appName, workspace)
			if err != nil {
				return err
			}
			ctx := signalContext()
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			if ac.session {
				path := "api/projects/" + project + "/drains/" + url.PathEscape(args[0])
				if workspace != "" {
					path = "api/workspace/drains/" + url.PathEscape(args[0])
				} else if project == "" {
					return errors.New("cluster drains are the platform operator's: run this with --context (shpyrd-ctl)")
				}
				if _, err := ac.serverRequest(ctx, "DELETE", path, nil, ""); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Removed drain %s\n", args[0])
				return nil
			}
			objectName := args[0]
			if workspace != "" {
				objectName = workspace + "-" + args[0]
			}
			d := &shpyrdv1.LogDrain{}
			if err := ac.c.Get(ctx, types.NamespacedName{Namespace: ns, Name: objectName}, d); err != nil {
				if apierrors.IsNotFound(err) {
					return fmt.Errorf("no drain %q; see `shpyrd drains list`", args[0])
				}
				return err
			}
			if d.Spec.HeadersFrom != nil {
				_ = ac.c.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: d.Spec.HeadersFrom.Name}})
			}
			if err := ac.c.Delete(ctx, d); err != nil {
				return err
			}
			if workspace != "" {
				ac.auditCluster(ctx, "drain.remove", args[0], "workspace "+workspace)
			} else if project == "" {
				ac.auditCluster(ctx, "drain.remove", args[0], "")
			} else {
				ac.audit(ctx, project, "drain.remove", args[0], "")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed drain %s\n", args[0])
			return nil
		},
	}

	for _, c := range []*cobra.Command{add, list, remove} {
		appFlag(c, &appName)
		c.Flags().BoolVar(&cluster, "cluster", false, "every project's logs of the platform (the operator, over a kubeconfig)")
		c.Flags().StringVar(&workspace, "workspace", "", "every project's logs of a workspace, by its slug (over a session, the door's)")
	}
	cmd.AddCommand(add, list, remove)
	return cmd
}

// drainScope resolves --cluster / --workspace / --project into a namespace.
func drainScope(cluster bool, appName, workspace string) (ns, project string, err error) {
	if cluster || workspace != "" {
		if appName != "" || (cluster && workspace != "") {
			return "", "", errors.New("--cluster, --workspace and --project are exclusive")
		}
		return install.DefaultSystemNamespace, "", nil
	}
	project, err = resolveAppName(appName)
	if err != nil {
		return "", "", err
	}
	return appNamespace(project), project, nil
}
