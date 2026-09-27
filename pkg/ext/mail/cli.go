package mail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/audit"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// The operator's commands (shpyrd-ctl mail ...). Settings are written to
// the cluster directly; the test message is sent by the server, from
// inside the cluster, where the delivery will happen.

func newMailCmd(g ext.CLIGlobals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mail",
		Short: "Email delivery: the SMTP sender the platform uses for invitations",
		Long: `Configure how the platform sends email (RFC-0013). Invitations carry their
link by email once a sender is set; until then the link is shown to whoever
invites, to pass along.

  shpyrd-ctl mail set --host smtp.example.com --port 587 \
      --user postmaster@example.com --password @/path/to/password \
      --from "shpyrd <noreply@example.com>"
  shpyrd-ctl mail test you@example.com`,
	}
	cmd.AddCommand(newSetCmd(g), newStatusCmd(g), newTestCmd(g), newUnsetCmd(g))
	return cmd
}

type cliDeps struct {
	k     *kube.Client
	store *Store
}

func connect(g ext.CLIGlobals) (*cliDeps, error) {
	k, err := kube.Connect(kube.Options{Kubeconfig: g.Kubeconfig(), Context: g.Context()})
	if err != nil {
		return nil, err
	}
	return &cliDeps{k: k, store: &Store{Kube: k.Kube, Namespace: install.DefaultSystemNamespace}}, nil
}

func (d *cliDeps) audit(ctx context.Context, action, target, detail string) {
	entry := audit.Entry{Actor: audit.LocalActor(), Action: action, Target: target, Detail: detail, Via: "cli"}
	_ = audit.Record(ctx, d.k.Kube, audit.ClusterRef(install.DefaultSystemNamespace), entry)
}

func (d *cliDeps) enabled(ctx context.Context) bool {
	info, err := install.ReadInstallInfo(ctx, d.k, install.DefaultSystemNamespace)
	if err != nil || info == nil {
		return false
	}
	for _, n := range strings.Split(info.Vars[install.VarExtensions], ",") {
		if strings.TrimSpace(n) == Name {
			return true
		}
	}
	return false
}

func cliContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		cancel()
	}()
	return ctx
}

func newSetCmd(g ext.CLIGlobals) *cobra.Command {
	var s Settings
	var useTLS, plain bool
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set the SMTP server and sender address",
		Long: `Set the SMTP server and the sender address. The password is stored in the
cluster (Secret shpyrd-mail) and never printed again; read it from a file
with @path rather than typing it where the shell history keeps it.

STARTTLS on port 587 is the default. Use --tls for servers that speak TLS
from the first byte (port 465), --plain only for a relay on localhost or a
private network.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.HasPrefix(s.Password, "@") {
				raw, err := os.ReadFile(strings.TrimPrefix(s.Password, "@"))
				if err != nil {
					return err
				}
				s.Password = strings.TrimSpace(string(raw))
			}
			switch {
			case useTLS && plain:
				return errors.New("--tls and --plain exclude each other")
			case useTLS:
				s.Security = SecurityTLS
			case plain:
				s.Security = SecurityNone
			default:
				s.Security = SecurityStartTLS
			}
			if err := s.Validate(); err != nil {
				return err
			}
			ctx := cliContext()
			d, err := connect(g)
			if err != nil {
				return err
			}
			if err := d.store.Set(ctx, s); err != nil {
				return err
			}
			d.audit(ctx, "mail.set", s.Addr(), "from "+s.From+", "+s.Security)
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Mail is sent through %s (%s) as %s.\n", s.Addr(), s.Security, s.From)
			if !d.enabled(ctx) {
				fmt.Fprintf(out, "The %s extension is not enabled on this cluster: run `shpyrd-ctl extensions enable %s`.\n", Name, Name)
				return nil
			}
			fmt.Fprintln(out, "Send a test message: shpyrd-ctl mail test you@example.com")
			return nil
		},
	}
	cmd.Flags().StringVar(&s.Host, "host", "", "SMTP server host name")
	cmd.Flags().IntVar(&s.Port, "port", 0, "SMTP port (587 with STARTTLS, 465 with --tls)")
	cmd.Flags().StringVar(&s.User, "user", "", "user name, when the server needs authentication")
	cmd.Flags().StringVar(&s.Password, "password", "", "password, or @path to read it from a file")
	cmd.Flags().StringVar(&s.From, "from", "", `sender address: "shpyrd <noreply@example.com>"`)
	cmd.Flags().BoolVar(&useTLS, "tls", false, "TLS from the first byte (port 465)")
	cmd.Flags().BoolVar(&plain, "plain", false, "no encryption (a trusted relay only)")
	_ = cmd.MarkFlagRequired("host")
	_ = cmd.MarkFlagRequired("from")
	return cmd
}

func newStatusCmd(g ext.CLIGlobals) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the sender settings (never the password)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cliContext()
			d, err := connect(g)
			if err != nil {
				return err
			}
			s, err := d.store.Get(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if s == nil {
				fmt.Fprintln(out, "Mail is not configured: run `shpyrd-ctl mail set`.")
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			fmt.Fprintf(tw, "SERVER\t%s\n", s.Addr())
			fmt.Fprintf(tw, "SECURITY\t%s\n", s.Security)
			fmt.Fprintf(tw, "FROM\t%s\n", s.From)
			auth := "none"
			if s.User != "" {
				auth = s.User
			}
			fmt.Fprintf(tw, "AUTH\t%s\n", auth)
			enabled := "no (run `shpyrd-ctl extensions enable mail`)"
			if d.enabled(ctx) {
				enabled = "yes"
			}
			fmt.Fprintf(tw, "ENABLED\t%s\n", enabled)
			return tw.Flush()
		},
	}
}

func newTestCmd(g ext.CLIGlobals) *cobra.Command {
	return &cobra.Command{
		Use:   "test <address>",
		Short: "Send a test message from the platform",
		Long: `Send a test message to an address. The server sends it from inside the
cluster, so the test proves what invitations will use: the settings, the
network path and the server's acceptance of the sender address.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cliContext()
			body, _ := json.Marshal(TestRequest{To: args[0]})
			raw, err := g.API().Request(ctx, "POST", "api/cluster/mail/test", body, "application/json")
			if err != nil {
				return err
			}
			var res struct {
				To   string `json:"to"`
				Took string `json:"took"`
			}
			_ = json.Unmarshal(raw, &res)
			fmt.Fprintf(cmd.OutOrStdout(), "Delivered a test message to %s (%s).\n", res.To, res.Took)
			return nil
		},
	}
}

func newUnsetCmd(g ext.CLIGlobals) *cobra.Command {
	return &cobra.Command{
		Use:   "unset",
		Short: "Remove the sender settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cliContext()
			d, err := connect(g)
			if err != nil {
				return err
			}
			if err := d.store.Delete(ctx); err != nil {
				return err
			}
			d.audit(ctx, "mail.unset", "", "")
			fmt.Fprintln(cmd.OutOrStdout(), "Mail settings removed; invitations show their link instead of sending it.")
			return nil
		},
	}
}
