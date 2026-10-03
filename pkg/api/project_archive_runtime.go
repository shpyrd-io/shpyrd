package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

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

type limitedErrorWriter struct{ text strings.Builder }

func (w *limitedErrorWriter) Write(p []byte) (int, error) {
	n := len(p)
	if left := 8192 - w.text.Len(); left > 0 {
		w.text.Write(p[:min(left, len(p))])
	}
	return n, nil
}

func (s *Server) archiveCommand(namespace, pod, container string) projectarchive.Command {
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
		return nil
	}
}

func (s *Server) archiveDatabase(ctx context.Context, namespace, name string) (projectarchive.PostgreSQL, error) {
	var pods corev1.PodList
	if err := s.apps.List(ctx, &pods, client.InNamespace(namespace), client.MatchingLabels{"cnpg.io/cluster": name, "cnpg.io/instanceRole": "primary"}); err != nil {
		return projectarchive.PostgreSQL{}, err
	}
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp == nil && pod.Status.Phase == corev1.PodRunning {
			return projectarchive.PostgreSQL{Exec: s.archiveCommand(namespace, pod.Name, "postgres")}, nil
		}
	}
	return projectarchive.PostgreSQL{}, fmt.Errorf("database %s has no running primary", name)
}

func archiveVolumePod(app *shpyrdv1.App, volume *shpyrdv1.Volume, operation, image string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "project-archive-", Namespace: app.Namespace, Labels: map[string]string{projectOperationLabel: operation, shpyrdv1.LabelApp: app.Name, "shpyrd.io/archive-volume": volume.Name}},
		Spec: corev1.PodSpec{
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

func (s *Server) archiveVolumeHelper(ctx context.Context, app *shpyrdv1.App, volume *shpyrdv1.Volume, operation string) (projectarchive.Command, error) {
	return s.archiveClaimHelper(ctx, app, volume.PVCName(), volume.Name, operation, "")
}

func (s *Server) archiveClaimHelper(ctx context.Context, app *shpyrdv1.App, claim, key, operation, node string) (projectarchive.Command, error) {
	return s.projectClaimHelper(ctx, app, claim, key, operation, node, false)
}

func (s *Server) projectClaimHelper(ctx context.Context, app *shpyrdv1.App, claim, key, operation, node string, readOnly bool) (projectarchive.Command, error) {
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
		pod = archiveVolumePod(app, volume, operation, image)
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
			return s.archiveCommand(app.Namespace, pod.Name, "archive"), nil
		}
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			return nil, fmt.Errorf("volume helper stopped: %s", pod.Status.Message)
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
	return s.waitArchiveCondition(ctx, func(ctx context.Context) (bool, error) {
		var remaining corev1.PodList
		if err := s.apps.List(ctx, &remaining, client.InNamespace(namespace), client.MatchingLabels{projectOperationLabel: operation}); err != nil {
			return false, err
		}
		return len(remaining.Items) == 0, nil
	})
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
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{GenerateName: "project-source-", Namespace: app.Namespace, Labels: map[string]string{projectOperationLabel: op.ID, shpyrdv1.LabelApp: app.Name}}, Spec: corev1.PodSpec{
		AutomountServiceAccountToken: ptr.To(false), EnableServiceLinks: ptr.To(false), RestartPolicy: corev1.RestartPolicyNever, TerminationGracePeriodSeconds: ptr.To[int64](5),
		SecurityContext: &corev1.PodSecurityContext{RunAsUser: ptr.To[int64](1000), RunAsGroup: ptr.To[int64](1000), FSGroup: ptr.To[int64](1000)},
		Containers:      []corev1.Container{{Name: "source", Image: image, Command: []string{"sh", "-ec", "trap 'exit 0' TERM; while :; do sleep 3600; done"}, SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}, VolumeMounts: []corev1.VolumeMount{{Name: "source", MountPath: "/source"}}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("512Mi")}}}},
		Volumes:         []corev1.Volume{{Name: "source", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
	}}
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
	command := s.archiveCommand(app.Namespace, pod.Name, "source")
	m.SourceEntry = "source/source.tgz"
	return addArchiveStream(ctx, b, m.SourceEntry, func(out io.Writer) error {
		return command(ctx, []string{"sh", "-ec", `export GIT_ALLOW_PROTOCOL=https:http:ssh
git clone --quiet --no-checkout -- "$1" /source/repo
git -C /source/repo archive --format=tar.gz "$2"`, "archive", m.Spec.Source.Git.URL, revision}, nil, out)
	})
}
