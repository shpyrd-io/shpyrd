package server

import "testing"

// The outside readiness checks run where names are published on public
// DNS. The server learns that from the DNS provider when it is handed one,
// and otherwise from SHPYRD_WILDCARD_TLS, which the installer sets to true
// exactly when a provider exists; production handed only the latter and
// the checks stayed off.
func TestPublishesNames(t *testing.T) {
	cases := []struct {
		provider, wildcard string
		want               bool
	}{
		{"oci", "", true},
		{"oci", "true", true},
		{"", "true", true},
		{"none", "true", true},
		{"", "false", false},
		{"", "", false},
		{"none", "", false},
	}
	for _, c := range cases {
		if got := publishesNames(c.provider, c.wildcard); got != c.want {
			t.Errorf("publishesNames(%q, %q) = %v, want %v", c.provider, c.wildcard, got, c.want)
		}
	}
}
