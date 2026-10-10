package install

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/shpyrd-io/shpyrd/deploy"
)

func TestLocalProfileRenders(t *testing.T) {
	testProfileRenders(t, "local", map[string]string{VarDomain: "example.test", VarHTTPSPort: "8443"}, "https://auth.example.test:8443")
}

// The cloud profile renders with every variable defined and URLs without a
// port (a load balancer listens on 443). Since RFC-0059 it runs the same
// in-cluster registry as the local profile, with the platform CA generated
// in the cluster.
func TestOCIProfileRenders(t *testing.T) {
	eng := testProfileRenders(t, "oci", map[string]string{VarDomain: "oci.example.com", VarACMEEmail: "ops@example.com"}, "https://auth.oci.example.com")
	if eng.vars[VarClusterIssuer] != "letsencrypt" || eng.vars[VarURLPort] != "443" || eng.vars[VarRegistrySecret] != RegistrySecretName || eng.vars[VarCASource] != CASourceCluster {
		t.Errorf("oci vars: issuer=%s urlport=%s registrysecret=%s ca=%s", eng.vars[VarClusterIssuer], eng.vars[VarURLPort], eng.vars[VarRegistrySecret], eng.vars[VarCASource])
	}
	for _, present := range []string{"letsencrypt-issuers", "ca-issuers", "trust-manager", "registry-credentials", "registry", "registry-nodes", "ingress-nginx", "kpack", "shpyrd"} {
		if eng.components[present] == nil {
			t.Errorf("oci profile must install %s", present)
		}
	}
}

func TestAWSProfileRenders(t *testing.T) {
	eng := testProfileRenders(t, "aws", map[string]string{VarDomain: "aws.example.com", VarACMEEmail: "ops@example.com", VarDNSProvider: "aws", VarDNSZoneID: "Z123", VarDNSRegion: "us-east-1", VarEFSID: "fs-0123", VarAWSCluster: "shpyrd-dev", VarAWSRegion: "us-east-1", VarAWSVPCID: "vpc-1", VarAWSLBEIPs: "eipalloc-1,eipalloc-2"}, "https://auth.aws.example.com")
	if eng.vars[VarClusterIssuer] != "letsencrypt" || eng.vars[VarNetworkPolicy] != "none" || eng.vars[VarStorageClass] != "gp3" || eng.vars[VarWildcardTLS] != "true" {
		t.Errorf("aws vars: issuer=%s policy=%s class=%s wildcard=%s", eng.vars[VarClusterIssuer], eng.vars[VarNetworkPolicy], eng.vars[VarStorageClass], eng.vars[VarWildcardTLS])
	}
	for _, present := range []string{"aws-load-balancer-controller", "letsencrypt-issuers", "registry", "registry-nodes", "ingress-nginx", "ingress-nginx-internal", "external-dns", "dns", "snapshot-controller", "storage-gp3", "storage-efs", "kpack", "shpyrd"} {
		if eng.components[present] == nil {
			t.Errorf("aws profile must install %s", present)
		}
	}
	for _, absent := range []string{"network-policy", "dns01-oci", "storage-fss"} {
		if eng.components[absent] != nil {
			t.Errorf("aws profile must not install %s", absent)
		}
	}
	// The issuer comes from the profile overlay: Route 53, not the OCI webhook.
	objs, err := eng.renderComponent(eng.components["dns"])
	if err != nil {
		t.Fatal(err)
	}
	var issuer string
	for _, o := range objs {
		if o.GetKind() == "ClusterIssuer" {
			issuer = mustYAML(t, o)
		}
	}
	if !strings.Contains(issuer, "route53") || !strings.Contains(issuer, `"hostedZoneID":"Z123"`) || strings.Contains(issuer, "webhook") {
		t.Errorf("aws issuer:\n%s", issuer)
	}
	// The NLB has a hostname: no loadBalancerIP, the wildcard from the Service.
	vals, err := loadValues(deploy.FS, valuesFiles(deploy.FS, eng.components["ingress-nginx"], "aws"), eng.vars)
	if err != nil {
		t.Fatal(err)
	}
	svc := vals["controller"].(map[string]interface{})["service"].(map[string]interface{})
	ann := svc["annotations"].(map[string]interface{})
	if _, has := svc["loadBalancerIP"]; has || ann["service.beta.kubernetes.io/aws-load-balancer-type"] != "external" || ann["service.beta.kubernetes.io/aws-load-balancer-nlb-target-type"] != "ip" || ann["service.beta.kubernetes.io/aws-load-balancer-eip-allocations"] != "eipalloc-1,eipalloc-2" || ann["external-dns.kubernetes.io/hostname"] != "*.aws.example.com" {
		t.Errorf("aws ingress-nginx service values: %v", svc)
	}
	if _, oci := ann["oci.oraclecloud.com/load-balancer-type"]; oci {
		t.Errorf("OCI annotations leaked into the aws profile: %v", ann)
	}
}

// Every profile links the registry credential to the builder ServiceAccount
// and gives the kpack controller the trust bundle (RFC-0059).
func TestKpackRendersRegistryTrust(t *testing.T) {
	for _, profile := range []string{"local", "oci"} {
		eng, err := New(nil, Options{Profile: profile, Vars: map[string]string{VarDomain: "example.test"}, Reporter: &quiet{}})
		if err != nil {
			t.Fatal(err)
		}
		objs, err := eng.renderComponent(eng.components["kpack"])
		if err != nil {
			t.Fatal(err)
		}
		var sa, controller bool
		for _, o := range objs {
			y := mustYAML(t, o)
			if o.GetKind() == "ServiceAccount" && o.GetName() == "kpack-builder" {
				sa = strings.Contains(y, RegistrySecretName)
			}
			if o.GetKind() == "Deployment" && o.GetName() == "kpack-controller" {
				controller = strings.Contains(y, "SSL_CERT_FILE") && strings.Contains(y, CABundleName)
			}
		}
		if !sa || !controller {
			t.Errorf("%s: kpack-builder secret=%v controller trust=%v", profile, sa, controller)
		}
	}
}

