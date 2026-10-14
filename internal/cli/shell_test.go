package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestOneOffReportsNamedUserBeforePodPhase(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "run", Namespace: "app-blog"}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "app", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CreateContainerConfigError", Message: "container has runAsNonRoot and image has non-numeric user (node)"}}}}}}
	a := &appClient{k: &kube.Client{Kube: fake.NewSimpleClientset(pod)}}
	err := a.waitPodRunningOrDone(context.Background(), pod.Namespace, pod.Name, time.Second)
	if err == nil || !strings.Contains(err.Error(), "USER 1000") {
		t.Fatalf("error = %v", err)
	}
}
