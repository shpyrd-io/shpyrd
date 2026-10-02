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
'shpyrd resize'; without one they get the catalog default. Databases pick
one with 'shpyrd pg create --size'; they take at least 128Mi, 256Mi when
they name none, and db-xs (128Mi, PostgreSQL tuned for about 20
connections) on a plan with less than 512Mi of memory.`,
	}
	cmd.AddCommand(newSizesListCmd(g), newSizesSetCmd(g), newSizesDeleteCmd(g), newSizesDefaultCmd(g))
	return cmd
}

func newSizesListCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List instance sizes",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			k, err := kube.Connect(kube.Options{Kubeconfig: g.kubeconfig, Context: g.kubeCtx})
			if err != nil {
				return err
			}
			cat, _, err := loadCatalog(ctx, k)
			if err != nil {
				return err
			}
			return g.print(cmd, cat, func(w io.Writer) {
				tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "NAME\tKIND\tCPU\tGUARANTEED\tMEMORY\tDESCRIPTION")
				for _, s := range cat.Sorted() {
					res := s.Resources()
					name := s.Name
					if s.Name == cat.Default {
						name += " (default)"
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", name, s.Kind, s.CPU, res.Requests.Cpu().String(), s.Memory, s.Description)
				}
				_ = tw.Flush()
				fmt.Fprintf(w, "\nDatabases take at least %s (a smaller size is raised to it) and %s when they name no size.\n", sizes.DBMinMemory, sizes.DBDefaultMemory)
				fmt.Fprintf(w, "Under %s PostgreSQL is tuned for it: a small app's database, about 20 connections; not a reporting database.\n", sizes.DBDefaultMemory)
				if _, ok := cat.Get(sizes.DBXS); ok {
					fmt.Fprintf(w, "On a plan with less than %s of memory a database is given %s unless it names a size.\n", sizes.DBXSPlanBelow, sizes.DBXS)
				}
			})
		},
	}
}

func newSizesSetCmd(g *globalFlags) *cobra.Command {
	var (
		kind, cpu, memory, desc string
		makeDefault             bool
	)
	cmd := &cobra.Command{
		Use:   "set <name> --kind shared|dedicated --cpu <cores> --memory <bytes>",
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
			cur, exists := cat.Get(args[0])
			size := sizes.Size{Name: args[0], Kind: firstNonEmpty(kind, cur.Kind, sizes.Shared), CPU: firstNonEmpty(cpu, cur.CPU), Memory: firstNonEmpty(memory, cur.Memory), Description: firstNonEmpty(desc, cur.Description)}
			if !exists && (cpu == "" || memory == "") {
				return errors.New("--cpu and --memory are required for a new size")
			}
			if err := cat.Upsert(size); err != nil {
				return err
			}
			if makeDefault {
				cat.Default = size.Name
			}
			if err := saveCatalog(ctx, k, cm, cat); err != nil {
				return err
			}
			verb := "Updated"
			if !exists {
				verb = "Added"
			}
			return g.print(cmd, size, func(w io.Writer) {
				fmt.Fprintf(w, "%s size %s (%s, %s cpu, %s memory). Processes using it are being resized.\n", verb, size.Name, size.Kind, size.CPU, size.Memory)
			})
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "shared or dedicated")
	cmd.Flags().StringVar(&cpu, "cpu", "", "cores, e.g. 0.25, 500m, 2")
	cmd.Flags().StringVar(&memory, "memory", "", "memory, e.g. 64Mi, 1Gi")
	cmd.Flags().StringVar(&desc, "description", "", "free text shown in the dashboard")
	cmd.Flags().BoolVar(&makeDefault, "default", false, "make this the default size")
	return cmd
}

func newSizesDeleteCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
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
			if err := cat.Remove(args[0]); err != nil {
				return err
			}
			if err := saveCatalog(ctx, k, cm, cat); err != nil {
				return err
			}
			return g.print(cmd, map[string]any{"size": args[0], "removed": true}, func(w io.Writer) {
				fmt.Fprintf(w, "Removed size %s\n", args[0])
			})
		},
	}
}

func newSizesDefaultCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "default <name>",
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
			if _, ok := cat.Get(args[0]); !ok {
				return fmt.Errorf("unknown size %q", args[0])
			}
			cat.Default = args[0]
			if err := saveCatalog(ctx, k, cm, cat); err != nil {
				return err
			}
			return g.print(cmd, map[string]string{"default": args[0]}, func(w io.Writer) {
				fmt.Fprintf(w, "Default size is now %s\n", args[0])
			})
		},
	}
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
