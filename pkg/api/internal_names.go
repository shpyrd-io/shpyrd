package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/edge"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Internal names (#311). A project reaches another of its workspace at
// http://<project>.internal, or http://<project>.<workspace>.internal. Both
// resolve to the internal front door; every project has an Ingress there
// for its qualified name, whose auth subrequest comes here with the address
// the call came from. The index of pod addresses says which project that
// is; the callee's allow list says whether it may call; the answer carries
// the caller's identity, svc:<workspace>/<project>, in headers and in a JWT
// of realm "service", the way a person's identity reaches an app.
//
// The bare name is one Ingress for *.internal, pointing here: the front
// door cannot tell two workspaces' "crm" apart by who is calling, so the
// server does. It forwards the request to the qualified name at the front
// door with a ticket naming the caller, and the usual path takes it from
// there. Nothing is asked of the caller beyond the URL.

const (
	internalTLD = ".internal"
	// realmService marks a service identity: the realm claim of the JWT,
	// and the query the internal Ingress asks /edge/auth with.
	realmService = "service"
	// Headers the callee receives.
	headerService       = "X-Shpyrd-Service"
	headerCallerProject = "X-Shpyrd-Caller-Project"
	// headerCallerTicket carries the ticket the server attaches when it
	// forwards a bare name (edge.CallerTicket).
	headerCallerTicket = "X-Shpyrd-Internal-Caller"
	// maintenancePath is where a project's Ingress sends every request while
	// the project is in maintenance (RFC-0020); it goes through on internal
	// hosts like on any other.
	maintenancePath = "/_shpyrd/maintenance"
)

// identityHeaders are every header the edge can set about a caller; nginx
// replaces each with the auth answer's, or drops it, so a client sends none
// of them to an app.
var identityHeaders = []string{"Authorization", "X-Shpyrd-User", "X-Shpyrd-Email", "X-Shpyrd-Name", "X-Shpyrd-Teams", "X-Shpyrd-Roles", "X-Shpyrd-Workspace", "X-Shpyrd-Operator", headerService, headerCallerProject}

// parseInternalHost reads an internal name: the project, and the workspace
// when the name carries one.
func parseInternalHost(host string) (slug, workspace string, ok bool) {
	host = hostOnly(host)
	if !strings.HasSuffix(host, internalTLD) {
		return "", "", false
	}
	labels := strings.Split(strings.TrimSuffix(host, internalTLD), ".")
	switch len(labels) {
	case 1:
		return labels[0], "", project.ValidSlug(labels[0])
	case 2:
		return labels[0], labels[1], project.ValidSlug(labels[0]) && labels[1] != ""
	}
	return "", "", false
}

// internalHostOf is a project's qualified internal name.
func internalHostOf(ws, slug string) string { return slug + "." + ws + internalTLD }

// serviceCallerOf finds the caller of an internal request from the address
// nginx put in the header named: a project's pod, through the index. The
// platform's own server presenting a ticket counts as the project the
// ticket names (it forwarded a bare name); without one it is the platform.
func (s *Server) serviceCallerOf(c *gin.Context, ipHeader string) (serviceCaller, string, bool) {
	ip := strings.TrimSpace(c.GetHeader(ipHeader))
	if i := strings.IndexByte(ip, ','); i >= 0 {
		ip = strings.TrimSpace(ip[:i])
	}
	if ip == "" || net.ParseIP(ip) == nil || s.callers == nil {
		return serviceCaller{}, ip, false
	}
	caller, ok := s.callers.Lookup(ip)
	if !ok {
		return serviceCaller{}, ip, false
	}
	if caller.Platform {
		if raw := c.GetHeader(headerCallerTicket); raw != "" {
			if t, err := s.edgeKeys.VerifyCallerTicket(raw); err == nil {
				return serviceCaller{Workspace: t.Workspace, Project: t.Project}, ip, true
			}
		}
	}
	return caller, ip, true
}

// serviceRefusal says why a call was not admitted, in the words the caller
// reads; empty when it was.
type serviceRefusal struct {
	Status int
	Error  string
}

