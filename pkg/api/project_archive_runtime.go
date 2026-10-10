package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/kexec"
	"github.com/shpyrd-io/shpyrd/pkg/projectarchive"
)

const projectOperationLabel = "shpyrd.io/project-operation"

// A helper that runs the server image in a project namespace pulls it the
// way the server does (#128). The server's own pull Secrets, named by its
// Deployment or its ServiceAccount in the system namespace, are copied
// into the project namespace for one operation: labelled with the
// operation and with the Secret they copy, removed with the operation's
// helpers, never left behind.
const (
	helperPullSecretLabel  = "shpyrd.io/helper-pull-secret"
	serverDeploymentName   = "shpyrd-server"
	helperImagePullMessage = "The platform could not start a helper for this project change; the operator has been told."
)

type limitedErrorWriter struct{ text strings.Builder }

func (w *limitedErrorWriter) Write(p []byte) (int, error) {
	n := len(p)
	if left := 64<<10 - w.text.Len(); left > 0 {
		w.text.Write(p[:min(left, len(p))])
	}
	return n, nil
}

// archiveCommand runs one command in a helper. What the helper says on its
// error stream is kept: on failure it is part of the error; on success each
// line it marked as a warning goes to warn, when given, for the operation's
// report (#129).
func (s *Server) archiveCommand(namespace, pod, container string, warn func(string)) projectarchive.Command {
	return func(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
		if stdin == nil {
			stdin = strings.NewReader("")
		}
		if stdout == nil {
			stdout = io.Discard
		}
		var stderr limitedErrorWriter
		err := kexec.StreamIO(ctx, s.kube, kexec.ExecURL(s.kube, namespace, pod, container, args, false), false, stdin, stdout, &stderr, nil)
		if err != nil {
			return fmt.Errorf("%s: %w: %s", args[0], err, strings.TrimSpace(stderr.text.String()))
		}
		if warn != nil {
			archiveWarnings(stderr.text.String(), warn)
		}
		return nil
	}
}

// archiveWarnings hands warn every warning line of a helper's error stream.
func archiveWarnings(stderr string, warn func(string)) {
	for _, line := range strings.Split(stderr, "\n") {
		if sentence, ok := strings.CutPrefix(line, projectarchive.WarningPrefix); ok {
			warn(strings.TrimSpace(sentence))
		}
	}
}

func (s *Server) archiveDatabase(ctx context.Context, namespace, name string) (projectarchive.PostgreSQL, error) {
	var pods corev1.PodList
	if err := s.apps.List(ctx, &pods, client.InNamespace(namespace), client.MatchingLabels{"cnpg.io/cluster": name, "cnpg.io/instanceRole": "primary"}); err != nil {
		return projectarchive.PostgreSQL{}, err
	}
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp == nil && pod.Status.Phase == corev1.PodRunning {
			return projectarchive.PostgreSQL{Exec: s.archiveCommand(namespace, pod.Name, "postgres", nil)}, nil
		}
	}
	return projectarchive.PostgreSQL{}, fmt.Errorf("database %s has no running primary", name)
}

func archiveVolumePod(app *shpyrdv1.App, volume *shpyrdv1.Volume, operation, image string, pullSecrets []corev1.LocalObjectReference) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "project-archive-", Namespace: app.Namespace, Labels: map[string]string{projectOperationLabel: operation, shpyrdv1.LabelApp: app.Name, "shpyrd.io/archive-volume": volume.Name}},
		Spec: corev1.PodSpec{
			ImagePullSecrets:             pullSecrets,
			AutomountServiceAccountToken: ptr.To(false), EnableServiceLinks: ptr.To(false), RestartPolicy: corev1.RestartPolicyNever, TerminationGracePeriodSeconds: ptr.To[int64](5),
			SecurityContext: &corev1.PodSecurityContext{RunAsUser: ptr.To[int64](0), RunAsGroup: ptr.To[int64](0), RunAsNonRoot: ptr.To(false), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
			Containers: []corev1.Container{{Name: "archive", Image: image, Command: []string{"/shpyrd-server", "project-volume", "wait"},
				SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}, Add: []corev1.Capability{"CHOWN", "DAC_OVERRIDE", "FOWNER"}}},
				Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("64Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")}},
				VolumeMounts:    []corev1.VolumeMount{{Name: "data", MountPath: "/data"}}}},
			Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: volume.PVCName()}}}},
		},
	}
}

