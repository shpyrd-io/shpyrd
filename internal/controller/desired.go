// Package controller reconciles shpyrd App resources into kpack builds,
// Deployments, Services and Ingresses.
package controller

import (
	"fmt"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"net/url"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Config carries cluster-level settings the controller needs.
type Config struct {
	// Domain is the wildcard domain apps of the implicit workspace are
	// published under.
	Domain string
	// WorkspaceDomain answers the domain the apps of an explicit workspace
	// live one label under as their URLs show it (the primary domain: a
	// verified custom domain made primary, else the address; RFC-0033
	// names); "" when the workspace is unknown, which falls back to
	// Domain. Nil: one workspace.
	WorkspaceDomain func(slug string) string
	// WorkspaceAddress answers the workspace's address: the domain its
	// wildcard certificate covers. Nil: the same as WorkspaceDomain.
	WorkspaceAddress func(slug string) string
	// WorkspaceExtraDomains answers the other domains the apps also answer
	// under, one label under each (the address when a custom domain is
	// primary; other verified custom domains). Nil: none.
	WorkspaceExtraDomains func(slug string) []string
	// WorkspaceSleepDefault answers a workspace's default HTTP sleep policy
	// from its plan (RFC-0075): after and resuming, "" when none. Projects
	// without a policy of their own inherit it; an explicit "off" opts out.
	WorkspaceSleepDefault func(slug string) (after, resuming string)
	// WorkspaceLimits answers a workspace's plan, nil when it has none; the
	// controller backs it with a ResourceQuota per project namespace.
	WorkspaceLimits func(slug string) *store.Limits
	// WorkspaceSuspended says a workspace is suspended: its apps keep
	// running but are not served (their Ingresses go; the front door
	// answers with a page saying so). Nil: never.
	WorkspaceSuspended func(slug string) bool
	// WorkspaceID answers a workspace's id (the store's UUID), "" when it
	// is not known yet: image repositories are keyed by it. Nil: no store,
	// as in tests, and repositories fall back to the slug.
	WorkspaceID func(slug string) string

	// Projects is the store's project registry (RFC-0076): the controller
	// mirrors every App into it and marks deletions. Nil: no store (tests).
	Projects store.Projects
	// DashboardURL is where the implicit workspace's dashboard answers:
	// the issuer of its apps' JWTs (RFC-0033). Explicit workspaces issue
	// from https://<address>.
	DashboardURL string
	// WorkspaceCertIssuer is the ClusterIssuer for workspace front-door
	// certificates; defaults to ClusterIssuer when empty. Use a DNS-01
	// issuer so cert-manager's self-check does not need in-cluster DNS for
	// the workspace's domain (shpyrd.app, not resolvable inside the cluster).
	WorkspaceCertIssuer string
	// HTTPSPort is the port users reach ingress on (443 unless kind maps another).
	HTTPSPort string
	// RegistryHost is where built images are pushed (host:port).
	RegistryHost string
	// ClusterIssuer signs app certificates.
	ClusterIssuer string
	// IngressClass for app Ingresses.
	IngressClass string
	// DefaultBuilder is the kpack ClusterBuilder used when the App does not
	// name one.
	DefaultBuilder string
	// AppsPool and PlatformPool are the shpyrd.io/pool label values of the
	// two node pools (RFC-0077); empty means a single pool and no selectors.
	// Application processes, builds and one-off runs select AppsPool; the
	// datastores select PlatformPool.
	AppsPool     string
	PlatformPool string
	// BuildCacheSize is the kpack cache volume size (e.g. "2Gi"); empty disables.
	// Ignored when BuildCacheRegistry is set.
	BuildCacheSize string
	// BuildCacheRegistry is the registry host for the kpack registry cache
	// (RFC-0075): when set, the Image spec uses cache.registry.tag at
	// <host>/<workspace>/build-cache/<project> instead of a PVC. The PVC
	// approach creates a 50 Gi block disk per app on OCI regardless of
	// actual use; the registry cache stores blobs where the app images live.
	BuildCacheRegistry string
	// SystemNamespace holds cluster-wide configuration such as the size
	// catalog.
	SystemNamespace string
	// BuildKitImage runs Dockerfile builds (rootless BuildKit).
	BuildKitImage string
	// PodCIDR, when known, lets the project network policy block egress to
	// pods of other projects while allowing the internet.
	PodCIDR string
	// RegistrySecret names the dockerconfigjson Secret in SystemNamespace
	// with the registry's credentials; "" when the registry needs none
	// (the in-cluster registry). It is mirrored into every project
	// namespace: builds push with it, instances pull with it.
	RegistrySecret string
	// RegistryInsecure says the registry speaks plain HTTP (an external
	// registry without TLS; the in-cluster registry serves TLS from the
	// platform CA since RFC-0059).
	RegistryInsecure bool
	// CABundle names the trust bundle ConfigMap trust-manager puts in every
	// namespace (public roots plus the platform CA); builds mount it so
	// they trust the in-cluster registry. "" mounts nothing.
	CABundle string
	// RegistryDeletes says images of pruned releases may be deleted from
	// the registry (the in-cluster one; provider registries keep their own
	// retention).
	RegistryDeletes bool
	// WildcardTLS says the front door serves the platform's wildcard
	// certificate by default (RFC-0061): project Ingresses get no
	// certificate of their own.
	WildcardTLS bool
	// Front doors (RFC-0036).
	IngressClassExternal string // default "nginx"
	IngressClassInternal string // default "nginx-internal"
	// InternalLBAddress is the address of the internal load balancer;
	// used as the ExternalDNS target for internal Ingresses.
	InternalLBAddress string
	// ExternalLBAddress is the public front door's address, what a custom
	// domain's A record points at (RFC-0034); "" when unknown at start.
	ExternalLBAddress string
	// PublicChecks says the platform publishes its names on public DNS (a
	// DNS provider is configured), so a workspace's readiness may be
	// checked from outside: the name resolves, the door answers over
	// HTTPS. Off on a local cluster, where neither can be asked.
	PublicChecks bool
}

// BuildServiceAccount is the ServiceAccount builds run as in a project
// namespace when the registry needs credentials.
const BuildServiceAccount = "shpyrd-builder"

// buildServiceAccountName is what kpack Images run as: the credentialed
// account when the registry needs one, else the namespace default.
func (c Config) buildServiceAccountName() string {
	if c.RegistrySecret != "" {
		return BuildServiceAccount
	}
	return "default"
}

// imagePullSecrets for project pods: the mirrored registry Secret, if any.
func (c Config) imagePullSecrets() []corev1.LocalObjectReference {
	if c.RegistrySecret == "" {
		return nil
	}
	return []corev1.LocalObjectReference{{Name: c.RegistrySecret}}
}

// DefaultBuildKitImage is the rootless BuildKit image used for Dockerfile builds.
// Fully qualified: CRI-O (OKE, OpenShift) refuses Docker Hub short names.
const DefaultBuildKitImage = "docker.io/moby/buildkit:v0.32.2-rootless"

// Defaults fills unset fields.
func (c Config) Defaults() Config {
	if c.Domain == "" {
		c.Domain = "127.0.0.1.nip.io"
	}
	if c.HTTPSPort == "" {
		c.HTTPSPort = "443"
	}
	if c.RegistryHost == "" {
		c.RegistryHost = "10.96.0.50:5000"
	}
	if c.ClusterIssuer == "" {
		c.ClusterIssuer = "shpyrd-ca"
	}
	if c.IngressClassExternal == "" {
		c.IngressClassExternal = c.IngressClass
	}
	if c.IngressClassExternal == "" {
		c.IngressClassExternal = "nginx"
	}
	if c.IngressClassInternal == "" {
		c.IngressClassInternal = c.IngressClassExternal
	}
	c.IngressClass = c.IngressClassExternal
	if c.DefaultBuilder == "" {
		c.DefaultBuilder = "shpyrd"
	}
	if c.BuildCacheSize == "" {
		c.BuildCacheSize = "2Gi"
	}
	// BuildCacheRegistry takes precedence over BuildCacheSize; when set,
	// the kpack cache is a registry image, not a PVC.
	if c.SystemNamespace == "" {
		c.SystemNamespace = "shpyrd-system"
	}
	if c.BuildKitImage == "" {
		c.BuildKitImage = DefaultBuildKitImage
	}
	return c
}

// kpack GVKs (handled as unstructured to avoid importing kpack's module).
var (
	KpackImageGVK = schema.GroupVersionKind{Group: "kpack.io", Version: "v1alpha2", Kind: "Image"}
	KpackBuildGVK = schema.GroupVersionKind{Group: "kpack.io", Version: "v1alpha2", Kind: "Build"}
)

// processes returns the effective process map (default: one web process),
// sorted by name for deterministic reconciliation. The default itself lives on
// the App, so everything that asks what an App runs gets the same answer: the
// Metrics tab read the bare map instead and drew no allocation for an App
// whose processes are implicit (issue #12).
func processes(app *shpyrdv1.App) []namedProcess {
	m := app.EffectiveProcesses()
	out := make([]namedProcess, 0, len(m))
	for name, p := range m {
		if name == releaseProcessType {
			continue // runs once per release as a Job, never as a workload
		}
		out = append(out, namedProcess{Name: name, Process: p})
	}
	if len(out) == 0 {
		// Only a release process was declared, which is not a workload.
		out = append(out, namedProcess{Name: shpyrdv1.DefaultProcessType, Process: shpyrdv1.Process{Size: app.Status.DefaultSize}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type namedProcess struct {
	Name string
	shpyrdv1.Process
}

// port returns the port the process listens on, or 0.
func (p namedProcess) port() int32 {
	if p.Port != nil {
		return *p.Port
	}
	if p.Name == "web" {
		return shpyrdv1.DefaultWebPort
	}
	return 0
}

func (p namedProcess) replicas() int32 {
	if p.Replicas != nil {
		return *p.Replicas
	}
	return 1
}

// Names derived from an App follow one of two schemes (RFC-0076). An App
// named by its ID lives in p-<id> and owns its namespace alone, so its
// objects carry plain names: Deployment "web", Ingress "app". A legacy App
// is named by its slug and keeps the slug-prefixed names it was created
// with — names are immutable in Kubernetes, and the controller must keep
// finding what it made. Every derivation goes through these helpers;
// nothing recomputes a name from the slug.

// WorkloadName is workloadName for other packages (the API's metric
// queries name Deployments).
func WorkloadName(app *shpyrdv1.App, process string) string { return workloadName(app, process) }

// workloadName is the Deployment/Service name of a process.
func workloadName(app *shpyrdv1.App, process string) string {
	if project.IDNamed(app) {
		return process
	}
	return app.Name + "-" + process
}

// ingressName is the app's main Ingress.
func ingressName(app *shpyrdv1.App) string {
	if project.IDNamed(app) {
		return "app"
	}
	return app.Name
}

// projectSlug is the slug people read: spec.slug, or the legacy name.
func projectSlug(app *shpyrdv1.App) string { return project.SlugOf(app) }

func commonLabels(app *shpyrdv1.App) map[string]string {
	l := map[string]string{
		"app.kubernetes.io/name": app.Name,
		shpyrdv1.LabelManagedBy:  "shpyrd",
		shpyrdv1.LabelApp:        app.Name,
	}
	// Apps of explicit workspaces carry their workspace on everything they
	// own; the implicit one adds nothing, so upgrading changes no labels.
	if ws := app.Labels[shpyrdv1.LabelWorkspace]; ws != "" && ws != project.DefaultWorkspace {
		l[shpyrdv1.LabelWorkspace] = ws
	}
	// Identity labels (RFC-0076) follow the App onto everything it owns,
	// with the display slug next to them for people reading kubectl.
	for k, v := range identityLabels(app) {
		l[k] = v
	}
	return l
}

// identityLabels are the RFC-0076 labels an App's objects carry: the
// project's ID and slug, and the workspace's ID when the App knows it.
// Legacy Apps without an ID yet contribute nothing (no empty labels).
func identityLabels(app *shpyrdv1.App) map[string]string {
	out := map[string]string{}
	if app.Spec.ID == "" {
		return out
	}
	out[shpyrdv1.LabelProjectID] = ids.Short(app.Spec.ID)
	out[shpyrdv1.LabelProject] = projectSlug(app)
	if ws := app.Labels[shpyrdv1.LabelWorkspaceID]; ws != "" {
		out[shpyrdv1.LabelWorkspaceID] = ws
	}
	return out
}

// workspaceOf is the workspace an App belongs to, from its authoritative
// label; Apps from before RFC-0033 carry none and are the default
// workspace's (slug "default": the only slug an install that old has).
func workspaceOf(app *shpyrdv1.App) string {
	if ws := app.Labels[shpyrdv1.LabelWorkspace]; ws != "" {
		return ws
	}
	return project.DefaultWorkspace
}

// DefaultWorkspace answers the slug of the operator's default workspace
// (RFC-0078): the one apps without a workspace label belong to, the one
// global vars reach, the one that is never suspended. The server points it
// at the setting; alone, it is the constant every install started with.
var DefaultWorkspace = func() string { return project.DefaultWorkspace }

// isDefault says the slug is the operator's default workspace.
func (c Config) isDefault(ws string) bool { return ws == DefaultWorkspace() }

// suspended says the app's workspace is suspended (RFC-0033). The
// operator's default workspace never is.
func (c Config) suspended(app *shpyrdv1.App) bool {
	if c.WorkspaceSuspended == nil {
		return false
	}
	ws := workspaceOf(app)
	return !c.isDefault(ws) && c.WorkspaceSuspended(ws)
}

// appsDomain is the domain the app's default host sits one label under:
// its workspace's (RFC-0080: every workspace has an address; the default
// one's is the platform domain in the open-source layout), the platform
// domain when the workspace is not known yet.
func (c Config) appsDomain(app *shpyrdv1.App) string {
	if c.WorkspaceDomain != nil {
		if d := c.WorkspaceDomain(workspaceOf(app)); d != "" {
			return d
		}
	}
	return c.Domain
}

func processLabels(app *shpyrdv1.App, process string) map[string]string {
	l := commonLabels(app)
	l[shpyrdv1.LabelProcess] = process
	return l
}

func selectorLabels(app *shpyrdv1.App, process string) map[string]string {
	return map[string]string{
		shpyrdv1.LabelApp:     app.Name,
		shpyrdv1.LabelProcess: process,
	}
}

// domains returns the hosts served by the web process.
// url is the public URL of the web process.
func (c Config) url(app *shpyrdv1.App) string {
	u := "https://" + c.domains(app)[0]
	if c.HTTPSPort != "" && c.HTTPSPort != "443" {
		u += ":" + c.HTTPSPort
	}
	return u
}

// issuer is the iss of the JWTs the edge hands the app: its workspace's
// dashboard URL, where /.well-known/jwks.json publishes the keys.
func (c Config) issuer(app *shpyrdv1.App) string {
	if c.WorkspaceDomain != nil {
		if address := c.WorkspaceDomain(workspaceOf(app)); address != "" {
			u := "https://" + address
			if c.HTTPSPort != "" && c.HTTPSPort != "443" {
				u += ":" + c.HTTPSPort
			}
			return u
		}
	}
	if c.DashboardURL != "" {
		return c.DashboardURL
	}
	u := "https://shpyrd." + c.Domain
	if c.HTTPSPort != "" && c.HTTPSPort != "443" {
		u += ":" + c.HTTPSPort
	}
	return u
}

// platformEnv tells a process where it runs, so it can verify what the
// edge sends (RFC-0033): the project and workspace slugs and the JWT
// issuer, whose /.well-known/jwks.json holds the signing keys. It also
// tells which code runs: REVISION (and SHPYRD_REVISION) is the git commit
// the release was built from, or the archive digest when the source was
// not a git checkout; empty for prebuilt images.
// platformEnv is what the platform tells every process about itself: that
// it runs here, which project and workspace it belongs to, which process
// it is, which release and revision it runs. `process` is the process
// type (web, worker, release); `release` is the number of the release the
// process belongs to, 0 while none is known.
func (c Config) platformEnv(app *shpyrdv1.App, revision, process string, release int) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "RUNNING_IN_SHPYRD", Value: "true"},
		{Name: "SHPYRD_PROJECT", Value: projectSlug(app)},
		{Name: "SHPYRD_PROJECT_NAME", Value: project.DisplayName(app)},
		{Name: "SHPYRD_WORKSPACE", Value: workspaceOf(app)},
		{Name: "SHPYRD_ISSUER", Value: c.issuer(app)},
	}
	if app.Spec.ID != "" {
		env = append(env, corev1.EnvVar{Name: "SHPYRD_PROJECT_ID", Value: app.Spec.ID})
	}
	if process != "" {
		env = append(env, corev1.EnvVar{Name: "SHPYRD_PROCESS", Value: process})
	}
	if release > 0 {
		env = append(env,
			corev1.EnvVar{Name: "SHPYRD_RELEASE", Value: fmt.Sprint(release)},
			corev1.EnvVar{Name: "SHPYRD_RELEASE_VERSION", Value: fmt.Sprintf("v%d", release)},
		)
	}
	if revision != "" {
		env = append(env,
			corev1.EnvVar{Name: "SHPYRD_REVISION", Value: revision},
			corev1.EnvVar{Name: "SHPYRD_PROJECT_REVISION", Value: revision},
			corev1.EnvVar{Name: "REVISION", Value: revision},
		)
	}
	return env
}

// releaseNumber is the number of the release a rollout of `image` with
// `hash` belongs to: the current one when nothing changed, the next one
// otherwise. It is what recordRelease will write, known before the
// workloads are built so that the processes can be told.
func releaseNumber(app *shpyrdv1.App, image, hash string) int {
	cur := app.CurrentRelease()
	if cur == nil {
		return 1
	}
	if cur.Image == image && cur.ConfigHash == hash {
		return cur.Number
	}
	return cur.Number + 1
}

// SourcesPort is where the server serves source archives to build pods
// (RFC-0033: the API and the edge are for the front doors only).
const SourcesPort = 8082

// sourceURL is where a build pod fetches an uploaded archive: the server's
// sources port. Archives uploaded before that port existed name the API
// port, which project namespaces can no longer reach; the host is the
// same, so the port is rewritten.
func (c Config) sourceURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	host := u.Hostname()
	if host != "shpyrd-server."+c.SystemNamespace+".svc" && host != "shpyrd-server."+c.SystemNamespace+".svc.cluster.local" {
		return raw
	}
	if p := u.Port(); p != "" && p != "80" {
		return raw
	}
	u.Host = host + ":" + strconv.Itoa(SourcesPort)
	return u.String()
}

// imageTag is the repository builds of this app are pushed to:
// apps/<workspace id>/<slug>, the id rendered in base36 (25 characters,
// RFC-0033). Two workspaces may both have a shop, and a repository shared
// between them would share tags, the BuildKit cache and the builder; the
// id rather than the slug because ids never change. The implicit workspace
// has an id too, so one rule covers every install. kpack's spec.tag is
// immutable: an app whose repository changes gets its Image recreated and
// rebuilt once. Without a store (tests) the slug stands in; a workspace
// the store does not know yet is an error, retried, never a guess that
// would move the repository later.
func (c Config) imageTag(app *shpyrdv1.App) (string, error) {
	ws := workspaceOf(app)
	if c.WorkspaceID == nil {
		return c.RegistryHost + "/apps/" + ws + "/" + app.Name, nil
	}
	id := c.WorkspaceID(ws)
	if id == "" {
		return "", fmt.Errorf("workspace %s is not known to the store yet", ws)
	}
	return c.RegistryHost + "/apps/" + ids.Short(id) + "/" + app.Name, nil
}

// workspaceKey is the workspace's id in its short form for registry paths
// that need no error path: the store's id when known, else the slug (tests,
// or a store that has not caught up).
func (c Config) workspaceKey(app *shpyrdv1.App) string {
	if v := app.Labels[shpyrdv1.LabelWorkspaceID]; v != "" {
		return v
	}
	ws := workspaceOf(app)
	if c.WorkspaceID != nil {
		if id := c.WorkspaceID(ws); id != "" {
			return ids.Short(id)
		}
	}
	return ws
}

// kpackImageKey names the kpack Image of an app (for lookups and deletes
// that need no spec).
func kpackImageKey(app *shpyrdv1.App) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(KpackImageGVK)
	u.SetName(app.Name)
	u.SetNamespace(app.Namespace)
	return u
}

