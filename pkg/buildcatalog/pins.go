// Package buildcatalog pins the platform's build catalog, the
// ClusterBuildpacks and ClusterStacks of deploy/components/kpack, to image
// digests, and moves the pins forward at a moment of the platform's
// choosing.
//
// The manifests name tags (paketobuildpacks/nodejs:latest), and kpack
// follows a tag: whenever Paketo publishes a buildpack or a base image it
// assembles the platform's builder again, then every project's builder,
// and a run-image change rebases every app. In October 2026 that happened
// 29 times on production, at whatever hour Paketo shipped; the first build
// of every app afterwards restored no cache and pushed every layer again
// (median 185 s against 80 s), and each builder left 3.75 GB on every build
// machine. With the pins, kpack sees a digest and nothing moves until the
// weekly refresh (deploy/components/kpack/base/refresh.yaml) or the
// server's first start on a fresh install.
//
// A pinned field keeps the tag it came from in an annotation, so the
// refresh knows what to resolve and the installer (pkg/install) knows to
// keep the pin while the manifest still names that tag. A release that
// names a new tag is applied as written and pinned at the next pass.
package buildcatalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

// Field is one image reference of a catalog object, and the annotation
// that records the tag it is pinned from.
type Field struct {
	Path       []string
	Annotation string
}

// Name is the field as a person reads it (spec.image).
func (f Field) Name() string { return strings.Join(f.Path, ".") }

// Fields are the image references of a kind of catalog object; nil for
// kinds the catalog does not pin.
func Fields(kind string) []Field {
	switch kind {
	case "ClusterBuildpack":
		return []Field{{[]string{"spec", "image"}, "shpyrd.io/pinned-image"}}
	case "ClusterStack":
		return []Field{
			{[]string{"spec", "buildImage", "image"}, "shpyrd.io/pinned-build-image"},
			{[]string{"spec", "runImage", "image"}, "shpyrd.io/pinned-run-image"},
		}
	}
	return nil
}

// Resources are the catalog kinds, by their kpack resource.
var Resources = map[string]schema.GroupVersionResource{
	"ClusterBuildpack": {Group: "kpack.io", Version: "v1alpha2", Resource: "clusterbuildpacks"},
	"ClusterStack":     {Group: "kpack.io", Version: "v1alpha2", Resource: "clusterstacks"},
}

// IsDigest says a reference names a digest (repo@sha256:...).
func IsDigest(ref string) bool { return strings.Contains(ref, "@") }

// Pinned is the digest form of a tag reference: the repository of ref at
// digest.
func Pinned(ref, digest string) string {
	repo := ref
	if i := strings.Index(repo, "@"); i >= 0 {
		repo = repo[:i]
	}
	// A tag follows the last colon after the last slash (a port is
	// before it).
	if colon := strings.LastIndex(repo, ":"); colon > strings.LastIndex(repo, "/") {
		repo = repo[:colon]
	}
	return repo + "@" + digest
}

// Tracked is the reference whose digest a field follows: the field's own
// value when it names a tag, the annotation when the field is pinned.
// pinned says which; ref is "" when the field is empty.
func Tracked(u *unstructured.Unstructured, f Field) (ref string, pinned bool) {
	cur, _, _ := unstructured.NestedString(u.Object, f.Path...)
	if cur == "" {
		return "", false
	}
	if !IsDigest(cur) {
		return cur, false
	}
	from := u.GetAnnotations()[f.Annotation]
	if from == "" {
		// Pinned by hand, or by a manifest that names a digest: nothing
		// to follow.
		return cur, true
	}
	return from, true
}

// Resolver finds the digest a tag reference points at now.
type Resolver interface {
	Digest(ctx context.Context, ref string) (string, error)
}

// Change is one pin moved: what the field said and what it says now.
type Change struct {
	Kind, Name, Field, From, To string
}

func (c Change) String() string {
	return fmt.Sprintf("%s %s %s: %s -> %s", c.Kind, c.Name, c.Field, c.From, c.To)
}

