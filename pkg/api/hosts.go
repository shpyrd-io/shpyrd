package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/authz"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
)

// Names of a workspace beyond its address (RFC-0033 names): the address
// can change (the old one redirects for a while), and a company can bring
// its own domain in CNAME mode: intranet.acme.com and *.intranet.acme.com
// point at the address, a TXT record (or the CNAME itself) proves the
// domain, the platform issues a certificate per host, and the domain may
// become the primary one — the dashboard and app URLs use it, the address
// keeps answering.

// movedTTL is how long a previous address redirects to the current one.
const movedTTL = 30 * 24 * time.Hour

// hostsCache remembers a workspace's host records briefly: the edge asks
// per request.
type hostsCache struct {
	mu    sync.Mutex
	by    map[string]hostsEntry
	clock func() time.Time
}

type hostsEntry struct {
	hosts   []store.WorkspaceHost
	expires time.Time
}

// hostsOf lists a workspace's host records, cached for ten seconds.
func (s *Server) hostsOf(ctx context.Context, ws *store.Workspace) []store.WorkspaceHost {
	if ws == nil || ws.Implicit() {
		return nil
	}
	now := time.Now()
	s.hosts.mu.Lock()
	if e, ok := s.hosts.by[ws.Slug]; ok && now.Before(e.expires) {
		s.hosts.mu.Unlock()
		return e.hosts
	}
	s.hosts.mu.Unlock()
	hosts, err := s.store.ListWorkspaceHosts(ctx, ws.Slug)
	if err != nil {
		return nil
	}
	s.hosts.mu.Lock()
	if s.hosts.by == nil {
		s.hosts.by = map[string]hostsEntry{}
	}
	s.hosts.by[ws.Slug] = hostsEntry{hosts: hosts, expires: now.Add(10 * time.Second)}
	s.hosts.mu.Unlock()
	return hosts
}

// forgetHosts drops the cache after a change, and the app host index
// built from it.
func (s *Server) forgetHosts() {
	s.hosts.mu.Lock()
	s.hosts.by = nil
	s.hosts.mu.Unlock()
	s.hostCache.mu.Lock()
	s.hostCache.apps = nil
	s.hostCache.mu.Unlock()
}

// primaryDomainOf is the domain a workspace's URLs use: its primary custom
// domain when verified, its address otherwise.
func (s *Server) primaryDomainOf(ctx context.Context, ws *store.Workspace) string {
	if ws == nil || ws.Implicit() {
		return ""
	}
	for _, h := range s.hostsOf(ctx, ws) {
		if h.Kind == store.HostCustom && h.Primary && h.VerifiedAt != nil {
			return h.Host
		}
	}
	return ws.Address
}

// appsDomainsOf lists every domain a workspace's apps answer under, the
// primary first.
func (s *Server) appsDomainsOf(ctx context.Context, ws *store.Workspace) []string {
	if ws == nil || ws.Implicit() {
		return []string{s.opts.Public.Domain}
	}
	primary := s.primaryDomainOf(ctx, ws)
	out := []string{primary}
	if ws.Address != "" && ws.Address != primary {
		out = append(out, ws.Address)
	}
	for _, h := range s.hostsOf(ctx, ws) {
		if h.Kind == store.HostCustom && h.VerifiedAt != nil && h.Host != primary {
			out = append(out, h.Host)
		}
	}
	return out
}

// movedTarget says where a request at a moved host goes: the same path at
// the current address (an app host keeps its label). "" when the host is
// not a moved one.
func (s *Server) movedTarget(c *gin.Context) string {
	host := tenancy.Host(c.Request.Host)
	// The edge's subrequests arrive at the server's own name and carry the
	// app host in X-Original-URL: not a moved host, and resolving the
	// tenant here would fix it to the wrong workspace for the request.
	if s.tenancy == nil || tenancy.Internal(host) || strings.HasPrefix(c.Request.URL.Path, "/edge/") {
		return ""
	}
	ws, err := s.tenancy.Resolve(c.Request.Context(), host)
	if err != nil || ws == nil || ws.Implicit() {
		return ""
	}
	if host == ws.Address || oneLabelUnder(host, ws.Address) {
		return ""
	}
	for _, h := range s.hostsOf(c.Request.Context(), ws) {
		if h.Kind != store.HostMoved || !tenancy.HostServes(&h) {
			continue
		}
		if host == h.Host {
			return "https://" + s.withPort(ws.Address) + c.Request.URL.RequestURI()
		}
		if label, ok := strings.CutSuffix(host, "."+h.Host); ok && label != "" && !strings.Contains(label, ".") {
			return "https://" + s.withPort(label+"."+ws.Address) + c.Request.URL.RequestURI()
		}
	}
	return ""
}

