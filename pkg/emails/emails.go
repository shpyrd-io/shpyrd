// Package emails holds the emails the platform sends, as HTML. They are
// drawn by design/emails (`make emails`) and embedded here as one Go
// template each, with their subjects beside them; a sender fills in the
// words. The plain-text part of a message stays the sender's.
package emails

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"
)

//go:embed html
var files embed.FS

// Name is which email.
type Name string

const (
	// A person is invited to a workspace and signs in through a provider.
	Invite Name = "invite"
	// A person is invited to a workspace with password sign-in.
	InvitePassword Name = "invite-password"
	// A person asked for a new password.
	Reset Name = "reset"
	// A person's password was changed.
	PasswordChanged Name = "password-changed"
	// A person signing up proves the address with a code.
	Code Name = "code"
	// The owners of a workspace are told what happened to it.
	Notice Name = "notice"
	// An administrator checks that the platform's mail is sent.
	Test Name = "test"
)

// Words are what an email says, by the name of its mark ({{.Door}}).
// Every mark the email has must be given.
type Words map[string]string

type email struct {
	subject *texttemplate.Template
	body    *htmltemplate.Template
}

var all = map[Name]email{}

func init() {
	raw, err := files.ReadFile("html/subjects.json")
	if err != nil {
		panic(fmt.Sprintf("emails: %v", err))
	}
	var subjects map[Name]string
	if err := json.Unmarshal(raw, &subjects); err != nil {
		panic(fmt.Sprintf("emails: subjects: %v", err))
	}
	for name, subject := range subjects {
		src, err := files.ReadFile("html/" + string(name) + ".html")
		if err != nil {
			panic(fmt.Sprintf("emails: %s: %v", name, err))
		}
		all[name] = email{
			subject: texttemplate.Must(texttemplate.New(string(name)).Option("missingkey=error").Parse(subject)),
			body:    htmltemplate.Must(htmltemplate.New(string(name)).Option("missingkey=error").Parse(string(src))),
		}
	}
}

// Render is the subject and the HTML of an email, saying what it is
// given. A word the email needs and was not given is an error.
func Render(name Name, words Words) (subject, html string, err error) {
	e, ok := all[name]
	if !ok {
		return "", "", fmt.Errorf("emails: no email %q", name)
	}
	var s, b bytes.Buffer
	if err := e.subject.Execute(&s, words); err != nil {
		return "", "", fmt.Errorf("emails: %s: subject: %w", name, err)
	}
	if err := e.body.Execute(&b, words); err != nil {
		return "", "", fmt.Errorf("emails: %s: %w", name, err)
	}
	return strings.Join(strings.Fields(s.String()), " "), b.String(), nil
}

// Logo is the address of the mark an email shows, on the door the email
// is sent from (https://acme.shpyrd.app): the applications serve it.
func Logo(door string) string {
	return strings.TrimSuffix(door, "/") + "/logo.png"
}

// Host is a door without its scheme, as an email names it.
func Host(door string) string {
	return strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(door, "https://"), "http://"), "/")
}
