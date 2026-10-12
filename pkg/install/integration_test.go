package install

import (
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"testing"
)

func TestIntegrationDefaults(t *testing.T) {
	v := derivedVars(map[string]string{VarDomain: "operator.example.test", VarClusterIssuer: "letsencrypt", VarDNSProvider: "oci"}, nil)
	if v["SHPYRD_PUBLIC_API_HOST"] != "api.operator.example.test" || v["SHPYRD_PUBLIC_API_ENABLED"] != "false" || v["SHPYRD_PUBLIC_API_ISSUER"] != "letsencrypt-dns01" {
		t.Fatalf("wrong public API defaults: %s %s %s", v["SHPYRD_PUBLIC_API_HOST"], v["SHPYRD_PUBLIC_API_ENABLED"], v["SHPYRD_PUBLIC_API_ISSUER"])
	}
	if v["SHPYRD_INTERNAL_API_ACCOUNTS_NAMESPACE"] != "shpyrd-integration-disabled" {
		t.Fatal("internal API must be closed by default")
	}
	v = derivedVars(map[string]string{VarDomain: "operator.example.test", VarClusterIssuer: "letsencrypt", VarDNSProvider: "oci", "SHPYRD_PUBLIC_API_ENABLED": "true", "SHPYRD_PUBLIC_API_URL": "https://api.example.test", "SHPYRD_INTERNAL_API_ACCOUNTS_NAMESPACE": "accounts-project"}, nil)
	if v["SHPYRD_PUBLIC_API_HOST"] != "api.example.test" || v["SHPYRD_PUBLIC_API_ISSUER"] != "letsencrypt" || v["SHPYRD_PUBLIC_API_ENABLED"] != "true" || v["SHPYRD_INTERNAL_API_ACCOUNTS_NAMESPACE"] != "accounts-project" {
		t.Fatal("custom configuration lost")
	}
}

func TestIntegrationRejectsUnsafeConfiguration(t *testing.T) {
	for _, raw := range []string{"http://api.example.test", "https://api.example.test/private", "https://user:pass@api.example.test", "https://operator.example.test", "https://api.example.test?token=x"} {
		v := derivedVars(map[string]string{VarDomain: "operator.example.test", "SHPYRD_PUBLIC_API_ENABLED": "true", "SHPYRD_PUBLIC_API_URL": raw}, nil)
		v[VarDomain] = "operator.example.test"
		if err := validateIntegrationVars(v); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestIntegrationManifests(t *testing.T) {
	eng, err := New(nil, Options{Profile: "local", Vars: map[string]string{VarDomain: "example.test", "SHPYRD_INTERNAL_API_ACCOUNTS_NAMESPACE": "accounts-project"}})
	if err != nil {
		t.Fatal(err)
	}
	objs, err := eng.renderComponent(eng.components[ServerComponent])
	if err != nil {
		t.Fatal(err)
	}
	services := 0
	for _, obj := range objs {
		if obj.GetKind() == "Service" && (obj.GetName() == "shpyrd-internal-api" || obj.GetName() == "shpyrd-public-api") {
			services++
			ports, _, _ := unstructured.NestedSlice(obj.Object, "spec", "ports")
			port := ports[0].(map[string]interface{})
			target := int64(8083)
			if obj.GetName() == "shpyrd-public-api" {
				target = 8084
			}
			if fmt.Sprint(port["targetPort"]) != fmt.Sprint(target) {
				t.Errorf("%s target: %v", obj.GetName(), port)
			}
		}
		if obj.GetKind() == "NetworkPolicy" && obj.GetName() == "shpyrd-server" {
			rules, _, _ := unstructured.NestedSlice(obj.Object, "spec", "ingress")
			found := false
			for _, raw := range rules {
				rule := raw.(map[string]interface{})
				ports := rule["ports"].([]interface{})
				if fmt.Sprint(ports[0].(map[string]interface{})["port"]) != "8083" {
					continue
				}
				found = true
				peers := rule["from"].([]interface{})
				for _, rawPeer := range peers {
					peer := rawPeer.(map[string]interface{})
					if peer["namespaceSelector"] == nil || peer["podSelector"] == nil {
						t.Fatal("internal peer must require namespace AND pod")
					}
				}
			}
			if !found {
				t.Fatal("internal network policy missing")
			}
		}
	}
	if services != 2 {
		t.Fatalf("integration services: %d", services)
	}
}