// redirectMoved sends requests at a previous address to the current one
// (301: browsers and crawlers update their links).
func (s *Server) redirectMoved() gin.HandlerFunc {
	return func(c *gin.Context) {
		if target := s.movedTarget(c); target != "" {
			c.Redirect(http.StatusMovedPermanently, target)
			c.Abort()
			return
		}
		c.Next()
	}
}

func oneLabelUnder(host, domain string) bool {
	label, ok := strings.CutSuffix(host, "."+domain)
	return ok && label != "" && !strings.Contains(label, ".")
}

var hostRe = regexp.MustCompile(`^([a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// ---- the address -------------------------------------------------------------

// changeAddress moves the workspace to a new address under the same parent
// domain (demo.shpyrd.app -> acme.shpyrd.app): the label is a workspace
// slug by shape, not another workspace's slug, address or host, and not
// reserved. The old address redirects for thirty days. Owners only.
func (s *Server) changeAddress(c *gin.Context, w *store.Workspace, want string) (*store.Workspace, error) {
	roles, _ := s.rolesOf(c)
	if !roles.Can(authz.WorkspaceOwner, "") {
		return nil, &apiError{http.StatusForbidden, denial(roles, authz.WorkspaceOwner, "").Error()}
	}
	if w.Implicit() || w.Address == "" {
		return nil, &apiError{http.StatusBadRequest, "this workspace answers at the platform's address; it has no address of its own to change"}
	}
	want = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(want), "."))
	_, parent, ok := strings.Cut(w.Address, ".")
	if !ok {
		return nil, &apiError{http.StatusBadRequest, "the current address has no parent domain"}
	}
	label := want
	if strings.Contains(want, ".") {
		l, p, _ := strings.Cut(want, ".")
		if p != parent {
			return nil, &apiError{http.StatusBadRequest, fmt.Sprintf("the address stays under %s (%s); to use your own domain, add it as a custom domain", parent, l+"."+parent)}
		}
		label = l
	}
	if err := project.ValidateWorkspaceSlug(label); err != nil {
		return nil, &apiError{http.StatusBadRequest, "the address label: " + err.Error()}
	}
	address := label + "." + parent
	if address == w.Address {
		return w, nil
	}
	ctx := c.Request.Context()
	if other, err := s.store.Workspace(ctx, label); err == nil && other.ID != w.ID {
		return nil, &apiError{http.StatusConflict, fmt.Sprintf("%s is another workspace's name", label)}
	}
	updated, err := s.store.UpdateWorkspaceAddress(ctx, w.Slug, address)
	if errors.Is(err, store.ErrConflict) {
		return nil, &apiError{http.StatusConflict, fmt.Sprintf("%s is taken", address)}
	}
	if err != nil {
		return nil, err
	}
	exp := time.Now().Add(movedTTL)
	if _, err := s.store.PutWorkspaceHost(ctx, w.Slug, store.WorkspaceHost{Host: w.Address, Kind: store.HostMoved, ExpiresAt: &exp}); err != nil {
		s.log.Warn("could not keep the old address redirecting", "workspace", w.Slug, "address", w.Address, "err", err.Error())
	}
	s.forgetHosts()
	s.workspacesChanged()
	s.audit(c, "", "workspace.address", w.Slug, w.Address+" -> "+address)
	return updated, nil
}

// apiError carries a status with a message.
type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string { return e.msg }

// ---- custom domains ------------------------------------------------------------

// DomainView is one custom domain of the workspace, with the DNS records
// to publish.
type DomainView struct {
	Host       string     `json:"host"`
	Verified   bool       `json:"verified"`
	VerifiedAt *time.Time `json:"verifiedAt,omitempty"`
	Primary    bool       `json:"primary"`
	// Records are what the company publishes in its DNS: the host and its
	// apps pointing at the workspace address, and the proof.
	Records []DNSRecord `json:"records"`
	// URL is where the dashboard answers at this domain once verified.
	URL string `json:"url"`
}

// DNSRecord is a record to publish.
type DNSRecord struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (s *Server) domainView(ws *store.Workspace, h store.WorkspaceHost) DomainView {
	return DomainView{
		Host: h.Host, Verified: h.VerifiedAt != nil, VerifiedAt: h.VerifiedAt, Primary: h.Primary,
		Records: []DNSRecord{
			{Type: "CNAME", Name: h.Host, Value: ws.Address},
			{Type: "CNAME", Name: "*." + h.Host, Value: ws.Address},
			{Type: "TXT", Name: "_shpyrd-verify." + h.Host, Value: "shpyrd-verify=" + h.Token},
		},
		URL: "https://" + s.withPort(h.Host),
	}
}

func (s *Server) listWorkspaceDomains(c *gin.Context) {
	ws, err := s.tenant(c)
	if err != nil {
		storeErr(c, err, "workspace")
		return
	}
	out := []DomainView{}
	for _, h := range s.hostsOf(c.Request.Context(), ws) {
		if h.Kind == store.HostCustom {
			out = append(out, s.domainView(ws, h))
		}
	}
	c.JSON(http.StatusOK, out)
}

// addDomain is POST /api/workspace/domains {host}: records the company's
// domain and tells what to publish. It must be the company's own name, not
// one under the platform's or another workspace's.
func (s *Server) addWorkspaceDomain(c *gin.Context) {
	var req struct {
		Host string `json:"host" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(req.Host), "."))
	if !hostRe.MatchString(host) {
		abort(c, http.StatusBadRequest, errors.New("that is not a host name (intranet.acme.com)"))
		return
	}
	ws, err := s.tenant(c)
	if err != nil {
		storeErr(c, err, "workspace")
		return
	}
	if ws.Implicit() || ws.Address == "" {
		abort(c, http.StatusBadRequest, errors.New("custom domains are for workspaces with an address of their own"))
		return
	}
	if host == ws.Address || strings.HasSuffix(host, "."+ws.Address) || host == s.opts.Public.Domain || strings.HasSuffix(host, "."+s.opts.Public.Domain) {
		abort(c, http.StatusBadRequest, errors.New("that name is the platform's already; a custom domain is one your company owns"))
		return
	}
	if _, parent, ok := strings.Cut(ws.Address, "."); ok && (host == parent || strings.HasSuffix(host, "."+parent)) {
		abort(c, http.StatusBadRequest, fmt.Errorf("names under %s are workspace addresses; change the address instead", parent))
		return
	}
	h, err := s.store.PutWorkspaceHost(c.Request.Context(), ws.Slug, store.WorkspaceHost{Host: host, Kind: store.HostCustom})
	if errors.Is(err, store.ErrConflict) {
		abort(c, http.StatusConflict, errors.New("that host belongs to another workspace"))
		return
	}
	if err != nil {
		storeErr(c, err, "domain")
		return
	}
	s.forgetHosts()
	s.audit(c, "", "workspace.domain.add", host, "")
	c.JSON(http.StatusCreated, s.domainView(ws, *h))
}

