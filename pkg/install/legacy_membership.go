package install

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// The Team and ProjectMember definitions left the manifests when teams and
// grants moved to the control-plane database (RFC-0033, #249). Leaving the
// manifests does not remove them from a cluster that has them, and removing
// a definition removes every object of it, so this step does it explicitly
// and only once the server has imported every object: it refuses while any
// is unmarked (shpyrdv1.AnnotationMigrated), and writes all of them to a
// local file before it removes anything.

var crdsGVR = schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}

// legacyMembership is each legacy resource with its definition's name.
var legacyMembership = []struct {
	gvr schema.GroupVersionResource
	crd string
}{
	{shpyrdv1.LegacyTeams, "teams.shpyrd.io"},
	{shpyrdv1.LegacyProjectMembers, "projectmembers.shpyrd.io"},
}

// LegacyRetirement is what RetireLegacyMembership did.
type LegacyRetirement struct {
	// Removed names the definitions removed; empty when none was left.
	Removed []string
	// Export is the file the objects were written to; empty when there
	// were none.
	Export  string
	Objects int
}

// ErrLegacyUnimported is returned while legacy objects wait to be imported.
type ErrLegacyUnimported struct{ Names []string }

func (e *ErrLegacyUnimported) Error() string {
	return fmt.Sprintf("%d legacy Team/ProjectMember object(s) are not imported into the control-plane database yet (%s), so their definitions were left in place and nothing was removed. "+
		"The shpyrd server imports them when it starts: check its log for \"legacy\", fix what it reports, restart it (kubectl -n shpyrd-system rollout restart deployment/shpyrd-server) and run this command again",
		len(e.Names), strings.Join(e.Names, ", "))
}

// RetireLegacyMembership removes the legacy definitions from a cluster that
// still has them, after a full apply; a run limited with --only leaves them.
// The objects go to a file in exportDir first. Idempotent: a cluster without
// the definitions has nothing to do.
func (e *Engine) RetireLegacyMembership(ctx context.Context, exportDir string) (*LegacyRetirement, error) {
	if len(e.opts.Only) > 0 {
		return &LegacyRetirement{}, nil
	}
	return retireLegacyMembership(ctx, e.kube.Dynamic, exportDir, time.Now())
}

func retireLegacyMembership(ctx context.Context, dyn dynamic.Interface, exportDir string, now time.Time) (*LegacyRetirement, error) {
	out := &LegacyRetirement{}
	var present []string
	var objects []unstructured.Unstructured
	var unimported []string
	for _, l := range legacyMembership {
		if _, err := dyn.Resource(crdsGVR).Get(ctx, l.crd, metav1.GetOptions{}); apierrors.IsNotFound(err) {
			continue
		} else if err != nil {
			return out, fmt.Errorf("read definition %s: %w", l.crd, err)
		}
		present = append(present, l.crd)
		list, err := dyn.Resource(l.gvr).List(ctx, metav1.ListOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return out, fmt.Errorf("list %s: %w", l.gvr.Resource, err)
		}
		if list == nil {
			continue
		}
		for _, u := range list.Items {
			if u.GetAnnotations()[shpyrdv1.AnnotationMigrated] == "" {
				unimported = append(unimported, strings.ToLower(u.GetKind())+"/"+u.GetName())
			}
			objects = append(objects, u)
		}
	}
	if len(present) == 0 {
		return out, nil
	}
	if len(unimported) > 0 {
		sort.Strings(unimported)
		return out, &ErrLegacyUnimported{Names: unimported}
	}
	if len(objects) > 0 {
		path, err := exportLegacy(exportDir, objects, now)
		if err != nil {
			return out, err
		}
		out.Export, out.Objects = path, len(objects)
	}
	for _, crd := range present {
		if err := dyn.Resource(crdsGVR).Delete(ctx, crd, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return out, fmt.Errorf("remove definition %s: %w", crd, err)
		}
		out.Removed = append(out.Removed, crd)
	}
	return out, nil
}

// exportLegacy writes the objects as one YAML stream, the way a backup
// archive holds them, so a later `shpyrd cluster restore` can read them. The
// file names people, so only its owner reads it.
func exportLegacy(dir string, objects []unstructured.Unstructured, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("export legacy objects: %w", err)
	}
	var buf bytes.Buffer
	for _, u := range objects {
		u := u.DeepCopy()
		unstructured.RemoveNestedField(u.Object, "metadata", "managedFields")
		unstructured.RemoveNestedField(u.Object, "metadata", "resourceVersion")
		unstructured.RemoveNestedField(u.Object, "metadata", "uid")
		b, err := yaml.Marshal(u.Object)
		if err != nil {
			return "", err
		}
		buf.WriteString("---\n")
		buf.Write(b)
	}
	path := filepath.Join(dir, "legacy-membership-"+now.UTC().Format("20060102T150405Z")+".yaml")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return "", fmt.Errorf("export legacy objects: %w", err)
	}
	return path, nil
}
