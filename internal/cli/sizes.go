package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
)

// ---- catalog ---------------------------------------------------------------

func loadCatalog(ctx context.Context, k *kube.Client) (*sizes.Catalog, *corev1.ConfigMap, error) {
	cm, err := k.Kube.CoreV1().ConfigMaps(install.DefaultSystemNamespace).Get(ctx, sizes.ConfigMapName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		d := sizes.Defaults()
		return &d, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	cat, err := sizes.Parse([]byte(cm.Data[sizes.ConfigMapKey]))
	if err != nil {
		return nil, cm, err
	}
	return cat, cm, nil
}

func saveCatalog(ctx context.Context, k *kube.Client, cm *corev1.ConfigMap, cat *sizes.Catalog) error {
	if err := cat.Validate(); err != nil {
		return err
	}
	data, err := cat.Marshal()
	if err != nil {
		return err
	}
	cms := k.Kube.CoreV1().ConfigMaps(install.DefaultSystemNamespace)
	if cm == nil {
		_, err = cms.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: sizes.ConfigMapName, Namespace: install.DefaultSystemNamespace},
			Data:       map[string]string{sizes.ConfigMapKey: string(data)},
		}, metav1.CreateOptions{})
		return err
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data[sizes.ConfigMapKey] = string(data)
	_, err = cms.Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

func newSizesCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sizes",
		Short: "Manage the instance size catalog (cpu and memory per process)",
		Long: `Instance sizes are named cpu/memory allocations a process runs with, like
Fly machine sizes or Render instance types. The catalog is cluster-wide.

  shared    cpu is the ceiling; 1/8 of it is guaranteed, the rest is borrowed
            from idle neighbours (Burstable QoS)
  dedicated requests equal limits, whole cores (Guaranteed QoS)

Processes pick a size in shpyrd.yaml (processes.<type>.size) or with
'shpyrd resize'; without one they get the default. Databases and stores
have lists of their own, with the same names, as Heroku's add-ons have
plans of their own: a Postgres shared-s (128Mi, PostgreSQL tuned for 20
connections) is not a process shared-s. They pick one with
'shpyrd pg create --size' or 'shpyrd redis create --size', change it with
'shpyrd pg resize' or 'shpyrd redis resize', and get their list's default
without one. Edit those lists with --for postgres or --for redis.

Without a subcommand, the catalog is listed.`,
		Args: cobra.NoArgs,
		RunE: sizesList(g),
	}
	cmd.AddCommand(newSizesListCmd(g), newSizesSetCmd(g), newSizesDeleteCmd(g), newSizesDefaultCmd(g))
	return cmd
}

func newSizesListCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List instance sizes",
		Aliases: []string{"ls"},
		RunE:    sizesList(g),
	}
}

// sizesList prints the catalog. Signed in to a workspace it reads the API,
// as every developer command does (RFC-0052), so an agent with a session and
// no kubeconfig sees the sizes it must choose from (issue #63); with a
// cluster it reads the ConfigMap. Both answer the API's shape.
func sizesList(g *globalFlags) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx := signalContext()
		ac, err := newAppClient(g, g.progress(cmd))
		if err != nil {
			return err
		}
		var resp api.SizesResponse
		if ac.session {
			body, err := ac.serverRequest(ctx, "GET", "api/sizes", nil, "")
			if err != nil {
				return err
			}
			if err := json.Unmarshal(body, &resp); err != nil {
				return fmt.Errorf("decode sizes: %w", err)
			}
		} else {
			cat, _, err := loadCatalog(ctx, ac.k)
			if err != nil {
				return err
			}
			resp = api.SizesOf(cat)
		}
		return g.print(cmd, resp, func(w io.Writer) {
			table := func(title string, list sizes.List, connections bool) {
				fmt.Fprintln(w, title)
				tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
				head := "NAME\tKIND\tCPU\tGUARANTEED\tMEMORY"
				if connections {
					head += "\tCONNECTIONS"
				}
				fmt.Fprintln(tw, head+"\tDESCRIPTION")
				for _, s := range list.Sizes {
					res := s.Resources()
					name := s.Name
					if s.Name == list.Default {
						name += " (default)"
					}
					row := fmt.Sprintf("%s\t%s\t%s\t%s\t%s", name, s.Kind, s.CPU, res.Requests.Cpu().String(), s.Memory)
					if connections {
						row += fmt.Sprintf("\t%d", s.Connections)
					}
					fmt.Fprintf(tw, "%s\t%s\n", row, s.Description)
				}
				_ = tw.Flush()
			}
			table("Processes", sizes.List{Default: resp.Default, Sizes: resp.Sizes}, false)
			fmt.Fprintln(w)
			table("Postgres (shpyrd pg create --size)", resp.Postgres, true)
			fmt.Fprintln(w)
			table("Redis (shpyrd redis create --size)", resp.Redis, true)
		})
	}
}

