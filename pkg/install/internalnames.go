package install

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Internal names (#311): http://<project>.internal between the projects of
// a workspace. Every *.internal name resolves, inside the cluster, to the
// internal front door; the front door and the server decide who may call
// whom. The names come from CoreDNS: a server block for the zone, kept in
// the Corefile between two marks, which the hook writes before the server
// is applied and rewrites when the front door's address changes.

// InternalNamesZone is the DNS zone of internal names.
const InternalNamesZone = "internal"

// VarInternalNamesIngressClass is the ingress class of the Ingress that
// catches the bare names, *.internal: the internal controller's where there
// is one, the external's otherwise (derived).
const VarInternalNamesIngressClass = "SHPYRD_INTERNAL_NAMES_INGRESS_CLASS"

const (
	internalNamesBegin = "# shpyrd internal names (begin): <project>.internal resolves to the internal front door; the platform decides who may call whom. Managed by `shpyrd cluster apply`."
	internalNamesEnd   = "# shpyrd internal names (end)"
)

// internalNamesBlock is the Corefile server block for the zone: every A
// query answered with the front door's address, every other query answered
// empty (not an error, so a resolver asking for AAAA first goes on to A).
func internalNamesBlock(ip string) string {
	return strings.Join([]string{
		internalNamesBegin,
		InternalNamesZone + ":53 {",
		"    errors",
		"    template IN A " + InternalNamesZone + " {",
		fmt.Sprintf("        answer \"{{ .Name }} 60 IN A %s\"", ip),
		"    }",
		"    template IN ANY " + InternalNamesZone + " {",
		"        rcode NOERROR",
		fmt.Sprintf("        authority \"%s. 60 IN SOA ns.%s. hostmaster.%s. 1 7200 3600 1209600 60\"", InternalNamesZone, InternalNamesZone, InternalNamesZone),
		"    }",
		"}",
		internalNamesEnd,
	}, "\n")
}

// WithInternalNames returns the Corefile with the zone's block answering
// ip: replaced where one is, appended otherwise. The rest of the file is
// left as it was.
func WithInternalNames(corefile, ip string) string {
	block := internalNamesBlock(ip)
	if b := strings.Index(corefile, internalNamesBegin); b >= 0 {
		if e := strings.Index(corefile[b:], internalNamesEnd); e >= 0 {
			return corefile[:b] + block + corefile[b+e+len(internalNamesEnd):]
		}
	}
	return strings.TrimRight(corefile, "\n") + "\n" + block + "\n"
}

// internalNamesDoorIP is the cluster address the names resolve to: the
// internal ingress controller's Service, or the external's on a cluster
// with one front door.
func (e *Engine) internalNamesDoorIP(ctx context.Context) (string, error) {
	for _, svc := range []struct{ ns, name string }{{"ingress-nginx-internal", "ingress-nginx-internal-controller"}, {"ingress-nginx", "ingress-nginx-controller"}} {
		s, err := e.kube.Kube.CoreV1().Services(svc.ns).Get(ctx, svc.name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if s.Spec.ClusterIP != "" && s.Spec.ClusterIP != "None" {
			return s.Spec.ClusterIP, nil
		}
	}
	return "", nil
}

// internalNamesDNSHook writes the zone into CoreDNS's Corefile. A cluster
// whose DNS is not CoreDNS in kube-system is told what to resolve instead;
// the names then wait for the operator, nothing else does.
func internalNamesDNSHook(ctx context.Context, e *Engine, c *Component) error {
	ip, err := e.internalNamesDoorIP(ctx)
	if err != nil {
		return fmt.Errorf("internal names: %w", err)
	}
	if ip == "" {
		e.rep.Step(c.Name, "internal names: no ingress controller Service yet; *.internal resolves after the next apply")
		return nil
	}
	cm, err := e.kube.Kube.CoreV1().ConfigMaps("kube-system").Get(ctx, "coredns", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		e.rep.Step(c.Name, fmt.Sprintf("internal names: no coredns ConfigMap in kube-system; make *.%s resolve to %s in the cluster's DNS for projects to call each other by name", InternalNamesZone, ip))
		return nil
	}
	if err != nil {
		return fmt.Errorf("internal names: %w", err)
	}
	corefile, ok := cm.Data["Corefile"]
	if !ok {
		e.rep.Step(c.Name, fmt.Sprintf("internal names: the coredns ConfigMap holds no Corefile; make *.%s resolve to %s in the cluster's DNS", InternalNamesZone, ip))
		return nil
	}
	updated := WithInternalNames(corefile, ip)
	if updated == corefile {
		return nil
	}
	cm.Data["Corefile"] = updated
	if _, err := e.kube.Kube.CoreV1().ConfigMaps("kube-system").Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("internal names: writing the Corefile: %w", err)
	}
	e.rep.Step(c.Name, fmt.Sprintf("internal names: *.%s resolves to the front door at %s", InternalNamesZone, ip))
	return nil
}
