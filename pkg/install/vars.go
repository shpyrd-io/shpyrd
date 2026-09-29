package install

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Variables are substituted into rendered manifests and Helm values as
// ${SHPYRD_NAME}. Only that exact form is touched so `$` in upstream
// manifests (nginx snippets, shell fragments) is left alone.
var varPattern = regexp.MustCompile(`\$\{(SHPYRD_[A-Z0-9_]+)\}`)

// Well known variables.
const (
	VarDomain       = "SHPYRD_DOMAIN"        // apps and dashboard domain, e.g. 127.0.0.1.nip.io
	VarCluster      = "SHPYRD_CLUSTER"       // cluster name
	VarProfile      = "SHPYRD_PROFILE"       // profile name
	VarRegistryHost = "SHPYRD_REGISTRY_HOST" // in-cluster registry host:port
	VarVersion      = "SHPYRD_VERSION"       // shpyrd version being installed
	VarSystemNS     = "SHPYRD_SYSTEM_NS"     // namespace of shpyrd's own components
	VarHTTPPort     = "SHPYRD_HTTP_PORT"     // host port reaching ingress HTTP (URLs only)
	VarHTTPSPort    = "SHPYRD_HTTPS_PORT"    // host port reaching ingress HTTPS (URLs only)
	// Derived variables, computed by the engine (see derivedVars).
	VarExtensions   = "SHPYRD_EXTENSIONS"    // enabled extensions, comma separated
	VarDashboardURL = "SHPYRD_DASHBOARD_URL" // external dashboard URL
	VarAuthURL      = "SHPYRD_AUTH_URL"      // external URL of the login issuer (auth.<domain>)
	// VarAuthHost is the hostname part of SHPYRD_AUTH_URL (for the Dex
	// Ingress/Certificate that cannot parse a full URL). Derived.
	VarAuthHost = "SHPYRD_AUTH_HOST"
	// VarAuthIssuer is the ClusterIssuer for the sign-in service's
	// certificate: the platform issuer (DNS-01) when auth.<host> is under
	// SHPYRD_DOMAIN, the HTTP-01 one when it is a host outside the zone the
	// platform's DNS automation owns (auth.shpyrd.io next to
	// operator.shpyrd.io, RFC-0078). Derived.
	VarAuthIssuer = "SHPYRD_AUTH_ISSUER"
	// VarConsoleHost is the hostname part of SHPYRD_DASHBOARD_URL (for the
	// console Ingress/Certificate): shpyrd.<domain>, or the domain itself
	// when SHPYRD_CONSOLE_NAME is "apex" (RFC-0078). Derived.
	VarConsoleHost = "SHPYRD_CONSOLE_HOST"
	// VarConsoleName is the subdomain of the platform domain the console
	// answers at (RFC-0078): "shpyrd" → shpyrd.<domain>; "" → the apex.
	// Default "shpyrd" keeps existing installs unchanged.
	VarConsoleName         = "SHPYRD_CONSOLE_NAME"
	VarServerImage         = "SHPYRD_SERVER_IMAGE"          // server image; derived from the version unless set
	VarWorkspacesDomain    = "SHPYRD_WORKSPACES_DOMAIN"     // domain tenant workspaces live under (cloud layer)
	VarWorkspaceCertIssuer = "SHPYRD_WORKSPACE_CERT_ISSUER" // DNS-01 issuer for workspace front-door certs (cloud layer)
	// Cloud profiles (RFC-0034/0035 counterparts).
	VarClusterIssuer    = "SHPYRD_CLUSTER_ISSUER"    // cert-manager ClusterIssuer for every certificate (shpyrd-ca locally, letsencrypt on cloud)
	VarACMEEmail        = "SHPYRD_ACME_EMAIL"        // Let's Encrypt account email (cloud profiles)
	VarRegistryInsecure = "SHPYRD_REGISTRY_INSECURE" // "true" keeps the in-cluster registry on plain HTTP (escape hatch, RFC-0059)
	VarRegistrySecret   = "SHPYRD_REGISTRY_SECRET"   // name of the registry credentials Secret ("" when the registry needs none)
	// In-cluster registry (RFC-0059).
	VarRegistryIP         = "SHPYRD_REGISTRY_IP"          // fixed ClusterIP of the in-cluster registry ("" with an external registry)
	VarRegistrySize       = "SHPYRD_REGISTRY_SIZE"        // size of its volume claim (only when using filesystem storage)
	VarRegistryBucket     = "SHPYRD_REGISTRY_BUCKET"      // OCI Object Storage bucket for registry blobs ("" = filesystem/PVC)
	VarRegistryEndpoint   = "SHPYRD_REGISTRY_ENDPOINT"    // S3-compatible endpoint for the registry bucket
	VarRegistryRegion     = "SHPYRD_REGISTRY_REGION"      // region of the registry bucket
	VarBuildCacheRegistry = "SHPYRD_BUILD_CACHE_REGISTRY" // registry host for kpack registry cache ("" = PVC per app)
	VarCASource           = "SHPYRD_CA_SOURCE"            // where the platform CA comes from: "local" (~/.shpyrd/ca, shared by kind clusters) or "cluster" (generated once in the cluster)
	// Network policy enforcement (RFC-0035): "calico" installs Calico in
	// policy-only mode next to the provider's CNI; "none" relies on the
	// cluster's own engine (kind's kindnet enforces policies).
	VarNetworkPolicy = "SHPYRD_NETWORK_POLICY"
	// DNS provider (RFC-0061): "none" or "oci". The credential travels in
	// Secrets written by the dns-credentials hook, never in variables.
	VarDNSProvider    = "SHPYRD_DNS_PROVIDER"
	VarDNSAuth        = "SHPYRD_DNS_AUTH"        // "key" (an API signing key) or "workload" (OKE workload identity)
	VarDNSDomains     = "SHPYRD_DNS_DOMAINS"     // derived: YAML list of the zones ExternalDNS manages (the platform's, plus the workspaces domain when set)
	VarDNSCompartment = "SHPYRD_DNS_COMPARTMENT" // compartment holding the zone
	VarDNSTenancy     = "SHPYRD_DNS_TENANCY"
	VarDNSRegion      = "SHPYRD_DNS_REGION"
	VarDNSUser        = "SHPYRD_DNS_USER"    // IAM user of the API key
	VarDNSZoneID      = "SHPYRD_DNS_ZONE_ID" // Route 53 hosted zone id (aws)
	// Derived from the DNS settings (see derivedVars).
	VarDNSProfileSecret  = "SHPYRD_DNS_PROFILE_SECRET"  // the webhook's credential Secret name ("" with workload identity)
	VarDNSProfileSecrets = "SHPYRD_DNS_PROFILE_SECRETS" // the same as a YAML list body
	VarDefaultTLSSecret  = "SHPYRD_DEFAULT_TLS_SECRET"  // namespace/name of ingress-nginx's default certificate
	VarWildcardTLS       = "SHPYRD_WILDCARD_TLS"        // "true" when the wildcard certificate serves every project host
	// Front doors (RFC-0036).
	VarLBIP                 = "SHPYRD_LB_IP"                    // static address(es) of the public front door: OCI's reserved IP, AWS's Elastic IPs (comma-separated)
	VarPlatformExposure     = "SHPYRD_PLATFORM_EXPOSURE"        // "external" or "internal"
	VarInternalLB           = "SHPYRD_INTERNAL_LB"              // "auto" (create lazily), "true", "false"
	VarInternalLBSubnet     = "SHPYRD_INTERNAL_LB_SUBNET"       // subnet OCID for the private LB (OCI)
	VarIngressClassInternal = "SHPYRD_INGRESS_CLASS_INTERNAL"   // ingress class of the internal controller
	VarIngressClassExternal = "SHPYRD_INGRESS_CLASS_EXTERNAL"   // ingress class of the external controller
	VarPlatformIngressClass = "SHPYRD_PLATFORM_INGRESS_CLASS"   // derived: the class the dashboard, sign-in and Grafana use
	VarPlatformIssuer       = "SHPYRD_PLATFORM_ISSUER"          // derived: the issuer of their certificates (DNS-01 when a DNS provider exists, so they work on either front door)
	VarPlatformIngressSvc   = "SHPYRD_PLATFORM_INGRESS_SERVICE" // derived: the controller Service the server dials for platform hostnames (sign-in discovery)
	// Volumes on cloud profiles (RFC-0060).
	VarStorageClass       = "SHPYRD_STORAGE_CLASS"        // class for single-instance volumes ("" = the cluster default)
	VarStorageClassShared = "SHPYRD_STORAGE_CLASS_SHARED" // class for shared (ReadWriteMany) volumes
	VarVolumeMinSize      = "SHPYRD_VOLUME_MIN_SIZE"      // provider minimum a request is rounded up to ("" = none)
	VarSnapshotClass      = "SHPYRD_SNAPSHOT_CLASS"       // VolumeSnapshotClass for `shpyrd volumes snapshot` ("" = snapshots unavailable)
	VarObjectStorageSize  = "SHPYRD_OBJECT_STORAGE_SIZE"  // volume of the object-storage extension (RFC-0046)
	// The control-plane database (RFC-0033).
	VarDatabaseURL        = "SHPYRD_DATABASE_URL"          // managed PostgreSQL; empty runs the control-plane-db component
	VarControlPlaneDBSize = "SHPYRD_CONTROL_PLANE_DB_SIZE" // its volume
	// Platform backups (RFC-0037): where the encrypted archives go.
	VarBackupTarget   = "SHPYRD_BACKUP_TARGET"   // s3://bucket/prefix ("" = component skipped)
	VarBackupEndpoint = "SHPYRD_BACKUP_ENDPOINT" // S3 endpoint URL ("" = AWS S3 in the region)
	VarBackupRegion   = "SHPYRD_BACKUP_REGION"
	VarBackupSchedule = "SHPYRD_BACKUP_SCHEDULE"  // cron, UTC
	VarBackupKeep     = "SHPYRD_BACKUP_KEEP"      // archives kept
	VarFSSMountTarget = "SHPYRD_FSS_MOUNT_TARGET" // OCI File Storage mount target OCID behind shared volumes ("" = no shared volumes)
	VarFSSAD          = "SHPYRD_FSS_AD"           // availability domain of the shared volumes' file systems (OCI)
	VarEFSID          = "SHPYRD_EFS_ID"           // EFS file system behind shared volumes ("" = no shared volumes) (AWS)
	// Node pools (RFC-0077): label values of the two pools ("" = single pool).
	VarAppsPool     = "SHPYRD_APPS_POOL"     // apps, builds, one-off runs
	VarPlatformPool = "SHPYRD_PLATFORM_POOL" // platform components, databases, stores
	// Cluster autoscaler (RFC-0075): node pool autoscaling for OCI OKE.
	VarNodePoolID   = "SHPYRD_NODE_POOL_ID"   // OCI node pool OCID ("" = autoscaler not deployed)
	VarNodeMinCount = "SHPYRD_NODE_MIN_COUNT" // minimum worker nodes (1 = never fully drain)
	VarNodeMaxCount = "SHPYRD_NODE_MAX_COUNT" // maximum worker nodes
	// AWS Load Balancer Controller (RFC-0035): the cluster it manages and the
	// Elastic IPs of the public front door.
	VarAWSCluster = "SHPYRD_AWS_CLUSTER"
	VarAWSRegion  = "SHPYRD_AWS_REGION"
	VarAWSVPCID   = "SHPYRD_AWS_VPC_ID"
	VarAWSLBEIPs  = "SHPYRD_AWS_LB_EIPS"
	// Local names and front door (RFC-0057).
	VarFrontDoor        = "SHPYRD_FRONT_DOOR"        // "kind" (kind maps the ports) or "caddy" (an existing Caddy on 443 proxies to kind)
	VarLocalDNS         = "SHPYRD_LOCAL_DNS"         // "true" when *.<domain> resolves through dnsmasq on this machine
	VarURLPort          = "SHPYRD_URL_PORT"          // derived: the https port public URLs carry ("443" behind Caddy)
	VarForwardedHeaders = "SHPYRD_FORWARDED_HEADERS" // derived: ingress-nginx trusts X-Forwarded-* only behind the front door
)

