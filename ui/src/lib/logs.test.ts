import { describe, expect, it } from "vitest";
import { atLeast, lineMatches, parseLogLine } from "@/lib/logs";

describe("parseLogLine on plain lines", () => {
  it("keeps the line as the message", () => {
    const l = parseLogLine("Listening on :8080");
    expect(l.structured).toBe(false);
    expect(l.message).toBe("Listening on :8080");
    expect(l.fields).toEqual([]);
  });

  it("reads the level from the words in the line", () => {
    expect(parseLogLine("Listening on :8080").level).toBe("info");
    expect(parseLogLine("WARN disk almost full").level).toBe("warn");
    expect(parseLogLine("panic: runtime error").level).toBe("error");
  });

  it("leaves the level label empty when the line does not name one", () => {
    expect(parseLogLine("Listening on :8080").levelText).toBe("");
  });
});

describe("parseLogLine on JSON objects", () => {
  it("pulls level, message and time out of the object", () => {
    const l = parseLogLine(
      '{"level":"info","msg":"request served","time":"2026-09-25T10:00:00Z"}',
    );
    expect(l.structured).toBe(true);
    expect(l.level).toBe("info");
    expect(l.levelText).toBe("info");
    expect(l.message).toBe("request served");
    expect(l.time).toBe("2026-09-25T10:00:00Z");
    expect(l.fields).toEqual([]);
  });

  it("keeps the remaining fields in the order the line wrote them", () => {
    const l = parseLogLine(
      '{"msg":"hi","zebra":1,"alpha":"two","level":"warn"}',
    );
    expect(l.fields).toEqual([
      { key: "zebra", value: "1" },
      { key: "alpha", value: "two" },
    ]);
  });

  it("puts the error field first, whatever its position", () => {
    const l = parseLogLine('{"msg":"boom","a":"1","err":"connection reset"}');
    expect(l.fields).toEqual([
      { key: "error", value: "connection reset" },
      { key: "a", value: "1" },
    ]);
  });

  it("renders nested values as compact JSON for the collapsed row", () => {
    const l = parseLogLine(
      '{"msg":"hi","req":{"path":"/","n":2},"tags":["a","b"],"ok":true,"none":null}',
    );
    expect(l.fields.map((f) => [f.key, f.value])).toEqual([
      ["req", '{"path":"/","n":2}'],
      ["tags", '["a","b"]'],
      ["ok", "true"],
      ["none", "null"],
    ]);
  });
});

describe("parseLogLine key aliases", () => {
  it("accepts the common spellings of each well-known key", () => {
    const l = parseLogLine(
      '{"severity":"ERROR","message":"nope","@timestamp":"2026-09-25T10:00:00Z","error":"eof"}',
    );
    expect(l.levelText).toBe("ERROR");
    expect(l.level).toBe("error");
    expect(l.message).toBe("nope");
    expect(l.time).toBe("2026-09-25T10:00:00Z");
    expect(l.fields).toEqual([{ key: "error", value: "eof" }]);
  });

  it("accepts lvl, event and ts", () => {
    const l = parseLogLine('{"lvl":"debug","event":"tick","ts":1758790000}');
    expect(l.level).toBe("debug");
    expect(l.message).toBe("tick");
    expect(l.time).toBe("1758790000");
  });

  it("folds level names onto severity buckets", () => {
    expect(parseLogLine('{"level":"fatal","msg":"x"}').level).toBe("error");
    expect(parseLogLine('{"level":"WARNING","msg":"x"}').level).toBe("warn");
    expect(parseLogLine('{"level":"trace","msg":"x"}').level).toBe("debug");
    expect(parseLogLine('{"level":"notice","msg":"x"}').level).toBe("info");
  });

  it("guesses the level from the message when the object names none", () => {
    expect(parseLogLine('{"msg":"failed to connect"}').level).toBe("error");
  });
});

