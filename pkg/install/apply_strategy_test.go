package install

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func deployment(strategy map[string]interface{}) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]interface{}{"name": "object-storage"}, "spec": map[string]interface{}{}}}
	if strategy != nil {
		u.Object["spec"].(map[string]interface{})["strategy"] = strategy
	}
	return u
}

// The switch to Recreate is the case server-side apply cannot do on its
// own: the live defaulted rollingUpdate block has to go first.
func TestNeedsRollingUpdateCleared(t *testing.T) {
	live := deployment(map[string]interface{}{"type": "RollingUpdate", "rollingUpdate": map[string]interface{}{"maxSurge": "25%", "maxUnavailable": "25%"}})
	if !needsRollingUpdateCleared(deployment(map[string]interface{}{"type": "Recreate"}), live) {
		t.Error("Recreate over a live rollingUpdate block needs the block cleared")
	}
	if needsRollingUpdateCleared(deployment(map[string]interface{}{"type": "RollingUpdate"}), live) {
		t.Error("staying on RollingUpdate needs nothing")
	}
	if needsRollingUpdateCleared(deployment(nil), live) {
		t.Error("a manifest without a strategy keeps the default")
	}
	if needsRollingUpdateCleared(deployment(map[string]interface{}{"type": "Recreate"}), deployment(map[string]interface{}{"type": "Recreate"})) {
		t.Error("already Recreate needs nothing")
	}
}
