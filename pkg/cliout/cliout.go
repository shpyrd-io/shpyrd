// Package cliout prints a command's result for two readers. People get the
// command's own text. Programs and agents pass --json and get one JSON
// document on stdout and nothing else, or --jq and get the outputs of a jq
// expression over that document (strings raw, everything else as JSON, the
// `gh --jq` convention).
package cliout

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/itchyny/gojq"
)

// Printer carries the two flags. The zero value prints for people.
type Printer struct {
	JSON bool   // --json
	JQ   string // --jq; implies JSON
}

// Machine says a program is reading stdout: print JSON, keep text off it.
func (p *Printer) Machine() bool { return p.JSON || p.JQ != "" }

// Print writes the result of a command: v as JSON (or through the jq
// expression) when a program is reading, otherwise whatever human writes.
// A nil human with a machine reader absent prints v as JSON anyway, so a
// command that has no text form still answers.
func (p *Printer) Print(w io.Writer, v any, human func(w io.Writer)) error {
	if p.JQ != "" {
		return p.jq(w, v)
	}
	if p.JSON || human == nil {
		return encode(w, v, "  ")
	}
	human(w)
	return nil
}

// Progress is where a command narrates what it is doing (progress lines,
// hints): stdout for people, stderr when a program reads stdout, so the
// JSON document stays alone there.
func (p *Printer) Progress(stdout, stderr io.Writer) io.Writer {
	if p.Machine() {
		return stderr
	}
	return stdout
}

func encode(w io.Writer, v any, indent string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", indent)
	enc.SetEscapeHTML(false) // URLs with & must survive copy and paste
	return enc.Encode(v)
}

func (p *Printer) jq(w io.Writer, v any) error {
	query, err := gojq.Parse(p.JQ)
	if err != nil {
		return fmt.Errorf("--jq: %w", err)
	}
	// Round-trip through JSON so structs and RawMessages behave like
	// plain objects, which is what jq expects.
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var input any
	if err := json.Unmarshal(raw, &input); err != nil {
		return err
	}
	iter := query.Run(input)
	for {
		out, ok := iter.Next()
		if !ok {
			return nil
		}
		if err, isErr := out.(error); isErr {
			return fmt.Errorf("--jq: %w", err)
		}
		if s, isStr := out.(string); isStr {
			fmt.Fprintln(w, s)
			continue
		}
		if err := encode(w, out, ""); err != nil {
			return err
		}
	}
}