// Left is a field left as a tag, and why: its registry wants a credential
// the refresh has none of, so kpack keeps following the tag there.
type Left struct {
	Kind, Name, Field, Ref string
}

func (l Left) String() string {
	return fmt.Sprintf("%s %s %s: %s left to kpack, its registry asks for a credential", l.Kind, l.Name, l.Field, l.Ref)
}

// Result is what a refresh did.
type Result struct {
	Changes []Change
	Left    []Left
}

// Refresh pins every image reference of the catalog to the current digest
// of the tag it tracks. With onlyUnpinned the fields already pinned are
// left as they are (the server's start on a fresh install). A reference
// that cannot be resolved leaves its field alone and is reported in the
// error; the others still move.
func Refresh(ctx context.Context, dyn dynamic.Interface, r Resolver, onlyUnpinned bool) (Result, error) {
	var res Result
	var errs []error
	for kind, gvr := range Resources {
		list, err := dyn.Resource(gvr).List(ctx, metav1.ListOptions{})
		if err != nil {
			errs = append(errs, fmt.Errorf("list %s: %w", kind, err))
			continue
		}
		for i := range list.Items {
			item := &list.Items[i]
			patch := map[string]interface{}{}
			annotations := map[string]interface{}{}
			for _, f := range Fields(kind) {
				cur, _, _ := unstructured.NestedString(item.Object, f.Path...)
				tracked, pinned := Tracked(item, f)
				if tracked == "" || IsDigest(tracked) || (pinned && onlyUnpinned) {
					continue
				}
				digest, err := r.Digest(ctx, tracked)
				if errors.Is(err, ErrCredential) {
					res.Left = append(res.Left, Left{Kind: kind, Name: item.GetName(), Field: f.Name(), Ref: tracked})
					continue
				}
				if err != nil {
					errs = append(errs, fmt.Errorf("%s %s %s: %s: %w", kind, item.GetName(), f.Name(), tracked, err))
					continue
				}
				want := Pinned(tracked, digest)
				if want == cur {
					continue
				}
				if err := unstructured.SetNestedField(patch, want, f.Path...); err != nil {
					errs = append(errs, err)
					continue
				}
				annotations[f.Annotation] = tracked
				res.Changes = append(res.Changes, Change{Kind: kind, Name: item.GetName(), Field: f.Name(), From: cur, To: want})
			}
			if len(patch) == 0 {
				continue
			}
			patch["metadata"] = map[string]interface{}{"annotations": annotations}
			raw, err := yaml.Marshal(patch)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			body, err := yaml.YAMLToJSON(raw)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if _, err := dyn.Resource(gvr).Patch(ctx, item.GetName(), types.MergePatchType, body, metav1.PatchOptions{FieldManager: "shpyrd-buildcatalog"}); err != nil {
				errs = append(errs, fmt.Errorf("pin %s %s: %w", kind, item.GetName(), err))
			}
		}
	}
	return res, errors.Join(errs...)
}

// KeepPins carries the live pins into the object about to be applied from
// a manifest: a field the live object has pinned from the very reference
// the manifest names keeps the live digest, so an upgrade does not undo a
// pin and rebuild the builders at deploy time. A field the manifest names
// differently (a new tag, a new repository) is applied as written. It
// returns how many fields kept their pin.
func KeepPins(desired, live *unstructured.Unstructured) int {
	if live == nil {
		return 0
	}
	kept := 0
	for _, f := range Fields(desired.GetKind()) {
		want, _, _ := unstructured.NestedString(desired.Object, f.Path...)
		cur, _, _ := unstructured.NestedString(live.Object, f.Path...)
		if want == "" || IsDigest(want) || !IsDigest(cur) || live.GetAnnotations()[f.Annotation] != want {
			continue
		}
		_ = unstructured.SetNestedField(desired.Object, cur, f.Path...)
		kept++
	}
	return kept
}
