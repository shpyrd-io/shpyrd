package api

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/uipkg"
)

// Registry credentials (RFC-0084): a workspace's read tokens for the
// registries its application is pulled from, one per host, in the system
// namespace labelled with the workspace, as Git tokens are (RFC-0017).
// Values are written, never returned.

type RegistryTokenView struct {
	Host      string    `json:"host"`
	Username  string    `json:"username"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type RegistryTokenRequest struct {
	Username string `json:"username" binding:"required"`
	Token    string `json:"token" binding:"required"`
}

var errRegistryHost = errors.New("give a registry host such as ghcr.io or registry.example.com:5000")

func registryHostParam(c *gin.Context) (string, bool) {
	host := strings.ToLower(c.Param("host"))
	if !uipkg.HostRe.MatchString(host) {
		abort(c, http.StatusBadRequest, errRegistryHost)
		return "", false
	}
	return host, true
}

func registryTokenView(sec *corev1.Secret) RegistryTokenView {
	return RegistryTokenView{
		Host:      sec.Annotations[uipkg.AnnotationHost],
		Username:  string(sec.Data[uipkg.KeyUsername]),
		UpdatedBy: sec.Annotations[uipkg.AnnotationUpdatedBy],
		UpdatedAt: sec.CreationTimestamp.Time,
	}
}

func (s *Server) listRegistryTokens(c *gin.Context) {
	var list corev1.SecretList
	if err := s.apps.List(c.Request.Context(), &list, client.InNamespace(s.kube.Namespace),
		client.MatchingLabels{uipkg.LabelCredential: uipkg.KindToken, shpyrdv1.LabelWorkspace: s.doorWorkspace(c)}); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	out := make([]RegistryTokenView, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, registryTokenView(&list.Items[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	c.JSON(http.StatusOK, out)
}

func (s *Server) putRegistryToken(c *gin.Context) {
	host, ok := registryHostParam(c)
	if !ok {
		return
	}
	var req RegistryTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, errors.New("give the username and the token"))
		return
	}
	ws := s.doorWorkspace(c)
	ctx := c.Request.Context()
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: uipkg.SecretName(ws, host), Namespace: s.kube.Namespace}}
	data := map[string][]byte{uipkg.KeyUsername: []byte(strings.TrimSpace(req.Username)), uipkg.KeyToken: []byte(strings.TrimSpace(req.Token))}
	err := s.apps.Get(ctx, client.ObjectKeyFromObject(sec), sec)
	switch {
	case apierrors.IsNotFound(err):
		sec.Labels = map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", uipkg.LabelCredential: uipkg.KindToken, shpyrdv1.LabelWorkspace: ws}
		sec.Annotations = map[string]string{uipkg.AnnotationHost: host, uipkg.AnnotationUpdatedBy: actorOf(c)}
		sec.Type = corev1.SecretTypeOpaque
		sec.Data = data
		err = s.apps.Create(ctx, sec)
	case err == nil:
		sec.Annotations = mergeStringMaps(sec.Annotations, map[string]string{uipkg.AnnotationUpdatedBy: actorOf(c)})
		sec.Data = data
		err = s.apps.Update(ctx, sec)
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	// A reference on this host may now pull: let the next request try.
	if w, err := s.store.Workspace(ctx, ws); err == nil && w.Settings.UI != nil {
		s.chosenUI().forget(w.Settings.UI.Ref)
	}
	s.audit(c, "", "registry-auth.set", host, "token for "+host)
	c.JSON(http.StatusOK, registryTokenView(sec))
}

func (s *Server) deleteRegistryToken(c *gin.Context) {
	host, ok := registryHostParam(c)
	if !ok {
		return
	}
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: uipkg.SecretName(s.doorWorkspace(c), host), Namespace: s.kube.Namespace}}
	if err := s.apps.Delete(c.Request.Context(), sec); err != nil && !apierrors.IsNotFound(err) {
		abort(c, http.StatusBadGateway, err)
		return
	}
	s.audit(c, "", "registry-auth.unset", host, "token for "+host)
	c.Status(http.StatusNoContent)
}
