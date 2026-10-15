package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"k8s.io/apimachinery/pkg/api/resource"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
)

// Converting a database (storage plan, step 3): POST .../postgres/:name/convert
// {"to":"dedicated|shared","size":"...","storage":"..."} stamps the
// database with the kind asked for; the controller moves it under its
// name, and the apps bound to it keep their binding. The answer says what
// happens, so the CLI can confirm first.

// ConvertRequest is the body of a conversion.
type ConvertRequest struct {
	To      string `json:"to"`
	Size    string `json:"size,omitempty"`
	Storage string `json:"storage,omitempty"`
}

// ConvertView is the answer.
type ConvertView struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
	Note string `json:"note"`
}

func (s *Server) convertPostgres(c *gin.Context) {
	var req ConvertRequest
	if err := c.ShouldBindJSON(&req); err != nil || (req.To != shpyrdv1.PostgresTypeShared && req.To != shpyrdv1.PostgresTypeDedicated) {
		abort(c, http.StatusBadRequest, errors.New(`body must be {"to":"dedicated"} or {"to":"shared"} (a dedicated one may add "size" and "storage")`))
		return
	}
	pg, ok := s.postgresOf(c)
	if !ok {
		return
	}
	from := shpyrdv1.PostgresTypeDedicated
	if pg.Spec.Type == shpyrdv1.PostgresTypeShared {
		from = shpyrdv1.PostgresTypeShared
	}
	spec := map[string]interface{}{"type": req.To}
	if req.Storage != "" {
		spec["storage"] = req.Storage
	}
	cap, err := s.applyPostgresType(c.Request.Context(), s.workspace(c), spec)
	if err != nil {
		abort(c, http.StatusConflict, err)
		return
	}
	note, refusal := convertWords(pg, from, req, cap)
	if refusal != "" {
		abort(c, http.StatusConflict, errors.New(refusal))
		return
	}
	opts := controller.ConvertOptions{Size: req.Size, Cap: cap}
	if storage, _ := spec["storage"].(string); storage != "" {
		if q, err := resource.ParseQuantity(storage); err != nil || q.Sign() <= 0 {
			abort(c, http.StatusBadRequest, fmt.Errorf("storage %q: use e.g. 10Gi", storage))
			return
		}
		opts.Storage = storage
	}
	raw, _ := json.Marshal(opts)
	pg, err = s.mutatePostgres(c, func(pg *shpyrdv1.Postgres) error {
		if pg.Annotations == nil {
			pg.Annotations = map[string]string{}
		}
		pg.Annotations[controller.AnnotationConvert] = req.To
		pg.Annotations[controller.AnnotationConvertOptions] = string(raw)
		delete(pg.Annotations, controller.AnnotationConvertFailed)
		return nil
	})
	if err != nil {
		return
	}
	s.audit(c, c.Param("slug"), "postgres.convert", pg.Name, fmt.Sprintf("from %s to %s", from, req.To))
	c.JSON(http.StatusAccepted, ConvertView{Name: pg.Name, From: from, To: req.To, Note: note})
}

// convertWords says what the conversion does, or why it is refused.
func convertWords(pg *shpyrdv1.Postgres, from string, req ConvertRequest, cap string) (note, refusal string) {
	switch {
	case pg.Annotations[controller.AnnotationConvert] != "":
		return "", fmt.Sprintf("%s is being converted to a %s database already; wait for it.", pg.Name, pg.Annotations[controller.AnnotationConvert])
	case from == req.To:
		return "", fmt.Sprintf("%s is a %s database already.", pg.Name, from)
	case pg.Status.Phase != shpyrdv1.ResourceReady:
		return "", fmt.Sprintf("%s is %s; convert it once it is ready.", pg.Name, strings.ToLower(firstNonEmpty(pg.Status.Phase, "provisioning")))
	}
	var notes []string
	if req.To == shpyrdv1.PostgresTypeDedicated {
		disk := firstNonEmpty(req.Storage, "5Gi")
		notes = append(notes, fmt.Sprintf("%s becomes a cluster of its own with a %s disk, priced as a dedicated database", pg.Name, humanQuantity(disk)))
		notes = append(notes, "its user and database names stay; DATABASE_URL changes only its host, and the apps bound to it restart once")
	} else {
		notes = append(notes, fmt.Sprintf("%s moves onto the platform's shared server with a %s cap; its disk goes", pg.Name, humanQuantity(cap)))
		notes = append(notes, "it gets new user and database names, so the apps bound to it restart once with the new credentials")
		notes = append(notes, "only the extensions a shared database can hold may be installed (PostgreSQL's trusted ones and pgvector)")
		if v := pg.Spec.Version; v != "" && v != "17" {
			notes = append(notes, fmt.Sprintf("the shared server runs PostgreSQL 17, so this is a major upgrade from %s, done through a dump and a restore", v))
		}
		if pg.Spec.Instances != nil && *pg.Spec.Instances > 1 {
			notes = append(notes, "the shared server has two instances of its own; the database's own high-availability setting goes")
		}
	}
	notes = append(notes, "connections pause while the data is copied (a few minutes per gigabyte), and a copy of the source stays restorable for seven days")
	return strings.Join(notes, "; ") + ".", ""
}

// humanQuantity is a quantity in words (5Gi: 5 GB).
func humanQuantity(q string) string {
	v, err := resource.ParseQuantity(q)
	if err != nil {
		return q
	}
	b := v.Value()
	switch {
	case b >= 1<<30 && b%(1<<30) == 0:
		return fmt.Sprintf("%d GB", b>>30)
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%d MB", b>>20)
	}
	return q
}
