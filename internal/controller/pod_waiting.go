package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const newMachineGrace = 5 * time.Minute

// podWaiting explains an instance that has not started its app container.
// transientPull only exempts the known registry-trust race on a new machine
// from the normal image-pull failure handling, and only during the grace.
func (r *AppReconciler) podWaiting(ctx context.Context, pod *corev1.Pod) (message string, transientPull bool) {
	if pod.DeletionTimestamp != nil || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return "", false
	}
	var app *corev1.ContainerStatus
	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Name == "app" {
			app = &pod.Status.ContainerStatuses[i]
			break
		}
	}
	if app != nil && (app.State.Running != nil || app.State.Terminated != nil) {
		return "", false
	}
	if app == nil && pod.Status.Phase == corev1.PodRunning {
		return "", false
	}
	events := r.instanceEvents(ctx, pod)
	latest := func(reasons ...string) *corev1.Event {
		var found *corev1.Event
		for i := range events {
			e := &events[i]
			for _, reason := range reasons {
				if e.Reason == reason && (found == nil || eventObservedAt(e).After(eventObservedAt(found))) {
					found = e
				}
			}
		}
		return found
	}
	waiting := func(text string, event *corev1.Event) string {
		since := pod.CreationTimestamp.Time
		if event != nil {
			at := event.FirstTimestamp.Time
			if at.IsZero() {
				at = event.EventTime.Time
			}
			if at.IsZero() {
				at = event.CreationTimestamp.Time
			}
			if at.After(since) {
				since = at
			}
		}
		return r.waitingMessage(text, since)
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type != corev1.PodScheduled || condition.Status != corev1.ConditionFalse {
			continue
		}
		e := latest("TriggeredScaleUp", "NotTriggerScaleUp")
		if e != nil && e.Reason == "TriggeredScaleUp" {
			return waiting("Waiting for room: a new machine is being added to the cluster (usually 2–4 minutes).", e), false
		}
		if e != nil && e.Reason == "NotTriggerScaleUp" && (strings.Contains(strings.ToLower(e.Message), "max node group size") || strings.Contains(strings.ToLower(e.Message), "maximum")) {
			return waiting("Waiting for room: the cluster is at its size limit. Ask the operator for more capacity.", e), false
		}
		return waiting("Waiting for room: no available machine can run this instance yet.", latest("FailedScheduling")), false
	}
	newMachine := func() bool {
		if r.Kube == nil || pod.Spec.NodeName == "" {
			return false
		}
		node, err := r.Kube.CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
		return err == nil && !node.CreationTimestamp.IsZero() && r.now().Sub(node.CreationTimestamp.Time) >= 0 && r.now().Sub(node.CreationTimestamp.Time) < newMachineGrace
	}
	if app != nil && app.State.Waiting != nil {
		state := app.State.Waiting
		platformImage := false
		for _, c := range pod.Spec.Containers {
			if c.Name == "app" && r.Config.RegistryHost != "" && strings.HasPrefix(c.Image, r.Config.RegistryHost+"/") {
				platformImage = true
			}
		}
		if (state.Reason == "ErrImagePull" || state.Reason == "ImagePullBackOff") && platformImage && newMachine() {
			certificateError := strings.Contains(state.Message, "x509: certificate signed by unknown authority")
			if e := latest("Failed"); e != nil && strings.Contains(e.Message, "x509: certificate signed by unknown authority") {
				certificateError = true
			}
			if certificateError {
				return waiting("Waiting for the new machine to trust the platform's registry (retrying).", latest("Failed")), true
			}
		}
		if failingReasons[state.Reason] {
			return "", false
		}
	}
	if e := latest("Pulling", "Pulled", "Created", "FailedCreatePodSandBox", "SandboxChanged"); e != nil {
		switch e.Reason {
		case "Pulling":
			text := "Downloading the app's image"
			if field := e.InvolvedObject.FieldPath; field != "" && field != "spec.containers{app}" {
				return waiting("Downloading an image needed to start the instance.", e), false
			}
			for _, c := range pod.Spec.Containers {
				if c.Name == "app" {
					if meta := r.metadataOf(ctx, c.Image); meta != nil && meta.ImageSize >= 500_000_000 {
						text += fmt.Sprintf(" (%.1f GB)", float64(meta.ImageSize)/1e9)
					}
				}
			}
			return waiting(text+".", e), false
		case "FailedCreatePodSandBox":
			if newMachine() {
				return waiting("Starting on a new machine: waiting for its network.", e), false
			}
			return waiting("Waiting for the instance's network to be ready.", e), false
		}
	}
	return waiting("Starting the instance.", nil), false
}

func (r *AppReconciler) waitingMessage(text string, since time.Time) string {
	if since.IsZero() {
		return text
	}
	elapsed := r.now().Sub(since)
	if elapsed < 0 {
		elapsed = 0
	}
	return fmt.Sprintf("%s Waiting %s.", text, elapsed.Truncate(time.Second))
}

func (r *AppReconciler) instanceEvents(ctx context.Context, pod *corev1.Pod) []corev1.Event {
	if r.Kube == nil {
		return nil
	}
	list, err := r.Kube.CoreV1().Events(pod.Namespace).List(ctx, metav1.ListOptions{FieldSelector: "involvedObject.name=" + pod.Name})
	if err != nil {
		return nil
	}
	var events []corev1.Event
	for _, e := range list.Items {
		if e.InvolvedObject.Name == pod.Name && e.InvolvedObject.UID == pod.UID {
			events = append(events, e)
		}
	}
	return events
}

func eventObservedAt(e *corev1.Event) time.Time {
	at := e.CreationTimestamp.Time
	for _, candidate := range []time.Time{e.FirstTimestamp.Time, e.LastTimestamp.Time, e.EventTime.Time} {
		if candidate.After(at) {
			at = candidate
		}
	}
	if e.Series != nil && e.Series.LastObservedTime.After(at) {
		at = e.Series.LastObservedTime.Time
	}
	return at
}
