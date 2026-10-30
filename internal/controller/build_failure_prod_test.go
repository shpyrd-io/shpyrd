package controller

import (
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// What the production cluster told people between 1 and 29 October 2026,
// from the failure messages kpack and the Job controller left on the 67
// failed buildpack builds and 12 failed Dockerfile builds. By then the
// build's log was usually gone (the runtime had dropped the containers), so
// the message is all that survives; it has to say whose fault it was and
// what to do. Each case is one real message; `want` is a phrase the
// explanation must carry and `never` phrases it must not.

// kpackBuild is a Build whose Succeeded condition carries kpack's message
// and nothing else: no annotation, no pod to read.
func kpackBuild(message string) *unstructured.Unstructured {
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(KpackBuildGVK)
	b.SetName("app-build-1")
	b.SetNamespace("p-app")
	b.Object["status"] = map[string]interface{}{
		"conditions": []interface{}{map[string]interface{}{"type": "Succeeded", "status": "False", "message": message}},
	}
	return b
}

func TestProductionKpackMessagesAreExplained(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    []string
		never   []string
	}{
		{
			// 2026-10-07 and 2026-10-20, four builds: the apps nodes ran
			// out of disk and the kubelet refused the build pod outright.
			name:    "node under disk pressure rejects the pod",
			message: "Pod was rejected: The node had condition: [DiskPressure]. ",
			want:    []string{"platform", "not of the app", "again"},
			never:   []string{"The build failed. The build's log"},
		},
		{
			// 2026-10-14 and 2026-10-20: the kubelet evicted the pod
			// while detecting, before any container of the app's own ran.
			name:    "node low on ephemeral storage, no step named",
			message: "The node was low on resource: ephemeral-storage. Threshold quantity: 5714883606, available: 5538364Ki. ",
			want:    []string{"platform", "not of the app", "again"},
			never:   []string{"The build failed. The build's log"},
		},
		{
			// 2026-10-20, six builds: evicted while exporting the image.
			name:    "node low on ephemeral storage while exporting",
			message: "Error: The node was low on resource: ephemeral-storage. Threshold quantity: 5714883606, available: 5425164Ki.  Container export terminated with error Adding cache layer 'paketo-buildpacks/node-engine:node': For more info use `kubectl logs -n p-x x-build-39-build-pod -c export`",
			want:    []string{"platform", "not of the app"},
		},
		{
			// 2026-10-04 13:51, eight builds at once: the server was
			// restarting and nothing served the archives for a minute.
			name:    "source server unreachable",
			message: `Error:  Container prepare terminated with error Get "http://shpyrd-server.shpyrd-system.svc:8082/api/sources/02e64370e017b4b4fe2a419b2d50f5185dfcc3979669c30db3bf466a25babd6b.tgz?token=befd5ebcf717d011134639f0a448cc19c540e2fbb5287d2d3fc81b885fed59b1": dial tcp 10.96.94.251:8082: connect: connection refused`,
			want:    []string{"platform", "not of the app", "again"},
			never:   []string{"shpyrd-server", "token="},
		},
		{
			// 2026-10-04 14:04-14:07, three builds of one app: after the
			// restart the archive it had uploaded was not there any more.
			// Only a new upload (a new deploy) fixes this; a plain retry
			// of the same build never will.
			name:    "uploaded archive gone",
			message: `Error:  Container prepare terminated with error failed to get blob http://shpyrd-server.shpyrd-system.svc:8082/api/sources/78e032a932e1050ed43543e6e432707e8bf748b85e26bb364f11906c337b4fcf.tgz?token=67b6156ac2855e997dfd07066c371a99e5db45e8d89f44408af0bb2462a0bf28: 404 Not Found: {"error":"not found"}`,
			want:    []string{"deploy", "again"},
			never:   []string{"shpyrd-server", "token=", "404"},
		},
		{
			// 2026-10-15 and 2026-10-21: a repository connected before
			// its first push.
			name:    "git repository is empty",
			message: "Error:  Container prepare terminated with error unable to fetch references for repository: remote repository is empty: For more info use `kubectl logs -n p-x x-build-1-build-pod -c prepare`",
			want:    []string{"empty", "push"},
			never:   []string{"could not fetch the source. The build's log"},
		},
		{
			// 2026-10-21: the repository's default branch is not main.
			name:    "git branch does not exist",
			message: `Error:  Container prepare terminated with error unable to fetch references for repository: couldn't find remote ref "main": For more info use ` + "`kubectl logs -n p-x x-build-2-build-pod -c prepare`",
			want:    []string{"main", "branch"},
			never:   []string{"could not fetch the source. The build's log"},
		},
		{
			// 2026-10-25 and 2026-10-26, five builds of three apps: the
			// registry was read-only during its garbage collection. The
			// pod was gone when the controller read it, so only kpack's
			// sentence was left to classify; production said "could not
			// fetch the source".
			name:    "registry refuses the write, pod gone",
			message: `Error:  Container prepare terminated with error Error verifying write access to "10.96.0.50:5000/apps/ws/app": POST https://10.96.0.50:5000/v2/apps/ws/app/blobs/uploads/: unexpected status code 405 Method Not Allowed: Method not allowed`,
			want:    []string{"registry", "platform", "not of the app"},
			never:   []string{"fetch the source", "10.96.0.50"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := KpackFailure(kpackBuild(c.message))
			for _, w := range c.want {
				if !strings.Contains(strings.ToLower(got), strings.ToLower(w)) {
					t.Errorf("lacks %q: %s", w, got)
				}
			}
			for _, n := range c.never {
				if strings.Contains(got, n) {
					t.Errorf("says %q: %s", n, got)
				}
			}
			if bad := PlatformWordingFault(got); bad != "" {
				t.Errorf("names %q: %s", bad, got)
			}
		})
	}
}

