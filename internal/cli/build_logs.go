package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// waitForFirstBuild explains why no logs exist yet, and follows preparation
// until a build can be streamed. A nil app means there is nothing to stream.
func (a *appClient) waitForFirstBuild(ctx context.Context, name string, app *shpyrdv1.App, follow bool, out io.Writer) (*shpyrdv1.App, error) {
	last := ""
	for app.Status.LatestBuild == "" {
		if app.Status.Phase == shpyrdv1.PhaseFailed {
			return nil, fmt.Errorf("build failed: %s", firstNonEmpty(app.Status.Message, "no build started"))
		}
		message := "No build has started yet: " + firstNonEmpty(app.Status.Message, "waiting for the first build")
		if message != last {
			fmt.Fprintln(out, message)
			last = message
		}
		if !follow || (app.Status.Phase != shpyrdv1.PhaseBuilding && app.Status.Phase != shpyrdv1.PhasePending) {
			return nil, nil
		}
		if err := sleepCtx(ctx, 2*time.Second); err != nil {
			return nil, err
		}
		var err error
		app, err = a.getApp(ctx, name)
		if err != nil {
			return nil, err
		}
	}
	return app, nil
}
