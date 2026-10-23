package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/scheduling"
)

const newMachineGrace = 5 * time.Minute

// unschedulableGrace is how long an instance may wait for room before its
// wait counts as a failure (#127): the autoscaler adds a machine in two to
// four minutes when it is going to.
const unschedulableGrace = 5 * time.Minute

// noMachineFits says, in words, that no machine the instance may run on
// is large enough for what it asks, whatever is free right now (#127):
// "web cannot start: it asks for 2 cores; no machine here has more than
// 1.83." Pinned to one machine (a disk on its node) the sentence names it.
// Empty when a machine could hold it, or when the machines cannot be read.
func (r *AppReconciler) noMachineFits(ctx context.Context, pod *corev1.Pod) string {
	if r.Kube == nil {
		return ""
	}
	opts := metav1.ListOptions{}
	if len(pod.Spec.NodeSelector) > 0 {
		opts.LabelSelector = labels.SelectorFromSet(pod.Spec.NodeSelector).String()
	}
	nodes, err := r.Kube.CoreV1().Nodes().List(ctx, opts)
	if err != nil {
		return ""
	}
	var cpu, mem resource.Quantity
	for _, c := range pod.Spec.Containers {
		cpu.Add(*c.Resources.Requests.Cpu())
		mem.Add(*c.Resources.Requests.Memory())
	}
	if cpu.IsZero() && mem.IsZero() {
		return ""
	}
	var bestCPU, bestMem resource.Quantity
	known := false
	for _, n := range nodes.Items {
		if n.Spec.Unschedulable {
			continue
		}
		c, m := n.Status.Allocatable.Cpu(), n.Status.Allocatable.Memory()
		if c.IsZero() {
			continue // a machine the cluster has not sized (tests, a node coming up)
		}
		known = true
		if c.Cmp(cpu) >= 0 && m.Cmp(mem) >= 0 {
			return ""
		}
		if c.Cmp(bestCPU) > 0 {
			bestCPU = *c
		}
		if m.Cmp(bestMem) > 0 {
			bestMem = *m
		}
	}
	if !known {
		return ""
	}
	process := firstNonEmpty(pod.Labels[shpyrdv1.LabelProcess], "the instance")
	asks, has := "", ""
	switch {
	case bestCPU.Cmp(cpu) < 0 && bestMem.Cmp(mem) < 0:
		asks, has = coreWords(cpu)+" and "+humanBytes(mem.Value()), coreWords(bestCPU)+" and "+humanBytes(bestMem.Value())
	case bestCPU.Cmp(cpu) < 0:
		asks, has = coreWords(cpu), coreWords(bestCPU)
	default:
		asks, has = humanBytes(mem.Value()), humanBytes(bestMem.Value())
	}
	if host := pod.Spec.NodeSelector[corev1.LabelHostname]; host != "" {
		return fmt.Sprintf("%s cannot start on %s, the machine that holds its disk: it asks for %s; that machine has %s. Choose a smaller size, or move the disk.", process, host, asks, has)
	}
	return fmt.Sprintf("%s cannot start: it asks for %s; no machine here has more than %s. Choose a smaller size.", process, asks, has)
}

// coreWords says a CPU quantity as a person would: "2 cores", "0.5 cores",
// "1.83 cores".
func coreWords(q resource.Quantity) string {
	f := q.AsApproximateFloat64()
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "1" {
		return "1 core"
	}
	return s + " cores"
}

// schedulingHopeless says an instance of a rollout will not be placed as
// asked (#127): no machine is large enough, or it has waited for room past
// the grace while the autoscaler refused to add a machine (or there is
// none to ask). The words are the reason the process fails with.
func (r *AppReconciler) schedulingHopeless(ctx context.Context, pod *corev1.Pod) string {
	condition := scheduling.Blocked(pod)
	if condition == nil || !scheduling.Capacity(condition.Message) {
		return ""
	}
	if words := r.noMachineFits(ctx, pod); words != "" {
		return words
	}
	if r.now().Sub(pod.CreationTimestamp.Time) < unschedulableGrace {
		return ""
	}
	var latest *corev1.Event
	for _, e := range r.instanceEvents(ctx, pod) {
		if e.Reason == "TriggeredScaleUp" || e.Reason == "NotTriggerScaleUp" {
			ev := e
			if latest == nil || eventObservedAt(&ev).After(eventObservedAt(latest)) {
				latest = &ev
			}
		}
	}
	if latest != nil && latest.Reason == "TriggeredScaleUp" {
		return "" // a machine is on its way
	}
	process := firstNonEmpty(pod.Labels[shpyrdv1.LabelProcess], "the instance")
	return fmt.Sprintf("%s cannot start: no machine has room for it and the cluster is not adding one (%s). Choose a smaller size, or ask the operator for capacity.", process, strings.TrimSpace(condition.Message))
}

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
	if condition := scheduling.Blocked(pod); condition != nil {
		e := latest("TriggeredScaleUp", "NotTriggerScaleUp")
		if e != nil && e.Reason == "TriggeredScaleUp" {
			return waiting("Waiting for cluster resources: a new machine is being added to the cluster (usually 2–4 minutes).", e), false
		}
		if e != nil && e.Reason == "NotTriggerScaleUp" && (strings.Contains(strings.ToLower(e.Message), "max node group size") || strings.Contains(strings.ToLower(e.Message), "maximum")) {
			return waiting("Waiting for cluster resources: the cluster is at its size limit. Ask the operator for more capacity.", e), false
		}
		if scheduling.Capacity(condition.Message) {
			if words := r.noMachineFits(ctx, pod); words != "" {
				return words, false // not a wait: it can never start as asked (#127)
			}
			return waiting("Waiting for cluster resources: not enough capacity to place this instance. Ask the operator to add capacity or review its resource requests.", latest("FailedScheduling")), false
		}
		return waiting("Waiting for scheduling: no available machine can run this instance yet.", latest("FailedScheduling")), false
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
