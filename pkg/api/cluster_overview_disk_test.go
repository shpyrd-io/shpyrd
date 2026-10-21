package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/install"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTheThresholdIsTheKubeletDefaultUnlessTheOperatorSaysOtherwise(t *testing.T) {
	for v, want := range map[string]struct {
		pct float64
		set bool
	}{
		"": {defaultEvictionPct, false}, "10": {10, true}, "12.5%": {12.5, true}, " 0 ": {0, true},
		"lots": {defaultEvictionPct, false}, "-1": {defaultEvictionPct, false}, "100": {defaultEvictionPct, false},
	} {
		pct, set := evictionPct(map[string]string{install.VarDiskEvictionPct: v})
		if pct != want.pct || set != want.set {
			t.Errorf("%q: %v %v, want %v %v", v, pct, set, want.pct, want.set)
		}
	}
}

func TestADiskIsCalledLowBeforeTheKubeletEvictsNotAfter(t *testing.T) {
	for _, c := range []struct {
		values map[string]float64
		want   []string
	}{
		// Production's dip: under OKE's 15%, above the old fixed 10%.
		{map[string]float64{"diskAvailablePct": 13.5, "diskEvictionPct": 15}, []string{"Disk about to evict pods: 13.5% free, evicts below 15%"}},
		{map[string]float64{"diskAvailablePct": 22, "diskEvictionPct": 15}, []string{"Low disk space: 22% free, evicts below 15%"}},
		{map[string]float64{"diskAvailablePct": 40, "diskEvictionPct": 15}, nil},
		// Fine now, not this afternoon.
		{map[string]float64{"diskAvailablePct": 40, "diskEvictionPct": 15, "diskAvailablePctMin24h": 15.1}, []string{"Free disk fell to 15.1% in the last 24 hours; evicts below 15%"}},
		{map[string]float64{"diskAvailablePct": 40, "diskEvictionPct": 15, "diskPressure7d": 32, "diskEvictions7d": 2}, []string{"Evicting: 32 disk-pressure episodes, 2 pods evicted in 7 days"}},
		{map[string]float64{"diskAvailablePct": 40, "diskEvictionPct": 15, "diskPressure7d": 1}, []string{"Evicting: 1 disk-pressure episode in 7 days"}},
		// No threshold, no judgement of the share.
		{map[string]float64{"diskAvailablePct": 5}, nil},
	} {
		n := overviewNode{Values: c.values}
		if got := diskIssues(&n); fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%v: %q, want %q", c.values, got, c.want)
		}
	}
}

func TestTheOverviewJudgesDiskByTheEvictionThresholdAndRemembersTheWeek(t *testing.T) {
	now := time.Now().Unix()
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		if strings.Contains(q, "kube_") || strings.Contains(q, "kubelet_") {
			t.Errorf("a raw series instead of a recording: %s", q)
		}
		if strings.Contains(q, "history") {
			fmt.Fprintf(w, `{"status":"success","data":{"result":[
				{"metric":{"node":"apps-1","history":"diskAvailablePctMin24h","mountpoint":"/"},"value":[%[1]d,"13.5"]},
				{"metric":{"node":"apps-1","history":"diskPressure7d"},"value":[%[1]d,"32"]},
				{"metric":{"node":"apps-1","history":"diskEvictions7d"},"value":[%[1]d,"2.003"]},
				{"metric":{"node":"apps-2","history":"diskPressure7d"},"value":[%[1]d,"0"]}]}}`, now)
			return
		}
		fmt.Fprintf(w, `{"status":"success","data":{"result":[
			{"metric":{"__name__":"shpyrd_node:diskAvailablePct","node":"apps-1","mountpoint":"/"},"value":[%[1]d,"40"]},
			{"metric":{"__name__":"shpyrd_node:diskSize","node":"apps-1","mountpoint":"/"},"value":[%[1]d,"100000000000"]},
			{"metric":{"__name__":"shpyrd_node:diskAvailablePct","node":"apps-2","mountpoint":"/"},"value":[%[1]d,"22"]},
			{"metric":{"__name__":"shpyrd_node:diskSize","node":"apps-2","mountpoint":"/"},"value":[%[1]d,"100000000000"]}]}}`, now)
	}))
	defer prom.Close()
	apps := map[string]string{"shpyrd.io/pool": "apps"}
	s, _ := newTestServer(t, NewPromClient(prom.URL), nil,
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "apps-1", Labels: apps}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "apps-2", Labels: apps}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}})
	snap, err := s.buildOverview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]overviewNode{}
	for _, n := range snap.Nodes {
		byName[n.Name] = n
	}
	one, two := byName["apps-1"], byName["apps-2"]
	if one.Values["diskEvictionPct"] != defaultEvictionPct || !one.DiskEvictionDefault {
		t.Errorf("apps-1 threshold: %v default=%v", one.Values["diskEvictionPct"], one.DiskEvictionDefault)
	}
	if want := []string{"Free disk fell to 13.5% in the last 24 hours; evicts below 15%", "Evicting: 32 disk-pressure episodes, 2 pods evicted in 7 days"}; fmt.Sprint(diskOnly(one.Issues)) != fmt.Sprint(want) {
		t.Errorf("apps-1 issues %q, want %q", one.Issues, want)
	}
	if fmt.Sprint(diskOnly(two.Issues)) != fmt.Sprint([]string{"Low disk space: 22% free, evicts below 15%"}) {
		t.Errorf("apps-2 issues %q", two.Issues)
	}
	pool := snap.Pools[0]
	if pool.Distribution["diskAvailablePct"] != 1 || pool.Coverage["diskAvailablePct"] != 2 || pool.Distribution["diskEvicting7d"] != 1 {
		t.Errorf("pool counts: distribution %v coverage %v", pool.Distribution, pool.Coverage)
	}

	// An operator who tuned the kubelets to evict at 10% says so once, and
	// 22% free is no longer within the warning margin.
	record := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: install.InstallRecordName, Namespace: install.DefaultSystemNamespace}, Data: map[string]string{"profile": "oci", "vars": install.VarDiskEvictionPct + ": \"10\"\n"}}
	s, _ = newTestServer(t, NewPromClient(prom.URL), nil, record,
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "apps-2", Labels: apps}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}})
	if snap, err = s.buildOverview(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := snap.Nodes[0]; n.Values["diskEvictionPct"] != 10 || n.DiskEvictionDefault || len(diskOnly(n.Issues)) != 0 {
		t.Errorf("with the operator's 10%%: %v default=%v %q", n.Values["diskEvictionPct"], n.DiskEvictionDefault, n.Issues)
	}
}

// diskOnly keeps a node's issues about its disk: these nodes have no CPU
// or memory figures, and say so.
func diskOnly(issues []string) []string {
	var out []string
	for _, i := range issues {
		if !strings.Contains(i, "telemetry") {
			out = append(out, i)
		}
	}
	return out
}
