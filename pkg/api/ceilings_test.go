package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/ext/all"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The three storage ceilings and the disks switch (storage plan step 4),
// judged by one computation under the workspace's lock.

func blockProfile(s *Server) {
	s.opts.Vars = func(key string) string {
		return map[string]string{install.VarStorageClass: "oci-bv", install.VarVolumeMinSize: "50Gi"}[key]
	}
}

func setLimits(t *testing.T, st store.Store, ws string, l *store.Limits) {
	t.Helper()
	w, err := st.Workspace(context.Background(), ws)
	if err != nil {
		t.Fatal(err)
	}
	settings := w.Settings
	settings.Limits = l
	if _, err := st.UpdateWorkspaceSettings(context.Background(), ws, settings); err != nil {
		t.Fatal(err)
	}
}

func workspaceUsageOf(t *testing.T, s *Server, host string) Usage {
	t.Helper()
	rec := at(t, s, host, "GET", "/api/workspace", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("workspace = %d %s", rec.Code, rec.Body.String())
	}
	var v WorkspaceView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Usage == nil {
		t.Fatalf("no usage in %s", rec.Body.String())
	}
	return *v.Usage
}

// A ceiling refuses what would be added, never what exists: a workspace
// above a ceiling that shrank keeps everything, may still act on the
// other axes, and may grow a disk it has where it may not claim a new one.
func TestACeilingRefusesWhatWouldBeAddedNotWhatExists(t *testing.T) {
	s, _, st := newTenantServer(t)
	setLimits(t, st, "acme", &store.Limits{Storage: "10Gi"})
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/volumes", `{"name":"data","size":"5Gi"}`); rec.Code != http.StatusCreated {
		t.Fatalf("a volume within the ceiling = %d %s", rec.Code, rec.Body.String())
	}
	// The ceiling shrinks under what exists.
	setLimits(t, st, "acme", &store.Limits{Storage: "2Gi"})
	rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/volumes", `{"name":"more","size":"1Gi"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "This would bring the workspace to 6 GB of 2 GB.") {
		t.Errorf("a new disk above the shrunk ceiling = %d %s", rec.Code, rec.Body.String())
	}
	rec = at(t, s, "acme.shpyrd.test", "PUT", "/api/projects/shop/volumes/data", `{"size":"8Gi"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "This would bring the workspace to 8 GB of 2 GB.") {
		t.Errorf("growth above the shrunk ceiling = %d %s", rec.Code, rec.Body.String())
	}
	// The other axes still move: shop scales.
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/scale", `{"process":"web","replicas":2}`); rec.Code != http.StatusOK {
		t.Errorf("a scale on another axis = %d %s", rec.Code, rec.Body.String())
	}
	if u := workspaceUsageOf(t, s, "acme.shpyrd.test"); u.Storage != "5Gi" {
		t.Errorf("usage storage = %s, want 5Gi (what exists is kept)", u.Storage)
	}
	// Without disks in the plan, the disk that exists may still grow within
	// the ceiling; a new one may not.
	no := false
	setLimits(t, st, "acme", &store.Limits{Storage: "20Gi", AllowDisks: &no})
	if rec := at(t, s, "acme.shpyrd.test", "PUT", "/api/projects/shop/volumes/data", `{"size":"8Gi"}`); rec.Code != http.StatusOK {
		t.Errorf("growth of a disk that exists, without disks in the plan = %d %s", rec.Code, rec.Body.String())
	}
	rec = at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/volumes", `{"name":"more","size":"1Gi"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Disks are not included in the free workspace; use a bucket for files, or upgrade.") {
		t.Errorf("a new disk without disks in the plan = %d %s", rec.Code, rec.Body.String())
	}
}

