package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/authz"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	project_ "github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
)

// Authorization (RFC-0008): every protected route names the action it
// performs; the middleware resolves the caller's roles and refuses with 403
// and a plain explanation. The dashboard reads the same roles from /api/me
// to hide what the role cannot do.

const rolesKey = "shpyrd.roles"

// rolesOf resolves (and caches on the request) the caller's roles.
func (s *Server) rolesOf(c *gin.Context) (authz.Roles, error) {
	if v, ok := c.Get(rolesKey); ok {
		return v.(authz.Roles), nil
	}
	id, ok := ext.IdentityFrom(c)
	if !ok {
		// Authentication disabled: everything is allowed.
		id = ext.Identity{Subject: "admin-token", Provider: "token", Admin: true}
	}
	roles, err := s.rolesAt(c, id)
	if err != nil {
		return authz.Roles{}, err
	}
	c.Set(rolesKey, roles)
	return roles, nil
}

// rolesAt resolves an identity's roles for the request's door (RFC-0080).
// The console realm reads the operator's default workspace in bootstrap
// mode; a workspace host reads that workspace, enforced from birth, and
// platform admins own operator workspaces without a membership.
func (s *Server) rolesAt(c *gin.Context, id ext.Identity) (authz.Roles, error) {
	ctx := c.Request.Context()
	t, err := s.door(c)
	if err != nil || t.AtConsole() {
		return s.authz.RolesIn(ctx, "", id)
	}
	ws := t.Workspace
	roles, err := s.authz.RolesIn(ctx, ws.Slug, id)
	if err != nil {
		return roles, err
	}
	if ws.OwnedByOperator() && roles.Workspace == "" && !roles.Suspended {
		// The operator's workspaces follow the console's roles, bootstrap
		// included: on a fresh cluster the first person through the
		// operator's doors is its admin everywhere, until the first role
		// is written — the same rule the console applies to itself.
		console, err := s.authz.RolesIn(ctx, "", id)
		if err != nil {
			return roles, err
		}
		if console.Platform == shpyrdv1.RolePlatformAdmin {
			roles.Workspace = store.WorkspaceRoleOwner
			roles.Platform = shpyrdv1.RolePlatformAdmin
			roles.Enforced = console.Enforced
		}
	}
	return roles, nil
}

// require refuses the request unless the caller may perform action; project
// actions take the project from the :slug parameter. A malformed slug is a
// 404: no such project can exist. Roles on a project are keyed by its id
// (RFC-0076), so the slug is looked up first; a project that does not exist
// is checked by its slug, and the handler answers 404.
func (s *Server) require(action authz.Action) gin.HandlerFunc {
	return func(c *gin.Context) {
		project := c.Param("slug")
		if project != "" && (!project_.ValidSlug(project) || strings.HasPrefix(project, "app-")) {
			// Slugs never start with "app-": that is the namespace prefix,
			// so a client passing a namespace here is a bug worth a loud 404.
			abort(c, http.StatusNotFound, errors.New("project not found (paths take the project slug, not its namespace)"))
			return
		}
		roles, err := s.rolesOf(c)
		if err != nil {
			abort(c, http.StatusBadGateway, fmt.Errorf("resolve roles: %w", err))
			return
		}
		key := project
		if project != "" {
			if app, err := s.projectApp(c); err == nil {
				key = projectGrantKey(app)
			}
		}
		if !roles.Can(action, key) {
			abort(c, http.StatusForbidden, projectDenial(roles, action, project, key))
			return
		}
		c.Next()
	}
}

// denial explains a refusal in the user's terms.
func denial(roles authz.Roles, action authz.Action, project string) error {
	return projectDenial(roles, action, project, project)
}

// projectDenial is denial for a project named by its slug whose roles are
// held under its grant key (see projectGrantKey).
func projectDenial(roles authz.Roles, action authz.Action, project, grantKey string) error {
	verb := map[authz.Action]string{
		authz.ProjectView: "view", authz.ProjectDeploy: "deploy or roll back", authz.ProjectScale: "scale or resize",
		authz.ProjectConfig: "change config vars of", authz.ProjectExec: "run commands in", authz.ProjectResource: "manage resources of",
		authz.ProjectMembers: "manage members of", authz.ProjectDestroy: "destroy",
		authz.ClusterView: "view the cluster", authz.ClusterAdmin: "administer the cluster", authz.ClusterCreate: "create projects",
		authz.WorkspaceOwner: "name or demote owners",
	}[action]
	if strings.HasPrefix(string(action), "workspace.") {
		return fmt.Errorf("only the workspace's owners can %s", verb)
	}
	if project == "" || strings.HasPrefix(string(action), "cluster.") {
		return fmt.Errorf("your role cannot %s (needs a platform role)", verb)
	}
	if role := roles.ProjectRole(grantKey); role != "" {
		return fmt.Errorf("your role on project %s is %s: it cannot %s the project", project, role, verb)
	}
	return fmt.Errorf("you have no access to project %s", project)
}

