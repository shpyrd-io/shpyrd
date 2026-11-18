package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/authz"
	"github.com/shpyrd-io/shpyrd/pkg/catalog"
	"github.com/shpyrd-io/shpyrd/pkg/configvars"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/project"
)

// Updating a project from the catalog. The catalog publishes a different
// version of the entry; a person previews what applying it does, then
// applies it. An update only adds: it never deletes anything and never
// overwrites a value a person set. What it does with each part of the
// project:
//
//   - the source (commit, sub-path, overlay) and the build settings are the
//     entry's, and are set;
//   - the entry's processes are applied (command, arguments, port, mounts,
//     health check); the replicas, size and sleep and autoscale settings
//     of a process that exists stay the person's, new processes are
//     added, and a process the entry no longer declares is kept;
//   - the entry's plain variables are set for the keys the entry
//     declares;
//   - an input that is already a config var is left as it is; a new
//     generated input is generated, a new one with a default takes it,
//     and a new required one must be given;
//   - volumes and resources the project lacks are made; the ones it has
//     are not touched.

// CatalogUpdatePreview is what applying the offered version does.
type CatalogUpdatePreview struct {
	Entry string `json:"entry"`
	// State is the project's catalog state: update-available when there
	// is something to apply, and Reason says why when there is not.
	State  string              `json:"state,omitempty"`
	Reason string              `json:"reason,omitempty"`
	From   *CatalogVersionView `json:"from,omitempty"`
	To     *CatalogVersionView `json:"to,omitempty"`
	// Repository is the code the project will be set to, at the commit
	// of To.
	Repository string `json:"repository,omitempty"`
	// Inputs are the entry's inputs the project does not have yet.
	Inputs []CatalogUpdateInput `json:"inputs"`
	// Volumes and Resources are what applying makes.
	Volumes   []string `json:"volumes"`
	Resources []string `json:"resources"`
	// ProcessesAdded and ProcessesChanged are the entry's process types
	// that are new to the project, or whose definition differs.
	ProcessesAdded   []string `json:"processesAdded"`
	ProcessesChanged []string `json:"processesChanged"`
	// EnvChanged names the entry's plain variables that are new or differ.
	EnvChanged     []string `json:"envChanged"`
	BuildChanged   bool     `json:"buildChanged"`
	OverlayChanged bool     `json:"overlayChanged"`
}

// CatalogUpdateInput is one input the update adds, and what happens to it.
type CatalogUpdateInput struct {
	Name     string `json:"name"`
	Label    string `json:"label,omitempty"`
	Secret   bool   `json:"secret,omitempty"`
	Required bool   `json:"required,omitempty"`
	// Action is "ask" (the person gives it), "generate" or "default".
	Action  string `json:"action"`
	Default string `json:"default,omitempty"`
}

// CatalogUpdateRequest applies the version that was previewed.
type CatalogUpdateRequest struct {
	// Digest is the digest of the entry the person previewed: the update
	// is refused if the catalog has moved on since.
	Digest string            `json:"digest"`
	Inputs map[string]string `json:"inputs,omitempty"`
}

// CatalogUpdateResult says what an apply did.
type CatalogUpdateResult struct {
	// Changed is false when nothing was done; Reason says why.
	Changed bool                `json:"changed"`
	Reason  string              `json:"reason,omitempty"`
	From    *CatalogVersionView `json:"from,omitempty"`
	To      *CatalogVersionView `json:"to,omitempty"`
	Notes   []string            `json:"notes,omitempty"`
	Project *AppSummary         `json:"project,omitempty"`
}

// updatePlan is everything an update will do, computed from the project
// and the catalog without changing either.
type updatePlan struct {
	app     *shpyrdv1.App
	entry   *catalog.IndexEntry
	state   *ProjectCatalog
	preview CatalogUpdatePreview

	// known: the input names the project already has as config vars.
	existing map[string]bool
	volumes  []catalog.Volume
	// resources to make, with their types.
	resources []catalog.Resource
	kinds     []ext.ResourceType
	// bind lists the bindable resources of the entry the project is not
	// yet bound to (made now, or made earlier).
	bind []shpyrdv1.Binding

	processes map[string]shpyrdv1.Process
	build     *shpyrdv1.Build
	env       []corev1.EnvVar
}

