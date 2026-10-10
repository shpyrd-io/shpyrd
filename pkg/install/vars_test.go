package install

import "testing"

func TestDefaultServerImage(t *testing.T) {
	cases := map[string]string{
		"v0.1.0":          ServerImageRepo + ":v0.1.0",
		"v1.2.3-rc.1":     ServerImageRepo + ":v1.2.3-rc.1",
		"dev":             ServerImageRepo + ":latest",
		"a81d19a-dirty":   ServerImageRepo + ":latest",
		"v0.1.0-3-gabcde": ServerImageRepo + ":latest",
		"":                ServerImageRepo + ":latest",
	}
	for v, want := range cases {
		if got := DefaultServerImage(v); got != want {
			t.Errorf("DefaultServerImage(%q) = %q, want %q", v, got, want)
		}
	}
	// Derived only when nobody set it.
	d := derivedVars(map[string]string{VarVersion: "v0.1.0", VarDomain: "x.test"}, nil)
	if d[VarServerImage] != ServerImageRepo+":v0.1.0" {
		t.Errorf("derived image = %q", d[VarServerImage])
	}
	d = derivedVars(map[string]string{VarVersion: "v0.1.0", VarDomain: "x.test", VarServerImage: "shpyrd-server:dev"}, nil)
	if _, ok := d[VarServerImage]; ok {
		t.Error("an explicit image must not be overridden")
	}
}

// The applications come from the server image unless an image holding
// only them is named (RFC-0080).
func TestTheUIImageFollowsTheServerImageUnlessSet(t *testing.T) {
	d := derivedVars(map[string]string{VarVersion: "v0.1.0", VarDomain: "x.test", VarUIImage: ""}, nil)
	if d[VarUIImage] != ServerImageRepo+":v0.1.0" {
		t.Errorf("ui image from the version = %q", d[VarUIImage])
	}
	d = derivedVars(map[string]string{VarVersion: "v0.1.0", VarDomain: "x.test", VarServerImage: "shpyrd-cloud-server:dev"}, nil)
	if d[VarUIImage] != "shpyrd-cloud-server:dev" {
		t.Errorf("ui image from an explicit server image = %q", d[VarUIImage])
	}
	d = derivedVars(map[string]string{VarVersion: "v0.1.0", VarDomain: "x.test", VarUIImage: "cloud-ui:v1"}, nil)
	if _, ok := d[VarUIImage]; ok {
		t.Error("an explicit ui image must not be overridden")
	}
}

func TestFrontDoorVars(t *testing.T) {
	kind := map[string]string{VarDomain: "127.0.0.1.nip.io", VarHTTPSPort: "8443", VarFrontDoor: FrontDoorKind}
	if u := BaseURL(kind)("shpyrd"); u != "https://shpyrd.127.0.0.1.nip.io:8443" {
		t.Errorf("kind mode URL = %s", u)
	}
	d := derivedVars(kind, nil)
	if d[VarURLPort] != "8443" || d[VarForwardedHeaders] != "false" || d[VarAuthURL] != "https://auth.127.0.0.1.nip.io:8443" {
		t.Errorf("kind mode derived = %v", d)
	}
	caddy := map[string]string{VarDomain: "shpyrd.test", VarHTTPPort: "8080", VarHTTPSPort: "8443", VarFrontDoor: FrontDoorCaddy}
	if u := BaseURL(caddy)("shpyrd"); u != "https://shpyrd.shpyrd.test" {
		t.Errorf("caddy mode URL = %s", u)
	}
	d = derivedVars(caddy, nil)
	if d[VarURLPort] != "443" || d[VarForwardedHeaders] != "true" || d[VarDashboardURL] != "https://shpyrd.shpyrd.test" {
		t.Errorf("caddy mode derived = %v", d)
	}
	if URLPort(map[string]string{}) != "443" {
		t.Error("URLPort default")
	}
}

