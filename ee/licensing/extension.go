//go:build !foss

package licensing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/shpyrd-io/shpyrd/pkg/audit"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// Name of the extension. It is always on wherever ee is built in: it is
// what switches the rest on.
const Name = "license"

// watchInterval is how often the server re-reads the Secret.
const watchInterval = time.Minute

type extension struct{}

// New returns the license extension.
func New() ext.Extension { return extension{} }

func (extension) Name() string                   { return Name }
func (extension) Description() string            { return "The enterprise license: switches the ee features on" }
func (extension) Components() []ext.ComponentRef { return nil }
func (extension) Types() []ext.ResourceType      { return nil }

// renewerOf is the online renewal with what the server has.
func renewerOf(deps ext.Deps) *Renewer {
	return &Renewer{Kube: deps.Kube.Kube, Namespace: deps.SystemNamespace, Store: deps.Store, Cluster: firstNonEmpty(deps.Var(install.VarCluster), deps.Var(install.VarDomain)), Logger: slog.Default()}
}

// Register renews the license online, on the leader.
func (extension) Register(mgr ctrl.Manager, deps ext.Deps) error {
	if deps.Kube == nil || deps.Store == nil {
		return nil
	}
	return mgr.Add(renewerOf(deps))
}

// Routes mounts the console's routes and starts reading the Secret:
// GET /api/cluster/license, POST /api/cluster/license/renew (now, whatever
// the date) and POST /api/cluster/license/billing (a link of one use to
// the customer's account at the billing app that issued the license).
func (extension) Routes(r ext.Router, deps ext.Deps) error {
	if deps.Kube == nil {
		r.Admin().GET("/cluster/license", func(c *gin.Context) { c.JSON(http.StatusOK, Current()) })
		return nil
	}
	k, ns := deps.Kube.Kube, deps.SystemNamespace
	status := func(c *gin.Context) Status {
		s := Current()
		if _, renewal, err := ReadSecret(c.Request.Context(), k, ns); err == nil {
			s.Renewal = renewal
		}
		return s
	}
	r.Admin().GET("/cluster/license", func(c *gin.Context) { c.JSON(http.StatusOK, status(c)) })
	r.Admin().POST("/cluster/license/renew", func(c *gin.Context) {
		rn := renewerOf(deps)
		_, err := rn.Renew(c.Request.Context(), true)
		rn.note(c.Request.Context(), err)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, status(c))
	})
	r.Admin().POST("/cluster/license/billing", func(c *gin.Context) {
		link, err := BillingLink(c.Request.Context(), nil, k, ns)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"url": link})
	})
	go Watch(context.Background(), k, ns, watchInterval, slog.Default())
	return nil
}

func (extension) CLI(g ext.CLIGlobals) []*cobra.Command {
	return []*cobra.Command{ext.ForOperator(newLicenseCmd(g))}
}

func newLicenseCmd(g ext.CLIGlobals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "license",
		Short: "The enterprise license: what switches the ee features on",
		Long: `A license switches every enterprise feature on until the day it expires; it
carries no limits. It lives in the cluster (Secret shpyrd-license) and the
server reads it within a minute.

  shpyrd-ctl license set acme.jwt
  shpyrd-ctl license status
  shpyrd-ctl license renew     # now, at the billing app that issued it
  shpyrd-ctl license billing   # a link to your account there

A license the billing app issued renews by itself: a week before it expires
the cluster sends it there, with what the cluster used and cost over the last
30 days, and installs the next one. An expired license does not renew: ask
for a new one.`,
	}
	cmd.AddCommand(newSetCmd(g), newStatusCmd(g), newRenewCmd(g), newBillingCmd(g))
	return cmd
}

func newRenewCmd(g ext.CLIGlobals) *cobra.Command {
	return &cobra.Command{
		Use:   "renew",
		Short: "Renew the license now, at the billing app that issued it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := g.API().Request(cmd.Context(), "POST", "api/cluster/license/renew", nil, "")
			if err != nil {
				return err
			}
			var s Status
			if err := json.Unmarshal(raw, &s); err != nil {
				return err
			}
			return ext.Print(g, cmd, s, func(w io.Writer) { printStatus(w, s) })
		},
	}
}

