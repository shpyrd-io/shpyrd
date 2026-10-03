package ext

import (
	"context"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// A Gate puts a project at a host of its own that people enter from their
// own workspace (RFC-0083): the edge carries their session there and checks
// it, and their access, on every request.
type Gate struct {
	Name      string // the gate's name in routes, cookies and the JWT's aud
	Host      string // served by the extension's own front door
	Workspace string // the workspace of the project behind the gate
	Project   string // the project behind the gate
	// Admit decides for people of any OTHER workspace, on the way in and on
	// every request. People of the gate's own workspace enter by the
	// project's access, as at any app's host. Nil admits nobody from
	// elsewhere.
	Admit func(ctx context.Context, v Visitor) bool
}

// Visitor is who stands at a gate: resolved now, in the workspace they
// came from (operator-owned adjustments included). Plain strings: pkg/authz
// imports pkg/ext, so ext cannot carry authz.Roles.
type Visitor struct {
	Identity      Identity
	Workspace     store.Workspace
	WorkspaceRole string // owner, admin, member, or ""
	PlatformRole  string // platform-admin, or ""
	Teams         []string
}

// GateProvider is implemented by extensions that declare gates.
type GateProvider interface {
	Gates(Deps) []Gate
}

// Link is an entry an extension adds to a workspace's sidebar, under its
// own section.
type Link struct {
	Section string `json:"section"`
	Label   string `json:"label"`
	URL     string `json:"url"`
	Icon    string `json:"icon,omitempty"`
}

// LinkProvider is implemented by extensions that add links to a
// workspace's sidebar, per person.
type LinkProvider interface {
	Links(ctx context.Context, v Visitor) []Link
}