// projectGrantKey returns the stable grant key for an app: the short base36
// ID when the app has one (post-RFC-0076), else the slug (legacy apps).
// This is what is stored in grants.project and api_tokens.project_roles
// after RekeyGrantsToIDs runs.
func projectGrantKey(app *shpyrdv1.App) string {
	if app.Spec.ID != "" {
		return ids.Short(app.Spec.ID)
	}
	return project_.SlugOf(app)
}

// canView filters lists to the projects the caller may see (by grant key).
func (s *Server) canView(c *gin.Context, grantKey string) bool {
	roles, err := s.rolesOf(c)
	return err == nil && roles.Can(authz.ProjectView, grantKey)
}

// canViewApp is canView using the stable grant key resolved from the App.
func (s *Server) canViewApp(c *gin.Context, app *shpyrdv1.App) bool {
	return s.canView(c, projectGrantKey(app))
}

// ---- teams ----------------------------------------------------------------

// TeamView is a team as returned by the API.
type TeamView struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Members      []string `json:"members"`
	Groups       []string `json:"groups"`
	PlatformRole string   `json:"platformRole,omitempty"`
	// Everyone marks the built-in team of every person who signed in.
	Everyone bool `json:"everyone,omitempty"`
}

func teamView(t store.Team) TeamView {
	v := TeamView{Name: t.Name, Description: t.Description, Members: t.Members, Groups: t.Groups, PlatformRole: t.PlatformRole, Everyone: t.Everyone}
	if v.Members == nil {
		v.Members = []string{}
	}
	if v.Groups == nil {
		v.Groups = []string{}
	}
	return v
}

// TeamRequest creates or replaces a team.
type TeamRequest struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Members      []string `json:"members"`
	Groups       []string `json:"groups"`
	PlatformRole string   `json:"platformRole,omitempty"`
}

var dnsName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// ctxWorkspace is the gin context key of the resolved workspace.
const ctxWorkspace = ext.WorkspaceContextKey

// ctxDoor is the gin context key of the resolved door.
const ctxDoor = "shpyrd.door"

// errConsoleNotWorkspace answers workspace routes reached at the console
// host (RFC-0080): the console is not a workspace.
var errConsoleNotWorkspace = errors.New("the console is not a workspace: open this at your workspace's address")

// door is which door the request came through (RFC-0080): the console at
// the console host, a workspace at its address or hosts, the console with
// the default workspace as tenant at internal hosts. Resolved once per
// request; a multi-workspace resolver may answer tenancy.ErrUnknownHost.
func (s *Server) door(c *gin.Context) (*tenancy.Tenant, error) {
	if v, ok := c.Get(ctxDoor); ok {
		return v.(*tenancy.Tenant), nil
	}
	if v, ok := c.Get(ctxWorkspace); ok {
		// The edge's subrequests resolved their workspace from the app host
		// nginx reported (edgeWorkspace): that is the door, not the
		// server's own name the subrequest arrived at.
		if ws, ok := v.(*store.Workspace); ok && ws != nil {
			t := &tenancy.Tenant{Realm: tenancy.RealmWorkspace, Workspace: ws}
			c.Set(ctxDoor, t)
			return t, nil
		}
	}
	t, err := s.tenancy.Resolve(c.Request.Context(), c.Request.Host)
	if errors.Is(err, tenancy.ErrUnknownHost) {
		// A project's custom domain (RFC-0034) is a host the resolver
		// cannot know: the app that claims it says whose it is, so sign-in
		// and the edge's callbacks work there too.
		if app, aerr := s.appByHost(c, c.Request.Host); aerr == nil {
			if w, werr := s.store.Workspace(c.Request.Context(), s.workspaceOfApp(c.Request.Context(), app)); werr == nil {
				t, err = &tenancy.Tenant{Realm: tenancy.RealmWorkspace, Workspace: w}, nil
			}
		}
	}
	if err != nil {
		return nil, err
	}
	c.Set(ctxDoor, t)
	if t.Workspace != nil {
		c.Set(ctxWorkspace, t.Workspace)
	}
	return t, nil
}

