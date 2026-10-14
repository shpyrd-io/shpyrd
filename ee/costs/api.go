//go:build !foss

package costs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The console's Costs and Cost Drains (with a license):
//
//	GET    /api/cluster/costs?from=&to=&kind=&group=   lines summed by group
//	GET    /api/cluster/cost-drains                    the drains
//	POST   /api/cluster/cost-drains                    {name, url, headers}
//	DELETE /api/cluster/cost-drains/:name
//	GET    /api/cluster/costs/oci                      whether the bill is read
//	PUT    /api/cluster/costs/oci                      {tenancy, user, fingerprint, region, key}
//	DELETE /api/cluster/costs/oci

type handlers struct {
	store     store.Store
	kube      kubernetes.Interface
	namespace string
}

type Row = store.CostGroupRow
type Summary = explorerSummary

var groups = map[string]bool{"workspace": true, "project": true, "workload": true, "process": true, "resourceType": true, "resource": true, "service": true}

func day(s string, def time.Time) (time.Time, error) {
	if s == "" {
		return def, nil
	}
	return time.Parse("2006-01-02", s)
}

// projectsByShortID is every project, deleted ones too, by the short id
// the lines carry.
func projectsByShortID(ctx context.Context, st store.Store) map[string]store.Project {
	out := map[string]store.Project{}
	all, err := st.ListWorkspaces(ctx)
	if err != nil {
		return out
	}
	for _, w := range all {
		projects, err := st.ListProjects(ctx, w.Slug, true)
		if err != nil {
			continue
		}
		for _, p := range projects {
			out[ids.Short(p.ID)] = p
		}
	}
	return out
}

var drainName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,38}[a-z0-9])?$`)

// NewDrain is POST /api/cluster/cost-drains.
type NewDrain struct {
	Name    string            `json:"name" binding:"required"`
	URL     string            `json:"url" binding:"required"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (h *handlers) listDrains(c *gin.Context) {
	list, err := h.store.ListCostDrains(c.Request.Context())
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// ValidateDrain checks a new drain: a name like a slug, an http(s) URL,
// header names a header may have.
func ValidateDrain(d NewDrain) error {
	if !drainName.MatchString(d.Name) {
		return errors.New("name: lowercase letters, digits and dashes, 40 at most")
	}
	u, err := url.Parse(d.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return errors.New("url: an http or https address")
	}
	for k := range d.Headers {
		if k == "" || strings.ContainsAny(k, " :\r\n") {
			return fmt.Errorf("header %q is not a header name", k)
		}
	}
	return nil
}

func (h *handlers) createDrain(c *gin.Context) {
	var req NewDrain
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := ValidateDrain(req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	names := make([]string, 0, len(req.Headers))
	for k := range req.Headers {
		names = append(names, k)
	}
	sort.Strings(names)
	// A drain starts from now: what changed before it existed is not sent.
	now := time.Now().UTC()
	d, err := h.store.CreateCostDrain(ctx, store.CostDrain{Name: req.Name, URL: req.URL, Headers: names, CursorAt: &now})
	if errors.Is(err, store.ErrConflict) {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "a cost drain with this name exists"})
		return
	}
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if len(req.Headers) > 0 && h.kube != nil {
		data := map[string][]byte{}
		for k, v := range req.Headers {
			data[k] = []byte(v)
		}
		sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: drainSecret(d.ID), Namespace: h.namespace, Labels: map[string]string{"app.kubernetes.io/managed-by": "shpyrd"}}, Data: data}
		if _, err := h.kube.CoreV1().Secrets(h.namespace).Create(ctx, sec, metav1.CreateOptions{}); err != nil {
			_ = h.store.DeleteCostDrain(ctx, d.Name)
			c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": "the headers' Secret: " + err.Error()})
			return
		}
	}
	c.JSON(http.StatusCreated, d)
}

func (h *handlers) deleteDrain(c *gin.Context) {
	ctx := c.Request.Context()
	name := c.Param("name")
	var id string
	if list, err := h.store.ListCostDrains(ctx); err == nil {
		for _, d := range list {
			if d.Name == name {
				id = d.ID
			}
		}
	}
	if err := h.store.DeleteCostDrain(ctx, name); errors.Is(err, store.ErrNotFound) {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "no cost drain " + name})
		return
	} else if err != nil {
		c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if id != "" && h.kube != nil {
		if err := h.kube.CoreV1().Secrets(h.namespace).Delete(ctx, drainSecret(id), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": "the headers' Secret: " + err.Error()})
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// OCISettings is PUT /api/cluster/costs/oci: an API key of an OCI user who
// may read usage and search resources.
type OCISettings struct {
	Tenancy     string `json:"tenancy" binding:"required"`
	User        string `json:"user" binding:"required"`
	Fingerprint string `json:"fingerprint" binding:"required"`
	Region      string `json:"region" binding:"required"`
	Key         string `json:"key" binding:"required"`
}

// OCIStatus is GET /api/cluster/costs/oci: never the key.
type OCIStatus struct {
	Configured bool   `json:"configured"`
	Tenancy    string `json:"tenancy,omitempty"`
	Region     string `json:"region,omitempty"`
}

func (h *handlers) ociStatus(c *gin.Context) {
	if h.kube == nil {
		c.JSON(http.StatusOK, OCIStatus{})
		return
	}
	sec, err := h.kube.CoreV1().Secrets(h.namespace).Get(c.Request.Context(), OCISecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		c.JSON(http.StatusOK, OCIStatus{})
		return
	}
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, OCIStatus{Configured: true, Tenancy: string(sec.Data["tenancy"]), Region: string(sec.Data["region"])})
}

func (h *handlers) putOCI(c *gin.Context) {
	var req OCISettings
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := parseRSAKey([]byte(req.Key)); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.kube == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "no cluster to keep the key in"})
		return
	}
	ctx := c.Request.Context()
	data := map[string][]byte{"tenancy": []byte(strings.TrimSpace(req.Tenancy)), "user": []byte(strings.TrimSpace(req.User)), "fingerprint": []byte(strings.TrimSpace(req.Fingerprint)), "region": []byte(strings.TrimSpace(req.Region)), "key": []byte(req.Key)}
	secrets := h.kube.CoreV1().Secrets(h.namespace)
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: OCISecretName, Namespace: h.namespace, Labels: map[string]string{"app.kubernetes.io/managed-by": "shpyrd"}}, Data: data}
	if _, err := secrets.Create(ctx, sec, metav1.CreateOptions{}); apierrors.IsAlreadyExists(err) {
		cur, gerr := secrets.Get(ctx, OCISecretName, metav1.GetOptions{})
		if gerr != nil {
			c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": gerr.Error()})
			return
		}
		cur.Data = data
		if _, err := secrets.Update(ctx, cur, metav1.UpdateOptions{}); err != nil {
			c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
	} else if err != nil {
		c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, OCIStatus{Configured: true, Tenancy: req.Tenancy, Region: req.Region})
}

func (h *handlers) deleteOCI(c *gin.Context) {
	if h.kube != nil {
		if err := h.kube.CoreV1().Secrets(h.namespace).Delete(c.Request.Context(), OCISecretName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
	}
	c.Status(http.StatusNoContent)
}
