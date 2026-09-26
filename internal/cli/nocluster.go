package cli

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// errNoCluster is what a command that still talks to the cluster directly
// gets when the CLI is signed in through `shpyrd login` and no kubeconfig
// was named: a sentence instead of a nil pointer.
var errNoCluster = errors.New("this command still needs cluster access: run it with --context <kubeconfig context> (or --kubeconfig); it will go through the API in a later release (RFC-0052)")

// noCluster is the controller-runtime client the app client carries when
// there is no cluster connection. Every method answers errNoCluster.
type noCluster struct{}

var _ client.Client = noCluster{}

func (noCluster) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return errNoCluster
}
func (noCluster) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return errNoCluster
}
func (noCluster) Create(context.Context, client.Object, ...client.CreateOption) error {
	return errNoCluster
}
func (noCluster) Delete(context.Context, client.Object, ...client.DeleteOption) error {
	return errNoCluster
}
func (noCluster) Update(context.Context, client.Object, ...client.UpdateOption) error {
	return errNoCluster
}
func (noCluster) Patch(context.Context, client.Object, client.Patch, ...client.PatchOption) error {
	return errNoCluster
}
func (noCluster) DeleteAllOf(context.Context, client.Object, ...client.DeleteAllOfOption) error {
	return errNoCluster
}
func (noCluster) Apply(context.Context, runtime.ApplyConfiguration, ...client.ApplyOption) error {
	return errNoCluster
}
func (noCluster) Status() client.SubResourceWriter            { return noSubResource{} }
func (noCluster) SubResource(string) client.SubResourceClient { return noSubResource{} }
func (noCluster) Scheme() *runtime.Scheme                     { return runtime.NewScheme() }
func (noCluster) RESTMapper() meta.RESTMapper                 { return nil }
func (noCluster) GroupVersionKindFor(runtime.Object) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, errNoCluster
}
func (noCluster) IsObjectNamespaced(runtime.Object) (bool, error) { return false, errNoCluster }

type noSubResource struct{}

func (noSubResource) Get(context.Context, client.Object, client.Object, ...client.SubResourceGetOption) error {
	return errNoCluster
}
func (noSubResource) Create(context.Context, client.Object, client.Object, ...client.SubResourceCreateOption) error {
	return errNoCluster
}
func (noSubResource) Update(context.Context, client.Object, ...client.SubResourceUpdateOption) error {
	return errNoCluster
}
func (noSubResource) Patch(context.Context, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
	return errNoCluster
}
func (noSubResource) Apply(context.Context, runtime.ApplyConfiguration, ...client.SubResourceApplyOption) error {
	return errNoCluster
}

// noClusterKube is a *kube.Client for the session mode: every call on the
// typed or dynamic client fails with errNoCluster instead of a nil pointer
// (fake clientsets with a reactor that refuses everything).
func noClusterKube() *kube.Client {
	cs := kubefake.NewSimpleClientset()
	cs.PrependReactor("*", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errNoCluster
	})
	dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	dyn.PrependReactor("*", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errNoCluster
	})
	return &kube.Client{
		Config:    &rest.Config{Host: "https://no-cluster.invalid"},
		Namespace: "shpyrd-system",
		Kube:      cs,
		Dynamic:   dyn,
	}
}
