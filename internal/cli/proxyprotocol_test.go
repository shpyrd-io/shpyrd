package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

func controllerService(proxy bool) *corev1.Service {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "ingress-nginx-controller", Namespace: "ingress-nginx"}}
	if proxy {
		svc.Annotations = map[string]string{install.ProxyProtocolAnnotation: "2"}
	}
	return svc
}

func TestExternalProxyProtocolOn(t *testing.T) {
	for name, tc := range map[string]struct {
		objs []runtime.Object
		want bool
	}{
		"annotated":     {[]runtime.Object{controllerService(true)}, true},
		"not annotated": {[]runtime.Object{controllerService(false)}, false},
		"no service":    {nil, false},
	} {
		got, err := externalProxyProtocolOn(context.Background(), &kube.Client{Kube: fake.NewSimpleClientset(tc.objs...)})
		if err != nil || got != tc.want {
			t.Errorf("%s: got %v, %v", name, got, err)
		}
	}
}

// A state that cannot be read is an error, never "on" or "off".
func TestExternalProxyProtocolOnReadFailure(t *testing.T) {
	cs := fake.NewSimpleClientset(controllerService(true))
	cs.PrependReactor("get", "services", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("connection refused")
	})
	if _, err := externalProxyProtocolOn(context.Background(), &kube.Client{Kube: cs}); err == nil {
		t.Error("expected an error")
	}
}

func TestPartialRun(t *testing.T) {
	for name, tc := range map[string]struct {
		only, skip []string
		want       bool
	}{
		"full":                  {nil, nil, false},
		"skip something else":   {nil, []string{"ingress-nginx-internal", "registry"}, false},
		"only":                  {[]string{"shpyrd"}, nil, true},
		"skip the controller":   {nil, []string{"ingress-nginx"}, true},
		"skip the server":       {nil, []string{"shpyrd"}, true},
		"only the controller":   {[]string{"ingress-nginx"}, []string{"ingress-nginx-internal"}, true},
		"skip only the dns one": {nil, []string{"dns"}, false},
	} {
		if got := partialRun(&initFlags{only: tc.only, skip: tc.skip}); got != tc.want {
			t.Errorf("%s: got %v", name, got)
		}
	}
}

// Each case of a partial run against a cluster that has the Service with or
// without the annotation: the server is told what the controller has, the
// request is kept out, and the output says so.
func TestPartialRunKeepsClusterProxyProtocol(t *testing.T) {
	for name, tc := range map[string]struct {
		cluster bool
		asked   string
		only    []string
		want    string
		note    bool
	}{
		"a on, only server":                {true, "", []string{"shpyrd"}, "true", true},
		"b off, only server, asked true":   {false, "true", []string{"shpyrd"}, "false", true},
		"c on, only server, asked false":   {true, "false", []string{"shpyrd"}, "true", true},
		"d on, only controller":            {true, "true", []string{"ingress-nginx"}, "true", false},
		"d off, only controller":           {false, "false", []string{"ingress-nginx"}, "false", false},
		"e on, record says false":          {true, "false", []string{"shpyrd"}, "true", true},
		"no service, only server, asked t": {false, "true", []string{"shpyrd"}, "false", true},
	} {
		var objs []runtime.Object
		if name != "no service, only server, asked t" {
			objs = append(objs, controllerService(tc.cluster))
		}
		k := &kube.Client{Kube: fake.NewSimpleClientset(objs...)}
		flags := &initFlags{only: tc.only}
		if !partialRun(flags) {
			t.Fatalf("%s: not a partial run", name)
		}
		on, err := externalProxyProtocolOn(context.Background(), k)
		if err != nil {
			t.Fatal(err)
		}
		vars := map[string]string{install.VarDomain: "oci.example.com", install.VarACMEEmail: "ops@example.com", install.VarLBSubnetCIDR: "10.7.0.0/24"}
		if tc.asked != "" {
			vars[install.VarProxyProtocol] = tc.asked
		}
		eng, err := install.New(nil, install.Options{Profile: "oci", Vars: vars, Only: tc.only, ProxyProtocolKeep: &on})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := eng.Vars()[install.VarProxyProtocol]; got != tc.want || eng.ProxyProtocolOn() != (tc.want == "true") {
			t.Errorf("%s: server is told %q", name, got)
		}
		notes := strings.Join(eng.Notices(), "\n")
		if tc.note != strings.Contains(notes, install.VarProxyProtocol) || strings.Contains(notes, "interrupted") {
			t.Errorf("%s: notices %q", name, notes)
		}
	}
}
