// Package objectstorage is the object-storage extension (RFC-0046): a
// cloud-backed gateway or local Garage, a logical bucket and a
// scoped credential per consumer through ObjectBucket resources, usage on
// the cluster page.
package objectstorage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ext/resources"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
)

// Name of the extension.
const Name = "object-storage"

// Endpoint of the store inside the cluster (the component's Service) and
// AdminEndpoint of its admin API.
func Endpoint(systemNamespace string) string {
	return "http://" + install.ObjectStorageService + "." + systemNamespace + ".svc:3900"
}

func AdminEndpoint(systemNamespace string) string {
	return "http://" + install.ObjectStorageService + "." + systemNamespace + ".svc:3903"
}

type extension struct{}

// New returns the extension.
func New() ext.Extension { return extension{} }

func (extension) Name() string { return Name }
func (extension) Description() string {
	return "S3-compatible storage through the cloud gateway or local Garage with a key per consumer: the backing store for Postgres backups and platform backups (RFC-0046)"
}

// Components installs local Garage; cloud profiles with a gateway skip it.
func (extension) Components() []ext.ComponentRef {
	return []ext.ComponentRef{{Name: "object-storage", Runlevel: "rc3"}}
}

// Register runs the ObjectBucket controller.
func (extension) Register(mgr ctrl.Manager, deps ext.Deps) error {
	var capacity int64
	if q, err := resource.ParseQuantity(deps.Var(install.VarObjectStorageSize)); err == nil {
		capacity = q.Value()
	}
	r := &controller.ObjectBucketReconciler{
		Client: mgr.GetClient(), Reader: mgr.GetAPIReader(), Scheme: mgr.GetScheme(), Recorder: mgr.GetEventRecorderFor("shpyrd"),
		SystemNamespace: deps.SystemNamespace, Endpoint: Endpoint(deps.SystemNamespace), AdminEndpoint: AdminEndpoint(deps.SystemNamespace),
		AdminSecret: install.ObjectStorageAdminSecretName, CapacityBytes: capacity,
	}
	if deps.Var(install.VarGatewayBucket) != "" {
		r.UseGateway = true
		r.Connect = func(endpoint, admin, token string) (controller.BucketStore, error) {
			return objectstore.ConnectGateway(endpoint, admin, token)
		}
	}
	return r.SetupWithManager(mgr)
}

// Types: buckets are resources of the platform, not attachable to apps.
func (extension) Types() []ext.ResourceType {
	return []ext.ResourceType{{Kind: "ObjectBucket", Group: shpyrdv1.GroupVersion.Group, Version: shpyrdv1.GroupVersion.Version, Resource: "objectbuckets"}}
}

// Summary is what the cluster page shows.
type Summary struct {
	Backend    string       `json:"backend"`
	Endpoint   string       `json:"endpoint"`
	TotalBytes int64        `json:"totalBytes"`
	UsedBytes  int64        `json:"usedBytes"`
	MeasuredAt *time.Time   `json:"measuredAt,omitempty"`
	Buckets    []BucketView `json:"buckets"`
	Message    string       `json:"message,omitempty"`
}

// BucketView is one ObjectBucket with its usage.
type BucketView struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Bucket    string `json:"bucket"`
	Phase     string `json:"phase"`
	Message   string `json:"message,omitempty"`
	UsedBytes int64  `json:"usedBytes"`
	Objects   int64  `json:"objects"`
	Retention int32  `json:"retentionDays,omitempty"`
}

// Routes: the summary for platform administrators.
func (extension) Routes(r ext.Router, deps ext.Deps) error {
	r.Admin().GET("/cluster/object-storage", func(c *gin.Context) {
		sum, err := summarize(c.Request.Context(), deps)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, sum)
	})
	return nil
}

func summarize(ctx context.Context, deps ext.Deps) (*Summary, error) {
	out := &Summary{Endpoint: Endpoint(deps.SystemNamespace), Backend: "garage", Buckets: []BucketView{}}
	if deps.Var(install.VarGatewayBucket) != "" {
		out.Backend = "gateway"
	}
	var list shpyrdv1.ObjectBucketList
	if deps.Client != nil {
		if err := deps.Client.List(ctx, &list); err != nil {
			return nil, err
		}
	}
	for _, b := range list.Items {
		out.Buckets = append(out.Buckets, BucketView{Namespace: b.Namespace, Name: b.Name, Bucket: b.Status.Bucket, Phase: firstNonEmpty(b.Status.Phase, shpyrdv1.BucketPending), Message: b.Status.Message, UsedBytes: b.Status.UsedBytes, Objects: b.Status.Objects, Retention: b.Spec.RetentionDays})
	}
	sort.Slice(out.Buckets, func(i, j int) bool {
		if out.Buckets[i].Namespace != out.Buckets[j].Namespace {
			return out.Buckets[i].Namespace < out.Buckets[j].Namespace
		}
		return out.Buckets[i].Name < out.Buckets[j].Name
	})
	// Capacity straight from the store, as root.
	if deps.Kube != nil && deps.Kube.Kube != nil {
		sec, err := deps.Kube.Kube.CoreV1().Secrets(deps.SystemNamespace).Get(ctx, install.ObjectStorageAdminSecretName, metav1.GetOptions{})
		if err != nil {
			out.Message = "object storage is not installed yet"
			return out, nil
		}
		token := string(sec.Data["adminToken"])
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if out.Backend == "gateway" {
			gw, err := connectGateway(out.Endpoint, AdminEndpoint(deps.SystemNamespace), token)
			if err != nil {
				out.Message = err.Error()
				return out, nil
			}
			if err := summarizeGateway(cctx, gw, list.Items, out); err != nil {
				out.Message = err.Error()
			}
			return out, nil
		}
		store, err := connectGarage(out.Endpoint, AdminEndpoint(deps.SystemNamespace), token)
		if err != nil {
			out.Message = err.Error()
			return out, nil
		}
		if cap, err := store.Usage(cctx); err == nil {
			out.TotalBytes, out.UsedBytes = cap.TotalBytes, cap.UsedBytes
			if !cap.MeasuredAt.IsZero() {
				t := cap.MeasuredAt
				out.MeasuredAt = &t
			}
			for i := range out.Buckets {
				if u, ok := cap.Buckets[out.Buckets[i].Bucket]; ok {
					out.Buckets[i].UsedBytes, out.Buckets[i].Objects = u.Bytes, u.Objects
				}
			}
		} else {
			out.Message = err.Error()
		}
	}
	return out, nil
}

