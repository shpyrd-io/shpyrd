package controller

import (
	"context"
	"encoding/json"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// removeAnnotationIfUnchanged removes key from obj on the server only
// while it still holds value: a newer value another writer left meanwhile
// stays (a JSON patch test, then remove). It reports whether it removed
// it; a failed test is an error the caller logs and moves past.
func removeAnnotationIfUnchanged(ctx context.Context, c client.Client, obj client.Object, key, value string) (bool, error) {
	path := "/metadata/annotations/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(key)
	patch, err := json.Marshal([]map[string]any{
		{"op": "test", "path": path, "value": value},
		{"op": "remove", "path": path},
	})
	if err != nil {
		return false, err
	}
	if err := c.Patch(ctx, obj, client.RawPatch(types.JSONPatchType, patch)); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
