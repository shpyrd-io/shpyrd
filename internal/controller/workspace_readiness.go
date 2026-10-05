package controller

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Readiness of a workspace's door. A workspace is a row the moment it is
// created; its address answers only once the front door has an Ingress,
// the certificate is issued, the name is published on public DNS and the
// door answers over HTTPS. Until then a link to it leads nowhere — and a
// resolver that was asked too early remembers the "no such name" answer
// for as long as the zone says (thirty minutes on OCI). So the controller
// looks, records what it saw on the workspace, and the invitation to the
// first owner waits for the moment every check passes (RFC-0033).
//
// Two checks need the outside world: the name on public DNS and the door
// over HTTPS. A platform that publishes no public names (a kind cluster
// with dnsmasq; no DNS provider) cannot make them, and says so instead of
// failing.

// Names of the checks, as the API and the CLI show them.
const (
	CheckFrontDoor   = "front door"
	CheckCertificate = "certificate"
	CheckPublicName  = "public name"
	CheckDoorAnswers = "door answers"
)

// readinessRetry is how soon the pass runs again while a workspace is not
// ready: a new workspace is watched closely, the rest every workspaceSync.
const readinessRetry = 15 * time.Second

// readinessChecker is what the checks need beyond the Kubernetes client,
// so tests can stand in for the network.
type readinessChecker struct {
	// authoritative resolves a host at the zone's own name servers, so no
	// cache in between — ours or anyone's — can hold a stale "no such
	// name". nil: the default implementation.
	authoritative func(ctx context.Context, host string) ([]net.IP, error)
	// door fetches https://<host>/api/healthz through the front door at
	// address (the load balancer), verifying the certificate for host.
	// nil: the default implementation.
	door func(ctx context.Context, host, address string) error
}

// checkReadiness looks at one workspace's door and records what it saw.
// It returns whether the door is ready.
func (r *WorkspaceReconciler) checkReadiness(ctx context.Context, ws *store.Workspace) bool {
	logger := log.FromContext(ctx)
	now := time.Now()
	var checks []store.ReadinessCheck
	add := func(name string, ok bool, detail string) {
		checks = append(checks, store.ReadinessCheck{Name: name, OK: ok, Detail: detail})
	}

	// A workspace of the shared layout (RFC-0033 names) answers through
	// the shared front door, with the workspaces' wildcard, and its apps
	// under the apps domain; any other through a door of its own.
	door, secret := workspaceFrontDoorName(ws.Slug), workspaceFrontDoorName(ws.Slug)+"-tls"
	var probe [4]byte
	_, _ = rand.Read(probe[:])
	appProbe := "probe-" + hex.EncodeToString(probe[:]) + "." + ws.Address
	label, shared := r.Config.Layout.Label(ws.Address)
	if shared {
		door, secret = workspacesFrontDoorName, WorkspacesWildcardSecretName
		appProbe = r.Config.Layout.AppHost(label, "probe-"+hex.EncodeToString(probe[:]))
	}

	// The front door: the Ingress exists and, where a load balancer
	// publishes its address on it, that address is there.
	lb := r.frontDoorAddress(ctx)
	ing := &networkingv1.Ingress{}
	switch err := r.Get(ctx, client.ObjectKey{Namespace: r.Config.SystemNamespace, Name: door}, ing); {
	case apierrors.IsNotFound(err):
		add(CheckFrontDoor, false, "the front door is not published yet")
	case err != nil:
		add(CheckFrontDoor, false, "front door: "+err.Error())
	case lb != "" && len(ing.Status.LoadBalancer.Ingress) == 0:
		add(CheckFrontDoor, false, "waiting for the front door's address")
	default:
		add(CheckFrontDoor, true, "")
	}

	// The certificate: the platform's wildcard covers an address directly
	// under the platform domain; every other workspace has its own, and
	// the Secret is the proof it was issued.
	if !shared && r.Config.WildcardTLS && r.Config.underClusterDomain(ws.Address) {
		add(CheckCertificate, true, "the platform's wildcard")
	} else {
		sec := &corev1.Secret{}
		switch err := r.Get(ctx, client.ObjectKey{Namespace: r.Config.SystemNamespace, Name: secret}, sec); {
		case err == nil && len(sec.Data["tls.crt"]) > 0:
			add(CheckCertificate, true, "")
		case err != nil && !apierrors.IsNotFound(err):
			add(CheckCertificate, false, "certificate: "+err.Error())
		default:
			add(CheckCertificate, false, r.certificateDetail(ctx, secret))
		}
	}

	// The outside world: only a platform that publishes its names can ask.
	if !r.Config.PublicChecks {
		add(CheckPublicName, true, "not checked: no DNS provider")
		add(CheckDoorAnswers, true, "not checked: no DNS provider")
	} else {
		add(r.publicName(ctx, []string{ws.Address, appProbe}, lb))
		add(r.doorAnswers(ctx, ws.Address, lb))
	}

	ready := true
	for _, c := range checks {
		ready = ready && c.OK
	}
	readiness := store.WorkspaceReadiness{Ready: ready, CheckedAt: now, Checks: checks}
	if sameReadiness(ws.Readiness, readiness) && ws.Readiness != nil && now.Sub(ws.Readiness.CheckedAt) < 10*time.Minute {
		return ready // nothing new to write
	}
	if _, err := r.Store.SetWorkspaceReadiness(ctx, ws.Slug, readiness); err != nil {
		logger.Error(err, "workspace readiness not recorded", "workspace", ws.Slug)
		return ready
	}
	if ready && ws.ReadyAt == nil {
		logger.Info("workspace door answers", "workspace", ws.Slug, "address", ws.Address)
	}
	return ready
}

