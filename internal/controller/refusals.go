package controller

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// Refusals the customer cannot see (#52): Kubernetes refuses a release
// step or a new instance that would pass the workspace's ceiling (the
// ResourceQuota of the project), and says so only in an event or a
// Deployment condition nobody reads. The controller says it on the app,
// in words: the ceiling, what takes it and what is missing.

var exceededQuota = regexp.MustCompile(`exceeded quota: [^,]+, requested: (\S+), used: (\S+), limited: (\S+)`)

// quotaRefusal turns Kubernetes' "exceeded quota" sentence into words;
// who is what could not start ("The release step", "New instances of
// web"). "" when msg is not such a refusal.
func (r *AppReconciler) quotaRefusal(ctx context.Context, namespace, who, msg string) string {
	m := exceededQuota.FindStringSubmatch(msg)
	if m == nil {
		return ""
	}
	requested, used, limited := quotaList(m[1]), quotaList(m[2]), quotaList(m[3])
	var key string
	for k := range requested {
		if key == "" || k == "requests.memory" {
			key = k
		}
	}
	if key == "" {
		return ""
	}
	what := map[string]string{"requests.memory": "memory", "requests.cpu": "CPU", "requests.storage": "storage", "limits.memory": "memory", "limits.cpu": "CPU"}[key]
	if what == "" {
		what = key
	}
	limit, use, need := limited[key], used[key], requested[key]
	takers, hasDatabase := r.quotaTakers(ctx, namespace, corev1.ResourceName(strings.TrimPrefix(key, "requests.")))
	var b strings.Builder
	fmt.Fprintf(&b, "%s cannot start: the workspace's %s ceiling", who, what)
	switch {
	case use.Cmp(limit) >= 0 && takers != "":
		fmt.Fprintf(&b, ", %s, is all taken by %s", limit.String(), takers)
	case use.Cmp(limit) >= 0:
		fmt.Fprintf(&b, ", %s, is all taken", limit.String())
	case takers != "":
		fmt.Fprintf(&b, " is %s, %s of it taken by %s", limit.String(), use.String(), takers)
	default:
		fmt.Fprintf(&b, " is %s, %s of it taken", limit.String(), use.String())
	}
	pronoun := "it needs"
	if strings.HasPrefix(who, "New instances") {
		pronoun = "they need"
	}
	fmt.Fprintf(&b, ", and %s %s more.", pronoun, need.String())
	if hasDatabase {
		b.WriteString(" Give the database a smaller size or remove it, give the processes smaller sizes, or ask for a larger plan.")
	} else {
		b.WriteString(" Give the processes smaller sizes or fewer instances, or ask for a larger plan.")
	}
	return b.String()
}

// quotaList reads "requests.cpu=1,requests.memory=256Mi".
func quotaList(s string) map[string]resource.Quantity {
	out := map[string]resource.Quantity{}
	for _, kv := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if q, err := resource.ParseQuantity(v); err == nil {
			out[k] = q
		}
	}
	return out
}

// quotaTakers says what in the project holds the resource: "the database
// db (256Mi)", "web (128Mi) and the database db (128Mi)".
func (r *AppReconciler) quotaTakers(ctx context.Context, namespace string, res corev1.ResourceName) (string, bool) {
	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(namespace)); err != nil {
		return "", false
	}
	sums := map[string]*resource.Quantity{}
	database := false
	for _, p := range pods.Items {
		if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed || p.DeletionTimestamp != nil {
			continue
		}
		var name string
		switch {
		case p.Labels["cnpg.io/cluster"] != "":
			name, database = "the database "+p.Labels["cnpg.io/cluster"], true
		case p.Labels[shpyrdv1.LabelProcess] == releaseProcessType:
			name = "the release step"
		case p.Labels[shpyrdv1.LabelProcess] != "":
			name = p.Labels[shpyrdv1.LabelProcess]
		default:
			name = "other services of the project"
		}
		for _, c := range p.Spec.Containers {
			if q, ok := c.Resources.Requests[res]; ok {
				if sums[name] == nil {
					sums[name] = &resource.Quantity{}
				}
				sums[name].Add(q)
			}
		}
	}
	names := make([]string, 0, len(sums))
	for n := range sums {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if c := sums[names[i]].Cmp(*sums[names[j]]); c != 0 {
			return c > 0
		}
		return names[i] < names[j]
	})
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("%s (%s)", n, sums[n].String())
	}
	switch len(parts) {
	case 0:
		return "", database
	case 1:
		return parts[0], database
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1], database
}

// releaseJobRefusal is the quota refusal Kubernetes reported for the
// release Job's pod, in words; "" when there is none.
func (r *AppReconciler) releaseJobRefusal(ctx context.Context, job *batchv1.Job) string {
	if r.Kube == nil {
		return ""
	}
	events, err := r.Kube.CoreV1().Events(job.Namespace).List(ctx, metav1.ListOptions{FieldSelector: "involvedObject.name=" + job.Name + ",reason=FailedCreate"})
	if err != nil {
		return ""
	}
	var latest *corev1.Event
	for i := range events.Items {
		e := &events.Items[i]
		if e.InvolvedObject.Name != job.Name || e.Reason != "FailedCreate" {
			continue
		}
		if latest == nil || e.LastTimestamp.After(latest.LastTimestamp.Time) {
			latest = e
		}
	}
	if latest == nil {
		return ""
	}
	return r.quotaRefusal(ctx, job.Namespace, "The release step", latest.Message)
}

// processRefusals are the quota refusals of the processes whose new
// instances Kubernetes would not create, in words, one per process.
func (r *AppReconciler) processRefusals(ctx context.Context, app *shpyrdv1.App, procs map[string]shpyrdv1.ProcessStatus) string {
	names := make([]string, 0, len(procs))
	for n, p := range procs {
		if p.Updated < p.Desired {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	var out []string
	for _, n := range names {
		d := &appsv1.Deployment{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: workloadName(app, n)}, d); err != nil {
			continue
		}
		for _, c := range d.Status.Conditions {
			if c.Type == appsv1.DeploymentReplicaFailure && c.Status == corev1.ConditionTrue {
				if msg := r.quotaRefusal(ctx, app.Namespace, "New instances of "+n, c.Message); msg != "" {
					out = append(out, msg)
				}
			}
		}
	}
	return strings.Join(out, " ")
}
