package logfmt_test

import (
	"bufio"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/logfmt"
)

func TestParsePlainLine(t *testing.T) {
	e := logfmt.Parse("Listening on :8080")
	if e.Structured {
		t.Errorf("Structured = true, want false")
	}
	if e.Message != "Listening on :8080" {
		t.Errorf("Message = %q", e.Message)
	}
	if e.Level != logfmt.LevelInfo {
		t.Errorf("Level = %q, want info", e.Level)
	}
	if e.LevelText != "" {
		t.Errorf("LevelText = %q, want empty", e.LevelText)
	}
	if len(e.Fields) != 0 {
		t.Errorf("Fields = %v, want none", e.Fields)
	}
}

func TestParseLevelFromPlainText(t *testing.T) {
	for line, want := range map[string]logfmt.Level{
		"Listening on :8080":      logfmt.LevelInfo,
		"WARN disk almost full":   logfmt.LevelWarn,
		"panic: runtime error":    logfmt.LevelError,
		"request failed after 3s": logfmt.LevelError,
	} {
		if got := logfmt.Parse(line).Level; got != want {
			t.Errorf("Parse(%q).Level = %q, want %q", line, got, want)
		}
	}
}

func TestParseStructured(t *testing.T) {
	e := logfmt.Parse(`{"level":"info","msg":"request served","time":"2026-09-25T10:00:00Z"}`)
	if !e.Structured {
		t.Fatalf("Structured = false, want true")
	}
	if e.Level != logfmt.LevelInfo || e.LevelText != "info" {
		t.Errorf("Level = %q/%q", e.Level, e.LevelText)
	}
	if e.Message != "request served" {
		t.Errorf("Message = %q", e.Message)
	}
	if e.Time != "2026-09-25T10:00:00Z" {
		t.Errorf("Time = %q", e.Time)
	}
	if len(e.Fields) != 0 {
		t.Errorf("Fields = %v, want none", e.Fields)
	}
}

func TestParseKeepsFieldOrder(t *testing.T) {
	e := logfmt.Parse(`{"msg":"hi","zebra":1,"alpha":"two","level":"warn"}`)
	want := []logfmt.Field{{Key: "zebra", Value: "1"}, {Key: "alpha", Value: "two"}}
	if !equal(e.Fields, want) {
		t.Errorf("Fields = %v, want %v", e.Fields, want)
	}
}

func TestParseErrorFieldLeads(t *testing.T) {
	e := logfmt.Parse(`{"msg":"boom","a":"1","err":"connection reset"}`)
	want := []logfmt.Field{{Key: "error", Value: "connection reset"}, {Key: "a", Value: "1"}}
	if !equal(e.Fields, want) {
		t.Errorf("Fields = %v, want %v", e.Fields, want)
	}
}

func TestParseNestedValues(t *testing.T) {
	e := logfmt.Parse(`{"msg":"hi","req":{"path":"/","n":2},"tags":["a","b"],"ok":true,"none":null}`)
	want := []logfmt.Field{
		{Key: "req", Value: `{"path":"/","n":2}`},
		{Key: "tags", Value: `["a","b"]`},
		{Key: "ok", Value: "true"},
		{Key: "none", Value: "null"},
	}
	if !equal(e.Fields, want) {
		t.Errorf("Fields = %v, want %v", e.Fields, want)
	}
}

func TestParseAliases(t *testing.T) {
	e := logfmt.Parse(`{"severity":"ERROR","message":"nope","@timestamp":"2026-09-25T10:00:00Z","error":"eof"}`)
	if e.LevelText != "ERROR" || e.Level != logfmt.LevelError {
		t.Errorf("Level = %q/%q", e.Level, e.LevelText)
	}
	if e.Message != "nope" || e.Time != "2026-09-25T10:00:00Z" {
		t.Errorf("Message = %q Time = %q", e.Message, e.Time)
	}
	want := []logfmt.Field{{Key: "error", Value: "eof"}}
	if !equal(e.Fields, want) {
		t.Errorf("Fields = %v, want %v", e.Fields, want)
	}
}