describe("parseLogLine on lines that only look structured", () => {
  it("treats a JSON array as plain text", () => {
    const l = parseLogLine("[1,2,3]");
    expect(l.structured).toBe(false);
    expect(l.message).toBe("[1,2,3]");
  });

  it("treats an unterminated object as plain text", () => {
    const l = parseLogLine('{"msg":"cut off"');
    expect(l.structured).toBe(false);
    expect(l.message).toBe('{"msg":"cut off"');
  });

  it("treats a bare JSON string as plain text", () => {
    expect(parseLogLine('"hello"').structured).toBe(false);
  });

  it("parses an object that is indented", () => {
    expect(parseLogLine('   {"msg":"hi"}  ').structured).toBe(true);
  });

  it("has an empty message when the object carries none", () => {
    const l = parseLogLine('{"level":"info","user":"ana"}');
    expect(l.message).toBe("");
    expect(l.fields).toEqual([{ key: "user", value: "ana" }]);
  });

  it("leaves ANSI escapes in the message for the renderer to strip", () => {
    const esc = String.fromCharCode(27);
    const l = parseLogLine(
      JSON.stringify({ msg: esc + "[31mred" + esc + "[0m" }),
    );
    expect(l.message).toBe(esc + "[31mred" + esc + "[0m");
  });
});

describe("atLeast", () => {
  it("keeps the level asked for and everything more severe", () => {
    expect(atLeast("error", "warn")).toBe(true);
    expect(atLeast("warn", "warn")).toBe(true);
    expect(atLeast("info", "warn")).toBe(false);
    expect(atLeast("debug", "info")).toBe(false);
  });
});

describe("lineMatches", () => {
  const line = parseLogLine('{"msg":"request served","path":"/checkout"}');

  it("matches the message", () => {
    expect(lineMatches(line, "web.1", "served")).toBe(true);
  });

  it("matches the instance", () => {
    expect(lineMatches(line, "web.1", "web.")).toBe(true);
  });

  it("matches a field value", () => {
    expect(lineMatches(line, "web.1", "checkout")).toBe(true);
  });

  it("matches a field key", () => {
    expect(lineMatches(line, "web.1", "path")).toBe(true);
  });

  it("ignores case and surrounding blanks", () => {
    expect(lineMatches(line, "web.1", "  CHECKOUT ")).toBe(true);
  });

  it("does not match text that is nowhere in the line", () => {
    expect(lineMatches(line, "web.1", "worker")).toBe(false);
  });

  it("matches everything when the filter is empty", () => {
    expect(lineMatches(line, "web.1", "   ")).toBe(true);
  });
});

// Mirrors TestParseDuplicateWellKnownKeys in pkg/logfmt.
describe("parseLogLine with two spellings of one well-known key", () => {
  it("consumes both, keeps the first non-empty value", () => {
    const l = parseLogLine(
      '{"msg":"a","message":"b","level":"","severity":"warn","keep":"1"}',
    );
    expect(l.message).toBe("a");
    expect(l.levelText).toBe("warn");
    expect(l.level).toBe("warn");
    expect(l.fields).toEqual([{ key: "keep", value: "1" }]);
  });
});

// Mirrors TestParseNumericLevels in pkg/logfmt.
describe("parseLogLine with a numeric level", () => {
  it("reads pino's scale as a bucket, keeping the digits as written", () => {
    for (const [level, want] of [
      [10, "debug"],
      [30, "info"],
      [40, "warn"],
      [50, "error"],
      [60, "error"],
    ] as const) {
      const l = parseLogLine(`{"level":${level},"msg":"x"}`);
      expect([l.level, l.levelText]).toEqual([want, String(level)]);
    }
  });

  it("names the syslog severities by their bucket", () => {
    for (const [level, want] of [
      [3, "error"],
      [4, "warn"],
      [6, "info"],
      [7, "debug"],
    ] as const) {
      expect(parseLogLine(`{"level":${level},"msg":"x"}`).level).toBe(want);
    }
  });
});

// Mirrors TestParseNumericLevels / TestPrettyNormalizesTheLevelLabel.
describe("parseLogLine keeps the spelling and the bucket apart", () => {
  it("keeps what the line wrote in levelText", () => {
    expect(parseLogLine('{"level":"warning","msg":"x"}').levelText).toBe(
      "warning",
    );
    expect(parseLogLine('{"level":50,"msg":"x"}').levelText).toBe("50");
  });
});

