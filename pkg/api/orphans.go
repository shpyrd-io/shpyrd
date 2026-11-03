package api

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/registry"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The orphan sweep (#274): what a cascade or a destroy left behind, as a
// report. A project namespace whose workspace is gone or whose project
// row is deleted, an image repository no App claims, a front door of a
// workspace that is gone. Report only: the operator removes what the
// report names, or the cascade does on its next pass.

// OrphanReport is GET /api/cluster/orphans.
type OrphanReport struct {
	CheckedAt    time.Time         `json:"checkedAt"`
	Namespaces   []OrphanNamespace `json:"namespaces"`
	Repositories []string          `json:"repositories"`
	FrontDoors   []string          `json:"frontDoors"`
	// RegistryError says the registry could not be read; Repositories is
	// then empty, not clean.
	RegistryError string `json:"registryError,omitempty"`
}

// OrphanNamespace is a project namespace with nothing live behind it.
type OrphanNamespace struct {
	Name      string `json:"name"`
	Workspace string `json:"workspace"`
	Project   string `json:"project"`
	Why       string `json:"why"`
}

// Clean says the report found nothing.
func (r *OrphanReport) Clean() bool {
	return len(r.Namespaces) == 0 && len(r.Repositories) == 0 && len(r.FrontDoors) == 0 && r.RegistryError == ""
}

func shortID(id string) string {
	if _, err := uuid.Parse(id); err != nil {
		return ""
	}
	return ids.Short(id)
}

// orphans is GET /api/cluster/orphans (console, cluster admin).
func (s *Server) orphans(c *gin.Context) {
	rep, err := s.orphanReport(c.Request.Context())
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, rep)
}

func (s *Server) orphanReport(ctx context.Context) (*OrphanReport, error) {
	rep := &OrphanReport{CheckedAt: time.Now().UTC(), Namespaces: []OrphanNamespace{}, Repositories: []string{}, FrontDoors: []string{}}
	live, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	bySlug := map[string]*store.Workspace{}
	byShort := map[string]*store.Workspace{}
	for i := range live {
		w := &live[i]
		if w.Gone() {
			continue
		}
		bySlug[w.Slug] = w
		if short := shortID(w.ID); short != "" {
			byShort[short] = w
		}
	}
	// Project namespaces.
	var nss corev1.NamespaceList
	if err := s.apps.List(ctx, &nss, client.HasLabels{shpyrdv1.LabelProject}); err != nil {
		return nil, err
	}
	deletedProjects := map[string]map[string]bool{} // workspace slug -> project slug
	for i := range nss.Items {
		ns := &nss.Items[i]
		if !ns.DeletionTimestamp.IsZero() {
			continue
		}
		project := ns.Labels[shpyrdv1.LabelProject]
		wsSlug, short := ns.Labels[shpyrdv1.LabelWorkspace], ns.Labels[shpyrdv1.LabelWorkspaceID]
		w := bySlug[wsSlug]
		if w == nil && short != "" {
			w = byShort[short]
		}
		if w == nil {
			rep.Namespaces = append(rep.Namespaces, OrphanNamespace{Name: ns.Name, Workspace: firstNonEmpty(wsSlug, short), Project: project, Why: "its workspace is gone"})
			continue
		}
		if _, seen := deletedProjects[w.Slug]; !seen {
			deletedProjects[w.Slug] = map[string]bool{}
			if rows, err := s.store.ListProjects(ctx, w.Slug, true); err == nil {
				for _, p := range rows {
					if p.DeletedAt != nil && p.Namespace == ns.Name {
						deletedProjects[w.Slug][p.Namespace] = true
					}
				}
			}
		}
		if deletedProjects[w.Slug][ns.Name] {
			rep.Namespaces = append(rep.Namespaces, OrphanNamespace{Name: ns.Name, Workspace: w.Slug, Project: project, Why: "its project row is deleted"})
		}
	}
	// Front doors.
	var doors networkingv1.IngressList
	if err := s.apps.List(ctx, &doors, client.InNamespace(s.deps().SystemNamespace), client.MatchingLabels{"shpyrd.io/workspace-front-door": "true"}); err == nil {
		for i := range doors.Items {
			if slug := doors.Items[i].Labels[shpyrdv1.LabelWorkspace]; slug != "" && bySlug[slug] == nil {
				rep.FrontDoors = append(rep.FrontDoors, doors.Items[i].Name)
			}
		}
	}
	// Image repositories: apps/<workspace id>/<project>, claimed by an App
	// of that project in that workspace.
	if host := s.vars("SHPYRD_REGISTRY_HOST"); host != "" && s.vars("SHPYRD_REGISTRY_IP") != "" && s.kube != nil && s.kube.Kube != nil {
		claimed := map[string]bool{}
		var apps shpyrdv1.AppList
		if err := s.apps.List(ctx, &apps); err == nil {
			for i := range apps.Items {
				a := &apps.Items[i]
				short := a.Labels[shpyrdv1.LabelWorkspaceID]
				if short == "" {
					if w := bySlug[a.Labels[shpyrdv1.LabelWorkspace]]; w != nil {
						short = shortID(w.ID)
					}
				}
				if short != "" {
					claimed["apps/"+short+"/"+firstNonEmpty(a.Labels[shpyrdv1.LabelProject], a.Name)] = true
				}
			}
		}
		h, _, _ := registry.SplitReference(host + "/x")
		cl := s.registryClientFor(ctx, h)
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		repos, err := cl.Catalog(cctx, 100000)
		if err != nil {
			rep.RegistryError = err.Error()
		}
		for _, repo := range repos {
			if !strings.HasPrefix(repo, "apps/") || claimed[repo] {
				continue
			}
			tags, err := cl.Tags(cctx, repo)
			if err != nil {
				rep.RegistryError = err.Error()
				break
			}
			if len(tags) > 0 {
				rep.Repositories = append(rep.Repositories, repo)
			}
		}
	}
	sort.Slice(rep.Namespaces, func(i, j int) bool { return rep.Namespaces[i].Name < rep.Namespaces[j].Name })
	sort.Strings(rep.Repositories)
	sort.Strings(rep.FrontDoors)
	return rep, nil
}
