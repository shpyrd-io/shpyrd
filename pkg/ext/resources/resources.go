// Package resources holds helpers shared by the data store extensions' CLIs:
// creating, listing and deleting project resources with the kubeconfig,
// and refusing to delete what an app still uses.
package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/project"
)

// Namespace of a project.
func Namespace(slug string) string { return project.Namespace(slug) }

// Connect builds the controller-runtime client the CLIs use.
func Connect(g ext.CLIGlobals) (*kube.Client, client.Client, error) {
	k, err := kube.Connect(kube.Options{Kubeconfig: g.Kubeconfig(), Context: g.Context()})
	if err != nil {
		return nil, nil, err
	}
	c, err := k.ControllerClient()
	if err != nil {
		return nil, nil, err
	}
	return k, c, nil
}

// RequireProject fails with a helpful message when the project is missing.
func RequireProject(ctx context.Context, c client.Client, project string) error {
	app := &shpyrdv1.App{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: Namespace(project), Name: project}, app); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("project %q not found (see `shpyrd projects list`)", project)
		}
		return err
	}
	return nil
}

// BoundBy lists the apps of the namespace attaching kind/name.
func BoundBy(ctx context.Context, c client.Client, namespace, kind, name string) ([]string, error) {
	var apps shpyrdv1.AppList
	if err := c.List(ctx, &apps, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	var out []string
	for _, a := range apps.Items {
		for _, b := range a.Spec.Bindings {
			if b.Kind == kind && b.Name == name {
				out = append(out, a.Name)
			}
		}
	}
	return out, nil
}

// CheckDeletable refuses deletion while bound unless forced.
func CheckDeletable(ctx context.Context, c client.Client, namespace, kind, name string, force bool) error {
	bound, err := BoundBy(ctx, c, namespace, kind, name)
	if err != nil {
		return err
	}
	if len(bound) > 0 && !force {
		return fmt.Errorf("%s %s is attached to %s: detach it first (`shpyrd detach %s`) or pass --force", kind, name, strings.Join(bound, ", "), name)
	}
	return nil
}

// ExtensionHint explains a missing controller.
func ExtensionHint(extension string) string {
	return fmt.Sprintf("the %s extension is not enabled on this cluster: run `shpyrd extensions enable %s`", extension, extension)
}

// ---- through the API (RFC-0052) ------------------------------------------------
//
// The core's /api/projects/:slug/resources routes create, list and delete
// resources of any extension kind; the CRD schema validates the spec. Going
// through them, the data store CLIs work for tenants of a hosted platform
// who have no kubeconfig, and for operators through the kubeconfig proxy.

// View is a resource as the API lists it (api.ResourceView's fields the
// CLIs print).
type View struct {
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	Phase      string            `json:"phase"`
	Message    string            `json:"message,omitempty"`
	Endpoint   string            `json:"endpoint,omitempty"`
	Details    map[string]string `json:"details,omitempty"`
	AttachedTo []string          `json:"attachedTo"`
	Data       bool              `json:"data"`
	CreatedAt  time.Time         `json:"createdAt"`
	// Note is what the platform decided on creation and why.
	Note string `json:"note,omitempty"`
}

// CreateAPI creates a resource of a kind with a spec, as the CRD spells it.
func CreateAPI(ctx context.Context, api ext.APIClient, project, kind, name string, spec any) (*View, error) {
	body, err := json.Marshal(map[string]any{"kind": kind, "name": name, "spec": spec})
	if err != nil {
		return nil, err
	}
	raw, err := api.Request(ctx, "POST", "api/projects/"+project+"/resources", body, "application/json")
	if err != nil {
		return nil, err
	}
	var v View
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("unexpected response: %s", raw)
	}
	return &v, nil
}

// ListAPI lists the project's resources of one kind.
func ListAPI(ctx context.Context, api ext.APIClient, project, kind string) ([]View, error) {
	raw, err := api.Request(ctx, "GET", "api/projects/"+project+"/resources", nil, "")
	if err != nil {
		return nil, err
	}
	var all []View
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, fmt.Errorf("unexpected response: %s", raw)
	}
	out := make([]View, 0, len(all))
	for _, v := range all {
		if strings.EqualFold(v.Kind, kind) {
			out = append(out, v)
		}
	}
	return out, nil
}

// GetAPI finds one resource by kind and name.
func GetAPI(ctx context.Context, api ext.APIClient, project, kind, name string) (*View, error) {
	list, err := ListAPI(ctx, api, project, kind)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Name == name {
			return &list[i], nil
		}
	}
	return nil, fmt.Errorf("%s %q not found in project %s", strings.ToLower(kind), name, project)
}

// DeleteAPI removes a resource; force detaches it from apps first.
func DeleteAPI(ctx context.Context, api ext.APIClient, project, kind, name string, force bool) error {
	path := "api/projects/" + project + "/resources/" + url.PathEscape(kind) + "/" + url.PathEscape(name)
	if force {
		path += "?force=true"
	}
	_, err := api.Request(ctx, "DELETE", path, nil, "")
	return err
}

// WaitReadyAPI follows a resource until it is Ready or Failed, printing
// phase changes, and returns the final view.
func WaitReadyAPI(ctx context.Context, api ext.APIClient, out io.Writer, project, kind, name string, timeout time.Duration) (*View, error) {
	deadline := time.Now().Add(timeout)
	last := ""
	for time.Now().Before(deadline) {
		v, err := GetAPI(ctx, api, project, kind, name)
		if err != nil {
			return nil, err
		}
		if msg := strings.TrimSpace(v.Phase + " " + v.Message); msg != last && v.Phase != "" {
			fmt.Fprintf(out, "    %s\n", msg)
			last = msg
		}
		switch v.Phase {
		case shpyrdv1.ResourceReady, shpyrdv1.ResourceFailed:
			return v, nil
		}
		select {
		case <-ctx.Done():
			return v, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return nil, nil // still provisioning
}
