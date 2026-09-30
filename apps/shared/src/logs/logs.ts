/** Log line parsing shared by the log viewer and the level filter.
 *
 * Mirrors pkg/logfmt on the server side: both turn one raw line into a
 * level, a message and the remaining fields, so the dashboard and
 * `shpyrd logs --pretty` agree on what a line says. Keep the two in step.
 *
 * Four shapes are recognised, tried in this order because each is more
 * specific than the next:
 *
 *  1. a JSON object that is the whole line;
 *  2. a prefix then a JSON object running to the end of the line — what Go's
 *     standard log package writes, stamping "2026/09/27 09:59:43 " in front
 *     of whatever it is given;
 *  3. klog/glog: "I0927 09:59:43.123456   1 server.go:42] message";
 *  4. logfmt: `level=info msg="request" method=GET`.
 *
 * Shapes 2 and 4 are the two that could mistake prose for a record, so each
 * carries a guard: a prefixed object counts only when the object holds a
 * well-known key, and a logfmt line only when every token is a key=value pair
 * and one of them is a level or a message. Anything else stays text, which is
 * the safe direction to be wrong in — a plain line rendered as plain is merely
 * unhelpful, while prose rendered as a record loses words.
 */

/** Severity buckets used for colouring and filtering. */
export type LogLevel = "error" | "warn" | "info" | "debug";

export type LogField = {
  key: string;
  /** One-line form, shown collapsed and searched by the filter. */
  value: string;
  /** The parsed value when it is a non-empty object or array, so the
   * viewer can open it instead of showing a wall of JSON. */
  json?: object;
};

export type ParsedLine = {
  /** True when the line was read as a record — any of the shapes listed at
   * the top of this file — rather than kept as text. */
  structured: boolean;
  /** Severity bucket: from the level field, or guessed from the text. */
  level: LogLevel;
  /** The level as the line spelled it ("" when the line names none). */
  levelText: string;
  message: string;
  /** The line's own timestamp, when it carried one. */
  time?: string;
  /** Everything that was not a well-known key, in source order. */
  fields: LogField[];
};

// Well-known keys, in the spellings the common loggers use.
const levelKeys = ["level", "severity", "lvl", "log.level"];
const messageKeys = ["msg", "message", "event"];
const timeKeys = ["time", "ts", "timestamp", "@timestamp"];
const errorKeys = ["error", "err"];

/** Guesses a level from a line that does not carry one. */
function guessLevel(text: string): LogLevel {
  const m = text.slice(0, 200).toLowerCase();
  if (/\b(error|err|fatal|panic|exception|traceback|failed)\b/.test(m))
    return "error";
  if (/\b(warn|warning)\b/.test(m)) return "warn";
  return "info";
}

/** Renders a JSON value as the single line a field shows. */
function fieldValue(v: unknown): string {
  if (typeof v === "string") return v;
  return JSON.stringify(v) ?? "";
}

/** The value as data when it is worth opening: a non-empty object or
 * array. Anything else reads fine on one line. */
function fieldJSON(v: unknown): object | undefined {
  if (v === null || typeof v !== "object") return undefined;
  return Object.keys(v).length > 0 ? (v as object) : undefined;
}

function field(key: string, v: unknown): LogField {
  const json = fieldJSON(v);
  return json
    ? { key, value: fieldValue(v), json }
    : { key, value: fieldValue(v) };
}

/** A record's pairs in source order, as the well-known-key mapping wants
 * them. JSON values arrive as data so nested objects stay openable; the text
 * formats produce strings. */
type Pair = [key: string, value: unknown];

/** Decodes a whole line as a JSON object, or null. */
function decodeObject(body: string): Pair[] | null {
  if (!body.startsWith("{")) return null;
  try {
    // The leading brace already rules out arrays and scalars; the typeof
    // check is what narrows the parsed value for TypeScript.
    const v: unknown = JSON.parse(body);
    if (v === null || typeof v !== "object" || Array.isArray(v)) return null;
    return Object.entries(v as Record<string, unknown>);
  } catch {
    return null;
  }
}