// desiredKpackImage renders the kpack Image that builds the App's source.
func (c Config) desiredKpackImage(app *shpyrdv1.App) (*unstructured.Unstructured, error) {
	tag, err := c.imageTag(app)
	if err != nil {
		return nil, err
	}
	builder := c.DefaultBuilder
	if app.Spec.Build != nil && app.Spec.Build.Builder != "" {
		builder = app.Spec.Build.Builder
	}
	source := map[string]interface{}{}
	switch {
	case app.Spec.Source.Git != nil:
		git := map[string]interface{}{"url": app.Spec.Source.Git.URL}
		rev := app.Spec.Source.Git.Revision
		if rev == "" {
			rev = "main"
		}
		git["revision"] = rev
		source["git"] = git
	case app.Spec.Source.Blob != nil:
		source["blob"] = map[string]interface{}{"url": c.sourceURL(app.Spec.Source.Blob.URL)}
	}
	if app.Spec.Source.SubPath != "" {
		source["subPath"] = app.Spec.Source.SubPath
	}

	spec := map[string]interface{}{
		"tag":                      tag,
		"serviceAccountName":       c.buildServiceAccountName(),
		"builder":                  map[string]interface{}{"name": builder, "kind": "ClusterBuilder"},
		"source":                   source,
		"failedBuildHistoryLimit":  int64(5),
		"successBuildHistoryLimit": int64(10),
		"imageTaggingStrategy":     "BuildNumber",
	}
	switch {
	case c.BuildCacheRegistry != "":
		// Registry-backed cache (RFC-0075): no PVC; the blobs are stored
		// alongside the app images in the same registry, under the same
		// keys: build-cache/<workspace id>/<app name> (RFC-0076 — the id,
		// so a workspace rename keeps its caches; the app name is the
		// project id for apps named by it).
		cacheTag := c.BuildCacheRegistry + "/build-cache/" + c.workspaceKey(app) + "/" + app.Name
		spec["cache"] = map[string]interface{}{"registry": map[string]interface{}{"tag": cacheTag}}
	case c.BuildCacheSize != "":
		spec["cache"] = map[string]interface{}{"volume": map[string]interface{}{"size": c.BuildCacheSize}}
	}
	build := map[string]interface{}{}
	var env []interface{}
	if app.Spec.Build != nil {
		for _, e := range app.Spec.Build.Env {
			env = append(env, map[string]interface{}{"name": e.Name, "value": e.Value})
		}
	}
	if len(env) > 0 {
		build["env"] = env
	}
	// The project's variables, through the Secret it binds (build_env.go).
	build["services"] = []interface{}{buildEnvService(app)}
	if sel := c.appsNodeSelector(); sel != nil {
		// Builds are bursty and transient: the apps pool (RFC-0077).
		ns := map[string]interface{}{}
		for k, v := range sel {
			ns[k] = v
		}
		build["nodeSelector"] = ns
	}
	if len(build) > 0 {
		spec["build"] = build
	}

	u := kpackImageKey(app)
	u.SetLabels(commonLabels(app))
	u.Object["spec"] = spec
	return u, nil
}