var (
	connectGarage  = objectstore.Connect
	connectGateway = objectstore.ConnectGateway
)

// The gateway has no disk to report and measuring usage there means listing
// objects, so the summary never asks it for the whole store. Each consumer's
// bucket is already measured by its controller (status.usedBytes); the
// platform's own buckets (registry, sources, platform backups) have no
// ObjectBucket and are measured by prefix, one at a time, no more often than
// platformUsageTTL across page loads.
const platformUsageTTL = 5 * time.Minute

var platformUsage struct {
	sync.Mutex
	buckets map[string]objectstore.Usage
	at      time.Time
}

type bucketMeasurer interface {
	BucketUsage(context.Context, string) (*objectstore.Capacity, error)
}

func summarizeGateway(ctx context.Context, gw bucketMeasurer, items []shpyrdv1.ObjectBucket, out *Summary) error {
	platform, at, err := measurePlatformBuckets(ctx, gw)
	if err != nil {
		return err
	}
	oldest := at
	for _, b := range items {
		out.UsedBytes += b.Status.UsedBytes
		if b.Status.MeasuredAt != nil && b.Status.MeasuredAt.Time.Before(oldest) {
			oldest = b.Status.MeasuredAt.Time
		}
	}
	for _, name := range install.PlatformGatewayBuckets {
		out.UsedBytes += platform[name].Bytes
	}
	out.MeasuredAt = &oldest
	return nil
}

func measurePlatformBuckets(ctx context.Context, gw bucketMeasurer) (map[string]objectstore.Usage, time.Time, error) {
	platformUsage.Lock()
	defer platformUsage.Unlock()
	if platformUsage.buckets != nil && time.Since(platformUsage.at) < platformUsageTTL {
		return platformUsage.buckets, platformUsage.at, nil
	}
	buckets := map[string]objectstore.Usage{}
	for _, name := range install.PlatformGatewayBuckets {
		cap, err := gw.BucketUsage(ctx, name)
		if err != nil {
			return nil, time.Time{}, err
		}
		buckets[name] = cap.Buckets[name]
	}
	platformUsage.buckets, platformUsage.at = buckets, time.Now()
	return buckets, platformUsage.at, nil
}

// CLI returns `shpyrd object-storage`.
func (extension) CLI(g ext.CLIGlobals) []*cobra.Command {
	// The platform's store: the operator's view.
	cmd := ext.ForOperator(&cobra.Command{
		Use:     "object-storage",
		Aliases: []string{"buckets"},
		Short:   "The platform's object store: buckets and their usage",
		Long: `The object-storage extension runs an S3-compatible store (Garage) in the
cluster. Extensions that need durable storage (Postgres backups, platform
backups) declare ObjectBucket resources and get a bucket with a credential
that opens only that bucket. This lists them.`,
	})
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List buckets with usage",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			_, c, err := resources.Connect(g)
			if err != nil {
				return err
			}
			var list shpyrdv1.ObjectBucketList
			if err := c.List(ctx, &list); err != nil {
				return err
			}
			if list.Items == nil {
				list.Items = []shpyrdv1.ObjectBucket{}
			}
			return ext.Print(g, cmd, list.Items, func(w io.Writer) {
				if len(list.Items) == 0 {
					fmt.Fprintln(w, "No buckets yet. Extensions create them when they need storage (Postgres backups, platform backups).")
					return
				}
				tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "NAMESPACE\tNAME\tBUCKET\tUSED\tOBJECTS\tRETENTION\tSTATUS")
				for _, b := range list.Items {
					retention := "-"
					if b.Spec.RetentionDays > 0 {
						retention = fmt.Sprintf("%dd", b.Spec.RetentionDays)
					}
					status := firstNonEmpty(b.Status.Phase, shpyrdv1.BucketPending)
					if b.Status.Message != "" {
						status += ": " + b.Status.Message
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n", b.Namespace, b.Name, b.Status.Bucket, humanBytes(b.Status.UsedBytes), b.Status.Objects, retention, status)
				}
				_ = tw.Flush()
			})
		},
	}
	cmd.AddCommand(list)
	return []*cobra.Command{cmd}
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
