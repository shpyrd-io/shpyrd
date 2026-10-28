package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
)

// SizesResponse is the instance size catalog as the dashboard sees it:
// the processes' sizes, and those of databases and stores (#57), each
// list sorted.
type SizesResponse struct {
	Default  string       `json:"default"`
	Sizes    []sizes.Size `json:"sizes"`
	Postgres sizes.List   `json:"postgres"`
	Redis    sizes.List   `json:"redis"`
}

// SizesOf renders a catalog for the API.
func SizesOf(cat *sizes.Catalog) SizesResponse {
	sorted := func(l *sizes.List) sizes.List { return sizes.List{Default: l.Default, Sizes: l.Sorted()} }
	return SizesResponse{Default: cat.Default, Sizes: cat.Sorted(), Postgres: sorted(cat.Postgres), Redis: sorted(cat.Redis)}
}

// loadCatalog reads the catalog ConfigMap, falling back to defaults.
func (s *Server) loadCatalog(c *gin.Context) (*sizes.Catalog, *corev1.ConfigMap, error) {
	return s.loadCatalogCtx(c.Request.Context())
}

// catalog is the size catalog for handlers without a gin context.
func (s *Server) catalog(ctx context.Context) (*sizes.Catalog, error) {
	cat, _, err := s.loadCatalogCtx(ctx)
	return cat, err
}

func (s *Server) loadCatalogCtx(ctx context.Context) (*sizes.Catalog, *corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{}
	err := s.apps.Get(ctx, types.NamespacedName{Namespace: s.kube.Namespace, Name: sizes.ConfigMapName}, cm)
	if apierrors.IsNotFound(err) {
		d := sizes.Defaults()
		return &d, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	cat, err := sizes.Parse([]byte(cm.Data[sizes.ConfigMapKey]))
	if err != nil {
		return nil, cm, err
	}
	return cat, cm, nil
}

func (s *Server) getSizes(c *gin.Context) {
	cat, _, err := s.loadCatalog(c)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, SizesOf(cat))
}

// putSizes replaces the whole catalog (the dashboard edits it as lists);
// a body without the lists of databases and stores keeps them.
func (s *Server) putSizes(c *gin.Context) {
	var req sizes.Catalog
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	if req.Postgres == nil || req.Redis == nil {
		cur, _, err := s.loadCatalog(c)
		if err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
		if req.Postgres == nil {
			req.Postgres = cur.Postgres
		}
		if req.Redis == nil {
			req.Redis = cur.Redis
		}
	}
	if err := req.Validate(); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	if err := s.saveCatalog(c, req); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, SizesOf(&req))
}

func (s *Server) saveCatalog(c *gin.Context, cat sizes.Catalog) error {
	data, err := cat.Marshal()
	if err != nil {
		return err
	}
	ctx := c.Request.Context()
	key := types.NamespacedName{Namespace: s.kube.Namespace, Name: sizes.ConfigMapName}
	cm := &corev1.ConfigMap{}
	if err := s.apps.Get(ctx, key, cm); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		cm = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}}
		cm.Data = map[string]string{sizes.ConfigMapKey: string(data)}
		return s.apps.Create(ctx, cm)
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data[sizes.ConfigMapKey] = string(data)
	return s.apps.Update(ctx, cm)
}

type resizeRequest struct {
	Process string `json:"process" binding:"required"`
	Size    string `json:"size" binding:"required"`
	// Force applies the size while a release is still rolling out (#127):
	// the rollout in progress is cancelled by it.
	Force bool `json:"force,omitempty"`
}