describe("parseLogLine keeps nested values as data", () => {
  it("carries the parsed object alongside the one-line preview", () => {
    const l = parseLogLine('{"msg":"hi","job":{"id":"j-12","queue":"mail"}}');
    const job = l.fields[0];
    expect(job.key).toBe("job");
    expect(job.value).toBe('{"id":"j-12","queue":"mail"}');
    expect(job.json).toEqual({ id: "j-12", queue: "mail" });
  });

  it("carries arrays too", () => {
    const l = parseLogLine('{"msg":"hi","tags":["a","b"]}');
    expect(l.fields[0].json).toEqual(["a", "b"]);
  });

  it("leaves primitives without a parsed value", () => {
    const l = parseLogLine('{"msg":"hi","n":2,"s":"text","b":true}');
    expect(l.fields.map((f) => f.json)).toEqual([
      undefined,
      undefined,
      undefined,
    ]);
  });

  it("treats an empty object or array as a leaf", () => {
    const l = parseLogLine('{"msg":"hi","empty":{},"none":[]}');
    expect(l.fields.map((f) => f.json)).toEqual([undefined, undefined]);
    expect(l.fields.map((f) => f.value)).toEqual(["{}", "[]"]);
  });

  it("keeps the error field's object when it has one", () => {
    const l = parseLogLine('{"msg":"boom","err":{"code":500,"why":"eof"}}');
    expect(l.fields[0].key).toBe("error");
    expect(l.fields[0].json).toEqual({ code: 500, why: "eof" });
  });
});

// The same case table as pkg/logfmt's, so the two parsers cannot drift.
describe("parseLogLine on a prefix before the object", () => {
  const line =
    '2026/09/27 09:59:43 {"level":"info","msg":"request","method":"GET","path":"/","status":200,"duration_ms":0,"remote":"10.0.1.204:37058"}';

  it("reads the record Go's standard log package wrapped", () => {
    const l = parseLogLine(line);
    expect(l.structured).toBe(true);
    expect(l.level).toBe("info");
    expect(l.levelText).toBe("info");
    expect(l.message).toBe("request");
    expect(l.time).toBe("2026/09/27 09:59:43");
    expect(l.fields).toEqual([
      { key: "method", value: "GET" },
      { key: "path", value: "/" },
      { key: "status", value: "200" },
      { key: "duration_ms", value: "0" },
      { key: "remote", value: "10.0.1.204:37058" },
    ]);
  });

  it.each([
    ["date and time", '2026/09/27 09:59:43 {"msg":"hi"}', "2026/09/27 09:59:43", undefined],
    ["microseconds", '2026/09/27 09:59:43.123456 {"msg":"hi"}', "2026/09/27 09:59:43.123456", undefined],
    ["time only", '09:59:43 {"msg":"hi"}', "09:59:43", undefined],
    ["date only", '2026/09/27 {"msg":"hi"}', "2026/09/27", undefined],
    ["RFC3339", '2026-09-27T09:59:43Z {"msg":"hi"}', "2026-09-27T09:59:43Z", undefined],
    ["with caller", '2026/09/27 09:59:43 main.go:42: {"msg":"hi"}', "2026/09/27 09:59:43", "main.go:42:"],
    ["no timestamp", 'myapp {"msg":"hi"}', undefined, "myapp"],
  ])("handles a %s prefix", (_name, raw, time, prefix) => {
    const l = parseLogLine(raw as string);
    expect(l.structured).toBe(true);
    expect(l.message).toBe("hi");
    expect(l.time).toBe(time);
    expect(l.fields.find((f) => f.key === "prefix")?.value).toBe(prefix);
  });

  it("keeps the prefix as a field when the record has its own time", () => {
    const l = parseLogLine(
      '2026/09/27 09:59:43 {"time":"2026-09-27T09:59:43.101Z","msg":"hi"}',
    );
    expect(l.time).toBe("2026-09-27T09:59:43.101Z");
    expect(l.fields).toEqual([
      { key: "prefix", value: "2026/09/27 09:59:43" },
    ]);
  });

  it.each([
    ['failed to parse config {"a":1,"b":2}'],
    ['2026/09/27 09:59:43 {"msg":"hi"} extra'],
    ['2026/09/27 09:59:43 {"msg":"cut off"'],
    ["2026/09/27 09:59:43 [1,2,3]"],
    ["connection to db=primary failed"],
  ])("leaves %s as plain text", (raw) => {
    const l = parseLogLine(raw);
    expect(l.structured).toBe(false);
    expect(l.message).toBe(raw);
  });
});