// Front door modes.
const (
	FrontDoorKind  = "kind"
	FrontDoorCaddy = "caddy"
	// FrontDoorLB is a cloud load balancer in front of ingress-nginx.
	FrontDoorLB = "lb"
)

// RegistrySecretName is the dockerconfigjson Secret with the credentials
// builds push with and instances pull with (private registries).
const RegistrySecretName = "shpyrd-registry"

// RegistryHtpasswdSecretName holds the htpasswd file the in-cluster registry
// authenticates against (key "htpasswd").
const RegistryHtpasswdSecretName = "registry-htpasswd"

// DNS automation (RFC-0061).
const (
	DNSAuthKey      = "key"
	DNSAuthWorkload = "workload"
	// DNSConfigSecretName holds ExternalDNS's oci.yaml (system namespace).
	DNSConfigSecretName = "external-dns-config"
	// DNSProfileSecretName holds the API key for the DNS-01 webhook
	// (cert-manager namespace).
	DNSProfileSecretName = "oci-dns"
	// WildcardTLSSecretName is the platform's wildcard certificate.
	WildcardTLSSecretName = "platform-wildcard-tls"
)

// Platform CA sources (SHPYRD_CA_SOURCE).
const (
	CASourceLocal   = "local"
	CASourceCluster = "cluster"
)

