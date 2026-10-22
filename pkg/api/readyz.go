package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/install"
)

// /api/readyz (#147, storage plan step 7): whether the platform can do what
// a deploy, a backup and a sleeping app need right now, one line per
// check. 200 with "ok" when every check passes, 503 with "degraded"
// otherwise; a component the platform does not run is "skipped", not a
// failure. /api/healthz stays the process's own liveness.

// ReadyCheck is one line of the readiness report.
type ReadyCheck struct {
	OK      bool   `json:"ok"`
	Skipped bool   `json:"skipped,omitempty"`
	Message string `json:"message,omitempty"`
}

// ReadyReport is GET /api/readyz.
type ReadyReport struct {
	Status string                `json:"status"`
	Checks map[string]ReadyCheck `json:"checks"`
}

// backupStaleAfter is how old the last platform backup may be: a daily
// job, two hours of slack.
const backupStaleAfter = 26 * time.Hour

func (s *Server) readyz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	report := s.readiness(ctx)
	code := http.StatusOK
	if report.Status != "ok" {
		code = http.StatusServiceUnavailable
	}
	c.JSON(code, report)
}

// readiness runs every check; the status is "ok" when none failed.
func (s *Server) readiness(ctx context.Context) ReadyReport {
	ns := s.deps().SystemNamespace
	checks := map[string]ReadyCheck{
		"sources":         s.readySources(ctx),
		"backup":          s.readyBackup(ctx, ns),
		"disks":           s.readyDisks(ctx),
		"registry":        s.readyDeployment(ctx, ns, "registry", false),
		"pg-gateway":      s.readyDeployment(ctx, ns, "pg-gateway", true),
		"keda-http":       s.readyDeployment(ctx, "keda", "keda-add-ons-http-interceptor", true),
		"object-gateway":  s.readyDeployment(ctx, ns, "object-storage", true),
		"shared-postgres": s.readySharedPostgres(ctx),
	}
	status := "ok"
	for _, ch := range checks {
		if !ch.OK && !ch.Skipped {
			status = "degraded"
		}
	}
	return ReadyReport{Status: status, Checks: checks}
}

// readySources: the store deploys read from answers (the bucket through
// the gateway on a cloud, the directory otherwise).
func (s *Server) readySources(ctx context.Context) ReadyCheck {
	if s.sources == nil {
		return ReadyCheck{OK: true, Skipped: true, Message: "no source store: deploys from source are off"}
	}
	if s.sources.Bucket != nil {
		if _, err := s.sources.Bucket.Usage(ctx); err != nil {
			return ReadyCheck{Message: "the sources bucket does not answer: " + err.Error()}
		}
		return ReadyCheck{OK: true, Message: "the sources bucket answers"}
	}
	if s.sources.Dir == "" {
		return ReadyCheck{OK: true, Skipped: true}
	}
	if info, err := os.Stat(s.sources.Dir); err != nil || !info.IsDir() {
		return ReadyCheck{Message: "the sources directory is missing: " + s.sources.Dir}
	}
	return ReadyCheck{OK: true, Message: "the sources directory is there"}
}

// readyBackup: the platform backup succeeded within a day and two hours.
func (s *Server) readyBackup(ctx context.Context, ns string) ReadyCheck {
	if s.kube == nil || s.kube.Kube == nil {
		return ReadyCheck{OK: true, Skipped: true}
	}
	cj, err := s.kube.Kube.BatchV1().CronJobs(ns).Get(ctx, "platform-backup", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ReadyCheck{OK: true, Skipped: true, Message: "no platform backup on this cluster"}
		}
		return ReadyCheck{Message: "the platform backup cannot be read: " + err.Error()}
	}
	if cj.Status.LastSuccessfulTime == nil {
		if time.Since(cj.CreationTimestamp.Time) < backupStaleAfter {
			return ReadyCheck{OK: true, Message: "the platform backup has not run yet (new)"}
		}
		return ReadyCheck{Message: "the platform backup never succeeded"}
	}
	if age := time.Since(cj.Status.LastSuccessfulTime.Time); age > backupStaleAfter {
		return ReadyCheck{Message: fmt.Sprintf("the last platform backup succeeded %s ago", age.Truncate(time.Hour))}
	}
	return ReadyCheck{OK: true, Message: "the platform backup succeeded " + time.Since(cj.Status.LastSuccessfulTime.Time).Truncate(time.Minute).String() + " ago"}
}

