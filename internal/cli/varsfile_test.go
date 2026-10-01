package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseVarsFileDotenv(t *testing.T) {
	raw := `# production
export DATABASE_URL=postgres://db/shop
SECRET_KEY="line1\nline2 \"quoted\""
MOTTO='keep "it" simple'
PORT=8080 # the web port
EMPTY=
URL=https://example.com/#anchor
`
	got, err := parseVarsFile([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"DATABASE_URL": "postgres://db/shop",
		"SECRET_KEY":   "line1\nline2 \"quoted\"",
		"MOTTO":        `keep "it" simple`,
		"PORT":         "8080",
		"EMPTY":        "",
		"URL":          "https://example.com/#anchor",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestParseVarsFileJSON(t *testing.T) {
	got, err := parseVarsFile([]byte(`  {"A": "1", "B": "two words"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, map[string]string{"A": "1", "B": "two words"}) {
		t.Fatalf("got %#v", got)
	}
	if _, err := parseVarsFile([]byte(`{"A": 1}`)); err == nil {
		t.Fatal("a non-string value must be refused")
	}
}

func TestParseVarsFileRefusesBadLines(t *testing.T) {
	for _, raw := range []string{"NOEQUALS\n", "=value\n", `KEY="open`} {
		if _, err := parseVarsFile([]byte(raw)); err == nil {
			t.Fatalf("%q was accepted", raw)
		}
	}
}

func TestVarsToSetMergesArgumentsOverFile(t *testing.T) {
	root := New()
	root.SetIn(strings.NewReader("A=file\nB=file\n"))
	got, err := varsToSet(root, []string{"B=arg", "C=arg"}, "-")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, map[string]string{"A": "file", "B": "arg", "C": "arg"}) {
		t.Fatalf("got %#v", got)
	}
	if _, err := varsToSet(root, nil, ""); err == nil {
		t.Fatal("no source must be an error")
	}
}