func TestParseAliasesLvlEventTs(t *testing.T) {
	e := logfmt.Parse(`{"lvl":"debug","event":"tick","ts":1758790000}`)
	if e.Level != logfmt.LevelDebug || e.Message != "tick" || e.Time != "1758790000" {
		t.Errorf("got %+v", e)
	}
}

func TestParseFoldsLevelNames(t *testing.T) {
	for text, want := range map[string]logfmt.Level{
		"fatal":   logfmt.LevelError,
		"WARNING": logfmt.LevelWarn,
		"trace":   logfmt.LevelDebug,
		"notice":  logfmt.LevelInfo,
	} {
		if got := logfmt.Parse(`{"level":"` + text + `","msg":"x"}`).Level; got != want {
			t.Errorf("level %q = %q, want %q", text, got, want)
		}
	}
}

func TestParseGuessesLevelFromMessage(t *testing.T) {
	if got := logfmt.Parse(`{"msg":"failed to connect"}`).Level; got != logfmt.LevelError {
		t.Errorf("Level = %q, want error", got)
	}
}

func TestParseOnlyLooksStructured(t *testing.T) {
	for _, line := range []string{
		"[1,2,3]",
		`{"msg":"cut off"`,
		`"hello"`,
		`{"a":1} trailing`,
		"{}{}",
	} {
		if e := logfmt.Parse(line); e.Structured {
			t.Errorf("Parse(%q) reported structured", line)
		} else if e.Message != line {
			t.Errorf("Parse(%q).Message = %q, want the line itself", line, e.Message)
		}
	}
}

func TestParseIndentedObject(t *testing.T) {
	if !logfmt.Parse(`   {"msg":"hi"}  `).Structured {
		t.Errorf("indented object not parsed")
	}
}

func TestParseEmptyMessage(t *testing.T) {
	e := logfmt.Parse(`{"level":"info","user":"ana"}`)
	if e.Message != "" {
		t.Errorf("Message = %q, want empty", e.Message)
	}
	want := []logfmt.Field{{Key: "user", Value: "ana"}}
	if !equal(e.Fields, want) {
		t.Errorf("Fields = %v, want %v", e.Fields, want)
	}
}

func TestParseRepeatedKeyKeepsLastValueInPlace(t *testing.T) {
	e := logfmt.Parse(`{"msg":"hi","a":"1","b":"2","a":"3"}`)
	want := []logfmt.Field{{Key: "a", Value: "3"}, {Key: "b", Value: "2"}}
	if !equal(e.Fields, want) {
		t.Errorf("Fields = %v, want %v", e.Fields, want)
	}
}

func TestPretty(t *testing.T) {
	for _, tc := range []struct{ line, want string }{
		{`{"level":"info","msg":"request served","path":"/","ms":12}`, `INFO  request served path=/ ms=12`},
		{`{"level":"error","msg":"boom","err":"eof"}`, `ERROR boom error=eof`},
		{`{"msg":"no level here"}`, `no level here`},
		{"plain line", "plain line"},
		{`{"level":"warn","msg":"slow","note":"took a while"}`, `WARN  slow note="took a while"`},
		{`{"level":"info","user":"ana"}`, `INFO  user=ana`},
	} {
		if got := logfmt.Parse(tc.line).Pretty(); got != tc.want {
			t.Errorf("Parse(%q).Pretty() = %q, want %q", tc.line, got, tc.want)
		}
	}
}

