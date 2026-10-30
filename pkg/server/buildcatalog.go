package server

import (
	"context"
	"log/slog"
	"time"

	"k8s.io/client-go/dynamic"

	"github.com/shpyrd-io/shpyrd/pkg/buildcatalog"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// runRefreshBuildpacks is `shpyrd-server refresh-buildpacks`: every
// buildpack and stack of the build catalog pinned to the digest its tag
// points at now (pkg/buildcatalog). kpack assembles the builders again
// where something moved. Run weekly by the kpack component's CronJob, at
// an hour nobody deploys.
func runRefreshBuildpacks(logger *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	k, err := kube.Connect(kube.Options{})
	if err != nil {
		return err
	}
	res, err := buildcatalog.Refresh(ctx, k.Dynamic, buildcatalog.Registry{}, false)
	for _, c := range res.Changes {
		logger.Info("pin moved", "kind", c.Kind, "name", c.Name, "field", c.Field, "from", c.From, "to", c.To)
	}
	for _, l := range res.Left {
		logger.Info("left to kpack: its registry asks for a credential", "kind", l.Kind, "name", l.Name, "field", l.Field, "ref", l.Ref)
	}
	if len(res.Changes) == 0 && err == nil {
		logger.Info("the build catalog is current; nothing moved")
	}
	return err
}

// pinBuildCatalog pins what is not pinned yet, at the server's start: the
// catalog of a fresh install, or a buildpack a release named by a new tag.
// The digest is the one kpack has already resolved, so nothing rebuilds;
// from then on the tag's movements wait for the weekly refresh. A registry
// that cannot be reached is logged; the next start or the job tries again.
func pinBuildCatalog(ctx context.Context, dyn dynamic.Interface, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	res, err := buildcatalog.Refresh(ctx, dyn, buildcatalog.Registry{}, true)
	for _, c := range res.Changes {
		logger.Info("build catalog pinned", "kind", c.Kind, "name", c.Name, "field", c.Field, "to", c.To)
	}
	for _, l := range res.Left {
		logger.Info("build catalog: left to kpack, its registry asks for a credential", "kind", l.Kind, "name", l.Name, "field", l.Field, "ref", l.Ref)
	}
	if err != nil {
		logger.Info("the build catalog could not be pinned entirely; the weekly refresh tries again", "err", err.Error())
	}
}