// The helpers take warn, the operation's report, for what the helper says
// about the volume without failing (#129); nil drops it.
func (s *Server) archiveVolumeHelper(ctx context.Context, app *shpyrdv1.App, volume *shpyrdv1.Volume, operation string, warn func(string)) (projectarchive.Command, error) {
	return s.archiveClaimHelper(ctx, app, volume.PVCName(), volume.Name, operation, "", warn)
}

func (s *Server) archiveClaimHelper(ctx context.Context, app *shpyrdv1.App, claim, key, operation, node string, warn func(string)) (projectarchive.Command, error) {
	return s.projectClaimHelper(ctx, app, claim, key, operation, node, false, warn)
}

func (s *Server) projectClaimHelper(ctx context.Context, app *shpyrdv1.App, claim, key, operation, node string, readOnly bool, warn func(string)) (projectarchive.Command, error) {
	volume := &shpyrdv1.Volume{ObjectMeta: metav1.ObjectMeta{Name: key, Namespace: app.Namespace}}

	var helpers corev1.PodList
	if err := s.apps.List(ctx, &helpers, client.InNamespace(app.Namespace), client.MatchingLabels{projectOperationLabel: operation, "shpyrd.io/archive-volume": volume.Name}); err != nil {
		return nil, err
	}
	var pod *corev1.Pod
	for i := range helpers.Items {
		candidate := &helpers.Items[i]
		if candidate.DeletionTimestamp == nil && candidate.Status.Phase != corev1.PodFailed && candidate.Status.Phase != corev1.PodSucceeded {
			pod = candidate
			break
		}
	}
	if pod == nil {
		image := s.vars(install.VarServerImage)
		if image == "" {
			return nil, errors.New("server image is not configured for volume archive helpers")
		}
		pullSecrets, err := s.helperPullSecrets(ctx, app.Namespace, operation)
		if err != nil {
			return nil, err
		}
		pod = archiveVolumePod(app, volume, operation, image, pullSecrets)
		pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName = claim
		if readOnly {
			pod.Spec.Volumes[0].PersistentVolumeClaim.ReadOnly = true
			pod.Spec.Containers[0].VolumeMounts[0].ReadOnly = true
			pod.Spec.Containers[0].SecurityContext.Capabilities.Add = []corev1.Capability{"DAC_READ_SEARCH"}
			pod.Spec.ActiveDeadlineSeconds = ptr.To[int64](600)
			if strings.HasPrefix(key, "source-move-") {
				pod.Spec.ActiveDeadlineSeconds = ptr.To[int64](3600)
			}
		}
		if pool := s.vars("SHPYRD_APPS_POOL"); pool != "" && claim == shpyrdv1.PVCPrefix+key {
			pod.Spec.NodeSelector = map[string]string{controller.PoolLabel: pool}
		}
		// A final cleanup may run after the workload has resumed. For an
		// RWO provider disk the helper must use its live consumer's node.
		var consumers corev1.PodList
		if err := s.apps.List(ctx, &consumers, client.InNamespace(app.Namespace)); err != nil {
			return nil, err
		}
		for _, consumer := range consumers.Items {
			if consumer.DeletionTimestamp != nil || consumer.Spec.NodeName == "" || consumer.Status.Phase != corev1.PodRunning {
				continue
			}
			for _, mounted := range consumer.Spec.Volumes {
				if mounted.PersistentVolumeClaim != nil && mounted.PersistentVolumeClaim.ClaimName == claim {
					pod.Spec.NodeName = consumer.Spec.NodeName
				}
			}
		}
		if node != "" {
			pod.Spec.NodeName = ""
			pod.Spec.NodeSelector = map[string]string{corev1.LabelHostname: node}
		}
		if err := s.apps.Create(ctx, pod); err != nil {
			return nil, err
		}
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := s.apps.Get(ctx, client.ObjectKeyFromObject(pod), pod); err != nil {
			return nil, err
		}
		if pod.DeletionTimestamp != nil {
			return nil, errors.New("volume helper is terminating; retry recovery after it stops")
		}
		if pod.Status.Phase == corev1.PodRunning {
			return s.archiveCommand(app.Namespace, pod.Name, "archive", warn), nil
		}
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			return nil, fmt.Errorf("volume helper stopped: %s", pod.Status.Message)
		}
		// A helper that cannot pull its image never starts; the kubelet
		// would retry for minutes while the project stays paused (#128).
		if reason, message := imagePullFault(pod); reason != "" {
			s.log.Error("project helper cannot pull the server image", "namespace", pod.Namespace, "pod", pod.Name, "image", pod.Spec.Containers[0].Image, "reason", reason, "message", message)
			return nil, errors.New(helperImagePullMessage)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Server) cleanupArchiveHelpers(ctx context.Context, namespace, operation string) error {
	var pods corev1.PodList
	if err := s.apps.List(ctx, &pods, client.InNamespace(namespace), client.MatchingLabels{projectOperationLabel: operation}); err != nil {
		return err
	}
	for i := range pods.Items {
		if err := s.apps.Delete(ctx, &pods.Items[i]); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	// Do not start workloads or create a replacement helper while an old
	// helper can still be writing to the same claim.
	if err := s.waitArchiveCondition(ctx, func(ctx context.Context) (bool, error) {
		var remaining corev1.PodList
		if err := s.apps.List(ctx, &remaining, client.InNamespace(namespace), client.MatchingLabels{projectOperationLabel: operation}); err != nil {
			return false, err
		}
		return len(remaining.Items) == 0, nil
	}); err != nil {
		return err
	}
	// The pull Secrets copied for the helpers go with them; the operation's
	// own record carries the same operation label and stays.
	var secrets corev1.SecretList
	if err := s.apps.List(ctx, &secrets, client.InNamespace(namespace), client.MatchingLabels{projectOperationLabel: operation}, client.HasLabels{helperPullSecretLabel}); err != nil {
		return err
	}
	for i := range secrets.Items {
		if err := s.apps.Delete(ctx, &secrets.Items[i]); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// imagePullFault reports the first container of the pod the kubelet cannot
// pull an image for, with the kubelet's own sentence for the log.
func imagePullFault(pod *corev1.Pod) (reason, message string) {
	for _, statuses := range [][]corev1.ContainerStatus{pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses} {
		for _, status := range statuses {
			if waiting := status.State.Waiting; waiting != nil {
				switch waiting.Reason {
				case "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
					return waiting.Reason, waiting.Message
				}
			}
		}
	}
	return "", ""
}

// helperPullSecrets copies the Secrets the server pulls its image with
// into the project namespace for this operation and returns what a helper
// in that namespace references them as. Without a server Deployment (a
// test, a run outside the cluster) or without pull Secrets on it (a public
// image), the helper references none, as before.
func (s *Server) helperPullSecrets(ctx context.Context, namespace, operation string) ([]corev1.LocalObjectReference, error) {
	system := s.deps().SystemNamespace
	deployment := &appsv1.Deployment{}
	if err := s.apps.Get(ctx, types.NamespacedName{Namespace: system, Name: serverDeploymentName}, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	seen := map[string]bool{}
	collect := func(refs []corev1.LocalObjectReference) {
		for _, ref := range refs {
			if ref.Name != "" && !seen[ref.Name] {
				seen[ref.Name] = true
				names = append(names, ref.Name)
			}
		}
	}
	collect(deployment.Spec.Template.Spec.ImagePullSecrets)
	// The cloud profile puts the Secret on the ServiceAccount, so that the
	// admission controller adds it to every pod the account runs.
	if account := deployment.Spec.Template.Spec.ServiceAccountName; account != "" {
		sa := &corev1.ServiceAccount{}
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: system, Name: account}, sa); err == nil {
			collect(sa.ImagePullSecrets)
		} else if !apierrors.IsNotFound(err) {
			return nil, err
		}
	}
	var refs []corev1.LocalObjectReference
	for i, name := range names {
		source := &corev1.Secret{}
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: system, Name: name}, source); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		copied := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("shpyrd-pull-%s-%d", operation[:min(16, len(operation))], i), Namespace: namespace, Labels: map[string]string{projectOperationLabel: operation, helperPullSecretLabel: name}},
			Type:       source.Type,
			Data:       source.Data,
		}
		// A second helper of the same operation finds the copy in place.
		if err := s.apps.Create(ctx, copied); err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
		refs = append(refs, corev1.LocalObjectReference{Name: copied.Name})
	}
	return refs, nil
}