// processResources resolves the instance size of a process against the
// cluster catalog (see pkg/sizes): explicit resources override, then the
// named size, then the catalog default. It returns the size name in effect.
func processResources(p namedProcess, catalog sizes.Catalog) (corev1.ResourceRequirements, string, error) {
	res, name, err := catalog.Resolve(p.Size, p.Resources)
	if err != nil {
		return corev1.ResourceRequirements{}, "", fmt.Errorf("process %s: %w", p.Name, err)
	}
	if name == "" {
		name = "custom"
	}
	return res, name, nil
}

// mutateDeployment sets the fields shpyrd owns on a process Deployment.
// mutateDeployment renders a process's Deployment. With scaledExternally
// (RFC-0075: KEDA drives the web Deployment between 0 and the process's
// instance count) the replica count is left to the scaler once the
// Deployment exists; setting it on every reconcile would wake a sleeping
// app and fight the scaler forever.
func (c Config) mutateDeployment(app *shpyrdv1.App, p namedProcess, image, configHash, revision string, release int, res corev1.ResourceRequirements, mounts []resolvedMount, d *appsv1.Deployment, scaledExternally bool) {
	labels := processLabels(app, p.Name)
	d.Labels = mergeMaps(d.Labels, labels)
	if d.Spec.Selector == nil {
		// The selector is immutable; only set it on creation.
		d.Spec.Selector = &metav1.LabelSelector{MatchLabels: selectorLabels(app, p.Name)}
	}
	if !scaledExternally || d.Spec.Replicas == nil {
		d.Spec.Replicas = ptr.To(p.replicas())
	}
	d.Spec.RevisionHistoryLimit = ptr.To[int32](3)
	d.Spec.Strategy = rolloutStrategy(p, mounts)

	container := corev1.Container{
		Name:            "app",
		Image:           image,
		Command:         p.Command,
		Args:            p.Args,
		Resources:       res,
		SecurityContext: hardenedSecurityContext(),
		// Global vars, then the project's config vars, then the vars of
		// attached resources: with envFrom the last source wins, so project
		// vars override globals and bound vars win over both (RFC-0003,
		// RFC-0016).
		EnvFrom: EnvSources(app),
	}
	// Buildpack images expose every process type as /cnb/process/<type>;
	// "web" is the image entrypoint so it also works for plain images.
	if len(p.Command) == 0 && p.Name != "web" {
		container.Command = []string{"/cnb/process/" + p.Name}
	}
	port := p.port()
	if port > 0 {
		container.Env = append(container.Env, corev1.EnvVar{Name: "PORT", Value: fmt.Sprint(port)})
		container.Ports = []corev1.ContainerPort{{Name: "http", ContainerPort: port, Protocol: corev1.ProtocolTCP}}
	}
	applyProbes(&container, p, port)
	container.Env = append(container.Env, c.platformEnv(app, revision, p.Name, release)...)
	container.Env = append(container.Env, app.Spec.Env...)

	d.Spec.Template.Labels = mergeMaps(d.Spec.Template.Labels, labels)
	d.Spec.Template.Annotations = mergeMaps(d.Spec.Template.Annotations, map[string]string{
		shpyrdv1.AnnotationConfigHash: configHash,
	})
	if at := app.Annotations[shpyrdv1.AnnotationRestartedAt]; at != "" {
		// A redeploy: same release, new pods.
		d.Spec.Template.Annotations[shpyrdv1.AnnotationRestartedAt] = at
	}
	d.Spec.Template.Spec.EnableServiceLinks = ptr.To(false)
	d.Spec.Template.Spec.NodeSelector = c.appsNodeSelector() // RFC-0077
	d.Spec.Template.Spec.ImagePullSecrets = c.imagePullSecrets()
	d.Spec.Template.Spec.SecurityContext = &corev1.PodSecurityContext{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}
	hc := p.HealthCheck
	if hc == nil || !hc.Disabled {
		shutdown := int64(parseDurationSecs(hc.GetStr("ShutdownDelay"), 5))
		timeout := int64(parseDurationSecs(hc.GetStr("Timeout"), 5))
		tgp := shutdown + timeout + 5
		d.Spec.Template.Spec.TerminationGracePeriodSeconds = ptr.To(tgp)
	}
	d.Spec.Template.Spec.Containers = []corev1.Container{container}
	applyMounts(d, mounts)
}

