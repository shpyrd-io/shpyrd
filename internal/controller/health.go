package controller

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Container waiting reasons that mean an instance will not come up on its
// own. Kubernetes keeps retrying; the user needs to know why.
var failingReasons = map[string]bool{
	"CrashLoopBackOff":           true,
	"ImagePullBackOff":           true,
	"ErrImagePull":               true,
	"CreateContainerConfigError": true,
	"CreateContainerError":       true,
	"InvalidImageName":           true,
	"RunContainerError":          true,
}

// processHealth inspects the instances of a process's current pod template,
// those of current (the Deployment's ReplicaSet for it, nil before the
// Deployment controller has made it), and reports how many are failing and a
// human readable reason for the first one. The pods of earlier templates are
// left out: during a rollout the previous release's instances stay until the
// new ones are ready, and their crash loop is not the new release's (#91).
func (r *AppReconciler) processHealth(ctx context.Context, current *appsv1.ReplicaSet) (failing int32, reason string) {
	if current == nil || current.Spec.Selector == nil {
		return 0, ""
	}
	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(current.Namespace), client.MatchingLabels(current.Spec.Selector.MatchLabels)); err != nil {
		return 0, ""
	}
	waiting := ""
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil || !metav1.IsControlledBy(&pod, current) {
			continue
		}
		if pod.Status.Phase == corev1.PodFailed && pod.Status.Reason == "Evicted" {
			// The node threw the instance off (disk or memory pressure) and
			// the Deployment controller has already replaced it; the dead
			// pod stays until garbage collection. Its end is the node's,
			// not the release's (#94: ten of them read as ten failing).
			continue
		}
		pending, transientPull := r.podWaiting(ctx, &pod)
		if waiting == "" {
			waiting = pending
		}
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Name != "app" {
				continue
			}
			var why string
			switch {
			case cs.State.Waiting != nil && failingReasons[cs.State.Waiting.Reason] && !transientPull:
				why = waitingFailure(cs)
			case cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 && pod.Status.Phase == corev1.PodFailed:
				why = fmt.Sprintf("exited with code %d", cs.State.Terminated.ExitCode)
				if m := shortMessage(cs.State.Terminated.Message); m != "" {
					why += ": " + m
				}
			}
			// A running container whose readiness probe keeps failing is
			// also "failing" from the user's perspective: it never serves.
			// Until its startup budget has passed it is merely starting.
			if why == "" && cs.State.Running != nil && !cs.Ready {
				if unready := r.now().Sub(cs.State.Running.StartedAt.Time); unready > startupBudget(pod) {
					why = probeFailureReason(pod, unready)
					if detail := r.recentProbeFailure(ctx, pod, cs, false); detail != "" {
						why = detail
					}
				}
			}
			if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
				if ended := cs.LastTerminationState.Terminated; ended != nil && ended.ExitCode == 143 && ended.Reason != "OOMKilled" {
					if detail := r.recentProbeFailure(ctx, pod, cs, true); detail != "" {
						why = detail
					}
				}
			}
			if why != "" {
				failing++
				if reason == "" {
					reason = why
				}
			}
		}
	}
	if failing == 0 {
		return 0, waiting
	}
	return failing, reason
}

// currentReplicaSet is the ReplicaSet the Deployment controller made for d's
// pod template, or nil while it has not made it yet.
func (r *AppReconciler) currentReplicaSet(ctx context.Context, d *appsv1.Deployment) (*appsv1.ReplicaSet, error) {
	if d.Spec.Selector == nil {
		return nil, nil
	}
	var sets appsv1.ReplicaSetList
	if err := r.List(ctx, &sets, client.InNamespace(d.Namespace), client.MatchingLabels(d.Spec.Selector.MatchLabels)); err != nil {
		return nil, err
	}
	for i := range sets.Items {
		rs := &sets.Items[i]
		if metav1.IsControlledBy(rs, d) && sameTemplate(rs.Spec.Template, d.Spec.Template) {
			return rs, nil
		}
	}
	return nil, nil
}

