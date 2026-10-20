package deploy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"html"
	"html/template"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Masterminds/sprig/v3"
	"sigs.k8s.io/yaml"
)

// The pages Dex serves itself are drawn by design/pages and written into
// web.yaml; these tests load them as Dex v2.45 does (server/templates.go:
// one set, sprig and Dex's own functions) and run each with the words Dex
// gives it.

func dexWeb(t *testing.T) map[string]string {
	t.Helper()
	raw, err := fs.ReadFile(FS, "components/dex/base/web.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var cm struct {
		Data map[string]string `json:"data"`
	}
	if err := yaml.Unmarshal(raw, &cm); err != nil {
		t.Fatal(err)
	}
	return cm.Data
}

func dexTemplates(t *testing.T, extra map[string]string) *template.Template {
	t.Helper()
	funcs := sprig.FuncMap()
	// Dex's own; url makes an asset's path relative to the request's.
	funcs["extra"] = func(k string) string { return extra[k] }
	funcs["issuer"] = func() string { return "shpyrd" }
	funcs["logo"] = func() string { return "theme/logo.svg" }
	funcs["url"] = func(reqPath, assetPath string) string { return "/" + assetPath }
	set := template.New("").Funcs(funcs)
	for name, text := range dexWeb(t) {
		if strings.HasSuffix(name, ".html") {
			template.Must(set.New(name).Parse(text))
		}
	}
	return set
}

func render(t *testing.T, set *template.Template, name string, data any) string {
	t.Helper()
	tmpl := set.Lookup(name)
	if tmpl == nil {
		t.Fatalf("no template %s", name)
	}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, data); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return b.String()
}

var heading = regexp.MustCompile(`<h1[^>]*>([^<]*)</h1>`)

func TestDexHasEveryTemplateItRequires(t *testing.T) {
	set := dexTemplates(t, nil)
	for _, name := range []string{"login.html", "approval.html", "password.html", "oob.html", "error.html", "device.html", "device_success.html"} {
		if set.Lookup(name) == nil {
			t.Errorf("web.yaml has no %s", name)
		}
	}
}

func TestADexErrorSaysWhatWentWrongInOurWords(t *testing.T) {
	set := dexTemplates(t, map[string]string{"home": "https://console.example.com"})
	type errData struct{ ErrType, ErrMsg, ReqPath string }
	for msg, want := range map[string]string{
		"Requested resource does not exist.": "This sign-in has expired",
		"User session error.":                "This sign-in has expired",
		"User session has expired.":          "This sign-in has expired",
		"Invalid or expired user code.":      "This sign-in has expired",
		"Authentication failed. Please contact your administrator or try again later.": "Sign-in didn't go through",
		"Login error.": "Sign-in didn't go through",
		"You are not a member of any of the required groups to authenticate.": "This account can't sign in here",
		"Approval rejected.": "Sign-in cancelled",
		"Database error.":    "Something went wrong",
	} {
		page := render(t, set, "error.html", errData{"Bad Request", msg, "/auth"})
		m := heading.FindStringSubmatch(page)
		if m == nil || html.UnescapeString(strings.TrimSpace(m[1])) != want {
			t.Errorf("%q: heading %v, want %q", msg, m, want)
		}
		if !strings.Contains(page, `href="https://console.example.com"`) {
			t.Errorf("%q: no way to start again at home", msg)
		}
		// Dex's own words show only when ours have none for them.
		if shows := strings.Contains(page, template.HTMLEscapeString(msg)); shows != (want == "Something went wrong") {
			t.Errorf("%q: Dex's words shown = %v", msg, shows)
		}
	}
}

func TestADexErrorOffersNoWayBackWhenNoHomeIsSet(t *testing.T) {
	set := dexTemplates(t, nil)
	page := render(t, set, "error.html", struct{ ErrType, ErrMsg, ReqPath string }{"Bad Request", "Requested resource does not exist.", "/auth"})
	if strings.Contains(page, "Start again</a>") {
		t.Error("a link to start again with no home to go to")
	}
}

