package controller

import (
	"strings"
	"testing"
)

func TestRegistryGarbageCollectionUsesTheConfiguredStorage(t *testing.T) {
	g := &RegistryGC{Namespace: "shpyrd-system", Image: "registry:3"}
	job := g.job("registry-node")
	pod := &job.Spec.Template.Spec
	if pod.EnableServiceLinks == nil || *pod.EnableServiceLinks {
		t.Error("GC must disable registry service discovery environment variables")
	}
	if pod.NodeName != "registry-node" || pod.Volumes[0].PersistentVolumeClaim.ClaimName != "registry-data" {
		t.Fatal("filesystem GC needs the registry's volume and node")
	}
	g.useBucket(job)
	if pod.NodeName != "" {
		t.Error("bucket GC should run on any node")
	}
	for _, v := range pod.Volumes {
		if v.PersistentVolumeClaim != nil {
			t.Error("bucket GC still needs a PVC")
		}
	}
	c := pod.Containers[0]
	if strings.Contains(strings.Join(c.Command, " "), "du -") {
		t.Error("bucket GC measures a local directory")
	}
	if len(c.Env) != 2 {
		t.Fatal("missing S3 credentials")
	}
	for _, e := range c.Env {
		if e.ValueFrom.SecretKeyRef.Name != "registry-s3" || e.ValueFrom.SecretKeyRef.Key != e.Name {
			t.Errorf("wrong credential reference: %+v", e)
		}
	}
}
