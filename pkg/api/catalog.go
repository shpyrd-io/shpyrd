package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/catalog"
	"github.com/shpyrd-io/shpyrd/pkg/configvars"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/project"
)

// The catalog (pkg/catalog): the applications a project can be made from.
// Whoever may make a project may browse it and install from it. An
// install makes the project the entry describes, step by step, as a
// person would by hand: the project, its volumes and resources, the
// config vars the person gave, then the source, which is the first
// deploy; from then on it is a project like any other, following the
// upstream branch.

// CatalogView is the catalog as the dashboard lists it.
type CatalogView struct {
	URL        string             `json:"url"`
	FetchedAt  time.Time          `json:"fetchedAt,omitempty"`
	Categories []string           `json:"categories"`
	Entries    []CatalogEntryView `json:"entries"`
}

// CatalogEntryView is one entry, as the dashboard and the CLI show it.
type CatalogEntryView struct {
	Name        string   `json:"name"`
	Digest      string   `json:"digest"`
	DisplayName string   `json:"displayName"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags,omitempty"`
	Icon        string   `json:"icon,omitempty"`
	IconColor   string   `json:"iconColor,omitempty"`
	// IconURL is the image the entry ships for its card, at the catalog.
	IconURL   string             `json:"iconUrl,omitempty"`
	Publisher string             `json:"publisher,omitempty"`
	License   string             `json:"license,omitempty"`
	Homepage  string             `json:"homepage,omitempty"`
	Source    catalog.Source     `json:"source"`
	Inputs    []catalog.Input    `json:"inputs"`
	Volumes   []catalog.Volume   `json:"volumes"`
	Resources []catalog.Resource `json:"resources"`
	Processes []string           `json:"processes"`
	// Build is how the code is built: buildpacks or dockerfile; Overlay
	// the files the entry lays over the upstream code, if any.
	Build   string   `json:"build"`
	Overlay []string `json:"overlay,omitempty"`
	// Readme is the entry's readme, Markdown; only with one entry.
	Readme string `json:"readme,omitempty"`
}

// CatalogInstallRequest is what an install asks: the project's slug and
// name (the entry's when left out), the inputs' values, and a revision to
// pin instead of the one the entry follows.
type CatalogInstallRequest struct {
	Slug     string            `json:"slug,omitempty"`
	Name     string            `json:"name,omitempty"`
	Inputs   map[string]string `json:"inputs,omitempty"`
	Revision string            `json:"revision,omitempty"`
	Access   string            `json:"access,omitempty"`
	Exposure string            `json:"exposure,omitempty"`
}

// CatalogInstallResponse is the project made, and what the steps said.
type CatalogInstallResponse struct {
	Project AppSummary `json:"project"`
	Notes   []string   `json:"notes,omitempty"`
}

func (s *Server) catalogEntryView(e *catalog.IndexEntry) CatalogEntryView {
	v := CatalogEntryView{
		Name: e.Name, Digest: e.Digest, DisplayName: e.Catalog.Name, Description: e.Catalog.Description,
		Category: e.Catalog.Category, Tags: e.Catalog.Tags, Icon: e.Catalog.Icon, IconColor: e.Catalog.Color,
		Publisher: e.Catalog.Publisher, License: e.Catalog.License, Homepage: e.Catalog.Homepage,
		Source: catalog.Source{Git: e.Catalog.Source.Git, Revision: e.Revision(), SubPath: e.Catalog.Source.SubPath},
		Inputs: e.Catalog.Inputs, Volumes: e.Catalog.Volumes, Resources: e.Catalog.Resources,
		Build: shpyrdv1.StrategyBuildpacks,
	}
	if v.Inputs == nil {
		v.Inputs = []catalog.Input{}
	}
	if v.Volumes == nil {
		v.Volumes = []catalog.Volume{}
	}
	if v.Resources == nil {
		v.Resources = []catalog.Resource{}
	}
	if e.Icon != "" {
		if u, err := s.appCatalog.Resolve(e.Icon); err == nil {
			v.IconURL = u
		}
	}
	v.Processes = []string{}
	for name := range e.Processes {
		v.Processes = append(v.Processes, name)
	}
	sort.Strings(v.Processes)
	if _, build, _, err := e.Spec(); err == nil && build != nil && build.Strategy == shpyrdv1.StrategyDockerfile {
		v.Build = shpyrdv1.StrategyDockerfile
	}
	if e.Overlay != nil {
		v.Overlay = e.Overlay.Files
	}
	return v
}

