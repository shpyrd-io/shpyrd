package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

func TestMaintenanceIngressLifecycle(t *testing.T) {
	for _, access := range []string{shpyrdv1.AccessPublic, shpyrdv1.AccessAuthenticated, shpyrdv1.AccessIdentified} {
		t.Run(access, func(t *testing.T) {
			app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "app-demo", Annotations: map[string]string{shpyrdv1.AnnotationMaintenance: "preparing"}}, Spec: shpyrdv1.AppSpec{Image: "example.test/image:1", Access: access, Domains: []string{"custom.example.test"}}}
			r, c := newTestReconciler(t, app)
			r.Config.SystemNamespace = "shpyrd-system"
			for _, phase := range []string{"preparing", "paused", "starting"} {
				cur := &shpyrdv1.App{}
				if err := c.Get(context.Background(), client.ObjectKeyFromObject(app), cur); err != nil {
					t.Fatal(err)
				}
				cur.Annotations[shpyrdv1.AnnotationMaintenance] = phase
				if err := c.Update(context.Background(), cur); err != nil {
					t.Fatal(err)
				}
				runReconcile(t, r, cur)
				for _, name := range []string{ingressName(app), edgeName(app)} {
					ing := &networkingv1.Ingress{}
					if err := c.Get(context.Background(), types.NamespacedName{Namespace: app.Namespace, Name: name}, ing); err != nil {
						t.Fatal(err)
					}
					if ing.Annotations["nginx.ingress.kubernetes.io/rewrite-target"] != "/_shpyrd/maintenance" {
						t.Fatalf("%s %s lost maintenance", phase, name)
					}
					for _, key := range edgeAnnotationKeys {
						if ing.Annotations[key] != "" {
							t.Fatalf("%s kept authentication annotation %s", phase, key)
						}
					}
					for _, rule := range ing.Spec.Rules {
						for _, path := range rule.HTTP.Paths {
							if path.Backend.Service.Name != EdgeServiceName {
								t.Fatalf("%s still routes to application", phase)
							}
						}
					}
				}
			}
			cur := &shpyrdv1.App{}
			_ = c.Get(context.Background(), client.ObjectKeyFromObject(app), cur)
			delete(cur.Annotations, shpyrdv1.AnnotationMaintenance)
			if err := c.Update(context.Background(), cur); err != nil {
				t.Fatal(err)
			}
			runReconcile(t, r, cur)
			ing := &networkingv1.Ingress{}
			if err := c.Get(context.Background(), types.NamespacedName{Namespace: app.Namespace, Name: ingressName(app)}, ing); err != nil {
				t.Fatal(err)
			}
			if ing.Annotations["nginx.ingress.kubernetes.io/rewrite-target"] != "" || ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name == EdgeServiceName {
				t.Fatal("normal routing was not restored")
			}
			if access != shpyrdv1.AccessPublic && ing.Annotations["nginx.ingress.kubernetes.io/auth-url"] == "" {
				t.Fatal("authentication was not restored")
			}
		})
	}
}

func TestMaintenanceDrainsProcessesAndPreservesSuspendedJobs(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "app-demo", Annotations: map[string]string{shpyrdv1.AnnotationMaintenance: "preparing"}}, Spec: shpyrdv1.AppSpec{Image: "example.test/image:1"}}
	meta := func(name string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: name, Namespace: app.Namespace, Labels: map[string]string{shpyrdv1.LabelApp: app.Name}}
	}
	dep := &appsv1.Deployment{ObjectMeta: meta("demo-web"), Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](2)}}
	job := &batchv1.Job{ObjectMeta: meta("active"), Status: batchv1.JobStatus{Active: 1, Failed: 1}}
	cron := &batchv1.CronJob{ObjectMeta: meta("schedule")}
	already := &batchv1.CronJob{ObjectMeta: meta("already-stopped"), Spec: batchv1.CronJobSpec{Suspend: ptr.To(true)}}
	r, c := newTestReconciler(t, app, dep, job, cron, already)
	if _, err := r.reconcileMaintenance(ctx, app); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(dep), dep); err != nil {
		t.Fatal(err)
	}
	if *dep.Spec.Replicas != 2 {
		t.Fatal("writers stopped before HTTP maintenance was confirmed")
	}
	app.Annotations[shpyrdv1.AnnotationMaintenance] = "paused"
	if _, err := r.reconcileMaintenance(ctx, app); err != nil {
		t.Fatal(err)
	}
	_ = c.Get(ctx, client.ObjectKeyFromObject(dep), dep)
	if *dep.Spec.Replicas != 0 {
		t.Fatal("deployment not stopped")
	}
	_ = c.Get(ctx, client.ObjectKeyFromObject(job), job)
	if job.Spec.Suspend == nil || !*job.Spec.Suspend {
		t.Fatal("active job with prior failed attempt not stopped")
	}
	if err := r.resumeMaintenanceJobs(ctx, app); err != nil {
		t.Fatal(err)
	}
	_ = c.Get(ctx, client.ObjectKeyFromObject(job), job)
	if *job.Spec.Suspend {
		t.Fatal("job not resumed")
	}
	_ = c.Get(ctx, client.ObjectKeyFromObject(cron), cron)
	if *cron.Spec.Suspend {
		t.Fatal("cron not resumed")
	}
	_ = c.Get(ctx, client.ObjectKeyFromObject(already), already)
	if !*already.Spec.Suspend {
		t.Fatal("resumed a previously suspended job")
	}
}
