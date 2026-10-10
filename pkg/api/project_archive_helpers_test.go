package api

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/install"
)

// helperFixture starts a project helper against the fake cluster and hands
// back the pod once the API has created it; the kubelet's part (starting
// it, or failing to pull its image) is the test's.
func helperFixture(t *testing.T, objects ...client.Object) (*Server, *shpyrdv1.App, string, *corev1.Pod, <-chan error) {
	t.Helper()
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "p-shop"}}
	operation := "0123456789abcdef0123456789abcdef"
	record := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: projectOperationSecret, Namespace: app.Namespace, Labels: map[string]string{shpyrdv1.LabelApp: app.Name, projectOperationLabel: operation}}}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "vol-data", Namespace: app.Namespace}}
	s, _ := newTestServer(t, nil, append([]client.Object{app, record, claim}, objects...))
	s.opts.Vars = func(key string) string {
		if key == install.VarServerImage {
			return "registry.test/shpyrd/server:dev"
		}
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		_, err := s.projectClaimHelper(ctx, app, claim.Name, "data", operation, "", true, nil)
		done <- err
	}()
	var pods corev1.PodList
	if err := s.waitArchiveCondition(ctx, func(c context.Context) (bool, error) {
		if err := s.apps.List(c, &pods, client.InNamespace(app.Namespace), client.MatchingLabels{projectOperationLabel: operation}); err != nil {
			return false, err
		}
		return len(pods.Items) == 1, nil
	}); err != nil {
		t.Fatalf("helper not created: %v", err)
	}
	return s, app, operation, &pods.Items[0], done
}

func setHelperStatus(t *testing.T, s *Server, pod *corev1.Pod, status corev1.PodStatus) {
	t.Helper()
	if err := s.apps.Get(context.Background(), client.ObjectKeyFromObject(pod), pod); err != nil {
		t.Fatal(err)
	}
	pod.Status = status
	if err := s.apps.Status().Update(context.Background(), pod); err != nil {
		t.Fatal(err)
	}
}

// #128: a helper that runs the server image in a project namespace pulls
// it the way the server does. The server's pull Secret is copied for the
// operation, referenced by the helper, and removed with the helpers; the
// operation's own record, under the same operation label, stays.
func TestProjectHelperCarriesTheServersPullSecret(t *testing.T) {
	pull := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: install.ImagePullSecretName, Namespace: "shpyrd-system"}, Type: corev1.SecretTypeDockerConfigJson, Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{"registry.test":{"auth":"dGVzdA=="}}}`)}}
	server := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: serverDeploymentName, Namespace: "shpyrd-system"}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
		ServiceAccountName: "shpyrd-server",
		ImagePullSecrets:   []corev1.LocalObjectReference{{Name: install.ImagePullSecretName}},
		Containers:         []corev1.Container{{Name: "server", Image: "registry.test/shpyrd/server:dev"}},
	}}}}
	// The cloud profile names the same Secret on the ServiceAccount too.
	account := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "shpyrd-server", Namespace: "shpyrd-system"}, ImagePullSecrets: []corev1.LocalObjectReference{{Name: install.ImagePullSecretName}}}
	s, app, operation, pod, done := helperFixture(t, pull, server, account)
	ctx := context.Background()

	if len(pod.Spec.ImagePullSecrets) != 1 || pod.Spec.ImagePullSecrets[0].Name != "shpyrd-pull-0123456789abcdef-0" {
		t.Fatalf("helper pull secrets = %v", pod.Spec.ImagePullSecrets)
	}
	copied := &corev1.Secret{}
	if err := s.apps.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: pod.Spec.ImagePullSecrets[0].Name}, copied); err != nil {
		t.Fatalf("pull secret not copied into the project: %v", err)
	}
	if copied.Type != pull.Type || string(copied.Data[corev1.DockerConfigJsonKey]) != string(pull.Data[corev1.DockerConfigJsonKey]) {
		t.Fatal("copy differs from the server's pull secret")
	}
	if copied.Labels[projectOperationLabel] != operation || copied.Labels[helperPullSecretLabel] != install.ImagePullSecretName {
		t.Fatalf("copy labels = %v", copied.Labels)
	}

	setHelperStatus(t, s, pod, corev1.PodStatus{Phase: corev1.PodRunning})
	if err := <-done; err != nil {
		t.Fatalf("running helper refused: %v", err)
	}
	if err := s.cleanupArchiveHelpers(ctx, app.Namespace, operation); err != nil {
		t.Fatal(err)
	}
	var secrets corev1.SecretList
	if err := s.apps.List(ctx, &secrets, client.InNamespace(app.Namespace)); err != nil {
		t.Fatal(err)
	}
	if len(secrets.Items) != 1 || secrets.Items[0].Name != projectOperationSecret {
		names := []string{}
		for _, sec := range secrets.Items {
			names = append(names, sec.Name)
		}
		t.Fatalf("after cleanup the project holds %v; want only the operation record", names)
	}
}

// Without pull Secrets on the server (a public image, a local install) the
// helper references none and nothing is copied.
func TestProjectHelperWithoutPullSecretsCopiesNothing(t *testing.T) {
	server := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: serverDeploymentName, Namespace: "shpyrd-system"}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
		ServiceAccountName: "shpyrd-server",
		Containers:         []corev1.Container{{Name: "server", Image: "ghcr.io/shpyrd-io/shpyrd:dev"}},
	}}}}
	s, app, _, pod, done := helperFixture(t, server)
	if len(pod.Spec.ImagePullSecrets) != 0 {
		t.Fatalf("helper pull secrets = %v, want none", pod.Spec.ImagePullSecrets)
	}
	var secrets corev1.SecretList
	if err := s.apps.List(context.Background(), &secrets, client.InNamespace(app.Namespace), client.HasLabels{helperPullSecretLabel}); err != nil {
		t.Fatal(err)
	}
	if len(secrets.Items) != 0 {
		t.Fatalf("copied %d pull secrets for a server without any", len(secrets.Items))
	}
	setHelperStatus(t, s, pod, corev1.PodStatus{Phase: corev1.PodRunning})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// A helper whose image cannot be pulled never starts; the operation fails
// at once with a sentence for people instead of waiting on the kubelet's
// back-off for minutes while the project stays paused (#128).
func TestProjectHelperFailsFastWhenTheImageCannotBePulled(t *testing.T) {
	for _, reason := range []string{"ErrImagePull", "ImagePullBackOff"} {
		t.Run(reason, func(t *testing.T) {
			s, _, _, pod, done := helperFixture(t)
			setHelperStatus(t, s, pod, corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "archive",
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason, Message: `Back-off pulling image "registry.test/shpyrd/server:dev": rpc error: unauthorized`}},
			}}})
			select {
			case err := <-done:
				if err == nil || err.Error() != helperImagePullMessage {
					t.Fatalf("error = %v, want the worded message", err)
				}
				if fault := controller.PlatformWordingFault(err.Error()); fault != "" {
					t.Errorf("message names %q", fault)
				}
				if strings.Contains(err.Error(), "registry.test") || errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("message leaks the kubelet's sentence: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("helper kept waiting on an image that cannot be pulled")
			}
		})
	}
}