// forFlag picks the list a sizes command edits: processes, or with
// --for postgres|redis the list of databases or stores.
func forFlag(cmd *cobra.Command, dst *string) {
	cmd.Flags().StringVar(dst, "for", "", "the list to edit: postgres or redis (default: processes)")
}

// listFor is the list of the catalog --for names.
func listFor(cat *sizes.Catalog, what string) (*sizes.List, error) {
	if l := cat.For(what); l != nil {
		return l, nil
	}
	return nil, fmt.Errorf("--for %q: postgres or redis (or nothing, for processes)", what)
}

// thoseUsing says who follows a change of a size of a list.
func thoseUsing(what string) string {
	switch what {
	case sizes.ForPostgres:
		return "Databases"
	case sizes.ForRedis:
		return "Stores"
	}
	return "Processes"
}

func newSizesSetCmd(g *globalFlags) *cobra.Command {
	var (
		kind, cpu, memory, desc, what string
		connections                   int
		makeDefault                   bool
	)
	cmd := &cobra.Command{
		Use:   "set <name> --kind shared|dedicated --cpu <cores> --memory <bytes> [--for postgres|redis --connections <n>]",
		Short: "Add or change an instance size",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			k, err := kube.Connect(kube.Options{Kubeconfig: g.kubeconfig, Context: g.kubeCtx})
			if err != nil {
				return err
			}
			cat, cm, err := loadCatalog(ctx, k)
			if err != nil {
				return err
			}
			list, err := listFor(cat, what)
			if err != nil {
				return err
			}
			cur, exists := list.Get(args[0])
			size := sizes.Size{Name: args[0], Kind: firstNonEmpty(kind, cur.Kind, sizes.Shared), CPU: firstNonEmpty(cpu, cur.CPU), Memory: firstNonEmpty(memory, cur.Memory), Description: firstNonEmpty(desc, cur.Description), Connections: cur.Connections}
			if cmd.Flags().Changed("connections") {
				size.Connections = connections
			}
			if !exists && (cpu == "" || memory == "") {
				return errors.New("--cpu and --memory are required for a new size")
			}
			if err := list.Upsert(size); err != nil {
				return err
			}
			if makeDefault {
				list.Default = size.Name
			}
			if err := saveCatalog(ctx, k, cm, cat); err != nil {
				return err
			}
			verb := "Updated"
			if !exists {
				verb = "Added"
			}
			return g.print(cmd, size, func(w io.Writer) {
				fmt.Fprintf(w, "%s size %s for %s (%s, %s cpu, %s memory). %s using it are being resized.\n", verb, size.Name, sizes.Of(what), size.Kind, size.CPU, size.Memory, thoseUsing(what))
			})
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "shared or dedicated")
	cmd.Flags().StringVar(&cpu, "cpu", "", "cores, e.g. 0.25, 500m, 2")
	cmd.Flags().StringVar(&memory, "memory", "", "memory, e.g. 64Mi, 1Gi")
	cmd.Flags().StringVar(&desc, "description", "", "free text shown in the dashboard")
	cmd.Flags().IntVar(&connections, "connections", 0, "clients a database or store of this size takes (max_connections, maxclients)")
	cmd.Flags().BoolVar(&makeDefault, "default", false, "make this the default size of its list")
	forFlag(cmd, &what)
	return cmd
}

