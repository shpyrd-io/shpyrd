package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/authz"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The workspace (RFC-0033): the tenant every project belongs to. The
// open-source platform has exactly one, implicit; the API exposes its name
// and the people it has seen sign in, so a "Workspace" page exists before
// the multi-workspace cloud does.

// WorkspaceView is GET /api/workspace.
type WorkspaceView struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	Implicit bool   `json:"implicit"`
	// Domain is where the workspace's apps live, one label under it.
	Domain string `json:"domain,omitempty"`
	// Address is the host of an explicit workspace's dashboard (RFC-0033
	// phase 6); empty for the implicit workspace, which answers at the
	// platform's dashboard URL.
	Address string `json:"address,omitempty"`
	// URL is where this workspace's dashboard answers.
	URL    string `json:"url"`
	Status string `json:"status"`
	// Limits is the workspace's plan (nil: no ceiling) and Usage what it
	// uses today, in the plan's terms.
	Limits *store.Limits `json:"limits,omitempty"`
	Usage  *Usage        `json:"usage,omitempty"`
	// JoinPolicy says who becomes a person on first sign-in: open,
	// company (through a claimed domain's method) or listed (already named
	// in a team or a grant, holding a role, or invited).
	JoinPolicy string `json:"joinPolicy"`
	// OwnMethodsOnly says the login page offers only the methods this
	// workspace configured (its company SSO), not the platform's.
	OwnMethodsOnly bool `json:"ownMethodsOnly"`
	// Branding is the workspace's look (logo URL, colour).
	Branding *BrandingView `json:"branding,omitempty"`
	// MCPName is the name assistants show for the workspace's MCP server;
	// MCPURL where it answers (RFC-0032).
	MCPName string `json:"mcpName"`
	MCPURL  string `json:"mcpUrl"`
	// Owners are the emails of the workspace's owners (RFC-0033).
	Owners    []string  `json:"owners"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PersonView is one person of the workspace: someone who has signed in,
// or holds a workspace role and has not yet (then the seen times are
// absent).
type PersonView struct {
	Email    string   `json:"email"`
	Name     string   `json:"name,omitempty"`
	Provider string   `json:"provider,omitempty"`
	Groups   []string `json:"groups"`
	Realm    string   `json:"realm,omitempty"`
	Status   string   `json:"status"` // active, suspended
	// Role is the person's workspace role: owner, admin, member or ""
	// (RFC-0033). PlatformRole is what a team gives them when they have
	// no workspace role (the older way; a role replaces it).
	Role         string     `json:"role,omitempty"`
	PlatformRole string     `json:"platformRole,omitempty"`
	FirstSeenAt  *time.Time `json:"firstSeenAt,omitempty"`
	LastSeenAt   *time.Time `json:"lastSeenAt,omitempty"`
}

func personView(id store.Identity) PersonView {
	groups := id.Groups
	if groups == nil {
		groups = []string{}
	}
	first, last := id.FirstSeenAt, id.LastSeenAt
	return PersonView{Email: id.Email, Name: id.Name, Provider: id.Provider, Groups: groups, Realm: id.Realm, Status: firstNonEmpty(id.Status, store.StatusActive), FirstSeenAt: &first, LastSeenAt: &last}
}

// withRoles fills the workspace role (and the team-given platform role)
// of a person from the membership snapshot.
func (s *Server) withRoles(snap *authz.Snapshot, p PersonView) PersonView {
	if snap == nil {
		return p
	}
	p.Role = snap.WorkspaceRole(p.Email)
	if p.Role == "" {
		roles := snap.RolesFor(ext.Identity{Email: p.Email, Provider: "person"})
		if roles.Enforced && !roles.Suspended {
			p.PlatformRole = roles.Platform
		}
	}
	return p
}

func (s *Server) workspaceView(c *gin.Context, w *store.Workspace) WorkspaceView {
	owners := []string{}
	if snap, err := s.authz.SnapshotFor(c.Request.Context(), w.Slug); err == nil {
		owners = snap.Owners()
		if owners == nil {
			owners = []string{}
		}
	}
	return WorkspaceView{
		Slug: w.Slug, Name: w.Name, Implicit: w.Implicit(),
		Domain: s.appsDomainOf(w), Address: w.Address, URL: s.dashboardURLOf(w), Status: firstNonEmpty(w.Status, store.WorkspaceActive),
		JoinPolicy: firstNonEmpty(w.Settings.JoinPolicy, store.JoinOpen), OwnMethodsOnly: w.Settings.OwnMethodsOnly, Branding: brandingView(w), Owners: owners, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt,
		MCPName: firstNonEmpty(strings.TrimSpace(w.Settings.MCPName), firstNonEmpty(w.Name, w.Slug)+" on shpyrd"), MCPURL: s.dashboardURLOf(w) + "/mcp",
	}
}

func (s *Server) getWorkspace(c *gin.Context) {
	w, err := s.store.Workspace(c.Request.Context(), s.workspace(c))
	if err != nil {
		storeErr(c, err, "workspace")
		return
	}
	view := s.workspaceView(c, w)
	if w.Settings.Limits != nil {
		view.Limits = w.Settings.Limits
		view.Usage = s.usageOf(c.Request.Context(), w.Slug)
	}
	c.JSON(http.StatusOK, view)
}

func (s *Server) updateWorkspace(c *gin.Context) {
	var req struct {
		Name           *string `json:"name"`
		JoinPolicy     *string `json:"joinPolicy"`
		OwnMethodsOnly *bool   `json:"ownMethodsOnly"`
		Address        *string `json:"address"`
		// Branding: the logo as a data URL (data:image/png;base64,...),
		// "" to remove it; the colour as #rrggbb, "" to reset.
		Logo  *string `json:"logo"`
		Color *string `json:"color"`
		// MCPName names the workspace's MCP server for assistants; "" resets.
		MCPName *string `json:"mcpName"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	ctx := c.Request.Context()
	w, err := s.store.Workspace(ctx, s.workspace(c))
	if err != nil {
		storeErr(c, err, "workspace")
		return
	}
	var changes []string
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > 80 {
			abort(c, http.StatusBadRequest, errors.New("name must be 1 to 80 characters"))
			return
		}
		if w, err = s.store.UpdateWorkspace(ctx, s.workspace(c), name); err != nil {
			storeErr(c, err, "workspace")
			return
		}
		changes = append(changes, "name: "+name)
	}
	if req.JoinPolicy != nil {
		switch *req.JoinPolicy {
		case store.JoinOpen, store.JoinCompany, store.JoinListed:
		default:
			abort(c, http.StatusBadRequest, errors.New("joinPolicy must be open, company or listed"))
			return
		}
		settings := w.Settings
		settings.JoinPolicy = *req.JoinPolicy
		if w, err = s.store.UpdateWorkspaceSettings(ctx, s.workspace(c), settings); err != nil {
			storeErr(c, err, "workspace")
			return
		}
		changes = append(changes, "join policy: "+*req.JoinPolicy)
	}
	if req.OwnMethodsOnly != nil && *req.OwnMethodsOnly != w.Settings.OwnMethodsOnly {
		if w.Implicit() {
			abort(c, http.StatusBadRequest, errors.New("the platform's own workspace offers every method"))
			return
		}
		if *req.OwnMethodsOnly && !s.hasOwnMethod(w) {
			abort(c, http.StatusBadRequest, errors.New("add a sign-in method of this workspace first, or nobody could sign in"))
			return
		}
		settings := w.Settings
		settings.OwnMethodsOnly = *req.OwnMethodsOnly
		if w, err = s.store.UpdateWorkspaceSettings(ctx, s.workspace(c), settings); err != nil {
			storeErr(c, err, "workspace")
			return
		}
		changes = append(changes, fmt.Sprintf("own methods only: %v", *req.OwnMethodsOnly))
	}
	if req.MCPName != nil {
		if err := validMCPName(*req.MCPName); err != nil {
			abort(c, http.StatusBadRequest, err)
			return
		}
		settings := w.Settings
		settings.MCPName = strings.TrimSpace(*req.MCPName)
		if w, err = s.store.UpdateWorkspaceSettings(ctx, s.workspace(c), settings); err != nil {
			storeErr(c, err, "workspace")
			return
		}
		changes = append(changes, "mcp name: "+firstNonEmpty(settings.MCPName, "(default)"))
	}
	if req.Logo != nil || req.Color != nil {
		settings := w.Settings
		b := store.Branding{}
		if settings.Branding != nil {
			b = *settings.Branding
		}
		if req.Logo != nil {
			logo, typ, err := parseLogo(*req.Logo)
			if err != nil {
				abort(c, http.StatusBadRequest, err)
				return
			}
			b.Logo, b.LogoType = logo, typ
			changes = append(changes, "logo")
		}
		if req.Color != nil {
			color := strings.ToLower(strings.TrimSpace(*req.Color))
			if color != "" && !colorRe.MatchString(color) {
				abort(c, http.StatusBadRequest, errors.New("color is #rrggbb"))
				return
			}
			b.Color = color
			changes = append(changes, "color")
		}
		if b == (store.Branding{}) {
			settings.Branding = nil
		} else {
			settings.Branding = &b
		}
		if w, err = s.store.UpdateWorkspaceSettings(ctx, s.workspace(c), settings); err != nil {
			storeErr(c, err, "workspace")
			return
		}
	}
	if req.Address != nil {
		moved, err := s.changeAddress(c, w, *req.Address)
		if err != nil {
			var ae *apiError
			if errors.As(err, &ae) {
				abort(c, ae.status, ae)
				return
			}
			storeErr(c, err, "workspace")
			return
		}
		w = moved
	}
	if len(changes) > 0 {
		s.forgetTenants()
		s.audit(c, "", "workspace.update", w.Slug, strings.Join(changes, ", "))
	}
	c.JSON(http.StatusOK, s.workspaceView(c, w))
}

