package api

import (
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// The client of a request follows the identity's provider: tokens are
// the CLI's way in, sessions the dashboard's.
func TestClientOf(t *testing.T) {
	cases := map[string]string{
		"api-token": "cli", "token": "admin", "kubeconfig": "kubeconfig", "local": "dashboard", "okta": "dashboard", "": "", "none": "",
	}
	for provider, want := range cases {
		if got := clientOf(ext.Identity{Provider: provider}); got != want {
			t.Errorf("clientOf(%q) = %q, want %q", provider, got, want)
		}
	}
}
