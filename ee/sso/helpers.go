//go:build !foss

package sso

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"

	"github.com/shpyrd-io/shpyrd/pkg/ext/authlocal"
)

// ErrNotEnabled says the bundled issuer, which auth-local installs, is not
// there: connectors live in it.
var ErrNotEnabled = authlocal.ErrNotEnabled

// wrap turns "no such resource" into ErrNotEnabled, as auth-local does.
func wrap(err error) error {
	if meta.IsNoMatchError(err) || apierrors.IsNotFound(err) && strings.Contains(err.Error(), "the server could not find the requested resource") {
		return ErrNotEnabled
	}
	return err
}

// cliContext ends on Ctrl-C.
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
