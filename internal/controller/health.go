package controller

import (
	"context"
	"fmt"
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
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil || !metav1.IsControlledBy(&pod, current) {
			continue
		}
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Name != "app" {
				continue
			}
			var why string
			switch {
			case cs.State.Waiting != nil && failingReasons[cs.State.Waiting.Reason]:
				why = cs.State.Waiting.Reason
				if m := cs.State.Waiting.Message; strings.Contains(m, "runAsNonRoot") {
					// Pod security: processes run as non-root (RFC-0008), and
					// the kubelet can only verify a numeric user.
					switch {
					case strings.Contains(m, "non-numeric user"):
						why = "the image sets a named USER, which Kubernetes cannot verify as non-root: use a numeric USER in the Dockerfile (for example USER 1000)"
					default:
						why = "the image runs as root: shpyrd runs processes as a non-root user (add USER 1000 to the Dockerfile)"
					}
					break
				}
				if t := cs.LastTerminationState.Terminated; t != nil {
					why += fmt.Sprintf(" (exit %d)", t.ExitCode)
					if m := shortMessage(t.Message); m != "" {
						why += ": " + m
					}
				} else if m := shortMessage(cs.State.Waiting.Message); m != "" && !strings.HasPrefix(m, "back-off") {
					why += ": " + m
				}
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