describe("parseLogLine on klog lines", () => {
  it.each([
    ["I0927 09:59:43.123456       1 server.go:42] starting up", "info", "info", "0927 09:59:43.123456", "starting up", "server.go:42"],
    ["W0927 09:59:43.123456       1 cache.go:7] cache miss", "warn", "warning", "0927 09:59:43.123456", "cache miss", "cache.go:7"],
    ["E0927 09:59:43.123456      17 db.go:113] connect refused", "error", "error", "0927 09:59:43.123456", "connect refused", "db.go:113"],
    ["F0927 09:59:43.123456       1 main.go:9] out of memory", "error", "fatal", "0927 09:59:43.123456", "out of memory", "main.go:9"],
    ["I0927 09:59:43       1 server.go:42] up", "info", "info", "0927 09:59:43", "up", "server.go:42"],
  ])("reads %s", (raw, level, levelText, time, message, source) => {
    const l = parseLogLine(raw);
    expect(l.structured).toBe(true);
    expect(l.level).toBe(level);
    expect(l.levelText).toBe(levelText);
    expect(l.time).toBe(time);
    expect(l.message).toBe(message);
    expect(l.fields).toEqual([{ key: "source", value: source }]);
  });

  it("prefers the inner record when a klog line wraps JSON", () => {
    const l = parseLogLine(
      'I0927 09:59:43.123456       1 server.go:42] {"level":"warn","msg":"slow"}',
    );
    expect(l.structured).toBe(true);
    expect(l.levelText).toBe("warn");
    expect(l.message).toBe("slow");
  });

  it.each([
    ["X0927 09:59:43.123456 1 server.go:42] bad letter"],
    ["I09 09:59:43 1 server.go:42] short date"],
    ["I0927 09:59:43.123456 1 server.go:42 no bracket"],
    ["Incoming request from 10.0.0.1"],
  ])("leaves %s as plain text", (raw) => {
    expect(parseLogLine(raw).structured).toBe(false);
  });
});

describe("parseLogLine on logfmt lines", () => {
  it("reads level, message and the remaining pairs", () => {
    const l = parseLogLine(
      'level=info msg="request served" method=GET path=/ status=200',
    );
    expect(l.structured).toBe(true);
    expect(l.level).toBe("info");
    expect(l.levelText).toBe("info");
    expect(l.message).toBe("request served");
    expect(l.fields).toEqual([
      { key: "method", value: "GET" },
      { key: "path", value: "/" },
      { key: "status", value: "200" },
    ]);
  });

  it.each([
    ["logrus", 'time="2026-09-27T09:59:43Z" level=warning msg="disk filling"', "disk filling", "2026-09-27T09:59:43Z"],
    ["escaped quote", 'level=error msg="say \\"hi\\"" code=5', 'say "hi"', undefined],
    ["empty value", "level=info msg=done note=", "done", undefined],
    ["message only", "msg=starting", "starting", undefined],
    ["level only", "level=debug component=cache", "", undefined],
  ])("handles %s", (_name, raw, message, time) => {
    const l = parseLogLine(raw as string);
    expect(l.structured).toBe(true);
    expect(l.message).toBe(message);
    expect(l.time).toBe(time);
  });

  it.each([
    ["connection to db=primary failed"],
    ["method=GET path=/ status=200"],
    ["GET /healthz 200"],
    ['msg="unterminated'],
    ["=novalue msg=hi"],
    ['level=info msg="a" trailing'],
  ])("leaves %s as plain text", (raw) => {
    const l = parseLogLine(raw);
    expect(l.structured).toBe(false);
    expect(l.message).toBe(raw);
  });
});
