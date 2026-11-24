package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/net/publicsuffix"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
)

// Custom domains (RFC-0034). A project is always served at
// <slug>.<cluster domain>; that hostname is what a custom domain's DNS record
// points at (CNAME), or the front door's address for a zone apex (A). Once
// DNS points here, cert-manager proves control with HTTP-01 and issues a
// certificate per custom domain; nothing else is asked of the owner, as on
// Heroku and Render. Each custom domain has its own Certificate so one whose
// DNS is not ready yet never blocks the others or the project's own host.

// CertificateGVK is cert-manager's Certificate.
var CertificateGVK = schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "Certificate"}

// Domain states.
const (
	DNSOK      = "ok"
	DNSMissing = "missing"
	DNSWrong   = "wrong"
	DNSUnknown = "unknown"

	CertReady    = "ready"
	CertIssuing  = "issuing"
	CertFailed   = "failed"
	CertWildcard = "wildcard"
)

// workspaceHosts are the hosts a project answers at because of its
// workspace (RFC-0033 names), the one its URL shows first:
// <workspace>-<app>.<apps domain> in the shared layout, one label under
// the workspace's domains otherwise, a primary custom domain first.
func (c Config) workspaceHosts(app *shpyrdv1.App) []string {
	if c.WorkspaceAppHosts != nil {
		if hosts := c.WorkspaceAppHosts(workspaceOf(app), projectSlug(app)); len(hosts) > 0 {
			return hosts
		}
	}
	out := []string{projectSlug(app) + "." + c.appsDomain(app)}
	if c.WorkspaceExtraDomains != nil {
		for _, d := range c.WorkspaceExtraDomains(workspaceOf(app)) {
			out = append(out, projectSlug(app)+"."+d)
		}
	}
	return out
}

// defaultHost is the hostname every project is served at, the one its URL
// shows.
func (c Config) defaultHost(app *shpyrdv1.App) string {
	return c.workspaceHosts(app)[0]
}

// normalizeHost is how a host of the spec is compared and served: lowercase,
// without spaces or a trailing dot.
func normalizeHost(h string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), "."))
}

// ownershipSnapshots holds, for each App being reconciled, the answers of
// the ownership rule taken once at the start of that reconcile, keyed by
// "namespace/name". An App is never reconciled twice at the same time, and
// the entry is removed when the reconcile ends.
var ownershipSnapshots sync.Map

type hostAnswers map[string]tenancy.Availability

func snapshotKey(app *shpyrdv1.App) string { return app.Namespace + "/" + app.Name }

// beginOwnership asks the ownership rule once for every host the app lists
// in spec.domains and keeps the answers for the rest of the reconcile: the
// freeze decision, the hosts served, the redirect, the Certificates and
// Secrets, the status entries and the routes all use this one set, so a
// lookup that starts failing, or a cache that empties, midway cannot change
// what one reconcile does. The result ends the snapshot.
func (c Config) beginOwnership(app *shpyrdv1.App) func() {
	if c.HostAvailability == nil {
		return func() {}
	}
	answers := hostAnswers{}
	for _, d := range app.Spec.Domains {
		h := normalizeHost(d)
		if h == "" {
			continue
		}
		if _, ok := answers[h]; !ok {
			answers[h] = c.askHost(app, h)
		}
	}
	key := snapshotKey(app)
	ownershipSnapshots.Store(key, answers)
	return func() { ownershipSnapshots.Delete(key) }
}

// hostAvailability is what the ownership rule says about a host the app
// lists in spec.domains. Without a lookup there is nothing to ask and the
// host is free (tests); the server always sets one. During a reconcile the
// answer is the one taken at its start; a host missing from that set is
// unknown, never free.
func (c Config) hostAvailability(app *shpyrdv1.App, host string) tenancy.Availability {
	if c.HostAvailability == nil {
		return tenancy.HostFree
	}
	if v, ok := ownershipSnapshots.Load(snapshotKey(app)); ok {
		if a, ok := v.(hostAnswers)[host]; ok {
			return a
		}
		return tenancy.HostUnknown
	}
	return c.askHost(app, host)
}

