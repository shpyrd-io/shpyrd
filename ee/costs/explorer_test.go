//go:build !foss

package costs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

func TestExplorerValidatesFiltersAndExportsAllPages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := store.NewMemory()
	h := &handlers{store: st}
	e := gin.New()
	e.GET("/costs", h.summary)
	e.POST("/costs/exports", h.createExport)
	e.GET("/costs/exports/:id", h.exportStatus)
	e.GET("/costs/exports/:id/download", h.downloadExport)
	request := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	for _, q := range []string{"limit=0", "limit=201", "offset=-1", "offset=oops", "sort=invalid", "group=unknown", "kind=unknown", "category=platform", "from=2025-01-01&to=2026-10-01", "from=2026-10-02&to=2026-10-01"} {
		if w := request("GET", "/costs?"+q); w.Code != 400 {
			t.Errorf("%s: %d", q, w.Code)
		}
	}
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	amount := 1.0
	lines := []store.CostLine{}
	for i := 0; i < 210; i++ {
		lines = append(lines, store.CostLine{Kind: store.CostEstimated, Source: "opencost", Start: day, End: day.Add(time.Hour), Project: strconv.Itoa(i), Process: "web", Cost: &amount, Currency: "USD"})
	}
	if _, err := st.UpsertCostLines(context.Background(), lines); err != nil {
		t.Fatal(err)
	}
	q := "?from=2026-10-01&to=2026-10-02&limit=1&offset=209"
	w := request("GET", "/costs"+q)
	var summary explorerSummary
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || summary.Count != 210 || len(summary.Rows) != 1 || summary.Total["USD"] != 210 || !summary.Incomplete || summary.Collection.CollectedAt != nil {
		t.Fatalf("summary: %d %+v", w.Code, summary)
	}
	w = request("POST", "/costs/exports"+q)
	var job store.CostExport
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil || w.Code != http.StatusAccepted {
		t.Fatalf("create export: %d %s %v", w.Code, w.Body, err)
	}
	if w = request("GET", "/costs/exports/"+job.ID+"/download"); w.Code != 409 {
		t.Fatalf("unready download: %d", w.Code)
	}
	if ok, err := st.BuildCostExport(context.Background()); !ok || err != nil {
		t.Fatalf("build export: %v %v", ok, err)
	}
	w = request("GET", "/costs/exports/"+job.ID+"/download")
	if w.Code != 200 || strings.Count(w.Body.String(), "\n") != 211 || w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) {
		t.Fatalf("download: status %d rows %d size %s/%d", w.Code, strings.Count(w.Body.String(), "\n"), w.Header().Get("Content-Length"), w.Body.Len())
	}
	if w = request("GET", "/costs/exports/missing"); w.Code != 404 {
		t.Fatalf("missing export: %d", w.Code)
	}
}

func TestFailedEstimateCollectionDoesNotAdvanceFreshness(t *testing.T) {
	withLicense(t)
	ctx := context.Background()
	st := store.NewMemory()
	before := hour.Add(-time.Hour)
	if err := st.RecordCostCollection(ctx, store.CostEstimated, before, before); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	c := &Collector{Store: st, OpenCostURL: server.URL, HoursBack: 1, now: func() time.Time { return hour.Add(65 * time.Minute) }}
	c.Collect(ctx)
	status, err := st.CostCollection(ctx, store.CostEstimated)
	if err != nil || status.Through == nil || !status.Through.Equal(before) {
		t.Fatalf("failed source advanced: %+v %v", status, err)
	}
}