func TestThePasswordPagePostsWhatDexReads(t *testing.T) {
	set := dexTemplates(t, nil)
	type pw struct {
		PostURL, BackLink, Username, UsernamePrompt string
		Invalid                                     bool
		ReqPath                                     string
	}
	page := render(t, set, "password.html", pw{"/auth/local/login?back=&state=s1", "/auth?back", "ana@acme.com", "Email Address", true, "/auth/local/login"})
	for _, want := range []string{
		`action="/auth/local/login?back=&amp;state=s1"`,
		`name="login"`, `name="password"`, `value="ana@acme.com"`,
		"Wrong email address or password.",
		`href="/auth?back"`,
		`href="/theme/styles.css"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("password page has no %s", want)
		}
	}
	quiet := render(t, set, "password.html", pw{PostURL: "/p", UsernamePrompt: "Email Address", ReqPath: "/p"})
	if strings.Contains(quiet, "Wrong") || strings.Contains(quiet, "another way") {
		t.Error("a first try shows an error or a back link it was not given")
	}
}

func TestTheChooserLinksEveryConnector(t *testing.T) {
	set := dexTemplates(t, nil)
	type conn struct {
		ID, Name string
		URL      template.URL
		Type     string
	}
	page := render(t, set, "login.html", struct {
		Connectors []conn
		ReqPath    string
	}{[]conn{{"github", "GitHub", "/auth/github?state=s1", "github"}, {"local", "Email", "/auth/local?state=s1", "local"}}, "/auth"})
	for _, want := range []string{`href="/auth/github?state=s1"`, "Continue with GitHub", `src="/static/img/github-icon.svg"`, `href="/auth/local?state=s1"`, "Continue with Email"} {
		if !strings.Contains(page, want) {
			t.Errorf("chooser has no %s", want)
		}
	}
	if strings.Contains(page, "local-icon") {
		t.Error("an icon Dex does not ship")
	}
}

func TestTheOtherDexPagesRender(t *testing.T) {
	set := dexTemplates(t, nil)
	approval := render(t, set, "approval.html", struct {
		User, Client, AuthReqID string
		Scopes                  []string
		ReqPath                 string
	}{"ana@acme.com", "shpyrd", "req1", []string{"View your email address"}, "/approval"})
	for _, want := range []string{`name="req" value="req1"`, `value="approve"`, `value="rejected"`, "View your email address"} {
		if !strings.Contains(approval, want) {
			t.Errorf("approval has no %s", want)
		}
	}
	device := render(t, set, "device.html", struct {
		PostURL, UserCode string
		Invalid           bool
		ReqPath           string
	}{"/device/auth/verify_code", "BCDF-GHJK", true, "/device"})
	for _, want := range []string{`action="/device/auth/verify_code"`, `name="user_code"`, `value="BCDF-GHJK"`, "wrong or has expired"} {
		if !strings.Contains(device, want) {
			t.Errorf("device has no %s", want)
		}
	}
	render(t, set, "device_success.html", struct{ ClientName, ReqPath string }{"the CLI", "/device"})
	render(t, set, "oob.html", struct{ Code, ReqPath string }{"c0de", "/oob"})
}

func TestTheDeploymentIsMarkedWithThePagesItLays(t *testing.T) {
	data := dexWeb(t)
	names := make([]string, 0, len(data))
	for name := range data {
		names = append(names, name)
	}
	sort.Strings(names)
	sum := sha256.New()
	for _, name := range names {
		sum.Write([]byte(name + "\x00" + data[name] + "\x00"))
	}
	want := hex.EncodeToString(sum.Sum(nil))[:16]
	deployment, err := fs.ReadFile(FS, "components/dex/base/deployment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(deployment, []byte(`shpyrd.io/web: "`+want+`"`)) {
		t.Errorf("deployment.yaml is not marked with web.yaml's hash %s: run `make pages`", want)
	}
}