// The registry serves TLS from the platform CA with the ClusterIP as a SAN
// and authenticates against the generated htpasswd; nodes get the CA from
// the registry-nodes DaemonSet.
func TestRegistryRendersTLS(t *testing.T) {
	eng, err := New(nil, Options{Profile: "local", Vars: map[string]string{VarDomain: "example.test"}, Reporter: &quiet{}})
	if err != nil {
		t.Fatal(err)
	}
	objs, err := eng.renderComponent(eng.components["registry"])
	if err != nil {
		t.Fatal(err)
	}
	var cert, config bool
	for _, o := range objs {
		y := mustYAML(t, o)
		switch {
		case o.GetKind() == "Certificate":
			cert = strings.Contains(y, "10.96.0.50") && strings.Contains(y, "shpyrd-ca")
		case o.GetKind() == "ConfigMap":
			config = strings.Contains(y, "tls:") && strings.Contains(y, "htpasswd") && !strings.Contains(y, "http: true")
		}
	}
	if !cert || !config {
		t.Errorf("registry: certificate=%v tls+auth config=%v", cert, config)
	}
	objs, err = eng.renderComponent(eng.components["registry-nodes"])
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objs {
		if o.GetKind() == "DaemonSet" && !strings.Contains(mustYAML(t, o), "10.96.0.50:5000") {
			t.Error("registry-nodes must carry the registry host")
		}
	}
}

