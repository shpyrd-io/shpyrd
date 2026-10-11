// Package authlocal is the auth-local extension (RFC-0007 step 3.1): a Dex
// identity broker. Optional local credentials are managed in Dex configuration.
package authlocal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/install"
)

// Name of the extension.
const Name = "auth-local"

var ErrNotEnabled = errors.New("the bundled Dex issuer is not enabled: run `shpyrd extensions enable auth-local`")

// ProviderID is the login provider id ("/api/auth/login?provider=local").
const ProviderID = "local"

type extension struct{}

// New returns the extension.
func New() ext.Extension { return extension{} }

func (extension) Name() string { return Name }
func (extension) Description() string {
	return "Bundled Dex identity broker; optional operator-managed static credentials"
}
func (extension) Components() []ext.ComponentRef {
	return []ext.ComponentRef{{Name: "dex", Runlevel: "rc4"}} // after dns (rc3): its certificate may use the DNS-01 issuer
}
func (extension) Register(ctrl.Manager, ext.Deps) error { return nil }
func (extension) Types() []ext.ResourceType             { return nil }

// Routes registers redirect-based Dex login; no password or user CRUD API is mounted.
func (extension) Routes(r ext.Router, deps ext.Deps) error {
	issuer := deps.Vars(install.VarAuthURL)
	if issuer == "" {
		return errors.New("SHPYRD_AUTH_URL is not set")
	}
	if deps.Auth != nil && deps.Kube != nil && deps.Var("SHPYRD_DEX_LOCAL_LOGIN") != "false" {
		go registerProvider(context.Background(), deps, issuer)
	}
	return nil
}

// registerProvider reads the client secret and registers Dex with the
// relying party, retrying while Dex is still starting (it may come up in
// the same runlevel as the server).
func registerProvider(ctx context.Context, deps ext.Deps, issuer string) {
	delay := 2 * time.Second
	for attempt := 1; ; attempt++ {
		sec, err := deps.Kube.Kube.CoreV1().Secrets(deps.SystemNamespace).Get(ctx, install.OIDCClientSecretName, metav1.GetOptions{})
		if err == nil {
			clientID, secret := strings.TrimSpace(string(sec.Data["client-id"])), strings.TrimSpace(string(sec.Data["client-secret"]))
			err = deps.Auth.AddOIDC(ctx, ext.OIDCProvider{
				ID: ProviderID, Label: "Email and password", Issuer: issuer, ConnectorID: "local",
				ClientID: clientID, ClientSecret: secret,
			})
			if err == nil {
				err = deps.Auth.AddOIDC(ctx, ext.OIDCProvider{ID: "local-console", Label: "Email and password", Issuer: issuer, ConnectorID: "local", Realm: ext.RealmConsole, ClientID: clientID, ClientSecret: secret})
			}
		}
		if err == nil {
			return
		}
		if attempt == 1 || attempt%10 == 0 {
			fmt.Printf("auth-local: login provider not ready yet (%v); retrying\n", err)
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

// Credentials live in Dex configuration; there is no shpyrd user/password API.
func (extension) CLI(ext.CLIGlobals) []*cobra.Command { return nil }
