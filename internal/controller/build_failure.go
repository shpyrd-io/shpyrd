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

// hostKeyChangedMarker is the line a Dockerfile build's fetch prints when
// ssh refused the git host's key while cloning with a deploy key. git's own
// output is the same as for a clone with no credential at all (host key
// verification failed, could not read from remote repository), so only the
// marker tells the two apart.
const hostKeyChangedMarker = "shpyrd: the git host's SSH host key does not match the one recorded with the deploy key"

// refusedClone is git (or kpack's git) refused access to a repository.
// kpack's go-git says SSH_AUTH_SOCK when no Secret matched an ssh URL, and
// "unable to authenticate" when the host refused the key.
var refusedClone = regexp.MustCompile(`(?i)could not read Username|Repository not found|Permission denied \(publickey\)|authentication required|Authentication failed|could not read from remote repository|SSH agent requested but SSH_AUTH_SOCK|ssh: unable to authenticate`)

// registryRefusedWrite is kpack's prepare step finding it may not write the
// image to the registry ("Error verifying write access to ...").
var registryRefusedWrite = regexp.MustCompile(`(?i)error verifying write access`)

// The platform's own faults, as kpack's message or the step's output says
// them (production, October 2026). Each is explained as the platform's, with
// what to do, and none of its words (an address, a token, a signal) is
// repeated.
var (
	// nodePressure: the machine refused or evicted the build for want of
	// disk (DiskPressure, "The node was low on resource: ephemeral-storage").
	nodePressure = regexp.MustCompile(`(?i)DiskPressure|MemoryPressure|was low on resource|Pod was rejected|\bevicted\b`)
	// sourceUnreachable: the server that holds the uploaded archive did
	// not answer (it was restarting).
	sourceUnreachable = regexp.MustCompile(`/api/sources/[^\n]*(?:connection refused|connection reset|i/o timeout|no such host|context deadline exceeded)`)
	// sourceGone: the archive is not on the server any more.
	sourceGone = regexp.MustCompile(`/api/sources/[^\n]*404 Not Found`)
	// toolCrashed: the platform's fetch tool itself died.
	toolCrashed = regexp.MustCompile(`Segmentation fault|core dumped|Illegal instruction|Bus error`)
	// emptyRepository and missingRef: kpack's git found nothing to build.
	emptyRepository = regexp.MustCompile(`remote repository is empty`)
	missingRef      = regexp.MustCompile(`couldn't find remote ref "?([^"\s:]+)"?`)
)

// What the Node package managers say on a first deploy (production,
// October 2026: eleven of the seventeen `npm run build` failures were one
// of these).
var (
	// pnpmLockfile: pnpm refuses a lockfile behind a package.json.
	pnpmLockfile = regexp.MustCompile(`ERR_PNPM_OUTDATED_LOCKFILE[^\n]*?(\S*pnpm-lock\.yaml) is not up to date with (\S*package\.json)`)
	// npmLockfile: npm ci refuses a lockfile behind package.json, naming
	// what is missing.
	npmLockfile = regexp.MustCompile("`npm ci` can only install packages when your package.json and package-lock.json")
	npmMissing  = regexp.MustCompile(`Missing: (\S+) from lock file`)
	// lifecycleNotFound: a package's lifecycle script (pnpm prints
	// "prepare: sh: 1: husky: not found") ran a command that is gone,
	// as happens when the dev dependencies are pruned and prepare runs
	// again.
	lifecycleNotFound = regexp.MustCompile(`(?m)^[^\n:]*?\b(\w+): sh: \d+: (\S+): not found`)
	// commandNotFound: a script ran a command that is not installed
	// (exit status 127).
	commandNotFound = regexp.MustCompile(`sh: \d+: (\S+): not found`)
)

// unsupportedVersion is a Paketo buildpack finding no version of the
// language that satisfies the app (#185): `failed to satisfy "go"
// dependency version constraint ">= 1.27": no compatible versions on
// "io.buildpacks.stacks.jammy" stack. Supported versions are: [1.25.13, ...]`.
var (
	unsupportedVersion = regexp.MustCompile(`failed to satisfy "([\w.-]+)" dependency version constraint "([^"]*)": no compatible versions[^\n]*`)
	supportedVersions  = regexp.MustCompile(`Supported versions are: \[([^\]]*)\]`)
)

