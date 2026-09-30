"use client";

import * as React from "react";
import { cn } from "cn";
import { ChevronDown, ChevronRight } from "lucide-react";
import { inks, order } from "../lib/chart";

// Lines of a log, as they come: when, from which instance, what. A line
// that was read as a record has a level and fields; the fields open
// under it. The reading is the application's: the view only draws.

export type LogLevel = "error" | "warn" | "info" | "debug";

export type LogField = {
  key: string;
  // In one line, as it is shown closed and searched.
  value: string;
  // The value as data when it is worth opening: an object or an array.
  json?: object;
};

export type LogLine = {
  // Already written for the eye: `17:04:12`.
  time?: string;
  instance?: string;
  // The bucket; without one the line is plain text.
  level?: LogLevel;
  // The level as the line spelled it. Empty when it was read from the text.
  levelText?: string;
  message: string;
  fields?: LogField[];
  // The line as the application wrote it, for the raw view.
  raw?: string;
};

const rank: Record<LogLevel, number> = { error: 0, warn: 1, info: 2, debug: 3 };

export function atLeast(level: LogLevel | undefined, min: LogLevel) {
  return rank[level ?? "info"] <= rank[min];
}

export function lineMatches(line: LogLine, filter: string) {
  const needle = filter.trim().toLowerCase();
  if (!needle) return true;
  const haystack = [line.message, line.instance ?? "", ...(line.fields ?? []).flatMap((f) => [f.key, f.value])]
    .join("\n")
    .toLowerCase();
  return haystack.includes(needle);
}

// The colour codes applications and buildpacks write: taken out.
const ansi = new RegExp(String.fromCharCode(27) + "\\[[0-9;]*m", "g");
export const plain = (text: string) => text.replace(ansi, "");

// Each instance keeps one ink, from its name.
function inkOf(name: string) {
  let h = 0;
  for (const c of name) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return inks[order[h % order.length]];
}

const levels: Record<LogLevel, { text: string; row: string; message: string }> = {
  error: { text: "text-destructive", row: "bg-destructive/8", message: "text-destructive" },
  warn: { text: "text-warning", row: "bg-warning/8", message: "" },
  info: { text: "text-muted-foreground", row: "", message: "" },
  debug: { text: "text-muted-foreground/60", row: "", message: "text-muted-foreground" },
};

// A level is shown when the line declared one, or when the text reads as
// a problem: labelling every ordinary line INFO is noise.
function labelled(line: LogLine) {
  return (line.levelText ?? "") !== "" || line.level === "error" || line.level === "warn";
}

function Toggle({ open, onClick, label }: { open: boolean; onClick: () => void; label: string }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-expanded={open}
      aria-label={(open ? "Hide " : "Show ") + label}
      className="mt-1 size-3 shrink-0 self-start rounded-xs text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 [&_svg]:size-3"
    >
      {open ? <ChevronDown /> : <ChevronRight />}
    </button>
  );
}

// The rows of an object or an array, as fields.
function entries(value: object): LogField[] {
  const pairs = Array.isArray(value) ? value.map((v, i) => ["[" + i + "]", v] as const) : Object.entries(value);
  return pairs.map(([key, v]) => {
    const json = v !== null && typeof v === "object" && Object.keys(v).length > 0 ? (v as object) : undefined;
    const value = typeof v === "string" ? v : (JSON.stringify(v) ?? "");
    return json ? { key, value, json } : { key, value };
  });
}

// The key and value rows under an open line, and under every object
// opened within it.
function Fields({
  fields,
  path,
  open,
  onToggle,
  nested = false,
}: {
  fields: LogField[];
  path: string;
  open: Set<string>;
  onToggle: (path: string) => void;
  nested?: boolean;
}) {
  return (
    <dl
      className={cn(
        "grid grid-cols-[auto_1fr] gap-x-3 text-muted-foreground",
        nested && "mt-0.5 border-l pl-2",
      )}
    >
      {fields.map((field) => {
        const at = path === "" ? field.key : path + "." + field.key;
        const expanded = open.has(at);
        return (
          <div key={field.key} className="col-span-2 grid grid-cols-subgrid">
            <dt className="flex gap-1">
              {field.json ? (
                <Toggle open={expanded} onClick={() => onToggle(at)} label={field.key} />
              ) : (
                <span className="size-3 shrink-0" aria-hidden="true" />
              )}
              {field.key}
            </dt>
            <dd className="break-all whitespace-pre-wrap text-foreground">
              {field.json && expanded ? (
                <Fields fields={entries(field.json)} path={at} open={open} onToggle={onToggle} nested />
              ) : (
                plain(field.value)
              )}
            </dd>
          </div>
        );
      })}
    </dl>
  );
}