// sameTemplate compares pod templates as the Deployment controller does,
// ignoring the pod-template-hash label it adds to a ReplicaSet's template.
func sameTemplate(a, b corev1.PodTemplateSpec) bool {
	a, b = *a.DeepCopy(), *b.DeepCopy()
	delete(a.Labels, appsv1.DefaultDeploymentUniqueLabelKey)
	delete(b.Labels, appsv1.DefaultDeploymentUniqueLabelKey)
	return apiequality.Semantic.DeepEqual(a, b)
}

// shortMessage trims runtime noise from container error messages.
func shortMessage(m string) string {
	m = strings.TrimSpace(m)
	// containerd/runc prefix chains end with the interesting part.
	if i := strings.LastIndex(m, "error during container init: "); i >= 0 {
		m = m[i+len("error during container init: "):]
	}
	if i := strings.LastIndex(m, "OCI runtime create failed: "); i >= 0 {
		m = m[i+len("OCI runtime create failed: "):]
	}
	if len(m) > 160 {
		m = m[:160] + "..."
	}
	return m
}

// now is swappable for tests.
func (r *AppReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// minStartupBudget is how long a freshly started instance may stay unready
// before it counts as failing when its probes allow less than that.
const minStartupBudget = 30 * time.Second

// startupBudget is how long the app container of pod may legitimately be
// running without being ready: the whole startup probe window plus one round
// of readiness failures, as configured on the pod (RFC-0019).
func startupBudget(pod corev1.Pod) time.Duration {
	budget := minStartupBudget
	for _, c := range pod.Spec.Containers {
		if c.Name != "app" {
			continue
		}
		var secs int32
		for _, p := range []*corev1.Probe{c.StartupProbe, c.ReadinessProbe} {
			if p == nil {
				continue
			}
			period, threshold := p.PeriodSeconds, p.FailureThreshold
			if period == 0 {
				period = 10 // kubelet default
			}
			if threshold == 0 {
				threshold = 3
			}
			secs += p.InitialDelaySeconds + period*threshold
		}
		if d := time.Duration(secs) * time.Second; d > budget {
			budget = d
		}
	}
	return budget
}

// probeFailureReason explains a container that has been running but unready
// for longer than its startup budget, quoting the pod's condition when it
// says more than "containers with unready status".
func probeFailureReason(pod corev1.Pod, unready time.Duration) string {
	why := fmt.Sprintf("not ready after %s: readiness probe failing", unready.Truncate(time.Second))
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.ContainersReady && cond.Status == corev1.ConditionFalse && cond.Message != "" &&
			!strings.HasPrefix(cond.Message, "containers with unready status") {
			return why + ": " + shortMessage(cond.Message)
		}
	}
	return why
}

// Recent events supplement container status; they are not proof that an
// arbitrary exit was caused by a probe. Only SIGTERM crash loops use the
// previous run, and readiness failures cannot explain a restart.
func (r *AppReconciler) recentProbeFailure(ctx context.Context, pod corev1.Pod, cs corev1.ContainerStatus, restarting bool) string {
	if r.Kube == nil || pod.UID == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	events, err := r.Kube.CoreV1().Events(pod.Namespace).List(ctx, metav1.ListOptions{FieldSelector: "involvedObject.name=" + pod.Name + ",reason=Unhealthy"})
	if err != nil {
		return ""
	}
	var latest time.Time
	var message string
	for _, event := range events.Items {
		if event.Reason != "Unhealthy" || event.InvolvedObject.UID != pod.UID || event.InvolvedObject.Name != pod.Name || event.InvolvedObject.Kind != "Pod" || event.InvolvedObject.FieldPath != "spec.containers{app}" {
			continue
		}
		kind := strings.SplitN(event.Message, " probe failed:", 2)
		if len(kind) != 2 || (kind[0] != "Startup" && kind[0] != "Readiness" && kind[0] != "Liveness") {
			continue
		}
		at := event.LastTimestamp.Time
		if event.EventTime.Time.After(at) {
			at = event.EventTime.Time
		}
		if event.Series != nil && event.Series.LastObservedTime.Time.After(at) {
			at = event.Series.LastObservedTime.Time
		}
		if at.IsZero() || at.Before(r.now().Add(-5*time.Minute)) || at.After(r.now().Add(5*time.Second)) {
			continue
		}
		if restarting {
			ended := cs.LastTerminationState.Terminated
			if kind[0] == "Readiness" || ended.FinishedAt.IsZero() || at.Before(ended.StartedAt.Time) || at.Before(ended.FinishedAt.Add(-2*time.Minute)) || at.After(ended.FinishedAt.Add(5*time.Second)) {
				continue
			}
		} else if cs.State.Running == nil || at.Before(cs.State.Running.StartedAt.Time) {
			continue
		}
		if at.After(latest) {
			latest = at
			message = describeProbeFailure(pod, kind[0], strings.TrimSpace(kind[1]))
		}
	}
	return message
}

