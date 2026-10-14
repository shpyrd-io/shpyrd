package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/install"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type noInventoryClient struct {
	client.Client
	lists atomic.Int32
}

func (c *noInventoryClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	c.lists.Add(1)
	return fmt.Errorf("unexpected tenant inventory list")
}
func TestOverviewNeverListsTenantProjectsNamespacesOrAllPods(t *testing.T) {
	record := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: install.InstallRecordName, Namespace: install.DefaultSystemNamespace}, Data: map[string]string{"profile": "local", "vars": "SHPYRD_PLATFORM_POOL: platform\n", "component.cert-manager": "name: cert-manager\n"}}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "apps-1", Labels: map[string]string{install.PoolLabel: "apps"}}}
	s, _ := newTestServer(t, nil, nil, node, record)
	guard := &noInventoryClient{Client: s.apps}
	s.apps = guard
	kube := s.kube.Kube.(*kubefake.Clientset)
	var nodeLists atomic.Int32
	kube.PrependReactor("list", "*", func(a ktesting.Action) (bool, runtime.Object, error) {
		switch a.GetResource().Resource {
		case "namespaces":
			t.Error("listed project namespaces")
		case "pods":
			if a.GetNamespace() == "" {
				t.Error("listed all cluster pods")
			}
		case "nodes":
			nodeLists.Add(1)
			if a.(interface{ GetListOptions() metav1.ListOptions }).GetListOptions().Limit != 500 {
				t.Error("node list was not chunked")
			}
		}
		return false, nil, nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.overviewSnapshot(context.Background(), ""); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if guard.lists.Load() != 0 || nodeLists.Load() != 1 {
		t.Fatalf("projects=%d node scans=%d", guard.lists.Load(), nodeLists.Load())
	}
}
func TestOverviewWeightsOnlyReportingNodesAndSeparatesPools(t *testing.T) {
	rows := []overviewNode{
		{NodeInfo: NodeInfo{Name: "a", Pool: "apps", Ready: true}, Values: map[string]float64{"cpuUsed": 2, "cpuCapacity": 4, "cpuRequested": 3, "cpuAllocatable": 3, "memoryUsed": 8, "memoryCapacity": 16}},
		{NodeInfo: NodeInfo{Name: "b", Pool: "apps", Ready: true}, Values: map[string]float64{"cpuCapacity": 100}},
		{NodeInfo: NodeInfo{Name: "c", Pool: "platform", Ready: true}, Values: map[string]float64{"cpuUsed": 0, "cpuCapacity": 16}},
	}
	for i := range rows {
		evaluateOverviewNode(&rows[i])
	}
	pools := aggregateOverview(rows)
	if len(pools) != 2 || pools[1].Values["cpuBusyPct"] != 50 || pools[1].Values["cpuRequestedPct"] != 100 || pools[1].Coverage["cpuBusyPct"] != 1 || pools[1].Missing != 1 {
		t.Fatalf("pool calculation: %+v", pools)
	}
	if _, ok := rows[1].Values["cpuBusyPct"]; ok {
		t.Fatal("missing metric turned into zero")
	}
}
func TestOverviewPaginationIsBoundedSortedAndPinned(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	snap := &overviewSnapshot{ID: "old", At: time.Now(), Nodes: make([]overviewNode, 100000)}
	for i := range snap.Nodes {
		snap.Nodes[i] = overviewNode{NodeInfo: NodeInfo{Name: fmt.Sprintf("node-%06d", i), Pool: "apps"}, Values: map[string]float64{"cpuRequestedPct": float64(i), "readIOPS": 100}}
	}
	s.overview.snapshots = []*overviewSnapshot{snap}
	s.overview.attempted = time.Now()
	first := do(t, s, "GET", "/api/cluster/overview/nodes?snapshot=old&pool=apps&limit=50", "", true)
	if first.Code != 200 {
		t.Fatal(first.Body.String())
	}
	var page struct {
		Nodes []overviewNode
		Next  int
		Total int
	}
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Nodes) != 50 || page.Total != 100000 || page.Next != 50 || page.Nodes[0].Name != "node-099999" {
		t.Fatalf("bad page: %+v", page)
	}
	if _, ok := page.Nodes[0].Values["readIOPS"]; ok {
		t.Fatal("compute page fetched storage fields")
	}
	s.overview.snapshots = append(s.overview.snapshots, &overviewSnapshot{ID: "new", At: time.Now()})
	next := do(t, s, "GET", "/api/cluster/overview/nodes?snapshot=old&pool=apps&offset=50", "", true)
	if err := json.Unmarshal(next.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Nodes[0].Name != "node-099949" {
		t.Fatal("refresh changed page ordering")
	}
	for _, q := range []string{"limit=101", "limit=-1", "offset=50", "snapshot=expired", "sort=invalid", "view=invalid"} {
		r := do(t, s, "GET", "/api/cluster/overview/nodes?"+q, "", true)
		if r.Code < 400 {
			t.Errorf("accepted %s", q)
		}
	}
	if first.Body.Len() > 100000 {
		t.Fatalf("oversized response: %d", first.Body.Len())
	}
}
func TestOverviewQueriesOnlyRecordingsAndRejectsStaleSamples(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		if strings.Contains(q, "kube_pod") || !strings.Contains(q, "shpyrd_node:") {
			t.Errorf("raw inventory query: %s", q)
		}
		fmt.Fprintf(w, `{"status":"success","data":{"result":[{"metric":{"__name__":"shpyrd_node:cpuUsed","node":"a"},"value":[%d,"2"]},{"metric":{"__name__":"shpyrd_node:cpuCapacity","node":"a"},"value":[%d,"4"]},{"metric":{"__name__":"shpyrd_node:memoryUsed","node":"a"},"value":[1,"50"]}]}}`, time.Now().Unix(), time.Now().Unix())
	}))
	defer server.Close()
	s, _ := newTestServer(t, NewPromClient(server.URL), nil, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "a"}})
	snap, err := s.buildOverview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Nodes[0].Values["cpuBusyPct"] != 50 {
		t.Fatal(snap.Nodes[0])
	}
	if _, ok := snap.Nodes[0].Values["memoryUsed"]; ok {
		t.Fatal("stale sample retained")
	}
}
func TestOverviewPodPageAlwaysSelectsOneNodeAndLimitsResults(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	k := s.kube.Kube.(*kubefake.Clientset)
	k.PrependReactor("list", "pods", func(a ktesting.Action) (bool, runtime.Object, error) {
		opts := a.(interface{ GetListOptions() metav1.ListOptions }).GetListOptions()
		if opts.Limit != 50 || opts.Continue != "cursor" || !strings.Contains(opts.FieldSelector, "spec.nodeName=node-a") || !strings.Contains(opts.FieldSelector, "status.phase=Running") || a.GetNamespace() != "tenant-a" {
			t.Errorf("unscoped list: %+v", opts)
		}
		return true, &corev1.PodList{ListMeta: metav1.ListMeta{Continue: "next"}}, nil
	})
	r := do(t, s, "GET", "/api/cluster/overview/pods?node=node-a&namespace=tenant-a&phase=Running&continue=cursor", "", true)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"continue":"next"`) {
		t.Fatal(r.Body.String())
	}
	r = do(t, s, "GET", "/api/cluster/overview/pods", "", true)
	if r.Code != 400 {
		t.Fatal("accepted unscoped pods")
	}
}
func TestOverviewChartRequestsHaveBoundedSeriesAndPoints(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		q := r.URL.Query()
		if !strings.Contains(q.Get("query"), `pool="apps"`) {
			t.Error(q.Get("query"))
		}
		if q.Get("step") != "2531" {
			t.Error("7d step", q.Get("step"))
		}
		fmt.Fprint(w, `{"status":"success","data":{"result":[]}}`)
	}))
	defer server.Close()
	s, _ := newTestServer(t, NewPromClient(server.URL), nil)
	for i := 0; i < 3; i++ {
		r := do(t, s, "GET", "/api/cluster/overview/charts?pool=apps&range=7d", "", true)
		if r.Code != 200 {
			t.Fatal(r.Body.String())
		}
	}
	if calls.Load() != 2 {
		t.Fatal("repeated chart queries", calls.Load())
	}
	r := do(t, s, "GET", "/api/cluster/overview/charts?nodes=a,b,c,d,e,f", "", true)
	if r.Code != 400 {
		t.Fatal("accepted too many comparisons")
	}
}
