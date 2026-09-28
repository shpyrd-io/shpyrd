package install

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

type nopReporter struct{}

func (nopReporter) Runlevel(string, []string)  {}
func (nopReporter) Step(string, string)        {}
func (nopReporter) Done(string, time.Duration) {}
func (nopReporter) Failed(string, error)       {}

// The autoscaler's OCI config is the SDK's INI shape with the key next to it.
func TestOCIINIConfig(t *testing.T) {
	ini := ociINIConfig("ocid1.user..u", "aa:bb", "ocid1.tenancy..t", "us-ashburn-1")
	for _, want := range []string{"[DEFAULT]", "user=ocid1.user..u", "fingerprint=aa:bb", "tenancy=ocid1.tenancy..t", "region=us-ashburn-1", "key_file=/etc/oci/oci_api_key.pem"} {
		if !strings.Contains(ini, want) {
			t.Errorf("ini lacks %q:\n%s", want, ini)
		}
	}
}

// The image pull secret is created from the Docker config given, typed so
// kubelet accepts it, and updated (not duplicated) on a second run.
func TestImagePullSecretHook(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewClientset()
	e := &Engine{kube: &kube.Client{Kube: cs}, rep: nopReporter{}, vars: map[string]string{}}
	c := &Component{Name: "registry-credentials", Namespace: "shpyrd-system"}

	// Nothing given: nothing created, no error (the OSS platform pulls
	// public images).
	if err := imagePullSecretHook(ctx, e, c); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.CoreV1().Secrets("shpyrd-system").Get(ctx, ImagePullSecretName, metav1.GetOptions{}); err == nil {
		t.Fatal("no config given, yet a secret was created")
	}

	e.opts.ImagePullConfig = []byte(`{"auths":{"iad.ocir.io":{"auth":"dXNlcjpwYXNz"}}}`)
	if err := imagePullSecretHook(ctx, e, c); err != nil {
		t.Fatal(err)
	}
	sec, err := cs.CoreV1().Secrets("shpyrd-system").Get(ctx, ImagePullSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if sec.Type != corev1.SecretTypeDockerConfigJson || string(sec.Data[corev1.DockerConfigJsonKey]) != string(e.opts.ImagePullConfig) {
		t.Errorf("secret type=%s data=%s", sec.Type, sec.Data[corev1.DockerConfigJsonKey])
	}
	// Second run with a rotated token replaces it.
	e.opts.ImagePullConfig = []byte(`{"auths":{"iad.ocir.io":{"auth":"bmV3"}}}`)
	if err := imagePullSecretHook(ctx, e, c); err != nil {
		t.Fatal(err)
	}
	sec, _ = cs.CoreV1().Secrets("shpyrd-system").Get(ctx, ImagePullSecretName, metav1.GetOptions{})
	if !strings.Contains(string(sec.Data[corev1.DockerConfigJsonKey]), "bmV3") {
		t.Error("second run did not update the secret")
	}
}

// Without a node pool the autoscaler hook is a no-op; with one and no key
// it must say what is missing rather than install a broken autoscaler.
func TestAutoscalerCredentialsHookGating(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewClientset()
	e := &Engine{kube: &kube.Client{Kube: cs}, rep: nopReporter{}, vars: map[string]string{}}
	c := &Component{Name: "cluster-autoscaler", Namespace: "kube-system"}
	if err := autoscalerCredentialsHook(ctx, e, c); err != nil {
		t.Fatalf("no node pool: %v", err)
	}
	e.vars[VarNodePoolID] = "ocid1.nodepool..p"
	err := autoscalerCredentialsHook(ctx, e, c)
	if err == nil || !strings.Contains(err.Error(), "--dns-key-file") {
		t.Fatalf("node pool without a key must name --dns-key-file, got %v", err)
	}
}