/** True when a record names a level, message, time or error — the evidence
 * that it is a log record rather than incidental data. */
function hasWellKnownKey(pairs: Pair[]): boolean {
  const known = new Set([...levelKeys, ...messageKeys, ...timeKeys, ...errorKeys]);
  return pairs.some(([k]) => known.has(k));
}

/** Reads a line whose JSON object is preceded by something else, which is
 * what Go's standard log package writes. The object has to run to the end of
 * the line and carry a well-known key: `failed to parse config {"a":1}` is a
 * sentence that happens to end in JSON, and belongs on screen as itself. */
function decodePrefixedObject(body: string): { prefix: string; pairs: Pair[] } | null {
  const i = body.indexOf("{");
  if (i <= 0) return null;
  const pairs = decodeObject(body.slice(i));
  if (!pairs || !hasWellKnownKey(pairs)) return null;
  return { prefix: body.slice(0, i).trim(), pairs };
}

/** True when y/m/d name a real calendar date, which is what keeps this in
 * step with Go's time.Parse rejecting "2026/02/30" rather than rolling it
 * over the way the Date constructor would. */
function realDate(y: number, m: number, d: number): boolean {
  const t = new Date(y, m - 1, d);
  return t.getFullYear() === y && t.getMonth() === m - 1 && t.getDate() === d;
}

function validTime(h: number, min: number, sec: number): boolean {
  return h < 24 && min < 60 && sec < 60;
}

/** The stamps a prefix may open with, matching pkg/logfmt's layout list: Go's
 * log flags in their combinations, then RFC3339 for wrappers that stamp it.
 * Each is range-checked rather than merely shaped, because the cost of being
 * wrong is a word of the line vanishing into a timestamp column. */
function timeStamp(candidate: string): boolean {
  let m = /^(\d{4})\/(\d{2})\/(\d{2}) (\d{2}):(\d{2}):(\d{2})(\.\d{6})?$/.exec(candidate);
  if (m)
    return (
      realDate(+m[1], +m[2], +m[3]) && validTime(+m[4], +m[5], +m[6])
    );
  m = /^(\d{4})\/(\d{2})\/(\d{2})$/.exec(candidate);
  if (m) return realDate(+m[1], +m[2], +m[3]);
  m = /^(\d{2}):(\d{2}):(\d{2})(\.\d{6})?$/.exec(candidate);
  if (m) return validTime(+m[1], +m[2], +m[3]);
  m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(\.\d+)?(Z|[+-]\d{2}:\d{2})$/.exec(
    candidate,
  );
  if (m)
    return realDate(+m[1], +m[2], +m[3]) && validTime(+m[4], +m[5], +m[6]);
  return false;
}

/** Separates a leading timestamp from the rest of a prefix, so
 * "2026/09/27 09:59:43 main.go:42:" yields the stamp and the caller
 * log.Lshortfile added. A prefix opening with no timestamp comes back whole
 * as the remainder. */
function splitLogPrefix(prefix: string): { stamp: string; rest: string } {
  const words = prefix.trim().split(/\s+/).filter(Boolean);
  // A stamp is one or two words: a date, a time, or a date and a time.
  for (let n = Math.min(2, words.length); n >= 1; n--) {
    const candidate = words.slice(0, n).join(" ");
    if (timeStamp(candidate))
      return { stamp: candidate, rest: words.slice(n).join(" ") };
  }
  return { stamp: "", rest: prefix.trim() };
}

/** klog/glog's header: a severity letter, the month and day with no year, the
 * time, the thread id, and the caller before a bracket. */
const klogLine =
  /^([IWEF])(\d{4} \d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+\d+ ([^\]\s]+)\] (.*)$/;

/** The header's letter is an abbreviation of exactly these words, so
 * levelText carries the word: a bare "W" would fold to info through
 * normalizeLevel's default, turning every klog warning into an info line. */
