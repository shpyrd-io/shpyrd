package install

import (
	"fmt"
	"k8s.io/apimachinery/pkg/util/validation"
	"net/url"
	"strings"
)

func validateIntegrationVars(vars map[string]string) error {
	enabled := vars["SHPYRD_PUBLIC_API_ENABLED"]
	if enabled != "true" && enabled != "false" {
		return fmt.Errorf("SHPYRD_PUBLIC_API_ENABLED must be true or false")
	}
	if enabled == "true" {
		u, err := url.Parse(vars["SHPYRD_PUBLIC_API_URL"])
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || len(validation.IsDNS1123Subdomain(u.Hostname())) != 0 {
			return fmt.Errorf("SHPYRD_PUBLIC_API_URL must be an HTTPS origin with a DNS hostname")
		}
		for _, host := range []string{vars[VarDomain], vars[VarConsoleHost], vars[VarAuthHost], vars[VarWorkspacesDomain], vars[VarAppsDomain], vars[VarDefaultWorkspaceAddress]} {
			if strings.EqualFold(u.Hostname(), host) {
				return fmt.Errorf("public API hostname conflicts with an existing platform hostname")
			}
		}
	}
	for _, prefix := range []string{"SHPYRD_INTERNAL_API_ACCOUNTS_", "SHPYRD_INTERNAL_API_BILLING_"} {
		if len(validation.IsDNS1123Label(vars[prefix+"NAMESPACE"])) != 0 {
			return fmt.Errorf("%sNAMESPACE must be a namespace name", prefix)
		}
		if len(validation.IsValidLabelValue(vars[prefix+"APP"])) != 0 || vars[prefix+"APP"] == "" {
			return fmt.Errorf("%sAPP must be a nonempty pod label value", prefix)
		}
	}
	return nil
}