func (s *Server) clearArchiveDatabaseMaintenance(ctx context.Context, namespace string, op *projectArchiveOperation) error {
	names := map[string]bool{}
	for _, pg := range op.Original.Databases {
		names[pg.Name] = true
	}
	for _, name := range op.CreatedDatabases {
		names[name] = true
	}
	for name := range op.Limits {
		names[name] = true
	}
	for name := range names {
		pg := &shpyrdv1.Postgres{}
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, pg); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if pg.Annotations[shpyrdv1.AnnotationMaintenance] != op.ID {
			continue
		}
		before := pg.DeepCopy()
		delete(pg.Annotations, shpyrdv1.AnnotationMaintenance)
		if err := s.apps.Patch(ctx, pg, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) setDatabaseMaintenance(ctx context.Context, namespace, name, value string) error {
	pg := &shpyrdv1.Postgres{}
	if err := s.apps.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, pg); err != nil {
		if apierrors.IsNotFound(err) && value == "" {
			return nil
		}
		return err
	}
	before := pg.DeepCopy()
	if pg.Annotations == nil {
		pg.Annotations = map[string]string{}
	}
	if value == "" {
		delete(pg.Annotations, shpyrdv1.AnnotationMaintenance)
	} else {
		pg.Annotations[shpyrdv1.AnnotationMaintenance] = value
	}
	return s.apps.Patch(ctx, pg, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}

func addArchiveStream(ctx context.Context, b *projectarchive.Bundle, name string, write func(io.Writer) error) error {
	r, w := io.Pipe()
	finished := make(chan error, 1)
	go func() { err := write(w); w.CloseWithError(err); finished <- err }()
	err := b.Add(ctx, name, r)
	r.CloseWithError(err)
	return errors.Join(err, <-finished)
}

func (s *Server) exportProjectGit(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation, b *projectarchive.Bundle, m *projectArchiveMetadata) error {
	if m.Spec.Source == nil || m.Spec.Source.Git == nil {
		return nil
	}
	revision := m.Spec.Source.Git.Revision
	if release := app.CurrentRelease(); release != nil && gitArchiveRevision.MatchString(release.Source) {
		revision = release.Source
	}
	if !gitArchiveRevision.MatchString(revision) {
		return errors.New("the deployed Git commit is not recorded; refusing to export a potentially different revision")
	}
	image := s.vars("SHPYRD_BUILDKIT_IMAGE")
	if image == "" {
		image = controller.DefaultBuildKitImage
	}
	pod := projectSourcePod(app, op.ID, image)
	if err := s.apps.Create(ctx, pod); err != nil {
		return err
	}
	if err := s.waitArchiveCondition(ctx, func(c context.Context) (bool, error) {
		if err := s.apps.Get(c, client.ObjectKeyFromObject(pod), pod); err != nil {
			return false, err
		}
		if pod.Status.Phase == corev1.PodFailed {
			return false, errors.New("source export helper failed")
		}
		return pod.Status.Phase == corev1.PodRunning, nil
	}); err != nil {
		return err
	}
	command := s.archiveCommand(app.Namespace, pod.Name, "source", nil)
	m.SourceEntry = "source/source.tgz"
	return addArchiveStream(ctx, b, m.SourceEntry, func(out io.Writer) error {
		return command(ctx, []string{"sh", "-ec", `export GIT_ALLOW_PROTOCOL=https:http:ssh
git clone --quiet --no-checkout -- "$1" /source/repo
git -C /source/repo archive --format=tar.gz "$2"`, "archive", m.Spec.Source.Git.URL, revision}, nil, out)
	})
}

// projectSourcePod holds the deployed Git commit while an export reads it.
// It asks for CPU as well as memory: a workspace's quota refuses a pod that
// asks for one and not the other (#117).
func projectSourcePod(app *shpyrdv1.App, operation, image string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{GenerateName: "project-source-", Namespace: app.Namespace, Labels: map[string]string{projectOperationLabel: operation, shpyrdv1.LabelApp: app.Name}}, Spec: corev1.PodSpec{
		AutomountServiceAccountToken: ptr.To(false), EnableServiceLinks: ptr.To(false), RestartPolicy: corev1.RestartPolicyNever, TerminationGracePeriodSeconds: ptr.To[int64](5),
		SecurityContext: &corev1.PodSecurityContext{RunAsUser: ptr.To[int64](1000), RunAsGroup: ptr.To[int64](1000), FSGroup: ptr.To[int64](1000)},
		Containers:      []corev1.Container{{Name: "source", Image: image, Command: []string{"sh", "-ec", "trap 'exit 0' TERM; while :; do sleep 3600; done"}, SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}, VolumeMounts: []corev1.VolumeMount{{Name: "source", MountPath: "/source"}}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("64Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("512Mi")}}}},
		Volumes:         []corev1.Volume{{Name: "source", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
	}}
}