func newSizesDeleteCmd(g *globalFlags) *cobra.Command {
	var what string
	cmd := &cobra.Command{
		Use:   "delete <name> [--for postgres|redis]",
		Short: "Remove an instance size (processes still naming it fall back to the default)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			k, err := kube.Connect(kube.Options{Kubeconfig: g.kubeconfig, Context: g.kubeCtx})
			if err != nil {
				return err
			}
			cat, cm, err := loadCatalog(ctx, k)
			if err != nil {
				return err
			}
			list, err := listFor(cat, what)
			if err != nil {
				return err
			}
			if err := list.Remove(args[0]); err != nil {
				return err
			}
			if err := saveCatalog(ctx, k, cm, cat); err != nil {
				return err
			}
			return g.print(cmd, map[string]any{"size": args[0], "for": firstNonEmpty(what, "processes"), "removed": true}, func(w io.Writer) {
				fmt.Fprintf(w, "Removed size %s for %s\n", args[0], sizes.Of(what))
			})
		},
	}
	forFlag(cmd, &what)
	return cmd
}

func newSizesDefaultCmd(g *globalFlags) *cobra.Command {
	var what string
	cmd := &cobra.Command{
		Use:   "default <name> [--for postgres|redis]",
		Short: "Set the default instance size",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			k, err := kube.Connect(kube.Options{Kubeconfig: g.kubeconfig, Context: g.kubeCtx})
			if err != nil {
				return err
			}
			cat, cm, err := loadCatalog(ctx, k)
			if err != nil {
				return err
			}
			list, err := listFor(cat, what)
			if err != nil {
				return err
			}
			if _, err := list.Pick(what, args[0]); err != nil {
				return err
			}
			list.Default = args[0]
			if err := saveCatalog(ctx, k, cm, cat); err != nil {
				return err
			}
			return g.print(cmd, map[string]string{"default": args[0], "for": firstNonEmpty(what, "processes")}, func(w io.Writer) {
				fmt.Fprintf(w, "The default size for %s is now %s\n", sizes.Of(what), args[0])
			})
		},
	}
	forFlag(cmd, &what)
	return cmd
}

// ---- resize -----------------------------------------------------------------

func newResizeCmd(g *globalFlags) *cobra.Command {
	var appName string
	cmd := &cobra.Command{
		Use:   "resize PROCESS=SIZE [PROCESS=SIZE...]",
		Short: "Set the instance size of process types, e.g. web=shared-m worker=dedicated-s",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			name, err := resolveAppName(appName)
			if err != nil {
				return err
			}
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			if ac.session {
				// Through the API: the server knows the catalog and the plan.
				var parts []string
				sizesOf := map[string]string{}
				for _, kv := range args {
					proc, size, ok := strings.Cut(kv, "=")
					if !ok {
						return fmt.Errorf("expected PROCESS=SIZE, got %q", kv)
					}
					body, _ := json.Marshal(map[string]string{"process": proc, "size": size})
					if _, err := ac.serverRequest(ctx, "POST", "api/projects/"+name+"/resize", body, "application/json"); err != nil {
						return err
					}
					parts = append(parts, kv)
					sizesOf[proc] = size
				}
				return g.print(cmd, map[string]any{"project": name, "processes": sizesOf}, func(w io.Writer) {
					fmt.Fprintf(w, "Resizing %s: %s\n", name, strings.Join(parts, " "))
				})
			}
			cat, _, err := loadCatalog(ctx, ac.k)
			if err != nil {
				return err
			}
			changes := map[string]string{}
			for _, kv := range args {
				proc, size, ok := strings.Cut(kv, "=")
				if !ok {
					return fmt.Errorf("expected PROCESS=SIZE, got %q", kv)
				}
				if _, ok := cat.Get(size); !ok {
					return fmt.Errorf("unknown size %q (see `shpyrd sizes list`)", size)
				}
				changes[proc] = size
			}
			_, err = ac.updateApp(ctx, name, func(a *shpyrdv1.App) error {
				if a.Spec.Processes == nil {
					a.Spec.Processes = map[string]shpyrdv1.Process{"web": {}}
				}
				for proc, size := range changes {
					p := a.Spec.Processes[proc]
					p.Size = size
					p.Resources = corev1.ResourceRequirements{}
					a.Spec.Processes[proc] = p
				}
				return nil
			})
			if err != nil {
				return err
			}
			var parts []string
			for proc, size := range changes {
				parts = append(parts, proc+"="+size)
			}
			ac.audit(ctx, name, "resize", name, strings.Join(args, " "))
			return g.print(cmd, map[string]any{"project": name, "processes": changes}, func(w io.Writer) {
				fmt.Fprintf(w, "Resizing %s: %s (new release, rolling restart)\n", name, strings.Join(parts, " "))
			})
		},
	}
	appFlag(cmd, &appName)
	return cmd
}