// The end of the three build logs still readable on 29 October 2026, and
// two tails typical of the eleven `npm ci` / `npm run build` failures whose
// logs were gone. Each is a first deploy that failed on something a
// sentence can name.
func TestProductionBuildLogsAreExplained(t *testing.T) {
	cases := []struct {
		name, step, out string
		want            []string
		never           []string
	}{
		{
			// A pnpm workspace deployed with a lockfile behind one of its
			// packages. The message named the Node Engine buildpack (the
			// last Paketo header in the log) although the install ran in
			// the platform's own workspace buildpack.
			name: "pnpm lockfile out of date",
			step: "build",
			out: "Paketo Buildpack for Node Engine 6.3.1\n  Resolving Node Engine version\n    Selected Node Engine version (using BP_NODE_VERSION): 22.23.4\n\n" +
				"shpyrd: installing apps/guest with pnpm@9.0.0\nScope: 7 of 18 workspace projects\n" +
				" ERR_PNPM_OUTDATED_LOCKFILE  Cannot install with \"frozen-lockfile\" because pnpm-lock.yaml is not up to date with packages/integrations/channex/package.json\n\n" +
				"Note that in CI environments this setting is true by default. If you still need to run install in such cases, use \"pnpm install --no-frozen-lockfile\"\n\n" +
				"    Failure reason:\n    specifiers in the lockfile ({}) don't match specs in package.json ({\"@types/node\":\"^20.14.0\"})\nERROR: failed to build: exit status 1\n",
			want:  []string{"pnpm-lock.yaml", "package.json"},
			never: []string{"Node Engine buildpack"},
		},
		{
			// The same app, one build later: `pnpm prune` reran the
			// prepare script, which calls husky, a dev dependency just
			// removed. Production said "The build failed."
			name: "prepare script needs a pruned dev dependency",
			step: "build",
			out: "shpyrd: removing dev dependencies\nScope: 7 of 18 workspace projects\nLockfile is up to date, resolution step is skipped\n" +
				".                                        | -497 --------------------------------\n. prepare$ husky\n. prepare: sh: 1: husky: not found\n ELIFECYCLE  Command failed.\nERROR: failed to build: exit status 1\n",
			want:  []string{"prepare", "husky"},
			never: []string{"The build failed. The build's log"},
		},
		{
			// Next.js in a repository whose package.json sits above the
			// deployed directory: Turbopack cannot find next from
			// /workspace/app. A record: the workspace buildpack
			// (build.workspace) now builds such a package, so this build
			// is not expected on production again.
			name: "next.js deployed from a subdirectory of its workspace",
			step: "build",
			out: "Paketo Buildpack for Node Run Script 2.3.58\n  Executing build process\n    Running 'npm run build'\n      > next build\n      ▲ Next.js 16.4.3 (Turbopack)\n" +
				"      > Build error occurred\n      Error: Turbopack build failed with 1 error:\n      ./app\n      Error: Could not find the Next.js package (next/package.json)\n      Resolved from: /workspace/app\n      Filesystem root used for resolution: /workspace\n" +
				"exit status 1\nERROR: failed to build: exit status 1\n",
			want: []string{"while running `npm run build`"},
		},
		{
			// Two first builds failed in `npm ci` with no reason given.
			// This is what npm prints when the lockfile lags package.json,
			// the commonest cause.
			name: "npm lockfile out of sync",
			step: "build",
			out: "Paketo Buildpack for NPM Install 1.6.12\n  Executing build process\n    Running 'npm ci --unsafe-perm --cache /layers/paketo-buildpacks_npm-install/npm-cache'\n" +
				"      npm error code EUSAGE\n      npm error\n      npm error `npm ci` can only install packages when your package.json and package-lock.json or npm-shrinkwrap.json are in sync. Please update your lock file with `npm install` before continuing.\n      npm error\n      npm error Missing: tailwindcss@4.1.0 from lock file\n" +
				"exit status 1\nERROR: failed to build: exit status 1\n",
			want: []string{"package-lock.json", "npm install"},
		},
		{
			// `npm run build` exited 127 on a first deploy: the build tool
			// is not installed, as happens when dev dependencies are left
			// out of the install.
			name: "build tool not installed",
			step: "build",
			out: "Paketo Buildpack for Node Run Script 2.3.58\n  Executing build process\n    Running 'npm run build'\n      > vite build\n      sh: 1: vite: not found\n" +
				"exit status 127\nERROR: failed to build: exit status 127\n",
			want: []string{"vite", "not installed"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := describeBuildFailure(c.step, c.out)
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("lacks %q: %s", w, got)
				}
			}
			for _, n := range c.never {
				if strings.Contains(got, n) {
					t.Errorf("says %q: %s", n, got)
				}
			}
			if bad := PlatformWordingFault(got); bad != "" {
				t.Errorf("names %q: %s", bad, got)
			}
		})
	}
}

