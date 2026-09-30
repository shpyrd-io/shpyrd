package api

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// The applications as a build leaves them: a folder each, with the entry,
// a page built for an address, a file, and scripts written into a page.
func uiServer(t *testing.T) *Server {
	t.Helper()
	s, _, _ := newTenantServer(t)
	s.opts.UI = fstest.MapFS{
		"console/index.html":       {Data: []byte("<html><script src=\"/app.js\"></script><script>console()</script>console</html>")},
		"console/app.js":           {Data: []byte("console js")},
		"console/workspaces.html":  {Data: []byte("<html>console: the workspaces</html>")},
		"workspace/index.html":     {Data: []byte("<html>workspace</html>")},
		"workspace/app.js":         {Data: []byte("workspace js")},
		"workspace/projects.html":  {Data: []byte("<html><script>projects()</script>workspace: the projects</html>")},
		"workspace/logo.svg":       {Data: []byte("<svg/>")},
		"workspace/_next/a/b/c.js": {Data: []byte("chunk")},
	}
	s.engine.NoRoute(s.serveUI())
	return s
}

func get(t *testing.T, s *Server, host, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := at(t, s, host, "GET", path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%s%s = %d", host, path, rec.Code)
	}
	return rec
}

func hashOf(script string) string {
	sum := sha256.Sum256([]byte(script))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

func TestTheHostChoosesTheApplicationsFolder(t *testing.T) {
	s := uiServer(t)
	if got := get(t, s, "shpyrd.example.test", "/").Body.String(); !strings.HasSuffix(got, "console</html>") {
		t.Errorf("console host = %q", got)
	}
	if got := get(t, s, "acme.shpyrd.test", "/").Body.String(); got != "<html>workspace</html>" {
		t.Errorf("workspace host = %q", got)
	}
}

func TestAFileIsServedFromTheHostsFolderOnly(t *testing.T) {
	s := uiServer(t)
	if got := get(t, s, "shpyrd.example.test", "/app.js").Body.String(); got != "console js" {
		t.Errorf("console file = %q", got)
	}
	if got := get(t, s, "acme.shpyrd.test", "/app.js").Body.String(); got != "workspace js" {
		t.Errorf("workspace file = %q", got)
	}
	if got := get(t, s, "acme.shpyrd.test", "/_next/a/b/c.js").Body.String(); got != "chunk" {
		t.Errorf("nested file = %q", got)
	}
	// The console's page is not the workspace's: an address the workspace
	// did not build gets its entry.
	if got := get(t, s, "acme.shpyrd.test", "/workspaces").Body.String(); got != "<html>workspace</html>" {
		t.Errorf("another application's page = %q", got)
	}
	if got := get(t, s, "acme.shpyrd.test", "/logo.svg"); !strings.HasPrefix(got.Header().Get("Content-Type"), "image/svg") {
		t.Errorf("content type of a file = %q", got.Header().Get("Content-Type"))
	}
}

func TestThePageBuiltForAnAddressIsServed(t *testing.T) {
	s := uiServer(t)
	if got := get(t, s, "shpyrd.example.test", "/workspaces").Body.String(); got != "<html>console: the workspaces</html>" {
		t.Errorf("/workspaces = %q", got)
	}
	if got := get(t, s, "acme.shpyrd.test", "/projects").Body.String(); !strings.HasSuffix(got, "workspace: the projects</html>") {
		t.Errorf("/projects = %q", got)
	}
	if got := get(t, s, "acme.shpyrd.test", "/projects.html").Body.String(); !strings.HasSuffix(got, "workspace: the projects</html>") {
		t.Errorf("/projects.html = %q", got)
	}
}

func TestAnUnknownAddressGetsTheHostsEntry(t *testing.T) {
	s := uiServer(t)
	if got := get(t, s, "acme.shpyrd.test", "/projects/shop/logs").Body.String(); got != "<html>workspace</html>" {
		t.Errorf("unknown address = %q", got)
	}
	if got := get(t, s, "shpyrd.example.test", "/accounts/x").Body.String(); !strings.HasSuffix(got, "console</html>") {
		t.Errorf("unknown console address = %q", got)
	}
}

func TestThePolicyCarriesTheHashesOfTheScriptsWrittenIntoAPage(t *testing.T) {
	s := uiServer(t)
	got := get(t, s, "shpyrd.example.test", "/").Header().Get("Content-Security-Policy")
	want := basePolicy + "; script-src 'self' " + hashOf("console()")
	if got != want {
		t.Errorf("policy of a page with a script written in:\n got %s\nwant %s", got, want)
	}
	if strings.Contains(got, "unsafe-inline'; script") || strings.Contains(got, "script-src 'self' 'unsafe-inline'") {
		t.Errorf("the policy allows any script: %s", got)
	}
	got = get(t, s, "acme.shpyrd.test", "/projects").Header().Get("Content-Security-Policy")
	if want := basePolicy + "; script-src 'self' " + hashOf("projects()"); got != want {
		t.Errorf("policy of a built page:\n got %s\nwant %s", got, want)
	}
	got = get(t, s, "acme.shpyrd.test", "/").Header().Get("Content-Security-Policy")
	if got != basePolicy {
		t.Errorf("policy of a page without scripts written in = %s", got)
	}
	got = get(t, s, "acme.shpyrd.test", "/app.js").Header().Get("Content-Security-Policy")
	if got != basePolicy {
		t.Errorf("policy of a file = %s", got)
	}
}

func TestWithoutABuildThePlaceholderAnswers(t *testing.T) {
	s, _, _ := newTenantServer(t)
	s.opts.UI = fstest.MapFS{".gitkeep": {Data: nil}}
	s.engine.NoRoute(s.serveUI())
	for _, host := range []string{"shpyrd.example.test", "acme.shpyrd.test"} {
		if got := get(t, s, host, "/").Body.String(); !strings.Contains(got, "not built into this binary") {
			t.Errorf("%s without a build = %.80q", host, got)
		}
	}
}

func TestScriptHashesReadWhatNextWrites(t *testing.T) {
	page := []byte(`<html><head><script src="/theme.js"></script><script src="/_next/a.js" async=""></script></head>` +
		`<body><script>(self.__next_f=self.__next_f||[]).push([0])</script>` +
		`<script>self.__next_f.push([1,"1:\"$Sreact.fragment\"\n"])</script>` +
		`<script SRC="/x.js"></script></body></html>`)
	got := scriptHashes(page)
	want := []string{hashOf("(self.__next_f=self.__next_f||[]).push([0])"), hashOf(`self.__next_f.push([1,"1:\"$Sreact.fragment\"\n"])`)}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("hashes = %v, want %v", got, want)
	}
	if got := scriptHashes([]byte("<html>no script</html>")); got != nil {
		t.Errorf("hashes of a page without scripts = %v", got)
	}
}