// rolloutStrategy returns the Deployment strategy for a process. Processes
// with a RWO volume use Recreate; everything else uses RollingUpdate with
// maxSurge=1 and maxUnavailable=0 so traffic is always served (RFC-0019).
func rolloutStrategy(p namedProcess, mounts []resolvedMount) appsv1.DeploymentStrategy {
	for _, m := range mounts {
		if !m.Shared {
			return appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
		}
	}
	return appsv1.DeploymentStrategy{
		Type: appsv1.RollingUpdateDeploymentStrategyType,
		RollingUpdate: &appsv1.RollingUpdateDeployment{
			MaxSurge:       ptr.To(intstr.FromInt32(1)),
			MaxUnavailable: ptr.To(intstr.FromInt32(0)),
		},
	}
}

// applyProbes configures the readiness, liveness and startup probes and the
// preStop lifecycle hook according to RFC-0019. Defaults by process type:
//   - web (port 8080): HTTP GET / on the port
//   - explicit port (non-web): TCP on that port
//   - no port (workers): no probe
func applyProbes(c *corev1.Container, p namedProcess, port int32) {
	hc := p.HealthCheck
	if hc != nil && hc.Disabled {
		return
	}
	interval := parseDurationSecs(hc.GetStr("Interval"), 10)
	timeout := parseDurationSecs(hc.GetStr("Timeout"), 5)
	grace := parseDurationSecs(hc.GetStr("GracePeriod"), 30)
	shutdown := parseDurationSecs(hc.GetStr("ShutdownDelay"), 5)

	var handler corev1.ProbeHandler
	switch {
	case hc != nil && len(hc.Command) > 0:
		handler = corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: hc.Command}}
	case hc != nil && hc.TCP:
		if port > 0 {
			handler = corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(port)}}
		}
	case hc != nil && hc.Path != "":
		handler = corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: hc.Path, Port: intstr.FromInt32(port)}}
	case p.Name == "web" && port > 0:
		handler = corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/", Port: intstr.FromInt32(port)}}
	case port > 0:
		handler = corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(port)}}
	default:
		return // workers without a port: no probe
	}

	readiness := &corev1.Probe{ProbeHandler: handler, PeriodSeconds: interval, TimeoutSeconds: timeout, FailureThreshold: 3, SuccessThreshold: 1}
	liveness := &corev1.Probe{ProbeHandler: handler, PeriodSeconds: interval, TimeoutSeconds: timeout, FailureThreshold: 6, SuccessThreshold: 1}
	startup := &corev1.Probe{ProbeHandler: handler, PeriodSeconds: 5, TimeoutSeconds: timeout, FailureThreshold: int32(grace / 5), SuccessThreshold: 1}
	if startup.FailureThreshold < 6 {
		startup.FailureThreshold = 6
	}
	c.ReadinessProbe = readiness
	c.LivenessProbe = liveness
	c.StartupProbe = startup

	gracePeriod := int64(shutdown) + int64(timeout) + 5
	c.TerminationMessagePolicy = corev1.TerminationMessageFallbackToLogsOnError
	c.Lifecycle = &corev1.Lifecycle{
		PreStop: &corev1.LifecycleHandler{Exec: &corev1.ExecAction{
			Command: []string{"sh", "-c", fmt.Sprintf("sleep %d", shutdown)},
		}},
	}
	_ = gracePeriod // applied on the pod template below (desired.go)
}

