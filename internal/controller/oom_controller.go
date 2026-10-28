package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/audit"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// OOMRecorder keeps every out-of-memory kill (#119). Kubernetes remembers
// only the last death of each container, in lastState, and the node's
// OOMKilling events expire within the hour, so a developer looking at the
// Memory chart saw a sawtooth and a restart, not that the process was
// killed for crossing its size. The recorder watches every pod status
// change, which is where each death shows for at least the restart
// back-off (10 s and up), and writes it to the store: durable, one row a
// death, independent of Prometheus. Project pods and the platform's own
// components (the backup CronJob) alike.
type OOMRecorder struct {
	client.Client
	// Store keeps the kills; nil records nothing (tests without a store).
	Store store.OOMKills
	// Kube writes the audit entry a project's Activity panel shows; nil
	// writes none.
	Kube kubernetes.Interface
	// SystemNamespace is the platform's own: its pods are recorded as
	// components, with no project.
	SystemNamespace string
	// Now returns the current time; nil means time.Now (tests override it).
	Now func() time.Time
	// audited remembers, per namespace and process, when an Activity entry
	// was last written: a crash loop is one entry an hour, not one a death.
	audited sync.Map
}

// oomReason is the kubelet's reason for a container the kernel killed.
const oomReason = "OOMKilled"

// oomAuditEvery is how often a process's kills reach its project's
// Activity: the first one at once, then one entry an hour at most.
const oomAuditEvery = time.Hour

// SetupWithManager registers the recorder on the pods that show a kill.
func (r *OOMRecorder) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("oom-kills").
		Watches(&corev1.Pod{}, &handler.EnqueueRequestForObject{}, builder.WithPredicates(predicate.NewPredicateFuncs(r.watches))).
		Complete(r)
}

// watches says whether a pod is worth a reconcile: a project's (it carries
// the app label) or a platform component's, and only once its status shows
// a kill. Every other pod change is left where it is.
func (r *OOMRecorder) watches(o client.Object) bool {
	pod, ok := o.(*corev1.Pod)
	if !ok || !r.covers(pod) {
		return false
	}
	return len(oomKillsOf(pod)) > 0
}

// covers says whose pods are recorded: a project's and the platform's own.
func (r *OOMRecorder) covers(pod *corev1.Pod) bool {
	return pod.Labels[shpyrdv1.LabelApp] != "" || (r.SystemNamespace != "" && pod.Namespace == r.SystemNamespace)
}

// Reconcile records the kills a pod's status shows. The store skips the
// ones it has; a failure to write is retried with the manager's back-off,
// and the status keeps saying the same until the container dies again.
func (r *OOMRecorder) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if r.Store == nil {
		return ctrl.Result{}, nil
	}
	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !r.covers(&pod) {
		return ctrl.Result{}, nil
	}
	kills := oomKillsOf(&pod)
	if len(kills) == 0 {
		return ctrl.Result{}, nil
	}
	added, err := r.Store.RecordOOMKills(ctx, kills)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("recording the out-of-memory kills of %s: %w", req.NamespacedName, err)
	}
	if added == 0 {
		return ctrl.Result{}, nil
	}
	latest := kills[0]
	for _, k := range kills[1:] {
		if k.At.After(latest.At) {
			latest = k
		}
	}
	log.FromContext(ctx).Info("out-of-memory kill recorded", "pod", pod.Name, "process", latest.Process, "limit", latest.LimitBytes, "new", added)
	r.audit(ctx, &pod, latest)
	return ctrl.Result{}, nil
}

// audit writes the kill to the project's Activity (RFC-0022a) when the
// pod is a project's and its process has not been written up in the last
// hour: a burst reads as one entry, not as a line a death.
func (r *OOMRecorder) audit(ctx context.Context, pod *corev1.Pod, k store.OOMKill) {
	app := pod.Labels[shpyrdv1.LabelApp]
	if r.Kube == nil || app == "" {
		return
	}
	now := r.now()
	key := pod.Namespace + "/" + k.Process
	if last, ok := r.audited.Load(key); ok && now.Sub(last.(time.Time)) < oomAuditEvery {
		return
	}
	r.audited.Store(key, now)
	entry := audit.Entry{
		Time:   k.At,
		Actor:  "platform",
		Action: "process.oom_killed",
		Target: k.Process,
		Detail: describeOOMKill(k),
		Via:    "controller", Realm: "workspace",
	}
	if err := audit.Record(ctx, r.Kube, audit.AppRefIn(pod.Namespace, app), entry); err != nil {
		log.FromContext(ctx).Info("could not audit the out-of-memory kill", "err", err.Error())
	}
}

