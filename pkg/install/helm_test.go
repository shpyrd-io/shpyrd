package install

import (
	"context"
	"testing"

	"helm.sh/helm/v3/pkg/chart"
)

// `cluster export` renders a chart that needs a recent Kubernetes, as
// cert-manager does (>= 1.22), for the Kubernetes the profile runs:
// without a cluster, Helm as a library would assume v1.20.0 and refuse it.
func TestExportRendersChartsThatNeedARecentKubernetes(t *testing.T) {
	ch := &chart.Chart{
		Metadata: &chart.Metadata{APIVersion: "v2", Name: "needs-122", Version: "0.1.0", KubeVersion: ">= 1.22.0-0"},
		Templates: []*chart.File{{Name: "templates/cm.yaml", Data: []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: rendered-for
data:
  kube: "{{ .Capabilities.KubeVersion.Version }}"
`)}},
	}
	// The cloud profiles name the version their Terraform creates; local
	// takes the client libraries'.
	for profile, want := range map[string]string{"oci": "v1.36.1", "aws": "v1.36", "local": kubeVersion().Version} {
		eng, err := New(nil, Options{Profile: profile, Vars: map[string]string{VarDomain: "example.com"}, Reporter: &quiet{}})
		if err != nil {
			t.Fatal(err)
		}
		kube, err := renderKubeVersion(eng.vars)
		if err != nil {
			t.Fatal(err)
		}
		objs, err := (&helmClient{}).template(context.Background(), &HelmSpec{Chart: "needs-122", Release: "needs-122"}, "default", ch, map[string]interface{}{}, kube, nil)
		if err != nil {
			t.Fatalf("%s: %v", profile, err)
		}
		if len(objs) != 1 {
			t.Fatalf("%s: %d objects", profile, len(objs))
		}
		if got := objs[0].Object["data"].(map[string]interface{})["kube"]; got != want {
			t.Errorf("%s: rendered for Kubernetes %v, want %s", profile, got, want)
		}
	}
	// --set names another.
	if kube, err := renderKubeVersion(map[string]string{VarKubeVersion: "v1.35.2"}); err != nil || kube.Version != "v1.35.2" {
		t.Errorf("SHPYRD_KUBE_VERSION=v1.35.2: %+v %v", kube, err)
	}
	if _, err := renderKubeVersion(map[string]string{VarKubeVersion: "latest"}); err == nil {
		t.Error("SHPYRD_KUBE_VERSION=latest must be refused")
	}
}

// The version is the one the client libraries were built for:
// k8s.io/client-go v0.<minor>.<patch> is Kubernetes 1.<minor>.<patch>,
// as the helm CLI's own build sets it.
func TestTheKubernetesVersionComesFromClientGo(t *testing.T) {
	for in, want := range map[string]string{
		"v0.37.0":       "v1.37.0",
		"v0.36.2":       "v1.36.2",
		"v0.38.0-rc.1":  "v1.38.0",
		"v1.2.3":        "",
		"":              "",
		"(devel)":       "",
		"v0.37":         "",
		"v0.notanumber": "",
	} {
		got := clientGoKubeVersion(in)
		switch {
		case want == "" && got != nil:
			t.Errorf("%q: got %+v, want none", in, got)
		case want != "" && (got == nil || got.Version != want || got.Major != "1" || got.Minor != want[3:5]):
			t.Errorf("%q: got %+v, want %s", in, got, want)
		}
	}
	if kv := kubeVersion(); kv == nil || kv.Major != "1" {
		t.Errorf("this build's Kubernetes version: %+v", kv)
	}
}