// parseDurationSecs parses a "Ns" or "Nm" string as seconds, or returns the
// default when the input is empty or invalid.
func parseDurationSecs(s string, def int32) int32 {
	if s == "" {
		return def
	}
	var n int32
	var unit string
	if _, err := fmt.Sscanf(s, "%d%s", &n, &unit); err != nil || n <= 0 {
		return def
	}
	if unit == "m" {
		return n * 60
	}
	return n
}

// mutateService sets the fields shpyrd owns on a process Service.
func (c Config) mutateService(app *shpyrdv1.App, p namedProcess, s *corev1.Service) {
	s.Labels = mergeMaps(s.Labels, processLabels(app, p.Name))
	s.Spec.Selector = selectorLabels(app, p.Name)
	s.Spec.Ports = []corev1.ServicePort{{
		Name:       "http",
		Port:       80,
		TargetPort: intstr.FromString("http"),
		Protocol:   corev1.ProtocolTCP,
	}}
}

// mutateIngress sets the fields shpyrd owns on the web Ingress. With the
// platform's wildcard certificate as the front door's default (RFC-0061)
// the Ingress declares its hosts under TLS without a certificate of its own:
// ingress-nginx serves the default and no issuance happens per project.
// mutateIngress renders the app's Ingress. sleepActive routes the web host
// through the KEDA interceptor ("web-sleep", RFC-0075) instead of the
// process Service; the edge's auth_request flow is unchanged either way.
func (c Config) mutateIngress(app *shpyrdv1.App, ing *networkingv1.Ingress, sleepActive bool) {
	ing.Labels = mergeMaps(ing.Labels, processLabels(app, "web"))
	ing.Annotations = mergeMaps(ing.Annotations, map[string]string{
		"nginx.ingress.kubernetes.io/ssl-redirect":    "true",
		"nginx.ingress.kubernetes.io/proxy-body-size": "50m",
	})
	hosts := c.domains(app)
	// Pick the ingress class and ExternalDNS target based on exposure.
	class := c.IngressClass // already set to IngressClassExternal by Defaults
	if app.Spec.Exposure == "internal" {
		if c.IngressClassInternal != "" {
			class = c.IngressClassInternal
		}
		// Point the host's A record at the private LB, not the public one.
		if c.InternalLBAddress != "" {
			ing.Annotations["external-dns.kubernetes.io/target"] = c.InternalLBAddress
		}
	} else {
		delete(ing.Annotations, "external-dns.kubernetes.io/target")
	}
	ing.Spec.IngressClassName = ptr.To(class)
	// Certificates are explicit objects (reconcileCertificates), one per host
	// that needs one, so the Ingress carries no cert-manager annotation.
	delete(ing.Annotations, "cert-manager.io/cluster-issuer")
	// Who may open the app (RFC-0033): the edge decides for non-public apps.
	for _, k := range edgeAnnotationKeys {
		delete(ing.Annotations, k)
	}
	if app.EffectiveAccess() != shpyrdv1.AccessPublic {
		for k, v := range c.edgeAnnotations(app) {
			ing.Annotations[k] = v
		}
		if sleepActive {
			// A sleeping app's first request must not be served a stale
			// decision for long: 5 s instead of 20 s (RFC-0075).
			ing.Annotations["nginx.ingress.kubernetes.io/auth-cache-duration"] = "200 5s, 401 5s, 403 5s"
		}
	}
	ing.Spec.TLS = c.ingressTLS(app)
	backend := workloadName(app, "web")
	if sleepActive {
		backend = webSleepServiceName
	}
	pathType := networkingv1.PathTypePrefix
	rules := make([]networkingv1.IngressRule, 0, len(hosts))
	for _, h := range hosts {
		rules = append(rules, networkingv1.IngressRule{
			Host: h,
			IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
				Paths: []networkingv1.HTTPIngressPath{{
					Path:     "/",
					PathType: &pathType,
					Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
						Name: backend,
						Port: networkingv1.ServiceBackendPort{Name: "http"},
					}},
				}},
			}},
		})
	}
	ing.Spec.Rules = rules
}