// serviceDecision decides an internal call to slug in ws from the address in
// ipHeader. It is the one judgement, made at the auth subrequest and again
// on the error page nginx asks for after a refusal.
func (s *Server) serviceDecision(c *gin.Context, ws *store.Workspace, slug, ipHeader string) (serviceCaller, *shpyrdv1.App, serviceRefusal) {
	caller, ip, ok := s.serviceCallerOf(c, ipHeader)
	if !ok {
		return caller, nil, serviceRefusal{http.StatusForbidden, fmt.Sprintf("internal names are for the projects of a workspace, and %s is not one of their addresses", firstNonEmpty(ip, "the caller"))}
	}
	if caller.Platform {
		return caller, nil, serviceRefusal{http.StatusForbidden, "the platform calls a project by its qualified name, <project>.<workspace>.internal"}
	}
	app, err := s.findApp(c.Request.Context(), ws.Slug, slug)
	if errors.Is(err, store.ErrNotFound) {
		return caller, nil, serviceRefusal{http.StatusNotFound, fmt.Sprintf("no project %s in the %s workspace", slug, ws.Slug)}
	}
	if err != nil {
		return caller, nil, serviceRefusal{http.StatusBadGateway, err.Error()}
	}
	if caller.Workspace != ws.Slug {
		// The names are the workspace's own: nothing crosses one.
		return caller, app, serviceRefusal{http.StatusForbidden, fmt.Sprintf("%s is in another workspace; calls never cross one", caller.Project)}
	}
	for _, e := range app.EffectiveAllow() {
		if e.Project == caller.Project {
			return caller, app, serviceRefusal{}
		}
	}
	return caller, app, serviceRefusal{http.StatusForbidden, fmt.Sprintf("%s does not let %s in: add it on %s's Connections page, or with `shpyrd allow add project %s --project %s`", slug, caller.Project, slug, caller.Project, slug)}
}

// serviceAuth is the auth subrequest of a project's internal Ingress,
// GET /edge/auth?project=<slug>&workspace=<slug>&realm=service: who is
// calling, and may they.
func (s *Server) serviceAuth(c *gin.Context, ws *store.Workspace, slug string) {
	for _, h := range identityHeaders {
		c.Header(h, "")
	}
	caller, _, refusal := s.serviceDecision(c, ws, slug, "X-Real-IP")
	if refusal.Status != 0 {
		countDenial(ws.Slug, slug, denialService)
		c.JSON(refusal.Status, gin.H{"error": refusal.Error})
		return
	}
	now := time.Now()
	claims := edge.Claims{
		Issuer: s.dashboardURLOf(ws), Subject: caller.Service(), Audience: slug,
		Workspace: ws.Slug, WorkspaceID: ws.ID, WorkspaceURL: s.dashboardURLOf(ws), Project: slug,
		Roles: []string{}, Teams: []string{}, Realm: realmService,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(edge.TokenTTL).Unix(),
	}
	jwt, err := s.edgeKeys.Sign("JWT", claims)
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	countAdmission(ws.Slug, slug)
	countServiceCall(ws.Slug, slug, caller.Project)
	c.Header(headerService, caller.Service())
	c.Header(headerCallerProject, caller.Project)
	c.Header("Authorization", "Bearer "+jwt)
	c.Status(http.StatusOK)
}

// internalNames answers what the front door sends the server for an
// internal name: a bare name is forwarded to the qualified one with a
// ticket; an error nginx hands over (X-Code) is answered in JSON, as the
// caller is a program; a qualified name no Ingress serves is a project that
// is not there. The maintenance page goes through like on any host.
func (s *Server) internalNames() gin.HandlerFunc {
	return func(c *gin.Context) {
		host := hostOnly(c.Request.Host)
		if !strings.HasSuffix(host, internalTLD) || c.Request.URL.Path == maintenancePath {
			c.Next()
			return
		}
		c.Header("Cache-Control", "no-store")
		slug, ws, ok := parseInternalHost(host)
		if code := c.GetHeader("X-Code"); code != "" {
			s.internalNameError(c, code, slug, ws)
			c.Abort()
			return
		}
		switch {
		case !ok:
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("%s is not an internal name: those are <project>.internal and <project>.<workspace>.internal", host)})
		case ws == "":
			s.forwardInternalName(c, slug)
		default:
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("no project %s in the %s workspace answers at %s", slug, ws, host)})
		}
		c.Abort()
	}
}