// With the S3 gateway on, the registry and the platform backups follow it
// only when nobody gave them a bucket of their own: a registry bucket from
// the registry credentials file keeps the registry writing straight to the
// provider, so image pulls never depend on the gateway; an explicit backup
// target keeps the archives on their own bucket. Sources always move.
func TestGatewayLeavesExplicitRegistryAndBackupTargetsAlone(t *testing.T) {
	gw := gatewayEndpoint("shpyrd-system")
	provider := "https://ns.compat.objectstorage.sa-saopaulo-1.oraclecloud.com"
	base := map[string]string{
		VarVersion: "v0.1.0", VarDomain: "x.test", VarSystemNS: "shpyrd-system", VarProfile: "oci",
		VarGatewayBucket: "shpyrd-prod-objects", VarGatewayEndpoint: provider, VarGatewayRegion: "sa-saopaulo-1",
		VarBackupTarget: "", VarBackupEndpoint: "", VarBackupRegion: "",
	}

	// Nothing of its own: registry, backups and sources through the gateway.
	v := mergeVars(base, derivedVars(base, nil))
	if v[VarRegistryBucket] != "registry" || v[VarRegistryEndpoint] != gw || v[VarRegistryRegion] != "garage" || v[VarRegistryS3Secure] != "false" {
		t.Errorf("registry without a bucket of its own: bucket=%q endpoint=%q region=%q secure=%q", v[VarRegistryBucket], v[VarRegistryEndpoint], v[VarRegistryRegion], v[VarRegistryS3Secure])
	}
	if v[VarBackupTarget] != "s3://platform-backups/platform" || v[VarBackupEndpoint] != gw || v[VarBackupRegion] != "garage" {
		t.Errorf("backups without a target of their own: target=%q endpoint=%q region=%q", v[VarBackupTarget], v[VarBackupEndpoint], v[VarBackupRegion])
	}
	if !registryViaGateway(v) || !backupsViaGateway(v) {
		t.Error("registry and backups should be seen as going through the gateway")
	}

	// The registry credentials file names a provider bucket and the backup
	// target is explicit (production): both stay direct.
	direct := mergeVars(base, map[string]string{
		VarRegistryBucket: "shpyrd-prod-registry", VarRegistryEndpoint: provider, VarRegistryRegion: "sa-saopaulo-1",
		VarBackupTarget: "s3://shpyrd-prod-backups/shpyrd-prod", VarBackupEndpoint: provider, VarBackupRegion: "sa-saopaulo-1",
	})
	d := derivedVars(direct, nil)
	for _, k := range []string{VarRegistryBucket, VarRegistryEndpoint, VarRegistryRegion, VarBackupTarget, VarBackupEndpoint, VarBackupRegion} {
		if got, ok := d[k]; ok {
			t.Errorf("%s=%q derived over an explicit value", k, got)
		}
	}
	v = mergeVars(direct, d)
	if v[VarRegistryBucket] != "shpyrd-prod-registry" || v[VarRegistryEndpoint] != provider || v[VarRegistryRegion] != "sa-saopaulo-1" || v[VarRegistryS3Secure] != "true" {
		t.Errorf("explicit registry: bucket=%q endpoint=%q region=%q secure=%q", v[VarRegistryBucket], v[VarRegistryEndpoint], v[VarRegistryRegion], v[VarRegistryS3Secure])
	}
	if v[VarBackupTarget] != "s3://shpyrd-prod-backups/shpyrd-prod" || v[VarBackupEndpoint] != provider || v[VarBackupRegion] != "sa-saopaulo-1" {
		t.Errorf("explicit backups: target=%q endpoint=%q region=%q", v[VarBackupTarget], v[VarBackupEndpoint], v[VarBackupRegion])
	}
	if registryViaGateway(v) || backupsViaGateway(v) {
		t.Error("an explicit registry bucket or backup target must not be seen as going through the gateway")
	}
	if v[VarSourcesBucket] != "sources" || v[VarSourcesEndpoint] != gw || v[VarSourcesRegion] != "garage" || v[VarSourcesSecret] != "gateway-sources" {
		t.Errorf("sources still move to the gateway: bucket=%q endpoint=%q region=%q secret=%q", v[VarSourcesBucket], v[VarSourcesEndpoint], v[VarSourcesRegion], v[VarSourcesSecret])
	}

	// One of each: the registry direct, the backups through the gateway.
	mixed := mergeVars(base, map[string]string{VarRegistryBucket: "shpyrd-prod-registry", VarRegistryEndpoint: provider, VarRegistryRegion: "sa-saopaulo-1"})
	v = mergeVars(mixed, derivedVars(mixed, nil))
	if registryViaGateway(v) || !backupsViaGateway(v) || v[VarRegistryBucket] != "shpyrd-prod-registry" || v[VarBackupTarget] != "s3://platform-backups/platform" {
		t.Errorf("mixed: registry bucket=%q (via gateway %v), backup target=%q (via gateway %v)", v[VarRegistryBucket], registryViaGateway(v), v[VarBackupTarget], backupsViaGateway(v))
	}
}

// The gateway hook creates logical buckets and credentials only for the
// flows that go through it; a direct registry keeps its own Secret.
func TestGatewayConsumersFollowTheRegistryAndBackupTargets(t *testing.T) {
	ns := "shpyrd-system"
	buckets := func(vars map[string]string) []string {
		var out []string
		for _, c := range gatewayConsumers(vars) {
			out = append(out, c.bucket)
		}
		return out
	}
	all := map[string]string{VarSystemNS: ns, VarGatewayBucket: "shared", VarRegistryEndpoint: gatewayEndpoint(ns), VarBackupEndpoint: gatewayEndpoint(ns)}
	if got := buckets(all); len(got) != 3 || got[0] != "registry" || got[1] != "sources" || got[2] != "platform-backups" {
		t.Errorf("everything through the gateway: consumers = %v", got)
	}
	for _, c := range gatewayConsumers(all) {
		if c.bucket == "registry" && c.secret != RegistryS3SecretName {
			t.Errorf("the registry's gateway credential goes in Secret %s, got %s", RegistryS3SecretName, c.secret)
		}
	}
	direct := map[string]string{VarSystemNS: ns, VarGatewayBucket: "shared", VarRegistryEndpoint: "https://provider.example", VarBackupEndpoint: "https://provider.example"}
	if got := buckets(direct); len(got) != 1 || got[0] != "sources" {
		t.Errorf("registry and backups direct: consumers = %v", got)
	}
}
