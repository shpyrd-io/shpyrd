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
	for name, opts := range map[string]Options{"skip": {Skip: []string{"ingress-nginx-internal"}}, "only": {Only: []string{"ingress-nginx"}}} {
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
