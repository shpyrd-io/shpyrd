package api

import (
	"context"
	"errors"
	"fmt"
	"k8s.io/apimachinery/pkg/api/resource"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
)

// ResourceView is any resource of a project in the shared shape the
// dashboard renders (RFC-0003): apps, volumes and, later, databases.
type ResourceView struct {
	Kind    string `json:"kind"` // App, Volume, ...
	Name    string `json:"name"`
	Phase   string `json:"phase"`
	Message string `json:"message,omitempty"`
	// Endpoint is the URL or host:port to reach the resource, when any.
	Endpoint string `json:"endpoint,omitempty"`
	// Details are kind-specific facts for display (size, mode, release).
	Details map[string]string `json:"details,omitempty"`
	// AttachedTo lists the apps (or app/process) using the resource.
	AttachedTo []string `json:"attachedTo"`
	// Data marks resources whose deletion loses data.
	Data      bool      `json:"data"`
	CreatedAt time.Time `json:"createdAt"`
	// Bindable resources can be attached to apps (Postgres, Redis).
	Bindable bool `json:"bindable"`
	// Note says, on creation, what the platform decided for the resource
	// and why (a database's size).
	Note string `json:"note,omitempty"`
}

// listProjectResources returns every resource of the project namespace.
func (s *Server) listProjectResources(c *gin.Context) {
	ns, ok := s.projectNamespace(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	var apps shpyrdv1.AppList
	if err := s.apps.List(ctx, &apps, client.InNamespace(ns)); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	var vols shpyrdv1.VolumeList
	if err := s.apps.List(ctx, &vols, client.InNamespace(ns)); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	out := make([]ResourceView, 0, len(apps.Items)+len(vols.Items))
	// Who attaches what, for the attachedTo column of bindable resources.
	attached := map[string][]string{} // "Kind/name" -> apps
	for _, a := range apps.Items {
		for _, b := range a.Spec.Bindings {
			attached[b.Kind+"/"+b.Name] = append(attached[b.Kind+"/"+b.Name], a.Name)
		}
	}
	for _, a := range apps.Items {
		v := ResourceView{
			Kind: "App", Name: a.Name, Phase: firstNonEmpty(a.Status.Phase, shpyrdv1.PhasePending), Message: a.Status.Message,
			Endpoint: a.Status.URL, Details: map[string]string{}, AttachedTo: []string{}, CreatedAt: a.CreationTimestamp.Time,
		}
		if cur := a.CurrentRelease(); cur != nil {
			v.Details["release"] = fmt.Sprintf("v%d", cur.Number)
		}
		if a.HasSource() {
			v.Details["build"] = a.BuildStrategy()
		}
		for _, b := range a.Spec.Bindings {
			v.Details["bindings"] = appendCSV(v.Details["bindings"], b.Kind+"/"+b.Name)
		}
		out = append(out, v)
	}
	for _, vol := range vols.Items {
		view := volumeView(vol)
		mode := "single-instance"
		if view.Shared {
			mode = "shared"
		}
		v := ResourceView{
			Kind: "Volume", Name: vol.Name, Phase: view.Phase, Message: view.Message,
			Details:    map[string]string{"size": view.Size, "mode": mode},
			AttachedTo: view.MountedBy, Data: true, CreatedAt: vol.CreationTimestamp.Time,
		}
		if view.Capacity != "" && view.Capacity != view.Size {
			v.Details["capacity"] = view.Capacity
		}
		out = append(out, v)
	}
	// Resources of enabled extensions, read generically through their
	// shared status shape.
	for _, t := range s.resourceTypes() {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{Group: t.Group, Version: t.Version, Kind: t.Kind + "List"})
		if err := s.apps.List(ctx, list, client.InNamespace(ns)); err != nil {
			continue // CRD missing: nothing of this kind
		}
		for _, u := range list.Items {
			v := resourceViewOf(t, u)
			v.AttachedTo = attached[t.Kind+"/"+u.GetName()]
			if v.AttachedTo == nil {
				v.AttachedTo = []string{}
			}
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == "App" || (out[j].Kind != "App" && out[i].Kind < out[j].Kind)
		}
		return out[i].Name < out[j].Name
	})
	c.JSON(http.StatusOK, out)
}

