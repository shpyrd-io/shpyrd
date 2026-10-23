package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// #126: `shpyrd run --process web` gets web's volumes, on the machine that
// holds its disk, and says so; an unknown process is refused.
func TestAOneOffRunTakesAProcessVolumes(t *testing.T) {
	shop := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"},
		Spec:   shpyrdv1.AppSpec{Image: "ghcr.io/acme/shop:1", Processes: map[string]shpyrdv1.Process{"web": {Volumes: []shpyrdv1.VolumeMount{{Name: "data", Path: "/data"}}}}},
		Status: shpyrdv1.AppStatus{Phase: shpyrdv1.PhaseRunning}}
	disk := &shpyrdv1.Volume{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "app-shop"}}
	holder := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "app-shop"}, Spec: corev1.PodSpec{NodeName: "10.0.1.38", Volumes: []corev1.Volume{{Name: "vol-data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "vol-data"}}}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	s, _ := newTestServer(t, nil, []client.Object{shop, disk}, holder)
	rec := do(t, s, "POST", "/api/projects/shop/run", `{"command":["rake","db:migrate"],"detach":true,"process":"web"}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("run = %d %s", rec.Code, rec.Body)
	}
	var res RunResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Note != "with the volumes of web (data at /data), on 10.0.1.38, the machine that holds the disk" {
		t.Errorf("note = %q", res.Note)
	}
	pod, err := s.kube.Kube.CoreV1().Pods("app-shop").Get(context.Background(), res.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pod.Spec.Volumes) != 1 || pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName != "vol-data" || pod.Spec.NodeSelector[corev1.LabelHostname] != "10.0.1.38" {
		t.Errorf("run pod = volumes %+v selector %v", pod.Spec.Volumes, pod.Spec.NodeSelector)
	}
	if rec := do(t, s, "POST", "/api/projects/shop/run", `{"command":["sh"],"detach":true,"process":"cron"}`, true); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "has no process") {
		t.Errorf("unknown process = %d %s", rec.Code, rec.Body)
	}
	// Without a process: no volumes, as before.
	rec = do(t, s, "POST", "/api/projects/shop/run", `{"command":["sh"],"detach":true}`, true)
	res = RunResult{}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if pod, err := s.kube.Kube.CoreV1().Pods("app-shop").Get(context.Background(), res.Name, metav1.GetOptions{}); err != nil || len(pod.Spec.Volumes) != 0 || res.Note != "" {
		t.Errorf("plain run = %v volumes %+v note %q", err, pod.Spec.Volumes, res.Note)
	}
}
