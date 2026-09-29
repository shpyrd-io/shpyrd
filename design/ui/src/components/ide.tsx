"use client";

import * as React from "react";
import { cn } from "cn";
import { Check, Copy, FileCode } from "lucide-react";
import { languageOf, lines, type Token } from "../lib/code";
import { Button } from "./button";

export type IDEFile = {
  name: string;
  code: string;
  // The language of the code. Without it, it is read from the name.
  language?: string;
};

// How each part of the code is written. Two inks, as the charts: the
// text colour and the orange, and grey for what is said aside.
const kinds: Record<Token["kind"], string> = {
  plain: "",
  comment: "text-muted-foreground italic",
  string: "text-primary",
  number: "text-primary",
  keyword: "font-semibold",
};

// Code to be read, with the numbers of the lines and a button that
// copies it. With `files` each file is in its tab, under its name; with
// `code` there is only the code, and no names over it.
function IDE({
  className,
  files,
  code,
  language,
  tabs,
  defaultFile = 0,
  showLineNumbers = true,
  height,
  ...props
}: React.ComponentProps<"figure"> & {
  files?: IDEFile[];
  // Code by itself, in place of `files`: it has no name to show.
  code?: string;
  // The language of `code`: `js`, `sh`, `yaml`, `go`, `sql`.
  language?: string;
  // The names of the files, over the code. Without them only the first
  // file is shown. They are there with `files`, and not with `code`.
  tabs?: boolean;
  // The file shown first, by its place in `files`.
  defaultFile?: number;
  showLineNumbers?: boolean;
  // The most it may be tall, in pixels. Over that, the code scrolls.
  height?: number;
}) {
  const id = React.useId();
  const [open, setOpen] = React.useState(defaultFile);
  const [copied, setCopied] = React.useState(false);
  const all: IDEFile[] = React.useMemo(
    () => files ?? (code === undefined ? [] : [{ name: "", code, language: language ?? "txt" }]),
    [files, code, language],
  );
  const named = tabs ?? files !== undefined;
  const file = all[Math.min(open, all.length - 1)];
  // The end of the last line is not one more line.
  const read = React.useMemo(
    () =>
      file
        ? lines(file.code.replace(/\n$/, ""), file.language ?? languageOf(file.name))
        : [],
    [file],
  );

  if (!file) return null;

  function copy() {
    navigator.clipboard?.writeText(file.code).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }

  function key(event: React.KeyboardEvent<HTMLDivElement>) {
    const move = { ArrowRight: 1, ArrowLeft: -1 }[event.key];
    if (!move) return;
    event.preventDefault();
    const next = (open + move + all.length) % all.length;
    setOpen(next);
    document.getElementById(`${id}-tab-${next}`)?.focus();
  }

  const button = (
    <Button
      variant="ghost"
      size="icon-xs"
      aria-label={copied ? "Copied" : file.name ? `Copy ${file.name}` : "Copy the code"}
      title={copied ? "Copied" : "Copy"}
      icon={copied ? <Check /> : <Copy />}
      onClick={copy}
    />
  );

  const lined = (
    <pre className="w-max min-w-full py-3 font-mono text-[13px] leading-6">
      <code className="grid">
        {read.map((line, i) => (
          <span key={i} className="flex px-4">
            {showLineNumbers && (
              <span
                aria-hidden="true"
                className="mr-4 shrink-0 text-right text-muted-foreground/70 select-none tabular-nums"
                style={{ width: `${String(read.length).length}ch` }}
              >
                {i + 1}
              </span>
            )}
            <span>
              {line.length === 0
                ? "\n"
                : line.map((token, j) => (
                    <span key={j} className={kinds[token.kind]}>
                      {token.text}
                    </span>
                  ))}
            </span>
          </span>
        ))}
      </code>
    </pre>
  );

  return (
    <figure
      data-slot="ide"
      data-tabs={named}
      className={cn(
        "relative overflow-hidden rounded-xl bg-card text-sm text-card-foreground ring-1 ring-foreground/10",
        className,
      )}
      {...props}
    >
      {named ? (
        <>
          <div className="flex items-center gap-2 border-b bg-muted/50 pr-2">
            <div
              role="tablist"
              aria-label="Files"
              className="flex min-w-0 flex-1 overflow-x-auto"
              onKeyDown={key}
            >
              {all.map((f, i) => (
                <button
                  key={f.name}
                  type="button"
                  role="tab"
                  id={`${id}-tab-${i}`}
                  aria-selected={i === open}
                  aria-controls={`${id}-panel`}
                  tabIndex={i === open ? 0 : -1}
                  onClick={() => setOpen(i)}
                  className="relative flex shrink-0 items-center gap-1.5 border-r px-3 py-2 font-mono text-xs text-muted-foreground outline-none hover:text-foreground focus-visible:bg-muted aria-selected:bg-card aria-selected:text-foreground aria-selected:after:absolute aria-selected:after:inset-x-0 aria-selected:after:top-0 aria-selected:after:h-0.5 aria-selected:after:bg-primary [&_svg]:size-3.5"
                >
                  <FileCode aria-hidden="true" />
                  {f.name}
                </button>
              ))}
            </div>
            {button}
          </div>
          <div
            role="tabpanel"
            id={`${id}-panel`}
            aria-labelledby={`${id}-tab-${open}`}
            tabIndex={0}
            className="overflow-auto outline-none focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:ring-inset"
            style={{ maxHeight: height }}
          >
            {lined}
          </div>
        </>
      ) : (
        <>
          {/* Without the names there is nothing over the code: the button
              is in its corner, over a piece of the surface so it can be
              read when a line goes under it. */}
          <div className="absolute top-2 right-2 z-10 rounded-md bg-card">{button}</div>
          <div
            role="region"
            aria-label="Code"
            tabIndex={0}
            className="overflow-auto outline-none focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:ring-inset"
            style={{ maxHeight: height }}
          >
            {lined}
          </div>
        </>
      )}
    </figure>
  );
}

export { IDE };