// tenant is the workspace the request is scoped to: the workspace at a
// workspace host, the default workspace at an internal host. At the
// console host there is none: errConsoleNotWorkspace.
func (s *Server) tenant(c *gin.Context) (*store.Workspace, error) {
	t, err := s.door(c)
	if err != nil {
		return nil, err
	}
	if t.Workspace == nil {
		return nil, errConsoleNotWorkspace
	}
	return t.Workspace, nil
}

// workspace is the slug the request is scoped to: "" at the console and
// for an unresolvable host, where every store call answers not found.
func (s *Server) workspace(c *gin.Context) string {
	ws, err := s.tenant(c)
	if err != nil {
		return ""
	}
	return ws.Slug
}

// workspaceID is the id of the request's workspace, "" at the console.
func (s *Server) workspaceID(c *gin.Context) string {
	ws, err := s.tenant(c)
	if err != nil {
		return ""
	}
	return ws.ID
}

// realm is the request's realm; the console's when the host is unknown,
// which only matters for error pages.
func (s *Server) realm(c *gin.Context) string {
	if t, err := s.door(c); err == nil {
		return t.Realm
	}
	return tenancy.RealmConsole
}

// requireTenant answers for hosts nobody claims and for suspended
// workspaces, so handlers behind it can count on s.door(c). The console
// host passes: its routes have no workspace.
func (s *Server) requireTenant() gin.HandlerFunc {
	return func(c *gin.Context) {
		t, err := s.door(c)
		asJSON := strings.HasPrefix(c.Request.URL.Path, "/api/") || wantsJSON(c)
		switch {
		case errors.Is(err, tenancy.ErrUnknownHost):
			if asJSON {
				abort(c, http.StatusNotFound, fmt.Errorf("no workspace answers at %s", tenancy.Host(c.Request.Host)))
			} else {
				s.edgePage(c, http.StatusNotFound, "Nothing here", "No workspace answers at this address.", nil)
				c.Abort()
			}
			return
		case err != nil:
			abort(c, http.StatusBadGateway, fmt.Errorf("resolve workspace: %w", err))
			return
		case t.Workspace == nil && workspaceOnlyPath(c.Request.URL.Path):
			// The console is not a workspace (RFC-0080).
			if asJSON {
				abort(c, http.StatusNotFound, errConsoleNotWorkspace)
			} else {
				s.edgePage(c, http.StatusNotFound, "Nothing here", "The console is not a workspace: open this at your workspace's address.", nil)
				c.Abort()
			}
			return
		case t.Workspace == nil:
			// The console's own routes.
		case t.Workspace.Status == store.WorkspaceSuspended && !t.Internal:
			if asJSON {
				abort(c, http.StatusForbidden, errors.New("this workspace is suspended"))
			} else {
				s.edgePage(c, http.StatusForbidden, "Workspace suspended", "This workspace has been switched off by the platform operator.", nil)
				c.Abort()
			}
			return
		}
		c.Next()
	}
}

// storeErr maps store errors to HTTP statuses.
func storeErr(c *gin.Context, err error, what string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		abort(c, http.StatusNotFound, fmt.Errorf("%s not found", what))
	case errors.Is(err, store.ErrConflict):
		abort(c, http.StatusConflict, fmt.Errorf("this %s already exists", what))
	case errors.Is(err, store.ErrBuiltIn):
		abort(c, http.StatusBadRequest, fmt.Errorf("the %s team is built in: every person who signs in belongs to it; grant it roles, but it cannot be edited or deleted", store.TeamEveryone))
	default:
		abort(c, http.StatusBadGateway, err)
	}
}

// membershipChanged refreshes the authz cache and the RBAC mirror.
func (s *Server) membershipChanged() {
	s.authz.Invalidate()
	if s.opts.MembershipChanged != nil {
		s.opts.MembershipChanged()
	}
}

