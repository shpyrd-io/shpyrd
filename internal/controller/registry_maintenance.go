package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// Builds during the registry's garbage collection (shpyrd-spec-002). The
// collector switches the registry to read-only, and a push then fails. The
// controller does not guess that from a build's failure text or from the
// registry's configuration, which anyone can point at the read-only copy by
// hand (shpyrd-les-001): the collector writes a mark on the registry's
// Deployment, in the update that swaps the configuration, and removes it in
// the update that swaps it back. The registry is "in maintenance" while that
// mark is there and young.

// RegistryMaintenanceBound is the longest the registry counts as in
// maintenance after the collector's mark. A collection is bounded at one
// hour, and each switch waits for the registry's rollout (five minutes per
// replica, two replicas at most): a mark older than this is a collection
// that died, or a registry left read-only, which is not maintenance. Builds
// then fail as they do for any other cause.
const RegistryMaintenanceBound = 90 * time.Minute

// maintenanceRerunGrace is how long a Build the controller asked kpack to
// build again may stay the Image's latest before the controller gives up
// waiting for the new one and lets the failure stand.
const maintenanceRerunGrace = 5 * time.Minute

// What a person reads while a build waits for, or runs again after, the
// registry's maintenance. None names an object of the platform.
const (
	maintenanceHoldMessage    = "The platform is in scheduled maintenance of its image storage, which takes under an hour. This build starts by itself when the maintenance ends; nothing needs doing."
	maintenanceRerunMessage   = "The build could not save its image because the platform is in scheduled maintenance of its image storage. It runs again by itself when the maintenance ends; nothing needs doing."
	maintenanceRerunStarted   = "The platform's scheduled maintenance is over: the build is running again by itself."
	maintenanceRerunEventNote = "building the same source again (the registry's maintenance ended)"
)

// markRegistryReadOnly writes (on) or removes (off) the mark of the
// collector's switch on the registry's Deployment, and says whether that
// changed it. Only RegistryGC.setReadOnly calls it.
func markRegistryReadOnly(dep *appsv1.Deployment, on bool, now time.Time) bool {
	if !on {
		if _, ok := dep.Annotations[shpyrdv1.AnnotationRegistryReadOnlySince]; !ok {
			return false
		}
		delete(dep.Annotations, shpyrdv1.AnnotationRegistryReadOnlySince)
		return true
	}
	if dep.Annotations == nil {
		dep.Annotations = map[string]string{}
	}
	dep.Annotations[shpyrdv1.AnnotationRegistryReadOnlySince] = now.UTC().Format(time.RFC3339)
	return true
}

// registryInMaintenance says the Deployment carries the collector's mark,
// younger than the bound, and runs the read-only configuration.
func registryInMaintenance(dep *appsv1.Deployment, now time.Time) bool {
	since, err := time.Parse(time.RFC3339, dep.Annotations[shpyrdv1.AnnotationRegistryReadOnlySince])
	if err != nil || now.Sub(since) >= RegistryMaintenanceBound {
		return false
	}
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == "config" && v.ConfigMap != nil && v.ConfigMap.Name == registryReadOnlyConfig {
			return true
		}
	}
	return false
}

// registryInMaintenance reads the registry's Deployment, past the cache so
// that a switch just made is seen, and says it is in maintenance. A registry
// that is not there, an external one, or one that cannot be read is not.
func (r *AppReconciler) registryInMaintenance(ctx context.Context) bool {
	if r.Config.RegistrySecret != "" {
		return false
	}
	var reader client.Reader = r.Client
	if r.APIReader != nil {
		reader = r.APIReader
	}
	dep := &appsv1.Deployment{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: r.Config.SystemNamespace, Name: registryDeploymentName}, dep); err != nil {
		return false
	}
	return registryInMaintenance(dep, r.now())
}

// heldBuild is the state of a build the controller does not start while the
// registry is in maintenance: what runs keeps running.
func heldBuild(app *shpyrdv1.App, image string) buildState {
	return buildState{
		Ready: "Unknown", Reason: shpyrdv1.ReasonRegistryMaintenance, Message: maintenanceHoldMessage,
		LatestImage: firstNonEmpty(image, app.Status.Image), LatestBuild: app.Status.LatestBuild,
	}
}

// failedWritingToRegistry says a kpack step failed for want of writing to
// the registry: the prepare step finding it may not write the image, or a
// step that saves the image.
func failedWritingToRegistry(step, text string) bool {
	switch step {
	case "prepare":
		return registryRefusedWrite.MatchString(text)
	case "export", "completion":
		return true
	}
	return false
}

// rerunActive says the Build is held for a new build: waiting for the
// maintenance to end, or asked to build again less than the grace ago.
func rerunActive(b *unstructured.Unstructured, now time.Time) bool {
	v := b.GetAnnotations()[shpyrdv1.AnnotationBuildRerun]
	if v == "" {
		return false
	}
	if v == shpyrdv1.BuildRerunWaiting {
		return true
	}
	asked, err := time.Parse(time.RFC3339, v)
	return err == nil && now.Sub(asked) < maintenanceRerunGrace
}

// holdFailedBuild decides what a failed kpack Build marked for a new build
// means for the project. While the registry is in maintenance the project
// waits; once it is writable the Build is annotated, once, so that kpack
// builds the same source again. false when the Build is not held.
func (r *AppReconciler) holdFailedBuild(ctx context.Context, app *shpyrdv1.App, b *unstructured.Unstructured, build buildState) (buildState, bool, error) {
	if !rerunActive(b, r.now()) {
		return build, false, nil
	}
	build.Ready = "Unknown"
	if b.GetAnnotations()[shpyrdv1.AnnotationBuildRerun] == shpyrdv1.BuildRerunWaiting {
		if r.registryInMaintenance(ctx) {
			build.Reason, build.Message = shpyrdv1.ReasonRegistryMaintenance, maintenanceRerunMessage
			return build, true, nil
		}
		patch := client.MergeFrom(b.DeepCopy())
		b.SetAnnotations(mergeMaps(b.GetAnnotations(), map[string]string{
			kpackBuildNeededAnnotation:    "true",
			shpyrdv1.AnnotationBuildRerun: r.now().UTC().Format(time.RFC3339),
		}))
		if err := r.Patch(ctx, b, patch); err != nil {
			return build, false, fmt.Errorf("build again after the registry's maintenance: %w", err)
		}
		r.Recorder.Event(app, corev1.EventTypeNormal, "BuildRequested", maintenanceRerunEventNote)
	}
	build.Reason, build.Message = "Building", maintenanceRerunStarted
	return build, true, nil
}

// markForRerun marks a Build that failed writing to the registry while the
// registry is in maintenance, so the controller builds it again after.
// Written once: a Build already marked, whatever became of it, is not marked
// again.
func (r *AppReconciler) markForRerun(ctx context.Context, b *unstructured.Unstructured, step, text string) bool {
	if _, ok := b.GetAnnotations()[shpyrdv1.AnnotationBuildRerun]; ok || !failedWritingToRegistry(step, text) || !r.registryInMaintenance(ctx) {
		return false
	}
	patch := client.MergeFrom(b.DeepCopy())
	b.SetAnnotations(mergeMaps(b.GetAnnotations(), map[string]string{shpyrdv1.AnnotationBuildRerun: shpyrdv1.BuildRerunWaiting}))
	if err := r.Patch(ctx, b, patch); err != nil {
		return false // read again next time
	}
	return true
}