// sameReadiness says two looks saw the same thing (the time aside).
func sameReadiness(a *store.WorkspaceReadiness, b store.WorkspaceReadiness) bool {
	if a == nil || a.Ready != b.Ready || len(a.Checks) != len(b.Checks) {
		return false
	}
	for i := range a.Checks {
		if a.Checks[i] != b.Checks[i] {
			return false
		}
	}
	return true
}

// frontDoorAddress is the public load balancer's address, "" when there
// is none to know of (kind, a first install before the balancer exists).
func (r *WorkspaceReconciler) frontDoorAddress(ctx context.Context) string {
	if r.Config.ExternalLBAddress != "" {
		return r.Config.ExternalLBAddress
	}
	if r.LookupLB != nil {
		return r.LookupLB(ctx)
	}
	return ""
}

// certificateDetail reads why the Certificate is not issued yet, in a
// few words, from cert-manager's conditions.
func (r *WorkspaceReconciler) certificateDetail(ctx context.Context, name string) string {
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(CertificateGVK)
	if err := r.Get(ctx, client.ObjectKey{Namespace: r.Config.SystemNamespace, Name: name}, cert); err != nil {
		return "certificate requested"
	}
	conds, _, _ := unstructured.NestedSlice(cert.Object, "status", "conditions")
	msg := ""
	for _, c := range conds {
		m, _ := c.(map[string]interface{})
		t, _ := m["type"].(string)
		message, _ := m["message"].(string)
		reason, _ := m["reason"].(string)
		if t == "Issuing" && reason == "Failed" {
			return "certificate failed: " + shortMessage(message)
		}
		if message != "" {
			msg = message
		}
	}
	return "certificate " + shortMessage(firstNonEmpty(msg, "waiting for the certificate authority"))
}

// publicName asks the zones' own name servers for the address and for a
// name where an app would answer (the apps' wildcard record), and compares
// with the front door.
func (r *WorkspaceReconciler) publicName(ctx context.Context, hosts []string, lb string) (string, bool, string) {
	resolve := r.checker.authoritative
	if resolve == nil {
		resolve = lookupAuthoritative
	}
	for _, host := range hosts {
		ips, err := resolve(ctx, host)
		switch {
		case isNotFound(err):
			return CheckPublicName, false, host + " is not published yet"
		case err != nil:
			return CheckPublicName, false, host + ": " + err.Error()
		case lb != "" && !pointsAt(ips, lb):
			return CheckPublicName, false, host + " points elsewhere: " + joinIPs(ips)
		}
	}
	return CheckPublicName, true, ""
}