var colorRe = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// logoTypes are the image types a logo may be.
var logoTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/svg+xml": true, "image/webp": true, "image/gif": true}

// parseLogo takes a data URL and returns the base64 payload and its type;
// "" removes the logo. At most 256 KB decoded.
func parseLogo(dataURL string) (logo, typ string, err error) {
	dataURL = strings.TrimSpace(dataURL)
	if dataURL == "" {
		return "", "", nil
	}
	rest, ok := strings.CutPrefix(dataURL, "data:")
	if !ok {
		return "", "", errors.New("logo must be a data URL (data:image/png;base64,...)")
	}
	meta, payload, ok := strings.Cut(rest, ",")
	if !ok || !strings.HasSuffix(meta, ";base64") {
		return "", "", errors.New("logo must be a base64 data URL")
	}
	typ = strings.TrimSuffix(meta, ";base64")
	if !logoTypes[typ] {
		return "", "", errors.New("logo must be a PNG, JPEG, SVG, WebP or GIF image")
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", "", errors.New("logo is not valid base64")
	}
	if len(raw) > 256*1024 {
		return "", "", errors.New("logo must be at most 256 KB")
	}
	return base64.StdEncoding.EncodeToString(raw), typ, nil
}

// brandingView is the look of a workspace as pages use it.
func brandingView(w *store.Workspace) *BrandingView {
	if w == nil || w.Settings.Branding == nil {
		return nil
	}
	b := w.Settings.Branding
	out := &BrandingView{Color: b.Color}
	if b.Logo != "" {
		sum := sha256.Sum256([]byte(b.Logo))
		out.LogoURL = "/api/workspace/logo?v=" + hex.EncodeToString(sum[:6])
	}
	return out
}

