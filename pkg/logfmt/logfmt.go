// Package logfmt reads one log line and says what it contains: a level, a
// message, a timestamp and the remaining fields.
//
// Applications log JSON in production, which reads badly as a wall of
// {"level":"info","msg":...}. Parse turns such a line into an Entry so
// `shpyrd logs --pretty` can render it, and leaves anything it cannot read as
// plain text. The dashboard does the same in ui/src/lib/logs.ts; the two are
// kept in step deliberately, so a change here belongs there too.
//
// Four shapes are recognised, tried in this order because each is more
// specific than the next:
//
//  1. a JSON object that is the whole line;
//  2. a prefix followed by a JSON object that runs to the end of the line —
//     what Go's standard log package produces, since log.Printf stamps
//     "2026/09/27 09:59:43 " in front of whatever it is given;
//  3. klog/glog, as every Kubernetes component and many Go binaries emit:
//     "I0927 09:59:43.123456   1 server.go:42] message";
//  4. logfmt, as logrus' text formatter and Go kit emit:
//     `level=info msg="request" method=GET`.
//
// Shapes 2 and 4 are the two that could mistake prose for a record, so each
// carries a guard: a prefixed object counts only when the object holds a
// well-known key, and a logfmt line only when every token is a key=value pair
// and one of them is a level or a message. Anything else stays text, which is
// the safe direction to be wrong in — a plain line rendered as plain is merely
// unhelpful, while prose rendered as a record loses words.
package logfmt

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Level is the severity bucket a line falls into, whatever the logger
// called it.
type Level string

const (
	LevelError Level = "error"
	LevelWarn  Level = "warn"
	LevelInfo  Level = "info"
	LevelDebug Level = "debug"
)

// Field is one key/value pair the line carried beyond the well-known keys.
type Field struct {
	Key   string
	Value string
}

// Entry is a parsed log line.
type Entry struct {
	// Structured is true when the line was read as a record — any of the
	// shapes listed in the package comment — rather than kept as text.
	Structured bool
	// Level is the severity bucket, from the level field or guessed from
	// the text.
	Level Level
	// LevelText is the level as the line spelled it; empty when the line
	// names none.
	LevelText string
	Message   string
	// Time is the line's own timestamp, when it carried one. The container
	// timestamp is a better column to sort by, so callers printing a time
	// of their own can ignore this.
	Time string
	// Fields are the remaining pairs in the order the line wrote them,
	// with any error field first.
	Fields []Field
}

// Well-known keys, in the spellings the common loggers use.
var (
	levelKeys   = []string{"level", "severity", "lvl", "log.level"}
	messageKeys = []string{"msg", "message", "event"}
	timeKeys    = []string{"time", "ts", "timestamp", "@timestamp"}
	errorKeys   = []string{"error", "err"}
)

var (
	errorWords = regexp.MustCompile(`\b(error|err|fatal|panic|exception|traceback|failed)\b`)
	warnWords  = regexp.MustCompile(`\b(warn|warning)\b`)
)

// Parse reads a single line. It never fails: a line none of the recognised
// shapes fit comes back as an unstructured Entry whose Message is the line.
func Parse(line string) Entry {
	body := strings.TrimSpace(line)
	if fields, ok := decodeObject(body); ok {
		return entryFrom(fields, "")
	}
	if prefix, fields, ok := decodePrefixedObject(body); ok {
		return entryFrom(fields, prefix)
	}
	if e, ok := parseKlog(body); ok {
		return e
	}
	if fields, ok := decodeLogfmt(body); ok {
		return entryFrom(fields, "")
	}
	return Entry{Level: guessLevel(line), Message: line}
}

