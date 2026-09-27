# RFC-0021 Structured logs in the viewer and the CLI

**Status:** implemented

**Owner:** Marcelo Paez Sequeira (merged in shpyrd-io/shpyrd#5)

**Depends on:** none

**Creation date:** 2026-09-22

**Last update:** 2026-09-27

## Summary

Detect JSON log lines and render them readably: level, message and time up front, other
fields collapsed and expandable, filtering by level and field; `shpyrd logs --pretty`
(default on a terminal) does the same in the CLI.

## Motivation

Most frameworks log JSON in production; a wall of `{"level":"info","msg":...}` is hard to
read and the viewer's level highlighting misses it.

### Goals

- JSON lines look like log lines; raw view one click away.
- Level filter works for JSON (`level`, `severity`, `lvl`) and plain lines alike.

### Non-Goals

- Storage or search over time (RFC-0022).

## Proposal

- Parser: a line is structured when it fits one of four shapes, tried most specific first —
  a JSON object that is the whole line; a prefix followed by a JSON object running to the end
  of the line (Go's standard `log` package stamps `2026/09/27 09:59:43 ` in front of whatever
  it is given); klog/glog (`I0927 09:59:43.123456   1 server.go:42] message`); and logfmt
  (`level=info msg="request" method=GET`). Whichever shape matched, well-known keys map to
  level (`level|severity|lvl|log.level`), message (`msg|message|event`), time
  (`time|ts|timestamp|@timestamp`), error (`error|err`).
- The two shapes that could mistake prose for a record each carry a guard: a prefixed object
  counts only when the object holds a well-known key, and a logfmt line only when every token
  is a `key=value` pair and one of them is a level or a message. Anything else stays text,
  which is the safe direction to be wrong in — a plain line rendered as plain is merely
  unhelpful, while prose rendered as a record loses words off the screen.
- A prefix is never discarded: a leading timestamp becomes the line's time when the record
  carries none of its own, and whatever is left (the caller `log.Lshortfile` adds, a
  `log.SetPrefix` string) becomes a `prefix` field.
- Dashboard: structured lines render `LEVEL message` with a chevron revealing the remaining
  fields as key/value; a "raw" toggle per view; the filter box matches field values too.
- CLI: `--pretty` renders `time level message key=value...`; `--json` passes lines through
  untouched; default: pretty when stdout is a terminal.
- Works on the live stream (no dependency on the pipeline); when RFC-0022 lands the same
  renderer applies to history.

## Design Details

- UI: `parseLogLine` in `ui/src/lib/logs.ts` with tests; CLI: `pkg/logfmt`.
- Multi-line JSON is not reassembled (lines are units).
- The two parsers are held to one golden table, `pkg/logfmt/testdata/cases.jsonl`, read by
  `pkg/logfmt`'s `TestGoldenCases` and by `ui/src/lib/logs.golden.test.ts`. Mirroring two
  hand-written test tables kept them in step only as long as someone remembered to mirror;
  one table of answers makes a one-sided change fail in one of the two suites.
- Prefix timestamps are validated by parsing, not by shape: `pkg/logfmt` runs the candidate
  through `time.Parse` against Go's `log` flag layouts and RFC3339, the same way
  `pkg/api`'s `splitTimestamp` validates kubelet's prefix. The browser has no layout parser,
  so it range-checks each field and rejects an unreal calendar date through the `Date`
  constructor — which is what keeps it from accepting `2026/02/30` where Go does not.

## Implementation History

- 2026-09-22: RFC written.
- 2026-09-25: implementation started.
- 2026-09-25: implemented.
  - `pkg/logfmt`: `Parse` reads one line into a level, a message, a timestamp and the
    remaining fields; `Entry.Pretty` renders "LEVEL message key=value...". A line counts as
    structured only when the whole line is a JSON object, so an array, a scalar, a
    truncated object or trailing content stays plain text.
  - `ui/src/lib/logs.ts`: the same contract in the browser, plus `atLeast` for the level
    filter and `lineMatches` for filtering over fields. The plain-line level guess moved
    here out of `log-view.tsx`, so structured and plain lines share one definition.
  - Both parsers consume every spelling of a well-known key (a line writing `msg` and
    `message` shows neither as a field) and keep the remaining fields in the order the line
    wrote them, with the error field first; nested values render as compact JSON, unescaped.
  - A numeric level is read and named by its bucket, so pino's `"level":30` reads as INFO
    rather than 30: tens up to 60 are pino's scale, 0 to 7 the syslog severities.
  - Viewer: a JSON line reads as its level and message with the fields behind a chevron;
    the level select picks a floor ("All levels" through "Errors only") and applies to plain
    lines too; Raw shows each line as the application wrote it. The text filter matches
    field keys and values. Each line is parsed once and kept against the line object in a
    WeakMap, because the stream re-renders on every 100 ms batch.
  - CLI: `shpyrd logs` renders JSON lines when stdout is a terminal, `--pretty` and `--json`
    force either and refuse to be combined. The time and instance columns stay the
    container's, so a line's own time field is not printed twice.
  - Tests: vitest joins the dashboard (`npm run test`, run by `make test` and the Dashboard
    CI job) with 38 tests, including react-dom/server smoke tests of the viewer that need no
    DOM; `pkg/logfmt` carries 18 and the CLI helpers 10, over the same case table, so the two
    parsers cannot drift. Rendering real zap, logrus, pino, bunyan and structlog lines is
    what turned up the numeric levels and the escaped nested values.
  - Verified on kind against `examples/blog` (which logs JSON): rendered on a terminal and
    untouched when piped, `--pretty` and `--json` forcing either, and `--json` byte-identical
    to the container's own lines. Lines written straight into the container's stdout covered
    the rest: a plain line and a `panic:` line pass through, pino's `"level":50` reads as
    ERROR with its `time` not printed twice, logrus's `"warning"` keeps its spelling in the
    warn bucket, a nested object and an array print unescaped, and a truncated object stays
    plain text.
  - Not built, and not promised by this RFC: a `--level` filter for the CLI, and `key=value`
    queries in the filter box (it matches field keys and values as substrings).
- 2026-09-26: the viewer, after reading real output on a cluster.
  - A field holding an object or an array opens too, as deep as the line goes, each level
    indented behind a rule; arrays number their entries and an empty object stays a leaf.
    The parser keeps the parsed value beside the one-line form, so the filter and
    `pkg/logfmt` are untouched.
  - The level column shows the bucket rather than the spelling, because logrus writes
    "warning" and pino a number and both pushed the message out of line; the spelling is on
    the element's title, and `--pretty` does the same.
  - A plain line that reads as an error or a warning is labelled too, dimmed, with a title
    saying it was read from the text. Ordinary output stays unlabelled: an INFO against
    every line of buildpack chatter is noise, and the blank column is what makes a labelled
    line worth looking at.
  - Fixed: the nested chevrons did nothing. The expansion was stored under
    `<line>#<field path>` and read back as the field path alone, so the write and the read
    never met.
  - jsdom and testing-library join the dashboard's tests. Both UI defects so far -- an
    unsized icon and that one -- were invisible to tests that can render but never click.
  - Considered and dropped: ANSI colour in `shpyrd logs`. It was built and reverted; the
    owner did not want it. A large record is read with `--json | jq`, and the terminal
    keeps one line per log line.

- 2026-09-27: three more shapes, after a report that
  `2026/09/27 09:59:43 {"level":"info","msg":"request",...}` rendered as a wall of JSON. The
  cause was the same in both parsers and was a gap in this RFC rather than a coding mistake:
  the Proposal said "starts with `{`", and the note above about trailing content had never
  considered content *before* the object. Go's standard `log` package puts a timestamp there
  whenever an application hands it a marshalled record, which is common enough to be worth
  reading.
  - Prefixed JSON, klog/glog and logfmt are now read, in that order of specificity ahead of
    plain text, with the guards described in the Proposal. A klog line whose message is
    itself JSON reads as the inner record, since it says more about the line than the header
    does, and the header is kept as the `prefix` field.
  - Deliberately still plain text: access logs (nothing structured to extract, and they read
    fine as text) and level-leading formats like `INFO 2026-09-27 ... message`, whose
    spellings vary too widely to detect without guessing. Heroku's router logs parse as
    logfmt but carry `at=info` rather than a level key, so they reach the guard and stay
    text; adding `at` to the level keys would have meant reading it as a level inside JSON
    records too, which is too eager.
  - klog's thread id is dropped rather than kept as a field: it would otherwise put
    `thread=1` on every line. The caller is kept as `source`.
  - Verified by running one line of each shape through `Entry.Pretty`, and by the golden
    table: 45 cases, both parsers byte-identical on all of them, including the calendar edges
    (`2026/02/30`, `2026/13/01`) where the Go and browser timestamp checks are implemented
    differently. The table was also checked to *fail* on a one-sided change, so it is known
    to detect drift rather than assumed to.
