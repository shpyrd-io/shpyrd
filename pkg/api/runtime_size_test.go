package api

import (
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
)

func TestUsageCountsRuntimeDefault(t *testing.T) {
	app := &shpyrdv1.App{Spec: shpyrdv1.AppSpec{Image: "image"}, Status: shpyrdv1.AppStatus{Runtime: "Node.js", DefaultSize: "shared-m"}}
	catalog := sizes.Defaults()
	var got usage
	got.addApp(app, &catalog)
	if got.instances != 1 || got.memory.String() != "256Mi" {
		t.Fatalf("runtime default usage = %+v", got.view())
	}
	app.Spec.Processes = map[string]shpyrdv1.Process{"web": {Size: "shared-l"}}
	got = usage{}
	got.addApp(app, &catalog)
	if got.memory.String() != "512Mi" {
		t.Fatalf("explicit size usage = %+v", got.view())
	}
}