// readyDisks: the profile's storage class exists, so a disk can be made.
func (s *Server) readyDisks(ctx context.Context) ReadyCheck {
	class := s.vars(install.VarStorageClass)
	if class == "" || s.kube == nil || s.kube.Kube == nil {
		return ReadyCheck{OK: true, Skipped: true, Message: "no storage class set: the cluster's default is used"}
	}
	if _, err := s.kube.Kube.StorageV1().StorageClasses().Get(ctx, class, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return ReadyCheck{Message: "the storage class " + class + " does not exist"}
		}
		return ReadyCheck{Message: "the storage class cannot be read: " + err.Error()}
	}
	return ReadyCheck{OK: true, Message: "disks come from " + class}
}

// readyDeployment: a component's Deployment has a ready replica. optional
// says the component may be absent (an extension that is off).
func (s *Server) readyDeployment(ctx context.Context, ns, name string, optional bool) ReadyCheck {
	if s.kube == nil || s.kube.Kube == nil {
		return ReadyCheck{OK: true, Skipped: true}
	}
	d, err := s.kube.Kube.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) && optional {
			return ReadyCheck{OK: true, Skipped: true, Message: "not on this cluster"}
		}
		return ReadyCheck{Message: name + " cannot be read: " + err.Error()}
	}
	if d.Status.ReadyReplicas == 0 {
		return ReadyCheck{Message: fmt.Sprintf("%s has no ready instance (%d wanted)", name, ptrInt32(d.Spec.Replicas))}
	}
	return ReadyCheck{OK: true, Message: fmt.Sprintf("%d of %d ready", d.Status.ReadyReplicas, ptrInt32(d.Spec.Replicas))}
}

func ptrInt32(p *int32) int32 {
	if p == nil {
		return 1
	}
	return *p
}

// readySharedPostgres: every shared server has a ready instance (its
// primary reachable), the tenants' always-on promise.
func (s *Server) readySharedPostgres(ctx context.Context) ReadyCheck {
	if s.apps == nil {
		return ReadyCheck{OK: true, Skipped: true}
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(controller.CNPGClusterGVK.GroupVersion().WithKind("ClusterList"))
	if err := s.apps.List(ctx, list, client.InNamespace(controller.SharedPostgresNamespace)); err != nil {
		if apierrors.IsNotFound(err) || isNoKindError(err) {
			return ReadyCheck{OK: true, Skipped: true, Message: "no shared servers on this cluster"}
		}
		return ReadyCheck{Message: "the shared servers cannot be read: " + err.Error()}
	}
	if len(list.Items) == 0 {
		return ReadyCheck{OK: true, Skipped: true, Message: "no shared server yet"}
	}
	var down []string
	for _, cl := range list.Items {
		ready, _, _ := unstructured.NestedInt64(cl.Object, "status", "readyInstances")
		if ready < 1 {
			down = append(down, cl.GetName())
		}
	}
	sort.Strings(down)
	if len(down) > 0 {
		return ReadyCheck{Message: fmt.Sprintf("no ready instance on %v", down)}
	}
	return ReadyCheck{OK: true, Message: fmt.Sprintf("%d shared server(s) with a ready primary", len(list.Items))}
}

// isNoKindError says the cluster has no such kind (an operator not installed).
func isNoKindError(err error) bool {
	return err != nil && (apierrors.IsNotFound(err) || len(err.Error()) > 0 && (contains(err.Error(), "no matches for kind") || contains(err.Error(), "could not find the requested resource") || contains(err.Error(), "no kind is registered")))
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