// askHost puts the question to the ownership rule. A name that is not a
// valid host (a wildcard, a malformed name, a URL) is HostInvalid without
// asking: it is refused for good, can never be free, and unlike an unknown
// answer it does not hold the routing of the app's other hosts. Only an
// answer that could not be obtained stays HostUnknown.
func (c Config) askHost(app *shpyrdv1.App, host string) tenancy.Availability {
	if !tenancy.ValidHost(host) {
		return tenancy.HostInvalid
	}
	return c.HostAvailability(workspaceOf(app), host)
}

// specHosts are the hosts of spec.domains that the project asks for beyond
// the ones its workspace gives it: normalised, without duplicates and
// without the workspace's own hosts, in the order of the spec.
func (c Config) specHosts(app *shpyrdv1.App) []string {
	seen := map[string]bool{}
	for _, h := range c.workspaceHosts(app) {
		seen[h] = true
	}
	var out []string
	for _, d := range app.Spec.Domains {
		h := normalizeHost(d)
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	return out
}

// hostRefusal is a host of spec.domains that the project may not serve.
type hostRefusal struct {
	Host    string
	Message string
}

// Messages of the status entry of a refused host. They say what rule
// applies and never whose host it is.
const (
	hostTakenMessage    = "this host belongs to another workspace and cannot be served by this project"
	hostPlatformMessage = "this host is a name of the platform and cannot be served by this project"
	hostInvalidMessage  = "this is not a valid hostname and cannot be served by this project"
)

// refusedHosts are the hosts of spec.domains the ownership rule refuses,
// with the message of each. The second result is false when the rule could
// not be asked for some host (the store is unreachable): the answer is then
// not to be acted on.
func (c Config) refusedHosts(app *shpyrdv1.App) ([]hostRefusal, bool) {
	var out []hostRefusal
	settled := true
	for _, h := range c.specHosts(app) {
		switch c.hostAvailability(app, h) {
		case tenancy.HostFree:
		case tenancy.HostTaken:
			out = append(out, hostRefusal{h, hostTakenMessage})
		case tenancy.HostPlatform:
			out = append(out, hostRefusal{h, hostPlatformMessage})
		case tenancy.HostInvalid:
			out = append(out, hostRefusal{h, hostInvalidMessage})
		default: // HostUnknown, and any value not known yet: never free
			settled = false
		}
	}
	return out, settled
}

// customDomains are the hosts the project added, normalised and without the
// default host (which is always served anyway), plus the app's other hosts
// in its workspace (RFC-0033 names: the shared name when a custom domain
// is primary, other verified custom domains). A host of spec.domains that
// another workspace owns, or that is a name of the platform, is not one of
// them, whatever path put it in the spec; one whose ownership could not be
// determined is left out too, and reconcile does not act on that answer
// (routingFrozen).
func (c Config) customDomains(app *shpyrdv1.App) []string {
	hosts := c.workspaceHosts(app)
	var out []string
	out = append(out, hosts[1:]...)
	for _, h := range c.specHosts(app) {
		if c.hostAvailability(app, h) == tenancy.HostFree {
			out = append(out, h)
		}
	}
	return dedupe(out, hosts[0])
}

// dedupe drops repeated hosts and the ones given as already seen.
func dedupe(hosts []string, seen ...string) []string {
	skip := map[string]bool{}
	for _, h := range seen {
		skip[h] = true
	}
	var out []string
	for _, h := range hosts {
		h = normalizeHost(h)
		if h == "" || skip[h] {
			continue
		}
		skip[h] = true
		out = append(out, h)
	}
	return out
}

type routingFrozenKey struct{}

// routingFrozen says this reconcile must not change what the app's hosts
// route to: the ownership of a host in its spec could not be determined.
// Nothing served is removed and nothing new is added until a later
// reconcile can ask.
func routingFrozen(ctx context.Context) bool {
	frozen, _ := ctx.Value(routingFrozenKey{}).(bool)
	return frozen
}

// sharedWildcard says the host is one label under the shared apps domain:
// the apps' wildcard certificate (copied into the namespace) and the
// wildcard record of the shared front door answer for it.
func (c Config) sharedWildcard(host string) bool {
	return c.Layout.UnderAppsDomain(host)
}

// domains are all hosts the Ingress serves: the default first, then the
// custom ones.
func (c Config) domains(app *shpyrdv1.App) []string {
	return append([]string{c.defaultHost(app)}, c.customDomains(app)...)
}

// underClusterDomain says the host is covered by the platform's wildcard
// certificate (a direct child of the cluster domain).
func (c Config) underClusterDomain(host string) bool {
	suffix := "." + c.Domain
	return strings.HasSuffix(host, suffix) && !strings.Contains(strings.TrimSuffix(host, suffix), ".")
}

// certificateSecretName is the TLS Secret of one host.
func certificateSecretName(app *shpyrdv1.App, host string) string {
	name := strings.NewReplacer(".", "-", "*", "wildcard").Replace(host)
	if len(name) > 40 {
		name = name[:40]
	}
	return app.Name + "-" + strings.Trim(name, "-") + "-tls"
}

// certificateHostLabel preserves existing short labels while bounding longer
// resource names to Kubernetes' label limit without losing their identity.
func certificateHostLabel(name string) string {
	if len(name) <= 63 {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	return strings.TrimRight(name[:50], "-_.") + "-" + hex.EncodeToString(sum[:6])
}

// ingressTLS is the TLS section of the project's Ingress: one entry per host.
// Hosts under the cluster domain use the wildcard when the front door serves
// it as its default certificate; every other host has its own Secret.
func (c Config) ingressTLS(app *shpyrdv1.App) []networkingv1.IngressTLS {
	var out []networkingv1.IngressTLS
	for _, h := range c.domains(app) {
		if c.sharedWildcard(h) {
			// The apps' wildcard, copied into the namespace.
			out = append(out, networkingv1.IngressTLS{Hosts: []string{h}, SecretName: AppsTLSSecretName})
			continue
		}
		if c.WildcardTLS && c.underClusterDomain(h) {
			out = append(out, networkingv1.IngressTLS{Hosts: []string{h}})
			continue
		}
		if c.underWorkspaceDomain(app, h) {
			// The workspace's wildcard, copied into the namespace (RFC-0033).
			out = append(out, networkingv1.IngressTLS{Hosts: []string{h}, SecretName: WorkspaceTLSSecretName})
			continue
		}
		out = append(out, networkingv1.IngressTLS{Hosts: []string{h}, SecretName: certificateSecretName(app, h)})
	}
	return out
}

// reconcileCertificates keeps one cert-manager Certificate per host that
// needs its own, and removes the ones for hosts that left.
func (r *AppReconciler) reconcileCertificates(ctx context.Context, app *shpyrdv1.App) error {
	if routingFrozen(ctx) {
		return nil
	}
	wanted := map[string]bool{}
	if hasWeb(app) {
		for _, h := range r.Config.domains(app) {
			if r.Config.sharedWildcard(h) || (r.Config.WildcardTLS && r.Config.underClusterDomain(h)) {
				continue
			}
			if r.Config.underWorkspaceDomain(app, h) {
				continue // the workspace wildcard covers it
			}
			name := certificateSecretName(app, h)
			wanted[name] = true
			if err := r.ensureCertificate(ctx, app, name, h, r.Config.ClusterIssuer); err != nil {
				return err
			}
		}
		// The internal name (#311), for a caller that insists on TLS: a
		// public CA cannot sign it, the platform's own does, and every
		// process trusts that CA through the bundle it is given.
		internal := internalHost(app)
		name := certificateSecretName(app, internal)
		wanted[name] = true
		if err := r.ensureCertificate(ctx, app, name, internal, install.CAClusterIssuer); err != nil {
			return err
		}
	}
	// A host the project may not serve keeps nothing: its TLS Secret goes
	// with its Certificate (which the sweep below removes), unless another
	// host's name shortens to the same Secret.
	refused, _ := r.Config.refusedHosts(app)
	for _, f := range refused {
		name := certificateSecretName(app, f.Host)
		if wanted[name] {
			continue
		}
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: app.Namespace}}
		if err := r.deleteIfExists(ctx, secret); err != nil {
			return err
		}
	}
	// Before RFC-0034 the Ingress annotation made cert-manager's ingress-shim
	// create <app>-tls for every host; that object outlives the annotation.
	legacy := &unstructured.Unstructured{}
	legacy.SetGroupVersionKind(CertificateGVK)
	if err := r.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: app.Name + "-tls"}, legacy); err == nil && !wanted[legacy.GetName()] && !metav1.IsControlledBy(legacy, app) {
		if err := r.Delete(ctx, legacy); err != nil && !apierrors.IsNotFound(err) {
			log.FromContext(ctx).Info("could not remove legacy certificate", "err", err.Error())
		}
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "CertificateList"})
	if err := r.List(ctx, list, client.InNamespace(app.Namespace), client.MatchingLabels{shpyrdv1.LabelApp: app.Name, shpyrdv1.LabelManagedBy: "shpyrd"}); err != nil {
		return nil // cert-manager may be absent in tests
	}
	for i := range list.Items {
		cert := &list.Items[i]
		if wanted[cert.GetName()] || !metav1.IsControlledBy(cert, app) {
			continue
		}
		if err := r.Delete(ctx, cert); err != nil && !apierrors.IsNotFound(err) {
			log.FromContext(ctx).Info("could not remove certificate", "name", cert.GetName(), "err", err.Error())
		}
	}
	return nil
}