// resourceTypes lists the resource kinds of the enabled extensions.
func (s *Server) resourceTypes() []ext.ResourceType {
	var out []ext.ResourceType
	for _, x := range s.opts.Extensions {
		out = append(out, x.Types()...)
	}
	return out
}

// resourceType finds an enabled extension kind.
func (s *Server) resourceType(kind string) (ext.ResourceType, bool) {
	for _, t := range s.resourceTypes() {
		if strings.EqualFold(t.Kind, kind) {
			return t, true
		}
	}
	return ext.ResourceType{}, false
}

// resourceViewOf renders any extension resource from the shared status shape.
func resourceViewOf(t ext.ResourceType, u unstructured.Unstructured) ResourceView {
	phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")
	msg, _, _ := unstructured.NestedString(u.Object, "status", "message")
	endpoint, _, _ := unstructured.NestedString(u.Object, "status", "endpoint")
	v := ResourceView{
		Kind: t.Kind, Name: u.GetName(), Phase: firstNonEmpty(phase, shpyrdv1.ResourcePending), Message: msg, Endpoint: endpoint,
		Details: map[string]string{}, AttachedTo: []string{}, Data: true, Bindable: t.Bindable, CreatedAt: u.GetCreationTimestamp().Time,
	}
	if spec, ok, _ := unstructured.NestedMap(u.Object, "spec"); ok {
		for k, val := range spec {
			switch x := val.(type) {
			case string:
				v.Details[k] = x
			case bool:
				v.Details[k] = fmt.Sprint(x)
			case int64, float64:
				v.Details[k] = fmt.Sprint(x)
			}
		}
	}
	// Backups (RFC-0038): on/off with retention, the last one and the window.
	if b, ok, _ := unstructured.NestedMap(u.Object, "spec", "backups"); ok && b != nil {
		retention, _ := b["retention"].(string)
		v.Details["backups"] = "daily, kept " + firstNonEmpty(retention, "14d")
		if last, _, _ := unstructured.NestedString(u.Object, "status", "lastBackup"); last != "" {
			v.Details["lastBackup"] = last
		}
		if from, _, _ := unstructured.NestedString(u.Object, "status", "recoverableFrom"); from != "" {
			v.Details["recoverableFrom"] = from
		}
	}
	if rec, ok, _ := unstructured.NestedMap(u.Object, "spec", "recovery"); ok && rec != nil {
		from, _ := rec["from"].(string)
		v.Details["restoredFrom"] = from
	}
	// The disk is the provider's minimum when the request was below it
	// (RFC-0060): show what exists, not what was asked.
	if eff, _, _ := unstructured.NestedString(u.Object, "status", "storage"); eff != "" {
		if req := v.Details["storage"]; req != "" && req != eff {
			v.Details["storage"] = eff + " (" + req + " requested; provider minimum)"
		} else {
			v.Details["storage"] = eff
		}
	}
	return v
}

// CreateResourceRequest creates a resource of an extension kind.
type CreateResourceRequest struct {
	Kind string                 `json:"kind" binding:"required"`
	Name string                 `json:"name" binding:"required"`
	Spec map[string]interface{} `json:"spec"`
}

// createResource creates an extension resource in the project; the CRD
// schema validates the spec.
func (s *Server) createResource(c *gin.Context) {
	var req CreateResourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	t, ok := s.resourceType(req.Kind)
	if !ok {
		abort(c, http.StatusBadRequest, fmt.Errorf("resources of kind %q are not available on this cluster (enable the extension that provides them)", req.Kind))
		return
	}
	if req.Spec == nil {
		req.Spec = map[string]interface{}{}
	}
	s.createResourceOf(c, t, req.Name, req.Spec, "resource.create")
}

