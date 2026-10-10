package install

import "testing"

// RFC-0060, the storage plan's step 1: the cloud profiles put project disks
// and databases on the provider's block class, with its minimum and its
// snapshots; the local profile keeps them on the node's disk.
func TestProfilesStorageClasses(t *testing.T) {
	cases := []struct {
		profile                                   string
		extra                                     map[string]string
		authURL                                   string
		class, shared, min, database, databaseMin string
	}{
		{"oci", map[string]string{VarDomain: "oci.example.com", VarACMEEmail: "ops@example.com"}, "https://auth.oci.example.com", "oci-bv", "shpyrd-fss", "50Gi", "oci-bv", "50Gi"},
		{"aws", map[string]string{VarDomain: "aws.example.com", VarACMEEmail: "ops@example.com", VarDNSProvider: "aws", VarDNSZoneID: "Z123", VarDNSRegion: "us-east-1", VarEFSID: "fs-0123", VarAWSCluster: "shpyrd-dev", VarAWSRegion: "us-east-1", VarAWSVPCID: "vpc-1", VarAWSLBEIPs: "eipalloc-1,eipalloc-2"}, "https://auth.aws.example.com", "gp3", "shpyrd-efs", "1Gi", "gp3", "1Gi"},
		{"local", map[string]string{VarDomain: "example.test", VarHTTPSPort: "8443"}, "https://auth.example.test:8443", LocalStorageClass, LocalStorageClass, "", LocalStorageClass, ""},
	}
	for _, tc := range cases {
		eng := testProfileRenders(t, tc.profile, tc.extra, tc.authURL)
		get := func(name string) string { return eng.vars[name] }
		if got := ProjectStorageClass(get); got != tc.class {
			t.Errorf("%s: project class = %q, want %q", tc.profile, got, tc.class)
		}
		if got := ProjectSharedStorageClass(get); got != tc.shared {
			t.Errorf("%s: shared class = %q, want %q", tc.profile, got, tc.shared)
		}
		if got := ProjectVolumeMinSize(get); got != tc.min {
			t.Errorf("%s: volume minimum = %q, want %q", tc.profile, got, tc.min)
		}
		if got := DatabaseStorageClass(get); got != tc.database {
			t.Errorf("%s: database class = %q, want %q", tc.profile, got, tc.database)
		}
		if got := DatabaseVolumeMinSize(get); got != tc.databaseMin {
			t.Errorf("%s: database minimum = %q, want %q", tc.profile, got, tc.databaseMin)
		}
		if _, ok := eng.vars[VarDatabaseStorageClass]; !ok {
			t.Errorf("%s: %s must be defined for the server manifest to render", tc.profile, VarDatabaseStorageClass)
		}
	}
	// An operator's explicit choice wins, and the node's disk has no minimum.
	custom := func(name string) string {
		return map[string]string{VarStorageClass: "oci-bv", VarVolumeMinSize: "50Gi", VarDatabaseStorageClass: LocalStorageClass}[name]
	}
	if DatabaseStorageClass(custom) != LocalStorageClass || DatabaseVolumeMinSize(custom) != "" || ProjectStorageClass(custom) != "oci-bv" || ProjectVolumeMinSize(custom) != "50Gi" {
		t.Errorf("explicit database class: %q/%q, project %q/%q", DatabaseStorageClass(custom), DatabaseVolumeMinSize(custom), ProjectStorageClass(custom), ProjectVolumeMinSize(custom))
	}
}
