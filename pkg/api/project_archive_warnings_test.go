package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/projectarchive"
)

// A link the volume cannot answer for is a line in the operation's report,
// read on the status while the operation runs; it names the file and says
// what happened in words (#52), and the export went on (#129).
func TestArchiveWarningsReachTheReportInWords(t *testing.T) {
	ctx := context.Background()
	volume := t.TempDir()
	if err := os.MkdirAll(filepath.Join(volume, "runtime", "profile"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/tmp/org.chromium.Chromium.k2Fw1a/SingletonSocket", filepath.Join(volume, "runtime", "profile", "SingletonSocket")); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if err := projectarchive.VolumeMain([]string{"export", volume}, strings.NewReader(""), io.Discard, &stderr); err != nil {
		t.Fatalf("export stopped: %v", err)
	}
	app := sampleApp("shop", shpyrdv1.PhaseRunning)
	s, _ := newTestServer(t, nil, []client.Object{app})
	op, err := s.beginProjectArchive(ctx, app, "move", &projectArchiveMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	archiveWarnings(stderr.String(), op.warn)
	archiveWarnings(stderr.String(), op.warn) // the same helper run twice says it once
	if len(op.Warnings) != 1 || !strings.Contains(op.Warnings[0], "runtime/profile/SingletonSocket") {
		t.Fatalf("report: %q", op.Warnings)
	}
	if err := s.saveProjectArchive(ctx, app.Namespace, op); err != nil {
		t.Fatal(err)
	}
	rec := do(t, s, "GET", "/api/project-archives/shop", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	var status struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if len(status.Warnings) != 1 || status.Warnings[0] != op.Warnings[0] {
		t.Fatalf("status does not carry the report: %s", rec.Body.String())
	}
	var v interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	checked := 0
	walkMessages(v, func(key, msg string) {
		checked++
		if bad := controller.PlatformWordingFault(msg); bad != "" {
			t.Errorf("%s = %q names %q", key, msg, bad)
		}
	})
	if checked == 0 {
		t.Fatal("no message checked")
	}
	// The report is bounded; past the bound it says so, once.
	for i := 0; i < 300; i++ {
		op.warn(strings.Repeat("x", i+1))
	}
	if n := len(op.Warnings); n != 101 || op.Warnings[100] != "Further warnings were left out of this report." {
		t.Fatalf("bound: %d %q", n, op.Warnings[n-1])
	}
}
