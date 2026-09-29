// Package all is the registry of built-in extensions (RFC-0002). Enabling
// is data (the install record), not a build tag: one binary serves every
// cluster.
package all

import (
	"strings"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ext/authlocal"
	"github.com/shpyrd-io/shpyrd/pkg/ext/logsagent"
	"github.com/shpyrd-io/shpyrd/pkg/ext/mail"
	"github.com/shpyrd-io/shpyrd/pkg/ext/objectstorage"
	"github.com/shpyrd-io/shpyrd/pkg/ext/opencost"
	"github.com/shpyrd-io/shpyrd/pkg/ext/postgres"
	"github.com/shpyrd-io/shpyrd/pkg/ext/redis"
	"github.com/shpyrd-io/shpyrd/pkg/ext/sleep"
)

// All lists every extension the binaries know about, in display order.
func All() []ext.Extension {
	return []ext.Extension{
		authlocal.New(),
		logsagent.New(),
		postgres.New(),
		redis.New(),
		objectstorage.New(),
		mail.New(),
		opencost.New(),
		sleep.New(),
	}
}

// Retired names extensions once had. An install record may still carry
// them; they are skipped with a note instead of failing the install, and
// the note says what took their place.
var Retired = map[string]string{
	// auth-oidc connected an OIDC issuer straight to the relying party
	// (RFC-0058, first cut). Connectors of kind oidc do the same through
	// the bundled issuer, per realm (RFC-0080): shpyrd auth connector add oidc.
	"auth-oidc": "superseded by connectors of kind oidc (shpyrd auth connector add oidc)",
}

// Enabled resolves a comma separated SHPYRD_EXTENSIONS value; unknown names
// are returned separately so callers can warn. Retired names are dropped
// silently: callers that want to tell the user consult Retired.
func Enabled(csv string) (enabled []ext.Extension, unknown []string) {
	for _, name := range splitCSV(csv) {
		if x := ext.Find(All(), name); x != nil {
			enabled = append(enabled, x)
		} else if _, retired := Retired[name]; !retired {
			unknown = append(unknown, name)
		}
	}
	return enabled, unknown
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// BindableTypes lists the resource kinds of the given extensions that apps
// can attach.
func BindableTypes(list []ext.Extension) []ext.ResourceType {
	var out []ext.ResourceType
	for _, x := range list {
		for _, t := range x.Types() {
			if t.Bindable {
				out = append(out, t)
			}
		}
	}
	return out
}