// verifyDomain is POST /api/workspace/domains/:host/verify: the TXT proof
// or the CNAME itself must point at the workspace. A verified domain is
// served: the front door and every app get a certificate for it.
func (s *Server) verifyWorkspaceDomain(c *gin.Context) {
	ws, h, ok := s.customDomainOf(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	lookupTXT := s.lookupTXT
	if lookupTXT == nil {
		lookupTXT = net.DefaultResolver.LookupTXT
	}
	lookupCNAME := s.lookupCNAME
	if lookupCNAME == nil {
		lookupCNAME = net.DefaultResolver.LookupCNAME
	}
	proven := false
	want := "shpyrd-verify=" + h.Token
	if records, _ := lookupTXT(ctx, "_shpyrd-verify."+h.Host); len(records) > 0 {
		for _, r := range records {
			if strings.TrimSpace(r) == want {
				proven = true
			}
		}
	}
	if !proven {
		if cname, err := lookupCNAME(ctx, h.Host); err == nil && strings.TrimSuffix(strings.ToLower(cname), ".") == ws.Address {
			proven = true
		}
	}
	if !proven {
		abort(c, http.StatusConflict, fmt.Errorf("neither a TXT record %q with value %q nor a CNAME from %s to %s is visible yet (DNS changes can take a few minutes)", "_shpyrd-verify."+h.Host, want, h.Host, ws.Address))
		return
	}
	now := time.Now()
	h.VerifiedAt = &now
	updated, err := s.store.PutWorkspaceHost(c.Request.Context(), ws.Slug, *h)
	if err != nil {
		storeErr(c, err, "domain")
		return
	}
	s.forgetHosts()
	s.forgetTenants()
	s.workspacesChanged()
	s.audit(c, "", "workspace.domain.verified", h.Host, "")
	c.JSON(http.StatusOK, s.domainView(ws, *updated))
}

// updateDomain is PATCH /api/workspace/domains/:host {primary}: a verified
// domain becomes the one the workspace's URLs use (owners), or stops
// being it.
func (s *Server) updateWorkspaceDomain(c *gin.Context) {
	var req struct {
		Primary *bool `json:"primary"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Primary == nil {
		abort(c, http.StatusBadRequest, errors.New("give primary: true or false"))
		return
	}
	ws, h, ok := s.customDomainOf(c)
	if !ok {
		return
	}
	roles, _ := s.rolesOf(c)
	if !roles.Can(authz.WorkspaceOwner, "") {
		abort(c, http.StatusForbidden, denial(roles, authz.WorkspaceOwner, ""))
		return
	}
	if *req.Primary && h.VerifiedAt == nil {
		abort(c, http.StatusBadRequest, errors.New("verify the domain first"))
		return
	}
	h.Primary = *req.Primary
	updated, err := s.store.PutWorkspaceHost(c.Request.Context(), ws.Slug, *h)
	if err != nil {
		storeErr(c, err, "domain")
		return
	}
	s.forgetHosts()
	s.forgetTenants()
	s.workspacesChanged()
	s.audit(c, "", "workspace.domain.primary", h.Host, fmt.Sprintf("%v", h.Primary))
	c.JSON(http.StatusOK, s.domainView(ws, *updated))
}

func (s *Server) deleteWorkspaceDomain(c *gin.Context) {
	ws, h, ok := s.customDomainOf(c)
	if !ok {
		return
	}
	if err := s.store.DeleteWorkspaceHost(c.Request.Context(), ws.Slug, h.Host); err != nil {
		storeErr(c, err, "domain")
		return
	}
	s.forgetHosts()
	s.forgetTenants()
	s.workspacesChanged()
	s.audit(c, "", "workspace.domain.remove", h.Host, "")
	c.Status(http.StatusNoContent)
}

// customDomainOf resolves :host to one of the workspace's custom domains,
// answering the request itself when it cannot.
func (s *Server) customDomainOf(c *gin.Context) (*store.Workspace, *store.WorkspaceHost, bool) {
	ws, err := s.tenant(c)
	if err != nil {
		storeErr(c, err, "workspace")
		return nil, nil, false
	}
	host := strings.ToLower(c.Param("host"))
	hosts, err := s.store.ListWorkspaceHosts(c.Request.Context(), ws.Slug)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return nil, nil, false
	}
	for i := range hosts {
		if hosts[i].Kind == store.HostCustom && hosts[i].Host == host {
			return ws, &hosts[i], true
		}
	}
	abort(c, http.StatusNotFound, errors.New("domain not found"))
	return nil, nil, false
}