// entryFrom maps a record's fields onto an Entry: the well-known keys become
// the level, message, time and error, and the rest stay in the order the line
// wrote them. prefix is whatever stood before the record on the line, and is
// never dropped: the timestamp in it becomes the entry's time when the record
// carries none of its own, and anything left over becomes a "prefix" field.
func entryFrom(fields []Field, prefix string) Entry {
	e := Entry{Structured: true}
	rest := make([]Field, 0, len(fields))
	var errValue string
	// Every well-known key is consumed, whichever spelling it used, so a
	// logger writing both "msg" and "message" does not show one of them as
	// a field; the first non-empty value of each wins.
	for _, f := range fields {
		switch {
		case matches(f.Key, levelKeys):
			e.LevelText = firstSet(e.LevelText, f.Value)
		case matches(f.Key, messageKeys):
			e.Message = firstSet(e.Message, f.Value)
		case matches(f.Key, timeKeys):
			e.Time = firstSet(e.Time, f.Value)
		case matches(f.Key, errorKeys):
			errValue = firstSet(errValue, f.Value)
		default:
			rest = append(rest, f)
		}
	}
	// The error reads as part of the message, so it leads the fields.
	if errValue != "" {
		e.Fields = append(e.Fields, Field{Key: "error", Value: errValue})
	}
	e.Fields = append(e.Fields, rest...)
	if prefix != "" {
		stamp, remainder := splitLogPrefix(prefix)
		// The record's own time is the application's and wins; the prefix's
		// then has nowhere to go but a field, which is better than losing it.
		if stamp != "" && e.Time == "" {
			e.Time = stamp
		} else if stamp != "" {
			remainder = strings.TrimSpace(prefix)
		}
		if remainder != "" {
			e.Fields = append(e.Fields, Field{Key: "prefix", Value: remainder})
		}
	}
	switch {
	case e.LevelText == "":
		e.Level = guessLevel(e.Message)
	default:
		e.Level = NormalizeLevel(e.LevelText)
	}
	return e
}

// Pretty renders the level, message and fields of a line as one string:
// "LEVEL message key=value...". Callers print their own time and instance
// columns around it; a line without a level starts at the message, so
// plain output is unchanged.
func (e Entry) Pretty() string {
	var parts []string
	if e.LevelText != "" {
		// The bucket, not the spelling: logrus writes "warning" and pino a
		// number, and the column has to stay one width. Padded so messages
		// line up across levels.
		parts = append(parts, pad(strings.ToUpper(string(e.Level)), 5))
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	for _, f := range e.Fields {
		parts = append(parts, f.Key+"="+quote(f.Value))
	}
	return strings.Join(parts, " ")
}

// NormalizeLevel folds a logger's level into a severity bucket, by name or
// by number: pino counts in tens up to 60, the syslog severities count down
// from 0, and both are common enough to read.
func NormalizeLevel(text string) Level {
	text = strings.TrimSpace(text)
	if n, err := strconv.Atoi(text); err == nil {
		return numericLevel(n)
	}
	switch strings.ToLower(text) {
	case "error", "err", "fatal", "crit", "critical", "panic", "alert", "emerg", "emergency":
		return LevelError
	case "warn", "warning":
		return LevelWarn
	case "debug", "trace":
		return LevelDebug
	default:
		return LevelInfo
	}
}

// numericLevel reads pino's scale (10 trace to 60 fatal) and, below 10, the
// syslog severities (0 emerg to 7 debug).
func numericLevel(n int) Level {
	if n < 10 {
		switch {
		case n <= 3:
			return LevelError
		case n == 4:
			return LevelWarn
		case n <= 6:
			return LevelInfo
		default:
			return LevelDebug
		}
	}
	switch {
	case n >= 50:
		return LevelError
	case n >= 40:
		return LevelWarn
	case n >= 30:
		return LevelInfo
	default:
		return LevelDebug
	}
}

// decodeObject reads a whole JSON object, keeping the source order of its
// keys; a repeated key keeps its first position and its last value, as
// JSON.parse does in the browser. Anything else (an array, a scalar, a
// truncated object, trailing content) is not a log record: ok is false.
func decodeObject(body string) ([]Field, bool) {
	if !strings.HasPrefix(body, "{") {
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(body))
	if _, err := dec.Token(); err != nil { // the opening brace
		return nil, false
	}
	var fields []Field
	index := map[string]int{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, ok := tok.(string)
		if !ok {
			return nil, false
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, false
		}
		value := renderValue(raw)
		if at, seen := index[key]; seen {
			fields[at].Value = value
			continue
		}
		index[key] = len(fields)
		fields = append(fields, Field{Key: key, Value: value})
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return nil, false
	}
	// A record is the whole line; "{} {}" or '{"a":1} x' is not one.
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	return fields, true
}

// renderValue is the one-line form of a JSON value: strings unquoted,
// everything else compact JSON.
func renderValue(raw json.RawMessage) string {
	// Only a JSON string is shown unquoted; null would also unmarshal into
	// a string, and reads better as "null".
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err == nil {
			return s
		}
	}
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		return string(raw)
	}
	return out.String()
}

