package cli

import (
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/api"
)

// The APPS line says where apps answer, as the server writes it (#242).
func TestTheWorkspaceSaysWhereItsAppsAnswer(t *testing.T) {
	for _, c := range []struct {
		ws   api.WorkspaceView
		want string
	}{
		{api.WorkspaceView{Domain: "wgk.shpyrd.cloud", AppHost: &api.AppHostView{Prefix: "wgk-", Suffix: ".shpyrd.app"}}, "wgk-<app>.shpyrd.app"},
		{api.WorkspaceView{Domain: "nkr.systems", AppHost: &api.AppHostView{Suffix: ".nkr.systems"}}, "<app>.nkr.systems"},
		{api.WorkspaceView{Domain: "acme.example.com"}, "<app>.acme.example.com"},
		{api.WorkspaceView{}, ""},
	} {
		if got := appsPattern(c.ws); got != c.want {
			t.Errorf("appsPattern(%+v) = %q, want %q", c.ws, got, c.want)
		}
	}
}
