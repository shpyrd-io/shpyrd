//go:build !foss

package costs

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/shpyrd-io/shpyrd/ee/licensing"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

var hour = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

func fixture(t *testing.T) map[string]allocation {
	t.Helper()
	raw, err := os.ReadFile("testdata/allocation.json")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Data []map[string]allocation `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out.Data[0]
}

func find(lines []store.CostLine, process, metric string) *store.CostLine {
	for i := range lines {
		if lines[i].Process == process && lines[i].Metric == metric {
			return &lines[i]
		}
	}
	return nil
}

// OpenCost's pods become lines by project and process, on the node's OCID;
// volumes are lines of their own, on the volume's; the platform's pods are
// lines with no project; each node's idle capacity is a line on the node.
func TestTheEstimateIsLinesByProjectProcessAndResource(t *testing.T) {
	res := resources{nodes: map[string]string{"worker-1": "ocid1.instance.oc1.iad.node1"}, volumes: map[string]string{"pvc-bbb": "ocid1.volume.oc1.iad.vol2"}}
	lines := estimateLines(fixture(t), hour, hour.Add(time.Hour), res)

	web := find(lines, "web", "cpu")
	if web == nil || web.Project != "47zzbjzdu14c07ueautsg6p8s" || web.Workspace != "5wt3dhu441tpry4dveibwh82o" || *web.Quantity != 0.25 || *web.Cost != 0.008 || web.Unit != "core-hours" || web.Resource != "ocid1.instance.oc1.iad.node1" || web.ResourceType != "node" || web.Currency != "USD" || web.Kind != store.CostEstimated {
		t.Errorf("web cpu = %+v", web)
	}
	if mem := find(lines, "web", "memory"); mem == nil || *mem.Quantity != 0.5 || mem.Unit != "GiB-hours" {
		t.Errorf("web memory = %+v", mem)
	}
	if vol := find(lines, "postgres/db", "storage"); vol == nil || vol.Resource != "ocid1.volume.oc1.iad.vol1" || vol.ResourceType != "volume" || *vol.Quantity != 10 {
		t.Errorf("the database's volume = %+v", vol)
	}
	if vol := find(lines, "volumes", "storage"); vol == nil || vol.Resource != "ocid1.volume.oc1.iad.vol2" || vol.Project == "" {
		t.Errorf("an unmounted volume, found by its PersistentVolume = %+v", vol)
	}
	if dns := find(lines, "kube-system", "cpu"); dns == nil || dns.Project != "" || dns.Resource != "ocid1.instance.oc1.iad.node1" {
		t.Errorf("the platform's pod = %+v", dns)
	}
	if idle := find(lines, "idle", "cpu"); idle == nil || idle.Project != "" || idle.Resource != "ocid1.instance.oc1.iad.node1" || *idle.Cost != 0.05 {
		t.Errorf("the node's idle capacity = %+v", idle)
	}
	// What costs nothing and uses nothing is no line.
	for _, l := range lines {
		if (l.Quantity == nil || *l.Quantity == 0) && (l.Cost == nil || *l.Cost == 0) {
			t.Errorf("an empty line: %+v", l)
		}
	}
}

func TestSummarizeSumsByGroupBiggestFirst(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	lines := []store.CostLine{
		{Project: "a", Process: "web", Cost: f(1), Currency: "USD"},
		{Project: "a", Process: "worker", Cost: f(2), Currency: "USD"},
		{Project: "b", Process: "web", Cost: f(5), Currency: "USD"},
		{Project: "", Process: "kube-system", Cost: f(0.5), Currency: "USD"},
	}
	rows, total := Summarize(lines, "project", map[string]string{"a": "shop", "b": "blog"})
	if len(rows) != 3 || rows[0].Slug != "blog" || rows[0].Cost["USD"] != 5 || rows[1].Slug != "shop" || rows[1].Cost["USD"] != 3 || rows[1].Lines != 2 || total["USD"] != 8.5 {
		t.Errorf("by project = %+v %v", rows, total)
	}
	if rows, _ := Summarize(lines, "process", nil); len(rows) != 4 {
		t.Errorf("by process = %+v", rows)
	}
}

func withLicense(t *testing.T) {
	t.Helper()
	licensing.Unlock()
	t.Cleanup(licensing.ResetForTest)
}

// The collector writes the hours just closed: the usage measured and
// OpenCost's estimate, on the store's workspace ids; without a license it
// writes nothing.
func TestTheCollectorWritesTheClosedHours(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	ws, _ := st.Workspace(ctx, store.DefaultWorkspace)
	q := 1800.0
	if err := st.WriteBuckets(ctx, []store.UsageBucket{{WorkspaceID: store.DefaultWorkspace, Project: "47zzbjzdu14c07ueautsg6p8s", Component: "web", Metric: store.MetricCPUUsed, PeriodStart: hour, PeriodEnd: hour.Add(5 * time.Minute), Quantity: &q, Unit: store.UnitCoreSeconds, Quality: store.QualityComplete, Revision: 1}}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile("testdata/allocation.json")
	// The fixture's workspace label is this store's default workspace.
	raw = []byte(strings.ReplaceAll(string(raw), "5wt3dhu441tpry4dveibwh82o", ids.Short(ws.ID)))
	var windows []string
	var mu sync.Mutex
	oc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		windows = append(windows, r.URL.Query().Get("window"))
		mu.Unlock()
		if r.URL.Query().Get("aggregate") != "pod" || r.URL.Query().Get("idleByNode") != "true" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write(raw)
	}))
	defer oc.Close()
	kube := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-1"}, Spec: corev1.NodeSpec{ProviderID: "ocid1.instance.oc1.iad.node1"}})
	c := &Collector{Store: st, Kube: kube, Namespace: "shpyrd-system", OpenCostURL: oc.URL, HoursBack: 1, now: func() time.Time { return hour.Add(65 * time.Minute) }}

	c.Collect(ctx)
	if len(windows) != 0 {
		t.Fatal("collected without a license")
	}
	withLicense(t)
	c.Collect(ctx)
	if len(windows) != 1 || windows[0] != "2026-10-04T12:00:00Z,2026-10-04T13:00:00Z" {
		t.Fatalf("windows = %v", windows)
	}
	usage, _ := st.QueryCostLines(ctx, store.CostQuery{From: hour, To: hour.Add(time.Hour), Kind: store.CostUsage})
	if len(usage) != 1 || usage[0].Workspace != ws.ID || *usage[0].Quantity != 1800 || usage[0].Unit != store.UnitCoreSeconds || usage[0].Process != "web" {
		t.Errorf("usage = %+v", usage)
	}
	est, _ := st.QueryCostLines(ctx, store.CostQuery{From: hour, To: hour.Add(time.Hour), Kind: store.CostEstimated})
	web := find(est, "web", "cpu")
	if web == nil || web.Workspace != ws.ID || web.Resource != "ocid1.instance.oc1.iad.node1" {
		t.Errorf("estimate = %+v", web)
	}
	// Read again, the same numbers change nothing.
	before, _ := st.CostLinesChangedSince(ctx, time.Time{}, "", 1000)
	c.Collect(ctx)
	after, _ := st.CostLinesChangedSince(ctx, time.Time{}, "", 1000)
	if len(before) != len(after) || before[len(before)-1].ChangedAt != after[len(after)-1].ChangedAt {
		t.Errorf("a re-read changed lines: %d %d", len(before), len(after))
	}
}

// A drain gets the lines that changed since its cursor, with its headers,
// and its cursor moves; a receiver that fails is counted and the lines go
// again.
func TestADrainSendsWhatChanged(t *testing.T) {
	withLicense(t)
	ctx := context.Background()
	st := store.NewMemory()
	var got []Payload
	var auth []string
	fail := false
	var mu sync.Mutex
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			http.Error(w, "down for a moment", http.StatusServiceUnavailable)
			return
		}
		var p Payload
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &p); err != nil {
			t.Errorf("body: %v", err)
		}
		got = append(got, p)
		auth = append(auth, r.Header.Get("Authorization"))
	}))
	defer recv.Close()
	d, err := st.CreateCostDrain(ctx, store.CostDrain{Name: "finance", URL: recv.URL, Headers: []string{"Authorization"}})
	if err != nil {
		t.Fatal(err)
	}
	kube := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: drainSecret(d.ID), Namespace: "shpyrd-system"}, Data: map[string][]byte{"Authorization": []byte("Bearer s3cret")}})
	s := &Sender{Store: st, Kube: kube, Namespace: "shpyrd-system", Cluster: "prod"}
	f := func(v float64) *float64 { return &v }
	line := store.CostLine{Kind: store.CostEstimated, Source: "opencost", Start: hour, End: hour.Add(time.Hour), Project: "p1", Process: "web", Metric: "cpu", Quantity: f(1), Cost: f(0.03), Currency: "USD", Resource: "ocid1.instance.a"}
	if _, err := st.UpsertCostLines(ctx, []store.CostLine{line}); err != nil {
		t.Fatal(err)
	}

	s.Send(ctx)
	if len(got) != 1 || len(got[0].Lines) != 1 || got[0].Cluster != "prod" || got[0].Lines[0].ID == "" || got[0].Lines[0].Resource != "ocid1.instance.a" || auth[0] != "Bearer s3cret" {
		t.Fatalf("first delivery = %+v %v", got, auth)
	}
	s.Send(ctx)
	if len(got) != 1 {
		t.Fatalf("nothing changed, yet %d deliveries", len(got))
	}
	// The receiver fails: counted, and the line goes again once it is back.
	line.Cost = f(0.04)
	_, _ = st.UpsertCostLines(ctx, []store.CostLine{line})
	fail = true
	s.Send(ctx)
	drains, _ := st.ListCostDrains(ctx)
	if drains[0].Errors != 1 || !strings.Contains(drains[0].Message, "503") || drains[0].Sent != 1 {
		t.Fatalf("after a failure = %+v", drains[0])
	}
	fail = false
	s.Send(ctx)
	if len(got) != 2 || *got[1].Lines[0].Cost != 0.04 || got[1].Lines[0].ID != got[0].Lines[0].ID {
		t.Fatalf("the revised line again = %+v", got)
	}
	if drains, _ := st.ListCostDrains(ctx); drains[0].Sent != 2 || drains[0].Message != "" {
		t.Errorf("after recovery = %+v", drains[0])
	}
}

func testRSAKey(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
}

// verifyOCISignature checks a request's signature the way OCI does.
func verifyOCISignature(t *testing.T, r *http.Request, body []byte, pub *rsa.PublicKey) {
	t.Helper()
	auth := r.Header.Get("Authorization")
	field := func(name string) string {
		i := strings.Index(auth, name+`="`)
		if i < 0 {
			t.Fatalf("no %s in %s", name, auth)
		}
		rest := auth[i+len(name)+2:]
		return rest[:strings.Index(rest, `"`)]
	}
	if !strings.HasPrefix(auth, `Signature version="1"`) || field("algorithm") != "rsa-sha256" || field("keyId") != "ocid1.tenancy.t/ocid1.user.u/aa:bb" {
		t.Errorf("authorization = %s", auth)
	}
	sum := sha256.Sum256(body)
	if r.Header.Get("X-Content-Sha256") != base64.StdEncoding.EncodeToString(sum[:]) {
		t.Errorf("x-content-sha256 = %s", r.Header.Get("X-Content-Sha256"))
	}
	var lines []string
	for _, h := range strings.Fields(field("headers")) {
		switch h {
		case "(request-target)":
			lines = append(lines, "(request-target): "+strings.ToLower(r.Method)+" "+r.URL.RequestURI())
		case "host":
			lines = append(lines, "host: "+r.Host)
		default:
			lines = append(lines, h+": "+r.Header.Get(h))
		}
	}
	sig, _ := base64.StdEncoding.DecodeString(field("signature"))
	digest := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
		t.Errorf("the signature does not verify: %v", err)
	}
}

