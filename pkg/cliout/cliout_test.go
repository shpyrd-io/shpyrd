package cliout

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestPrintChoosesReader(t *testing.T) {
	v := map[string]any{"items": []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}}}
	var out bytes.Buffer
	p := &Printer{}
	if err := p.Print(&out, v, func(w io.Writer) { w.Write([]byte("two items\n")) }); err != nil {
		t.Fatal(err)
	}
	if out.String() != "two items\n" {
		t.Fatalf("people got %q", out.String())
	}

	out.Reset()
	p = &Printer{JSON: true}
	if err := p.Print(&out, v, func(w io.Writer) { t.Fatal("human ran") }); err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(out.Bytes(), &back); err != nil || len(back["items"].([]any)) != 2 {
		t.Fatalf("--json printed %q", out.String())
	}
	if !strings.Contains(out.String(), "\n  ") {
		t.Fatalf("--json is not indented: %q", out.String())
	}

	out.Reset()
	p = &Printer{}
	if err := p.Print(&out, json.RawMessage(`{"a":1}`), nil); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "{\n  \"a\": 1\n}" {
		t.Fatalf("raw message without a text form printed %q", out.String())
	}
}

func TestJQ(t *testing.T) {
	v := map[string]any{"items": []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}}}
	var out bytes.Buffer
	p := &Printer{JQ: ".items[].id"}
	if !p.Machine() {
		t.Fatal("--jq must imply --json")
	}
	if err := p.Print(&out, v, nil); err != nil {
		t.Fatal(err)
	}
	if out.String() != "a\nb\n" {
		t.Fatalf("strings print raw: got %q", out.String())
	}

	out.Reset()
	p.JQ = "{n: (.items | length)}"
	if err := p.Print(&out, v, nil); err != nil {
		t.Fatal(err)
	}
	if out.String() != "{\"n\":2}\n" {
		t.Fatalf("objects print as compact JSON: got %q", out.String())
	}

	p.JQ = ".["
	if err := p.Print(&out, v, nil); err == nil || !strings.Contains(err.Error(), "--jq") {
		t.Fatalf("bad expression: %v", err)
	}
}

func TestProgressLeavesStdoutToJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if (&Printer{}).Progress(&stdout, &stderr) != &stdout {
		t.Fatal("people read progress on stdout")
	}
	if (&Printer{JSON: true}).Progress(&stdout, &stderr) != &stderr {
		t.Fatal("programs must not get progress on stdout")
	}
}