// workspaceLogo is GET /api/workspace/logo (public): the image itself,
// cacheable by its version.
func (s *Server) workspaceLogo(c *gin.Context) {
	w, err := s.tenant(c)
	if err != nil || w.Settings.Branding == nil || w.Settings.Branding.Logo == "" {
		c.Status(http.StatusNotFound)
		return
	}
	raw, err := base64.StdEncoding.DecodeString(w.Settings.Branding.Logo)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=86400")
	c.Header("X-Content-Type-Options", "nosniff")
	// An SVG opened as a document must not run scripts on this origin.
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	c.Data(http.StatusOK, w.Settings.Branding.LogoType, raw)
}

// hasOwnMethod reports whether the workspace configured a login method of
// its own (RFC-0033 per-workspace SSO).
func (s *Server) hasOwnMethod(w *store.Workspace) bool {
	if s.rp == nil {
		return false
	}
	for _, p := range s.rp.providerList() {
		if p.Workspace == w.Slug {
			return true
		}
	}
	if p := s.rp.passwordProvider(); p != nil && p.Workspace == w.Slug {
		return true
	}
	return false
}

// ---- domain claims -----------------------------------------------------------

// DomainClaimView is one claimed email domain.
type DomainClaimView struct {
	Domain     string     `json:"domain"`
	Connector  string     `json:"connector,omitempty"`
	Verified   bool       `json:"verified"`
	VerifiedAt *time.Time `json:"verifiedAt,omitempty"`
	// Record is the DNS TXT record that proves ownership.
	Record      string `json:"record"`
	RecordValue string `json:"recordValue"`
}