// resizeApp sets the instance size of one process type.
func (s *Server) resizeApp(c *gin.Context) {
	var req resizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	cat, _, err := s.loadCatalog(c)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	size, ok := cat.Get(req.Size)
	if !ok {
		abort(c, http.StatusBadRequest, fmt.Errorf("unknown size %q", req.Size))
		return
	}
	if err := s.fitsAPool(c.Request.Context(), size); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	app, err := s.mutateApp(c, func(a *shpyrdv1.App) error {
		if a.Spec.Processes == nil {
			a.Spec.Processes = map[string]shpyrdv1.Process{"web": {}}
		}
		p, ok := a.Spec.Processes[req.Process]
		if !ok && req.Process != "web" {
			return errors.New("unknown process " + req.Process)
		}
		if err := s.fitsItsMachine(c.Request.Context(), a, req.Process, size); err != nil {
			return err
		}
		p.Size = req.Size
		p.Resources = corev1.ResourceRequirements{} // the size is authoritative
		a.Spec.Processes[req.Process] = p
		return nil
	})
	if err != nil {
		return
	}
	s.audit(c, project.SlugOf(app), "resize", project.SlugOf(app), req.Process+"="+req.Size+forcedNote(req.Force))
	c.JSON(http.StatusOK, summarize(app))
}

// forcedNote marks an audit entry made over a rollout in progress.
func forcedNote(force bool) string {
	if force {
		return " (forced over a rollout in progress)"
	}
	return ""
}

// ProcessChange is one entry of an applyProcesses request.
type ProcessChange struct {
	Size     *string `json:"size,omitempty"`
	Replicas *int32  `json:"replicas,omitempty"`
	// Sleep sets HTTP sleep for the web process (RFC-0075):
	// {"after":"15m","resuming":"page"}. after "off" keeps the process
	// awake even when the workspace has a default; after "default" clears
	// the project's policy so the workspace's default applies (#135).
	Sleep *shpyrdv1.SleepSpec `json:"sleep,omitempty"`
	// Autoscale sets or clears the bounds the web process follows its load
	// between (RFC-0047): {"min":2,"max":10}, with "cpu" (percent of the
	// size's CPU) and "concurrency" (requests in flight per instance) when
	// the defaults will not do; {"off":true} clears them, and the process
	// keeps the count it last asked for.
	Autoscale *AutoscaleChange `json:"autoscale,omitempty"`
}

// AutoscaleChange is the bounds of a process as a request states them.
type AutoscaleChange struct {
	Off         bool   `json:"off,omitempty"`
	Min         int32  `json:"min,omitempty"`
	Max         int32  `json:"max,omitempty"`
	CPU         *int32 `json:"cpu,omitempty"`
	Concurrency *int32 `json:"concurrency,omitempty"`
}

// maxReplicas is the most instances a process may ask for, by count or by
// bound.
const maxReplicas = 100

// validateAutoscale checks a bounds change: only the web process follows
// its load, at least one instance, the maximum from the minimum up to
// maxReplicas, the CPU target a percentage, the requests in flight at
// least one. Returns the spec to store: nil for off.
func validateAutoscale(process string, ch *AutoscaleChange) (*shpyrdv1.AutoscaleSpec, error) {
	if process != "web" {
		return nil, fmt.Errorf("only the web process follows its load (got %s)", process)
	}
	if ch.Off {
		return nil, nil
	}
	if ch.Min < 1 {
		return nil, errors.New("autoscale min must be at least 1: a process that follows its load keeps one instance; sleep is what takes it to zero")
	}
	if ch.Max < ch.Min {
		return nil, fmt.Errorf("autoscale max must be at least the minimum (%d)", ch.Min)
	}
	if ch.Max > maxReplicas {
		return nil, fmt.Errorf("autoscale max must be at most %d", maxReplicas)
	}
	if ch.CPU != nil && (*ch.CPU < 1 || *ch.CPU > 100) {
		return nil, errors.New("autoscale cpu is a percentage of the size's CPU, between 1 and 100")
	}
	if ch.Concurrency != nil && *ch.Concurrency < 1 {
		return nil, errors.New("autoscale concurrency is the requests in flight an instance may carry, at least 1")
	}
	return &shpyrdv1.AutoscaleSpec{Min: ch.Min, Max: ch.Max, CPU: ch.CPU, Concurrency: ch.Concurrency}, nil
}

