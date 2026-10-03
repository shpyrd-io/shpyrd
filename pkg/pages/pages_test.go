package pages

import (
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"strings"
	"testing"
)

// The markup of a page, without its stylesheet.
var styles = regexp.MustCompile(`(?s)<style>.*?</style>`)

func markup(html string) string { return styles.ReplaceAllString(html, "") }

func TestAPageSaysWhatItIsGivenAndNothingElse(t *testing.T) {
	html := HTML(NoAccess, Page{
		Title: "No access to <shop>",
		Text:  "Ask an admin.",
		Links: Links(map[string]string{"Sign in": "/.shpyrd/signin?x=1&y=2", "Console": "https://console.example"}),
	})
	for _, want := range []string{
		"<title>No access to &lt;shop&gt;</title>",
		"<h1", "No access to &lt;shop&gt;</h1>",
		"Ask an admin.",
		`href="https://console.example"`, ">Console</a>",
		`href="/.shpyrd/signin?x=1&amp;y=2"`, ">Sign in</a>",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	for _, stray := range []string{"{{", "}}", "<script", "http-equiv", "@font-face", "/_next/"} {
		if strings.Contains(markup(html), stray) {
			t.Errorf("the page still has %q", stray)
		}
	}
	if i := strings.Index(html, ">Console</a>"); i > strings.Index(html, ">Sign in</a>") {
		t.Errorf("the links are not in the order of their labels")
	}
}

func TestAPageThatGoesAwayByItselfRefreshes(t *testing.T) {
	html := HTML(Waking, Page{Title: "Waking shop up.", Text: "One moment.", Refresh: 3})
	if !strings.Contains(html, `<meta http-equiv="refresh" content="3">`) {
		t.Errorf("the page does not refresh itself")
	}
	if strings.Contains(HTML(Waking, Page{Title: "x"}), "http-equiv") {
		t.Errorf("a page that stays refreshes itself")
	}
}

func TestTheMarkAloneSaysNothing(t *testing.T) {
	html := HTML(Mark, Page{})
	if strings.Contains(html, "<h1") || strings.Contains(html, "<p ") || strings.Contains(html, "<p>") {
		t.Errorf("the mark alone has words")
	}
	if !strings.Contains(html, "<title>shpyrd</title>") {
		t.Errorf("the mark alone is not named")
	}
	if !strings.Contains(html, `aria-label="shpyrd"`) {
		t.Errorf("the wordmark is missing")
	}
}

func TestEveryPageIsSelfContained(t *testing.T) {
	for _, kind := range []Kind{Nothing, NoAccess, Waking, Mark} {
		html := HTML(kind, Page{Title: "t", Text: "x"})
		for _, stray := range []string{"<script", "<link", "url(", "/_next/"} {
			if strings.Contains(html, stray) {
				t.Errorf("%s reaches out: %q", kind, stray)
			}
		}
		if !strings.Contains(html, "prefers-color-scheme:dark") {
			t.Errorf("%s does not follow the system's theme", kind)
		}
	}
}

func TestThePageThatMovesCarriesItsScriptAndItsHash(t *testing.T) {
	html := HTML(ServiceUnavailable, Page{Title: "Service unavailable", Text: "Try again in a few moments."})
	start := strings.Index(html, "<script>")
	end := strings.LastIndex(html, "</script>")
	if start < 0 || end < start || !strings.HasSuffix(html[end:], "</script></body></html>") {
		t.Fatalf("the script is not at the end of the page")
	}
	sum := sha256.Sum256([]byte(html[start+len("<script>") : end]))
	if want := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"; ScriptHash(ServiceUnavailable) != want {
		t.Errorf("the hash is %q, the script's is %q", ScriptHash(ServiceUnavailable), want)
	}
	if !strings.Contains(html, `data-slot="shipyard"`) || strings.Contains(markup(html), "/_next/") {
		t.Errorf("the shipyard is missing, or points at files the page has not")
	}
	if ScriptHash(Nothing) != "" || strings.Contains(HTML(Nothing, Page{Title: "x"}), "<script") {
		t.Errorf("a page that stands still has a script")
	}
}

func TestTheConsentSaysWhoAsksWhereTheAnswerGoesAndAnswers(t *testing.T) {
	page := ConsentPage{
		Title: "Allow <Claude>?", Client: "<Claude>", Icon: "claude", Initial: "C", Workspace: "Acme", Account: "joao@acme.com",
		Host:   "claude.ai",
		Scopes: []Scope{{Text: "See your projects"}, {Text: "Change your projects"}},
		Fields: []Field{{Name: "client_id", Value: "c1"}, {Name: "state", Value: `a"b`}},
	}
	html := HTML(Consent, page)
	for _, want := range []string{
		"Allow &lt;Claude&gt; to use Acme?", ">claude.ai</strong>", ">joao@acme.com</strong>",
		"See your projects", "Change your projects",
		`name="client_id" value="c1"`, `name="state" value="a&#34;b"`,
		`action="/oauth/authorize"`, `value="allow" name="decision"`, `value="deny" name="decision"`,
		`aria-label="Claude"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the consent lacks %q", want)
		}
	}
	for _, stray := range []string{"{{", "}}", "<script", "<img"} {
		if strings.Contains(markup(html), stray) {
			t.Errorf("the consent still has %q", stray)
		}
	}
	page.Icon = ""
	if html := HTML(Consent, page); strings.Contains(html, `aria-label="Claude"`) || !strings.Contains(html, ">C</span>") {
		t.Errorf("a client the page has no mark for is not drawn by its initial")
	}
}