func claimView(d store.DomainClaim) DomainClaimView {
	return DomainClaimView{Domain: d.Domain, Connector: d.Connector, Verified: d.VerifiedAt != nil, VerifiedAt: d.VerifiedAt, Record: "_shpyrd-verify." + d.Domain, RecordValue: "shpyrd-verify=" + d.Token}
}

func (s *Server) listDomainClaims(c *gin.Context) {
	claims, err := s.store.ListDomainClaims(c.Request.Context(), s.workspace(c))
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	out := make([]DomainClaimView, 0, len(claims))
	for _, d := range claims {
		out = append(out, claimView(d))
	}
	c.JSON(http.StatusOK, out)
}

var emailDomainRe = regexp.MustCompile(`^([a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?\.)+[a-z]{2,}$`)

func (s *Server) putDomainClaim(c *gin.Context) {
	var req struct {
		Domain    string `json:"domain" binding:"required"`
		Connector string `json:"connector"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	if !emailDomainRe.MatchString(domain) {
		abort(c, http.StatusBadRequest, errors.New("that is not a domain name (acme.com)"))
		return
	}
	d, err := s.store.PutDomainClaim(c.Request.Context(), s.workspace(c), domain, strings.TrimSpace(req.Connector))
	if err != nil {
		storeErr(c, err, "domain claim")
		return
	}
	s.audit(c, "", "workspace.domain.claim", domain, "connector "+firstNonEmpty(d.Connector, "-"))
	c.JSON(http.StatusOK, claimView(*d))
}

// verifyDomainClaim is POST /api/workspace/domain-claims/:domain/verify:
// looks the TXT record up and records the result.
func (s *Server) verifyDomainClaim(c *gin.Context) {
	domain := strings.ToLower(c.Param("domain"))
	claims, err := s.store.ListDomainClaims(c.Request.Context(), s.workspace(c))
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	var claim *store.DomainClaim
	for i := range claims {
		if claims[i].Domain == domain {
			claim = &claims[i]
		}
	}
	if claim == nil {
		abort(c, http.StatusNotFound, errors.New("domain claim not found"))
		return
	}
	lookup := s.lookupTXT
	if lookup == nil {
		lookup = func(ctx context.Context, name string) ([]string, error) {
			return net.DefaultResolver.LookupTXT(ctx, name)
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	records, _ := lookup(ctx, "_shpyrd-verify."+domain)
	want := "shpyrd-verify=" + claim.Token
	for _, r := range records {
		if strings.TrimSpace(r) == want {
			d, err := s.store.MarkDomainVerified(c.Request.Context(), s.workspace(c), domain, time.Now())
			if err != nil {
				storeErr(c, err, "domain claim")
				return
			}
			s.audit(c, "", "workspace.domain.verified", domain, "")
			c.JSON(http.StatusOK, claimView(*d))
			return
		}
	}
	abort(c, http.StatusConflict, fmt.Errorf("no TXT record %q with value %q yet (DNS changes can take a few minutes)", "_shpyrd-verify."+domain, want))
}

func (s *Server) deleteDomainClaim(c *gin.Context) {
	domain := strings.ToLower(c.Param("domain"))
	if err := s.store.DeleteDomainClaim(c.Request.Context(), s.workspace(c), domain); err != nil {
		storeErr(c, err, "domain claim")
		return
	}
	s.audit(c, "", "workspace.domain.unclaim", domain, "")
	c.Status(http.StatusNoContent)
}

// ---- admission at sign-in ------------------------------------------------------

// admitSignIn decides whether a person may sign in (RFC-0033 phase 3):
// suspended people may not; accounts of a verified domain claim with a
// connector must arrive through that connector; someone signing in for the
// first time must satisfy the workspace's join policy, unless they were
// invited or already hold a role. Operators (token, kubeconfig) are not
// people and always pass.
func (s *Server) admitSignIn(ctx context.Context, ws string, id ext.Identity) error {
	if id.Email == "" || id.Provider == "token" || id.Provider == "kubeconfig" {
		return nil
	}
	email := strings.ToLower(id.Email)
	if ws == "" {
		ws = store.DefaultWorkspace
	}
	people, err := s.store.ListIdentities(ctx, ws)
	if err != nil {
		return nil // the store is down: sign-in must not depend on it
	}
	var known *store.Identity
	for i := range people {
		if people[i].Email == email {
			known = &people[i]
		}
	}
	if known != nil && known.Status == store.StatusSuspended {
		return errors.New("your access is suspended; ask an administrator")
	}
	emailDomain := ""
	if i := strings.LastIndex(email, "@"); i > 0 {
		emailDomain = email[i+1:]
	}
	claims, _ := s.store.ListDomainClaims(ctx, ws)
	var claimed *store.DomainClaim
	for i := range claims {
		if claims[i].Domain == emailDomain && claims[i].VerifiedAt != nil {
			claimed = &claims[i]
		}
	}
	if claimed != nil && claimed.Connector != "" && id.Provider != claimed.Connector {
		label := claimed.Connector
		if s.rp != nil {
			if p := s.rp.provider(claimed.Connector); p != nil {
				label = p.Label
			}
		}
		return fmt.Errorf("accounts of %s sign in with %s", emailDomain, label)
	}
	if known != nil {
		return nil
	}
	w, err := s.store.Workspace(ctx, ws)
	if err != nil {
		return nil
	}
	// An invitation, or a role given ahead of the first sign-in, is the
	// explicit act every join policy asks for.
	if inv := s.pendingInvitation(ctx, ws, email); inv != nil {
		return nil
	}
	snap, err := s.authz.SnapshotFor(ctx, ws)
	if err == nil && snap.WorkspaceRole(email) != "" {
		return nil
	}
	switch w.Settings.JoinPolicy {
	case store.JoinCompany:
		if claimed == nil {
			return fmt.Errorf("only accounts of the company's domain can join; ask an administrator to invite you")
		}
	case store.JoinListed:
		if snap == nil {
			return nil
		}
		for _, t := range snap.Teams {
			for _, m := range t.Members {
				if m == email {
					return nil
				}
			}
		}
		for _, g := range snap.Grants {
			if g.User == email {
				return nil
			}
		}
		return errors.New("only people who were invited or added to a team may join; ask an administrator to invite you")
	}
	return nil
}

// listPeople is GET /api/workspace/people: everyone who has signed in,
// plus those who hold a role and have not yet.
func (s *Server) listPeople(c *gin.Context) {
	ctx := c.Request.Context()
	ids, err := s.store.ListIdentities(ctx, s.workspace(c))
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	snap, _ := s.authz.SnapshotFor(ctx, s.workspace(c))
	out := make([]PersonView, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id.Email] = true
		out = append(out, s.withRoles(snap, personView(id)))
	}
	if snap != nil {
		for _, m := range snap.Memberships {
			if !seen[m.Email] {
				out = append(out, s.withRoles(snap, PersonView{Email: m.Email, Groups: []string{}, Status: store.StatusActive}))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	c.JSON(http.StatusOK, out)
}

// updatePerson is PATCH /api/workspace/people/:email {status?, role?}:
// suspend a person (no role anywhere, no app opens, at once) or
// reactivate them; set or remove their workspace role (RFC-0033). Naming
// or demoting an owner takes an owner, and the last owner stays.
func (s *Server) updatePerson(c *gin.Context) {
	var req struct {
		Status *string `json:"status"`
		Role   *string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	if req.Status == nil && req.Role == nil {
		abort(c, http.StatusBadRequest, errors.New("nothing to change: give status or role"))
		return
	}
	ctx := c.Request.Context()
	email := strings.ToLower(c.Param("email"))
	me, _ := ext.IdentityFrom(c)
	var person *PersonView
	if req.Status != nil {
		if *req.Status != store.StatusActive && *req.Status != store.StatusSuspended {
			abort(c, http.StatusBadRequest, errors.New("status must be active or suspended"))
			return
		}
		if strings.EqualFold(me.Email, email) && *req.Status == store.StatusSuspended {
			abort(c, http.StatusBadRequest, errors.New("you cannot suspend yourself"))
			return
		}
		id, err := s.store.SetIdentityStatus(ctx, s.workspace(c), email, *req.Status)
		if err != nil {
			storeErr(c, err, "person")
			return
		}
		s.membershipChanged()
		s.audit(c, "", "workspace.person."+*req.Status, email, "")
		v := personView(*id)
		person = &v
	}
	if req.Role != nil {
		role := strings.TrimSpace(*req.Role)
		if role != "" && !store.ValidWorkspaceRole(role) {
			abort(c, http.StatusBadRequest, errors.New("role must be owner, admin, member or empty (no role)"))
			return
		}
		if !validEmail(email) {
			abort(c, http.StatusBadRequest, errors.New("that is not an email address"))
			return
		}
		s.claimOwnershipInBootstrap(c)
		snap, err := s.authz.SnapshotFor(ctx, s.workspace(c))
		if err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
		current := snap.WorkspaceRole(email)
		if current == store.WorkspaceRoleOwner || role == store.WorkspaceRoleOwner {
			roles, _ := s.rolesOf(c)
			if !roles.Can(authz.WorkspaceOwner, "") {
				abort(c, http.StatusForbidden, denial(roles, authz.WorkspaceOwner, ""))
				return
			}
		}
		if current == store.WorkspaceRoleOwner && role != store.WorkspaceRoleOwner && len(snap.Owners()) == 1 {
			abort(c, http.StatusConflict, errors.New("the workspace needs an owner: name another owner first"))
			return
		}
		if role != current {
			if role == "" {
				err = s.store.DeleteMembership(ctx, s.workspace(c), email)
			} else {
				_, err = s.store.PutMembership(ctx, s.workspace(c), email, role)
			}
			if err != nil {
				storeErr(c, err, "person")
				return
			}
			s.membershipChanged()
			s.audit(c, "", "workspace.role", email, firstNonEmpty(current, "none")+" -> "+firstNonEmpty(role, "none"))
		}
		if person == nil {
			v := PersonView{Email: email, Groups: []string{}, Status: store.StatusActive}
			if id, err := s.store.GetIdentity(ctx, s.workspace(c), email); err == nil {
				v = personView(*id)
			}
			person = &v
		}
	}
	snap, _ := s.authz.SnapshotFor(ctx, s.workspace(c))
	c.JSON(http.StatusOK, s.withRoles(snap, *person))
}

// claimOwnershipInBootstrap makes the acting person the workspace's owner
// when they are the first to define who is who (a role, an invitation, a
// team, a grant) in a workspace still in bootstrap mode: the change ends
// bootstrap, and without this it would take the actor's own access with
// it. Operators (the admin token) are not people and claim nothing.
func (s *Server) claimOwnershipInBootstrap(c *gin.Context) {
	roles, err := s.rolesOf(c)
	if err != nil || roles.Enforced {
		return
	}
	me, ok := ext.IdentityFrom(c)
	if !ok || me.Email == "" || me.Provider == "token" || me.Provider == "kubeconfig" || me.Subject == "admin-token" {
		return
	}
	ctx := c.Request.Context()
	if _, err := s.store.PutMembership(ctx, s.workspace(c), me.Email, store.WorkspaceRoleOwner); err != nil {
		s.log.Warn("could not claim ownership", "email", me.Email, "err", err.Error())
		return
	}
	s.membershipChanged()
	s.audit(c, "", "workspace.role", strings.ToLower(me.Email), "none -> owner (first to define roles)")
	if fresh, err := s.authz.RolesIn(ctx, s.workspace(c), me); err == nil {
		c.Set(rolesKey, fresh) // the request goes on as the owner
	}
}

// validEmail says the string is one address (no display name, no list).
func validEmail(email string) bool {
	email = strings.TrimSpace(email)
	if email == "" || len(email) > 254 || strings.ContainsAny(email, " <>,;\n\t") {
		return false
	}
	addr, err := mail.ParseAddress(email)
	return err == nil && strings.EqualFold(addr.Address, email)
}

// forgetPerson removes the record of a person; grants and team memberships
// are by email and stay (remove those explicitly). Sign-in recreates the
// record.
func (s *Server) forgetPerson(c *gin.Context) {
	email := strings.ToLower(c.Param("email"))
	if err := s.store.DeleteIdentity(c.Request.Context(), s.workspace(c), email); err != nil {
		storeErr(c, err, "person")
		return
	}
	s.audit(c, "", "workspace.person.forget", email, "")
	c.Status(http.StatusNoContent)
}

// recordSignIn notes an identity in the workspace's people and returns the
// person as the workspace knows them (their id is what apps see as the
// JWT's subject: stable across login methods, unlike a provider's). Tokens
// and the admin token are not people. Failures are logged, never surfaced:
// a sign-in must not depend on the store.
func (s *Server) recordSignIn(c *gin.Context, id ext.Identity) *store.Identity {
	if id.Email == "" || id.Provider == "token" || id.Provider == "kubeconfig" || id.Subject == "admin-token" {
		return nil
	}
	person, err := s.store.TouchIdentity(c.Request.Context(), s.workspace(c), store.Identity{Email: id.Email, Name: id.Name, Provider: id.Provider, Groups: id.Groups})
	if err != nil {
		s.log.Warn("could not record sign-in", "email", id.Email, "err", err.Error())
		return nil
	}
	return person
}

// exportWorkspace is GET /api/workspace/export: the store's content as the
// platform backup carries it (RFC-0037).
func (s *Server) exportWorkspace(c *gin.Context) {
	dump, err := s.store.Export(c.Request.Context(), s.workspace(c))
	if err != nil {
		storeErr(c, err, "workspace")
		return
	}
	c.JSON(http.StatusOK, dump)
}

// importWorkspace is POST /api/workspace/import[?overwrite=true]: teams,
// grants and people from a backup (`shpyrd cluster restore`). At the
// console, ?workspace=<slug> restores an explicit workspace's dump on a
// platform that has workspaces, recreating the workspace from the dump
// when it is gone (a restore onto a fresh cluster).
func (s *Server) importWorkspace(c *gin.Context) {
	var dump store.Dump
	if err := c.ShouldBindJSON(&dump); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	if dump.Version > store.DumpVersion {
		abort(c, http.StatusBadRequest, errors.New("this backup was made by a newer shpyrd; upgrade first"))
		return
	}
	ctx := c.Request.Context()
	target := s.workspace(c)
	if ws := c.Query("workspace"); ws != "" && ws != target {
		if !s.atConsole(c) {
			abort(c, http.StatusNotFound, errors.New("another workspace's dump is restored from the console"))
			return
		}
		if !s.hasCapability("workspaces") {
			abort(c, http.StatusBadRequest, errors.New("this platform has one workspace; the dump names another"))
			return
		}
		if dump.Workspace.Slug != "" && dump.Workspace.Slug != ws {
			abort(c, http.StatusBadRequest, fmt.Errorf("the dump is of workspace %q, not %q", dump.Workspace.Slug, ws))
			return
		}
		if _, err := s.store.Workspace(ctx, ws); errors.Is(err, store.ErrNotFound) {
			created, err := s.store.CreateWorkspace(ctx, store.Workspace{Slug: ws, Name: firstNonEmpty(dump.Workspace.Name, ws), Address: dump.Workspace.Address, Settings: dump.Workspace.Settings})
			if err != nil {
				storeErr(c, err, "workspace")
				return
			}
			if dump.Workspace.Status == store.WorkspaceSuspended {
				_, _ = s.store.SetWorkspaceStatus(ctx, created.Slug, store.WorkspaceSuspended)
			}
			s.workspacesChanged()
		} else if err != nil {
			storeErr(c, err, "workspace")
			return
		}
		target = ws
	}
	res, err := s.store.Import(ctx, target, &dump, c.Query("overwrite") == "true")
	if err != nil {
		storeErr(c, err, "workspace")
		return
	}
	s.membershipChanged()
	s.audit(c, "", "workspace.import", "store "+target, "teams "+itoa(res.Teams)+", grants "+itoa(res.Grants)+", people "+itoa(res.Identities))
	c.JSON(http.StatusOK, gin.H{"workspace": target, "teams": res.Teams, "grants": res.Grants, "people": res.Identities, "skipped": res.Skipped})
}

func itoa(n int) string { return strconv.Itoa(n) }
