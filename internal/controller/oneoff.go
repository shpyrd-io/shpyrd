package controller

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/install"
)

// One-off commands (`shpyrd run`, RFC-0005): a temporary instance of the
// project's current release that runs one command and goes away. The CLI
// builds the pod when it has the cluster; the API server builds it for a
// CLI signed in with `shpyrd login` (RFC-0052). Both call OneOffPod so the
// instance is the same whichever way it was started.

// RunProcess is the process label of one-off pods: they share the
// project's namespace and are never offered a shell or a release.
const RunProcess = "run"

// CNBLauncher sets up a buildpack image's runtime (PATH, env); a command
// exec'd without it would miss the language's environment.
const CNBLauncher = "/cnb/lifecycle/launcher"

// RunDeadline bounds a one-off instance: whatever it is doing, it is gone
// after an hour.
const RunDeadline = int64(3600)

// OneOffPod builds the pod of a one-off command: the release image and
// config vars, the command through the CNB launcher when the image has
// one, stdin attached (a TTY when interactive). StdinOnce keeps
// stdout/stderr attached after stdin reaches EOF; without it the runtime
// detaches the session as soon as piped input ends. pool, when set, is the
// node pool the pod is scheduled on (RFC-0077).
func OneOffPod(app *shpyrdv1.App, image string, command []string, res corev1.ResourceRequirements, tty, attach bool, pool string) *corev1.Pod {
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	name := fmt.Sprintf("%s-run-%s", app.Name, hex.EncodeToString(suffix))
	// "--" makes the launcher exec the command as-is instead of via bash -c.
	cmd := append([]string{CNBLauncher, "--"}, command...)
	if !app.UsesBuildpacks() {
		cmd = command // Dockerfile and prebuilt images have no launcher
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: app.Namespace,
			Labels: map[string]string{
				shpyrdv1.LabelApp:       app.Name,
				shpyrdv1.LabelProcess:   RunProcess,
				shpyrdv1.LabelManagedBy: "shpyrd",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:         corev1.RestartPolicyNever,
			ActiveDeadlineSeconds: ptr.To(RunDeadline),
			EnableServiceLinks:    ptr.To(false),
			// Private registries: the controller mirrors the credentials
			// into the project namespace under a fixed name; the pull secret
			// is optional for Kubernetes, so listing it is harmless without.
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: install.RegistrySecretName}},
			Containers: []corev1.Container{{
				Name:      "app",
				Image:     image,
				Command:   cmd,
				Stdin:     attach,
				StdinOnce: attach,
				TTY:       tty,
				Resources: res,
				Env:       append([]corev1.EnvVar{{Name: "SHPYRD_RUN", Value: "1"}}, app.Spec.Env...),
				// Globals, config vars, bound vars: the same sources and
				// order as the deployed processes (RFC-0016).
				EnvFrom: EnvSources(app),
				// Same hardening as deployed processes (RFC-0008).
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: ptr.To(false),
					RunAsNonRoot:             ptr.To(true),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
					SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
				},
			}},
		},
	}
	if pool != "" {
		pod.Spec.NodeSelector = map[string]string{PoolLabel: pool}
	}
	return pod
}