func equal(a, b []logfmt.Field) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Loggers occasionally emit two spellings of the same well-known key. Both
// are consumed, the first non-empty value wins, and neither shows up as a
// field: apps/shared/src/logs/logs.ts behaves the same way.
func TestParseDuplicateWellKnownKeys(t *testing.T) {
	e := logfmt.Parse(`{"msg":"a","message":"b","level":"","severity":"warn","keep":"1"}`)
	if e.Message != "a" {
		t.Errorf("Message = %q, want a", e.Message)
	}
	if e.LevelText != "warn" || e.Level != logfmt.LevelWarn {
		t.Errorf("Level = %q/%q, want warn", e.Level, e.LevelText)
	}
	want := []logfmt.Field{{Key: "keep", Value: "1"}}
	if !equal(e.Fields, want) {
		t.Errorf("Fields = %v, want %v", e.Fields, want)
	}
}

// pino and the syslog severities report the level as a number. "30" tells a
// reader nothing, so the bucket is what gets displayed; LevelText keeps the
// digits for anyone who wants what the line actually said.
func TestParseNumericLevels(t *testing.T) {
	for _, tc := range []struct {
		level string
		want  logfmt.Level
	}{
		{"10", logfmt.LevelDebug}, // pino trace
		{"20", logfmt.LevelDebug}, // pino debug
		{"30", logfmt.LevelInfo},  // pino info
		{"40", logfmt.LevelWarn},  // pino warn
		{"50", logfmt.LevelError}, // pino error
		{"60", logfmt.LevelError}, // pino fatal
		{"3", logfmt.LevelError},  // syslog err
		{"4", logfmt.LevelWarn},   // syslog warning
		{"6", logfmt.LevelInfo},   // syslog info
		{"7", logfmt.LevelDebug},  // syslog debug
	} {
		e := logfmt.Parse(`{"level":` + tc.level + `,"msg":"x"}`)
		if e.Level != tc.want {
			t.Errorf("level %s = %q, want %q", tc.level, e.Level, tc.want)
		}
		if e.LevelText != tc.level {
			t.Errorf("level %s: LevelText = %q, want the digits", tc.level, e.LevelText)
		}
	}
}

func TestPrettyDoesNotEscapeNestedValues(t *testing.T) {
	got := logfmt.Parse(`{"level":"error","msg":"job lost","job":{"id":"j-12","queue":"mail"},"tags":["a b"]}`).Pretty()
	want := `ERROR job lost job={"id":"j-12","queue":"mail"} tags=["a b"]`
	if got != want {
		t.Errorf("Pretty() = %q, want %q", got, want)
	}
}

// The label is the bucket, not the spelling the logger used, so the column
// stays four or five characters wide whatever the application writes.
func TestPrettyNormalizesTheLevelLabel(t *testing.T) {
	for _, tc := range []struct{ line, want string }{
		{`{"level":"warning","msg":"slow"}`, "WARN  slow"},
		{`{"level":"WARN","msg":"slow"}`, "WARN  slow"},
		{`{"level":"fatal","msg":"gone"}`, "ERROR gone"},
		{`{"level":"notice","msg":"fyi"}`, "INFO  fyi"},
		{`{"level":"trace","msg":"deep"}`, "DEBUG deep"},
		{`{"level":50,"msg":"upstream down"}`, "ERROR upstream down"},
	} {
		if got := logfmt.Parse(tc.line).Pretty(); got != tc.want {
			t.Errorf("Parse(%q).Pretty() = %q, want %q", tc.line, got, tc.want)
		}
	}
}

// The spelling the line used is still available to callers that want it.
func TestLevelTextKeepsTheSourceSpelling(t *testing.T) {
	if got := logfmt.Parse(`{"level":"warning","msg":"x"}`).LevelText; got != "warning" {
		t.Errorf("LevelText = %q, want warning", got)
	}
}

