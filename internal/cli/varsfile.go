package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Config vars from a file: `shpyrd secrets set --from-file .env` and
// `shpyrd globals set --from-file vars.json`. The file is dotenv (one
// KEY=VALUE per line, comments, blank lines, optional `export`, single or
// double quotes) or, when it starts with `{`, a JSON object of strings, the
// shape `--json` prints. "-" reads stdin.

// readConfigVarsFile reads path (or stdin for "-") and parses it.
func readConfigVarsFile(path string, stdin io.Reader) (map[string]string, error) {
	var raw []byte
	var err error
	if path == "-" {
		raw, err = io.ReadAll(stdin)
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("--from-file: %w", err)
	}
	vars, err := parseVarsFile(raw)
	if err != nil {
		return nil, fmt.Errorf("--from-file %s: %w", path, err)
	}
	return vars, nil
}

// parseVarsFile decodes a dotenv file or a JSON object of strings.
func parseVarsFile(raw []byte) (map[string]string, error) {
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		var vars map[string]string
		if err := json.Unmarshal(raw, &vars); err != nil {
			return nil, fmt.Errorf("not a JSON object of strings: %w", err)
		}
		for k := range vars {
			if k == "" {
				return nil, fmt.Errorf("empty variable name")
			}
		}
		return vars, nil
	}
	vars := map[string]string{}
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE, got %q", i+1, line)
		}
		value, err := unquoteEnvValue(strings.TrimSpace(v))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		vars[k] = value
	}
	return vars, nil
}

// unquoteEnvValue strips one pair of matching quotes. Double quotes take
// the usual escapes (\n, \t, \", \\); single quotes are literal; bare
// values stop at a ` #` comment.
func unquoteEnvValue(v string) (string, error) {
	switch {
	case len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"':
		s, err := unquoteDouble(v[1 : len(v)-1])
		if err != nil {
			return "", err
		}
		return s, nil
	case len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'':
		return v[1 : len(v)-1], nil
	case len(v) >= 1 && (v[0] == '"' || v[0] == '\''):
		return "", fmt.Errorf("unterminated quote in %q", v)
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v, nil
}

func unquoteDouble(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(s) {
			return "", fmt.Errorf("trailing backslash in %q", s)
		}
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '"', '\\', '$':
			b.WriteByte(s[i])
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String(), nil
}

// parseKeyValues turns KEY=VALUE arguments into a map.
func parseKeyValues(args []string) (map[string]string, error) {
	set := map[string]string{}
	for _, kv := range args {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("expected KEY=VALUE, got %q", kv)
		}
		set[k] = v
	}
	return set, nil
}

// varsToSet merges the file (when asked for) with the KEY=VALUE arguments;
// the arguments win. At least one source must be given.
func varsToSet(cmd *cobra.Command, args []string, fromFile string) (map[string]string, error) {
	if fromFile == "" && len(args) == 0 {
		return nil, fmt.Errorf("give KEY=VALUE arguments, --from-file <path>, or both")
	}
	set := map[string]string{}
	if fromFile != "" {
		vars, err := readConfigVarsFile(fromFile, cmd.InOrStdin())
		if err != nil {
			return nil, err
		}
		for k, v := range vars {
			set[k] = v
		}
	}
	inline, err := parseKeyValues(args)
	if err != nil {
		return nil, err
	}
	for k, v := range inline {
		set[k] = v
	}
	return set, nil
}
