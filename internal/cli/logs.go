package cli

import (
	"errors"

	"github.com/shpyrd-io/shpyrd/pkg/logfmt"
)

// prettyLogs decides whether log lines are rendered or passed through:
// --pretty and --json say so explicitly, otherwise JSON lines are rendered
// for a human reading a terminal and left alone when the output is piped
// into another tool.
func prettyLogs(pretty, asJSON, terminal bool) (bool, error) {
	switch {
	case pretty && asJSON:
		return false, errors.New("--pretty and --json cannot be used together")
	case pretty:
		return true, nil
	case asJSON:
		return false, nil
	default:
		return terminal, nil
	}
}

// renderLogLine turns one container log line into what the user sees.
// A line none of pkg/logfmt's shapes fit comes back untouched, colour codes
// and all.
func renderLogLine(line string, pretty bool) string {
	if !pretty {
		return line
	}
	e := logfmt.Parse(line)
	if !e.Structured {
		return line
	}
	return e.Pretty()
}