// catalogIndexOf is the catalog, or the answer when there is none (404)
// or it cannot be read (502).
func (s *Server) catalogIndexOf(c *gin.Context) (*catalog.Index, bool) {
	if s.appCatalog == nil {
		abort(c, http.StatusNotFound, errors.New("this server reads no catalog"))
		return nil, false
	}
	ix, err := s.appCatalog.Index(c.Request.Context())
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return nil, false
	}
	return ix, true
}

// catalogIndex lists the catalog.
func (s *Server) catalogIndex(c *gin.Context) {
	ix, ok := s.catalogIndexOf(c)
	if !ok {
		return
	}
	st := s.appCatalog.Status()
	view := CatalogView{URL: st.URL, FetchedAt: st.FetchedAt, Categories: ix.Categories(), Entries: []CatalogEntryView{}}
	if view.Categories == nil {
		view.Categories = []string{}
	}
	for i := range ix.Entries {
		view.Entries = append(view.Entries, s.catalogEntryView(&ix.Entries[i]))
	}
	c.JSON(http.StatusOK, view)
}

// catalogEntry shows one entry, readme included.
func (s *Server) catalogEntry(c *gin.Context) {
	ix, ok := s.catalogIndexOf(c)
	if !ok {
		return
	}
	e := ix.Find(c.Param("name"))
	if e == nil {
		abort(c, http.StatusNotFound, fmt.Errorf("the catalog has no entry %q", c.Param("name")))
		return
	}
	view := s.catalogEntryView(e)
	readme, err := s.appCatalog.Readme(c.Request.Context(), e)
	if err != nil {
		s.log.Warn("catalog: the readme could not be read", "entry", e.Name, "err", err.Error())
	}
	view.Readme = readme
	c.JSON(http.StatusOK, view)
}