// The free workspace: no disks, one shared database, no dedicated one;
// each refusal in the plan's words.
func TestTheFreeWorkspaceHasNoDisksAndASharedDatabase(t *testing.T) {
	s, _, st := newTenantServer(t)
	s.opts.Extensions = all.All()
	blockProfile(s)
	no := false
	setLimits(t, st, "acme", &store.Limits{AllowDisks: &no, Postgres: &store.PostgresLimits{Types: []string{"shared"}, DefaultType: "shared", Databases: 1}})
	create := func(body string) (int, string) {
		rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/resources", body)
		return rec.Code, rec.Body.String()
	}
	if code, body := create(`{"kind":"Postgres","name":"db","spec":{"type":"dedicated"}}`); code != http.StatusBadRequest || !strings.Contains(body, "Your plan includes a shared database. A dedicated database comes with a paid plan.") {
		t.Errorf("a dedicated database on free = %d %s", code, body)
	}
	if code, body := create(`{"kind":"Postgres","name":"db"}`); code != http.StatusCreated || !strings.Contains(body, `"type":"shared"`) {
		t.Errorf("the database the plan includes = %d %s", code, body)
	}
	if code, body := create(`{"kind":"Postgres","name":"second"}`); code != http.StatusBadRequest || !strings.Contains(body, "This would bring the workspace to 2 of 1 databases.") {
		t.Errorf("a second database = %d %s", code, body)
	}
	if code, body := create(`{"kind":"Redis","name":"durable","spec":{"persistent":true}}`); code != http.StatusBadRequest || !strings.Contains(body, "Disks are not included in the free workspace") {
		t.Errorf("a durable store on free = %d %s", code, body)
	}
	if code, body := create(`{"kind":"Redis","name":"cache","spec":{"persistent":false}}`); code != http.StatusCreated {
		t.Errorf("a cache on free = %d %s", code, body)
	}
	u := workspaceUsageOf(t, s, "acme.shpyrd.test")
	if u.Databases != 1 || u.SharedDatabases != 1 || u.Storage != "0" {
		t.Errorf("usage = %+v, want one shared database and no storage", u)
	}
}

// A dedicated database's disk counts once per instance, and the plan may
// bound how many instances one runs.
func TestADedicatedDatabaseCountsItsDiskPerInstance(t *testing.T) {
	s, _, st := newTenantServer(t)
	s.opts.Extensions = all.All()
	blockProfile(s)
	setLimits(t, st, "acme", &store.Limits{Storage: "120Gi", Postgres: &store.PostgresLimits{MaxInstances: 2}})
	rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/resources", `{"kind":"Postgres","name":"ha","spec":{"instances":3}}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "A dedicated database here runs up to 2 instances.") {
		t.Errorf("three instances = %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/resources", `{"kind":"Postgres","name":"ha","spec":{"instances":2}}`); rec.Code != http.StatusCreated {
		t.Fatalf("two instances = %d %s", rec.Code, rec.Body.String())
	}
	if u := workspaceUsageOf(t, s, "acme.shpyrd.test"); u.Storage != "100Gi" {
		t.Errorf("usage storage = %s, want 100Gi (50Gi per instance)", u.Storage)
	}
	rec = at(t, s, "acme.shpyrd.test", "POST", "/api/projects/wiki/resources", `{"kind":"Postgres","name":"db"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "This would bring the workspace to 150 GB of 120 GB.") {
		t.Errorf("a disk past the ceiling = %d %s", rec.Code, rec.Body.String())
	}
}

// A project's own cap holds it below the workspace, and the project page
// reads its usage against whichever ceiling applies.
func TestAProjectCapHoldsOneProjectBelowTheWorkspace(t *testing.T) {
	s, _, st := newTenantServer(t)
	setLimits(t, st, "acme", &store.Limits{Storage: "100Gi"})
	rec := at(t, s, "acme.shpyrd.test", "PUT", "/api/workspace/project-caps", `{"projectCaps":{"shop":{"storage":"10Gi"},"nowhere":{"storage":"1Gi"}}}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "nowhere is not one of the workspace's") {
		t.Errorf("a cap for a project that is not there = %d %s", rec.Code, rec.Body.String())
	}
	rec = at(t, s, "acme.shpyrd.test", "PUT", "/api/workspace/project-caps", `{"projectCaps":{"shop":{"storage":"10Gi"}}}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"projectCaps":{"shop":{"storage":"10Gi"}}`) {
		t.Fatalf("set a cap = %d %s", rec.Code, rec.Body.String())
	}
	rec = at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/volumes", `{"name":"data","size":"20Gi"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "This would bring the project to 20 GB of 10 GB.") {
		t.Errorf("a disk past the project's cap = %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/wiki/volumes", `{"name":"data","size":"20Gi"}`); rec.Code != http.StatusCreated {
		t.Errorf("the same disk in a project without a cap = %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/volumes", `{"name":"small","size":"4Gi"}`); rec.Code != http.StatusCreated {
		t.Errorf("a disk within the cap = %d %s", rec.Code, rec.Body.String())
	}
	rec = at(t, s, "acme.shpyrd.test", "GET", "/api/projects/shop/usage", "")
	var u ProjectUsage
	if err := json.Unmarshal(rec.Body.Bytes(), &u); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("project usage = %d %s", rec.Code, rec.Body.String())
	}
	if u.Storage != "4Gi" || u.Ceiling != "10Gi" || !u.Own {
		t.Errorf("shop usage = %+v, want 4Gi of its own 10Gi", u)
	}
	rec = at(t, s, "acme.shpyrd.test", "GET", "/api/projects/wiki/usage", "")
	var w ProjectUsage
	if err := json.Unmarshal(rec.Body.Bytes(), &w); err != nil || w.Storage != "20Gi" || w.Ceiling != "100Gi" || w.Own {
		t.Errorf("wiki usage = %d %s, want 20Gi of the workspace's 100Gi", rec.Code, rec.Body.String())
	}
}

