// Package pages holds the pages the server serves by itself, where no
// application answers: nothing here, no access, waking up, and the mark
// alone. They are drawn from the design library by design/pages (`make
// pages`) and embedded here as one self-contained file each: no script,
// no font to fetch, the dark theme the system's. The words are the
// server's, written into the page as a template.
package pages

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"sort"
)

//go:embed html/*.html
var files embed.FS

// Kind is which page: what it shows over the words.
type Kind string

const (
	// Nothing answers here: the mark over the words.
	Nothing Kind = "nothing"
	// This is not for the person: a lock over the words.
	NoAccess Kind = "no-access"
	// Wait a moment: a rocket, and a spinner under the words.
	Waking Kind = "waking"
	// Nothing to say: the mark alone. The words are not shown.
	Mark Kind = "mark"
)

// Link is something the person can go to from the page.
type Link struct {
	Label string
	URL   string
}

// Page is what a page says.
type Page struct {
	Title string
	Text  string
	Links []Link
	// Refresh is the seconds after which the page opens itself again,
	// for a page that goes away by itself; 0 for a page that stays.
	Refresh int
}

var templates = map[Kind]*template.Template{}

func init() {
	for _, kind := range []Kind{Nothing, NoAccess, Waking, Mark} {
		src, err := files.ReadFile("html/" + string(kind) + ".html")
		if err != nil {
			panic(fmt.Sprintf("pages: %s: %v", kind, err))
		}
		templates[kind] = template.Must(template.New(string(kind)).Parse(string(src)))
	}
}

// HTML is the page of a kind, saying what it is given.
func HTML(kind Kind, page Page) string {
	t, ok := templates[kind]
	if !ok {
		t = templates[Nothing]
	}
	var b bytes.Buffer
	if err := t.Execute(&b, page); err != nil {
		panic(fmt.Sprintf("pages: %s: %v", kind, err))
	}
	return b.String()
}

// Links makes the links of a page from labels and their addresses, in
// the order of the labels.
func Links(byLabel map[string]string) []Link {
	labels := make([]string, 0, len(byLabel))
	for label := range byLabel {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	links := make([]Link, 0, len(labels))
	for _, label := range labels {
		links = append(links, Link{Label: label, URL: byLabel[label]})
	}
	return links
}