// ensureCertificate keeps one Certificate of the app: a Secret of the
// given name for one host, signed by the ClusterIssuer named.
func (r *AppReconciler) ensureCertificate(ctx context.Context, app *shpyrdv1.App, name, host, issuer string) error {
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(CertificateGVK)
	cert.SetName(name)
	cert.SetNamespace(app.Namespace)
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, cert, func() error {
		cert.SetLabels(mergeMaps(cert.GetLabels(), map[string]string{shpyrdv1.LabelApp: app.Name, shpyrdv1.LabelManagedBy: "shpyrd", "shpyrd.io/host": certificateHostLabel(name)}))
		_ = unstructured.SetNestedField(cert.Object, name, "spec", "secretName")
		_ = unstructured.SetNestedStringSlice(cert.Object, []string{host}, "spec", "dnsNames")
		_ = unstructured.SetNestedMap(cert.Object, map[string]interface{}{"kind": "ClusterIssuer", "name": issuer}, "spec", "issuerRef")
		return controllerutil.SetControllerReference(app, cert, r.Scheme)
	}); err != nil {
		return fmt.Errorf("certificate for %s: %w", host, err)
	}
	return nil
}

// domainStatuses checks DNS and certificates of the custom domains. It
// returns true when something is still pending, so the reconcile is
// requeued.
func (r *AppReconciler) domainStatuses(ctx context.Context, app *shpyrdv1.App) ([]shpyrdv1.DomainStatus, bool) {
	hosts := r.Config.customDomains(app)
	if target := r.Config.requestedRedirect(app); target != "" {
		found := false
		for _, h := range hosts {
			found = found || h == target
		}
		if !found {
			hosts = append(hosts, target)
		}
	}
	refused, _ := r.Config.refusedHosts(app)
	if (len(hosts) == 0 && len(refused) == 0) || !hasWeb(app) {
		return nil, false
	}
	address := r.frontDoorAddress(ctx, app)
	target := r.Config.defaultHost(app)
	if r.Config.requestedRedirect(app) != "" {
		target = r.Config.platformHost(app)
	}
	pending := false
	var out []shpyrdv1.DomainStatus
	for _, h := range hosts {
		st := shpyrdv1.DomainStatus{Host: h, Target: target, Address: address}
		st.DNS = r.dnsState(ctx, h, target, address)
		if r.Config.sharedWildcard(h) || (r.Config.WildcardTLS && r.Config.underClusterDomain(h)) || r.Config.underWorkspaceDomain(app, h) {
			st.Certificate = CertWildcard
		} else {
			st.Certificate, st.Message = r.certificateState(ctx, app, h)
		}
		switch st.DNS {
		case DNSMissing:
			typ, value := RecordFor(h, target, address)
			st.Message = fmt.Sprintf("create a DNS record: %s %s -> %s", typ, h, value)
		case DNSWrong:
			typ, value := RecordFor(h, target, address)
			st.Message = fmt.Sprintf("%s points elsewhere: change it to %s -> %s", h, typ, value)
		}
		if st.DNS != DNSOK || (st.Certificate != CertReady && st.Certificate != CertWildcard) {
			pending = true
		}
		out = append(out, st)
	}
	// A refused host is neither looked up nor given a certificate: it only
	// says why.
	for _, f := range refused {
		out = append(out, shpyrdv1.DomainStatus{Host: statusHost(f.Host), Target: target, Address: address, DNS: DNSUnknown, Certificate: CertFailed, Message: f.Message})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out, pending
}

// statusHost is a refused host as the status shows it: the name as stored,
// with every byte that is not printable ASCII replaced by "?" and cut at
// 253 bytes, so a malformed entry cannot put control characters or an
// unbounded string in the status.
func statusHost(h string) string {
	b := []byte(h)
	if len(b) > 253 {
		b = b[:253]
	}
	for i, c := range b {
		if c < 0x21 || c > 0x7e {
			b[i] = '?'
		}
	}
	return string(b)
}

// RecordFor is the record that points host at a project: a CNAME to the
// project's hostname (target), except at a zone apex, a root domain such as
// example.com or acme.co.uk, which cannot carry a CNAME and points at the
// front door's address instead (ApexRecordType).
func RecordFor(host, target, address string) (typ, value string) {
	if IsApex(host) {
		return ApexRecordType(address), address
	}
	return "CNAME", target
}

// IsApex says host is a root domain: the registrable name itself, by the
// public suffix list, not a name under it.
func IsApex(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	root, err := publicsuffix.EffectiveTLDPlusOne(host)
	return err == nil && root == host
}

// ApexRecordType is the record a zone apex points at the front door with:
// A where the load balancer has addresses (one or several, comma-separated),
// ALIAS where it has a hostname only. With no address known, A: the most
// common, its value for the operator to give.
func ApexRecordType(address string) string {
	if strings.TrimSpace(address) == "" {
		return "A"
	}
	for _, a := range strings.Split(address, ",") {
		if net.ParseIP(strings.TrimSpace(a)) == nil {
			return "ALIAS"
		}
	}
	return "A"
}

// frontDoorAddress is the address a custom domain must resolve to: the
// internal load balancer for internal projects, else the external one.
func (r *AppReconciler) frontDoorAddress(ctx context.Context, app *shpyrdv1.App) string {
	if app.Spec.Exposure == "internal" && r.Config.InternalLBAddress != "" {
		return r.Config.InternalLBAddress
	}
	if r.Config.ExternalLBAddress != "" {
		return r.Config.ExternalLBAddress
	}
	if r.LookupLB != nil {
		return r.LookupLB(ctx)
	}
	return ""
}

// dnsState resolves host and compares with the front door: a CNAME to the
// project's hostname or an A record to its address both count.
func (r *AppReconciler) dnsState(ctx context.Context, host, target, address string) string {
	resolver := r.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if cname, err := resolver.LookupCNAME(lctx, host); err == nil {
		cname = strings.TrimSuffix(strings.ToLower(cname), ".")
		if cname == strings.ToLower(target) {
			return DNSOK
		}
	}
	ips, err := resolver.LookupIPAddr(lctx, host)
	if err != nil {
		var dnsErr *net.DNSError
		if asDNSError(err, &dnsErr) && dnsErr.IsNotFound {
			return DNSMissing
		}
		return DNSUnknown
	}

	targetIPs := map[string]bool{}
	for _, a := range strings.Split(address, ",") {
		if ip := net.ParseIP(strings.TrimSpace(a)); ip != nil {
			targetIPs[ip.String()] = true
		}
	}
	if tips, err := resolver.LookupIPAddr(lctx, target); err == nil {
		for _, ip := range tips {
			targetIPs[ip.IP.String()] = true
		}
	}
	if len(targetIPs) == 0 {
		return DNSUnknown
	}
	for _, ip := range ips {
		if targetIPs[ip.IP.String()] {
			return DNSOK
		}
	}
	return DNSWrong
}

func asDNSError(err error, out **net.DNSError) bool {
	for e := err; e != nil; {
		if d, ok := e.(*net.DNSError); ok {
			*out = d
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// certificateState reads the host's Certificate.
func (r *AppReconciler) certificateState(ctx context.Context, app *shpyrdv1.App, host string) (string, string) {
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(CertificateGVK)
	if err := r.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: certificateSecretName(app, host)}, cert); err != nil {
		return CertIssuing, "certificate requested"
	}
	conds, _, _ := unstructured.NestedSlice(cert.Object, "status", "conditions")
	var ready, issuing bool
	msg := ""
	for _, c := range conds {
		m, _ := c.(map[string]interface{})
		t, _ := m["type"].(string)
		s, _ := m["status"].(string)
		message, _ := m["message"].(string)
		reason, _ := m["reason"].(string)
		switch t {
		case "Ready":
			ready = s == "True"
			if !ready && message != "" {
				msg = message
			}
		case "Issuing":
			issuing = s == "True"
			if message != "" {
				msg = message
			}
			if reason == "Failed" {
				return CertFailed, shortMessage(message)
			}
		}
	}
	if ready {
		return CertReady, ""
	}
	_ = issuing
	return CertIssuing, shortMessage(firstNonEmpty(msg, "waiting for the certificate authority"))
}

// reconcileMovedHosts creates a 301 Ingress for each host the project used to
// serve (stored in AnnotationMovedHosts after a rename), removing the entry
// once the redirect has been live for 30 days.
func (r *AppReconciler) reconcileMovedHosts(ctx context.Context, app *shpyrdv1.App) error {
	raw := app.Annotations[shpyrdv1.AnnotationMovedHosts]
	if raw == "" {
		return nil
	}
	var hosts []movedHost
	if err := json.Unmarshal([]byte(raw), &hosts); err != nil {
		return nil // malformed — clear it
	}
	now := time.Now()
	keep := hosts[:0:0]
	for _, h := range hosts {
		if now.Sub(h.At) < 30*24*time.Hour {
			if err := r.ensureRedirectIngress(ctx, app, h.Host); err != nil {
				return err
			}
			keep = append(keep, h)
		} else {
			// Past 30 days: delete the redirect Ingress if we created it.
			_ = r.deleteIfExists(ctx, &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{
				Name: redirectIngressName(app, h.Host), Namespace: app.Namespace,
			}})
		}
	}
	patch := client.MergeFrom(app.DeepCopy())
	if len(keep) == 0 {
		delete(app.Annotations, shpyrdv1.AnnotationMovedHosts)
	} else {
		b, _ := json.Marshal(keep)
		app.Annotations[shpyrdv1.AnnotationMovedHosts] = string(b)
	}
	if err := r.Patch(ctx, app, patch); err != nil {
		return client.IgnoreNotFound(err)
	}
	return nil
}

type movedHost struct {
	Host string    `json:"host"`
	At   time.Time `json:"at"`
}

func redirectIngressName(app *shpyrdv1.App, host string) string {
	sum := sha256.Sum256([]byte(host))
	return ingressName(app) + "-moved-" + hex.EncodeToString(sum[:])[:8]
}

// Redirects are answered by the server, not by nginx (#133): ingress-nginx
// refuses an nginx variable in permanent-redirect, and without
// $request_uri the path is lost. A redirect Ingress rewrites each request
// to RedirectPath on the server with its path after it (nginx keeps the
// query), and the server reads where to send it from the
// AnnotationRedirectTo of the Ingress labelled LabelRedirect that serves
// the request's host.
const (
	RedirectPath           = "/_shpyrd/redirect"
	AnnotationRedirectTo   = "shpyrd.io/redirect-to"
	LabelRedirect          = "shpyrd.io/redirect"
	redirectServiceName    = "shpyrd-redirect"
	nginxPermanentRedirect = "nginx.ingress.kubernetes.io/permanent-redirect"
)

// routeRedirect turns ing into a redirect to target (https://host[:port])
// answered by the server: every path of each host, captured whole.
func routeRedirect(ing *networkingv1.Ingress, target string) {
	// From earlier versions; the webhook refuses them.
	delete(ing.Annotations, nginxPermanentRedirect)
	delete(ing.Annotations, nginxPermanentRedirect+"-code")
	ing.Annotations["nginx.ingress.kubernetes.io/use-regex"] = "true"
	ing.Annotations["nginx.ingress.kubernetes.io/rewrite-target"] = RedirectPath + "/$1"
	ing.Annotations[AnnotationRedirectTo] = target
	ing.Labels = mergeMaps(ing.Labels, map[string]string{LabelRedirect: "true"})
	// A regular expression is no Prefix path to the webhook.
	pathType := networkingv1.PathTypeImplementationSpecific
	for i := range ing.Spec.Rules {
		ing.Spec.Rules[i].HTTP = &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{
			Path: "/(.*)", PathType: &pathType,
			Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
				Name: redirectServiceName, Port: networkingv1.ServiceBackendPort{Name: "http"},
			}},
		}}}
	}
}