// Twenty creates at once against a ceiling admit exactly the set that
// fits: the judgement runs under the workspace's lock, and each sees what
// the one before it added.
func TestParallelCreatesAdmitExactlyWhatFits(t *testing.T) {
	s, _, st := newTenantServer(t)
	setLimits(t, st, "acme", &store.Limits{Storage: "100Gi"})
	var wg sync.WaitGroup
	codes := make([]int, 20)
	bodies := make([]string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/volumes", fmt.Sprintf(`{"name":"data-%d","size":"10Gi"}`, i))
			codes[i], bodies[i] = rec.Code, rec.Body.String()
		}(i)
	}
	wg.Wait()
	created, refused := 0, 0
	for i, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusBadRequest:
			refused++
			if !strings.Contains(bodies[i], "This would bring the workspace to 110 GB of 100 GB.") {
				t.Errorf("refusal %d = %s", i, bodies[i])
			}
		default:
			t.Errorf("create %d = %d %s", i, code, bodies[i])
		}
	}
	if created != 10 || refused != 10 {
		t.Errorf("created %d, refused %d; want exactly the 10 that fit", created, refused)
	}
	if u := workspaceUsageOf(t, s, "acme.shpyrd.test"); u.Storage != "100Gi" {
		t.Errorf("usage storage = %s, want 100Gi", u.Storage)
	}
}