func (s *Server) listTeams(c *gin.Context) {
	teams, err := s.store.ListTeams(c.Request.Context(), s.workspace(c))
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	out := make([]TeamView, 0, len(teams))
	for _, t := range teams {
		out = append(out, teamView(t))
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) putTeam(c *gin.Context) {
	var req TeamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	name := firstNonEmpty(c.Param("name"), req.Name)
	if !dnsName.MatchString(name) {
		abort(c, http.StatusBadRequest, errors.New("team names use lowercase letters, digits and dashes"))
		return
	}
	switch req.PlatformRole {
	case "", shpyrdv1.RolePlatformAdmin, shpyrdv1.RolePlatformViewer:
	default:
		abort(c, http.StatusBadRequest, errors.New("platformRole must be platform-admin, platform-viewer or empty"))
		return
	}
	members, err := normalizeEmails(req.Members)
	if err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	s.claimOwnershipInBootstrap(c) // the first team ends bootstrap: its author stays in charge
	team, created, err := s.store.PutTeam(c.Request.Context(), s.workspace(c), store.Team{Name: name, Description: req.Description, Members: members, Groups: compact(req.Groups), PlatformRole: req.PlatformRole})
	if err != nil {
		storeErr(c, err, "team")
		return
	}
	s.membershipChanged()
	s.audit(c, "", "team."+map[bool]string{true: "create", false: "update"}[created], name, "")
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, teamView(*team))
}

func (s *Server) deleteTeam(c *gin.Context) {
	name := c.Param("name")
	// Grants given to the team go with it.
	if err := s.store.DeleteTeam(c.Request.Context(), s.workspace(c), name); err != nil {
		storeErr(c, err, "team")
		return
	}
	s.membershipChanged()
	s.audit(c, "", "team.delete", name, "")
	c.Status(http.StatusNoContent)
}

// ---- project members -------------------------------------------------------

// MemberView is one grant on a project.
type MemberView struct {
	Name string `json:"name"`
	ID   string `json:"id,omitempty"`
	// Project is the stable grant key (short base36 project ID for
	// ID-named projects, slug for legacy ones).
	Project string `json:"project"`
	// ProjectSlug is the human-readable current slug; resolved at read
	// time from the projects table (RFC-0076).
	ProjectSlug string `json:"projectSlug,omitempty"`
	Role        string `json:"role"`
	User        string `json:"user,omitempty"`
	Team        string `json:"team,omitempty"`
}

// MemberRequest grants a role to a user or a team.
type MemberRequest struct {
	Role string `json:"role" binding:"required"`
	User string `json:"user,omitempty"`
	Team string `json:"team,omitempty"`
}

func memberView(g store.Grant) MemberView {
	return MemberView{Name: MemberName(g.Project, g.Role, g.User, g.Team), ID: g.ID, Project: g.Project, Role: g.Role, User: g.User, Team: g.Team}
}

// memberViewWithSlug is memberView with the project slug resolved from the
// projects table (RFC-0076): the UI and CLI show the slug, not the base36 ID.
func (s *Server) memberViewWithSlug(ctx context.Context, ws string, g store.Grant) MemberView {
	v := memberView(g)
	if pr, err := s.store.ProjectBySlug(ctx, ws, g.Project); err == nil {
		v.ProjectSlug = pr.Slug
	} else {
		// Try resolving by short id if the project key is already an id.
		v.ProjectSlug = g.Project // fallback: show the raw key
	}
	return v
}

// resolveGrantSlugs adds ProjectSlug to a slice of MemberViews by looking up
// the projects table once. Keys that are already slugs (legacy grants) are
// returned unchanged.
func (s *Server) resolveGrantSlugs(ctx context.Context, ws string, views []MemberView) {
	// Build id→slug map from the projects table.
	prs, err := s.store.ListProjects(ctx, ws, false)
	if err != nil {
		return
	}
	byKey := make(map[string]string, len(prs))
	for _, p := range prs {
		byKey[p.Short()] = p.Slug
		byKey[p.Slug] = p.Slug // legacy: slug maps to itself
	}
	for i := range views {
		if slug, ok := byKey[views[i].Project]; ok {
			views[i].ProjectSlug = slug
		} else {
			views[i].ProjectSlug = views[i].Project
		}
	}
}

// MemberName is the deterministic name of a grant, kept from the days
// grants were objects: the CLI and the dashboard remove grants by it.
func MemberName(project, role, user, team string) string {
	subject := "user-" + strings.NewReplacer("@", "-at-", ".", "-", "+", "-plus-", "_", "-").Replace(strings.ToLower(user))
	if team != "" {
		subject = "team-" + team
	}
	name := project + "-" + role + "-" + subject
	if len(name) > 63 {
		name = name[:63]
	}
	return strings.TrimRight(name, "-")
}