func testProfileRenders(t *testing.T, profile string, vars map[string]string, wantAuthURL string) *Engine {
	t.Helper()
	// Extension components render with the same profile.
	exts := []ExtensionComponent{{Extension: "auth-local", Component: "dex", Runlevel: "rc3"}}
	eng, err := New(nil, Options{Profile: profile, Vars: vars, Extensions: exts, Reporter: &quiet{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := eng.vars[VarExtensions]; got != "auth-local" {
		t.Errorf("SHPYRD_EXTENSIONS = %q", got)
	}
	if got := eng.vars[VarAuthURL]; got != wantAuthURL {
		t.Errorf("SHPYRD_AUTH_URL = %q, want %q", got, wantAuthURL)
	}
	comps, err := eng.profile.Components(deploy.FS)
	if err != nil {
		t.Fatalf("Components: %v", err)
	}
	if len(comps) == 0 {
		t.Fatal("profile has no components")
	}
	if eng.components["dex"] == nil {
		t.Error("extension component dex must join the profile")
	}
	if _, err := New(nil, Options{Profile: profile, Extensions: []ExtensionComponent{{Extension: "x", Component: "dex", Runlevel: "rc9"}}, Reporter: &quiet{}}); err == nil {
		t.Error("unknown runlevel must be refused")
	}
	for _, c := range comps {
		if c.Helm == nil && c.Kustomize == nil && len(c.Hooks) == 0 {
			t.Errorf("%s: neither helm, kustomize nor hooks", c.Name)
		}
		if c.Helm != nil {
			if c.Helm.Chart == "" || c.Helm.Version == "" {
				t.Errorf("%s: helm chart and version must be pinned", c.Name)
			}
			if _, err := loadValues(deploy.FS, valuesFiles(deploy.FS, c, profile), eng.vars); err != nil {
				t.Errorf("%s: values: %v", c.Name, err)
			}
		}
		if c.Kustomize != nil {
			objs, err := eng.renderComponent(c)
			if err != nil {
				t.Errorf("%s: render: %v", c.Name, err)
				continue
			}
			if len(objs) == 0 {
				t.Errorf("%s: rendered nothing", c.Name)
			}
			for _, o := range objs {
				if o.GetKind() == "" || o.GetName() == "" {
					t.Errorf("%s: object without kind/name: %v", c.Name, o.Object)
				}
				if strings.Contains(mustYAML(t, o), "${SHPYRD_") {
					t.Errorf("%s: unsubstituted variable in %s/%s", c.Name, o.GetKind(), o.GetName())
				}
			}
		}
		for _, w := range c.Wait {
			if w.String() == "unknown" {
				t.Errorf("%s: empty wait spec", c.Name)
			}
		}
	}
	return eng
}

func TestSubstitute(t *testing.T) {
	vars := map[string]string{"SHPYRD_DOMAIN": "example.test"}
	out, err := Substitute([]byte("host: app.${SHPYRD_DOMAIN}\nkeep: $1 and ${OTHER}\n"), vars)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "host: app.example.test\nkeep: $1 and ${OTHER}\n" {
		t.Errorf("unexpected output %q", out)
	}
	if _, err := Substitute([]byte("${SHPYRD_MISSING}"), vars); err == nil {
		t.Error("expected error for undefined variable")
	}
}

func TestSortObjects(t *testing.T) {
	mk := func(kind string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "v1", "kind": kind, "metadata": map[string]interface{}{"name": "x"}}}
	}
	objs := []*unstructured.Unstructured{mk("ValidatingWebhookConfiguration"), mk("ClusterBuilder"), mk("Deployment"), mk("Namespace"), mk("CustomResourceDefinition"), mk("ServiceAccount")}
	sortObjects(objs)
	var got []string
	for _, o := range objs {
		got = append(got, o.GetKind())
	}
	want := "Namespace,ServiceAccount,CustomResourceDefinition,Deployment,ClusterBuilder,ValidatingWebhookConfiguration"
	if strings.Join(got, ",") != want {
		t.Errorf("got %s want %s", strings.Join(got, ","), want)
	}
}

func mustYAML(t *testing.T, o *unstructured.Unstructured) string {
	b, err := o.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type quiet struct{}

func (quiet) Runlevel(string, []string)  {}
func (quiet) Step(string, string)        {}
func (quiet) Done(string, time.Duration) {}
func (quiet) Failed(string, error)       {}

// The oci profile enforces NetworkPolicy with Calico in policy-only mode
// (RFC-0035): the upstream manifest with Oracle's edits for VCN-native pod
// networking.
func TestNetworkPolicyRendersForOKE(t *testing.T) {
	eng, err := New(nil, Options{Profile: "oci", Vars: map[string]string{VarDomain: "oci.example.com", VarACMEEmail: "ops@example.com"}, Reporter: &quiet{}})
	if err != nil {
		t.Fatal(err)
	}
	if eng.vars[VarNetworkPolicy] != "calico" || eng.components["network-policy"] == nil {
		t.Fatalf("oci profile must install network-policy (SHPYRD_NETWORK_POLICY=%s)", eng.vars[VarNetworkPolicy])
	}
	objs, err := eng.renderComponent(eng.components["network-policy"])
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objs {
		if o.GetKind() != "DaemonSet" || o.GetName() != "calico-node" {
			continue
		}
		y := mustYAML(t, o)
		for _, want := range []string{"FELIX_INTERFACEPREFIX", "NO_DEFAULT_POOLS", "FELIX_CHAININSERTMODE", "FELIX_IPTABLESBACKEND", "CALICO_NETWORKING_BACKEND"} {
			if !strings.Contains(y, want) {
				t.Errorf("calico-node lacks %s", want)
			}
		}
		for _, absent := range []string{"initContainers", "FELIX_TYPHAK8SSERVICENAME", "cni-bin-dir", "cni-net-dir", "cni-log-dir", "kubernetes-services-endpoint"} {
			if strings.Contains(y, absent) {
				t.Errorf("calico-node still has %s", absent)
			}
		}
		return
	}
	t.Error("calico-node DaemonSet not rendered")
}

// RFC-0078 production layout: the console at the domain's apex, the sign-in
// host outside the platform zone. The console Ingress and Certificate must
// name the apex, and the sign-in certificate must fall back to HTTP-01 since
// DNS-01 cannot write into a zone the platform's DNS user does not own.
func TestOCIProfileApexConsoleAndExternalAuth(t *testing.T) {
	eng := testProfileRenders(t, "oci", map[string]string{
		VarDomain:           "operator.shpyrd.example",
		VarACMEEmail:        "ops@shpyrd.example",
		VarConsoleName:      "apex",
		VarWorkspacesDomain: "shpyrd.example",
		VarAuthURL:          "https://auth.shpyrd.example",
		VarDNSProvider:      "oci",
		VarDNSZoneID:        "ocid1.dns-zone.oc1..x",
		VarDNSRegion:        "us-ashburn-1",
	}, "https://auth.shpyrd.example")
	v := eng.vars
	if v[VarDashboardURL] != "https://operator.shpyrd.example" || v[VarConsoleHost] != "operator.shpyrd.example" {
		t.Errorf("apex console: dashboard=%s host=%s", v[VarDashboardURL], v[VarConsoleHost])
	}
	if v[VarAuthHost] != "auth.shpyrd.example" {
		t.Errorf("auth host = %s", v[VarAuthHost])
	}
	// The console holds the apex, so the operator's default workspace needs
	// an address of its own under the workspaces domain (RFC-0080).
	if v[VarDefaultWorkspace] != "default" || v[VarDefaultWorkspaceAddress] != "default.shpyrd.example" {
		t.Errorf("default workspace: slug=%s address=%s", v[VarDefaultWorkspace], v[VarDefaultWorkspaceAddress])
	}
	// With a DNS provider the platform's certificates are DNS-01; the
	// external auth host is not, because DNS-01 cannot reach it.
	if v[VarPlatformIssuer] != "letsencrypt-dns01" || v[VarAuthIssuer] != "letsencrypt" {
		t.Errorf("issuers: platform=%s auth=%s", v[VarPlatformIssuer], v[VarAuthIssuer])
	}
	// The rendered console manifests name the apex, never shpyrd.<domain>.
	objs, err := eng.renderComponent(eng.components["shpyrd"])
	if err != nil {
		t.Fatal(err)
	}
	var sawIngress, sawCert bool
	for _, o := range objs {
		switch o.GetKind() {
		case "Ingress":
			if o.GetName() == "shpyrd-server" {
				sawIngress = true
				rules, _, _ := unstructured.NestedSlice(o.Object, "spec", "rules")
				host, _, _ := unstructured.NestedString(rules[0].(map[string]interface{}), "host")
				if host != "operator.shpyrd.example" {
					t.Errorf("console ingress host = %s", host)
				}
			}
		case "Certificate":
			if o.GetName() == "shpyrd-tls" {
				sawCert = true
				names, _, _ := unstructured.NestedStringSlice(o.Object, "spec", "dnsNames")
				if len(names) != 1 || names[0] != "operator.shpyrd.example" {
					t.Errorf("console certificate dnsNames = %v", names)
				}
			}
		}
	}
	if !sawIngress || !sawCert {
		t.Errorf("console ingress/certificate rendered: %v/%v", sawIngress, sawCert)
	}
}

// The default layout is unchanged: shpyrd.<domain>, auth.<domain>, both
// through the platform issuer.
func TestOCIProfileDefaultConsoleAndAuth(t *testing.T) {
	eng := testProfileRenders(t, "oci", map[string]string{
		VarDomain: "oci.example.com", VarACMEEmail: "ops@example.com",
		VarDNSProvider: "oci", VarDNSZoneID: "ocid1.dns-zone.oc1..x", VarDNSRegion: "sa-saopaulo-1",
	}, "https://auth.oci.example.com")
	v := eng.vars
	if v[VarConsoleHost] != "shpyrd.oci.example.com" || v[VarAuthHost] != "auth.oci.example.com" {
		t.Errorf("hosts: console=%s auth=%s", v[VarConsoleHost], v[VarAuthHost])
	}
	// The default workspace's address is the platform domain: project URLs
	// stay <project>.<domain> (RFC-0080).
	if v[VarDefaultWorkspaceAddress] != "oci.example.com" {
		t.Errorf("default workspace address = %s", v[VarDefaultWorkspaceAddress])
	}
	if v[VarAuthIssuer] != v[VarPlatformIssuer] || v[VarAuthIssuer] != "letsencrypt-dns01" {
		t.Errorf("auth under the domain uses the platform issuer: auth=%s platform=%s", v[VarAuthIssuer], v[VarPlatformIssuer])
	}
}

// On the cloud profile the server image is private (OCIR). Every pod that
// runs it, whatever creates the pod, must be able to pull it: the pull
// secret therefore sits on the ServiceAccount the pod uses, not only on the
// Deployment. The backup CronJob (and the Jobs the API clones from it) is
// what this catches; it ran as shpyrd-server with no pull secret on the
// first production cluster and never started.
func TestOCIProfileServerImagePullsThroughServiceAccount(t *testing.T) {
	eng := testProfileRenders(t, "oci", map[string]string{
		VarDomain: "oci.example.com", VarACMEEmail: "ops@example.com",
		VarBackupTarget: "s3://bucket/prefix",
	}, "https://auth.oci.example.com")
	image := eng.vars[VarServerImage]
	if image == "" {
		t.Fatal("no server image")
	}
	// Pass 1: ServiceAccounts carrying the pull secret, across components.
	pulls := map[string]bool{}
	type workload struct{ component, kind, name, sa string }
	var runners []workload
	for _, c := range eng.components {
		if c.Kustomize == nil {
			continue
		}
		objs, err := eng.renderComponent(c)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		for _, o := range objs {
			// Manifests may leave the namespace to the component.
			ns := o.GetNamespace()
			if ns == "" {
				ns = c.Namespace
			}
			var podSpec map[string]interface{}
			switch o.GetKind() {
			case "ServiceAccount":
				secrets, _, _ := unstructured.NestedSlice(o.Object, "imagePullSecrets")
				for _, s := range secrets {
					if name, _, _ := unstructured.NestedString(s.(map[string]interface{}), "name"); name == ImagePullSecretName {
						pulls[ns+"/"+o.GetName()] = true
					}
				}
				continue
			case "Deployment", "StatefulSet", "DaemonSet", "Job":
				podSpec, _, _ = unstructured.NestedMap(o.Object, "spec", "template", "spec")
			case "CronJob":
				podSpec, _, _ = unstructured.NestedMap(o.Object, "spec", "jobTemplate", "spec", "template", "spec")
			default:
				continue
			}
			containers, _, _ := unstructured.NestedSlice(podSpec, "containers")
			for _, ctr := range containers {
				if img, _, _ := unstructured.NestedString(ctr.(map[string]interface{}), "image"); img == image {
					sa, _, _ := unstructured.NestedString(podSpec, "serviceAccountName")
					runners = append(runners, workload{c.Name, o.GetKind(), o.GetName(), ns + "/" + sa})
				}
			}
		}
	}
	// The server and the backup CronJob are always there (pg-gateway joins
	// with the postgres extension); the check must not pass vacuously.
	seen := map[string]bool{}
	for _, r := range runners {
		seen[r.kind+"/"+r.name] = true
	}
	if !seen["Deployment/shpyrd-server"] || !seen["CronJob/platform-backup"] {
		t.Fatalf("expected the server and the backup CronJob to run %s, found %v", image, runners)
	}
	for _, r := range runners {
		if !pulls[r.sa] {
			t.Errorf("%s: %s/%s runs the server image as %s, which has no imagePullSecret %s", r.component, r.kind, r.name, r.sa, ImagePullSecretName)
		}
	}
}

// ExternalDNS must manage the workspaces domain as well as the platform's:
// workspace hosts are Ingresses like any other, and a filter naming only
// the platform domain drops them without a log line.
func TestExternalDNSFiltersIncludeWorkspacesDomain(t *testing.T) {
	filters := func(vars map[string]string) []interface{} {
		eng := testProfileRenders(t, "oci", vars, "https://auth."+vars[VarDomain])
		c := eng.components["external-dns"]
		if c == nil || c.Helm == nil {
			t.Fatal("oci profile installs external-dns with Helm")
		}
		values, err := loadValues(deploy.FS, valuesFiles(deploy.FS, c, "oci"), eng.vars)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := values["domainFilters"].([]interface{})
		if !ok {
			t.Fatalf("domainFilters = %#v, want a list", values["domainFilters"])
		}
		return got
	}
	base := map[string]string{VarDomain: "operator.shpyrd.example", VarACMEEmail: "ops@shpyrd.example",
		VarDNSProvider: "oci", VarDNSZoneID: "ocid1.dns-zone.oc1..x", VarDNSRegion: "us-ashburn-1"}
	if got := filters(base); len(got) != 1 || got[0] != "operator.shpyrd.example" {
		t.Errorf("platform only: %v", got)
	}
	withWS := map[string]string{VarWorkspacesDomain: "shpyrd.example"}
	for k, v := range base {
		withWS[k] = v
	}
	if got := filters(withWS); len(got) != 2 || got[0] != "operator.shpyrd.example" || got[1] != "shpyrd.example" {
		t.Errorf("with workspaces domain: %v", got)
	}
	// The apps of the shared layout have a zone of their own too.
	withApps := map[string]string{VarWorkspacesDomain: "shpyrd.cloud", VarAppsDomain: "shpyrd.app"}
	for k, v := range base {
		withApps[k] = v
	}
	if got := filters(withApps); len(got) != 3 || got[1] != "shpyrd.cloud" || got[2] != "shpyrd.app" {
		t.Errorf("with workspaces and apps domains: %v", got)
	}
	// The same domain twice would be a duplicate filter, not two zones.
	same := map[string]string{VarWorkspacesDomain: "operator.shpyrd.example"}
	for k, v := range base {
		same[k] = v
	}
	if got := filters(same); len(got) != 1 {
		t.Errorf("workspaces domain equal to the platform's: %v", got)
	}
}

// Workspace certificates are wildcards; without an explicit issuer they
// follow the platform's, which is DNS-01 whenever a DNS provider exists.
func TestWorkspaceCertIssuerDefaultsToPlatformIssuer(t *testing.T) {
	withDNS := testProfileRenders(t, "oci", map[string]string{
		VarDomain: "oci.example.com", VarACMEEmail: "ops@example.com",
		VarDNSProvider: "oci", VarDNSZoneID: "ocid1.dns-zone.oc1..x", VarDNSRegion: "sa-saopaulo-1",
		VarWorkspaceCertIssuer: "", // the vars file writes an empty value when there is no workspaces zone
	}, "https://auth.oci.example.com")
	if got := withDNS.vars[VarWorkspaceCertIssuer]; got != "letsencrypt-dns01" {
		t.Errorf("with a DNS provider: workspace issuer = %q, want letsencrypt-dns01", got)
	}
	explicit := testProfileRenders(t, "oci", map[string]string{
		VarDomain: "oci.example.com", VarACMEEmail: "ops@example.com",
		VarDNSProvider: "oci", VarDNSZoneID: "ocid1.dns-zone.oc1..x", VarDNSRegion: "sa-saopaulo-1",
		VarWorkspaceCertIssuer: "my-issuer",
	}, "https://auth.oci.example.com")
	if got := explicit.vars[VarWorkspaceCertIssuer]; got != "my-issuer" {
		t.Errorf("explicit issuer overridden: %q", got)
	}
	local := testProfileRenders(t, "local", map[string]string{VarDomain: "example.test", VarHTTPSPort: "8443"}, "https://auth.example.test:8443")
	if got := local.vars[VarWorkspaceCertIssuer]; got == "" || got != local.vars[VarPlatformIssuer] {
		t.Errorf("no DNS provider: workspace issuer = %q, platform issuer = %q", got, local.vars[VarPlatformIssuer])
	}
}

// `shpyrd deploy` uploads through POST /api/sources: the API takes
// archives up to 512 MiB, nginx defaults to 1 MiB, and the first real
// project on the first production cluster got a 413 page (1.3 MiB of
// source). The larger body belongs to that path alone, on a companion
// Ingress; the console's own stays at the default.
func TestConsoleIngressAllowsSourceUploads(t *testing.T) {
	for _, profile := range []string{"local", "oci"} {
		vars := map[string]string{VarDomain: "example.test", VarHTTPSPort: "8443"}
		auth := "https://auth.example.test:8443"
		if profile == "oci" {
			vars = map[string]string{VarDomain: "oci.example.com", VarACMEEmail: "ops@example.com"}
			auth = "https://auth.oci.example.com"
		}
		eng := testProfileRenders(t, profile, vars, auth)
		objs, err := eng.renderComponent(eng.components["shpyrd"])
		if err != nil {
			t.Fatal(err)
		}
		var console, sources bool
		for _, o := range objs {
			if o.GetKind() != "Ingress" {
				continue
			}
			ann := o.GetAnnotations()
			switch o.GetName() {
			case "shpyrd-server":
				console = true
				if got, ok := ann["nginx.ingress.kubernetes.io/proxy-body-size"]; ok {
					t.Errorf("%s: console ingress sets proxy-body-size %q; uploads have their own Ingress", profile, got)
				}
			case "shpyrd-server-sources":
				sources = true
				if ann["nginx.ingress.kubernetes.io/proxy-body-size"] != "512m" || ann["nginx.ingress.kubernetes.io/proxy-request-buffering"] != "off" {
					t.Errorf("%s: sources ingress annotations = %v", profile, ann)
				}
				rules, _, _ := unstructured.NestedSlice(o.Object, "spec", "rules")
				paths, _, _ := unstructured.NestedSlice(rules[0].(map[string]interface{}), "http", "paths")
				path, _, _ := unstructured.NestedString(paths[0].(map[string]interface{}), "path")
				pathType, _, _ := unstructured.NestedString(paths[0].(map[string]interface{}), "pathType")
				if len(rules) != 1 || len(paths) != 1 || path != "/api/sources" || pathType != "Exact" {
					t.Errorf("%s: sources ingress path = %s %s (%d rules, %d paths)", profile, pathType, path, len(rules), len(paths))
				}
			}
		}
		if !console || !sources {
			t.Errorf("%s: console/sources ingress rendered: %v/%v", profile, console, sources)
		}
	}
}

// The log agent (RFC-0022a) is fenced by a NetworkPolicy carrying the
// profile's pod CIDR, and its console sink is a file of its own: loaded on
// the local profile, left out by the cloud ones.
func TestLogsAgentRenders(t *testing.T) {
	render := func(profile string, vars map[string]string) (cm, ds, np string) {
		t.Helper()
		exts := []ExtensionComponent{{Extension: "logs-agent", Component: "logs-agent", Runlevel: "rc3"}}
		eng, err := New(nil, Options{Profile: profile, Vars: vars, Extensions: exts, Reporter: &quiet{}})
		if err != nil {
			t.Fatal(err)
		}
		objs, err := eng.renderComponent(eng.components["logs-agent"])
		if err != nil {
			t.Fatalf("%s: %v", profile, err)
		}
		for _, o := range objs {
			switch o.GetKind() + "/" + o.GetName() {
			case "ConfigMap/vector":
				cm = mustYAML(t, o)
			case "DaemonSet/vector":
				ds = mustYAML(t, o)
			case "NetworkPolicy/vector":
				np = mustYAML(t, o)
			}
		}
		if cm == "" || ds == "" || np == "" {
			t.Fatalf("%s: ConfigMap, DaemonSet or NetworkPolicy missing", profile)
		}
		return cm, ds, np
	}
	cm, ds, np := render("local", map[string]string{VarDomain: "example.test", VarHTTPSPort: "8443"})
	if !strings.Contains(cm, "console.yaml") || !strings.Contains(cm, "type: console") || !strings.Contains(ds, "/etc/vector/console.yaml") {
		t.Errorf("local profile must load the console sink:\n%s\n%s", cm, ds)
	}
	if !strings.Contains(np, "10.244.0.0/16") || !strings.Contains(np, "169.254.0.0/16") || !strings.Contains(np, "shpyrd.io/project") {
		t.Errorf("network policy:\n%s", np)
	}
	cloud := map[string]map[string]string{
		"oci": {VarDomain: "oci.example.com", VarACMEEmail: "ops@example.com"},
		"aws": {VarDomain: "aws.example.com", VarACMEEmail: "ops@example.com", VarDNSProvider: "aws", VarDNSZoneID: "Z123", VarDNSRegion: "us-east-1", VarEFSID: "fs-0123", VarAWSCluster: "shpyrd-dev", VarAWSRegion: "us-east-1", VarAWSVPCID: "vpc-1", VarAWSLBEIPs: "eipalloc-1,eipalloc-2"},
	}
	for profile, vars := range cloud {
		cm, ds, np := render(profile, vars)
		if strings.Contains(cm, "console.yaml") || strings.Contains(cm, "type: console") || strings.Contains(ds, "console.yaml") {
			t.Errorf("%s profile must not load the console sink:\n%s\n%s", profile, cm, ds)
		}
		if !strings.Contains(cm, "vector.yaml") || !strings.Contains(ds, "/etc/vector/vector.yaml") || !strings.Contains(ds, "/etc/vector/drains/drains.yaml") || !strings.Contains(ds, "--watch-config") {
			t.Errorf("%s profile lost the agent's config:\n%s", profile, ds)
		}
		if strings.Contains(np, "${SHPYRD_") {
			t.Errorf("%s network policy unsubstituted:\n%s", profile, np)
		}
	}
}

// The backup job fetches source archives from the server inside the
// cluster. The server's NetworkPolicy opens the API port to the front doors
// alone; the platform's own pods reach it on the sources port (8082,
// controller.SourcesPort). Production ran for two days with the job on
// port 80: every run timed out and no archive was ever written.
func TestPlatformBackupFetchesSourcesOnTheSourcesPort(t *testing.T) {
	eng, err := New(nil, Options{Profile: "oci", Vars: map[string]string{VarDomain: "oci.example.com", VarACMEEmail: "ops@example.com", VarBackupTarget: "s3://bucket/prefix"}, Reporter: &quiet{}})
	if err != nil {
		t.Fatal(err)
	}
	if eng.components["platform-backup"] == nil {
		t.Fatal("a backup target must install platform-backup")
	}
	objs, err := eng.renderComponent(eng.components["platform-backup"])
	if err != nil {
		t.Fatal(err)
	}
	var cronjob string
	for _, o := range objs {
		if o.GetKind() == "CronJob" {
			cronjob = mustYAML(t, o)
		}
	}
	if cronjob == "" {
		t.Fatal("CronJob missing")
	}
	if !strings.Contains(cronjob, `"value":"http://shpyrd-server.shpyrd-system.svc:8082"`) {
		t.Errorf("the backup job must fetch sources on the sources port:\n%s", cronjob)
	}
}

// The server's Deployment takes its applications from an init container
// (RFC-0080): the server image itself by default, the image named by
// SHPYRD_UI_IMAGE otherwise; the server reads them from the shared
// directory either way.
func TestTheServerDeploymentTakesItsApplicationsFromTheUIImage(t *testing.T) {
	server := func(t *testing.T, vars map[string]string) (init, server map[string]interface{}) {
		t.Helper()
		eng := testProfileRenders(t, "local", vars, "https://auth.example.test:8443")
		objs, err := eng.renderComponent(eng.components["shpyrd"])
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range objs {
			if o.GetKind() != "Deployment" || o.GetName() != "shpyrd-server" {
				continue
			}
			inits, _, _ := unstructured.NestedSlice(o.Object, "spec", "template", "spec", "initContainers")
			conts, _, _ := unstructured.NestedSlice(o.Object, "spec", "template", "spec", "containers")
			if len(inits) != 1 || len(conts) != 1 {
				t.Fatalf("init containers = %d, containers = %d", len(inits), len(conts))
			}
			return inits[0].(map[string]interface{}), conts[0].(map[string]interface{})
		}
		t.Fatal("no shpyrd-server Deployment")
		return nil, nil
	}
	envOf := func(c map[string]interface{}, name string) string {
		env, _, _ := unstructured.NestedSlice(c, "env")
		for _, e := range env {
			if m := e.(map[string]interface{}); m["name"] == name {
				return m["value"].(string)
			}
		}
		return ""
	}
	base := map[string]string{VarDomain: "example.test", VarHTTPSPort: "8443", VarServerImage: "shpyrd-server:dev"}
	init, srv := server(t, base)
	if init["image"] != "shpyrd-server:dev" || srv["image"] != "shpyrd-server:dev" {
		t.Errorf("by default the init container runs the server image: init=%v server=%v", init["image"], srv["image"])
	}
	args, _, _ := unstructured.NestedStringSlice(init, "args")
	if strings.Join(args, " ") != "ui-export /ui" {
		t.Errorf("init container args = %v", args)
	}
	if envOf(srv, "SHPYRD_UI_DIR") != "/ui" {
		t.Errorf("the server must read the applications from /ui")
	}
	with := map[string]string{VarDomain: "example.test", VarHTTPSPort: "8443", VarServerImage: "shpyrd-server:dev", VarUIImage: "cloud-ui:v1"}
	init, srv = server(t, with)
	if init["image"] != "cloud-ui:v1" || srv["image"] != "shpyrd-server:dev" {
		t.Errorf("with SHPYRD_UI_IMAGE: init=%v server=%v", init["image"], srv["image"])
	}
	// It is for this run alone, like the server image: the next
	// `cluster init` without it goes back to the applications built into
	// the server, so a server release takes precedence over an older UI
	// image.
	eng, err := New(nil, Options{Profile: "local", Vars: with, Reporter: &quiet{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, kept := eng.overrides()[VarUIImage]; kept {
		t.Errorf("SHPYRD_UI_IMAGE must not be recorded as an override: %v", eng.overrides())
	}
}

// A drain or an autoscaler eviction takes one gateway pod at a time: with
// two replicas one keeps serving every S3 consumer, and the single replica
// of a single writer (oci) does not block the drain.
func TestObjectGatewayRendersADisruptionBudget(t *testing.T) {
	eng, err := New(nil, Options{Profile: "oci", Vars: map[string]string{VarDomain: "oci.example.com", VarACMEEmail: "ops@example.com", VarGatewayBucket: "gateway", VarGatewayEndpoint: "https://ns.compat.objectstorage.sa-saopaulo-1.oraclecloud.com", VarGatewayRegion: "sa-saopaulo-1"}, Reporter: &quiet{}})
	if err != nil {
		t.Fatal(err)
	}
	objs, err := eng.renderComponent(eng.components["object-gateway"])
	if err != nil {
		t.Fatal(err)
	}
	var deployment, budget bool
	for _, o := range objs {
		switch {
		case o.GetKind() == "Deployment" && o.GetName() == "object-storage":
			deployment = true
		case o.GetKind() == "PodDisruptionBudget" && o.GetName() == "object-storage":
			max, _, _ := unstructured.NestedFieldNoCopy(o.Object, "spec", "maxUnavailable")
			_, hasMin, _ := unstructured.NestedFieldNoCopy(o.Object, "spec", "minAvailable")
			app, _, _ := unstructured.NestedString(o.Object, "spec", "selector", "matchLabels", "app.kubernetes.io/name")
			budget = fmt.Sprint(max) == "1" && !hasMin && app == "object-storage"
		}
	}
	if !deployment || !budget {
		t.Errorf("object-gateway: deployment=%v disruption budget=%v", deployment, budget)
	}
}

// The gateway's pod count and writer mode are variables: OCI Object
// Storage's S3 compatibility ignores If-Match, so the oci profile runs one
// pod as the single writer of the descriptors; the other profiles run two.
// A profile from before the variables renders the two as well.
func TestObjectGatewayReplicasAndWriterModeAreVariables(t *testing.T) {
	gateway := func(t *testing.T, profile string, vars map[string]string) (replicas, singleWriter string) {
		t.Helper()
		for k, v := range map[string]string{VarGatewayBucket: "gateway", VarGatewayEndpoint: "https://ns.compat.objectstorage.sa-saopaulo-1.oraclecloud.com", VarGatewayRegion: "sa-saopaulo-1"} {
			vars[k] = v
		}
		eng, err := New(nil, Options{Profile: profile, Vars: vars, Reporter: &quiet{}})
		if err != nil {
			t.Fatal(err)
		}
		objs, err := eng.renderComponent(eng.components["object-gateway"])
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range objs {
			if o.GetKind() != "Deployment" || o.GetName() != "object-storage" {
				continue
			}
			n, _, _ := unstructured.NestedFieldNoCopy(o.Object, "spec", "replicas")
			containers, _, _ := unstructured.NestedSlice(o.Object, "spec", "template", "spec", "containers")
			env, _, _ := unstructured.NestedSlice(containers[0].(map[string]interface{}), "env")
			for _, e := range env {
				if m := e.(map[string]interface{}); m["name"] == "SHPYRD_GATEWAY_SINGLE_WRITER" {
					singleWriter, _ = m["value"].(string)
				}
			}
			return fmt.Sprint(n), singleWriter
		}
		t.Fatal("no object-storage Deployment")
		return "", ""
	}
	if n, sw := gateway(t, "oci", map[string]string{VarDomain: "oci.example.com", VarACMEEmail: "ops@example.com"}); n != "1" || sw != "true" {
		t.Errorf("oci: replicas=%s single writer=%q, want 1 and true", n, sw)
	}
	for _, profile := range []string{"local", "aws"} {
		if n, sw := gateway(t, profile, map[string]string{VarDomain: "example.test", VarACMEEmail: "ops@example.com"}); n != "2" || sw != "false" {
			t.Errorf("%s: replicas=%s single writer=%q, want 2 and false", profile, n, sw)
		}
	}
	// A profile from before the variables renders with the defaults; a set
	// value is kept.
	d := derivedVars(map[string]string{VarDomain: "x.test"}, nil)
	if d[VarGatewayReplicas] != "2" || d[VarGatewaySingleWriter] != "false" {
		t.Errorf("derived replicas=%q single writer=%q", d[VarGatewayReplicas], d[VarGatewaySingleWriter])
	}
	d = derivedVars(map[string]string{VarDomain: "x.test", VarGatewayReplicas: "1", VarGatewaySingleWriter: "true"}, nil)
	if _, ok := d[VarGatewayReplicas]; ok {
		t.Error("an explicit replica count must not be overridden")
	}
	if _, ok := d[VarGatewaySingleWriter]; ok {
		t.Error("an explicit writer mode must not be overridden")
	}
}

// The backup job's memory limit is a variable with a default (#119: the
// production job died at 512Mi) and the archive is written to a scratch
// disk.
func TestPlatformBackupMemoryIsAVariable(t *testing.T) {
	for set, want := range map[string]string{"": DefaultBackupMemory, "2Gi": "2Gi"} {
		vars := map[string]string{VarDomain: "example.test"}
		if set != "" {
			vars[VarBackupMemory] = set
		}
		eng, err := New(nil, Options{Profile: "local", Vars: vars, Reporter: &quiet{}})
		if err != nil {
			t.Fatal(err)
		}
		objs, err := eng.renderComponent(eng.components["platform-backup"])
		if err != nil {
			t.Fatal(err)
		}
		var seen bool
		for _, o := range objs {
			if o.GetKind() != "CronJob" {
				continue
			}
			seen = true
			containers, _, _ := unstructured.NestedSlice(o.Object, "spec", "jobTemplate", "spec", "template", "spec", "containers")
			if len(containers) != 1 {
				t.Fatalf("containers = %v", containers)
			}
			mem, _, _ := unstructured.NestedString(containers[0].(map[string]interface{}), "resources", "limits", "memory")
			if mem != want {
				t.Errorf("SHPYRD_BACKUP_MEMORY=%q: memory limit = %q, want %q", set, mem, want)
			}
			y := mustYAML(t, o)
			if !strings.Contains(y, `"mountPath":"/tmp"`) || !strings.Contains(y, `"emptyDir"`) {
				t.Errorf("the archive needs a scratch disk on /tmp:\n%s", y)
			}
		}
		if !seen {
			t.Fatal("platform-backup rendered no CronJob")
		}
	}
	// A profile from before the variable renders with the default; a set
	// value is kept.
	if d := derivedVars(map[string]string{VarDomain: "x.test"}, nil); d[VarBackupMemory] != DefaultBackupMemory {
		t.Errorf("derived memory = %q", d[VarBackupMemory])
	}
	if d := derivedVars(map[string]string{VarDomain: "x.test", VarBackupMemory: "3Gi"}, nil); d[VarBackupMemory] != "" {
		t.Errorf("an explicit memory limit must not be overridden: %q", d[VarBackupMemory])
	}
}

// The monitoring stack carries the rules that make a failed backup run and
// a stale backup visible (#119), worded for an operator (#52), and keeps
// the kube-state-metrics collectors they read.
func TestPlatformBackupAlerts(t *testing.T) {
	for _, profile := range []string{"local", "oci"} {
		eng, err := New(nil, Options{Profile: profile, Vars: map[string]string{VarDomain: "example.test", VarACMEEmail: "ops@example.com"}, Reporter: &quiet{}})
		if err != nil {
			t.Fatal(err)
		}
		vals, err := loadValues(deploy.FS, valuesFiles(deploy.FS, eng.components["monitoring"], profile), eng.vars)
		if err != nil {
			t.Fatal(err)
		}
		rulesMap, _ := vals["additionalPrometheusRulesMap"].(map[string]interface{})
		groups, _, _ := unstructured.NestedSlice(rulesMap, "shpyrd-platform-backup", "groups")
		if len(groups) != 1 {
			t.Fatalf("%s: backup rule groups = %v", profile, groups)
		}
		rules, _, _ := unstructured.NestedSlice(groups[0].(map[string]interface{}), "rules")
		alerts := map[string]map[string]interface{}{}
		for _, r := range rules {
			rule := r.(map[string]interface{})
			alerts[rule["alert"].(string)] = rule
		}
		for _, name := range []string{"PlatformBackupFailed", "PlatformBackupStale"} {
			rule := alerts[name]
			if rule == nil {
				t.Errorf("%s: no %s rule", profile, name)
				continue
			}
			expr, _ := rule["expr"].(string)
			if !strings.Contains(expr, "platform-backup") || !strings.Contains(expr, `namespace="shpyrd-system"`) {
				t.Errorf("%s: %s expression does not select the backup job: %s", profile, name, expr)
			}
			if sev, _, _ := unstructured.NestedString(rule, "labels", "severity"); sev == "" {
				t.Errorf("%s: %s has no severity", profile, name)
			}
			ann, _, _ := unstructured.NestedStringMap(rule, "annotations")
			for _, k := range []string{"summary", "description"} {
				if ann[k] == "" {
					t.Errorf("%s: %s has no %s", profile, name, k)
				}
				for _, word := range []string{"kubectl", "CronJob", "Job", "pod", "namespace", "shpyrd-system", "`"} {
					if strings.Contains(ann[k], word) {
						t.Errorf("%s: %s %s names %q: %s", profile, name, k, word, ann[k])
					}
				}
			}
		}
		if len(alerts) == 2 {
			if !strings.Contains(alerts["PlatformBackupFailed"]["expr"].(string), "kube_job_status_failed") || !strings.Contains(alerts["PlatformBackupStale"]["expr"].(string), "kube_cronjob_status_last_successful_time") {
				t.Errorf("%s: rules read the wrong metrics", profile)
			}
		}
		// The alerts read the jobs and cronjobs collectors: the chart's
		// default list must stay, or name both.
		ksm, _ := vals["kube-state-metrics"].(map[string]interface{})
		if collectors, has := ksm["collectors"]; has {
			y := fmt.Sprint(collectors)
			if !strings.Contains(y, "jobs") || !strings.Contains(y, "cronjobs") {
				t.Errorf("%s: kube-state-metrics collectors drop jobs or cronjobs: %v", profile, collectors)
			}
		}
	}
}
