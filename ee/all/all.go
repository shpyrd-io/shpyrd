//go:build !foss

// Package all lists the ee extensions (ee/LICENSE). They are always on
// where ee is built in, and the license decides what they do; a build with
// the foss tag has none of this folder.
package all

import (
	"github.com/shpyrd-io/shpyrd/ee/autosleep"
	"github.com/shpyrd-io/shpyrd/ee/costs"
	"github.com/shpyrd-io/shpyrd/ee/licensing"
	"github.com/shpyrd-io/shpyrd/ee/mcp"
	"github.com/shpyrd-io/shpyrd/ee/sso"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// Extensions are the ee extensions, in display order.
func Extensions() []ext.Extension {
	return []ext.Extension{licensing.New(), costs.New(), sso.New(), autosleep.New(), mcp.New()}
}