func (s *Server) listMembers(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	grants, err := s.store.ListProjectGrants(ctx, s.workspace(c), projectGrantKey(app))
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	out := make([]MemberView, 0, len(grants))
	for _, g := range grants {
		out = append(out, memberView(g))
	}
	s.resolveGrantSlugs(ctx, s.workspace(c), out)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	c.JSON(http.StatusOK, out)
}

// listAllMembers lists every grant of the workspace (`shpyrd members list`).
func (s *Server) listAllMembers(c *gin.Context) {
	grants, err := s.store.ListGrants(c.Request.Context(), s.workspace(c))
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	out := make([]MemberView, 0, len(grants))
	for _, g := range grants {
		out = append(out, memberView(g))
	}
	s.resolveGrantSlugs(c.Request.Context(), s.workspace(c), out)
	c.JSON(http.StatusOK, out)
}

func (s *Server) addMember(c *gin.Context) {
	var req MemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	project := projectGrantKey(app)
	switch req.Role {
	case shpyrdv1.RoleReader, shpyrdv1.RoleUser, shpyrdv1.RoleViewer, shpyrdv1.RoleDeveloper, shpyrdv1.RoleAdmin:
	default:
		abort(c, http.StatusBadRequest, errors.New("role must be reader, user, viewer, developer or admin"))
		return
	}
	if (req.User == "") == (req.Team == "") {
		abort(c, http.StatusBadRequest, errors.New("give exactly one of user (email) or team"))
		return
	}
	if req.User != "" {
		emails, err := normalizeEmails([]string{req.User})
		if err != nil {
			abort(c, http.StatusBadRequest, err)
			return
		}
		req.User = emails[0]
	}
	s.claimOwnershipInBootstrap(c) // the first grant ends bootstrap: its author stays in charge
	g, err := s.store.AddGrant(c.Request.Context(), s.workspace(c), store.Grant{Project: project, Role: req.Role, User: req.User, Team: req.Team})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			abort(c, http.StatusNotFound, errors.New("team not found"))
			return
		}
		storeErr(c, err, "grant")
		return
	}
	s.membershipChanged()
	s.audit(c, project, "member.add", firstNonEmpty(req.User, "team "+req.Team), req.Role)
	v := memberView(*g)
	s.resolveGrantSlugs(c.Request.Context(), s.workspace(c), []MemberView{v})
	c.JSON(http.StatusCreated, v)
}

// removeMember accepts the grant's id or its deterministic name.
func (s *Server) removeMember(c *gin.Context) {
	key := c.Param("name")
	ctx := c.Request.Context()
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	project := projectGrantKey(app)
	grants, err := s.store.ListProjectGrants(ctx, s.workspace(c), project)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	for _, g := range grants {
		if g.ID != key && MemberName(g.Project, g.Role, g.User, g.Team) != key {
			continue
		}
		if err := s.store.DeleteGrant(ctx, s.workspace(c), g.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
			abort(c, http.StatusBadGateway, err)
			return
		}
		s.membershipChanged()
		s.audit(c, project, "member.remove", firstNonEmpty(g.User, "team "+g.Team), g.Role)
		c.Status(http.StatusNoContent)
		return
	}
	abort(c, http.StatusNotFound, errors.New("member not found"))
}

func normalizeEmails(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, e := range in {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !strings.Contains(e, "@") {
			return nil, fmt.Errorf("%q is not an email address", e)
		}
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out, nil
}

func compact(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// workspaceOnlyPath says a path belongs to workspaces and has no meaning
// at the console host (RFC-0080): projects and everything under them, the
// workspace's own pages, sign-in for apps, MCP.
func workspaceOnlyPath(p string) bool {
	if p == "/api/workspace" || p == "/mcp" {
		return true
	}
	if p == "/api/workspace/import" || strings.HasPrefix(p, "/api/workspace/login-methods") {
		return false // the console recreates a workspace from its dump (RFC-0033); its own sign-in methods use the scoped route (RFC-0080)
	}
	for _, prefix := range []string{"/api/projects", "/api/teams", "/api/tokens", "/api/launcher", "/api/invitations", "/api/sources", "/api/workspace/", "/oauth/", "/.shpyrd/", "/.well-known/oauth-protected-resource"} {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}