// A prefix before the JSON is what Go's standard log package produces:
// log.Printf("%s", b) stamps "2026/09/27 09:59:43 " in front of the payload.
// The record is still a record, so it must read as one.
func TestParsePrefixedObject(t *testing.T) {
	e := logfmt.Parse(`2026/09/27 09:59:43 {"level":"info","msg":"request","method":"GET","path":"/","status":200,"duration_ms":0,"remote":"10.0.1.204:37058"}`)
	if !e.Structured {
		t.Fatal("a stdlib-log prefix before the object left the line unparsed")
	}
	if e.Level != logfmt.LevelInfo || e.LevelText != "info" {
		t.Errorf("level = %q/%q, want info/info", e.Level, e.LevelText)
	}
	if e.Message != "request" {
		t.Errorf("Message = %q, want request", e.Message)
	}
	// The prefix parses as a time, so it becomes the entry's time rather
	// than a field.
	if e.Time != "2026/09/27 09:59:43" {
		t.Errorf("Time = %q, want the prefix timestamp", e.Time)
	}
	want := []logfmt.Field{
		{Key: "method", Value: "GET"},
		{Key: "path", Value: "/"},
		{Key: "status", Value: "200"},
		{Key: "duration_ms", Value: "0"},
		{Key: "remote", Value: "10.0.1.204:37058"},
	}
	if !equal(e.Fields, want) {
		t.Errorf("Fields = %v, want %v", e.Fields, want)
	}
}

func TestParsePrefixedObjectPrefixForms(t *testing.T) {
	for _, tc := range []struct {
		name, line, time, prefix string
	}{
		{"date and time", `2026/09/27 09:59:43 {"msg":"hi"}`, "2026/09/27 09:59:43", ""},
		{"microseconds", `2026/09/27 09:59:43.123456 {"msg":"hi"}`, "2026/09/27 09:59:43.123456", ""},
		{"time only", `09:59:43 {"msg":"hi"}`, "09:59:43", ""},
		{"date only", `2026/09/27 {"msg":"hi"}`, "2026/09/27", ""},
		{"RFC3339", `2026-09-27T09:59:43Z {"msg":"hi"}`, "2026-09-27T09:59:43Z", ""},
		// log.Lshortfile adds the caller after the time. Nothing is dropped:
		// what is not a timestamp is kept as a field.
		{"with caller", `2026/09/27 09:59:43 main.go:42: {"msg":"hi"}`, "2026/09/27 09:59:43", "main.go:42:"},
		// log.SetPrefix, and anything else that is not a time at all.
		{"no timestamp", `myapp {"msg":"hi"}`, "", "myapp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := logfmt.Parse(tc.line)
			if !e.Structured {
				t.Fatalf("Parse(%q) not structured", tc.line)
			}
			if e.Message != "hi" {
				t.Errorf("Message = %q, want hi", e.Message)
			}
			if e.Time != tc.time {
				t.Errorf("Time = %q, want %q", e.Time, tc.time)
			}
			var got string
			for _, f := range e.Fields {
				if f.Key == "prefix" {
					got = f.Value
				}
			}
			if got != tc.prefix {
				t.Errorf("prefix field = %q, want %q", got, tc.prefix)
			}
		})
	}
}

// The object's own time field is the application's, so it wins; the prefix is
// then kept as a field rather than thrown away.
func TestParsePrefixedObjectKeepsBothTimes(t *testing.T) {
	e := logfmt.Parse(`2026/09/27 09:59:43 {"time":"2026-09-27T09:59:43.101Z","msg":"hi"}`)
	if e.Time != "2026-09-27T09:59:43.101Z" {
		t.Errorf("Time = %q, want the object's own time", e.Time)
	}
	want := []logfmt.Field{{Key: "prefix", Value: "2026/09/27 09:59:43"}}
	if !equal(e.Fields, want) {
		t.Errorf("Fields = %v, want the prefix kept as a field", e.Fields)
	}
}

// The guard against reading incidental JSON in prose as a record: a prefixed
// object counts only when it carries a well-known key. Trailing content stays
// plain text as it always has.
func TestParsePrefixedObjectGuards(t *testing.T) {
	for _, line := range []string{
		`failed to parse config {"a":1,"b":2}`,   // no well-known key
		`2026/09/27 09:59:43 {"msg":"hi"} extra`, // trailing content
		`2026/09/27 09:59:43 {"msg":"cut off"`,   // truncated
		`2026/09/27 09:59:43 [1,2,3]`,            // not an object
		`connection to db=primary failed`,        // prose with an = in it
	} {
		if e := logfmt.Parse(line); e.Structured {
			t.Errorf("Parse(%q) reported structured", line)
		} else if e.Message != line {
			t.Errorf("Parse(%q).Message = %q, want the line itself", line, e.Message)
		}
	}
}

