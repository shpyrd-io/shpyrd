package controller

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/logs"
)

// A failed build is explained in words (#52): what happened and how to
// resolve it, never a command, a flag, a tool or a Kubernetes name. The
// reader is a person or their agent in the dashboard, the CLI or an
// assistant, and each of those shows the build's log itself; the build's
// own output stays in that log and is never quoted into the message.
// kpack's sentence ("Container build terminated ... use kubectl logs")
// is never shown.

// buildLogHint closes every message: where the rest is.
const buildLogHint = "The build's log has the full output."

var (
	ansiEscape     = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	paketoHeader   = regexp.MustCompile(`^Paketo Buildpack for (.+?) v?\d+(\.\d+)*$`)
	runningScript  = regexp.MustCompile(`^\s*Running '(.+)'\s*$`)
	exitStatus     = regexp.MustCompile(`^exit status (\d+)$`)
	dockerfileStep = regexp.MustCompile(`process "/bin/(?:ba)?sh -c (.+?)" did not complete successfully: exit code: (\d+)`)
	// A variable the build read and did not find. The name is what the
	// message keeps of the output.
	missingVariable = []*regexp.Regexp{
		regexp.MustCompile(`Cannot resolve environment variable: ([A-Za-z_][A-Za-z0-9_]*)`),
		regexp.MustCompile(`Environment variable not found: ([A-Za-z_][A-Za-z0-9_]*)`),
		regexp.MustCompile(`[Mm]issing (?:required )?environment variables?:? ['"\x60]?([A-Z][A-Z0-9_]+)`),
		regexp.MustCompile(`KeyError: '([A-Z][A-Z0-9_]+)'`),
		regexp.MustCompile(`key not found: "([A-Z][A-Z0-9_]+)"`),
		regexp.MustCompile(`[Ee]nvironment variable ['"\x60]?([A-Z][A-Z0-9_]+)['"\x60]? (?:is )?(?:not set|not defined|undefined|missing|required)`),
		regexp.MustCompile(`\b([A-Z][A-Z0-9]*_[A-Z0-9_]+|[A-Z]{3,}) (?:is not set|is not defined|is undefined|is required|não está definida|no está definida)`),
	}
	missingModule = []*regexp.Regexp{
		regexp.MustCompile(`Cannot find module '([^']+)'`),
		regexp.MustCompile(`Module not found: Error: Can't resolve '([^']+)'`),
		regexp.MustCompile(`ModuleNotFoundError: No module named '([^']+)'`),
		regexp.MustCompile(`cannot load such file -- (\S+)`),
	}
)

// describeBuildFailure says what made a build fail, from the step that
// failed (a kpack phase: prepare, analyze, detect, restore, build, export,
// completion; "dockerfile" for a BuildKit build) and the end of its output.
func describeBuildFailure(step, out string) string {
	lines := strings.Split(ansiEscape.ReplaceAllString(out, ""), "\n")
	var buildpack, script, code string
	for _, l := range lines {
		l = strings.TrimRight(l, "\r ")
		if m := paketoHeader.FindStringSubmatch(l); m != nil {
			buildpack, script = m[1], ""
		}
		if m := runningScript.FindStringSubmatch(l); m != nil {
			script = m[1]
		}
		if m := exitStatus.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			code = m[1]
		}
		if m := dockerfileStep.FindStringSubmatch(l); m != nil {
			script, code = m[1], m[2]
		}
	}
	if len(script) > 80 {
		script = script[:77] + "..."
	}
	text := strings.Join(lines, "\n")
	while := "The build failed"
	if script != "" {
		while = fmt.Sprintf("The build failed while running `%s`", script)
	}
	for _, re := range missingVariable {
		if m := re.FindStringSubmatch(text); m != nil {
			return fmt.Sprintf("%s: the app reads the variable %s while building, and it is not set. Set it as a config var of the project, or attach the resource that provides it, and deploy again; or make the build not depend on it. %s", while, m[1], buildLogHint)
		}
	}
	for _, re := range missingModule {
		if m := re.FindStringSubmatch(text); m != nil {
			return fmt.Sprintf("%s: the module %s cannot be found. Add it to the app's dependencies, or make the build not need it. %s", while, m[1], buildLogHint)
		}
	}
	if strings.Contains(text, "No buildpack groups passed detection") || (step == "detect" && script == "") {
		return "No buildpack recognised the source: nothing in it names a language the buildpacks build (a package.json, go.mod, requirements.txt, Gemfile, pom.xml, composer.json and the like), at its root or under the path deployed. " + buildLogHint
	}
	if script != "" {
		if code != "" {
			return fmt.Sprintf("%s (exit status %s). %s", while, code, buildLogHint)
		}
		return fmt.Sprintf("%s. %s", while, buildLogHint)
	}
	switch step {
	case "prepare":
		return "The build could not fetch the source. " + buildLogHint
	case "analyze", "restore":
		return "The build failed while reading the cache of earlier builds, before the app's own code ran: a fault of the platform, not of the app. Deploying again usually passes. " + buildLogHint
	case "dockerfile":
		return "The Dockerfile build failed. " + buildLogHint
	case "export", "completion":
		return "The build ran, but its image could not be saved: a fault of the platform, not of the app. Deploying again usually passes. " + buildLogHint
	}
	if buildpack != "" {
		return fmt.Sprintf("The build failed in the %s buildpack. %s", buildpack, buildLogHint)
	}
	return "The build failed. " + buildLogHint
}