// Edge (RFC-0033): apps whose access is not public get the ingress-nginx
// auth_request annotations pointing at the server, a companion Ingress for
// the /.shpyrd/ paths (sign-in, callback, denied page; no auth on those,
// which is why they cannot live on the app's own Ingress), and an
// ExternalName Service so that companion can name the server from the
// app's namespace.

// EdgeServiceName is the per-namespace alias of the server.
const EdgeServiceName = "shpyrd-edge"

// edgeName is the companion Ingress of an app.
func edgeName(app *shpyrdv1.App) string { return ingressName(app) + "-edge" }

// edgeAnnotations are the auth_request annotations for the app's Ingress.
func (c Config) edgeAnnotations(app *shpyrdv1.App) map[string]string {
	mode := app.EffectiveAccess()
	// Fully qualified: nginx resolves the name itself, without the pod's
	// search domains.
	server := fmt.Sprintf("http://shpyrd-server.%s.svc.cluster.local/edge/auth?project=%s&mode=%s", c.SystemNamespace, projectSlug(app), mode)
	if ws := app.Labels[shpyrdv1.LabelWorkspace]; ws != "" {
		server += "&workspace=" + ws
	}
	ann := map[string]string{
		"nginx.ingress.kubernetes.io/auth-url":              server,
		"nginx.ingress.kubernetes.io/auth-response-headers": "Authorization,X-Shpyrd-User,X-Shpyrd-Email,X-Shpyrd-Name,X-Shpyrd-Teams,X-Shpyrd-Roles",
		// The decision differs for a browser and an API client with the
		// same (absent) credentials: a sign-in redirect for one, a JSON 401
		// for the other. Accept is part of the key so a cached answer is
		// never handed to the other kind of client; the method too, so a
		// reader's cached GET never admits their POST.
		"nginx.ingress.kubernetes.io/auth-cache-key":      "$http_cookie$http_authorization$http_x_shpyrd_token$http_accept$request_method",
		"nginx.ingress.kubernetes.io/auth-cache-duration": "200 20s, 401 5s, 403 5s",
		// The 403 goes to the controller's default backend — the server —
		// which renders the "available to team X" page. (A per-Ingress
		// default-backend cannot be an ExternalName.)
		"nginx.ingress.kubernetes.io/custom-http-errors": "403",
	}
	if mode == shpyrdv1.AccessAuthenticated {
		// $http_host keeps the port (kind maps 8443); $host would drop it.
		ann["nginx.ingress.kubernetes.io/auth-signin"] = "https://$http_host/.shpyrd/signin?rd=$escaped_request_uri"
	}
	return ann
}