// createResourceOf makes a resource of kind t named name in the request's
// project, audited as action: a database or store gets its size recorded
// and counted against the workspace's limits, a database restored from
// another's backups has its source and moment checked first (#74).
func (s *Server) createResourceOf(c *gin.Context, t ext.ResourceType, name string, spec map[string]interface{}, action string) {
	if !volumeName.MatchString(name) {
		abort(c, http.StatusBadRequest, errors.New("name must be lowercase letters, digits and dashes (max 40 chars)"))
		return
	}
	ns, ok := s.projectNamespace(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if t.Kind == "Postgres" {
		if rec, ok := spec["recovery"].(map[string]interface{}); ok {
			from, _ := rec["from"].(string)
			var to *time.Time
			if raw, _ := rec["targetTime"].(string); raw != "" {
				at, err := time.Parse(time.RFC3339, raw)
				if err != nil {
					abort(c, http.StatusBadRequest, fmt.Errorf("recovery.targetTime %q: use RFC 3339, e.g. 2026-09-25T10:00:00Z", raw))
					return
				}
				to = &at
			}
			if err := s.checkRecovery(ctx, ns, name, from, to); err != nil {
				abort(c, http.StatusBadRequest, err)
				return
			}
		}
	}
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": t.Group + "/" + t.Version,
		"kind":       t.Kind,
		"metadata": map[string]interface{}{
			"name": name, "namespace": ns,
			"labels": map[string]interface{}{shpyrdv1.LabelManagedBy: "shpyrd", shpyrdv1.LabelProject: c.Param("slug")},
		},
		"spec": spec,
	}}
	note := ""
	if t.Kind == "Postgres" || t.Kind == "Redis" {
		n, err := s.storeSize(ctx, s.workspace(c), t.Kind, spec)
		if err != nil {
			abort(c, http.StatusBadRequest, err)
			return
		}
		note = n
		// Its disk counts against the workspace's storage ceiling, as the
		// namespace quota will count it: refused here, in words, rather
		// than left waiting for a claim the quota never admits.
		if disk, rounded, has := s.storeDisk(t.Kind, spec); has {
			if err := s.checkStorageLimit(ctx, s.workspace(c), disk); err != nil {
				abort(c, http.StatusConflict, err)
				return
			}
			if rounded {
				note = strings.TrimSpace(note + fmt.Sprintf(" Its disk is %s, the provider minimum.", disk.String()))
			}
		}
	}
	if err := s.apps.Create(ctx, u); err != nil {
		switch {
		case apierrors.IsAlreadyExists(err):
			abort(c, http.StatusConflict, fmt.Errorf("%s %q already exists", t.Kind, name))
		case apierrors.IsInvalid(err):
			abort(c, http.StatusBadRequest, err)
		default:
			abort(c, http.StatusBadGateway, err)
		}
		return
	}
	s.audit(c, c.Param("slug"), action, t.Kind+" "+name, "")
	view := resourceViewOf(t, *u)
	view.Note = note
	c.JSON(http.StatusCreated, view)
}

// storeSize records the size of a new database or store (#57): the one
// it names from the list of its kind, or that list's default, which the
// note says; one past the workspace's CPU or memory is refused.
func (s *Server) storeSize(ctx context.Context, ws, kind string, spec map[string]interface{}) (string, error) {
	cat, err := s.catalog(ctx)
	if err != nil {
		return "", err
	}
	what := strings.ToLower(kind)
	named, _ := spec["size"].(string)
	size, err := cat.For(what).Pick(what, named)
	if err != nil {
		return "", err
	}
	spec["size"] = size.Name
	var instances *int32
	switch n := spec["instances"].(type) {
	case float64:
		instances = ptrTo(int32(n))
	case int64:
		instances = ptrTo(int32(n))
	}
	res, count, _ := storeResources(cat, kind, size.Name, instances)
	if err := s.checkStoreLimits(ctx, ws, res, corev1.ResourceRequirements{}, count, 0); err != nil {
		return "", err
	}
	if named != "" {
		return "", nil
	}
	return fmt.Sprintf("It has the size %s, the default: %s.", size.Name, sizeWords(size)), nil
}