// kpackStep is the step kpack names in a Build's failure message
// ("Container build terminated with error ..."), "" when none.
var kpackStepRe = regexp.MustCompile(`Container (\w+) terminated`)

func kpackStep(kpackMessage string) string {
	if m := kpackStepRe.FindStringSubmatch(kpackMessage); m != nil {
		return m[1]
	}
	return ""
}

// KpackFailure is what the platform says of a failed kpack Build: the
// message the controller wrote on it, else one from the step kpack names.
// For the API, which never reads logs.
func KpackFailure(b *unstructured.Unstructured) string {
	if msg := b.GetAnnotations()[shpyrdv1.AnnotationBuildFailure]; msg != "" {
		return msg
	}
	if stepNotStarted(kpackMessage(b)) {
		return notStartedMessage
	}
	return describeBuildFailure(kpackStep(kpackMessage(b)), "")
}

// buildFailed says a kpack Build's Succeeded condition is False.
func buildFailed(b *unstructured.Unstructured) bool {
	conds, _, _ := unstructured.NestedSlice(b.Object, "status", "conditions")
	for _, raw := range conds {
		if m, ok := raw.(map[string]interface{}); ok && m["type"] == "Succeeded" {
			return m["status"] == "False"
		}
	}
	return false
}

// kpackMessage is the message of a kpack Build's Succeeded condition.
func kpackMessage(b *unstructured.Unstructured) string {
	conds, _, _ := unstructured.NestedSlice(b.Object, "status", "conditions")
	for _, raw := range conds {
		if m, ok := raw.(map[string]interface{}); ok && m["type"] == "Succeeded" {
			msg, _ := m["message"].(string)
			return msg
		}
	}
	return ""
}

// kpackBuildFailure explains a failed kpack Build: once, from the end of
// the failed step's output, written on the Build so the API and the next
// reconcile read it from there.
func (r *AppReconciler) kpackBuildFailure(ctx context.Context, b *unstructured.Unstructured) string {
	if b == nil {
		return describeBuildFailure("", "")
	}
	if msg := b.GetAnnotations()[shpyrdv1.AnnotationBuildFailure]; msg != "" {
		return msg
	}
	step, out, started := r.failedStepOutput(ctx, b.GetNamespace(), b.GetName()+"-build-pod")
	if step == "" {
		step = kpackStep(kpackMessage(b))
	}
	msg := describeBuildFailure(step, out)
	if !started || stepNotStarted(kpackMessage(b)) {
		msg = notStartedMessage
	}
	// The message quotes the script that ran, from the build's output:
	// masked like the log (#54).
	var vars corev1.Secret
	if app := r.appOfBuild(ctx, b); app != nil && r.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: app.BuildEnvName()}, &vars) == nil {
		msg = logs.NewMasker(logs.BuildSecrets(app, vars.Data)).Line(msg)
	}
	patch := client.MergeFrom(b.DeepCopy())
	b.SetAnnotations(mergeMaps(b.GetAnnotations(), map[string]string{shpyrdv1.AnnotationBuildFailure: msg}))
	_ = r.Patch(ctx, b, patch) // read again next time when it does not stick
	return msg
}

// notStartedMessage is what a build says when the platform could not start
// one of its steps (the container runtime refused it): no output to read,
// nothing the app did.
const notStartedMessage = "The build could not start one of its steps: a fault of the platform, not of the app. Deploying again usually passes."

// stepNotStarted says kpack's message is about a step the container runtime
// could not start.
func stepNotStarted(kpackMessage string) bool {
	return strings.Contains(kpackMessage, "failed to create containerd task") || strings.Contains(kpackMessage, "OCI runtime create failed")
}

// failedStepOutput finds the step of a build pod that exited with an
// error and the end of its output; "" when the pod is gone. started is
// false when the step never ran (the runtime could not start it).
func (r *AppReconciler) failedStepOutput(ctx context.Context, namespace, podName string) (step, out string, started bool) {
	pod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: podName}, pod); err != nil {
		return "", "", true
	}
	for _, cs := range append(append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...) {
		if t := cs.State.Terminated; t != nil && t.ExitCode != 0 {
			step, started = cs.Name, t.Reason != "StartError"
			break
		}
	}
	if step == "" {
		return "", "", true
	}
	if !started || r.Kube == nil {
		return step, "", started
	}
	raw, err := r.Kube.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{Container: step, TailLines: ptr.To[int64](200)}).Do(ctx).Raw()
	if err != nil {
		return step, "", true
	}
	return step, string(raw), true
}

// appOfBuild is the App a kpack Build belongs to (its Image is named after
// the App), nil when it cannot be read.
func (r *AppReconciler) appOfBuild(ctx context.Context, b *unstructured.Unstructured) *shpyrdv1.App {
	name := b.GetLabels()["image.kpack.io/image"]
	if name == "" {
		return nil
	}
	app := &shpyrdv1.App{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: b.GetNamespace(), Name: name}, app); err != nil {
		return nil
	}
	return app
}