// klog/glog: every Kubernetes component and many Go binaries. The leading
// letter is the level, which is why these lines are worth reading.
func TestParseKlog(t *testing.T) {
	for _, tc := range []struct {
		line    string
		level   logfmt.Level
		text    string
		time    string
		message string
		source  string
	}{
		{`I0927 09:59:43.123456       1 server.go:42] starting up`, logfmt.LevelInfo, "info", "0927 09:59:43.123456", "starting up", "server.go:42"},
		{`W0927 09:59:43.123456       1 cache.go:7] cache miss`, logfmt.LevelWarn, "warning", "0927 09:59:43.123456", "cache miss", "cache.go:7"},
		{`E0927 09:59:43.123456      17 db.go:113] connect refused`, logfmt.LevelError, "error", "0927 09:59:43.123456", "connect refused", "db.go:113"},
		{`F0927 09:59:43.123456       1 main.go:9] out of memory`, logfmt.LevelError, "fatal", "0927 09:59:43.123456", "out of memory", "main.go:9"},
		// Without the microseconds, which klog omits in some builds.
		{`I0927 09:59:43       1 server.go:42] up`, logfmt.LevelInfo, "info", "0927 09:59:43", "up", "server.go:42"},
	} {
		e := logfmt.Parse(tc.line)
		if !e.Structured {
			t.Errorf("Parse(%q) not structured", tc.line)
			continue
		}
		if e.Level != tc.level || e.LevelText != tc.text {
			t.Errorf("Parse(%q) level = %q/%q, want %q/%q", tc.line, e.Level, e.LevelText, tc.level, tc.text)
		}
		if e.Time != tc.time {
			t.Errorf("Parse(%q) Time = %q, want %q", tc.line, e.Time, tc.time)
		}
		if e.Message != tc.message {
			t.Errorf("Parse(%q) Message = %q, want %q", tc.line, e.Message, tc.message)
		}
		want := []logfmt.Field{{Key: "source", Value: tc.source}}
		if !equal(e.Fields, want) {
			t.Errorf("Parse(%q) Fields = %v, want %v", tc.line, e.Fields, want)
		}
	}
}

// A klog line whose message is itself JSON reads as the JSON record: it is
// the more specific of the two, and the klog part is kept as the prefix.
func TestParseKlogWrappingObject(t *testing.T) {
	e := logfmt.Parse(`I0927 09:59:43.123456       1 server.go:42] {"level":"warn","msg":"slow"}`)
	if !e.Structured || e.Message != "slow" || e.LevelText != "warn" {
		t.Fatalf("entry = %+v, want the inner record", e)
	}
}

func TestParseKlogGuards(t *testing.T) {
	for _, line := range []string{
		`X0927 09:59:43.123456 1 server.go:42] bad letter`,
		`I09 09:59:43 1 server.go:42] short date`,
		`I0927 09:59:43.123456 1 server.go:42 no bracket`,
		`Incoming request from 10.0.0.1`, // starts with I, and nothing else fits
	} {
		if e := logfmt.Parse(line); e.Structured {
			t.Errorf("Parse(%q) reported structured", line)
		}
	}
}

