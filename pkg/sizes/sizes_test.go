package sizes

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestDefaultsValidAndRoundTrip(t *testing.T) {
	c := Defaults()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(c.Sizes) < 8 {
		t.Errorf("want at least 8 sizes, got %d", len(c.Sizes))
	}
	b, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if back.Default != "shared-s" || len(back.Sizes) != len(c.Sizes) {
		t.Errorf("round trip changed the catalog: %+v", back)
	}
}

func TestResources(t *testing.T) {
	s := Size{Name: "shared-s", Kind: Shared, CPU: "0.5", Memory: "64Mi"}
	r := s.Resources()
	// A shared size's CPU is its ceiling; it is guaranteed an eighth of it.
	if r.Requests.Cpu().String() != "62m" || r.Limits.Cpu().String() != "500m" || r.Limits.Memory().String() != "64Mi" || r.Requests.Memory().String() != "64Mi" {
		t.Errorf("shared resources = %+v", r)
	}
	d := Size{Name: "dedicated-m", Kind: Dedicated, CPU: "2", Memory: "4Gi"}
	r = d.Resources()
	if r.Requests.Cpu().String() != "2" || r.Limits.Cpu().String() != "2" || r.Limits.Memory().String() != "4Gi" {
		t.Errorf("dedicated resources = %+v", r)
	}
}

func TestResolve(t *testing.T) {
	c := Defaults()
	r, name, err := c.Resolve("", corev1.ResourceRequirements{})
	if err != nil || name != "shared-s" || r.Requests.Cpu().String() != "62m" || r.Limits.Cpu().String() != "500m" {
		t.Errorf("default resolve = %v %q %+v", err, name, r)
	}
	if _, _, err := c.Resolve("nope", corev1.ResourceRequirements{}); err == nil {
		t.Error("unknown size must fail")
	}
	// Explicit memory override keeps the size's cpu.
	r, name, err = c.Resolve("shared-m", corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")}})
	if err != nil || name != "" || r.Limits.Memory().String() != "1Gi" || r.Requests.Memory().String() != "1Gi" || r.Requests.Cpu().String() != "62m" || r.Limits.Cpu().String() != "500m" {
		t.Errorf("override resolve = %v %q %+v", err, name, r)
	}
	// An explicit cpu on a shared size moves the ceiling and the share with it.
	r, _, err = c.Resolve("shared-m", corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}})
	if err != nil || r.Limits.Cpu().String() != "2" || r.Requests.Cpu().String() != "250m" {
		t.Errorf("cpu override resolve = %v %+v", err, r)
	}
}

func TestUpsertRemove(t *testing.T) {
	c := Defaults()
	if err := c.Upsert(Size{Name: "huge", Kind: Dedicated, CPU: "32", Memory: "64Gi"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get("huge"); !ok {
		t.Error("upsert did not add")
	}
	if err := c.Remove("shared-s"); err == nil {
		t.Error("removing the default must fail")
	}
	if err := c.Remove("huge"); err != nil {
		t.Error(err)
	}
	if err := c.Upsert(Size{Name: "Bad", Kind: Shared, CPU: "1", Memory: "1Gi"}); err == nil {
		t.Error("invalid name must fail")
	}
	if err := (Catalog{Default: "x", Sizes: []Size{{Name: "a", Kind: Shared, CPU: "1", Memory: "1Gi"}}}).Validate(); err == nil {
		t.Error("default not in catalog must fail")
	}
}

// Databases and stores have lists of their own (#57), with the processes'
// names: a catalog saved before them gets the built-in ones and keeps its
// own processes, db-xs included; a Postgres size under 128Mi is refused.
func TestListsOfDatabasesAndStores(t *testing.T) {
	old := []byte(`default: shared-s
sizes:
- {name: shared-s, kind: shared, cpu: "0.5", memory: 64Mi}
- {name: db-xs, kind: shared, cpu: "0.5", memory: 128Mi}
`)
	c, err := Parse(old)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Sizes) != 2 || c.For(ForPostgres).Default != "shared-s" || c.For(ForRedis).Default != "shared-s" {
		t.Fatalf("old catalog = %+v %+v %+v", c.List, c.Postgres, c.Redis)
	}
	pg, err := c.For(ForPostgres).Pick(ForPostgres, "")
	if err != nil || pg.Memory != "128Mi" || pg.Connections != 20 {
		t.Errorf("Postgres default = %+v %v", pg, err)
	}
	rd, err := c.For(ForRedis).Pick(ForRedis, "shared-s")
	if err != nil || rd.Memory != "64Mi" || rd.Connections == 0 {
		t.Errorf("Redis shared-s = %+v %v", rd, err)
	}
	if _, err := c.For(ForPostgres).Pick(ForPostgres, "db-xs"); err == nil || !strings.Contains(err.Error(), "shared-s, shared-m") {
		t.Errorf("a process size for a database = %v", err)
	}
	// Saved again, the lists are written out and read back the same.
	b, _ := c.Marshal()
	back, err := Parse(b)
	if err != nil || len(back.Postgres.Sizes) != len(Defaults().Postgres.Sizes) || len(back.Redis.Sizes) != len(Defaults().Redis.Sizes) {
		t.Errorf("round trip = %v %+v", err, back)
	}

	small := Defaults()
	small.Postgres.Sizes = append(small.Postgres.Sizes, Size{Name: "tiny", Kind: Shared, CPU: "0.5", Memory: "64Mi"})
	if err := small.Validate(); err == nil || !strings.Contains(err.Error(), "at least 128Mi") {
		t.Errorf("a 64Mi Postgres size = %v", err)
	}
	proc := Defaults()
	proc.Sizes[0].Connections = 10
	if err := proc.Validate(); err == nil {
		t.Error("connections on a process size must be refused")
	}
	if c.For("mysql") != nil {
		t.Error("no list for an unknown kind")
	}
}