function LogView({
  className,
  lines,
  follow = true,
  filter = "",
  level = "debug",
  raw = false,
  empty = "Waiting for lines…",
  height = 480,
  ...props
}: React.ComponentProps<"div"> & {
  lines: LogLine[];
  // The view stays at the end as lines come.
  follow?: boolean;
  // Words a line has to have, in its message, its instance or its fields.
  filter?: string;
  // The lowest level shown; `debug` shows everything.
  level?: LogLevel;
  // Each line as the application wrote it, without level or fields.
  raw?: boolean;
  empty?: React.ReactNode;
  height?: number;
}) {
  const ref = React.useRef<HTMLDivElement>(null);
  const [open, setOpen] = React.useState<Set<string>>(new Set());
  const visible = React.useMemo(
    () => lines.filter((line) => atLeast(line.level, level) && lineMatches(line, filter)),
    [lines, filter, level],
  );
  React.useEffect(() => {
    if (follow && ref.current) ref.current.scrollTop = ref.current.scrollHeight;
  }, [visible, follow]);
  const toggle = (key: string) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (!next.delete(key)) next.add(key);
      return next;
    });

  return (
    <div
      ref={ref}
      data-slot="log-view"
      className={cn(
        "overflow-auto rounded-lg border bg-card p-2 font-mono text-xs leading-5 text-foreground",
        className,
      )}
      style={{ height }}
      {...props}
    >
      {visible.length === 0 ? (
        <div className="p-2 text-muted-foreground">{lines.length === 0 ? empty : "No line matches the filter"}</div>
      ) : (
        visible.map((line, i) => {
          const key = `${line.time ?? ""}|${line.instance ?? ""}|${i}`;
          const fields = raw ? [] : (line.fields ?? []);
          const opened = open.has(key);
          const tone = levels[line.level ?? "info"];
          return (
            <div
              key={key}
              data-level={line.level}
              className={cn(
                "grid grid-cols-[4.5rem_6.5rem_1fr] gap-2 rounded-sm px-1 hover:bg-muted",
                !raw && tone.row,
              )}
            >
              <span className="text-muted-foreground select-none">{line.time}</span>
              <span className={cn("truncate text-(--ink)", inkOf(line.instance ?? ""))} title={line.instance}>
                {line.instance}
              </span>
              <div className="min-w-0">
                <div className="flex gap-1.5">
                  {fields.length > 0 ? (
                    <Toggle open={opened} onClick={() => toggle(key)} label="fields" />
                  ) : (
                    <span className="size-3 shrink-0" aria-hidden="true" />
                  )}
                  {!raw && labelled(line) && line.level && (
                    // One width for the column whatever the application
                    // spelled; the spelling is in the title. A level read
                    // off the text is dimmed: nothing declared it.
                    <span
                      title={line.levelText || "read from the text of the line"}
                      className={cn("shrink-0 uppercase", tone.text, !line.levelText && "opacity-60")}
                    >
                      {line.level}
                    </span>
                  )}
                  <span className={cn("min-w-0 flex-1 break-all whitespace-pre-wrap", !raw && tone.message)}>
                    {plain(raw ? (line.raw ?? line.message) : line.message)}
                  </span>
                </div>
                {opened && fields.length > 0 && (
                  <div className="mb-1 ml-4.5">
                    <Fields fields={fields} path={key} open={open} onToggle={toggle} />
                  </div>
                )}
              </div>
            </div>
          );
        })
      )}
    </div>
  );
}

// The output of a build, line by line: the steps stand out, the errors
// are red.
function TextLogView({
  className,
  lines,
  follow = true,
  empty = "No output",
  height = 480,
  ...props
}: React.ComponentProps<"pre"> & {
  lines: string[];
  follow?: boolean;
  empty?: React.ReactNode;
  height?: number;
}) {
  const ref = React.useRef<HTMLPreElement>(null);
  React.useEffect(() => {
    if (follow && ref.current) ref.current.scrollTop = ref.current.scrollHeight;
  }, [lines, follow]);
  return (
    <pre
      ref={ref}
      data-slot="text-log-view"
      className={cn(
        "overflow-auto rounded-lg border bg-card p-3 font-mono text-xs leading-5 whitespace-pre-wrap text-foreground",
        className,
      )}
      style={{ height }}
      {...props}
    >
      {lines.length === 0 ? (
        <span className="text-muted-foreground">{empty}</span>
      ) : (
        lines.map((line, i) => {
          const clean = plain(line);
          const step = clean.startsWith("===> ");
          return (
            <div
              key={i}
              className={cn(
                step && "mt-2 font-semibold text-primary first:mt-0",
                !step && /error|failed/i.test(clean) && "text-destructive",
              )}
            >
              {clean}
            </div>
          );
        })
      )}
    </pre>
  );
}

export { LogView, TextLogView };
