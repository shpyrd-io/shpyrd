// Package opencost is the OpenCost extension (RFC-0075): it installs
// OpenCost, which allocates what the cluster costs to namespaces, pods and
// nodes, and answers a status route for the console. What the platform
// does with those costs is the enterprise Costs (ee/costs).
package opencost

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// Name of the extension.
const Name = "opencost"

// AllocationURL is the OpenCost service's Allocation API.
// OpenCost 1.121+ uses /allocation/compute (not /model/allocation).
const AllocationURL = "http://opencost.opencost.svc:9003/allocation/compute"

// MetricsURL is OpenCost's Prometheus metrics endpoint: node costs.
const MetricsURL = "http://opencost.opencost.svc:9003/metrics"

type extension struct{}

func New() ext.Extension { return extension{} }

func (extension) Name() string { return Name }
func (extension) Description() string {
	return "Infrastructure cost allocation via OpenCost (RFC-0075)"
}
func (extension) Components() []ext.ComponentRef {
	return []ext.ComponentRef{{Name: "opencost", Runlevel: "rc4"}}
}
func (extension) Register(ctrl.Manager, ext.Deps) error { return nil }
func (extension) Types() []ext.ResourceType             { return nil }
func (extension) CLI(g ext.CLIGlobals) []*cobra.Command {
	return []*cobra.Command{ext.ForOperator(newOpenCostCmd(g))}
}

// Routes mounts GET /api/cluster/opencost/status (operator console).
func (extension) Routes(r ext.Router, _ ext.Deps) error {
	r.Admin().GET("/cluster/opencost/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"source": "opencost", "url": AllocationURL})
	})
	return nil
}

func newOpenCostCmd(ext.CLIGlobals) *cobra.Command {
	return &cobra.Command{
		Use:   "opencost",
		Short: "OpenCost extension status",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "OpenCost is enabled: it allocates what the cluster costs to namespaces, pods and nodes.")
			fmt.Fprintln(cmd.OutOrStdout(), "Costs and cost drains, with a license: shpyrd-ctl costs")
			return nil
		},
	}
}
