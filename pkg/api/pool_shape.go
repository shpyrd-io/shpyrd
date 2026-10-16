package api

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
)

// A size no machine can hold is refused up front (#127): a process asked
// to run with more CPU or memory than the largest node of its pool offers
// would stop its running instance and wait for ever, with every way back
// locked behind the rollout. The apps pool's nodes are the measure; where
// the cluster has no pools, every node is.

// fitsAPool refuses a size whose requests no node of the apps pool can
// take; nothing is said when the nodes cannot be listed.
func (s *Server) fitsAPool(ctx context.Context, size sizes.Size) error {
	if s.kube == nil || s.kube.Kube == nil {
		return nil
	}
	opts := metav1.ListOptions{}
	if pool := s.vars(install.VarAppsPool); pool != "" {
		opts.LabelSelector = "shpyrd.io/pool=" + pool
	}
	nodes, err := s.kube.Kube.CoreV1().Nodes().List(ctx, opts)
	if err != nil || len(nodes.Items) == 0 {
		return nil
	}
	req := size.Resources().Requests
	cpu, mem := req[corev1.ResourceCPU], req[corev1.ResourceMemory]
	var bestCPU, bestMem resource.Quantity
	for _, n := range nodes.Items {
		if n.Spec.Unschedulable {
			continue
		}
		c, m := n.Status.Allocatable[corev1.ResourceCPU], n.Status.Allocatable[corev1.ResourceMemory]
		if c.Cmp(cpu) >= 0 && m.Cmp(mem) >= 0 {
			return nil // one machine can hold it
		}
		if c.Cmp(bestCPU) > 0 {
			bestCPU = c
		}
		if m.Cmp(bestMem) > 0 {
			bestMem = m
		}
	}
	return fmt.Errorf("%s needs %s cores and %s; the largest machine in this cluster offers %s cores and %s", size.Name, coreWords(cpu), bytesWords(mem), coreWords(bestCPU), bytesWords(bestMem))
}