// forwardInternalName sends a request for a bare name on to the qualified
// name of the caller's workspace at the front door, with a ticket saying who
// the caller is, since the front door will see this server's address.
func (s *Server) forwardInternalName(c *gin.Context, slug string) {
	caller, ip, ok := s.serviceCallerOf(c, "X-Real-IP")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("internal names are for the projects of a workspace, and %s is not one of their addresses", firstNonEmpty(ip, "the caller"))})
		return
	}
	if caller.Platform {
		c.JSON(http.StatusForbidden, gin.H{"error": "the platform calls a project by its qualified name, <project>.<workspace>.internal"})
		return
	}
	ticket, err := s.edgeKeys.SignCallerTicket(edge.CallerTicket{Workspace: caller.Workspace, Project: caller.Project})
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	door := s.internalDoor(c.Request.Context())
	qualified := internalHostOf(caller.Workspace, slug)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(&url.URL{Scheme: "http", Host: door})
			r.Out.Host = qualified
			r.SetXForwarded()
			for _, h := range identityHeaders {
				r.Out.Header.Del(h)
			}
			r.Out.Header.Set(headerCallerTicket, ticket)
		},
		FlushInterval: -1, // streams and long polls come through as they happen
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			s.log.Warn("internal name: the front door did not answer", "door", door, "host", qualified, "err", err)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, `{"error":%q}`, "the front door did not answer for "+qualified)
		},
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}

// internalNameError is the body of a refusal on an internal name: nginx's
// auth_request discards the auth answer's body and asks the default backend
// for a page instead, so the judgement is made again, from the address nginx
// recorded, and said in JSON.
func (s *Server) internalNameError(c *gin.Context, code, slug, wsSlug string) {
	switch code {
	case "403":
		ws, err := s.store.Workspace(c.Request.Context(), wsSlug)
		if err != nil || slug == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "this call was refused at the front door"})
			return
		}
		caller, _, refusal := s.serviceDecision(c, ws, slug, "X-Forwarded-For")
		if refusal.Status == 0 {
			// Admitted now: the refusal was a moment ago, before the allow
			// list changed, and nginx still had the old answer.
			c.JSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("%s lets %s in now; try again in a few seconds", slug, caller.Project)})
			return
		}
		body := gin.H{"error": refusal.Error, "project": slug}
		if caller.Project != "" {
			body["caller"] = caller.Service()
		}
		c.JSON(refusal.Status, body)
	case "503":
		c.Header("Retry-After", "5")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("%s is not answering right now: it is starting, or a new version is taking the old one's place; try again in a few moments", slug)})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("%s did not answer", slug)})
	}
}

// internalDoor is where the server forwards a bare internal name: the
// internal ingress controller's Service when the cluster has one, else the
// external's (a local cluster has one front door), on its plain HTTP port.
// Looked up once a minute: the controllers are installed once.
func (s *Server) internalDoor(ctx context.Context) string {
	if s.internalDoorFn != nil {
		return s.internalDoorFn()
	}
	s.doorCache.mu.Lock()
	defer s.doorCache.mu.Unlock()
	if s.doorCache.addr != "" && time.Since(s.doorCache.at) < time.Minute {
		return s.doorCache.addr
	}
	addr := doorHost(s.opts.IngressServiceExternal, "ingress-nginx-controller.ingress-nginx.svc.cluster.local")
	if s.kube != nil {
		if _, err := s.kube.Kube.CoreV1().Services("ingress-nginx-internal").Get(ctx, "ingress-nginx-internal-controller", metav1.GetOptions{}); err == nil {
			addr = doorHost(s.opts.IngressServiceInternal, "ingress-nginx-internal-controller.ingress-nginx-internal.svc.cluster.local")
		}
	}
	s.doorCache.addr, s.doorCache.at = addr, time.Now()
	return addr
}

// doorHost is a controller Service's host on port 80: the configured
// address without its port, or the default name.
func doorHost(configured, def string) string {
	host := def
	if configured != "" {
		host = hostOnly(configured)
	}
	return host + ":80"
}

type doorCache struct {
	mu   sync.Mutex
	addr string
	at   time.Time
}