// ensureRedirectService is the server's alias in the app's namespace that
// redirect Ingresses route to.
func (r *AppReconciler) ensureRedirectService(ctx context.Context, app *shpyrdv1.App) error {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: redirectServiceName, Namespace: app.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		r.Config.mutateEdgeService(app, svc)
		return controllerutil.SetControllerReference(app, svc, r.Scheme)
	})
	return err
}

// ensureRedirectIngress creates or updates a redirect Ingress from the old
// host to the app's current default host.
func (r *AppReconciler) ensureRedirectIngress(ctx context.Context, app *shpyrdv1.App, oldHost string) error {
	newHost := r.Config.defaultHost(app)
	if target := r.Config.redirectTarget(app); target != "" {
		newHost = target
	}
	if newHost == "" || newHost == oldHost {
		return nil
	}
	if err := r.ensureRedirectService(ctx, app); err != nil {
		return err
	}
	name := redirectIngressName(app, oldHost)
	class := r.Config.IngressClassExternal
	if class == "" {
		class = "nginx"
	}
	pathType := networkingv1.PathTypePrefix
	ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: app.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ing, func() error {
		ing.Labels = mergeMaps(ing.Labels, map[string]string{
			shpyrdv1.LabelManagedBy: "shpyrd",
			shpyrdv1.LabelApp:       app.Name,
		})
		ing.Annotations = mergeMaps(ing.Annotations, map[string]string{
			"nginx.ingress.kubernetes.io/ssl-redirect": "true",
		})
		ing.Spec.IngressClassName = &class
		// The old host's certificate: the apps' wildcard under the shared
		// apps domain (its record too), the front door's default otherwise.
		ing.Spec.TLS = nil
		if r.Config.sharedWildcard(oldHost) {
			ing.Spec.TLS = []networkingv1.IngressTLS{{Hosts: []string{oldHost}, SecretName: AppsTLSSecretName}}
		}
		r.Config.dnsAnnotations(app, ing, []string{oldHost})
		ing.Spec.Rules = []networkingv1.IngressRule{{
			Host: oldHost,
			IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
				Paths: []networkingv1.HTTPIngressPath{{Path: "/", PathType: &pathType,
					Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
						Name: workloadName(app, "web"), Port: networkingv1.ServiceBackendPort{Name: "http"},
					}}}},
			}},
		}}
		routeRedirect(ing, "https://"+newHost)
		return controllerutil.SetControllerReference(app, ing, r.Scheme)
	})
	return err
}