// The bill: the Usage API's items, page after page, each with the tags the
// Search API knows of its resource; every request signed.
func TestTheBillIsReadFromOCI(t *testing.T) {
	withLicense(t)
	ctx := context.Background()
	key, keyPEM := testRSAKey(t)
	var usagePages, searchPages int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		verifyOCISignature(t, r, body, &key.PublicKey)
		switch r.URL.Path {
		case "/usage":
			usagePages++
			var q map[string]any
			_ = json.Unmarshal(body, &q)
			if q["granularity"] != "DAILY" || q["tenantId"] != "ocid1.tenancy.t" || q["timeUsageEnded"] != "2026-10-04T00:00:00Z" || q["timeUsageStarted"] != "2026-10-01T00:00:00Z" {
				t.Errorf("usage query = %v", q)
			}
			if r.URL.Query().Get("page") == "" {
				w.Header().Set("opc-next-page", "p2")
				_, _ = w.Write([]byte(`{"items":[{"resourceId":"ocid1.instance.oc1.iad.node1","service":"Compute","skuName":"Standard - E5","unit":"OCPU Hours","computedAmount":18.85,"computedQuantity":24,"currency":"BRL","timeUsageStarted":"2026-10-01T00:00:00Z","timeUsageEnded":"2026-10-02T00:00:00Z"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[{"resourceId":"ocid1.volume.oc1.iad.vol1","service":"Block Storage","skuName":"Block Volume - Storage","unit":"GB Months","computedAmount":0.86,"computedQuantity":1.6,"currency":"BRL","timeUsageStarted":"2026-10-01T00:00:00Z","timeUsageEnded":"2026-10-02T00:00:00Z"}]}`))
		case "/search":
			searchPages++
			_, _ = w.Write([]byte(`{"items":[{"identifier":"ocid1.instance.oc1.iad.node1","resourceType":"Instance","definedTags":{"orcl-containerengine":{"NodePool":"ocid1.nodepool.x","Cluster":"ocid1.cluster.y"}},"freeformTags":{"shpyrd-platform":"shpyrd-prod"}}]}`))
		default:
			t.Errorf("path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	st := store.NewMemory()
	kube := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: OCISecretName, Namespace: "shpyrd-system"}, Data: map[string][]byte{
		"tenancy": []byte("ocid1.tenancy.t"), "user": []byte("ocid1.user.u"), "fingerprint": []byte("aa:bb"), "region": []byte("us-ashburn-1"), "key": keyPEM,
	}})
	c := &Collector{Store: st, Kube: kube, Namespace: "shpyrd-system", ociEndpoints: ociEndpoints{usage: srv.URL + "/usage", search: srv.URL + "/search"}}
	if err := c.collectBill(ctx, time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if usagePages != 2 || searchPages != 1 {
		t.Errorf("pages: usage %d, search %d", usagePages, searchPages)
	}
	lines, _ := st.QueryCostLines(ctx, store.CostQuery{From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), Kind: store.CostReal})
	if len(lines) != 2 {
		t.Fatalf("real lines = %+v", lines)
	}
	var node store.CostLine
	for _, l := range lines {
		if l.Resource == "ocid1.instance.oc1.iad.node1" {
			node = l
		}
	}
	if node.Service != "Compute" || node.SKU != "Standard - E5" || *node.Cost != 18.85 || node.Currency != "BRL" || node.ResourceType != "Instance" || node.Tags["orcl-containerengine.NodePool"] != "ocid1.nodepool.x" || node.Tags["shpyrd-platform"] != "shpyrd-prod" {
		t.Errorf("node line = %+v", node)
	}
	// No Secret: no bill, and no error to log.
	c.Kube = fake.NewSimpleClientset()
	if err := c.collectBill(ctx, time.Now()); err != errNoCredentials {
		t.Errorf("without credentials: %v", err)
	}
}

