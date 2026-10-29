package api

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/shpyrd-io/shpyrd/internal/controller"
)

// hostRedirect answers what a redirect Ingress rewrote to the server (#133):
// a host that moved, or the platform host of an app that redirects to its
// own domain. The target is the one the platform wrote on the Ingress that
// serves the request's host, never one from the request, so nothing here
// sends a visitor anywhere the platform did not; the path and query are
// the visitor's, which nginx puts after RedirectPath.
func (s *Server) hostRedirect(c *gin.Context) {
	target := s.redirectTarget(c, hostOnly(c.Request.Host))
	if target == "" {
		c.Status(http.StatusNotFound)
		return
	}
	uri := (&url.URL{Path: "/" + strings.TrimPrefix(c.Param("path"), "/"), RawQuery: c.Request.URL.RawQuery}).RequestURI()
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusPermanentRedirect, target+uri)
}

// redirectTarget is where host redirects, "" when it does not. The index of
// the redirect Ingresses is kept for a few seconds, as the edge's is.
func (s *Server) redirectTarget(c *gin.Context, host string) string {
	s.redirects.mu.Lock()
	defer s.redirects.mu.Unlock()
	if s.redirects.to == nil || time.Since(s.redirects.at) >= 15*time.Second {
		var list networkingv1.IngressList
		if err := s.apps.List(c.Request.Context(), &list, client.MatchingLabels{controller.LabelRedirect: "true"}); err != nil {
			s.log.Warn("redirect: listing ingresses", "err", err)
			return ""
		}
		index := map[string]string{}
		for _, ing := range list.Items {
			target := ing.Annotations[controller.AnnotationRedirectTo]
			if !strings.HasPrefix(target, "https://") {
				continue
			}
			for _, rule := range ing.Spec.Rules {
				index[strings.ToLower(rule.Host)] = target
			}
		}
		s.redirects.to, s.redirects.at = index, time.Now()
	}
	return s.redirects.to[strings.ToLower(host)]
}

type redirectCache struct {
	mu sync.Mutex
	to map[string]string
	at time.Time
}