// catalogUpdateError is a refusal with the HTTP status it takes.
type catalogUpdateError struct {
	status int
	err    error
}

func (e *catalogUpdateError) Error() string { return e.err.Error() }

func refuse(status int, format string, args ...any) *catalogUpdateError {
	return &catalogUpdateError{status: status, err: fmt.Errorf(format, args...)}
}

// planCatalogUpdate reads the project, the catalog and the cluster. A
// project the catalog does not offer an update is a plan with a reason
// and nothing to do (plan.entry is nil).
func (s *Server) planCatalogUpdate(c *gin.Context, app *shpyrdv1.App) (*updatePlan, *catalogUpdateError) {
	name := app.Annotations[shpyrdv1.AnnotationCatalogEntry]
	if name == "" {
		return nil, refuse(http.StatusNotFound, "this project was not made from the catalog")
	}
	ctx := c.Request.Context()
	st := s.catalogLookup(ctx).of(app)
	p := &updatePlan{app: app, state: st}
	p.preview = CatalogUpdatePreview{
		Entry: name, State: st.State, From: st.Running, To: st.Offered,
		Inputs: []CatalogUpdateInput{}, Volumes: []string{}, Resources: []string{},
		ProcessesAdded: []string{}, ProcessesChanged: []string{}, EnvChanged: []string{},
	}
	switch st.State {
	case CatalogUpdateAvailable:
	case CatalogUpToDate:
		p.preview.Reason = "the project already runs the version the catalog offers"
		return p, nil
	case CatalogUpdating:
		p.preview.Reason = "the project is already rolling out the version the catalog offers"
		return p, nil
	default:
		p.preview.Reason = "the catalog offers this project no update: it is not read, is not the one the project came from, no longer has the entry or a version of it, or the project's source is no longer the entry's repository"
		return p, nil
	}
	ix, err := s.appCatalog.Index(ctx)
	if err != nil || ix == nil {
		p.preview.State, p.preview.Reason = "", "the catalog could not be read"
		return p, nil
	}
	e := ix.Find(name)
	if e == nil {
		p.preview.State, p.preview.Reason = "", "the catalog has no such entry any more"
		return p, nil
	}
	p.entry = e
	p.preview.Repository = e.Catalog.Source.Git

	processes, build, env, perr := e.Spec()
	if perr != nil {
		return nil, refuse(http.StatusBadGateway, "entry %s: %v", e.Name, perr)
	}
	p.processes, p.build = processes, build
	p.env = env

	// Inputs: only the ones the project does not have.
	sec, _, rerr := configvars.Read(ctx, s.apps, app)
	if rerr != nil {
		return nil, refuse(http.StatusBadGateway, "the project's config vars could not be read: %v", rerr)
	}
	p.existing = map[string]bool{}
	for k := range sec.Data {
		p.existing[k] = true
	}
	for _, in := range e.Catalog.Inputs {
		if p.existing[in.Name] {
			continue
		}
		ui := CatalogUpdateInput{Name: in.Name, Label: in.Label, Secret: in.Secret || in.Generate > 0, Required: in.Required}
		switch {
		case in.Generate > 0:
			ui.Action = "generate"
		case in.Default != "":
			ui.Action, ui.Default = "default", in.Default
			if ui.Secret {
				ui.Default = ""
			}
		default:
			ui.Action = "ask"
		}
		p.preview.Inputs = append(p.preview.Inputs, ui)
	}

	// Volumes and resources the project lacks.
	for _, v := range e.Catalog.Volumes {
		var have shpyrdv1.Volume
		gerr := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: v.Name}, &have)
		switch {
		case gerr == nil:
		case apierrors.IsNotFound(gerr):
			p.volumes = append(p.volumes, v)
			p.preview.Volumes = append(p.preview.Volumes, v.Name)
		default:
			return nil, refuse(http.StatusBadGateway, "the project's volumes could not be read: %v", gerr)
		}
	}
	for _, r := range e.Catalog.Resources {
		t, ok := s.resourceType(r.Kind)
		if !ok {
			return nil, refuse(http.StatusBadRequest, "the new version needs a %s, and resources of that kind are not available on this cluster (enable the extension that provides them)", r.Kind)
		}
		u := &unstructured.Unstructured{}
		u.SetAPIVersion(t.Group + "/" + t.Version)
		u.SetKind(t.Kind)
		gerr := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: r.Name}, u)
		switch {
		case gerr == nil:
		case apierrors.IsNotFound(gerr):
			p.resources = append(p.resources, r)
			p.kinds = append(p.kinds, t)
			p.preview.Resources = append(p.preview.Resources, r.Kind+" "+r.Name)
		default:
			return nil, refuse(http.StatusBadGateway, "the project's resources could not be read: %v", gerr)
		}
		if t.Bindable && !bound(app, t.Kind, r.Name) {
			p.bind = append(p.bind, shpyrdv1.Binding{Kind: t.Kind, Name: r.Name})
		}
	}

	// Processes, env, build, overlay.
	for _, n := range sortedProcessNames(processes) {
		cur, ok := app.Spec.Processes[n]
		switch {
		case !ok:
			p.preview.ProcessesAdded = append(p.preview.ProcessesAdded, n)
		case !sameDefinition(cur, processes[n]):
			p.preview.ProcessesChanged = append(p.preview.ProcessesChanged, n)
		}
	}
	for _, v := range env {
		if !hasEnvValue(app.Spec.Env, v.Name, v.Value) {
			p.preview.EnvChanged = append(p.preview.EnvChanged, v.Name)
		}
	}
	p.preview.BuildChanged = build != nil && !reflect.DeepEqual(app.Spec.Build, build)
	cur := ""
	if app.Spec.Source != nil && app.Spec.Source.Overlay != nil {
		cur = app.Spec.Source.Overlay.SHA256
	}
	want := ""
	if e.Overlay != nil {
		want = e.Overlay.SHA256
	}
	p.preview.OverlayChanged = cur != want
	return p, nil
}