// sizeWords says what a size gives: "0.5 CPU, 128Mi of memory, 20 connections".
func sizeWords(size sizes.Size) string {
	out := fmt.Sprintf("%s CPU, %s of memory", size.CPU, size.Memory)
	if size.Connections > 0 {
		out += fmt.Sprintf(", %d connections", size.Connections)
	}
	return out
}

func ptrTo[T any](v T) *T { return &v }

// resizeResourceRequest is POST /api/projects/:slug/resources/:kind/:name/resize.
type resizeResourceRequest struct {
	Size string `json:"size" binding:"required"`
}

// resizeResource gives a database or store another size of its kind's
// list (#57), counted against the workspace's limits instead of the size
// it had. The controller applies it: a Postgres's instances restart one
// at a time, a Redis restarts (a cache comes back empty).
func (s *Server) resizeResource(c *gin.Context) {
	var req resizeResourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, errors.New(`body must be {"size":"shared-m"}`))
		return
	}
	t, ok := s.resourceType(c.Param("kind"))
	if !ok || (t.Kind != "Postgres" && t.Kind != "Redis") {
		abort(c, http.StatusBadRequest, fmt.Errorf("resources of kind %q have no size", c.Param("kind")))
		return
	}
	ns, ok := s.projectNamespace(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	cat, err := s.catalog(ctx)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	what := strings.ToLower(t.Kind)
	size, err := cat.For(what).Pick(what, req.Size)
	if err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	name := c.Param("name")
	var u *unstructured.Unstructured
	was := ""
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		u = &unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{Group: t.Group, Version: t.Version, Kind: t.Kind})
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, u); err != nil {
			return err
		}
		was, _, _ = unstructured.NestedString(u.Object, "spec", "size")
		if was == size.Name {
			return nil
		}
		var instances *int32
		if n, found, _ := unstructured.NestedInt64(u.Object, "spec", "instances"); found {
			instances = ptrTo(int32(n))
		}
		before, beforeN, _ := storeResources(cat, t.Kind, was, instances)
		after, afterN, _ := storeResources(cat, t.Kind, size.Name, instances)
		if err := s.checkStoreLimits(ctx, s.workspace(c), after, before, afterN, beforeN); err != nil {
			return &userError{err}
		}
		_ = unstructured.SetNestedField(u.Object, size.Name, "spec", "size")
		return s.apps.Update(ctx, u)
	})
	if err != nil {
		var ue *userError
		switch {
		case errors.As(err, &ue):
			abort(c, http.StatusBadRequest, ue.error)
		default:
			abortNotFound(c, err, strings.ToLower(t.Kind)+" "+name)
		}
		return
	}
	view := resourceViewOf(t, *u)
	if was == size.Name {
		view.Note = fmt.Sprintf("%s already has the size %s.", name, size.Name)
		c.JSON(http.StatusOK, view)
		return
	}
	s.audit(c, c.Param("slug"), "resource.resize", t.Kind+" "+name, firstNonEmpty(was, "none")+" → "+size.Name)
	restart := "Its instances restart with it, one at a time; with one instance the database is unavailable for a moment."
	if t.Kind == "Redis" {
		restart = "It restarts with it: a cache comes back empty, a persistent store reloads its file."
	}
	view.Note = fmt.Sprintf("%s now has the size %s: %s. %s", name, size.Name, sizeWords(size), restart)
	c.JSON(http.StatusOK, view)
}