var probeHTTPStatus = regexp.MustCompile(`statuscode: (\d{3})`)

func describeProbeFailure(pod corev1.Pod, kind, detail string) string {
	var probe *corev1.Probe
	for _, c := range pod.Spec.Containers {
		if c.Name != "app" {
			continue
		}
		switch kind {
		case "Startup":
			probe = c.StartupProbe
		case "Readiness":
			probe = c.ReadinessProbe
		case "Liveness":
			probe = c.LivenessProbe
		}
	}
	why := strings.ToLower(kind) + " health check"
	if probe != nil && probe.HTTPGet != nil {
		why += fmt.Sprintf(" on %q", probe.HTTPGet.Path)
	}
	why += " is failing: "
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "redirect") || (strings.Contains(lower, "/.shpyrd/signin") && strings.Contains(lower, "rd=")):
		why += "the request could not complete after a redirect. "
	case probeHTTPStatus.MatchString(detail):
		code := probeHTTPStatus.FindStringSubmatch(detail)[1]
		switch code {
		case "401", "403":
			why += "the process refused the request (HTTP " + code + "). "
		default:
			why += "the process answered HTTP " + code + ". "
		}
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out") || strings.Contains(lower, "deadline exceeded"):
		why += "the process did not answer before the timeout. "
	default:
		why += shortMessage(detail) + ". "
	}
	if probe != nil && probe.HTTPGet != nil {
		why += "The check reaches the process directly, without edge sign-in, cookies or identity headers. Make healthCheck.path answer a plain GET with 2xx outside the app's sign-in gate, or use healthCheck.tcp: true. "
	}
	// Keep the original observation for failures whose useful detail is not
	// captured by the explanation above (connection errors, redirect targets).
	if strings.Contains(lower, "redirect") || strings.Contains(lower, "signin") {
		why += "Probe: " + shortMessage(detail)
	}
	return strings.TrimSpace(why)
}

func waitingFailure(cs corev1.ContainerStatus) string {
	why := cs.State.Waiting.Reason
	if m := cs.State.Waiting.Message; strings.Contains(m, "runAsNonRoot") {
		// Pod security: processes run as non-root (RFC-0008), and
		// the kubelet can only verify a numeric user.
		switch {
		case strings.Contains(m, "non-numeric user"):
			why = "the image sets a named USER, which Kubernetes cannot verify as non-root: use a numeric USER in the Dockerfile (for example USER 1000)"
		default:
			why = "the image runs as root: shpyrd runs processes as a non-root user (add USER 1000 to the Dockerfile)"
		}
		return why
	}
	if t := cs.LastTerminationState.Terminated; t != nil {
		why += fmt.Sprintf(" (exit %d)", t.ExitCode)
		if m := shortMessage(t.Message); m != "" {
			why += ": " + m
		}
	} else if m := shortMessage(cs.State.Waiting.Message); m != "" && !strings.HasPrefix(m, "back-off") {
		why += ": " + m
	}
	return why
}