// guessLevel reads a level out of a line that does not carry one.
func guessLevel(text string) Level {
	if len(text) > 200 {
		text = text[:200]
	}
	m := strings.ToLower(text)
	switch {
	case errorWords.MatchString(m):
		return LevelError
	case warnWords.MatchString(m):
		return LevelWarn
	default:
		return LevelInfo
	}
}

// firstSet keeps the value already found, or takes the new one.
func firstSet(have, next string) string {
	if have != "" {
		return have
	}
	return next
}

func matches(key string, keys []string) bool {
	for _, k := range keys {
		if key == k {
			return true
		}
	}
	return false
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// quote wraps a value in quotes when it would otherwise run into the next
// field. A nested object or array is left as it is: escaping the quotes it
// is made of turns it into a thicket.
func quote(v string) string {
	if strings.HasPrefix(v, "{") || strings.HasPrefix(v, "[") {
		return v
	}
	if v == "" || strings.ContainsAny(v, " \t\"") {
		return strconv.Quote(v)
	}
	return v
}

// decodePrefixedObject reads a line whose JSON object is preceded by
// something else: "2026/09/27 09:59:43 {...}", which is what Go's standard
// log package writes when an application hands it a marshalled record.
//
// The object has to run to the end of the line, and has to carry at least one
// well-known key. That last requirement is the guard against reading prose as
// a record: `failed to parse config {"a":1}` is a sentence that happens to end
// in JSON, and belongs on screen as the sentence it is. A line with no prefix
// at all is decodeObject's business, not this function's.
func decodePrefixedObject(body string) (prefix string, fields []Field, ok bool) {
	i := strings.IndexByte(body, '{')
	if i <= 0 {
		return "", nil, false
	}
	fields, ok = decodeObject(body[i:])
	if !ok || !hasWellKnownKey(fields) {
		return "", nil, false
	}
	return strings.TrimSpace(body[:i]), fields, true
}

// hasWellKnownKey reports whether a record names a level, a message, a time or
// an error — the evidence that it is a log record and not incidental data.
func hasWellKnownKey(fields []Field) bool {
	for _, f := range fields {
		if matches(f.Key, levelKeys) || matches(f.Key, messageKeys) ||
			matches(f.Key, timeKeys) || matches(f.Key, errorKeys) {
			return true
		}
	}
	return false
}

// prefixTimeLayouts are the stamps a prefix may open with, longest first so
// "2026/09/27 09:59:43" is not read as a bare date with leftovers. The first
// four are Go's log flags (Ldate, Ltime and Lmicroseconds in their
// combinations); the last two cover wrappers that stamp RFC3339 instead.
var prefixTimeLayouts = []string{
	"2006/01/02 15:04:05.000000",
	"2006/01/02 15:04:05",
	"2006/01/02",
	"15:04:05.000000",
	"15:04:05",
	time.RFC3339Nano,
	time.RFC3339,
}

// splitLogPrefix separates a leading timestamp from the rest of a prefix, so
// "2026/09/27 09:59:43 main.go:42:" yields the stamp and "main.go:42:" — the
// caller log.Lshortfile adds. A prefix that opens with no timestamp at all
// (log.SetPrefix's own string, say) comes back whole as rest.
//
// The candidate is validated by parsing it, the way pkg/api's splitTimestamp
// validates kubelet's: a regexp that merely looks like a date would promote
// anything shaped like one, and the cost of being wrong here is a word of the
// line disappearing into a timestamp column.
func splitLogPrefix(prefix string) (stamp, rest string) {
	prefix = strings.TrimSpace(prefix)
	words := strings.Fields(prefix)
	// A stamp is one or two words: a date, a time, or a date and a time.
	for n := 2; n >= 1; n-- {
		if len(words) < n {
			continue
		}
		candidate := strings.Join(words[:n], " ")
		for _, layout := range prefixTimeLayouts {
			if _, err := time.Parse(layout, candidate); err == nil {
				return candidate, strings.TrimSpace(strings.Join(words[n:], " "))
			}
		}
	}
	return "", prefix
}

// klogLine matches klog/glog's header: a severity letter, the month and day
// with no year, the time, the thread id, and the caller before a bracket.
var klogLine = regexp.MustCompile(`^([IWEF])(\d{4} \d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+\d+ ([^\]\s]+)\] (.*)$`)

// klogLevels expands the header's severity letter. The letter is an
// abbreviation of exactly these words, so LevelText carries the word: a bare
// "W" would fold to info through NormalizeLevel's default, turning every klog
// warning into an info line.
var klogLevels = map[string]string{"I": "info", "W": "warning", "E": "error", "F": "fatal"}

// parseKlog reads a klog/glog line. Its message may itself be a JSON record,
// but that case never reaches here: decodePrefixedObject runs first and takes
// it, keeping the klog header as the prefix, because the inner record says
// more about the line than the header does.
func parseKlog(body string) (Entry, bool) {
	m := klogLine.FindStringSubmatch(body)
	if m == nil {
		return Entry{}, false
	}
	fields := []Field{
		{Key: "level", Value: klogLevels[m[1]]},
		{Key: "time", Value: m[2]},
		{Key: "msg", Value: m[4]},
		// The caller is worth keeping; the thread id the header also carries
		// is not, and would otherwise put "thread=1" on every line.
		{Key: "source", Value: m[3]},
	}
	return entryFrom(fields, ""), true
}

// decodeLogfmt reads a line of key=value pairs, as logrus' text formatter, Go
// kit and Heroku's router emit. Every token must be a pair and one of them
// must be a level or a message; a line like "connection to db=primary failed"
// is prose with an "=" in it, and stays prose.
//
// Values are unquoted when quoted, so msg="request served" is one value rather
// than two tokens. A repeated key keeps its first position and last value,
// matching decodeObject.
func decodeLogfmt(body string) ([]Field, bool) {
	if body == "" {
		return nil, false
	}
	var fields []Field
	index := map[string]int{}
	for i := 0; i < len(body); {
		for i < len(body) && (body[i] == ' ' || body[i] == '\t') {
			i++
		}
		if i >= len(body) {
			break
		}
		start := i
		for i < len(body) && isKeyByte(body[i]) {
			i++
		}
		// A token that is not "key=" at all means this is not a logfmt line.
		if i == start || i >= len(body) || body[i] != '=' {
			return nil, false
		}
		key := body[start:i]
		i++ // the '='
		var value string
		if i < len(body) && body[i] == '"' {
			v, n, ok := scanQuoted(body[i:])
			if !ok {
				return nil, false
			}
			value, i = v, i+n
		} else {
			from := i
			for i < len(body) && body[i] != ' ' && body[i] != '\t' {
				i++
			}
			value = body[from:i]
		}
		// A quoted value has to end the token, so `msg="a"b` is not logfmt.
		if i < len(body) && body[i] != ' ' && body[i] != '\t' {
			return nil, false
		}
		if at, seen := index[key]; seen {
			fields[at].Value = value
			continue
		}
		index[key] = len(fields)
		fields = append(fields, Field{Key: key, Value: value})
	}
	if len(fields) == 0 {
		return nil, false
	}
	// The guard: without a level or a message this is data, not a log line.
	for _, f := range fields {
		if matches(f.Key, levelKeys) || matches(f.Key, messageKeys) {
			return fields, true
		}
	}
	return nil, false
}

// isKeyByte reports whether c may appear in a logfmt key. Deliberately narrow:
// the wider the key alphabet, the more prose a stray "=" can drag in.
func isKeyByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '_' || c == '.' || c == '-' || c == '@'
}

// scanQuoted reads a double-quoted value from the front of s, returning the
// unquoted text and how many bytes it spanned.
func scanQuoted(s string) (value string, n int, ok bool) {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++ // an escaped byte cannot close the string
		case '"':
			v, err := strconv.Unquote(s[:i+1])
			if err != nil {
				return "", 0, false
			}
			return v, i + 1, true
		}
	}
	return "", 0, false
}
