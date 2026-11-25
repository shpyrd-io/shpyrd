package install

import (
	"reflect"
	"strings"
	"testing"

	"github.com/shpyrd-io/shpyrd/deploy"
)

func ociVars(extra map[string]string) map[string]string {
	v := map[string]string{VarDomain: "oci.example.com", VarACMEEmail: "ops@example.com"}
	for k, x := range extra {
		v[k] = x
	}
	return v
}

func controllerValues(t *testing.T, profile string, vars map[string]string, opts Options, component string) (*Engine, map[string]interface{}) {
	t.Helper()
	opts.Profile, opts.Vars, opts.Reporter = profile, vars, &quiet{}
	eng, err := New(nil, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	vals, err := eng.componentValues(eng.components[component])
	if err != nil {
		t.Fatal(err)
	}
	return eng, vals
}

func serviceAndConfig(vals map[string]interface{}) (map[string]interface{}, map[string]interface{}, map[string]interface{}) {
	c := vals["controller"].(map[string]interface{})
	svc := c["service"].(map[string]interface{})
	ann, _ := svc["annotations"].(map[string]interface{})
	cfg, _ := c["config"].(map[string]interface{})
	return svc, ann, cfg
}

func TestProxyProtocolOnExternalController(t *testing.T) {
	eng, vals := controllerValues(t, "oci", ociVars(map[string]string{VarProxyProtocol: "true", VarLBSubnetCIDR: "10.7.0.0/24"}), Options{}, "ingress-nginx")
	svc, ann, cfg := serviceAndConfig(vals)
	if ann[ProxyProtocolAnnotation] != "2" || cfg["use-proxy-protocol"] != "true" || cfg["proxy-real-ip-cidr"] != "10.7.0.0/24" || svc["externalTrafficPolicy"] != "Local" {
		t.Errorf("service %v config %v", svc, cfg)
	}
	if eng.vars[VarProxyProtocol] != "true" {
		t.Errorf("server must be told: %q", eng.vars[VarProxyProtocol])
	}
	// The server's environment carries the effective value.
	objs, err := eng.renderComponent(eng.components["shpyrd"])
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range objs {
		if o.GetKind() == "Deployment" && strings.Contains(mustYAML(t, o), `"name":"SHPYRD_PROXY_PROTOCOL","value":"true"`) {
			found = true
		}
	}
	if !found {
		t.Error("server Deployment lacks SHPYRD_PROXY_PROTOCOL=true")
	}
}

func TestProxyProtocolOffEqualsBase(t *testing.T) {
	for _, v := range []string{"false", ""} {
		_, off := controllerValues(t, "oci", ociVars(map[string]string{VarProxyProtocol: v}), Options{}, "ingress-nginx")
		eng, _ := controllerValues(t, "oci", ociVars(nil), Options{}, "ingress-nginx")
		base, err := loadValues(deploy.FS, valuesFiles(deploy.FS, eng.components["ingress-nginx"], "oci"), eng.vars)
		if err != nil {
			t.Fatal(err)
		}
		svc, ann, cfg := serviceAndConfig(off)
		if _, ok := ann[ProxyProtocolAnnotation]; ok || cfg["use-proxy-protocol"] != nil || cfg["proxy-real-ip-cidr"] != nil || svc["externalTrafficPolicy"] != nil {
			t.Errorf("%q: proxy settings present: %v %v", v, svc, cfg)
		}
		_, _, bcfg := serviceAndConfig(base)
		if !reflect.DeepEqual(cfg, bcfg) {
			t.Errorf("%q: config differs from the profile files: %v vs %v", v, cfg, bcfg)
		}
	}
}

func TestProxyProtocolNeverOnOtherProfilesOrInternal(t *testing.T) {
	set := map[string]string{VarProxyProtocol: "true", VarLBSubnetCIDR: "10.7.0.0/24"}
	for _, profile := range []string{"local", "aws", "oci"} {
		vars := map[string]string{VarDomain: "x.example.com", VarACMEEmail: "a@example.com"}
		for k, v := range set {
			vars[k] = v
		}
		eng, ext := controllerValues(t, profile, vars, Options{}, "ingress-nginx")
		if eng.components["ingress-nginx-internal"] != nil && eng.profile.HasComponent("ingress-nginx-internal") {
			vals, err := eng.componentValues(eng.components["ingress-nginx-internal"])
			if err != nil {
				t.Fatal(err)
			}
			_, ann, cfg := serviceAndConfig(vals)
			if _, ok := ann[ProxyProtocolAnnotation]; ok || cfg["use-proxy-protocol"] != nil || cfg["proxy-real-ip-cidr"] != nil {
				t.Errorf("%s: internal controller got PROXY settings", profile)
			}
		}
		if profile != "oci" {
			base, err := loadValues(deploy.FS, valuesFiles(deploy.FS, eng.components["ingress-nginx"], profile), eng.vars)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(ext, base) {
				t.Errorf("%s: external controller values differ from the profile files", profile)
			}
			if eng.vars[VarProxyProtocol] != "false" || len(eng.Notices()) != 1 {
				t.Errorf("%s: vars %q notices %v", profile, eng.vars[VarProxyProtocol], eng.Notices())
			}
		}
	}
}

func TestProxyProtocolRefusesBadRange(t *testing.T) {
	for _, cidr := range []string{"", "not-a-cidr", "10.0.10.5", "0.0.0.0/0", "::/0"} {
		_, err := New(nil, Options{Profile: "oci", Vars: ociVars(map[string]string{VarProxyProtocol: "true", VarLBSubnetCIDR: cidr}), Reporter: &quiet{}})
		if err == nil || !strings.Contains(err.Error(), VarLBSubnetCIDR) {
			t.Errorf("%q: error %v", cidr, err)
		}
	}
	if _, err := New(nil, Options{Profile: "oci", Vars: ociVars(map[string]string{VarProxyProtocol: "yes"}), Reporter: &quiet{}}); err == nil || !strings.Contains(err.Error(), VarProxyProtocol) {
		t.Errorf("bad switch value: %v", err)
	}
}

func TestProxyProtocolOffWithoutInternalController(t *testing.T) {
	vars := ociVars(map[string]string{VarProxyProtocol: "true", VarLBSubnetCIDR: "10.7.0.0/24"})
	for name, opts := range map[string]Options{"skip": {Skip: []string{"ingress-nginx-internal"}}} {
		eng, vals := controllerValues(t, "oci", vars, opts, "ingress-nginx")
		svc, ann, cfg := serviceAndConfig(vals)
		if _, ok := ann[ProxyProtocolAnnotation]; ok || cfg["use-proxy-protocol"] != nil || svc["externalTrafficPolicy"] != nil {
			t.Errorf("%s: external controller changed", name)
		}
		notes := strings.Join(eng.Notices(), "\n")
		if eng.vars[VarProxyProtocol] != "false" || !strings.Contains(notes, "stays off") || !strings.Contains(notes, "internal") {
			t.Errorf("%s: vars %q notices %q", name, eng.vars[VarProxyProtocol], notes)
		}
	}
}

// serverProxyEnv is the SHPYRD_PROXY_PROTOCOL value the server Deployment
// is rendered with.
func serverProxyEnv(t *testing.T, eng *Engine) string {
	t.Helper()
	objs, err := eng.renderComponent(eng.components["shpyrd"])
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objs {
		if o.GetKind() != "Deployment" {
			continue
		}
		y := mustYAML(t, o)
		for _, v := range []string{"true", "false"} {
			if strings.Contains(y, `"name":"SHPYRD_PROXY_PROTOCOL","value":"`+v+`"`) {
				return v
			}
		}
	}
	t.Fatal("server Deployment has no SHPYRD_PROXY_PROTOCOL")
	return ""
}

func controllerProxyOn(vals map[string]interface{}) bool {
	svc, ann, cfg := serviceAndConfig(vals)
	return ann[ProxyProtocolAnnotation] == "2" && cfg["use-proxy-protocol"] == "true" && cfg["proxy-real-ip-cidr"] == "10.7.0.0/24" && svc["externalTrafficPolicy"] == "Local"
}

func controllerProxyOff(vals map[string]interface{}) bool {
	svc, ann, cfg := serviceAndConfig(vals)
	_, has := ann[ProxyProtocolAnnotation]
	return !has && cfg["use-proxy-protocol"] == nil && cfg["proxy-real-ip-cidr"] == nil && svc["externalTrafficPolicy"] == nil
}

// An export limited with --only renders each component as the full one does
// (C15): --only does not make the internal controller "left out".
func TestProxyProtocolOnlyRendersAsFull(t *testing.T) {
	vars := ociVars(map[string]string{VarProxyProtocol: "true", VarLBSubnetCIDR: "10.7.0.0/24"})
	for name, skip := range map[string][]string{"whole": nil, "skip internal": {"ingress-nginx-internal"}} {
		full, fullVals := controllerValues(t, "oci", vars, Options{Skip: skip}, "ingress-nginx")
		wantOn := skip == nil
		if controllerProxyOn(fullVals) != wantOn || serverProxyEnv(t, full) != map[bool]string{true: "true", false: "false"}[wantOn] {
			t.Fatalf("%s: full render not as expected", name)
		}
		for _, only := range [][]string{{"shpyrd"}, {"ingress-nginx"}, {"ingress-nginx", "shpyrd"}} {
			part, partVals := controllerValues(t, "oci", vars, Options{Skip: skip, Only: only}, "ingress-nginx")
			if !reflect.DeepEqual(fullVals, partVals) {
				t.Errorf("%s only %v: external controller values differ from the full render", name, only)
			}
			if got, want := serverProxyEnv(t, part), serverProxyEnv(t, full); got != want {
				t.Errorf("%s only %v: server env %s, full render %s", name, only, got, want)
			}
			if !reflect.DeepEqual(full.Notices(), part.Notices()) {
				t.Errorf("%s only %v: notices %v, full render %v", name, only, part.Notices(), full.Notices())
			}
		}
	}
}

func boolPtr(b bool) *bool { return &b }

// A run that applies only some components keeps the cluster's state (C16).
func TestProxyProtocolKeepsClusterState(t *testing.T) {
	type tc struct {
		name    string
		asked   string // SHPYRD_PROXY_PROTOCOL given to the run
		cluster bool
		only    []string
		skip    []string
		note    bool
	}
	cases := []tc{
		{"a cluster on, only server", "", true, []string{"shpyrd"}, nil, true},
		{"b cluster off, only server, asked true", "true", false, []string{"shpyrd"}, nil, true},
		{"c cluster on, only server, asked false", "false", true, []string{"shpyrd"}, nil, true},
		{"d cluster on, only controller", "true", true, []string{"ingress-nginx"}, nil, false},
		{"d cluster off, only controller, asked true", "true", false, []string{"ingress-nginx"}, nil, true},
		{"cluster on, internal skipped too", "true", true, []string{"shpyrd"}, []string{"ingress-nginx-internal"}, false},
		{"cluster on, skip server, no only", "false", true, nil, []string{"shpyrd"}, true},
		{"cluster off, skip controller, no only", "true", false, nil, []string{"ingress-nginx"}, true},
	}
	for _, c := range cases {
		vars := ociVars(map[string]string{VarLBSubnetCIDR: "10.7.0.0/24"})
		if c.asked != "" {
			vars[VarProxyProtocol] = c.asked
		}
		eng, vals := controllerValues(t, "oci", vars, Options{Only: c.only, Skip: c.skip, ProxyProtocolKeep: boolPtr(c.cluster)}, "ingress-nginx")
		want := map[bool]string{true: "true", false: "false"}[c.cluster]
		if got := serverProxyEnv(t, eng); got != want {
			t.Errorf("%s: server env %s, want %s", c.name, got, want)
		}
		if c.cluster && !controllerProxyOn(vals) || !c.cluster && !controllerProxyOff(vals) {
			t.Errorf("%s: external controller values do not follow the cluster state %v", c.name, c.cluster)
		}
		notes := strings.Join(eng.Notices(), "\n")
		if c.note {
			if !strings.Contains(notes, VarProxyProtocol) || !strings.Contains(notes, "without --only") || !strings.Contains(notes, "stays "+map[bool]string{true: "on", false: "off"}[c.cluster]) {
				t.Errorf("%s: notice %q", c.name, notes)
			}
		} else if notes != "" {
			t.Errorf("%s: unexpected notice %q", c.name, notes)
		}
		if strings.Contains(notes, "interrupted") {
			t.Errorf("%s: partial run talks of an interruption: %q", c.name, notes)
		}
	}
}

// Keeping the state on needs a usable range only when the run applies the
// external controller (C5 for a partial run).
func TestProxyProtocolKeepOnNeedsRangeOnlyForController(t *testing.T) {
	noRange := ociVars(map[string]string{VarLBSubnetCIDR: ""})
	if _, err := New(nil, Options{Profile: "oci", Vars: noRange, Only: []string{"ingress-nginx"}, ProxyProtocolKeep: boolPtr(true), Reporter: &quiet{}}); err == nil || !strings.Contains(err.Error(), VarLBSubnetCIDR) {
		t.Errorf("controller applied without a range: %v", err)
	}
	eng, err := New(nil, Options{Profile: "oci", Vars: noRange, Only: []string{"shpyrd"}, ProxyProtocolKeep: boolPtr(true), Reporter: &quiet{}})
	if err != nil {
		t.Fatalf("server only: %v", err)
	}
	if serverProxyEnv(t, eng) != "true" {
		t.Error("server must be told true")
	}
	if _, err := New(nil, Options{Profile: "oci", Vars: ociVars(map[string]string{VarProxyProtocol: "yes"}), Only: []string{"shpyrd"}, ProxyProtocolKeep: boolPtr(false), Reporter: &quiet{}}); err == nil {
		t.Error("an invalid value is refused also in a partial run")
	}
}

// A full run switches whatever the cluster has, also the first phase of a
// cloud install, which holds the server back with --skip itself (C18).
func TestProxyProtocolFullRunSwitchesInFirstPhase(t *testing.T) {
	vars := ociVars(map[string]string{VarProxyProtocol: "true", VarLBSubnetCIDR: "10.7.0.0/24"})
	_, vals := controllerValues(t, "oci", vars, Options{Skip: []string{"shpyrd", "dns"}}, "ingress-nginx")
	if !controllerProxyOn(vals) {
		t.Errorf("first phase did not switch on: %v", vals)
	}
	vars[VarProxyProtocol] = "false"
	eng, vals := controllerValues(t, "oci", vars, Options{Skip: []string{"shpyrd", "dns"}}, "ingress-nginx")
	if !controllerProxyOff(vals) || eng.vars[VarProxyProtocol] != "false" {
		t.Errorf("first phase did not switch off: %v", vals)
	}
}
