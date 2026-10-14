package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/configvars"
	"github.com/shpyrd-io/shpyrd/pkg/project"
)

// ConfigVarsResponse exposes only explicitly plain values. Secrets remain names only.
type ConfigVarsResponse struct {
	Vars      []configvars.Var `json:"vars"`
	Variables []configvars.Var `json:"variables"`
	Pending   bool             `json:"pending"`
	// Bound are read-only variables provided by attached resources
	// (RFC-0003); they win over Vars with the same name.
	Bound []BoundVar `json:"bound,omitempty"`
	// Global are read-only variables provided by the cluster (RFC-0016); a
	// project var of the same name overrides them.
	Global []configvars.Var `json:"global,omitempty"`
}

// ConfigVarsUpdate sets and/or unsets config vars. Dotenv is parsed as
// KEY=VALUE lines and merged into Set (bulk paste).
type ConfigVarsUpdate struct {
	Kind         string            `json:"kind,omitempty"`
	Convert      []string          `json:"convert,omitempty"`
	ConfirmPlain bool              `json:"confirmPlain,omitempty"`
	Set          map[string]string `json:"set,omitempty"`
	Unset        []string          `json:"unset,omitempty"`
	Dotenv       string            `json:"dotenv,omitempty"`
}

// BoundVar is a config var injected by an attached resource.
type BoundVar struct {
	Name     string `json:"name"`
	Provider string `json:"provider"` // "<Kind>/<name>"
}

func (s *Server) appSecretKeys(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	sec, pending, err := configvars.Read(c.Request.Context(), s.apps, app)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	resp := configResponse(sec, pending)
	resp.Bound = s.boundVars(c.Request.Context(), app)
	resp.Global = s.globalVars(c.Request.Context(), app)
	c.JSON(http.StatusOK, resp)
}

// boundVars lists the variables of <app>-bindings with their providers.
func (s *Server) boundVars(ctx context.Context, app *shpyrdv1.App) []BoundVar {
	sec := &corev1.Secret{}
	if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: app.BindingsSecretName()}, sec); err != nil {
		return nil
	}
	providers := map[string]string{}
	_ = json.Unmarshal([]byte(sec.Annotations[shpyrdv1.AnnotationBindingProviders]), &providers)
	out := make([]BoundVar, 0, len(sec.Data))
	for k := range sec.Data {
		out = append(out, BoundVar{Name: k, Provider: providers[k]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Server) updateAppSecrets(c *gin.Context) {
	var req ConfigVarsUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	if req.Dotenv != "" {
		parsed, err := configvars.ParseDotenv(req.Dotenv)
		if err != nil {
			abort(c, http.StatusBadRequest, err)
			return
		}
		if req.Set == nil {
			req.Set = map[string]string{}
		}
		for k, v := range parsed {
			req.Set[k] = v
		}
	}
	if req.Kind != "" && req.Kind != "secret" && req.Kind != "variable" {
		abort(c, http.StatusBadRequest, errors.New("kind must be secret or variable"))
		return
	}
	if len(req.Set) == 0 && len(req.Unset) == 0 && len(req.Convert) == 0 {
		abort(c, http.StatusBadRequest, errors.New("nothing to change"))
		return
	}
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	key := types.NamespacedName{Namespace: app.Namespace, Name: configvars.PendingName(app)}
	for attempt := 0; attempt < 5; attempt++ {
		sec, pending, err := configvars.Read(ctx, s.apps, app)
		if err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
		create := !pending
		if create {
			data, annotations := sec.Data, sec.Annotations
			sec = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace, Labels: map[string]string{shpyrdv1.LabelApp: app.Name}, Annotations: annotations}, Type: corev1.SecretTypeOpaque, Data: data}
		}
		if err := configvars.ApplyKind(sec, req.Set, req.Unset, req.Convert, req.Kind == "variable", req.ConfirmPlain, time.Now()); err != nil {
			abort(c, http.StatusBadRequest, err)
			return
		}
		if sec.Annotations == nil {
			sec.Annotations = map[string]string{}
		}
		sec.Annotations[configvars.AnnotationPendingBase] = configvars.DeployToken(app)
		if create {
			err = s.apps.Create(ctx, sec)
		} else {
			err = s.apps.Update(ctx, sec)
		}
		if err == nil {
			s.audit(c, project.SlugOf(app), "config.set", project.SlugOf(app), configChangeDetail(req))
			c.JSON(http.StatusOK, configResponse(sec, true))
			return
		}
		if !apierrors.IsConflict(err) && !apierrors.IsAlreadyExists(err) && !apierrors.IsNotFound(err) {
			abort(c, http.StatusBadGateway, err)
			return
		}
	}
	abort(c, http.StatusConflict, errors.New("too many conflicts"))
}

// configChangeDetail names the variables touched, never their values.
func configChangeDetail(req ConfigVarsUpdate) string {
	var parts []string
	keys := make([]string, 0, len(req.Set))
	for k := range req.Set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		parts = append(parts, "set "+strings.Join(keys, ", "))
	}
	if len(req.Unset) > 0 {
		parts = append(parts, "unset "+strings.Join(req.Unset, ", "))
	}
	if len(req.Convert) > 0 {
		parts = append(parts, "convert to "+req.Kind+" "+strings.Join(req.Convert, ", "))
	}
	if req.Dotenv != "" {
		parts = append(parts, "bulk update")
	}
	return strings.Join(parts, "; ")
}

func configResponse(sec *corev1.Secret, pending bool) ConfigVarsResponse {
	return ConfigVarsResponse{Vars: configvars.Secrets(sec), Variables: configvars.Public(sec), Pending: pending}
}