// languageVersions names a Paketo dependency as a person knows it, and where
// the app asks for its version.
var languageVersions = map[string][2]string{
	"go":      {"Go", "the go line of go.mod"},
	"node":    {"Node.js", "engines.node in package.json, or .nvmrc"},
	"cpython": {"Python", ".python-version, or the version the project's tool pins"},
	"python":  {"Python", ".python-version, or the version the project's tool pins"},
	"ruby":    {"Ruby", "the ruby line of the Gemfile"},
	"php":     {"PHP", "require.php in composer.json"},
}

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
	if nodePressure.MatchString(text) {
		return "The machine the build ran on ran out of disk while it was building: a fault of the platform, not of the app. Deploying again puts the build on a machine with room, and usually passes. " + buildLogHint
	}
	if step == "prepare" && toolCrashed.MatchString(text) {
		return "The platform's own tool crashed while fetching the source, before the app's code ran: a fault of the platform, not of the app. Deploying again usually passes. " + buildLogHint
	}
	if step == "prepare" && sourceUnreachable.MatchString(text) {
		return "The build could not download the uploaded source from the platform, which was not answering (it was restarting): a fault of the platform, not of the app. Deploying again usually passes. " + buildLogHint
	}
	if step == "prepare" && sourceGone.MatchString(text) {
		return "The uploaded source is no longer on the platform (it was lost when the platform restarted). Deploy again from the source directory, which uploads it anew; building this build again will not pass. " + buildLogHint
	}
	if step == "prepare" && emptyRepository.MatchString(text) {
		return "The repository is empty: nothing has been pushed to it yet. Push a first commit, and the build runs again. " + buildLogHint
	}
	if m := missingRef.FindStringSubmatch(text); step == "prepare" && m != nil {
		return fmt.Sprintf("The repository has no branch named %s. Set the branch the project deploys from to one that exists (the repository's default branch may be master, or another name), then deploy again. %s", m[1], buildLogHint)
	}
	if step == "prepare" && registryRefusedWrite.MatchString(text) {
		// kpack checks it may write the image before fetching the source;
		// the registry refusing (read-only, unreachable, out of space) is
		// the platform's, not the app's or its repository's.
		return "The build could not save its image to the platform's registry, before the app's own code ran: a fault of the platform, not of the app. Deploying again later usually passes. " + buildLogHint
	}
	if step == "prepare" && strings.Contains(text, hostKeyChangedMarker) {
		return "The build could not check the git host's identity: its SSH host key is not the one recorded with the deploy key. " +
			"If the host changed its key, make a new deploy key with `shpyrd git-auth deploy-key` and add it to the repository. " + buildLogHint
	}
	if step == "prepare" && refusedClone.MatchString(text) {
		return "The build could not read the repository: it is private, or does not exist under that name. " +
			"Give shpyrd access: `shpyrd git-auth deploy-key` prints a key to add to the repository as a read-only deploy key, " +
			"or a workspace admin sets a token for the host with `shpyrd git-auth set --host <host> --token -`. Then build again with `shpyrd redeploy`. " + buildLogHint
	}
	if m := unsupportedVersion.FindStringSubmatch(text); m != nil {
		var have string
		if v := supportedVersions.FindStringSubmatch(m[0]); v != nil {
			have = v[1]
		}
		return unsupportedVersionMessage(m[1], m[2], have)
	}
	if m := pnpmLockfile.FindStringSubmatch(text); m != nil {
		return fmt.Sprintf("The build failed installing the dependencies: %s is not up to date with %s. Run pnpm install, commit the lockfile and deploy again. %s", m[1], m[2], buildLogHint)
	}
	if npmLockfile.MatchString(text) {
		missing := ""
		if m := npmMissing.FindStringSubmatch(text); m != nil {
			missing = fmt.Sprintf(" (%s is not in the lockfile)", m[1])
		}
		return fmt.Sprintf("The build failed installing the dependencies: package-lock.json is not in sync with package.json%s. Run npm install, commit the updated lockfile and deploy again. %s", missing, buildLogHint)
	}
	if m := lifecycleNotFound.FindStringSubmatch(text); m != nil && script == "" {
		return fmt.Sprintf("The build failed in the package's %s script: it runs %s, which is not installed at that point (a dev dependency, removed before the script ran again). Make the script tolerate its absence, as in \"%s || true\", or move that step out of %s. %s", m[1], m[2], m[2], m[1], buildLogHint)
	}
	if m := commandNotFound.FindStringSubmatch(text); m != nil {
		return fmt.Sprintf("%s: %s is not installed in the build. When it is a dev dependency, check that package.json lists it and the lockfile has it (the build installs dev dependencies); a tool installed on your machine by hand has to be a dependency too. %s", while, m[1], buildLogHint)
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

// unsupportedVersionMessage says the app asks for a version of its language
// the platform's buildpacks do not have yet. New versions reach them some
// days after their release, and the platform takes them up by itself.
func unsupportedVersionMessage(dependency, constraint, supported string) string {
	language, where := dependency, ""
	if l, ok := languageVersions[dependency]; ok {
		language, where = l[0], " ("+l[1]+")"
	}
	have := ""
	if versions := strings.Fields(strings.ReplaceAll(supported, ",", " ")); len(versions) > 0 {
		have = " They have " + strings.Join(versions, ", ") + "."
	}
	return fmt.Sprintf("The app asks for %s %s, and the platform's buildpacks have no version of it that matches.%s "+
		"A new version reaches the buildpacks some days or weeks after its release, and the platform takes it up by itself. "+
		"Until then, ask for a version they have%s, or build with a Dockerfile. %s",
		language, constraint, have, where, buildLogHint)
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
// message the controller wrote on it, else one from kpack's own message,
// which names the step and, for the platform's faults (the machine out of
// disk, the source server away, the registry refusing), the fault itself.
// For the API, which never reads logs.
func KpackFailure(b *unstructured.Unstructured) string {
	if msg := b.GetAnnotations()[shpyrdv1.AnnotationBuildFailure]; msg != "" {
		return msg
	}
	msg := kpackMessage(b)
	if stepNotStarted(msg) {
		return notStartedMessage
	}
	return describeBuildFailure(kpackStep(msg), msg)
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
	// kpack's message names the platform's faults when the step's output
	// does not (the machine evicted the build; nothing was written).
	msg := describeBuildFailure(step, out+"\n"+kpackMessage(b))
	if !started || stepNotStarted(kpackMessage(b)) {
		msg = notStartedMessage
	}
	// The message quotes the script that ran, from the build's output,
	// and the output itself is kept: masked like the log (#54).
	masker := r.buildMasker(ctx, r.appOfBuild(ctx, b))
	ann := map[string]string{shpyrdv1.AnnotationBuildFailure: masker.Line(msg)}
	if kept := keptOutput(out); kept != "" {
		ann[shpyrdv1.AnnotationBuildOutput] = masker.Line(kept)
		ann[shpyrdv1.AnnotationBuildFailedStep] = step
	}
	patch := client.MergeFrom(b.DeepCopy())
	b.SetAnnotations(mergeMaps(b.GetAnnotations(), ann))
	_ = r.Patch(ctx, b, patch) // read again next time when it does not stick
	return ann[shpyrdv1.AnnotationBuildFailure]
}

// buildMasker masks the project's secret variables (#54); nil, which masks
// nothing, when the app or its build Secret cannot be read.
func (r *AppReconciler) buildMasker(ctx context.Context, app *shpyrdv1.App) *logs.Masker {
	if app == nil {
		return nil
	}
	var vars corev1.Secret
	if r.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: app.BuildEnvName()}, &vars) != nil {
		return nil
	}
	return logs.NewMasker(logs.BuildSecrets(app, vars.Data))
}

// keptOutputBytes bounds what a build keeps of its failed step's output:
// enough for the error and what led to it, small beside the object it is
// written on.
const keptOutputBytes = 16 << 10

// keptOutput is the end of a failed step's output, as kept on the build:
// the last lines within keptOutputBytes, without colour codes.
func keptOutput(out string) string {
	out = strings.TrimRight(ansiEscape.ReplaceAllString(out, ""), "\n ")
	if out == "" {
		return ""
	}
	if len(out) > keptOutputBytes {
		out = out[len(out)-keptOutputBytes:]
		if i := strings.IndexByte(out, '\n'); i >= 0 {
			out = out[i+1:]
		}
	}
	return out
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