// platformHost remains the DNS target even when content redirects.
func (c Config) platformHost(app *shpyrdv1.App) string {
	hosts := c.Layout.AppHosts(c.workspaceAddress(workspaceOf(app)), nil, c.Domain, projectSlug(app))
	return hosts[0]
}

func (c Config) requestedRedirect(app *shpyrdv1.App) string {
	if app.Spec.RedirectTo != "" {
		for _, h := range app.Spec.Domains {
			if h == app.Spec.RedirectTo && c.hostAvailability(app, normalizeHost(h)) == tenancy.HostFree {
				return h
			}
		}
		return ""
	}
	if c.WorkspaceRedirectDomain != nil {
		if domain := c.WorkspaceRedirectDomain(workspaceOf(app)); domain != "" {
			return projectSlug(app) + "." + domain
		}
	}
	return ""
}

func (c Config) redirectTarget(app *shpyrdv1.App) string {
	if maintenance(app) {
		return ""
	}
	target := c.requestedRedirect(app)
	if target == "" || len(validation.IsDNS1123Subdomain(target)) != 0 || target == c.platformHost(app) {
		return ""
	}
	served := false
	for _, h := range c.domains(app) {
		served = served || h == target
	}
	if !served {
		return ""
	}
	for _, d := range app.Status.Domains {
		if d.Host == target && d.DNS == DNSOK && (d.Certificate == CertReady || d.Certificate == CertWildcard) {
			return target
		}
	}
	return ""
}