// The console's routes answer 402 without a license; with one, drains are
// made, listed and removed, their headers kept in a Secret, never shown.
func TestTheRoutesNeedALicense(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(licensing.ResetForTest)
	st := store.NewMemory()
	kube := fake.NewSimpleClientset()
	e := gin.New()
	if err := New().Routes(router{e}, ext.Deps{Store: st, SystemNamespace: "shpyrd-system", Kube: nil}); err != nil {
		t.Fatal(err)
	}
	h := &handlers{store: st, kube: kube, namespace: "shpyrd-system"}
	e2 := gin.New()
	g := e2.Group("/cluster", licensing.Require())
	g.POST("/cost-drains", h.createDrain)
	g.GET("/cost-drains", h.listDrains)
	g.DELETE("/cost-drains/:name", h.deleteDrain)
	g.GET("/costs", h.summary)
	call := func(e *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		e.ServeHTTP(w, r)
		return w
	}
	if w := call(e, "GET", "/cluster/costs", ""); w.Code != http.StatusPaymentRequired {
		t.Fatalf("without a license = %d", w.Code)
	}
	licensing.Unlock()
	w := call(e2, "POST", "/cluster/cost-drains", `{"name":"finance","url":"https://finance.example.test/costs","headers":{"Authorization":"Bearer s3cret"}}`)
	if w.Code != http.StatusCreated || strings.Contains(w.Body.String(), "s3cret") {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	var d store.CostDrain
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	if sec, err := kube.CoreV1().Secrets("shpyrd-system").Get(context.Background(), drainSecret(d.ID), metav1.GetOptions{}); err != nil || string(sec.Data["Authorization"]) != "Bearer s3cret" {
		t.Errorf("the headers' Secret: %v", err)
	}
	for _, bad := range []string{`{"name":"Bad Name","url":"https://x.test"}`, `{"name":"x","url":"ftp://x.test"}`, `{"name":"finance","url":"https://x.test"}`} {
		if w := call(e2, "POST", "/cluster/cost-drains", bad); w.Code != http.StatusBadRequest && w.Code != http.StatusConflict {
			t.Errorf("%s = %d", bad, w.Code)
		}
	}
	if w := call(e2, "GET", "/cluster/costs?group=nope", ""); w.Code != http.StatusBadRequest {
		t.Errorf("bad group = %d", w.Code)
	}
	if w := call(e2, "GET", "/cluster/costs?from=2026-10-01&to=2026-11-01&group=process", ""); w.Code != http.StatusOK {
		t.Errorf("summary = %d %s", w.Code, w.Body)
	}
	if w := call(e2, "DELETE", "/cluster/cost-drains/finance", ""); w.Code != http.StatusNoContent {
		t.Errorf("delete = %d", w.Code)
	}
	if _, err := kube.CoreV1().Secrets("shpyrd-system").Get(context.Background(), drainSecret(d.ID), metav1.GetOptions{}); err == nil {
		t.Error("the headers' Secret stayed")
	}
}

type router struct{ e *gin.Engine }

func (r router) Public() gin.IRouter         { return r.e }
func (r router) Protected() gin.IRouter      { return r.e }
func (r router) WorkspaceAdmin() gin.IRouter { return r.e }
func (r router) Admin() gin.IRouter          { return r.e }
func (r router) Platform() gin.IRouter       { return r.e }
