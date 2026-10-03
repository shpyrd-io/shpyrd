package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

const maintenanceSuspended = "shpyrd.io/maintenance-suspended"

func maintenance(app *shpyrdv1.App) bool {
	return app.Annotations[shpyrdv1.AnnotationMaintenance] != ""
}

// Keep the same HTTP hosts and TLS while replacing every route, including
// login routes, with a 503 responder. Authentication redirects must not
// acknowledge or swallow callbacks during a maintenance window.
func maintenanceIngress(app *shpyrdv1.App, ing *networkingv1.Ingress) {
	if !maintenance(app) {
		delete(ing.Annotations, "nginx.ingress.kubernetes.io/rewrite-target")
		return
	}
	for _, key := range edgeAnnotationKeys {
		delete(ing.Annotations, key)
	}
	ing.Annotations["nginx.ingress.kubernetes.io/rewrite-target"] = "/_shpyrd/maintenance"
	for i := range ing.Spec.Rules {
		if ing.Spec.Rules[i].HTTP == nil {
			continue
		}
		for j := range ing.Spec.Rules[i].HTTP.Paths {
			ing.Spec.Rules[i].HTTP.Paths[j].Backend.Service = &networkingv1.IngressServiceBackend{Name: EdgeServiceName, Port: networkingv1.ServiceBackendPort{Name: "http"}}
		}
	}
}

func (r *AppReconciler) reconcileMaintenance(ctx context.Context, app *shpyrdv1.App) (outcome, error) {
	// Preparing changes HTTP first. The API confirms the maintenance response
	// before advancing to paused and draining the writers.
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: EdgeServiceName, Namespace: app.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		r.Config.mutateEdgeService(app, svc)
		return controllerutil.SetControllerReference(app, svc, r.Scheme)
	}); err != nil {
		return outcome{}, err
	}
	for _, edge := range []bool{false, true} {
		name := ingressName(app)
		if edge {
			name = edgeName(app)
		}
		ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: app.Namespace}}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, ing, func() error {
			if edge {
				r.Config.mutateEdgeIngress(app, ing)
			} else {
				r.Config.mutateIngress(app, ing, false)
			}
			return controllerutil.SetControllerReference(app, ing, r.Scheme)
		}); err != nil {
			return outcome{}, err
		}
	}
	if app.Annotations[shpyrdv1.AnnotationMaintenance] != "preparing" {
		if err := r.stopProjectProcesses(ctx, app); err != nil {
			return outcome{}, err
		}
	}
	app.Status.Phase = "Maintenance"
	app.Status.Message = "project maintenance: requests receive HTTP 503; processing resumes after recovery completes"
	return requeue(2 * time.Second), nil
}

func (r *AppReconciler) stopProjectProcesses(ctx context.Context, app *shpyrdv1.App) error {
	if err := r.deleteSleepObjects(ctx, app); err != nil {
		return err
	}
	var runs corev1.PodList
	if err := r.List(ctx, &runs, client.InNamespace(app.Namespace), client.MatchingLabels{shpyrdv1.LabelApp: app.Name, shpyrdv1.LabelProcess: RunProcess}); err != nil {
		return err
	}
	for i := range runs.Items {
		p := &runs.Items[i]
		if p.Status.Phase != corev1.PodSucceeded && p.Status.Phase != corev1.PodFailed {
			if err := r.deleteIfExists(ctx, p); err != nil {
				return err
			}
		}
	}
	var deployments appsv1.DeploymentList
	if err := r.List(ctx, &deployments, client.InNamespace(app.Namespace), client.MatchingLabels{shpyrdv1.LabelApp: app.Name}); err != nil {
		return err
	}
	for i := range deployments.Items {
		d := &deployments.Items[i]
		if d.Spec.Replicas != nil && *d.Spec.Replicas == 0 {
			continue
		}
		before := d.DeepCopy()
		d.Spec.Replicas = ptr.To(int32(0))
		if err := r.Patch(ctx, d, client.MergeFrom(before)); err != nil {
			return err
		}
	}
	var jobs batchv1.CronJobList
	if err := r.List(ctx, &jobs, client.InNamespace(app.Namespace), client.MatchingLabels{shpyrdv1.LabelApp: app.Name}); err != nil {
		return err
	}
	for i := range jobs.Items {
		j := &jobs.Items[i]
		if j.Spec.Suspend != nil && *j.Spec.Suspend {
			continue
		}
		before := j.DeepCopy()
		j.Annotations = mergeMaps(j.Annotations, map[string]string{maintenanceSuspended: "true"})
		j.Spec.Suspend = ptr.To(true)
		if err := r.Patch(ctx, j, client.MergeFrom(before)); err != nil {
			return err
		}
	}
	// Suspending active Jobs stops their pods as well as future completions.
	var active batchv1.JobList
	if err := r.List(ctx, &active, client.InNamespace(app.Namespace), client.MatchingLabels{shpyrdv1.LabelApp: app.Name}); err != nil {
		return err
	}
	for i := range active.Items {
		j := &active.Items[i]
		finished := false
		for _, c := range j.Status.Conditions {
			if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
				finished = true
			}
		}
		if finished || (j.Spec.Suspend != nil && *j.Spec.Suspend) {
			continue
		}
		before := j.DeepCopy()
		j.Annotations = mergeMaps(j.Annotations, map[string]string{maintenanceSuspended: "true"})
		j.Spec.Suspend = ptr.To(true)
		if err := r.Patch(ctx, j, client.MergeFrom(before)); err != nil {
			return fmt.Errorf("pause job: %w", err)
		}
	}
	return nil
}

func (r *AppReconciler) resumeMaintenanceJobs(ctx context.Context, app *shpyrdv1.App) error {
	var cronjobs batchv1.CronJobList
	if err := r.List(ctx, &cronjobs, client.InNamespace(app.Namespace), client.MatchingLabels{shpyrdv1.LabelApp: app.Name}); err != nil {
		return err
	}
	for i := range cronjobs.Items {
		j := &cronjobs.Items[i]
		if j.Annotations[maintenanceSuspended] != "true" {
			continue
		}
		before := j.DeepCopy()
		delete(j.Annotations, maintenanceSuspended)
		j.Spec.Suspend = ptr.To(false)
		if err := r.Patch(ctx, j, client.MergeFrom(before)); err != nil {
			return err
		}
	}
	var jobs batchv1.JobList
	if err := r.List(ctx, &jobs, client.InNamespace(app.Namespace), client.MatchingLabels{shpyrdv1.LabelApp: app.Name}); err != nil {
		return err
	}
	for i := range jobs.Items {
		j := &jobs.Items[i]
		if j.Annotations[maintenanceSuspended] != "true" {
			continue
		}
		before := j.DeepCopy()
		delete(j.Annotations, maintenanceSuspended)
		j.Spec.Suspend = ptr.To(false)
		if err := r.Patch(ctx, j, client.MergeFrom(before)); err != nil {
			return err
		}
	}
	return nil
}
