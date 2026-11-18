package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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
// deploy, pinned to the commit the catalog published; from then on it is
// a project like any other, and the catalog says when it publishes a
// different version of the entry.

// CatalogView is the catalog as the dashboard lists it.
type CatalogView struct {
	URL        string             `json:"url"`
	FetchedAt  time.Time          `json:"fetchedAt,omitempty"`
	Categories []string           `json:"categories"`
	Entries    []CatalogEntryView `json:"entries"`
}

// CatalogEntryView is one entry, as the dashboard and the CLI show it.
type CatalogEntryView struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	// Commit is the commit the catalog published for the entry; with the
	// digest, its version. Empty when the catalog published none.
	Commit      string   `json:"commit,omitempty"`
	DisplayName string   `json:"displayName"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags,omitempty"`
	Icon        string   `json:"icon,omitempty"`
	IconColor   string   `json:"iconColor,omitempty"`
	// IconURL is the image the entry ships for its card, served from this
	// server (the pages draw images from this origin only), and IconType
	// what it is: an SVG is drawn as a symbol in the entry's colour, as
	// the project's card will draw it.
	IconURL   string             `json:"iconUrl,omitempty"`
	IconType  string             `json:"iconType,omitempty"`
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
		Name: e.Name, Digest: e.Digest, Commit: e.Commit, DisplayName: e.Catalog.Name, Description: e.Catalog.Description,
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
	if typ := catalog.IconType(e.Icon); typ != "" {
		v.IconURL, v.IconType = catalogIconURL(e), typ
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

// catalogIconURL is where the entry's image is served from, versioned by
// the entry's digest, which covers the image.
func catalogIconURL(e *catalog.IndexEntry) string {
	v := strings.TrimPrefix(e.Digest, "sha256:")
	if len(v) > 12 {
		v = v[:12]
	}
	return "/api/catalog/entries/" + url.PathEscape(e.Name) + "/icon?v=" + v
}

// catalogEntryIcon is GET /api/catalog/entries/:name/icon: the image the
// entry ships, read from the catalog and answered from here, since the
// pages' policy lets them draw images from this origin only.
func (s *Server) catalogEntryIcon(c *gin.Context) {
	ix, ok := s.catalogIndexOf(c)
	if !ok {
		return
	}
	e := ix.Find(c.Param("name"))
	if e == nil {
		abort(c, http.StatusNotFound, fmt.Errorf("the catalog has no entry %q", c.Param("name")))
		return
	}
	raw, typ, err := s.appCatalog.Icon(c.Request.Context(), e)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	if len(raw) == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "private, max-age=86400")
	c.Header("X-Content-Type-Options", "nosniff")
	// An SVG opened as a document must not run scripts on this origin.
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	c.Data(http.StatusOK, typ, raw)
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
	// The entry's own image, read now for the same reason. One that
	// cannot be read is no reason not to install: the card keeps the
	// entry's symbol, and the answer says so.
	var notes []string
	icon, iconTyp, err := s.appCatalog.Icon(ctx, e)
	if err != nil {
		notes = append(notes, fmt.Sprintf("The entry's image was not copied to the project (%v); its card shows the symbol instead.", err))
		icon = nil
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
	if len(icon) > 0 {
		if err := s.store.SetProjectIcon(ctx, app.Spec.ID, icon, iconTyp); err != nil {
			notes = append(notes, fmt.Sprintf("The entry's image was not copied to the project (%v); its card shows the symbol instead.", err))
			icon = nil
		}
	}
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
	// An entry published with a commit installs pinned to it; an explicit
	// --revision installs as it always did.
	pinned := req.Revision == "" && e.Commit != ""
	revision := firstNonEmpty(req.Revision, e.Commit, e.Revision())
	app, err = s.updateAppIn(ctx, app, func(a *shpyrdv1.App) error {
		a.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: git, Revision: revision}, SubPath: e.Catalog.Source.SubPath, Overlay: overlay}
		if pinned {
			if a.Annotations == nil {
				a.Annotations = map[string]string{}
			}
			a.Annotations[shpyrdv1.AnnotationCatalogCommit] = e.Commit
			a.Annotations[shpyrdv1.AnnotationCatalogRevision] = e.Revision()
		}
		a.Spec.Bindings = bindings
		if len(icon) > 0 {
			project.SetIconFile(a, iconVersion(icon), iconKinds[iconTyp])
		}
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

// The states of a project from the catalog.
const (
	CatalogUpToDate        = "up-to-date"
	CatalogUpdateAvailable = "update-available"
	CatalogUpdating        = "updating"
)

// CatalogVersionView is one version of an entry: what a person reads as
// "v1.4.0 (3f2a9c1b8d7e)".
type CatalogVersionView struct {
	// Revision is the name the entry gave the commit (a tag or a branch).
	Revision    string `json:"revision,omitempty"`
	Commit      string `json:"commit"`
	ShortCommit string `json:"shortCommit"`
	Digest      string `json:"digest"`
}

// ProjectCatalog is what a project from the catalog says of it: the entry,
// the version its current release runs (absent when that is not known: a
// project installed before versions, or deployed from other code), and,
// when the catalog is read and still offers the project's entry, whether
// it is up to date, has an update available, or is rolling the offered
// version out.
type ProjectCatalog struct {
	Entry   string              `json:"entry"`
	Running *CatalogVersionView `json:"running,omitempty"`
	// State is empty when the catalog offers the project nothing: it is not
	// read, or is another one, the entry is gone or has no commit, the
	// project's source is no longer the entry's repository, or an image
	// deployed by hand is pinned on it.
	State   string              `json:"state,omitempty"`
	Offered *CatalogVersionView `json:"offered,omitempty"`
}

func versionView(v *shpyrdv1.CatalogVersion) *CatalogVersionView {
	if v == nil {
		return nil
	}
	return &CatalogVersionView{Revision: v.Revision, Commit: v.Commit, ShortCommit: shortCommit(v.Commit), Digest: v.Digest}
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

// How long a project read waits for the catalog when it has to be read
// again; the fetcher serves from memory the rest of the time.
const catalogReadWait = 2 * time.Second

// catalogLookup reads the catalog once for the projects of one request,
// and only if one of them comes from it.
type catalogLookup struct {
	s    *Server
	ctx  context.Context
	done bool
	ix   *catalog.Index
	url  string
}

func (s *Server) catalogLookup(ctx context.Context) *catalogLookup {
	return &catalogLookup{s: s, ctx: ctx}
}

func (l *catalogLookup) index() (*catalog.Index, string) {
	if l.done {
		return l.ix, l.url
	}
	l.done = true
	if l.s.appCatalog == nil {
		return nil, ""
	}
	ctx, cancel := context.WithTimeout(l.ctx, catalogReadWait)
	defer cancel()
	ix, err := l.s.appCatalog.Index(ctx)
	if err != nil || ix == nil {
		return nil, ""
	}
	l.ix, l.url = ix, l.s.appCatalog.Status().URL
	return l.ix, l.url
}

// of is the catalog state of a project, or nil when it is not from the
// catalog. It never fails: whatever cannot be read leaves State empty.
func (l *catalogLookup) of(a *shpyrdv1.App) *ProjectCatalog {
	name := a.Annotations[shpyrdv1.AnnotationCatalogEntry]
	if name == "" {
		return nil
	}
	pc := &ProjectCatalog{Entry: name}
	cur := a.CurrentRelease()
	if cur != nil && cur.Catalog != nil && cur.Catalog.Entry == name {
		pc.Running = versionView(cur.Catalog)
	}
	ix, url := l.index()
	if ix == nil || url != a.Annotations[shpyrdv1.AnnotationCatalogSource] {
		return pc
	}
	e := ix.Find(name)
	if e == nil || e.Commit == "" {
		return pc
	}
	if src := a.Spec.Source; src == nil || src.Git == nil || src.Git.URL != e.Catalog.Source.Git {
		return pc
	}
	// An image deployed by hand is no longer the entry's code. An image
	// pinned by a rollback is: the project is held on an older version.
	pinned := a.Spec.Image != ""
	if pinned && !heldByRollback(a) {
		return pc
	}
	offered := &shpyrdv1.CatalogVersion{Entry: name, Digest: e.Digest, Commit: e.Commit, Revision: e.Revision()}
	here := !pinned && sourceIsVersion(a, e)
	switch {
	case pc.Running != nil && pc.Running.Commit == e.Commit && pc.Running.Digest == e.Digest && (here || (pinned && cur != nil && cur.Image == a.Spec.Image)):
		pc.State = CatalogUpToDate
	case here && a.Annotations[shpyrdv1.AnnotationCatalogCommit] == e.Commit && a.Annotations[shpyrdv1.AnnotationCatalogDigest] == e.Digest:
		// Set to the offered version, with its code in the source and no
		// pinned image: it is rolling out, or its build failed. No second
		// offer.
		pc.State, pc.Offered = CatalogUpdating, versionView(offered)
	default:
		pc.State, pc.Offered = CatalogUpdateAvailable, versionView(offered)
	}
	return pc
}

// sourceIsVersion says the project's source is the entry's code at the
// version's commit, with the entry's sub-path and overlay.
func sourceIsVersion(a *shpyrdv1.App, e *catalog.IndexEntry) bool {
	src := a.Spec.Source
	if src == nil || src.Git == nil || src.Git.URL != e.Catalog.Source.Git || src.Git.Revision != e.Commit || src.SubPath != e.Catalog.Source.SubPath {
		return false
	}
	have, want := "", ""
	if src.Overlay != nil {
		have = src.Overlay.SHA256
	}
	if e.Overlay != nil {
		want = e.Overlay.SHA256
	}
	return have == want
}

// heldByRollback says the image pinned on the project was pinned by a
// rollback, not deployed by hand. Both ways of rolling back (the API, and
// the CLI writing the App with a kubeconfig) leave the same marks: the
// request, until the controller has recorded it (the rollback annotation),
// and then, on the release the controller records for it, the number of the
// release it returned to (Release.RollbackTo), which only the controller
// writes. The first release of the run that runs the pinned image carries
// it; releases after it by a change of config vars or sizes run the same
// image. A deploy of an image leaves neither mark, whatever note it gives
// and whichever images were deployed before.
func heldByRollback(a *shpyrdv1.App) bool {
	if a.Annotations[shpyrdv1.AnnotationRollbackTo] != "" {
		return true
	}
	rels := a.Status.Releases
	i := len(rels)
	for i > 0 && rels[i-1].Image == a.Spec.Image {
		i--
	}
	return i < len(rels) && rels[i].RollbackTo > 0
}