const klogLevels: Record<string, string> = {
  I: "info",
  W: "warning",
  E: "error",
  F: "fatal",
};

/** Reads a klog line. One whose message is itself JSON never reaches here:
 * decodePrefixedObject runs first and takes it, keeping the klog header as the
 * prefix, because the inner record says more about the line than the header. */
function decodeKlog(body: string): Pair[] | null {
  const m = klogLine.exec(body);
  if (!m) return null;
  return [
    ["level", klogLevels[m[1]]],
    ["time", m[2]],
    ["msg", m[4]],
    // The caller is worth keeping; the thread id the header also carries is
    // not, and would otherwise put "thread=1" on every line.
    ["source", m[3]],
  ];
}

/** Deliberately narrow: the wider the key alphabet, the more prose a stray
 * "=" can drag in. */
function isKeyChar(c: string): boolean {
  return /[A-Za-z0-9_.@-]/.test(c);
}

/** Reads a double-quoted value from the front of s. */
function scanQuoted(s: string): { value: string; length: number } | null {
  for (let i = 1; i < s.length; i++) {
    if (s[i] === "\\") {
      i++; // an escaped character cannot close the string
      continue;
    }
    if (s[i] === '"') {
      try {
        const v: unknown = JSON.parse(s.slice(0, i + 1));
        if (typeof v !== "string") return null;
        return { value: v, length: i + 1 };
      } catch {
        return null;
      }
    }
  }
  return null;
}

/** Reads a line of key=value pairs, as logrus' text formatter, Go kit and
 * Heroku's router emit. Every token must be a pair and one of them must be a
 * level or a message; "connection to db=primary failed" is prose with an "="
 * in it, and stays prose. */
function decodeLogfmt(body: string): Pair[] | null {
  if (body === "") return null;
  const pairs: Pair[] = [];
  const index = new Map<string, number>();
  let i = 0;
  while (i < body.length) {
    while (i < body.length && /\s/.test(body[i])) i++;
    if (i >= body.length) break;
    const start = i;
    while (i < body.length && isKeyChar(body[i])) i++;
    // A token that is not "key=" at all means this is not a logfmt line.
    if (i === start || i >= body.length || body[i] !== "=") return null;
    const key = body.slice(start, i);
    i++; // the "="
    let value: string;
    if (body[i] === '"') {
      const q = scanQuoted(body.slice(i));
      if (!q) return null;
      value = q.value;
      i += q.length;
    } else {
      const from = i;
      while (i < body.length && !/\s/.test(body[i])) i++;
      value = body.slice(from, i);
    }
    // A quoted value has to end the token, so `msg="a"b` is not logfmt.
    if (i < body.length && !/\s/.test(body[i])) return null;
    const at = index.get(key);
    if (at !== undefined) {
      pairs[at] = [key, value];
      continue;
    }
    index.set(key, pairs.length);
    pairs.push([key, value]);
  }
  if (pairs.length === 0) return null;
  // The guard: without a level or a message this is data, not a log line.
  const named = new Set([...levelKeys, ...messageKeys]);
  return pairs.some(([k]) => named.has(k)) ? pairs : null;
}

/** The first value of the aliases that is set, over ordered pairs. */
function firstOf(pairs: Pair[], keys: string[]): unknown {
  for (const k of keys) {
    for (const [pk, pv] of pairs) {
      if (pk === k && pv !== undefined && pv !== null && pv !== "") return pv;
    }
  }
  return undefined;
}

/** Maps a record's pairs onto a ParsedLine: the well-known keys become the
 * level, message, time and error, the rest stay in the order the line wrote
 * them. prefix is whatever stood before the record, and is never dropped: its
 * timestamp becomes the line's time when the record carries none of its own,
 * and anything left over becomes a "prefix" field. */
