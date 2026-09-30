package api

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/pages"
)

// The applications the server serves (RFC-0080): the static files Next
// exported, a folder per application in the embedded file system, chosen
// by the host: console/ at the console, workspace/ everywhere else. Inside
// the folder, for any address, in this order: a file of the build by its
// path; the page built for the address (<address>.html); the entry,
// index.html. No route is declared.
//
// Next writes scripts into every page. When the server starts it reads
// each page it embeds, takes the SHA-256 of every such script, and sends
// the page with the policy of every response plus `script-src 'self'` and
// those hashes.

const (
	uiConsole   = "console"
	uiWorkspace = "workspace"
)

// basePolicy is the Content-Security-Policy of every response that is not
// the API's (RFC-0008): the applications are same-origin, scripts and
// styles come from this server only.
const basePolicy = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// policy is the base policy, with the scripts a page may run when it has
// scripts written into it: its own files, and those, by their hashes.
func policy(hashes []string) string {
	if len(hashes) == 0 {
		return basePolicy
	}
	return basePolicy + "; script-src 'self' " + strings.Join(hashes, " ")
}

var inlineScript = regexp.MustCompile(`(?is)<script(\s[^>]*)?>(.*?)</script>`)

// scriptHashes are the hashes of the scripts written into a page, in the
// form the policy takes; a script the page loads by address has none.
func scriptHashes(page []byte) []string {
	var hashes []string
	for _, m := range inlineScript.FindAllSubmatch(page, -1) {
		if strings.Contains(strings.ToLower(string(m[1])), "src=") {
			continue
		}
		sum := sha256.Sum256(m[2])
		hashes = append(hashes, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	return hashes
}

// application is one built application: its folder, and the hashes of
// the scripts of each of its pages, by path within the folder.
type application struct {
	files fs.FS
	serve http.Handler
	pages map[string][]string
}

// loadApplication reads an application's folder; nil when it was not
// built into the file system.
func loadApplication(root fs.FS, name string) *application {
	files, err := fs.Sub(root, name)
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(files, "index.html"); err != nil {
		return nil
	}
	a := &application{files: files, serve: http.FileServer(http.FS(files)), pages: map[string][]string{}}
	_ = fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") {
			return nil
		}
		body, err := fs.ReadFile(files, p)
		if err != nil {
			return nil
		}
		a.pages[p] = scriptHashes(body)
		return nil
	})
	return a
}

// handle answers an address of the application.
func (a *application) handle(c *gin.Context) {
	p := strings.TrimPrefix(path.Clean("/"+c.Request.URL.Path), "/")
	if p == "" {
		a.page(c, "index.html")
		return
	}
	if st, err := fs.Stat(a.files, p); err == nil && !st.IsDir() {
		if strings.HasSuffix(p, ".html") {
			a.page(c, p)
			return
		}
		a.serve.ServeHTTP(c.Writer, c.Request)
		return
	}
	if _, ok := a.pages[p+".html"]; ok {
		a.page(c, p+".html")
		return
	}
	a.page(c, "index.html")
}

// page sends one page of the application with the policy that lets its
// scripts run.
func (a *application) page(c *gin.Context, name string) {
	body, err := fs.ReadFile(a.files, name)
	if err != nil {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(placeholderHTML))
		return
	}
	c.Header("Cache-Control", "no-cache")
	c.Header("Content-Security-Policy", policy(a.pages[name]))
	c.Data(http.StatusOK, "text/html; charset=utf-8", body)
}

// serveUI serves the embedded applications: the host's, by the rule
// above; the placeholder where the host's was not built.
func (s *Server) serveUI() gin.HandlerFunc {
	apps := map[string]*application{}
	for _, name := range []string{uiConsole, uiWorkspace} {
		if a := loadApplication(s.opts.UI, name); a != nil {
			apps[name] = a
		}
	}
	return func(c *gin.Context) {
		if s.atSignInHost(c) {
			c.Header("Cache-Control", "no-store")
			c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(pages.HTML(pages.Mark, pages.Page{})))
			return
		}
		if s.customError(c) { // ingress-nginx's error backend for app hosts
			return
		}
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		name := uiWorkspace
		if s.atConsole(c) {
			name = uiConsole
		}
		app := apps[name]
		if app == nil {
			c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(placeholderHTML))
			return
		}
		app.handle(c)
	}
}