var edgeAnnotationKeys = []string{
	"nginx.ingress.kubernetes.io/auth-url", "nginx.ingress.kubernetes.io/auth-signin",
	"nginx.ingress.kubernetes.io/auth-response-headers", "nginx.ingress.kubernetes.io/auth-cache-key",
	"nginx.ingress.kubernetes.io/auth-cache-duration", "nginx.ingress.kubernetes.io/custom-http-errors",
	"nginx.ingress.kubernetes.io/default-backend", // from earlier versions
}

// mutateEdgeIngress builds the companion Ingress: the same hosts, class and
// TLS as the app's, paths under /.shpyrd/ to the server.
func (c Config) mutateEdgeIngress(app *shpyrdv1.App, ing *networkingv1.Ingress) {
	ing.Labels = mergeMaps(ing.Labels, processLabels(app, "web"))
	ing.Annotations = mergeMaps(ing.Annotations, map[string]string{
		"nginx.ingress.kubernetes.io/ssl-redirect": "true",
	})
	class := c.IngressClass
	if app.Spec.Exposure == "internal" && c.IngressClassInternal != "" {
		class = c.IngressClassInternal
		if c.InternalLBAddress != "" {
			ing.Annotations["external-dns.kubernetes.io/target"] = c.InternalLBAddress
		}
	}
	ing.Spec.IngressClassName = ptr.To(class)
	ing.Spec.TLS = c.ingressTLS(app)
	pathType := networkingv1.PathTypePrefix
	hosts := c.domains(app)
	rules := make([]networkingv1.IngressRule, 0, len(hosts))
	for _, h := range hosts {
		rules = append(rules, networkingv1.IngressRule{
			Host: h,
			IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
				Paths: []networkingv1.HTTPIngressPath{{
					Path:     "/.shpyrd/",
					PathType: &pathType,
					Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
						Name: EdgeServiceName,
						Port: networkingv1.ServiceBackendPort{Name: "http"},
					}},
				}},
			}},
		})
	}
	ing.Spec.Rules = rules
}