// canAutoscale reports whether KEDA's kinds are installed (the autoscale
// extension), by asking the REST mapper for the ScaledObject kind.
func (s *Server) canAutoscale() bool {
	if s.autoscaleAvailable != nil {
		return s.autoscaleAvailable()
	}
	if s.apps == nil {
		return false
	}
	for _, gk := range []schema.GroupKind{{Group: "keda.sh", Kind: "ScaledObject"}, {Group: "http.keda.sh", Kind: "InterceptorRoute"}} {
		if _, err := s.apps.RESTMapper().RESTMapping(gk); err != nil {
			return false
		}
	}
	return true
}

// errNoAutoscale answers bounds asked for on a cluster without KEDA's kinds.
var errNoAutoscale = errors.New("this cluster cannot autoscale yet: enable the autoscale extension first (shpyrd-ctl extensions enable autoscale)")

// setReplicas applies a count to a process: the count itself or, on a
// process that follows its load, its minimum (RFC-0047: `shpyrd scale
// web=3` on an autoscaled process sets min). The count is kept too, as
// what the process returns to when the bounds are taken away.
func (s *Server) setReplicas(ctx context.Context, a *shpyrdv1.App, name string, n int32) error {
	p := a.Spec.Processes[name]
	if p.Autoscale.Valid() {
		switch {
		case n == 0:
			return fmt.Errorf("%s follows its load between %d and %d instances and keeps at least the minimum; turn autoscaling off to stop it (shpyrd autoscale %s --off)", name, p.Autoscale.Min, p.Autoscale.Max, name)
		case n > p.Autoscale.Max:
			return fmt.Errorf("%s follows its load between %d and %d instances: a count sets the minimum, and %d is above the maximum; raise it first (shpyrd autoscale %s --max %d)", name, p.Autoscale.Min, p.Autoscale.Max, n, name, n)
		}
		as := *p.Autoscale
		as.Min = n
		p.Autoscale = &as
	}
	if err := s.checkScale(ctx, a, name, n); err != nil {
		return err
	}
	p.Replicas = ptr.To(n)
	a.Spec.Processes[name] = p
	return nil
}

// sleepAllowed says whether an extension lets things sleep by themselves
// now (ext.SleepGate: the enterprise's auto sleep, with a license).
func (s *Server) sleepAllowed() bool {
	if s.sleepGate != nil {
		return s.sleepGate()
	}
	for _, x := range s.opts.Extensions {
		if g, ok := x.(ext.SleepGate); ok && g.SleepAllowed() {
			return true
		}
	}
	return false
}

// errSleepLicensed answers a sleep policy asked for without auto sleep.
var errSleepLicensed = errors.New("available with a license: apps and databases sleeping by themselves is an enterprise feature")

// canSleep reports whether the KEDA HTTP add-on is installed, by asking the
// REST mapper for its InterceptorRoute kind.
func (s *Server) canSleep() bool {
	if s.sleepAvailable != nil {
		return s.sleepAvailable()
	}
	if s.apps == nil {
		return false
	}
	_, err := s.apps.RESTMapper().RESTMapping(schema.GroupKind{Group: "http.keda.sh", Kind: "InterceptorRoute"})
	return err == nil
}

// sleepOff is the after value of a policy that keeps a process awake. It
// is stored as it is: the project opts out of the workspace's default, and
// the controller reads it the same way (#135).
const sleepOff = "off"