// catalogInstall makes a project from an entry.
func (s *Server) catalogInstall(c *gin.Context) {
	var req CatalogInstallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	ix, ok := s.catalogIndexOf(c)
	if !ok {
		return
	}
	e := ix.Find(c.Param("name"))
	if e == nil {
		abort(c, http.StatusNotFound, fmt.Errorf("the catalog has no entry %q", c.Param("name")))
		return
	}
	ctx := c.Request.Context()

	// The inputs: what the person gave, the defaults, and the generated
	// ones; a required one missing is refused before anything is made.
	secrets, plain := map[string]string{}, map[string]string{}
	known := map[string]bool{}
	var missing []string
	for _, in := range e.Catalog.Inputs {
		known[in.Name] = true
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
		abort(c, http.StatusUnprocessableEntity, fmt.Errorf("the install needs %s", strings.Join(missing, ", ")))
		return
	}
	for name := range req.Inputs {
		if !known[name] {
			abort(c, http.StatusBadRequest, fmt.Errorf("the entry asks no input %q", name))
			return
		}
	}
	if req.Revision != "" && strings.ContainsAny(req.Revision, " \t\n") {
		abort(c, http.StatusBadRequest, fmt.Errorf("revision %q is not a branch, tag or commit", req.Revision))
		return
	}
	processes, build, env, err := e.Spec()
	if err != nil {
		abort(c, http.StatusBadGateway, fmt.Errorf("entry %s: %w", e.Name, err))
		return
	}
	// The resources' kinds, before anything is made: a kind this cluster
	// does not have refuses the whole install.
	kinds := make([]ext.ResourceType, len(e.Catalog.Resources))
	for i, r := range e.Catalog.Resources {
		t, ok := s.resourceType(r.Kind)
		if !ok {
			abort(c, http.StatusBadRequest, fmt.Errorf("the entry needs a %s, and resources of that kind are not available on this cluster (enable the extension that provides them)", r.Kind))
			return
		}
		kinds[i] = t
	}
	// The overlay, read and kept before the project exists, so a catalog
	// that cannot be reached leaves nothing behind.
	var overlay *shpyrdv1.BlobSource
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
	git := e.Catalog.Source.Git
	linkedBy, ok := s.githubAccess(c, git)
	if !ok {
		return
	}

	// 1. The project, with what the entry says of it; no source yet, so
	// nothing builds before the config vars are in place.
	slug := firstNonEmpty(strings.TrimSpace(req.Slug), e.Name)
	name := firstNonEmpty(strings.TrimSpace(req.Name), e.Catalog.Name)
	st := s.appCatalog.Status()
	app, ok := s.createProject(c, CreateAppRequest{Name: name, Slug: slug, Exposure: req.Exposure, Access: req.Access, Processes: processes}, func(a *shpyrdv1.App) error {
		project.SetDescription(a, e.Catalog.Description)
		if err := project.SetIcon(a, e.Catalog.Icon); err != nil {
			return err
		}
		if err := project.SetIconColor(a, e.Catalog.Color); err != nil {
			return err
		}
		if a.Annotations == nil {
			a.Annotations = map[string]string{}
		}
		a.Annotations[shpyrdv1.AnnotationCatalogEntry] = e.Name
		a.Annotations[shpyrdv1.AnnotationCatalogDigest] = e.Digest
		a.Annotations[shpyrdv1.AnnotationCatalogSource] = st.URL
		a.Spec.Build = build
		a.Spec.Env = env
		return nil
	})
	if !ok {
		return
	}
	// From here on the project exists: a step that fails says so, and
	// leaves the project for the person to finish or delete.
	fail := func(status int, step string, err error) {
		abort(c, status, fmt.Errorf("the project %s was created, but %s: %v. Finish it by hand or delete it, then install again", slug, step, err))
	}
	var notes []string
	// 2. Its volumes and resources.
	for _, v := range e.Catalog.Volumes {
		view, status, err := s.makeVolume(c, app.Namespace, slug, CreateVolumeRequest{Name: v.Name, Size: v.Size})
		if err != nil {
			fail(status, fmt.Sprintf("its volume %s could not be made", v.Name), err)
			return
		}
		if view.Note != "" {
			notes = append(notes, view.Note)
		}
	}
	var bindings []shpyrdv1.Binding
	for i, r := range e.Catalog.Resources {
		spec := r.Spec
		if spec == nil {
			spec = map[string]interface{}{}
		}
		view, status, err := s.makeResource(c, app.Namespace, slug, kinds[i], r.Name, spec, "resource.create")
		if err != nil {
			fail(status, fmt.Sprintf("its %s %s could not be made", r.Kind, r.Name), err)
			return
		}
		if view.Note != "" {
			notes = append(notes, view.Note)
		}
		if kinds[i].Bindable {
			bindings = append(bindings, shpyrdv1.Binding{Kind: kinds[i].Kind, Name: r.Name})
		}
	}
	// 3. The config vars: secrets as secrets, the rest as plain
	// variables, pending until the first deploy applies them.
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
	// 4. The source: the first deploy, attributed to whoever installs.
	revision := firstNonEmpty(req.Revision, e.Revision())
	app, err = s.updateAppIn(ctx, app, func(a *shpyrdv1.App) error {
		a.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: git, Revision: revision}, SubPath: e.Catalog.Source.SubPath, Overlay: overlay}
		a.Spec.Bindings = bindings
		markGitHubLinkedBy(a, linkedBy)
		if a.Annotations == nil {
			a.Annotations = map[string]string{}
		}
		a.Annotations[shpyrdv1.AnnotationReleaseNote] = fmt.Sprintf("Install %s from the catalog", e.Catalog.Name)
		a.Annotations[configvars.AnnotationDeployRequested] = time.Now().UTC().Format(time.RFC3339Nano)
		if id, ok := ext.IdentityFrom(c); ok {
			a.Annotations[shpyrdv1.AnnotationDeployedBy] = firstNonEmpty(id.Email, id.Name, id.Subject)
			a.Annotations[shpyrdv1.AnnotationDeployedSubject] = s.subjectOf(c, id)
			a.Annotations[shpyrdv1.AnnotationDeployedClient] = clientOf(id)
		}
		return nil
	})
	if err != nil {
		fail(http.StatusBadGateway, "its source could not be set", err)
		return
	}
	s.audit(c, slug, "catalog.install", e.Name, fmt.Sprintf("%s at %s (%s)", e.Catalog.Name, revision, e.Digest))
	c.JSON(http.StatusCreated, CatalogInstallResponse{Project: summarize(app), Notes: notes})
}

// updateAppIn changes an App the handler already holds, reading it again
// on a conflict.
func (s *Server) updateAppIn(ctx context.Context, app *shpyrdv1.App, mutate func(*shpyrdv1.App) error) (*shpyrdv1.App, error) {
	key := types.NamespacedName{Namespace: app.Namespace, Name: app.Name}
	for attempt := 0; attempt < 5; attempt++ {
		cur := &shpyrdv1.App{}
		if err := s.apps.Get(ctx, key, cur); err != nil {
			return nil, err
		}
		if err := mutate(cur); err != nil {
			return nil, err
		}
		err := s.apps.Update(ctx, cur)
		if err == nil {
			return cur, nil
		}
		if !apierrors.IsConflict(err) {
			return nil, err
		}
	}
	return nil, errors.New("too many conflicts")
}

func sortedNames(maps ...map[string]string) []string {
	var names []string
	for _, m := range maps {
		for k := range m {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	return names
}