func newBillingCmd(g ext.CLIGlobals) *cobra.Command {
	return &cobra.Command{
		Use:   "billing",
		Short: "A link of one use to your account at the billing app that issued the license",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := g.API().Request(cmd.Context(), "POST", "api/cluster/license/billing", nil, "")
			if err != nil {
				return err
			}
			var out struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal(raw, &out); err != nil {
				return err
			}
			return ext.Print(g, cmd, out, func(w io.Writer) {
				fmt.Fprintf(w, "Open within a few minutes (it works once):\n  %s\n", out.URL)
			})
		},
	}
}

func newSetCmd(g ext.CLIGlobals) *cobra.Command {
	return &cobra.Command{
		Use:   "set <file>",
		Short: "Install a license (replaces the one installed)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			k, err := kube.Connect(kube.Options{Kubeconfig: g.Kubeconfig(), Context: g.Context()})
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			ns := install.DefaultSystemNamespace
			l, err := Write(ctx, k.Kube, ns, string(raw))
			if err != nil {
				return err
			}
			entry := audit.Entry{Actor: audit.LocalActor(), Action: "license.set", Target: l.ID, Detail: l.Customer, Via: "cli"}
			_ = audit.Record(ctx, k.Kube, audit.ClusterRef(ns), entry)
			return ext.Print(g, cmd, l, func(w io.Writer) {
				fmt.Fprintf(w, "License %s for %s installed, in force until %s.\n", l.ID, l.Customer, l.ExpiresAt.Format("2006-01-02"))
				fmt.Fprintln(w, "The server picks it up within a minute.")
			})
		},
	}
}

func newStatusCmd(g ext.CLIGlobals) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the license installed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := kube.Connect(kube.Options{Kubeconfig: g.Kubeconfig(), Context: g.Context()})
			if err != nil {
				return err
			}
			token, renewal, err := ReadSecret(cmd.Context(), k.Kube, install.DefaultSystemNamespace)
			if err != nil {
				return err
			}
			s := statusOf(token, now())
			s.Renewal = renewal
			return ext.Print(g, cmd, s, func(w io.Writer) { printStatus(w, s) })
		},
	}
}

// statusOf is the status of a token as the CLI reads it from the Secret.
func statusOf(token string, at time.Time) Status {
	if token == "" {
		return Status{}
	}
	l, err := Parse(token)
	if err != nil {
		return Status{Error: err.Error()}
	}
	return Status{Active: l.ActiveAt(at), License: &l}
}

func printStatus(w io.Writer, s Status) {
	switch {
	case s.Error != "":
		fmt.Fprintf(w, "The license installed is refused: %s.\n", s.Error)
	case s.License == nil:
		fmt.Fprintln(w, "No license installed: the enterprise features are off.")
	case s.Active:
		fmt.Fprintf(w, "License %s for %s, in force until %s.\n", s.License.ID, s.License.Customer, s.License.ExpiresAt.Format("2006-01-02"))
	default:
		fmt.Fprintf(w, "License %s for %s expired on %s: the enterprise features are off.\n", s.License.ID, s.License.Customer, s.License.ExpiresAt.Format("2006-01-02"))
	}
	if s.License == nil || s.Error != "" {
		return
	}
	if s.License.Issuer == "" {
		fmt.Fprintln(w, "Issued by hand: it renews offline, with a new license.")
	} else {
		fmt.Fprintf(w, "Renews online at %s, a week before it expires.\n", s.License.Issuer)
	}
	if r := s.Renewal; r != nil {
		if r.Error != "" {
			fmt.Fprintf(w, "The last renewal, %s, failed: %s\n", r.At.Local().Format("Jan 2 15:04"), r.Error)
		} else {
			fmt.Fprintf(w, "Last renewed %s.\n", r.At.Local().Format("Jan 2 15:04"))
		}
	}
}