// ServerImageRepo is where release workflows publish the server image.
const ServerImageRepo = "ghcr.io/shpyrd-io/shpyrd-server"

// releaseTagRe matches release tags (v1.2.3, v1.2.3-rc.1) but not what git
// describe makes of commits after a tag (v1.2.3-4-gabcdef, -dirty).
var releaseTagRe = regexp.MustCompile(`^v\d+\.\d+\.\d+(-(alpha|beta|rc)\.?\d*)?$`)

// DefaultServerImage is the image a CLI of the given version installs when
// nobody says otherwise (RFC-0045): a release installs the image of its own
// tag; a development build (git describe, "dev") gets the latest release,
// and developers point at their own build with --set SHPYRD_SERVER_IMAGE.
func DefaultServerImage(version string) string {
	if releaseTagRe.MatchString(version) {
		return ServerImageRepo + ":" + version
	}
	return ServerImageRepo + ":latest"
}

// derivedVars computes the variables manifests may use but nobody sets by
// hand: external URLs and the enabled extensions.
func derivedVars(vars map[string]string, exts []ExtensionComponent) map[string]string {
	base := BaseURL(vars)
	// An extension may bring several components: name it once.
	names := make([]string, 0, len(exts))
	seen := map[string]bool{}
	for _, x := range exts {
		if seen[x.Extension] {
			continue
		}
		seen[x.Extension] = true
		names = append(names, x.Extension)
	}
	consoleName := vars[VarConsoleName]
	if consoleName == "" || consoleName == "apex" {
		consoleName = "shpyrd" // default for URL building; "apex" handled in dashboardURL below
	}
	authURL := vars[VarAuthURL] // explicit wins: auth.shpyrd.io is a manual record on production
	if authURL == "" {
		authURL = base("auth")
	}
	var dashboardURL string
	if vars[VarConsoleName] == "apex" {
		// Explicit apex mode (production layout): the console sits at the
		// platform domain itself, e.g. operator.shpyrd.io.
		scheme := "https"
		if vars[VarHTTPSPort] != "" && vars[VarHTTPSPort] != "443" {
			dashboardURL = scheme + "://" + vars[VarDomain] + ":" + vars[VarHTTPSPort]
		} else {
			dashboardURL = scheme + "://" + vars[VarDomain]
		}
	} else {
		dashboardURL = base(consoleName)
	}
	// Derive the auth host from the URL so the Dex ingress can use it
	// (Dex's manifest cannot parse a URL for just the hostname).
	authHost := authURL
	if u, err := url.Parse(authURL); err == nil && u.Host != "" {
		authHost = u.Host
	}
	consoleHost := dashboardURL
	if u, err := url.Parse(dashboardURL); err == nil && u.Host != "" {
		consoleHost = u.Hostname() // no port: certificates and Ingress hosts carry none
	}
	out := map[string]string{
		VarDashboardURL:     dashboardURL,
		VarAuthURL:          authURL,
		VarAuthHost:         authHost,
		VarConsoleHost:      consoleHost,
		VarExtensions:       strings.Join(names, ","),
		VarURLPort:          URLPort(vars),
		VarForwardedHeaders: "false",
	}
	if vars[VarFrontDoor] == FrontDoorCaddy {
		out[VarForwardedHeaders] = "true"
	}
	if vars[VarServerImage] == "" {
		out[VarServerImage] = DefaultServerImage(vars[VarVersion])
	}
	// DNS (RFC-0061): with a provider, one wildcard certificate is the front
	// door's default and project Ingresses carry none of their own.
	out[VarDNSProfileSecret], out[VarDNSProfileSecrets] = "", ""
	if vars[VarDNSProvider] != "" && vars[VarDNSProvider] != "none" && vars[VarDNSAuth] != DNSAuthWorkload {
		out[VarDNSProfileSecret], out[VarDNSProfileSecrets] = DNSProfileSecretName, DNSProfileSecretName
	}
	out[VarDefaultTLSSecret] = DefaultSystemNamespace + "/shpyrd-tls"
	out[VarWildcardTLS] = "false"
	// The platform's own certificates follow the cluster issuer, except
	// that with a DNS provider they are solved through DNS-01: HTTP-01
	// needs the public front door, and the platform may sit behind the
	// internal one (RFC-0036).
	out[VarPlatformIssuer] = vars[VarClusterIssuer]
	if vars[VarDNSProvider] != "" && vars[VarDNSProvider] != "none" {
		out[VarDefaultTLSSecret] = DefaultSystemNamespace + "/" + WildcardTLSSecretName
		out[VarWildcardTLS] = "true"
		out[VarPlatformIssuer] = "letsencrypt-dns01"
	}
	// The sign-in host's certificate: DNS-01 only works for names inside
	// the zone the platform's DNS user manages (SHPYRD_DOMAIN). An auth
	// host outside it (auth.shpyrd.io, a manual record at the registrar)
	// gets HTTP-01 through the public front door.
	out[VarAuthIssuer] = out[VarPlatformIssuer]
	if domain := vars[VarDomain]; domain != "" && authHost != domain && !strings.HasSuffix(authHost, "."+domain) {
		out[VarAuthIssuer] = vars[VarClusterIssuer]
	}
	// Front door of the dashboard, sign-in and Grafana (RFC-0036), and the
	// controller Service behind it, which the server dials for those
	// hostnames instead of hairpinning through the load balancer.
	// Cloud layer: empty string means not in use, which the template
	// renders as an empty env-var value — safe for the OSS platform.
	if _, ok := vars[VarWorkspacesDomain]; !ok {
		out[VarWorkspacesDomain] = ""
	}
	if _, ok := vars[VarWorkspaceCertIssuer]; !ok {
		out[VarWorkspaceCertIssuer] = ""
	}
	// ExternalDNS publishes hosts under the platform domain and, when the
	// cloud layer gives workspaces their own domain, under that one too:
	// <workspace>.<domain> and *.<workspace>.<domain> live in a zone the
	// same DNS user manages. A filter naming only the platform domain
	// silently skips them (the first production cluster's).
	domains := []string{vars[VarDomain]}
	if ws := vars[VarWorkspacesDomain]; ws != "" && ws != vars[VarDomain] {
		domains = append(domains, ws)
	}
	out[VarDNSDomains] = "[" + strings.Join(domains, ", ") + "]"
	out[VarPlatformIngressClass] = vars[VarIngressClassExternal]
	out[VarPlatformIngressSvc] = "ingress-nginx-controller.ingress-nginx.svc:443"
	if vars[VarPlatformExposure] == "internal" {
		out[VarPlatformIngressClass] = vars[VarIngressClassInternal]
		out[VarPlatformIngressSvc] = "ingress-nginx-internal-controller.ingress-nginx-internal.svc:443"
	}
	// Registry OCI Object Storage: always defined so the registry YAML renders.
	for _, v := range []string{VarRegistryBucket, VarRegistryEndpoint, VarRegistryRegion, VarBuildCacheRegistry} {
		if _, ok := vars[v]; !ok {
			out[v] = ""
		}
	}
	for _, v := range []string{VarAppsPool, VarPlatformPool} {
		if _, ok := vars[v]; !ok {
			out[v] = ""
		}
	}
	// Cluster autoscaler (RFC-0075): always defined so the deployment YAML
	// renders on every profile; empty SHPYRD_NODE_POOL_ID means the
	// component is skipped by selected() before render anyway.
	if _, ok := vars[VarNodePoolID]; !ok {
		out[VarNodePoolID] = ""
	}
	if _, ok := vars[VarNodeMinCount]; !ok {
		out[VarNodeMinCount] = "1"
	}
	if _, ok := vars[VarNodeMaxCount]; !ok {
		out[VarNodeMaxCount] = "5"
	}
	return out
}

