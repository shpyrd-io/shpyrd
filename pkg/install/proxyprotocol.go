package install

import (
	"fmt"
	"net/netip"
	"strings"
)

// The public load balancer of the oci profile can pass each client's address
// to the external ingress controller in a PROXY protocol header, so the
// platform sees who is connecting instead of the load balancer. It is one
// switch, SHPYRD_PROXY_PROTOCOL, and it changes only the external
// controller; SHPYRD_LB_SUBNET_CIDR says whose header is believed.
const (
	VarProxyProtocol = "SHPYRD_PROXY_PROTOCOL" // "true" or "false" (default): the external controller expects a PROXY header
	VarLBSubnetCIDR  = "SHPYRD_LB_SUBNET_CIDR" // range of the public load balancer's subnet: the only source of PROXY headers believed

	// ExternalIngressComponent and InternalIngressComponent are the two
	// ingress controllers of a profile.
	ExternalIngressComponent = "ingress-nginx"
	InternalIngressComponent = "ingress-nginx-internal"

	// ProxyProtocolAnnotation asks the OCI load balancer to send a PROXY
	// header (version 2) to its backends on every listener.
	ProxyProtocolAnnotation = "service.beta.kubernetes.io/oci-load-balancer-connection-proxy-protocol-version"
)

// resolveProxyProtocol decides whether the external controller expects PROXY
// protocol on this install. It returns the effective value ("true" or
// "false"), the CIDR to believe when on, and a notice when the request was
// not honoured. The feature is on only for the oci profile with the
// internal controller in the install, that is, in the profile and not
// skipped: without it the external controller is the door of internal
// names, which must stay plain. A run limited to some components passes
// keep, the state the cluster has, and never changes it; --only does not
// make a component "left out", so it does not change what a render holds.
// An error is returned for a value that is neither "true" nor "false", or
// when the feature would be on, the run applies the external controller and
// the subnet range is missing or is not a range: nothing is then rendered
// that believes every sender.
func resolveProxyProtocol(profile string, vars map[string]string, internalInInstall bool, keep *bool, externalApplied bool) (on bool, cidr string, notice string, err error) {
	asked := false
	switch vars[VarProxyProtocol] {
	case "", "false":
	case "true":
		asked = true
	default:
		return false, "", "", fmt.Errorf("%s must be \"true\" or \"false\", got %q", VarProxyProtocol, vars[VarProxyProtocol])
	}
	if profile != "oci" {
		if asked {
			return false, "", fmt.Sprintf("%s=true is ignored: only the oci profile has a load balancer that sends the PROXY header; the front door is unchanged", VarProxyProtocol), nil
		}
		return false, "", "", nil
	}
	if keep != nil {
		state := map[bool]string{true: "on", false: "off"}[*keep]
		if *keep {
			prefix, perr := netip.ParsePrefix(strings.TrimSpace(vars[VarLBSubnetCIDR]))
			if perr == nil && prefix.Bits() != 0 {
				cidr = prefix.Masked().String()
			} else if externalApplied {
				return false, "", "", fmt.Errorf("the external ingress controller has PROXY protocol on and this run applies it, so %s must be the public load balancer subnet's range (for example 10.0.10.0/24), got %q: the PROXY header is believed only from that range", VarLBSubnetCIDR, vars[VarLBSubnetCIDR])
			}
		}
		if asked != *keep {
			notice = fmt.Sprintf("%s=%t is not applied: this run applies only some of the components, so the front door stays %s as the external ingress controller has it. A cluster init without --only that applies both %s and shpyrd makes the change", VarProxyProtocol, asked, state, ExternalIngressComponent)
		}
		return *keep, cidr, notice, nil
	}
	if !asked {
		return false, "", "", nil
	}
	if !internalInInstall {
		return false, "", fmt.Sprintf("%s stays off: the internal ingress controller (%s) is not part of this install, so the external controller also answers internal names, which must stay plain", VarProxyProtocol, InternalIngressComponent), nil
	}
	prefix, perr := netip.ParsePrefix(strings.TrimSpace(vars[VarLBSubnetCIDR]))
	if perr != nil || prefix.Bits() == 0 {
		return false, "", "", fmt.Errorf("%s=true needs %s to be the public load balancer subnet's range (for example 10.0.10.0/24), got %q: the PROXY header is believed only from that range", VarProxyProtocol, VarLBSubnetCIDR, vars[VarLBSubnetCIDR])
	}
	return true, prefix.Masked().String(), "", nil
}

// proxyProtocolValues turns on, in the external controller's chart values,
// what the PROXY header needs: the load balancer sends it, nginx reads it
// and believes it only from the load balancer's subnet. The traffic policy
// is Local so that nginx sees the load balancer's own address as the peer
// (a node address would put the whole node subnet in the trusted range, and
// a pod connecting to a node port would be believed); a pod's connection
// keeps its pod address, which is outside the range.
func proxyProtocolValues(vals map[string]interface{}, cidr string) {
	controller := table(vals, "controller")
	service := table(controller, "service")
	annotations := table(service, "annotations")
	annotations[ProxyProtocolAnnotation] = "2"
	service["externalTrafficPolicy"] = "Local"
	config := table(controller, "config")
	config["use-proxy-protocol"] = "true"
	config["proxy-real-ip-cidr"] = cidr
}

// ProxyProtocolOn says whether the external controller is installed to
// expect PROXY protocol.
func (e *Engine) ProxyProtocolOn() bool { return e.vars[VarProxyProtocol] == "true" }

// Notices are what the installer has to say about the settings it
// resolved, for the person running it.
func (e *Engine) Notices() []string { return append([]string(nil), e.notices...) }
