package install

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

type nopReporter struct{}

func (nopReporter) Runlevel(string, []string)  {}
func (nopReporter) Step(string, string)        {}
func (nopReporter) Done(string, time.Duration) {}
func (nopReporter) Failed(string, error)       {}

func TestRegistryCredentialsAreIndependentOfPlatformBackupCredentials(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewClientset()
	e := &Engine{kube: &kube.Client{Kube: cs}, rep: nopReporter{}, vars: map[string]string{VarRegistryBucket: "images", VarRegistryRegion: "region"}}
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("Secret"), meta.RESTScopeNamespace)
	dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	dyn.PrependReactor("patch", "secrets", func(action ktesting.Action) (bool, runtime.Object, error) {
		patch := action.(ktesting.PatchAction).GetPatch()
		var sec corev1.Secret
		if err := json.Unmarshal(patch, &sec); err != nil {
			return true, nil, err
		}
		secrets := cs.CoreV1().Secrets(sec.Namespace)
		if _, err := secrets.Get(ctx, sec.Name, metav1.GetOptions{}); err != nil {
			if _, err := secrets.Create(ctx, &sec, metav1.CreateOptions{}); err != nil {
				return true, nil, err
			}
		} else if _, err := secrets.Update(ctx, &sec, metav1.UpdateOptions{}); err != nil {
			return true, nil, err
		}
		obj := &unstructured.Unstructured{}
		return true, obj, json.Unmarshal(patch, &obj.Object)
	})
	e.kube.Mapper, e.kube.Dynamic = mapper, dyn
	e.applier = &applier{kube: e.kube}
	e.opts.BackupCredentials = map[string]string{"AWS_ACCESS_KEY_ID": "backup-key", "AWS_SECRET_ACCESS_KEY": "backup-secret"}
	c := &Component{Name: "registry", Namespace: "shpyrd-system"}
	if err := registryS3Hook(ctx, e, c); err == nil {
		t.Fatal("registry silently reused the backup credential")
	}
	e.opts.RegistryCredentials = map[string]string{"AWS_ACCESS_KEY_ID": "registry-key"}
	if err := registryS3Hook(ctx, e, c); err == nil {
		t.Fatal("accepted an incomplete registry credential")
	}
	e.opts.RegistryCredentials["AWS_SECRET_ACCESS_KEY"] = "registry-secret"
	if err := registryS3Hook(ctx, e, c); err != nil {
		t.Fatal(err)
	}
	sec, err := cs.CoreV1().Secrets(c.Namespace).Get(ctx, RegistryS3SecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(sec.Data["AWS_ACCESS_KEY_ID"]) != "registry-key" {
		t.Error("wrong registry credential")
	}
	e.opts.RegistryCredentials["AWS_ACCESS_KEY_ID"] = "rotated-key"
	if err := registryS3Hook(ctx, e, c); err != nil {
		t.Fatal(err)
	}
	sec, _ = cs.CoreV1().Secrets(c.Namespace).Get(ctx, RegistryS3SecretName, metav1.GetOptions{})
	if string(sec.Data["AWS_ACCESS_KEY_ID"]) != "rotated-key" {
		t.Fatal("registry credential was not rotated")
	}
	e.opts.RegistryCredentials = nil
	if err := registryS3Hook(ctx, e, c); err != nil {
		t.Fatalf("did not keep existing registry credential: %v", err)
	}
	if e.opts.BackupCredentials["AWS_ACCESS_KEY_ID"] != "backup-key" {
		t.Fatal("registry overwrote backup credential")
	}
}

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

func TestSourceSigningMigratesExistingAppsOnlyOnce(t *testing.T) {
	ctx := context.Background()
	ns := "shpyrd-system"
	name := strings.Repeat("a", 64) + ".tgz"
	gvr := schema.GroupVersionResource{Group: "shpyrd.io", Version: "v1alpha1", Resource: "apps"}
	app := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "shpyrd.io/v1alpha1", "kind": "App", "metadata": map[string]any{"name": "shop", "namespace": "app-shop"}, "spec": map[string]any{"source": map[string]any{"blob": map[string]any{"url": "http://shpyrd-server." + ns + ".svc/api/sources/" + name}}}}}
	cs := fake.NewClientset()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: "AppList"}, app)
	dyn.PrependReactor("patch", "secrets", func(a ktesting.Action) (bool, runtime.Object, error) {
		var sec corev1.Secret
		if err := json.Unmarshal(a.(ktesting.PatchAction).GetPatch(), &sec); err != nil {
			return true, nil, err
		}
		secrets := cs.CoreV1().Secrets(sec.Namespace)
		if _, err := secrets.Get(ctx, sec.Name, metav1.GetOptions{}); err != nil {
			_, err = secrets.Create(ctx, &sec, metav1.CreateOptions{})
			if err != nil {
				return true, nil, err
			}
		} else {
			_, err = secrets.Update(ctx, &sec, metav1.UpdateOptions{})
			if err != nil {
				return true, nil, err
			}
		}
		raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&sec)
		return true, &unstructured.Unstructured{Object: raw}, err
	})
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("Secret"), meta.RESTScopeNamespace)
	k := &kube.Client{Kube: cs, Dynamic: dyn, Mapper: mapper}
	e := &Engine{kube: k, applier: &applier{kube: k}, rep: nopReporter{}}
	component := &Component{Name: ServerComponent, Namespace: ns}
	if err := sourcesSigningHook(ctx, e, component); err != nil {
		t.Fatal(err)
	}
	signed, err := dyn.Resource(gvr).Namespace("app-shop").Get(ctx, "shop", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _, _ := unstructured.NestedString(signed.Object, "spec", "source", "blob", "url")
	if !strings.Contains(raw, "?token=") {
		t.Fatal("old source was not signed")
	}
	// A later App edit cannot use the install hook as a signing oracle.
	unsigned := "http://shpyrd-server." + ns + ".svc/api/sources/" + strings.Repeat("b", 64) + ".tgz"
	_ = unstructured.SetNestedField(signed.Object, unsigned, "spec", "source", "blob", "url")
	if _, err := dyn.Resource(gvr).Namespace("app-shop").Update(ctx, signed, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := sourcesSigningHook(ctx, e, component); err != nil {
		t.Fatal(err)
	}
	after, _ := dyn.Resource(gvr).Namespace("app-shop").Get(ctx, "shop", metav1.GetOptions{})
	raw, _, _ = unstructured.NestedString(after.Object, "spec", "source", "blob", "url")
	if raw != unsigned {
		t.Fatal("migration signed a new untrusted URL")
	}
}