// validateSleep checks a sleep change: only the web process sleeps, the
// quiet period is 5m..24h, resuming is page or wait. Returns the
// normalised spec: an explicit off for "off", nil for "default" (and for
// an empty value), which clears the project's policy so the workspace's
// default applies.
func validateSleep(process string, sp *shpyrdv1.SleepSpec) (*shpyrdv1.SleepSpec, error) {
	if process != "web" {
		return nil, fmt.Errorf("only the web process can sleep (got %s)", process)
	}
	after := strings.ToLower(strings.TrimSpace(sp.After))
	switch after {
	case "", "default":
		return nil, nil
	case sleepOff, "false":
		return &shpyrdv1.SleepSpec{After: sleepOff}, nil
	}
	d, err := time.ParseDuration(after)
	if err != nil {
		return nil, fmt.Errorf("sleep after: %q is not a quiet period (try 15m or 1h); off keeps the app awake, default follows the workspace", sp.After)
	}
	if d < 5*time.Minute || d > 24*time.Hour {
		return nil, errors.New("sleep after must be between 5m and 24h")
	}
	resuming := strings.ToLower(strings.TrimSpace(sp.Resuming))
	switch resuming {
	case "":
		resuming = "wait"
	case "page", "wait":
	default:
		return nil, fmt.Errorf("sleep resuming must be page or wait (got %s)", sp.Resuming)
	}
	return &shpyrdv1.SleepSpec{After: d.String(), Resuming: resuming}, nil
}

// errSleepWaitWithDisk refuses "resuming: wait" on a process with a disk
// (storage plan step 6): a held connection would wait for the volume to
// reattach, longer than clients wait.
const errSleepWaitWithDisk = "an app with a disk resumes behind the waiting page: the disk takes a moment to reattach (use --resuming page)"

// sleepTurnsOn reports whether a validated spec makes the process sleep:
// an explicit off and a cleared policy (nil) need neither auto sleep nor
// the add-on.
func sleepTurnsOn(sp *shpyrdv1.SleepSpec) bool {
	return sp != nil && sp.After != sleepOff
}

type applyProcessesRequest struct {
	Processes map[string]ProcessChange `json:"processes" binding:"required"`
	// Force applies the changes while a release is still rolling out
	// (#127): the rollout in progress is cancelled by them.
	Force bool `json:"force,omitempty"`
}