// logfmt: logrus' text formatter, Go kit, Heroku's router. Accepted only when
// the whole line is key=value pairs and one of them is a level or a message,
// so prose with an "=" in it stays prose.
func TestParseLogfmt(t *testing.T) {
	e := logfmt.Parse(`level=info msg="request served" method=GET path=/ status=200`)
	if !e.Structured {
		t.Fatal("logfmt line not parsed")
	}
	if e.Level != logfmt.LevelInfo || e.LevelText != "info" {
		t.Errorf("level = %q/%q, want info/info", e.Level, e.LevelText)
	}
	if e.Message != "request served" {
		t.Errorf("Message = %q, want the unquoted value", e.Message)
	}
	want := []logfmt.Field{
		{Key: "method", Value: "GET"},
		{Key: "path", Value: "/"},
		{Key: "status", Value: "200"},
	}
	if !equal(e.Fields, want) {
		t.Errorf("Fields = %v, want %v", e.Fields, want)
	}
}

func TestParseLogfmtForms(t *testing.T) {
	for _, tc := range []struct {
		name, line, message, time string
	}{
		{"logrus", `time="2026-09-27T09:59:43Z" level=warning msg="disk filling"`, "disk filling", "2026-09-27T09:59:43Z"},
		{"escaped quote", `level=error msg="say \"hi\"" code=5`, `say "hi"`, ""},
		{"empty value", `level=info msg=done note=`, "done", ""},
		{"message only", `msg=starting`, "starting", ""},
		{"level only", `level=debug component=cache`, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := logfmt.Parse(tc.line)
			if !e.Structured {
				t.Fatalf("Parse(%q) not structured", tc.line)
			}
			if e.Message != tc.message {
				t.Errorf("Message = %q, want %q", e.Message, tc.message)
			}
			if e.Time != tc.time {
				t.Errorf("Time = %q, want %q", e.Time, tc.time)
			}
		})
	}
}

func TestParseLogfmtGuards(t *testing.T) {
	for _, line := range []string{
		`connection to db=primary failed`, // bare words around a pair
		`method=GET path=/ status=200`,    // pairs, but no level or msg
		`GET /healthz 200`,                // no pairs at all
		`msg="unterminated`,               // broken quoting
		`=novalue msg=hi`,                 // no key
		`level=info msg="a" trailing`,     // a bare word at the end
	} {
		if e := logfmt.Parse(line); e.Structured {
			t.Errorf("Parse(%q) reported structured", line)
		} else if e.Message != line {
			t.Errorf("Parse(%q).Message = %q, want the line itself", line, e.Message)
		}
	}
}

// The golden table in testdata is read by this test and by
// apps/shared/src/logs/logs.golden.test.ts, so the two parsers are held to one set of
// answers rather than to two hand-mirrored test tables. A change that moves
// one parser and not the other fails here or there; regenerate the file
// deliberately when the contract is meant to change.
func TestGoldenCases(t *testing.T) {
	f, err := os.Open("testdata/cases.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var want struct {
		Line       string      `json:"line"`
		Structured bool        `json:"structured"`
		Level      string      `json:"level"`
		LevelText  string      `json:"levelText"`
		Message    string      `json:"message"`
		Time       string      `json:"time"`
		Fields     [][2]string `json:"fields"`
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		if err := json.Unmarshal(sc.Bytes(), &want); err != nil {
			t.Fatalf("case %d: %v", n+1, err)
		}
		n++
		e := logfmt.Parse(want.Line)
		got := [][2]string{}
		for _, f := range e.Fields {
			got = append(got, [2]string{f.Key, f.Value})
		}
		if e.Structured != want.Structured || string(e.Level) != want.Level ||
			e.LevelText != want.LevelText || e.Message != want.Message ||
			e.Time != want.Time || !reflect.DeepEqual(got, want.Fields) {
			t.Errorf("Parse(%q) =\n  structured=%v level=%q levelText=%q message=%q time=%q fields=%v\nwant\n  structured=%v level=%q levelText=%q message=%q time=%q fields=%v",
				want.Line, e.Structured, e.Level, e.LevelText, e.Message, e.Time, got,
				want.Structured, want.Level, want.LevelText, want.Message, want.Time, want.Fields)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no golden cases read; the assertions above proved nothing")
	}
	t.Logf("%d golden cases", n)
}
