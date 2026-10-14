//go:build !foss

package costs

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

type explorerSummary struct {
	store.CostPage
	From       time.Time                  `json:"from"`
	To         time.Time                  `json:"to"`
	Kind       string                     `json:"kind"`
	Group      string                     `json:"group"`
	Collection store.CostCollectionStatus `json:"collection"`
	Incomplete bool                       `json:"incomplete"`
	Workspaces []costWorkspace            `json:"workspaces"`
}
type costWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (h *handlers) explorerQuery(c *gin.Context) (store.CostExploreQuery, []costWorkspace, error) {
	now := time.Now().UTC()
	from, e1 := day(c.Query("from"), time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
	to, e2 := day(c.Query("to"), now.Truncate(24*time.Hour).Add(24*time.Hour))
	q := store.CostExploreQuery{From: from, To: to, Kind: firstNonEmpty(c.Query("kind"), store.CostEstimated), Group: firstNonEmpty(c.Query("group"), "project"),
		Workspace: c.Query("workspace"), Project: c.Query("project"), Category: c.Query("category"), Workload: c.Query("workload"), Process: c.Query("process"), ResourceType: c.Query("resourceType"), Resource: c.Query("resource"), Service: c.Query("service"), Search: strings.TrimSpace(c.Query("search")), Currency: c.Query("currency"), Sort: firstNonEmpty(c.Query("sort"), "cost_desc"), Limit: 50}
	if e1 != nil || e2 != nil || !to.After(from) || to.Sub(from) > 366*24*time.Hour {
		return q, nil, fmt.Errorf("choose a date range of 1–366 days; to is exclusive")
	}
	if !groups[q.Group] {
		return q, nil, fmt.Errorf("unknown cost grouping")
	}
	if q.Kind != store.CostEstimated && q.Kind != store.CostReal && q.Kind != store.CostUsage {
		return q, nil, fmt.Errorf("unknown cost source")
	}
	if q.Sort != "name" && q.Sort != "cost_asc" && q.Sort != "cost_desc" {
		return q, nil, fmt.Errorf("unknown sort order")
	}
	if q.Category != "" && q.Category != "project" && q.Category != "system" && q.Category != "idle" && q.Category != "unattributed" {
		return q, nil, fmt.Errorf("unknown cost category")
	}
	if len(q.Search) > 200 {
		return q, nil, fmt.Errorf("search must be 200 characters or fewer")
	}
	var err error
	if s := c.Query("limit"); s != "" {
		q.Limit, err = strconv.Atoi(s)
		if err != nil || q.Limit < 1 || q.Limit > 200 {
			return q, nil, fmt.Errorf("limit must be 1–200")
		}
	}
	if s := c.Query("offset"); s != "" {
		q.Offset, err = strconv.Atoi(s)
		if err != nil || q.Offset < 0 || q.Offset > 10000000 {
			return q, nil, fmt.Errorf("invalid offset")
		}
	}
	q.Names = store.CostNames{Workspaces: map[string]string{}, Projects: map[string]string{}}
	ws, err := h.store.ListWorkspaces(c.Request.Context())
	if err != nil {
		return q, nil, err
	}
	choices := []costWorkspace{}
	for _, w := range ws {
		q.Names.Workspaces[w.ID] = w.Name + " (" + w.Slug + ")"
		choices = append(choices, costWorkspace{ID: w.ID, Name: q.Names.Workspaces[w.ID]})
		if q.Workspace == w.Slug {
			q.Workspace = w.ID
		}
	}
	return q, choices, nil
}
func (h *handlers) summary(c *gin.Context) {
	q, ws, err := h.explorerQuery(c)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	page, err := h.store.ExploreCosts(c.Request.Context(), q)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	status, err := h.store.CostCollection(c.Request.Context(), q.Kind)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, explorerSummary{CostPage: page, From: q.From, To: q.To, Kind: q.Kind, Group: q.Group, Collection: status, Incomplete: status.Through == nil || status.Through.Before(q.To), Workspaces: ws})
}