// doorAnswers fetches the door's health through the front door, as a
// browser would once the name resolves: TLS for the address, a 200.
func (r *WorkspaceReconciler) doorAnswers(ctx context.Context, address, lb string) (string, bool, string) {
	fetch := r.checker.door
	if fetch == nil {
		fetch = fetchDoor
	}
	if err := fetch(ctx, address, lb); err != nil {
		return CheckDoorAnswers, false, err.Error()
	}
	return CheckDoorAnswers, true, ""
}

// lookupAuthoritative resolves host at the name servers of the nearest
// zone that has them, so the answer is what the zone publishes now.
func lookupAuthoritative(ctx context.Context, host string) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ns, err := zoneNameServers(ctx, host)
	if err != nil {
		return nil, err
	}
	var last error
	for _, server := range ns {
		res := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, net.JoinHostPort(server, "53"))
		}}
		addrs, err := res.LookupIPAddr(ctx, host)
		if err == nil {
			ips := make([]net.IP, 0, len(addrs))
			for _, a := range addrs {
				ips = append(ips, a.IP)
			}
			return ips, nil
		}
		last = err
		if isNotFound(err) {
			return nil, err // the zone itself says no: ask nobody else
		}
	}
	return nil, last
}

// zoneNameServers finds the NS records of host or of the closest parent
// that has them.
func zoneNameServers(ctx context.Context, host string) ([]string, error) {
	labels := strings.Split(strings.TrimSuffix(host, "."), ".")
	for i := 0; i < len(labels)-1; i++ {
		zone := strings.Join(labels[i:], ".")
		ns, err := net.DefaultResolver.LookupNS(ctx, zone)
		if err == nil && len(ns) > 0 {
			out := make([]string, 0, len(ns))
			for _, n := range ns {
				out = append(out, strings.TrimSuffix(n.Host, "."))
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("no name servers found for %s", host)
}

// fetchDoor is doorAnswers' default: GET https://<host>/api/healthz with
// the connection made to the front door's address (the public name may
// not resolve from inside the cluster yet) and the certificate verified
// for the host. Without an address, the name is used as a browser would.
func fetchDoor(ctx context.Context, host, address string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if address != "" {
				_, port, _ := net.SplitHostPort(addr)
				addr = net.JoinHostPort(strings.Split(address, ",")[0], firstNonEmpty(port, "443"))
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
	defer transport.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/api/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		var certErr *tls.CertificateVerificationError
		if errors.As(err, &certErr) {
			return fmt.Errorf("the certificate served for %s is not valid yet: %v", host, certErr.Err)
		}
		return fmt.Errorf("https://%s does not answer: %v", host, unwrapURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("https://%s/api/healthz answered %d", host, resp.StatusCode)
	}
	return nil
}

func unwrapURLError(err error) error {
	for {
		u, ok := err.(interface{ Unwrap() error })
		if !ok || u.Unwrap() == nil {
			return err
		}
		err = u.Unwrap()
	}
}

func isNotFound(err error) bool {
	var dnsErr *net.DNSError
	return err != nil && asDNSError(err, &dnsErr) && dnsErr.IsNotFound
}

func pointsAt(ips []net.IP, address string) bool {
	want := map[string]bool{}
	for _, a := range strings.Split(address, ",") {
		want[strings.TrimSpace(a)] = true
	}
	for _, ip := range ips {
		if want[ip.String()] {
			return true
		}
	}
	return false
}

func joinIPs(ips []net.IP) string {
	parts := make([]string, 0, len(ips))
	for _, ip := range ips {
		parts = append(parts, ip.String())
	}
	return strings.Join(parts, ", ")
}
