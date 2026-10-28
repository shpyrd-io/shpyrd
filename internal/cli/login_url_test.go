package cli

import "testing"

// A workspace's name alone is its address on shpyrd cloud; anything that
// already reads as an address is taken as it is.
func TestNormaliseURL(t *testing.T) {
	t.Setenv("SHPYRD_WORKSPACES_DOMAIN", "")
	for in, want := range map[string]string{
		"acme":                           "https://acme.shpyrd.cloud",
		" acme \n":                       "https://acme.shpyrd.cloud",
		"my-team-2":                      "https://my-team-2.shpyrd.cloud",
		"acme.shpyrd.cloud":              "https://acme.shpyrd.cloud",
		"https://acme.shpyrd.cloud/":     "https://acme.shpyrd.cloud",
		"https://acme.shpyrd.app":        "https://acme.shpyrd.app",
		"shpyrd.oci.example.com":         "https://shpyrd.oci.example.com",
		"localhost":                      "https://localhost",
		"localhost:8443":                 "https://localhost:8443",
		"http://shpyrd.127.0.0.1.nip.io": "http://shpyrd.127.0.0.1.nip.io",
	} {
		if got, err := normaliseURL(in); err != nil || got != want {
			t.Errorf("normaliseURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	t.Setenv("SHPYRD_WORKSPACES_DOMAIN", "workspaces.example.com.")
	if got, _ := normaliseURL("acme"); got != "https://acme.workspaces.example.com" {
		t.Errorf("with SHPYRD_WORKSPACES_DOMAIN: %q", got)
	}
}

// SHPYRD_URL takes a workspace's name alone too: commands dial its address.
func TestEnvWorkspaceURL(t *testing.T) {
	t.Setenv("SHPYRD_WORKSPACES_DOMAIN", "")
	for in, want := range map[string]string{
		"":                          "",
		"acme":                      "https://acme.shpyrd.cloud",
		"https://acme.shpyrd.cloud": "https://acme.shpyrd.cloud",
		"http://localhost:8080/":    "http://localhost:8080",
	} {
		t.Setenv("SHPYRD_URL", in)
		if got := envWorkspaceURL(); got != want {
			t.Errorf("SHPYRD_URL=%q: envWorkspaceURL() = %q; want %q", in, got, want)
		}
	}
}
