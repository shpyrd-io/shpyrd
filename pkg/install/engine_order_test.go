package install

import "testing"

// The platform server discovers at start the kinds the other components
// of its runlevel install (KEDA's, Dex's, OpenCost's): it goes after
// them, never alongside. Seen on a kind cluster: `extensions enable sleep`
// brought the server up before keda-http, and no sleep object was made
// until the next restart.
func TestServerGoesAfterTheOthersOfItsRunlevel(t *testing.T) {
	comps := []*Component{{Name: "keda-http"}, {Name: ServerComponent}, {Name: "opencost"}}
	others, server := splitServer(comps)
	if server == nil || server.Name != ServerComponent {
		t.Fatalf("server = %+v", server)
	}
	if len(others) != 2 || others[0].Name != "keda-http" || others[1].Name != "opencost" {
		t.Fatalf("others = %+v", others)
	}
	if _, server := splitServer([]*Component{{Name: "keda"}}); server != nil {
		t.Fatalf("a runlevel without the server: %+v", server)
	}
}
