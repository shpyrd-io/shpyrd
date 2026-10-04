//go:build !foss

// Package sso is the enterprise's sign-in through other identity providers
// (ee/LICENSE): GitHub, Google, Microsoft and any OpenID Connect issuer,
// for a workspace or for the console, added as connectors to the bundled
// issuer that auth-local installs (RFC-0058). The platform's own sign-in,
// email and password, stays in the core. Without a license in force the
// connectors stay stored, their buttons leave the login pages, and their
// routes answer 402.
package sso

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/shpyrd-io/shpyrd/ee/licensing"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/install"
)

// Name of the extension.
const Name = "sso"

type extension struct{}

// New returns the SSO extension.
func New() ext.Extension { return extension{} }

func (extension) Name() string { return Name }
func (extension) Description() string {
	return "Sign-in through GitHub, Google, Microsoft or any OIDC issuer"
}
func (extension) Components() []ext.ComponentRef        { return nil }
func (extension) Register(ctrl.Manager, ext.Deps) error { return nil }
func (extension) Types() []ext.ResourceType             { return nil }

// Routes mounts the login methods' routes behind the license and registers
// the connectors that exist as login providers.
func (extension) Routes(r ext.Router, deps ext.Deps) error {
	issuer := deps.Var(install.VarAuthURL)
	if issuer == "" {
		return nil // no bundled issuer: auth-local is not enabled
	}
	if deps.Auth != nil && deps.Kube != nil {
		go registerConnectors(context.Background(), deps, issuer)
	}
	// Login methods (RFC-0058 connectors) managed from the Workspace page:
	// the platform's at the console, a workspace's own at its host
	// (RFC-0033 per-workspace SSO).
	ch := &connectorHandlers{deps: deps, issuer: issuer}
	api := r.Admin().Group("", licensing.Require())
	api.GET("/auth/connectors", ch.list)
	api.POST("/auth/connectors", ch.add)
	api.DELETE("/auth/connectors/:id", ch.remove)
	wh := &connectorHandlers{deps: deps, issuer: issuer, scoped: true}
	wa := r.WorkspaceAdmin().Group("", licensing.Require())
	wa.GET("/workspace/login-methods", wh.list)
	wa.POST("/workspace/login-methods", wh.add)
	wa.DELETE("/workspace/login-methods/:id", wh.remove)
	return nil
}

// CLI: the platform's connectors are the operator's; a workspace's own
// sign-in methods are its admins'.
func (extension) CLI(g ext.CLIGlobals) []*cobra.Command {
	return []*cobra.Command{ext.ForOperator(newAuthConnectorCmd(g)), newSSOCmd(g)}
}

// registerConnectors registers every connector as a login provider, with the
// Dex client the password method uses, retrying while Dex is starting.
func registerConnectors(ctx context.Context, deps ext.Deps, issuer string) {
	delay := 2 * time.Second
	for attempt := 1; ; attempt++ {
		err := registerOnce(ctx, deps, issuer)
		if err == nil || errors.Is(err, ErrNotEnabled) {
			return
		}
		if attempt == 1 || attempt%10 == 0 {
			slog.Info("sso: connectors not registered yet; retrying", "error", err)
		}
		if delay < 30*time.Second {
			delay *= 2
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func registerOnce(ctx context.Context, deps ext.Deps, issuer string) error {
	sec, err := deps.Kube.Kube.CoreV1().Secrets(deps.SystemNamespace).Get(ctx, install.OIDCClientSecretName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	clientID, secret := strings.TrimSpace(string(sec.Data["client-id"])), strings.TrimSpace(string(sec.Data["client-secret"]))
	cs := &ConnectorStore{Dynamic: deps.Kube.Dynamic, Namespace: deps.SystemNamespace, Issuer: issuer}
	// Connectors from before RFC-0080 are keyed by workspace slug: move
	// them to the workspace's id, once.
	if deps.Store != nil {
		moved, err := cs.Rekey(ctx, func(slug string) string {
			if ws, err := deps.Store.Workspace(ctx, slug); err == nil {
				return ids.Short(ws.ID)
			}
			return ""
		})
		if err != nil && !errors.Is(err, ErrNotEnabled) {
			return fmt.Errorf("rekey connectors: %w", err)
		}
		if moved > 0 {
			slog.Info("workspace connectors rekeyed to workspace ids (RFC-0080)", "moved", moved)
		}
	}
	list, err := cs.List(ctx)
	if err != nil {
		return err
	}
	for _, c := range list {
		if err := deps.Auth.AddOIDC(ctx, ext.OIDCProvider{
			ID: c.FullID, Label: c.Name, Kind: c.Type, ConnectorID: c.FullID, Issuer: issuer,
			ClientID: clientID, ClientSecret: secret, Realm: c.Realm, Workspace: c.Workspace,
			Available: licensing.Active,
		}); err != nil {
			return fmt.Errorf("connector %s: %w", c.FullID, err)
		}
	}
	return nil
}
