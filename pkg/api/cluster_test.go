package api

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNodeInfoTellsThePoolFromTheLabel(t *testing.T) {
	n := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "10.0.1.10", Labels: map[string]string{
		"shpyrd.io/pool":                   "data",
		"node-role.kubernetes.io/node":     "",
		"node.kubernetes.io/instance-type": "VM.Standard.E5.Flex",
		"topology.kubernetes.io/zone":      "US-ASHBURN-AD-1",
	}}}
	info := nodeInfo(n)
	if info.Pool != "data" {
		t.Errorf("pool from the label: got %q", info.Pool)
	}
	if info.Roles != "worker" {
		t.Errorf("OKE's node role reads as worker: got %q", info.Roles)
	}
	if info.InstanceType != "VM.Standard.E5.Flex" || info.Zone != "US-ASHBURN-AD-1" {
		t.Errorf("shape and zone: %+v", info)
	}
}

func TestNodeInfoWithoutPoolsOrRoles(t *testing.T) {
	info := nodeInfo(corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "kind-control-plane", Labels: map[string]string{"node-role.kubernetes.io/control-plane": ""}}})
	if info.Pool != "" {
		t.Errorf("a single-pool cluster has no pool: got %q", info.Pool)
	}
	if info.Roles != "control-plane" {
		t.Errorf("roles: got %q", info.Roles)
	}
	if plain := nodeInfo(corev1.Node{}); plain.Roles != "worker" {
		t.Errorf("an unlabelled node is a worker: got %q", plain.Roles)
	}
}
