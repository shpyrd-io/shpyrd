package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// Two replicas of the server on one store: a project is exported on one
// and its download lands on the other, which fetches the file from the
// first over the internal sources port. The ticket is good once, on
// either replica.
func TestProjectArchiveDownloadAcrossReplicas(t *testing.T) {
	app := sampleApp("private", shpyrdv1.PhasePending)
	app.Spec.Source = nil
	app.Status.Image = ""
	app.Status.URL = ""
	a, cr := newTestServer(t, nil, []client.Object{app})
	b, _ := newTestServerOn(t, a.store, cr, nil, nil)
	for _, s := range []*Server{a, b} {
		peer := httptest.NewServer(s.SourcesHandler())
		t.Cleanup(peer.Close)
		s.archiveDownloads = sharedArchiveDownloads(a.store, peer.URL)
	}
	export := func(s *Server) string {
		t.Helper()
		rec := do(t, s, "POST", "/api/project-archives/private/export", "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("export: %d %s", rec.Code, rec.Body.String())
		}
		var out struct {
			Ticket string `json:"ticket"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Ticket == "" {
			t.Fatalf("export answer: %s", rec.Body.String())
		}
		return out.Ticket
	}
	download := func(s *Server, ticket string) *httptest.ResponseRecorder {
		t.Helper()
		return do(t, s, "GET", "/api/project-archives/private/download?ticket="+ticket, "", true)
	}

	// Downloaded where it was built: as before.
	local := download(a, export(a))
	if local.Code != http.StatusOK || local.Body.Len() == 0 {
		t.Fatalf("local download: %d %s", local.Code, local.Body.String())
	}

	// Downloaded on the other replica: fetched from the first.
	ticket := export(a)
	relayed := download(b, ticket)
	if relayed.Code != http.StatusOK || relayed.Body.Len() == 0 || relayed.Header().Get("Content-Type") != "application/gzip" {
		t.Fatalf("relayed download: %d %s", relayed.Code, relayed.Header())
	}
	if got := relayed.Header().Get("Content-Disposition"); got == "" {
		t.Error("the relayed download has no file name")
	}
	if zr, err := gzip.NewReader(bytes.NewReader(relayed.Body.Bytes())); err != nil {
		t.Fatalf("the relayed file is not gzip: %v", err)
	} else if _, err := tar.NewReader(zr).Next(); err != nil {
		t.Fatalf("the relayed file is not an archive: %v", err)
	}
	// Once, on either replica.
	for _, s := range []*Server{a, b} {
		if rec := download(s, ticket); rec.Code != http.StatusGone {
			t.Errorf("a used ticket downloaded again: %d", rec.Code)
		}
	}

	// A ticket is the project's: another project's download route finds
	// nothing, and the internal port hands nothing out without the ticket.
	ticket = export(a)
	peer := httptest.NewServer(a.SourcesHandler())
	t.Cleanup(peer.Close)
	if resp, err := http.Get(peer.URL + archivePeerPath + "?namespace=" + app.Namespace); err != nil || resp.StatusCode != http.StatusGone {
		t.Errorf("the internal port without a ticket: %v %v", resp, err)
	}
	if rec := download(b, ticket); rec.Code != http.StatusOK {
		t.Errorf("the ticket was spent by the probe: %d", rec.Code)
	}
}
