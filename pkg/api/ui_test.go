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
		"workspaces/index.html":    {Data: []byte("<html><script>list()</script>the cloud's workspaces</html>")},
		"workspaces/_next/x.js":    {Data: []byte("their chunk")},
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

// A binary built on the core writes into every page (analytics, say): the
// additions land before </head> and </body>, their scripts are hashed
// like Next's, and their origins join the policy for scripts, connections
// and images. Files are left alone.
func TestWhatABinaryAddsToThePagesIsServedAndAllowed(t *testing.T) {
	s, _, _ := newTenantServer(t)
	s.opts.UI = fstest.MapFS{
		"console/index.html":    {Data: []byte("<html><head><title>c</title></head><body><script>console()</script>console</body></html>")},
		"console/app.js":        {Data: []byte("console js")},
		"workspace/index.html":  {Data: []byte("<html><head></head><body>workspace</body></html>")},
		"workspace/people.html": {Data: []byte("<html>no closing tags")},
	}
	s.opts.Pages = PageAdditions{
		Head:    `<script async src="https://cdn.example.com/a.js"></script><script>track()</script>`,
		Body:    `<footer>cloud</footer>`,
		Origins: []string{"https://cdn.example.com", "https://api.example.com"},
	}
	s.engine.NoRoute(s.serveUI())

	rec := get(t, s, "shpyrd.example.test", "/")
	want := `<html><head><title>c</title><script async src="https://cdn.example.com/a.js"></script><script>track()</script></head><body><script>console()</script>console<footer>cloud</footer></body></html>`
	if got := rec.Body.String(); got != want {
		t.Errorf("page with additions:\n got %s\nwant %s", got, want)
	}
	wantPolicy := "default-src 'self'; img-src 'self' data: https://cdn.example.com https://api.example.com; style-src 'self' 'unsafe-inline'; font-src 'self' data:; connect-src 'self' https://cdn.example.com https://api.example.com; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; script-src 'self' https://cdn.example.com https://api.example.com " + hashOf("track()") + " " + hashOf("console()")
	if got := rec.Header().Get("Content-Security-Policy"); got != wantPolicy {
		t.Errorf("policy with origins:\n got %s\nwant %s", got, wantPolicy)
	}
	// A page without scripts of its own still allows the added one.
	rec = get(t, s, "acme.shpyrd.test", "/")
	if got := rec.Body.String(); got != `<html><head><script async src="https://cdn.example.com/a.js"></script><script>track()</script></head><body>workspace<footer>cloud</footer></body></html>` {
		t.Errorf("workspace page = %s", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.HasSuffix(got, "script-src 'self' https://cdn.example.com https://api.example.com "+hashOf("track()")) {
		t.Errorf("policy of a page without scripts of its own = %s", got)
	}
	// Without the closing tags nothing is written in; the page is served
	// as it is.
	if got := get(t, s, "acme.shpyrd.test", "/people").Body.String(); got != "<html>no closing tags" {
		t.Errorf("page without closing tags = %s", got)
	}
	if got := get(t, s, "shpyrd.example.test", "/app.js").Body.String(); got != "console js" {
		t.Errorf("file = %s", got)
	}
}

func TestWithoutAdditionsThePagesAreWhatTheBuildWrote(t *testing.T) {
	s := uiServer(t)
	if got := get(t, s, "acme.shpyrd.test", "/").Body.String(); got != "<html>workspace</html>" {
		t.Errorf("page = %q", got)
	}
	if got := policy(nil, nil); got != basePolicy {
		t.Errorf("policy without origins or hashes = %s", got)
	}
}

// Another application of the binary is served at the console under
// /apps/<folder>/, with its files and the policy of its scripts; not at a
// workspace's host, where the address is the workspace's own.
func TestTheConsoleServesTheOtherApplicationsUnderApps(t *testing.T) {
	s := uiServer(t)
	for _, p := range []string{"/apps/workspaces", "/apps/workspaces/", "/apps/workspaces/acme"} {
		rec := get(t, s, "shpyrd.example.test", p)
		if !strings.HasSuffix(rec.Body.String(), "the cloud's workspaces</html>") || !strings.Contains(rec.Header().Get("Content-Security-Policy"), hashOf("list()")) {
			t.Errorf("%s = %q %q", p, rec.Body.String(), rec.Header().Get("Content-Security-Policy"))
		}
	}
	if got := get(t, s, "shpyrd.example.test", "/apps/workspaces/_next/x.js").Body.String(); got != "their chunk" {
		t.Errorf("their file = %q", got)
	}
	if got := get(t, s, "shpyrd.example.test", "/apps/nothing").Body.String(); !strings.HasSuffix(got, "console</html>") {
		t.Errorf("no such application = %q", got)
	}
	if got := get(t, s, "acme.shpyrd.test", "/apps/workspaces").Body.String(); got != "<html>workspace</html>" {
		t.Errorf("at a workspace = %q", got)
	}
}
