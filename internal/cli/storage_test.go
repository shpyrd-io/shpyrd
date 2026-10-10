package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

// A volume nothing mounts is its own group; a mounted one belongs to the
// group of the process that mounts it, whose id is the process's.
func TestVolumeGroupFindsAMountedVolumeByItsProcess(t *testing.T) {
	groups := []placementGroupAnswer{
		{ID: "volume:data", Volumes: []string{"data"}, Nodes: []string{"10.0.1.38"}},
		{ID: "process:web", Processes: []string{"web"}, Volumes: []string{"fluxyr"}, Nodes: []string{"10.0.1.38"}, StorageClasses: []string{"shpyrd-local"}},
	}
	if g := volumeGroup(groups, "data"); g == nil || g.ID != "volume:data" {
		t.Errorf("own group: %+v", g)
	}
	if g := volumeGroup(groups, "fluxyr"); g == nil || g.ID != "process:web" {
		t.Errorf("mounted volume must be found through its process: %+v", g)
	}
	if g := volumeGroup(groups, "uploads"); g != nil {
		t.Errorf("unknown volume: %+v", g)
	}
}

// A lost connection during the copy: the CLI waits for the server's
// operation to end and reads the result from the placement page.
func TestWaitForMoveReadsTheResultAfterALostConnection(t *testing.T) {
	calls := 0
	request := func(method, path string, body []byte) ([]byte, error) {
		calls++
		switch {
		case path == "" && calls == 1:
			return nil, errors.New("Post \"https://x/move\": http2: client connection lost")
		case path == "" && calls == 2:
			return []byte(`{"id":"op1","kind":"move","phase":"copying","active":true}`), nil
		case path == "":
			return []byte(`{"phase":"idle","active":false}`), nil
		case path == "/placement":
			return []byte(`{"groups":[{"id":"process:web","processes":["web"],"volumes":["fluxyr"],"nodes":["10.0.1.38"],"storageClasses":["oci-bv"]}]}`), nil
		}
		return nil, errors.New("unexpected " + path)
	}
	warnings, err := waitForMove(context.Background(), request, "fluxyr", "oci-bv", time.Millisecond)
	if err != nil || len(warnings) != 0 || calls != 4 {
		t.Errorf("waitForMove = %v %v after %d calls", warnings, err, calls)
	}
	// The operation failed: its error is the answer.
	failed := func(method, path string, body []byte) ([]byte, error) {
		return []byte(`{"id":"op2","kind":"move","phase":"copying","error":"copy fluxyr: fingerprint mismatch","active":false}`), nil
	}
	if _, err := waitForMove(context.Background(), failed, "fluxyr", "oci-bv", time.Millisecond); err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") {
		t.Errorf("a failed operation must be reported: %v", err)
	}
	// Idle but still on the old class: not done.
	notMoved := func(method, path string, body []byte) ([]byte, error) {
		if path == "/placement" {
			return []byte(`{"groups":[{"id":"process:web","volumes":["fluxyr"],"storageClasses":["shpyrd-local"]}]}`), nil
		}
		return []byte(`{"phase":"idle","active":false}`), nil
	}
	if _, err := waitForMove(context.Background(), notMoved, "fluxyr", "oci-bv", time.Millisecond); err == nil || !strings.Contains(err.Error(), "still on [shpyrd-local]") {
		t.Errorf("a move that did not happen must be reported: %v", err)
	}
	if !lostConnection(errors.New("http2: client connection lost")) || lostConnection(errors.New("shpyrd-server: project not found")) {
		t.Error("lostConnection must tell a dead connection from an answer")
	}
}