// applyProcesses changes sizes and instance counts of several process types
// in one update, so a batch of edits yields a single release and rollout.
func (s *Server) applyProcesses(c *gin.Context) {
	var req applyProcessesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	if len(req.Processes) == 0 {
		abort(c, http.StatusBadRequest, errors.New("no changes"))
		return
	}
	cat, _, err := s.loadCatalog(c)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	resizes := false
	sleeps := map[string]*shpyrdv1.SleepSpec{}
	bounds := map[string]*shpyrdv1.AutoscaleSpec{}
	for name, ch := range req.Processes {
		if ch.Autoscale != nil {
			as, err := validateAutoscale(name, ch.Autoscale)
			if err != nil {
				abort(c, http.StatusBadRequest, err)
				return
			}
			if as != nil && !s.canAutoscale() {
				abort(c, http.StatusConflict, errNoAutoscale)
				return
			}
			bounds[name] = as
		}
		if ch.Sleep != nil {
			sp, err := validateSleep(name, ch.Sleep)
			if err != nil {
				abort(c, http.StatusBadRequest, err)
				return
			}
			if sleepTurnsOn(sp) && !s.sleepAllowed() {
				abort(c, http.StatusPaymentRequired, errSleepLicensed)
				return
			}
			if sleepTurnsOn(sp) && !s.canSleep() {
				abort(c, http.StatusConflict, errors.New("this cluster cannot put apps to sleep yet: enable the sleep extension first (shpyrd-ctl extensions enable sleep)"))
				return
			}
			sleeps[name] = sp
		}
		if ch.Size != nil {
			size, ok := cat.Get(*ch.Size)
			if !ok {
				abort(c, http.StatusBadRequest, fmt.Errorf("unknown size %q for %s", *ch.Size, name))
				return
			}
			if err := s.fitsAPool(c.Request.Context(), size); err != nil {
				abort(c, http.StatusBadRequest, err)
				return
			}
			resizes = true
		}
		if ch.Replicas != nil && (*ch.Replicas < 0 || *ch.Replicas > maxReplicas) {
			abort(c, http.StatusBadRequest, fmt.Errorf("replicas must be between 0 and %d", maxReplicas))
			return
		}
	}
	app, err := s.mutateApp(c, func(a *shpyrdv1.App) error {
		if resizes && !req.Force && rolloutInProgress(a) {
			return fmt.Errorf("%s; wait for it to finish, or apply the change anyway with force (the rollout in progress is cancelled)", rolloutInProgressWords(a))
		}
		if a.Spec.Processes == nil {
			a.Spec.Processes = map[string]shpyrdv1.Process{"web": {}}
		}
		for name, ch := range req.Processes {
			p, ok := a.Spec.Processes[name]
			if !ok && name != "web" {
				return errors.New("unknown process " + name)
			}
			if ch.Size != nil {
				if size, ok := cat.Get(*ch.Size); ok {
					if err := s.fitsItsMachine(c.Request.Context(), a, name, size); err != nil {
						return err
					}
				}
				p.Size = *ch.Size
				p.Resources = corev1.ResourceRequirements{}
			}
			if ch.Autoscale != nil {
				// The maximum is what the process may come to: a
				// single-instance volume refuses it like any count.
				if as := bounds[name]; as != nil {
					if err := s.checkScale(c.Request.Context(), a, name, as.Max); err != nil {
						return err
					}
				}
				p.Autoscale = bounds[name] // nil clears the bounds
			}
			a.Spec.Processes[name] = p
			if ch.Replicas != nil {
				if err := s.setReplicas(c.Request.Context(), a, name, *ch.Replicas); err != nil {
					return err
				}
				p = a.Spec.Processes[name]
			}
			if ch.Sleep != nil {
				if sp := sleeps[name]; sleepTurnsOn(sp) && sp.Resuming == "wait" {
					if v := s.singleInstanceVolume(c.Request.Context(), a, name); v != "" {
						return fmt.Errorf("%s: %s", v, errSleepWaitWithDisk)
					}
				}
				p.Sleep = sleeps[name] // nil clears it: the workspace's default applies
			}
			a.Spec.Processes[name] = p
		}
		return nil
	})
	if err != nil {
		return
	}
	s.audit(c, project.SlugOf(app), "processes", project.SlugOf(app), processChangesDetail(req.Processes)+forcedNote(req.Force))
	c.JSON(http.StatusOK, summarize(app))
}

// processChangesDetail summarises a batch of process changes for the audit trail.
func processChangesDetail(changes map[string]ProcessChange) string {
	names := make([]string, 0, len(changes))
	for n := range changes {
		names = append(names, n)
	}
	sort.Strings(names)
	var parts []string
	for _, n := range names {
		ch := changes[n]
		if ch.Replicas != nil {
			parts = append(parts, fmt.Sprintf("%s=%d", n, *ch.Replicas))
		}
		if ch.Size != nil {
			parts = append(parts, fmt.Sprintf("%s:%s", n, *ch.Size))
		}
		if ch.Sleep != nil {
			// Off and the workspace's default are different states (#135).
			switch sp, _ := validateSleep(n, ch.Sleep); {
			case sp == nil:
				parts = append(parts, n+" sleep workspace default")
			case sp.After == sleepOff:
				parts = append(parts, n+" sleep off")
			default:
				parts = append(parts, fmt.Sprintf("%s sleep after %s (%s)", n, sp.After, sp.Resuming))
			}
		}
		if ch.Autoscale != nil {
			if ch.Autoscale.Off {
				parts = append(parts, n+" autoscale off")
			} else {
				parts = append(parts, fmt.Sprintf("%s autoscale %d-%d (cpu %d%%, %d in flight)", n, ch.Autoscale.Min, ch.Autoscale.Max, (&shpyrdv1.AutoscaleSpec{CPU: ch.Autoscale.CPU}).TargetCPU(), (&shpyrdv1.AutoscaleSpec{Concurrency: ch.Autoscale.Concurrency}).TargetConcurrency()))
			}
		}
	}
	return strings.Join(parts, " ")
}
