package api

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
)

// A size no machine can hold is refused up front (#127): a process asked
// to run with more CPU or memory than the largest node of its pool offers
// would stop its running instance and wait for ever, with every way back
// locked behind the rollout. The apps pool's nodes are the measure; where
// the cluster has no pools, every node is.

// fitsItsMachine refuses a size the machine a process is pinned to cannot
// take (#127): a process with a node-local disk can only run where its disk
// is, and a Recreate rollout would stop the running instance before the
// new one proved impossible to place. The free room counts the process's
// own instance as free, since it is replaced. Nothing is said for a process
// that is not pinned, or when the machine cannot be read.
func (s *Server) fitsItsMachine(ctx context.Context, app *shpyrdv1.App, process string, size sizes.Size) error {
	node := controller.ProcessNodes(app)[process]
	if node == "" || s.kube == nil || s.kube.Kube == nil {
		return nil
	}
	n, err := s.kube.Kube.CoreV1().Nodes().Get(ctx, node, metav1.GetOptions{})
	if err != nil {
		return nil
	}
	pods, err := s.kube.Kube.CoreV1().Pods("").List(ctx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + node})
	if err != nil {
		return nil
	}
	freeCPU, freeMem := n.Status.Allocatable[corev1.ResourceCPU], n.Status.Allocatable[corev1.ResourceMemory]
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		if pod.Namespace == app.Namespace && pod.Labels[shpyrdv1.LabelApp] == app.Name && pod.Labels[shpyrdv1.LabelProcess] == process {
			continue // replaced by the new instance
		}
		for _, c := range pod.Spec.Containers {
			freeCPU.Sub(*c.Resources.Requests.Cpu())
			freeMem.Sub(*c.Resources.Requests.Memory())
		}
	}
	req := size.Resources().Requests
	cpu, mem := req[corev1.ResourceCPU], req[corev1.ResourceMemory]
	if freeCPU.Cmp(cpu) >= 0 && freeMem.Cmp(mem) >= 0 {
		return nil
	}
	if freeCPU.Sign() < 0 {
		freeCPU = resource.MustParse("0")
	}
	if freeMem.Sign() < 0 {
		freeMem = resource.MustParse("0")
	}
	return fmt.Errorf("%s runs on %s, the machine that holds its disk; it has %s cores and %s free, and %s asks for %s cores and %s. Choose a smaller size, or move the disk to block storage first", process, node, coreWords(freeCPU), bytesWords(freeMem), size.Name, coreWords(cpu), bytesWords(mem))
}

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