// Every refusal is said in words a person reads (#52): no pod, no
// namespace, no command.
func TestCeilingRefusalsNameNoInternals(t *testing.T) {
	q := resource.MustParse
	l := &store.Limits{Projects: 2, Instances: 3, CPU: "2", Memory: "1Gi", Storage: "100Gi", Postgres: &store.PostgresLimits{Databases: 1, MaxInstances: 2}, ObjectStorage: &store.ObjectStorageLimits{Buckets: 1}}
	no := false
	none := usage{}
	cases := []struct {
		name  string
		l     *store.Limits
		ch    change
		after usage
	}{
		{"projects", l, change{}, usage{projects: 3}},
		{"instances", l, change{}, usage{instances: 4}},
		{"cpu", l, change{}, usage{cpu: q("2500m")}},
		{"memory", l, change{}, usage{memory: q("1100Mi")}},
		{"storage", l, change{}, usage{storage: q("150Gi")}},
		{"disks", &store.Limits{AllowDisks: &no}, change{newDisk: true}, none},
		{"databases", l, change{}, usage{databases: 2}},
		{"instances of a database", l, change{instances: 3}, none},
		{"buckets", l, change{}, usage{buckets: 2}},
	}
	for _, c := range cases {
		msg := overCeiling(c.l, nil, c.ch, none, c.after)
		if msg == "" {
			t.Errorf("%s: not refused", c.name)
			continue
		}
		if bad := controller.PlatformWordingFault(msg); bad != "" {
			t.Errorf("%s: %q names %q", c.name, msg, bad)
		}
		if !strings.HasSuffix(msg, ".") || strings.ToUpper(msg[:1]) != msg[:1] {
			t.Errorf("%s: %q is not a sentence", c.name, msg)
		}
	}
	for _, msg := range []string{errDedicatedNotIncluded, errSharedNotIncluded} {
		if bad := controller.PlatformWordingFault(msg); bad != "" {
			t.Errorf("%q names %q", msg, bad)
		}
	}
	if msg := overCeiling(l, map[string]store.ProjectCap{"shop": {Storage: "10Gi"}}, change{project: "shop"}, none, usage{perProject: map[string]resource.Quantity{"shop": q("20Gi")}}); msg != "This would bring the project to 20 GB of 10 GB." {
		t.Errorf("project cap = %q", msg)
	}
}

// The settings carry the new ceilings through normalisation and say them
// in the audit line; "disks allowed" is the default and is not kept.
func TestWorkspaceSettingsCarryTheThreeCeilings(t *testing.T) {
	yes, no := true, false
	in := WorkspaceSettings{Limits: &store.Limits{
		AllowDisks:    &yes,
		ObjectStorage: &store.ObjectStorageLimits{Buckets: 1, BucketCap: "1024Mi", Total: "2Gi"},
		Postgres:      &store.PostgresLimits{Types: []string{"shared"}, Databases: 1, MaxInstances: 2},
	}}
	out, err := NormalizeWorkspaceSettings(in)
	if err != nil {
		t.Fatal(err)
	}
	l := out.Limits
	if l == nil || l.AllowDisks != nil || l.ObjectStorage == nil || l.ObjectStorage.BucketCap != "1Gi" || l.ObjectStorage.Total != "2Gi" || l.Postgres.Databases != 1 || l.Postgres.MaxInstances != 2 {
		t.Errorf("normalised = %+v", l)
	}
	in.Limits.AllowDisks = &no
	out, err = NormalizeWorkspaceSettings(in)
	if err != nil || out.Limits.AllowsDisks() {
		t.Errorf("no disks = %+v, %v", out.Limits, err)
	}
	if d := settingsDetail(out.Limits, nil); !strings.Contains(d, "disks=no") || !strings.Contains(d, "buckets=1 bucketCap=1Gi objectStorage=2Gi") || !strings.Contains(d, "databases=1 maxInstances=2") {
		t.Errorf("audit detail = %q", d)
	}
	if _, err := NormalizeWorkspaceSettings(WorkspaceSettings{Limits: &store.Limits{ObjectStorage: &store.ObjectStorageLimits{BucketCap: "lots"}}}); err == nil {
		t.Error("a bucket cap that is not a quantity passed")
	}
	if _, err := NormalizeProjectCaps(map[string]store.ProjectCap{"shop": {Storage: "-1Gi"}}); err == nil {
		t.Error("a negative project cap passed")
	}
	caps, err := NormalizeProjectCaps(map[string]store.ProjectCap{"shop": {Storage: "10240Mi"}, "wiki": {}})
	if err != nil || len(caps) != 1 || caps["shop"].Storage != "10Gi" {
		t.Errorf("caps = %v, %v", caps, err)
	}
	merged := store.MergeLimits(&store.Limits{Storage: "10Gi", Postgres: &store.PostgresLimits{SharedCap: "2Gi"}}, &store.Limits{AllowDisks: &no, Postgres: &store.PostgresLimits{Databases: 3}})
	if merged.Storage != "10Gi" || merged.AllowsDisks() || merged.Postgres.SharedCap != "2Gi" || merged.Postgres.Databases != 3 {
		t.Errorf("merged = %+v %+v", merged, merged.Postgres)
	}
}
