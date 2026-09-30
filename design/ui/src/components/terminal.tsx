"use client";

import * as React from "react";
import { cn } from "cn";
import type { FitAddon } from "@xterm/addon-fit";
import type { Terminal as XTerm } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";

// A terminal: what a shell prints, and what is typed into it. It is
// xterm.js in the colours of the theme, light or dark, and it fits the
// room it is given. What the bytes come from and go to is the
// application's: a socket, a made-up shell.

export type TerminalHandle = {
  write: (data: string | Uint8Array) => void;
  writeln: (text: string) => void;
  reset: () => void;
  focus: () => void;
  clear: () => void;
  readonly cols: number;
  readonly rows: number;
};

// The colours, read from the tokens where the terminal is, so that they
// follow the theme.
function themeOf(element: HTMLElement) {
  const style = getComputedStyle(element);
  const token = (name: string) => style.getPropertyValue(name).trim();
  const bg = token("--card") || token("--background");
  const fg = token("--foreground");
  return {
    background: bg,
    foreground: fg,
    cursor: token("--primary"),
    cursorAccent: bg,
    selectionBackground: token("--primary"),
    selectionForeground: token("--primary-foreground"),
    black: fg,
    red: token("--destructive"),
    green: token("--success"),
    yellow: token("--warning"),
    blue: token("--info"),
    magenta: token("--chart-4"),
    cyan: token("--chart-2"),
    white: token("--muted-foreground"),
    brightBlack: token("--muted-foreground"),
    brightRed: token("--destructive"),
    brightGreen: token("--success"),
    brightYellow: token("--warning"),
    brightBlue: token("--info"),
    brightMagenta: token("--chart-4"),
    brightCyan: token("--chart-2"),
    brightWhite: fg,
  };
}

function Terminal({
  className,
  ref,
  onData,
  onResize,
  height = 480,
  ...props
}: Omit<React.ComponentProps<"div">, "onResize" | "ref"> & {
  // The handle, not the element: what to write into, reset, focus.
  ref?: React.Ref<TerminalHandle>;
  // What is typed, as text, to be sent on.
  onData?: (data: string) => void;
  // The size changed: the other end may want to know.
  onResize?: (size: { cols: number; rows: number }) => void;
  height?: number;
}) {
  const host = React.useRef<HTMLDivElement>(null);
  const term = React.useRef<XTerm | null>(null);
  const fit = React.useRef<FitAddon | null>(null);
  const dataHandler = React.useRef(onData);
  const resizeHandler = React.useRef(onResize);
  dataHandler.current = onData;
  resizeHandler.current = onResize;

  React.useImperativeHandle(ref, () => ({
    write: (data) => term.current?.write(data),
    writeln: (text) => term.current?.writeln(text),
    reset: () => term.current?.reset(),
    focus: () => term.current?.focus(),
    clear: () => term.current?.clear(),
    get cols() {
      return term.current?.cols ?? 0;
    },
    get rows() {
      return term.current?.rows ?? 0;
    },
  }));

  // One terminal for the life of the element. xterm reaches for the
  // window as it loads, so it is loaded here, in the browser, never
  // where the page is rendered ahead.
  React.useEffect(() => {
    const element = host.current;
    if (!element) return;
    let gone = false;
    let undo = () => {};
    Promise.all([import("@xterm/xterm"), import("@xterm/addon-fit")]).then(
      ([{ Terminal: XTerm }, { FitAddon }]) => {
        if (gone) return;
        const t = new XTerm({
          cursorBlink: true,
          fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace",
          fontSize: 13,
          lineHeight: 1.2,
          theme: themeOf(element),
        });
        const f = new FitAddon();
        t.loadAddon(f);
        t.open(element);
        f.fit();
        term.current = t;
        fit.current = f;
        const data = t.onData((d) => dataHandler.current?.(d));
        const resize = t.onResize((size) => resizeHandler.current?.(size));

        // Refit when the room changes, not on every pixel of a drag.
        let timer: ReturnType<typeof setTimeout> | null = null;
        const observer = new ResizeObserver(() => {
          if (timer) clearTimeout(timer);
          timer = setTimeout(() => f.fit(), 100);
        });
        observer.observe(element);

        // Recolour when the theme changes: the class of the document, or
        // the system's preference.
        const recolour = () => {
          t.options.theme = themeOf(element);
        };
        const classes = new MutationObserver(recolour);
        classes.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
        const media = window.matchMedia("(prefers-color-scheme: dark)");
        media.addEventListener("change", recolour);

        undo = () => {
          data.dispose();
          resize.dispose();
          observer.disconnect();
          classes.disconnect();
          media.removeEventListener("change", recolour);
          if (timer) clearTimeout(timer);
          t.dispose();
          term.current = null;
          fit.current = null;
        };
      },
    );
    return () => {
      gone = true;
      undo();
    };
  }, []);

  return (
    <div
      ref={host}
      data-slot="terminal"
      // The fit addon measures the inner box: the padding here is the room
      // between the border and the first column.
      className={cn("overflow-hidden rounded-lg border bg-card px-3 py-2.5", className)}
      style={{ height }}
      {...props}
    />
  );
}

export { Terminal };