func bound(app *shpyrdv1.App, kind, name string) bool {
	for _, b := range app.Spec.Bindings {
		if b.Kind == kind && b.Name == name {
			return true
		}
	}
	return false
}

func hasEnvValue(env []corev1.EnvVar, name, value string) bool {
	for _, e := range env {
		if e.Name == name && e.Value == value && e.ValueFrom == nil {
			return true
		}
	}
	return false
}

func sortedProcessNames(m map[string]shpyrdv1.Process) []string {
	out := make([]string, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// sameDefinition compares the parts of a process the entry defines.
func sameDefinition(a, b shpyrdv1.Process) bool {
	return reflect.DeepEqual(a.Command, b.Command) && reflect.DeepEqual(a.Args, b.Args) &&
		reflect.DeepEqual(a.Port, b.Port) && reflect.DeepEqual(a.HealthCheck, b.HealthCheck) &&
		reflect.DeepEqual(a.Volumes, b.Volumes)
}

// can says whether the caller holds action on the project in the path.
func (s *Server) can(c *gin.Context, app *shpyrdv1.App, action authz.Action) bool {
	roles, err := s.rolesOf(c)
	return err == nil && roles.Can(action, projectGrantKey(app))
}

// catalogUpdatePreview is GET /api/projects/:slug/catalog/update: what
// applying the offered version would do. It changes nothing.
func (s *Server) catalogUpdatePreview(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	plan, perr := s.planCatalogUpdate(c, app)
	if perr != nil {
		abort(c, perr.status, perr.err)
		return
	}
	c.JSON(http.StatusOK, plan.preview)
}

// catalogUpdateApply is POST /api/projects/:slug/catalog/update: applies
// the version that was previewed. It checks everything before it changes
// anything, then does what an install does for what is new, in the
// install's order, and requests the deploy.
func (s *Server) catalogUpdateApply(c *gin.Context) {
	var req CatalogUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	// The route needs the permission to deploy. An update also changes the
	// project's config, and one that makes a volume or a resource needs
	// what making it by hand needs.
	if !s.can(c, app, authz.ProjectConfig) {
		roles, _ := s.rolesOf(c)
		abort(c, http.StatusForbidden, projectDenial(roles, authz.ProjectConfig, project.SlugOf(app), projectGrantKey(app)))
		return
	}
	plan, perr := s.planCatalogUpdate(c, app)
	if perr != nil {
		abort(c, perr.status, perr.err)
		return
	}
	if plan.entry == nil {
		if plan.state.State == CatalogUpToDate || plan.state.State == CatalogUpdating {
			c.JSON(http.StatusOK, CatalogUpdateResult{Reason: plan.preview.Reason, From: plan.state.Running, To: plan.state.Offered})
			return
		}
		abort(c, http.StatusConflict, errors.New(plan.preview.Reason))
		return
	}
	e := plan.entry
	if req.Digest != e.Digest {
		abort(c, http.StatusConflict, errors.New("the catalog has published another version since you previewed this one: preview the update again"))
		return
	}
	if (len(plan.volumes) > 0 || len(plan.resources) > 0) && !s.can(c, app, authz.ProjectResource) {
		roles, _ := s.rolesOf(c)
		abort(c, http.StatusForbidden, projectDenial(roles, authz.ProjectResource, project.SlugOf(app), projectGrantKey(app)))
		return
	}

	// The inputs: given, default, generated; one that is already a config
	// var is never set again, and one the entry does not ask is refused.
	asked := map[string]catalog.Input{}
	for _, in := range e.Catalog.Inputs {
		asked[in.Name] = in
	}
	for n := range req.Inputs {
		in, known := asked[n]
		switch {
		case !known:
			abort(c, http.StatusBadRequest, fmt.Errorf("the entry asks no input %q", n))
			return
		case plan.existing[n]:
			abort(c, http.StatusBadRequest, fmt.Errorf("%s is already set on the project and an update does not change it; change it with the project's config vars", in.Name))
			return
		}
	}
	secrets, plain := map[string]string{}, map[string]string{}
	var missing []string
	for _, ui := range plan.preview.Inputs {
		in := asked[ui.Name]
		value := strings.TrimSpace(req.Inputs[in.Name])
		switch {
		case in.Generate > 0:
			b := make([]byte, in.Generate)
			if _, err := rand.Read(b); err != nil {
				abort(c, http.StatusInternalServerError, err)
				return
			}
			value = hex.EncodeToString(b)
		case value == "":
			value = in.Default
		}
		if value == "" {
			if in.Required {
				missing = append(missing, fmt.Sprintf("%s (%s)", in.Label, in.Name))
			}
			continue
		}
		if in.Secret || in.Generate > 0 {
			secrets[in.Name] = value
		} else {
			plain[in.Name] = value
		}
	}
	if len(missing) > 0 {
		abort(c, http.StatusUnprocessableEntity, fmt.Errorf("the update needs %s", strings.Join(missing, ", ")))
		return
	}
	// The overlay, read and kept before anything changes.
	var overlay *shpyrdv1.BlobSource
	ctx := c.Request.Context()
	if e.Overlay != nil {
		if s.sources == nil {
			abort(c, http.StatusServiceUnavailable, errors.New("source store not configured: the entry's overlay cannot be kept"))
			return
		}
		b, err := s.appCatalog.Overlay(ctx, e)
		if err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
		info, err := s.sources.PutContext(ctx, bytes.NewReader(b))
		if err != nil {
			abort(c, http.StatusBadGateway, fmt.Errorf("the entry's overlay could not be kept: %w", err))
			return
		}
		overlay = &shpyrdv1.BlobSource{URL: info.URL, SHA256: info.SHA256}
	}

	slug := project.SlugOf(app)
	var notes []string
	fail := func(status int, step string, err error) {
		abort(c, status, fmt.Errorf("the update of %s stopped: %s: %v. Nothing was removed; apply the update again to finish it", slug, step, err))
	}
	// 1. Volumes and resources.
	for _, v := range plan.volumes {
		view, status, err := s.makeVolume(c, app.Namespace, slug, CreateVolumeRequest{Name: v.Name, Size: v.Size})
		if err != nil {
			fail(status, fmt.Sprintf("its volume %s could not be made", v.Name), err)
			return
		}
		if view.Note != "" {
			notes = append(notes, view.Note)
		}
	}
	for i, r := range plan.resources {
		spec := r.Spec
		if spec == nil {
			spec = map[string]interface{}{}
		}
		view, status, err := s.makeResource(c, app.Namespace, slug, plan.kinds[i], r.Name, spec, "resource.create")
		if err != nil {
			fail(status, fmt.Sprintf("its %s %s could not be made", r.Kind, r.Name), err)
			return
		}
		if view.Note != "" {
			notes = append(notes, view.Note)
		}
	}
	// 2. Config vars: only the new ones.
	var updates []ConfigVarsUpdate
	if len(secrets) > 0 {
		updates = append(updates, ConfigVarsUpdate{Set: secrets})
	}
	if len(plain) > 0 {
		updates = append(updates, ConfigVarsUpdate{Kind: "variable", ConfirmPlain: true, Set: plain})
	}
	if len(updates) > 0 {
		if _, status, err := s.writeConfigVars(ctx, app, updates...); err != nil {
			fail(status, "its config vars could not be set", err)
			return
		}
		s.audit(c, slug, "config.set", slug, "set "+strings.Join(sortedNames(secrets, plain), ", "))
	}
	// 3. The source, build settings and the version the project is set to,
	// together: the deploy. An image pinned by a rollback is released.
	updated, err := s.mutateApp(c, func(a *shpyrdv1.App) error {
		if a.Spec.Source == nil || a.Spec.Source.Git == nil {
			return errors.New("the project's source is no longer the entry's repository")
		}
		a.Spec.Source.Git.Revision = e.Commit
		a.Spec.Source.SubPath = e.Catalog.Source.SubPath
		a.Spec.Source.Overlay = overlay
		a.Spec.Image = ""
		if plan.build != nil {
			a.Spec.Build = plan.build
		}
		for _, n := range sortedProcessNames(plan.processes) {
			want := plan.processes[n]
			if a.Spec.Processes == nil {
				a.Spec.Processes = map[string]shpyrdv1.Process{}
			}
			cur, ok := a.Spec.Processes[n]
			if !ok {
				a.Spec.Processes[n] = want
				continue
			}
			cur.Command, cur.Args, cur.Port, cur.HealthCheck, cur.Volumes = want.Command, want.Args, want.Port, want.HealthCheck, want.Volumes
			a.Spec.Processes[n] = cur
		}
		for _, v := range plan.env {
			setEnvValue(&a.Spec.Env, v)
		}
		for _, b := range plan.bind {
			if !bound(a, b.Kind, b.Name) {
				a.Spec.Bindings = append(a.Spec.Bindings, b)
			}
		}
		if a.Annotations == nil {
			a.Annotations = map[string]string{}
		}
		a.Annotations[shpyrdv1.AnnotationCatalogDigest] = e.Digest
		a.Annotations[shpyrdv1.AnnotationCatalogCommit] = e.Commit
		a.Annotations[shpyrdv1.AnnotationCatalogRevision] = e.Revision()
		a.Annotations[shpyrdv1.AnnotationReleaseNote] = fmt.Sprintf("Update %s to %s (%s) from the catalog", e.Catalog.Name, e.Revision(), shortCommit(e.Commit))
		a.Annotations[configvars.AnnotationDeployRequested] = time.Now().UTC().Format(time.RFC3339Nano)
		// The source watcher reads the pinned commit again at once.
		delete(a.Annotations, shpyrdv1.AnnotationGitCommit)
		delete(a.Annotations, shpyrdv1.AnnotationGitCommitRef)
		if id, ok := ext.IdentityFrom(c); ok {
			a.Annotations[shpyrdv1.AnnotationDeployedBy] = firstNonEmpty(id.Email, id.Name, id.Subject)
			a.Annotations[shpyrdv1.AnnotationDeployedSubject] = s.subjectOf(c, id)
			a.Annotations[shpyrdv1.AnnotationDeployedClient] = clientOf(id)
		}
		return nil
	})
	if err != nil {
		return // mutateApp answered
	}
	s.audit(c, slug, "catalog.update", e.Name, fmt.Sprintf("%s to %s at %s (%s)", e.Catalog.Name, e.Revision(), shortCommit(e.Commit), e.Digest))
	sum := summarize(updated)
	c.JSON(http.StatusAccepted, CatalogUpdateResult{Changed: true, From: plan.state.Running, To: plan.state.Offered, Notes: notes, Project: &sum})
}

// setEnvValue sets the variable in env, keeping the order and any other
// variable.
func setEnvValue(env *[]corev1.EnvVar, v corev1.EnvVar) {
	for i := range *env {
		if (*env)[i].Name == v.Name {
			(*env)[i] = v
			return
		}
	}
	*env = append(*env, v)
}