func (c Config) servingDomains(app *shpyrdv1.App) []string {
	hosts := c.domains(app)
	if c.redirectTarget(app) == "" {
		return hosts
	}
	source := c.platformHost(app)
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		if h != source {
			out = append(out, h)
		}
	}
	return out
}

// reconcileDomainRedirect keeps the platform DNS/TLS while redirecting only /.
// The separate /.shpyrd/ ingress retains sign-in and callback endpoints.
func (r *AppReconciler) reconcileDomainRedirect(ctx context.Context, app *shpyrdv1.App, enabled bool) error {
	ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: ingressName(app) + "-domain-redirect", Namespace: app.Namespace}}
	target := r.Config.redirectTarget(app)
	if !enabled || target == "" {
		return r.deleteIfExists(ctx, ing)
	}
	if err := r.ensureRedirectService(ctx, app); err != nil {
		return err
	}
	source := r.Config.platformHost(app)
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ing, func() error {
		r.Config.mutateIngress(app, ing, false, false)
		for _, key := range edgeAnnotationKeys {
			delete(ing.Annotations, key)
		}
		host := target
		if r.Config.HTTPSPort != "" && r.Config.HTTPSPort != "443" {
			host += ":" + r.Config.HTTPSPort
		}
		// Reuse the app's class, but only the platform host.
		rule := ing.Spec.Rules[0]
		rule.Host = source
		ing.Spec.Rules = []networkingv1.IngressRule{rule}
		routeRedirect(ing, "https://"+host)
		tls := ing.Spec.TLS[:0:0]
		for _, entry := range ing.Spec.TLS {
			for _, h := range entry.Hosts {
				if h == source {
					tls = append(tls, entry)
				}
			}
		}
		ing.Spec.TLS = tls
		r.Config.dnsAnnotations(app, ing, []string{source})
		return controllerutil.SetControllerReference(app, ing, r.Scheme)
	})
	return err
}