// A Dockerfile build whose Job failed without a pod to read: production
// had six of these, and three were annotated with Kubernetes' own words
// ("Job has reached the specified backoff limit"), and one with nothing at
// all. The reader has to hear a sentence about the build.
func TestDockerfileJobFailureIsInWords(t *testing.T) {
	for _, c := range []struct {
		reason, message string
		want            string
	}{
		{"BackoffLimitExceeded", "Job has reached the specified backoff limit", "Dockerfile build failed"},
		{"DeadlineExceeded", "Job was active longer than specified deadline", "30 minutes"},
	} {
		j := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "app-build-40", Namespace: "p-app"},
			Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: c.reason, Message: c.message},
			}},
		}
		got := JobFailureMessage(j)
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: %q lacks %q", c.reason, got, c.want)
		}
		if bad := PlatformWordingFault(got); bad != "" {
			t.Errorf("%s: %q names %q", c.reason, got, bad)
		}
	}
}

// A Dockerfile build's fetch step crashed (production, 30 September 2026:
// "Segmentation fault (core dumped)"); the platform's own tool failing is
// the platform's fault.
func TestDockerfileFetchCrashIsThePlatforms(t *testing.T) {
	got := describeBuildFailure("prepare", "fetching the source failed: Segmentation fault (core dumped)")
	if !strings.Contains(got, "platform") || strings.Contains(got, "Segmentation") {
		t.Errorf("message = %q", got)
	}
}
