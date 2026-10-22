package api

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// The resource view carries a database's sleep read-back (#120): state,
// the quiet period that applies, where it comes from and the message.
func TestResourceViewReadsBackSleep(t *testing.T) {
	u := unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": "db", "namespace": "p-1"},
		"spec":     map[string]interface{}{"size": "small"},
		"status": map[string]interface{}{
			"phase": "Ready",
			"sleep": map[string]interface{}{"state": "awake", "after": "10m0s", "source": "workspace", "message": "awake: idle for 2m0s of 10m0s (workspace default)"},
		},
	}}
	v := resourceViewOf(ext.ResourceType{Kind: "Postgres"}, u)
	for key, want := range map[string]string{"sleepState": "awake", "sleepAfter": "10m0s", "sleepSource": "workspace", "sleepMessage": "awake: idle for 2m0s of 10m0s (workspace default)", "size": "small"} {
		if v.Details[key] != want {
			t.Errorf("details[%s] = %q, want %q", key, v.Details[key], want)
		}
	}
	plain := resourceViewOf(ext.ResourceType{Kind: "Redis"}, unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "cache"}, "status": map[string]interface{}{"phase": "Ready"}}})
	if _, ok := plain.Details["sleepState"]; ok {
		t.Error("a resource without a sleep status gets no sleep details")
	}
}