// deleteResource removes an extension resource; attached ones need ?force=true.
func (s *Server) deleteResource(c *gin.Context) {
	t, ok := s.resourceType(c.Param("kind"))
	if !ok {
		abort(c, http.StatusNotFound, errors.New("unknown resource kind"))
		return
	}
	ns, ok := s.projectNamespace(c)
	if !ok {
		return
	}
	name := c.Param("name")
	var apps shpyrdv1.AppList
	_ = s.apps.List(c.Request.Context(), &apps, client.InNamespace(ns))
	var bound []string
	for _, a := range apps.Items {
		for _, b := range a.Spec.Bindings {
			if strings.EqualFold(b.Kind, t.Kind) && b.Name == name {
				bound = append(bound, a.Name)
			}
		}
	}
	if len(bound) > 0 && c.Query("force") != "true" {
		abort(c, http.StatusConflict, fmt.Errorf("%s %s is attached to %s: detach it first (or force)", t.Kind, name, strings.Join(bound, ", ")))
		return
	}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: t.Group, Version: t.Version, Kind: t.Kind})
	u.SetNamespace(ns)
	u.SetName(name)
	if err := s.apps.Delete(c.Request.Context(), u); err != nil {
		abortNotFound(c, err, strings.ToLower(t.Kind))
		return
	}
	s.audit(c, c.Param("slug"), "resource.delete", t.Kind+" "+name, "")
	c.Status(http.StatusNoContent)
}

// ---- bindings (attach / detach) -----------------------------------------------

// BindingRequest attaches a resource to the app.
type BindingRequest struct {
	Kind   string `json:"kind" binding:"required"`
	Name   string `json:"name" binding:"required"`
	Prefix string `json:"prefix,omitempty"`
}

var prefixRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,30}$`)

// attachResource adds a binding: a config release once the resource is ready.
func (s *Server) attachResource(c *gin.Context) {
	var req BindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	t, ok := s.resourceType(req.Kind)
	if !ok || !t.Bindable {
		abort(c, http.StatusBadRequest, fmt.Errorf("resources of kind %q cannot be attached", req.Kind))
		return
	}
	if req.Prefix != "" && !prefixRe.MatchString(req.Prefix) {
		abort(c, http.StatusBadRequest, errors.New("prefix must be letters, digits and underscores"))
		return
	}
	ns, ok := s.projectNamespace(c)
	if !ok {
		return
	}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: t.Group, Version: t.Version, Kind: t.Kind})
	if err := s.apps.Get(c.Request.Context(), types.NamespacedName{Namespace: ns, Name: req.Name}, u); err != nil {
		abortNotFound(c, err, strings.ToLower(t.Kind)+" "+req.Name)
		return
	}
	app, err := s.mutateApp(c, func(a *shpyrdv1.App) error {
		for _, b := range a.Spec.Bindings {
			if b.Kind == t.Kind && b.Name == req.Name {
				return fmt.Errorf("%s %s is already attached", t.Kind, req.Name)
			}
		}
		a.Spec.Bindings = append(a.Spec.Bindings, shpyrdv1.Binding{Kind: t.Kind, Name: req.Name, Prefix: strings.ToUpper(req.Prefix)})
		if a.Annotations == nil {
			a.Annotations = map[string]string{}
		}
		a.Annotations[shpyrdv1.AnnotationReleaseNote] = fmt.Sprintf("Attach %s %s", t.Kind, req.Name)
		return nil
	})
	if err != nil {
		return
	}
	s.audit(c, project.SlugOf(app), "attach", t.Kind+" "+req.Name, req.Prefix)
	c.JSON(http.StatusOK, summarize(app))
}

// detachResource removes a binding.
func (s *Server) detachResource(c *gin.Context) {
	kind, name := c.Param("kind"), c.Param("name")
	app, err := s.mutateApp(c, func(a *shpyrdv1.App) error {
		kept := a.Spec.Bindings[:0]
		found := false
		for _, b := range a.Spec.Bindings {
			if strings.EqualFold(b.Kind, kind) && b.Name == name {
				found = true
				continue
			}
			kept = append(kept, b)
		}
		if !found {
			return fmt.Errorf("%s %s is not attached", kind, name)
		}
		a.Spec.Bindings = kept
		if a.Annotations == nil {
			a.Annotations = map[string]string{}
		}
		a.Annotations[shpyrdv1.AnnotationReleaseNote] = fmt.Sprintf("Detach %s %s", kind, name)
		return nil
	})
	if err != nil {
		return
	}
	s.audit(c, project.SlugOf(app), "detach", kind+" "+name, "")
	c.JSON(http.StatusOK, summarize(app))
}

func appendCSV(list, item string) string {
	if list == "" {
		return item
	}
	return list + ", " + item
}

// ---- Postgres sleep (RFC-0075, section 5) ---------------------------------

// postgresSleepRequest is PATCH /api/projects/:slug/resources/postgres/:name.
type postgresSleepRequest struct {
	Sleep *struct {
		After string `json:"after"`
	} `json:"sleep"`
}

// patchPostgresSleep sets or clears the automatic hibernation policy of a
// database. Only single-instance databases sleep: an HA database exists to
// be available. Apps attached to the database get a config release when
// the policy appears or disappears (the host moves between "<name>-rw" and
// the shpyrd Service "<name>"); the CLI says so.
//
// Three states, as for a web process (#135): a quiet period of its own,
// an explicit "off" (stored, so the database stays awake when the
// workspace has a default for databases), and "default" (the policy is
// cleared and the workspace's default applies).
func (s *Server) patchPostgresSleep(c *gin.Context) {
	var req postgresSleepRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Sleep == nil {
		abort(c, http.StatusBadRequest, errors.New(`body must be {"sleep":{"after":"30m"}}; "off" keeps the database awake, "default" follows the workspace`))
		return
	}
	after := strings.ToLower(strings.TrimSpace(req.Sleep.After))
	switch after {
	case "", "default":
		after = ""
	case sleepOff, "false":
		after = sleepOff
	default:
		d, err := time.ParseDuration(after)
		if err != nil {
			abort(c, http.StatusBadRequest, fmt.Errorf("sleep after: %q is not a quiet period (try 30m or 2h); off keeps the database awake, default follows the workspace", req.Sleep.After))
			return
		}
		if d < 5*time.Minute || d > 24*time.Hour {
			abort(c, http.StatusBadRequest, errors.New("sleep after must be between 5m and 24h"))
			return
		}
		after = d.String()
		if !s.sleepAllowed() {
			abort(c, http.StatusPaymentRequired, errSleepLicensed)
			return
		}
	}
	pg, err := s.mutatePostgres(c, func(pg *shpyrdv1.Postgres) error {
		if after != "" && after != sleepOff && pg.Spec.Instances != nil && *pg.Spec.Instances > 1 {
			return fmt.Errorf("%s runs %d instances (high availability) and never sleeps", pg.Name, *pg.Spec.Instances)
		}
		if after == "" {
			// Back to the workspace's default; a suspension by hand stays.
			if pg.Spec.Sleep != nil {
				pg.Spec.Sleep.After = ""
				if !pg.Spec.Sleep.Suspended {
					pg.Spec.Sleep = nil
				}
			}
			return nil
		}
		if pg.Spec.Sleep == nil {
			pg.Spec.Sleep = &shpyrdv1.PostgresSleepSpec{}
		}
		pg.Spec.Sleep.After = after
		return nil
	})
	if err != nil {
		return
	}
	detail := "workspace default"
	switch after {
	case sleepOff:
		detail = "off"
	case "":
	default:
		detail = "after " + after
	}
	s.audit(c, c.Param("slug"), "postgres.sleep", pg.Name, detail)
	c.JSON(http.StatusOK, gin.H{"name": pg.Name, "sleep": pg.Spec.Sleep})
}

// suspendPostgres hibernates a database now with no wake on connect;
// resumePostgres brings it back. Both are POSTs without a body.
func (s *Server) suspendPostgres(c *gin.Context) { s.setPostgresSuspended(c, true) }
func (s *Server) resumePostgres(c *gin.Context)  { s.setPostgresSuspended(c, false) }

func (s *Server) setPostgresSuspended(c *gin.Context, suspended bool) {
	pg, err := s.mutatePostgres(c, func(pg *shpyrdv1.Postgres) error {
		if suspended && pg.Spec.Instances != nil && *pg.Spec.Instances > 1 {
			return fmt.Errorf("%s runs %d instances (high availability); scale it to 1 before suspending", pg.Name, *pg.Spec.Instances)
		}
		if pg.Spec.Sleep == nil {
			if !suspended {
				return nil
			}
			pg.Spec.Sleep = &shpyrdv1.PostgresSleepSpec{}
		}
		pg.Spec.Sleep.Suspended = suspended
		if !suspended && pg.Spec.Sleep.After == "" {
			pg.Spec.Sleep = nil // resumed with no policy: back to plain -rw
		}
		return nil
	})
	if err != nil {
		return
	}
	action := "postgres.resume"
	if suspended {
		action = "postgres.suspend"
	}
	s.audit(c, c.Param("slug"), action, pg.Name, "")
	c.JSON(http.StatusOK, gin.H{"name": pg.Name, "suspended": suspended})
}

// mutatePostgres loads a Postgres of the project, applies mutate and updates
// it; conflicts are retried like mutateApp. Errors are already written.
func (s *Server) mutatePostgres(c *gin.Context, mutate func(*shpyrdv1.Postgres) error) (*shpyrdv1.Postgres, error) {
	ns, ok := s.projectNamespace(c)
	if !ok {
		return nil, errors.New("project not found")
	}
	name := c.Param("name")
	var out *shpyrdv1.Postgres
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pg := &shpyrdv1.Postgres{}
		if err := s.apps.Get(c.Request.Context(), types.NamespacedName{Namespace: ns, Name: name}, pg); err != nil {
			return err
		}
		if err := mutate(pg); err != nil {
			return &userError{err}
		}
		if err := s.apps.Update(c.Request.Context(), pg); err != nil {
			return err
		}
		out = pg
		return nil
	})
	if err != nil {
		var ue *userError
		switch {
		case errors.As(err, &ue):
			abort(c, http.StatusBadRequest, ue.error)
		case apierrors.IsNotFound(err):
			abort(c, http.StatusNotFound, fmt.Errorf("no database %q in this project", name))
		default:
			abort(c, http.StatusBadGateway, err)
		}
		return nil, err
	}
	return out, nil
}

// userError marks a mutate error as the caller's (400, not 502).
type userError struct{ error }

// storeRequestedDisk reads the disk size a database or store asked for.
func storeRequestedDisk(spec map[string]interface{}) *resource.Quantity {
	raw, _ := spec["storage"].(string)
	if raw == "" {
		return nil
	}
	q, err := resource.ParseQuantity(raw)
	if err != nil {
		return nil
	}
	return &q
}

// storeDisk is the disk a new database or persistent store gets: the
// request, or the default, rounded to the provider minimum; rounded says
// whether the minimum made it larger than asked. A store without
// persistence has none.
func (s *Server) storeDisk(kind string, spec map[string]interface{}) (disk resource.Quantity, rounded, has bool) {
	requested := storeRequestedDisk(spec)
	var base resource.Quantity
	switch kind {
	case "Postgres":
		disk, base = s.databaseDiskSize(requested), resource.MustParse("5Gi")
	case "Redis":
		if persistent, _ := spec["persistent"].(bool); !persistent {
			return resource.Quantity{}, false, false
		}
		disk, base = s.redisDiskSize(requested), resource.MustParse("1Gi")
	default:
		return resource.Quantity{}, false, false
	}
	if requested != nil {
		base = *requested
	}
	return disk, disk.Cmp(base) > 0, true
}