function lineFrom(pairs: Pair[], prefix: string): ParsedLine {
  const levelText = fieldValue(firstOf(pairs, levelKeys) ?? "");
  const message = fieldValue(firstOf(pairs, messageKeys) ?? "");
  let time = fieldValue(firstOf(pairs, timeKeys) ?? "");
  const errRaw = firstOf(pairs, errorKeys);

  const known = new Set([
    ...levelKeys,
    ...messageKeys,
    ...timeKeys,
    ...errorKeys,
  ]);
  const fields: LogField[] = [];
  // The error reads as part of the message, so it leads the fields.
  if (errRaw !== undefined) fields.push(field("error", errRaw));
  // Pairs come in the order the application wrote them.
  for (const [k, v] of pairs) {
    if (known.has(k)) continue;
    fields.push(field(k, v));
  }
  if (prefix !== "") {
    const { stamp, rest } = splitLogPrefix(prefix);
    let remainder = rest;
    // The record's own time is the application's and wins; the prefix's then
    // has nowhere to go but a field, which beats losing it.
    if (stamp !== "" && time === "") time = stamp;
    else if (stamp !== "") remainder = prefix.trim();
    if (remainder !== "") fields.push(field("prefix", remainder));
  }

  // levelText keeps the spelling the line used; the bucket in `level` is
  // what gets displayed, so "warning" and pino's 40 read the same width.
  const level =
    levelText === "" ? guessLevel(message) : normalizeLevel(levelText);

  return {
    structured: true,
    level,
    levelText,
    message,
    time: time || undefined,
    fields,
  };
}

export function parseLogLine(raw: string): ParsedLine {
  const body = raw.trim();
  const whole = decodeObject(body);
  if (whole) return lineFrom(whole, "");
  const prefixed = decodePrefixedObject(body);
  if (prefixed) return lineFrom(prefixed.pairs, prefixed.prefix);
  const klog = decodeKlog(body);
  if (klog) return lineFrom(klog, "");
  const logfmt = decodeLogfmt(body);
  if (logfmt) return lineFrom(logfmt, "");
  return {
    structured: false,
    level: guessLevel(raw),
    levelText: "",
    message: raw,
    fields: [],
  };
}

/** Reads pino's scale (10 trace to 60 fatal) and, below 10, the syslog
 * severities (0 emerg to 7 debug). */
function numericLevel(n: number): LogLevel {
  if (n < 10) {
    if (n <= 3) return "error";
    if (n === 4) return "warn";
    return n <= 6 ? "info" : "debug";
  }
  if (n >= 50) return "error";
  if (n >= 40) return "warn";
  return n >= 30 ? "info" : "debug";
}

/** Folds a logger's level into a severity bucket, by name or by number:
 * pino counts in tens up to 60, the syslog severities count down from 0,
 * and both are common enough to read. */
export function normalizeLevel(text: string): LogLevel {
  const t = text.trim();
  if (/^-?\d+$/.test(t)) return numericLevel(Number(t));
  switch (t.toLowerCase()) {
    case "error":
    case "err":
    case "fatal":
    case "crit":
    case "critical":
    case "panic":
    case "alert":
    case "emerg":
    case "emergency":
      return "error";
    case "warn":
    case "warning":
      return "warn";
    case "debug":
    case "trace":
      return "debug";
    default:
      return "info";
  }
}

const rank: Record<LogLevel, number> = { debug: 0, info: 1, warn: 2, error: 3 };

/** True when level is min or more severe; how the level filter selects. */
export function atLeast(level: LogLevel, min: LogLevel): boolean {
  return rank[level] >= rank[min];
}

/** True when the filter text appears anywhere in the line, fields included. */
export function lineMatches(
  line: ParsedLine,
  instance: string,
  filter: string,
): boolean {
  const f = filter.trim().toLowerCase();
  if (!f) return true;
  if (line.message.toLowerCase().includes(f)) return true;
  if (instance.toLowerCase().includes(f)) return true;
  return line.fields.some(
    (x) => x.key.toLowerCase().includes(f) || x.value.toLowerCase().includes(f),
  );
}