// mutateEdgeService is the ExternalName alias of the server.
func (c Config) mutateEdgeService(app *shpyrdv1.App, svc *corev1.Service) {
	svc.Labels = mergeMaps(svc.Labels, processLabels(app, "web"))
	svc.Spec.Type = corev1.ServiceTypeExternalName
	svc.Spec.ExternalName = "shpyrd-server." + c.SystemNamespace + ".svc.cluster.local"
	svc.Spec.Ports = []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromInt32(80)}}
	svc.Spec.Selector = nil
	svc.Spec.ClusterIP = ""
}

// PoolLabel is the node label naming a node pool (RFC-0077).
const PoolLabel = "shpyrd.io/pool"

// appsNodeSelector pins a pod to the apps pool, nil in single-pool mode.
func (c Config) appsNodeSelector() map[string]string {
	if c.AppsPool == "" {
		return nil
	}
	return map[string]string{PoolLabel: c.AppsPool}
}

// platformNodeSelector pins a pod to the platform pool, nil in single-pool mode.
func (c Config) platformNodeSelector() map[string]string {
	if c.PlatformPool == "" {
		return nil
	}
	return map[string]string{PoolLabel: c.PlatformPool}
}

func mergeMaps(dst, src map[string]string) map[string]string {
	if dst == nil {
		dst = map[string]string{}
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// shortImage trims a digest reference for display.
func shortImage(ref string) string {
	if i := strings.Index(ref, "@sha256:"); i >= 0 && len(ref) >= i+8+12 {
		return ref[:i] + "@" + ref[i+1:i+8+12]
	}
	return ref
}

// hardenedSecurityContext is the restricted Pod Security Standard for app
// containers (RFC-0008): non-root, no privilege escalation, no capabilities,
// the runtime's default seccomp profile. Buildpack images already run as a
// non-root user; Dockerfile images need a USER.
func hardenedSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		RunAsNonRoot:             ptr.To(true),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
}

// EnvSources lists the Secrets a process reads its environment from, in
// precedence order (later wins): globals, the project's config vars, bound
// vars. Every source is optional: a project may have no config vars, no
// attachments or no globals yet. One-off commands use the same list.
func EnvSources(app *shpyrdv1.App) []corev1.EnvFromSource {
	optional := func(name string) corev1.EnvFromSource {
		return corev1.EnvFromSource{SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: name},
			Optional:             ptr.To(true),
		}}
	}
	var out []corev1.EnvFromSource
	if !globalsDisabled(app) {
		out = append(out, optional(shpyrdv1.GlobalEnvSecretName))
	}
	return append(out, optional(app.EnvSecretName()), optional(app.BindingsSecretName()))
}
