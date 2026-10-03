package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

const releaseQuietAfter = 5 * time.Minute

// releaseDiagnostic reads the current attempt only. Job conditions can lag a
// container's death; waiting for them hides the most useful failure reason.
func (r *AppReconciler) releaseDiagnostic(ctx context.Context, app *shpyrdv1.App, job *batchv1.Job) (message string, failed bool, err error) {
	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(job.Namespace), client.MatchingLabels{"job-name": job.Name}); err != nil {
		return "", false, fmt.Errorf("read release instances: %w", err)
	}
	for _, pod := range pods.Items {
		owner := metav1.GetControllerOf(&pod)
		if owner == nil || owner.UID != job.UID {
			continue
		}
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Name != "app" {
				continue
			}
			if (cs.State.Terminated != nil && cs.State.Terminated.Reason == "OOMKilled") ||
				(cs.LastTerminationState.Terminated != nil && cs.LastTerminationState.Terminated.Reason == "OOMKilled") {
				return releaseOOMMessage(app, job), true, nil
			}
		}
		if pod.Status.Reason == "Evicted" {
			return "The release step was stopped because its machine ran short of resources. Deploy again; if it happens again, ask the operator for more capacity. The previous release keeps running.", true, nil
		}
		if r.Kube != nil {
			events, e := r.Kube.CoreV1().Events(pod.Namespace).List(ctx, metav1.ListOptions{FieldSelector: "involvedObject.name=" + pod.Name})
			if e == nil {
				for _, event := range events.Items {
					if event.InvolvedObject.UID != pod.UID || event.InvolvedObject.Name != pod.Name {
						continue
					}
					switch event.Reason {
					case "OOMKilled":
						return releaseOOMMessage(app, job), true, nil
					case "Evicted":
						return "The release step was stopped because its machine ran short of resources. Deploy again; if it happens again, ask the operator for more capacity. The previous release keeps running.", true, nil
					}
				}
			}
		}
		// Silence is a warning, not proof of failure: some migrations are quiet.
		// Read only the last five minutes, with a bounded response and timeout.
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Name != "app" || cs.State.Running == nil || r.Kube == nil || r.now().Sub(cs.State.Running.StartedAt.Time) < releaseQuietAfter {
				continue
			}
			cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			raw, logErr := r.Kube.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
				Container: "app", SinceSeconds: ptr.To[int64](int64(releaseQuietAfter.Seconds())), TailLines: ptr.To[int64](1), LimitBytes: ptr.To[int64](1024),
			}).Do(cctx).Raw()
			cancel()
			if logErr == nil && strings.TrimSpace(string(raw)) == "" {
				message = "The release step has produced no output for at least 5 minutes. It may still be running: check the migration and its memory size. The previous release keeps running."
			}
		}
	}
	return message, false, nil
}

func releaseOOMMessage(app *shpyrdv1.App, job *batchv1.Job) string {
	memory := "its memory limit"
	for _, c := range job.Spec.Template.Spec.Containers {
		if c.Name == "app" {
			if q := c.Resources.Limits.Memory(); q != nil && !q.IsZero() {
				memory = q.String()
			}
		}
	}
	source := "the size of the web process"
	if p, ok := app.Spec.Processes[releaseProcessType]; ok && (p.Size != "" || len(p.Resources.Limits) > 0 || len(p.Resources.Requests) > 0) {
		source = "the size of processes.release"
	}
	return fmt.Sprintf("The release step ran out of memory at %s, %s. Give web or processes.release a larger size, or check the migration. The previous release keeps running.", memory, source)
}
