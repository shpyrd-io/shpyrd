package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shpyrd-ctl is the operator's tool: it speaks to the cluster the kubeconfig
// points at. A saved `shpyrd login` session (a developer's token for one
// workspace door) must not be picked up when --context is absent: the
// operator endpoints refuse it, and it goes stale when the cluster is rebuilt.
func TestCtlIgnoresLoginSessionsWithoutContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", filepath.Join(home, "no-such-kubeconfig"))
	if err := os.MkdirAll(filepath.Join(home, ".shpyrd"), 0o700); err != nil {
		t.Fatal(err)
	}
	sessions := `{"current":"https://operator.example.com","sessions":{"https://operator.example.com":{"url":"https://operator.example.com","token":"stale"}}}`
	if err := os.WriteFile(filepath.Join(home, ".shpyrd", "sessions.json"), []byte(sessions), 0o600); err != nil {
		t.Fatal(err)
	}
	defer func(prev bool) { preferKubeconfig = prev }(preferKubeconfig)

	root := NewCtl()
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !preferKubeconfig {
		t.Fatal("shpyrd-ctl must prefer the kubeconfig even when no --context is given")
	}
	ac, err := newAppClient(&globalFlags{}, os.Stderr)
	if err == nil {
		if ac.session {
			t.Fatal("newAppClient took the login session")
		}
		t.Fatal("newAppClient connected without a kubeconfig")
	}
	if strings.Contains(err.Error(), "signed in") {
		t.Fatalf("error speaks of login sessions: %v", err)
	}
}