func (r *OOMRecorder) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// describeOOMKill says what died and at what limit, for the Activity:
// "web.1 was killed: out of memory at its 256Mi".
func describeOOMKill(k store.OOMKill) string {
	who := k.Instance
	if who == "" {
		switch k.Process {
		case releaseProcessType:
			who = "the release step"
		case "build":
			who = "the build"
		case runProcess:
			who = "a one-off run"
		default:
			who = k.Process
		}
	}
	s := who + " was killed: out of memory"
	if k.LimitBytes > 0 {
		s += " at its " + resource.NewQuantity(k.LimitBytes, resource.BinarySI).String()
	}
	return s
}

// oomKillsOf reads the kills a pod's status shows: a container killed and
// restarted (lastState) or killed for good (state, as a Job's container
// is). Only the kubelet's OOMKilled reason counts: an exit 137 from a
// liveness probe or a scale-down is not one. A death without a time cannot
// be told from the next and is left out.
func oomKillsOf(pod *corev1.Pod) []store.OOMKill {
	base := store.OOMKill{
		WorkspaceID: pod.Labels[shpyrdv1.LabelWorkspaceID],
		Project:     pod.Labels[shpyrdv1.LabelProjectID],
		Namespace:   pod.Namespace,
		Pod:         pod.Name,
		Process:     oomProcessOf(pod),
		Instance:    pod.Annotations[shpyrdv1.AnnotationInstance],
		Release:     releaseOfPod(pod),
	}
	var out []store.OOMKill
	for _, statuses := range [][]corev1.ContainerStatus{pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses} {
		for _, cs := range statuses {
			for _, t := range []*corev1.ContainerStateTerminated{cs.LastTerminationState.Terminated, cs.State.Terminated} {
				if t == nil || t.Reason != oomReason || t.FinishedAt.IsZero() {
					continue
				}
				k := base
				k.Container, k.At, k.LimitBytes = cs.Name, t.FinishedAt.Time.UTC(), memoryLimitOf(pod, cs.Name)
				dup := false
				for _, have := range out {
					if have.Container == k.Container && have.At.Equal(k.At) {
						dup = true
					}
				}
				if !dup {
					out = append(out, k)
				}
			}
		}
	}
	return out
}

// oomProcessOf names what a pod ran: a project's process type (web,
// worker; release, run for the Job pods that carry one), "build" for a
// Dockerfile build, and a platform component's workload otherwise.
func oomProcessOf(pod *corev1.Pod) string {
	switch {
	case pod.Labels[shpyrdv1.LabelProcess] != "":
		return pod.Labels[shpyrdv1.LabelProcess]
	case pod.Labels[shpyrdv1.LabelBuild] != "":
		return "build"
	}
	if name := pod.Labels["app.kubernetes.io/name"]; name != "" {
		return name
	}
	if job := pod.Labels["job-name"]; job != "" {
		// A CronJob names its Jobs after itself and the schedule's minute.
		if i := strings.LastIndexByte(job, '-'); i > 0 {
			if _, err := strconv.Atoi(job[i+1:]); err == nil {
				return job[:i]
			}
		}
		return job
	}
	if owner := metav1.GetControllerOf(pod); owner != nil {
		return owner.Name
	}
	return pod.Name
}

// releaseOfPod reads the release the pod ran off the platform env of its
// app container (SHPYRD_RELEASE); 0 when it carries none.
func releaseOfPod(pod *corev1.Pod) int {
	for _, c := range pod.Spec.Containers {
		for _, e := range c.Env {
			if e.Name == "SHPYRD_RELEASE" {
				n, _ := strconv.Atoi(e.Value)
				return n
			}
		}
	}
	return 0
}

// memoryLimitOf is the memory limit of the named container, 0 without one.
func memoryLimitOf(pod *corev1.Pod, container string) int64 {
	for _, list := range [][]corev1.Container{pod.Spec.InitContainers, pod.Spec.Containers} {
		for _, c := range list {
			if c.Name != container {
				continue
			}
			if q, ok := c.Resources.Limits[corev1.ResourceMemory]; ok {
				return q.Value()
			}
			return 0
		}
	}
	return 0
}
