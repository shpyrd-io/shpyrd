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

// SizesResponse is the instance size catalog as the dashboard sees it.
type SizesResponse struct {
	Default string       `json:"default"`
	Sizes   []sizes.Size `json:"sizes"`
	// DatabaseMinMemory is the least a database runs with (a smaller size
	// is raised to it); DatabaseDefaultMemory what one that names no size
	// gets, unless the plan is small and it is given DatabaseSmallSize.
	DatabaseMinMemory     string `json:"databaseMinMemory"`
	DatabaseDefaultMemory string `json:"databaseDefaultMemory"`
	DatabaseSmallSize     string `json:"databaseSmallSize,omitempty"`
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
	out := SizesResponse{Default: cat.Default, Sizes: cat.Sorted(), DatabaseMinMemory: sizes.DBMinMemory, DatabaseDefaultMemory: sizes.DBDefaultMemory}
	if _, ok := cat.Get(sizes.DBXS); ok {
		out.DatabaseSmallSize = sizes.DBXS
	}
	c.JSON(http.StatusOK, out)
}

// putSizes replaces the whole catalog (the dashboard edits it as a list).
func (s *Server) putSizes(c *gin.Context) {
	var req sizes.Catalog
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	if err := req.Validate(); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	if err := s.saveCatalog(c, req); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, SizesResponse{Default: req.Default, Sizes: req.Sorted()})
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
	if _, ok := cat.Get(req.Size); !ok {
		abort(c, http.StatusBadRequest, fmt.Errorf("unknown size %q", req.Size))
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
		p.Size = req.Size
		p.Resources = corev1.ResourceRequirements{} // the size is authoritative
		a.Spec.Processes[req.Process] = p
		return nil
	})
	if err != nil {
		return
	}
	s.audit(c, project.SlugOf(app), "resize", project.SlugOf(app), req.Process+"="+req.Size)
	c.JSON(http.StatusOK, summarize(app))
}

// ProcessChange is one entry of an applyProcesses request.
type ProcessChange struct {
	Size     *string `json:"size,omitempty"`
	Replicas *int32  `json:"replicas,omitempty"`
	// Sleep sets or clears HTTP sleep for the web process (RFC-0075):
	// {"after":"15m","resuming":"page"}; after "off" disables.
	Sleep *shpyrdv1.SleepSpec `json:"sleep,omitempty"`
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

// validateSleep checks a sleep change: only the web process sleeps, the
// quiet period is 5m..24h (or "off"), resuming is page or wait. Returns
// the normalised spec, or nil when sleep is being disabled.
func validateSleep(process string, sp *shpyrdv1.SleepSpec) (*shpyrdv1.SleepSpec, error) {
	if process != "web" {
		return nil, fmt.Errorf("only the web process can sleep (got %s)", process)
	}
	after := strings.ToLower(strings.TrimSpace(sp.After))
	if after == "" || after == "off" || after == "false" {
		return nil, nil
	}
	d, err := time.ParseDuration(after)
	if err != nil {
		return nil, fmt.Errorf("sleep after: %q is not a duration (try 15m, 1h)", sp.After)
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

type applyProcessesRequest struct {
	Processes map[string]ProcessChange `json:"processes" binding:"required"`
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
	for name, ch := range req.Processes {
		if ch.Sleep != nil {
			sp, err := validateSleep(name, ch.Sleep)
			if err != nil {
				abort(c, http.StatusBadRequest, err)
				return
			}
			if sp != nil && !s.sleepAllowed() {
				abort(c, http.StatusPaymentRequired, errSleepLicensed)
				return
			}
			if sp != nil && !s.canSleep() {
				abort(c, http.StatusConflict, errors.New("this cluster cannot put apps to sleep yet: enable the sleep extension first (shpyrd-ctl extensions enable sleep)"))
				return
			}
			sleeps[name] = sp
		}
		if ch.Size != nil {
			if _, ok := cat.Get(*ch.Size); !ok {
				abort(c, http.StatusBadRequest, fmt.Errorf("unknown size %q for %s", *ch.Size, name))
				return
			}
			resizes = true
		}
		if ch.Replicas != nil && (*ch.Replicas < 0 || *ch.Replicas > 100) {
			abort(c, http.StatusBadRequest, errors.New("replicas must be between 0 and 100"))
			return
		}
	}
	app, err := s.mutateApp(c, func(a *shpyrdv1.App) error {
		if resizes && rolloutInProgress(a) {
			return fmt.Errorf("a release is still rolling out (%s); wait for it to finish", firstNonEmpty(a.Status.Message, a.Status.Phase))
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
				p.Size = *ch.Size
				p.Resources = corev1.ResourceRequirements{}
			}
			if ch.Replicas != nil {
				if err := s.checkScale(c.Request.Context(), a, name, *ch.Replicas); err != nil {
					return err
				}
				p.Replicas = ch.Replicas
			}
			if ch.Sleep != nil {
				p.Sleep = sleeps[name] // nil clears it
			}
			a.Spec.Processes[name] = p
		}
		return nil
	})
	if err != nil {
		return
	}
	s.audit(c, project.SlugOf(app), "processes", project.SlugOf(app), processChangesDetail(req.Processes))
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
			if sp, _ := validateSleep(n, ch.Sleep); sp == nil {
				parts = append(parts, n+" sleep off")
			} else {
				parts = append(parts, fmt.Sprintf("%s sleep after %s (%s)", n, sp.After, sp.Resuming))
			}
		}
	}
	return strings.Join(parts, " ")
}
