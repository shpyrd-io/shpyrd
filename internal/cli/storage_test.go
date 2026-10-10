package cli

import (
	"strings"
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/storagemigrate"
)

func TestPrintMigrationSaysNothingAboutDataItNeverRead(t *testing.T) {
	var b strings.Builder
	printMigration(&b, &storagemigrate.Result{Database: "p-shop/db", To: "oci-bv"})
	if b.Len() != 0 {
		t.Errorf("a refused run must not claim where the data is: %q", b.String())
	}
	b.Reset()
	printMigration(&b, &storagemigrate.Result{Database: "p-shop/db", From: "shpyrd-local", To: "oci-bv", Verified: true, OldPV: "pv-1", Snapshot: "db-2-m"})
	if got := b.String(); got != "p-shop/db: data on oci-bv, verified; old volume pv-1 kept; snapshot db-2-m.\n" {
		t.Errorf("verified run = %q", got)
	}
	b.Reset()
	printMigration(&b, &storagemigrate.Result{Database: "p-shop/db", From: "oci-bv", To: "shpyrd-local", ReturnedTo: "shpyrd-local"})
	if got := b.String(); !strings.Contains(got, "back on its old volume (shpyrd-local)") || strings.Contains(got, "verified") {
		t.Errorf("--back run = %q", got)
	}
	b.Reset()
	printMigration(&b, &storagemigrate.Result{Database: "p-shop/db", From: "shpyrd-local", To: "oci-bv", StoppedOn2: true, Notes: []string{"public.orders: 1000 rows on the old instance, 10 on the new"}})
	if got := b.String(); !strings.Contains(got, "stopped on two instances") || !strings.Contains(got, "  public.orders: 1000 rows") {
		t.Errorf("stopped run = %q", got)
	}
}
