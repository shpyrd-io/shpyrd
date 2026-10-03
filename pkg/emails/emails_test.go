package emails

import (
	"strings"
	"testing"
)

// Words for every mark any email has.
func every() Words {
	return Words{
		"Logo": "https://acme.shpyrd.app/logo.png", "Door": "acme.shpyrd.app", "Email": "joao@acme.com",
		"Inviter": "Ana <ana@acme.com>", "Workspace": "Acme", "What": "as a developer", "Link": "https://acme.shpyrd.app/invite/x?a=1&b=2",
		"SetLink": "https://acme.shpyrd.app/account/set-password?token=t", "Until": "Oct 17, 2026", "When": "Oct 3, 2026 at 14:32 UTC",
		"Code": "482913", "Title": "Acme is paused", "Text": "It reached its limit.", "Action": "See the usage", "More": "Write to us.",
		"Host": "smtp.example.com", "Port": "587", "Security": "STARTTLS", "From": "shpyrd@acme.com", "Sent": "now",
	}
}

func TestEveryEmailIsEmbeddedAndFillsIn(t *testing.T) {
	for _, name := range []Name{Invite, InvitePassword, Reset, PasswordChanged, Code, Notice, Test} {
		subject, html, err := Render(name, every())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if subject == "" || strings.Contains(subject, "{{") {
			t.Errorf("%s: the subject is %q", name, subject)
		}
		for _, stray := range []string{"{{", "}}", "<script", "/_next/", "<!-- -->"} {
			if strings.Contains(html, stray) {
				t.Errorf("%s: the email still has %q", name, stray)
			}
		}
		if !strings.Contains(html, `src="https://acme.shpyrd.app/logo.png"`) {
			t.Errorf("%s: the email does not show the mark", name)
		}
	}
}

func TestTheWordsAreEscapedForWhereTheyGo(t *testing.T) {
	subject, html, err := Render(Invite, every())
	if err != nil {
		t.Fatal(err)
	}
	if subject != "Ana <ana@acme.com> invited you to Acme" {
		t.Errorf("the subject is %q: a subject is plain text", subject)
	}
	if !strings.Contains(html, "Ana &lt;ana@acme.com&gt;") {
		t.Errorf("the inviter is not escaped in the HTML")
	}
	if !strings.Contains(html, `href="https://acme.shpyrd.app/invite/x?a=1&amp;b=2"`) {
		t.Errorf("the link is not in the email as an address")
	}
}

func TestAWordNotGivenIsAnError(t *testing.T) {
	words := every()
	delete(words, "Code")
	if _, _, err := Render(Code, words); err == nil {
		t.Errorf("the code email was sent without its code")
	}
	if _, _, err := Render("nothing", every()); err == nil {
		t.Errorf("an email that does not exist was drawn")
	}
}

func TestTheMarkAndTheHostComeFromTheDoor(t *testing.T) {
	if got := Logo("https://acme.shpyrd.app/"); got != "https://acme.shpyrd.app/logo.png" {
		t.Errorf("Logo = %q", got)
	}
	if got := Host("https://acme.shpyrd.app/"); got != "acme.shpyrd.app" {
		t.Errorf("Host = %q", got)
	}
}
