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
//
// A binary built on the core may ship other applications in the same file
// system, a folder each: the console serves each under /apps/<folder>/
// (Next's basePath), the same way. They are the binary's pages, linked
// from the console's sidebar (ext.Link), and carry their own way back.
//
// A binary built on the core may add to every page (Options.Pages): HTML
// before </head> and before </body>, and the origins those additions
// load scripts from, connect to and show images from. The additions are
// written into the pages as they are read, so their scripts are hashed
// like Next's, and the origins join the policy.

const (
	uiConsole   = "console"
	uiWorkspace = "workspace"
	// uiOthers is where the console serves the other applications.
	uiOthers = "/apps/"
)

// PageAdditions is what a binary built on the core writes into every page
// of the applications: Head goes before </head>, Body before </body>, and
// Origins are the sources beyond this server the additions may load
// scripts from, connect to and show images from ("https://cdn.example.com").
// The core adds nothing.
type PageAdditions struct {
	Head    string
	Body    string
	Origins []string
}

func (p PageAdditions) empty() bool {
	return p.Head == "" && p.Body == "" && len(p.Origins) == 0
}

// apply writes the additions into a page. A page without the closing tags
// is left as it is.
func (p PageAdditions) apply(page []byte) []byte {
	if p.Head != "" {
		page = insertBefore(page, "</head>", p.Head)
	}
	if p.Body != "" {
		page = insertBefore(page, "</body>", p.Body)
	}
	return page
}

func insertBefore(page []byte, tag, html string) []byte {
	i := strings.LastIndex(strings.ToLower(string(page)), tag)
	if i < 0 {
		return page
	}
	out := make([]byte, 0, len(page)+len(html))
	out = append(out, page[:i]...)
	out = append(out, html...)
	return append(out, page[i:]...)
}

// basePolicy is the Content-Security-Policy of every response that is not
// the API's (RFC-0008): the applications are same-origin, scripts and
// styles come from this server only.
const basePolicy = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// policy is the base policy, with the scripts a page may run when it has
// scripts written into it: its own files, and those, by their hashes;
// and, when a binary added origins to the pages, those origins for
// scripts, connections and images.
func policy(hashes, origins []string) string {
	if len(origins) == 0 {
		if len(hashes) == 0 {
			return basePolicy
		}
		return basePolicy + "; script-src 'self' " + strings.Join(hashes, " ")
	}
	more := " " + strings.Join(origins, " ")
	p := strings.Replace(basePolicy, "img-src 'self' data:", "img-src 'self' data:"+more, 1)
	p = strings.Replace(p, "connect-src 'self'", "connect-src 'self'"+more, 1)
	p += "; script-src 'self'" + more
	if len(hashes) > 0 {
		p += " " + strings.Join(hashes, " ")
	}
	return p
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

// application is one built application: its folder, and each of its
// pages by path within the folder, as served (with the additions written
// in) and with the hashes of the scripts in it.
type application struct {
	files   fs.FS
	serve   http.Handler
	pages   map[string]page
	origins []string
	// prefix is where the application answers ("" for the console's and
	// the workspace's, /apps/<folder> for the others).
	prefix string
}

type page struct {
	body   []byte
	hashes []string
}

// loadApplication reads an application's folder; nil when it was not
// built into the file system.
func loadApplication(root fs.FS, name string, add PageAdditions, prefix string) *application {
	files, err := fs.Sub(root, name)
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(files, "index.html"); err != nil {
		return nil
	}
	var serve http.Handler = http.FileServer(http.FS(files))
	if prefix != "" {
		serve = http.StripPrefix(prefix, serve)
	}
	a := &application{files: files, serve: serve, pages: map[string]page{}, origins: add.Origins, prefix: prefix}
	_ = fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") {
			return nil
		}
		body, err := fs.ReadFile(files, p)
		if err != nil {
			return nil
		}
		body = add.apply(body)
		a.pages[p] = page{body: body, hashes: scriptHashes(body)}
		return nil
	})
	return a
}

// handle answers an address of the application.
func (a *application) handle(c *gin.Context) {
	p := strings.TrimPrefix(strings.TrimPrefix(path.Clean("/"+c.Request.URL.Path), a.prefix), "/")
	if p == "" {
		a.page(c, "index.html")
		return
	}
	if _, ok := a.pages[p]; ok {
		a.page(c, p)
		return
	}
	if st, err := fs.Stat(a.files, p); err == nil && !st.IsDir() {
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
	pg, ok := a.pages[name]
	if !ok {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(placeholderHTML))
		return
	}
	c.Header("Cache-Control", "no-cache")
	c.Header("Content-Security-Policy", policy(pg.hashes, a.origins))
	c.Data(http.StatusOK, "text/html; charset=utf-8", pg.body)
}

// serveUI serves the embedded applications: the host's, by the rule
// above; the placeholder where the host's was not built.
func (s *Server) serveUI() gin.HandlerFunc {
	apps := map[string]*application{}
	for _, name := range []string{uiConsole, uiWorkspace} {
		if a := loadApplication(s.opts.UI, name, s.opts.Pages, ""); a != nil {
			apps[name] = a
		}
	}
	others := map[string]*application{}
	if dirs, err := fs.ReadDir(s.opts.UI, "."); err == nil {
		for _, d := range dirs {
			if name := d.Name(); d.IsDir() && name != uiConsole && name != uiWorkspace {
				if a := loadApplication(s.opts.UI, name, s.opts.Pages, uiOthers+name); a != nil {
					others[name] = a
				}
			}
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
		if s.toSuspendedGate(c) {
			return
		}
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		name := uiWorkspace
		if s.atConsole(c) {
			name = uiConsole
			if rest, ok := strings.CutPrefix(c.Request.URL.Path, uiOthers); ok {
				folder, _, _ := strings.Cut(rest, "/")
				if a := others[folder]; a != nil {
					a.handle(c)
					return
				}
			}
		}
		app := apps[name]
		if app == nil {
			c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(placeholderHTML))
			return
		}
		app.handle(c)
	}
}