// URLPort is the https port public URLs carry: 443 when a front door
// terminates TLS on the standard port or a cloud load balancer listens
// there, else the port kind maps.
func URLPort(vars map[string]string) string {
	if vars[VarFrontDoor] == FrontDoorCaddy || vars[VarFrontDoor] == FrontDoorLB {
		return "443"
	}
	if p := vars[VarHTTPSPort]; p != "" {
		return p
	}
	return "443"
}

// BaseURL returns a function building https URLs for <name>.<domain>,
// including the port when it is not 443.
func BaseURL(vars map[string]string) func(name string) string {
	domain := vars[VarDomain]
	port := URLPort(vars)
	return func(name string) string {
		u := "https://" + name + "." + domain
		if port != "443" {
			u += ":" + port
		}
		return u
	}
}

// Substitute replaces variables in data. Unknown variables are an error so
// typos in manifests surface immediately.
func Substitute(data []byte, vars map[string]string) ([]byte, error) {
	var missing []string
	out := varPattern.ReplaceAllFunc(data, func(m []byte) []byte {
		name := string(m[2 : len(m)-1])
		v, ok := vars[name]
		if !ok {
			missing = append(missing, name)
			return m
		}
		return []byte(v)
	})
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("undefined variables: %s", strings.Join(uniq(missing), ", "))
	}
	return out, nil
}

func uniq(in []string) []string {
	var out []string
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// mergeVars returns base overridden by extra.
func mergeVars(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
