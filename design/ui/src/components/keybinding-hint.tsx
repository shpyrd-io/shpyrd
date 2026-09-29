"use client";

import * as React from "react";
import { cn } from "cn";

// Other names a key goes by.
const aliases: Record<string, string> = {
  cmd: "command",
  meta: "command",
  ctrl: "control",
  option: "alt",
  return: "enter",
  esc: "escape",
  del: "delete",
  up: "arrowup",
  down: "arrowdown",
  left: "arrowleft",
  right: "arrowright",
};

// How a key is drawn and how it is called: elsewhere, then on a Mac when
// it differs there.
const table: Record<string, [string, string, string?, string?]> = {
  command: ["Win", "Windows", "⌘", "Command"],
  control: ["Ctrl", "Control", "⌃"],
  alt: ["Alt", "Alt", "⌥", "Option"],
  shift: ["⇧", "Shift"],
  enter: ["⏎", "Enter"],
  escape: ["Esc", "Escape"],
  tab: ["⇥", "Tab"],
  backspace: ["⌫", "Backspace"],
  delete: ["Del", "Delete", "⌦"],
  space: ["␣", "Space"],
  plus: ["+", "Plus"],
  arrowup: ["↑", "Up"],
  arrowdown: ["↓", "Down"],
  arrowleft: ["←", "Left"],
  arrowright: ["→", "Right"],
  pageup: ["PgUp", "Page Up"],
  pagedown: ["PgDn", "Page Down"],
  home: ["Home", "Home"],
  end: ["End", "End"],
};

// The order the modifiers are written in, which is not the same everywhere.
const order = {
  mac: ["control", "alt", "shift", "command"],
  other: ["control", "command", "alt", "shift"],
};

function read(keys: string, mac: boolean) {
  const modifiers = mac ? order.mac : order.other;
  const rank = (key: string) => {
    const at = modifiers.indexOf(key);
    return at < 0 ? modifiers.length : at;
  };
  return keys
    .trim()
    .split(/\s+/)
    .map((chord) =>
      chord
        .split("+")
        .filter(Boolean)
        .map((written) => {
          const name = written.toLowerCase();
          if (name === "mod") return { key: mac ? "command" : "control", written };
          return { key: aliases[name] ?? name, written };
        })
        .sort((a, b) => rank(a.key) - rank(b.key))
        .map(({ key, written }) => {
          const known = table[key];
          if (!known) {
            // A key it does not know is written as it was given: `F5`.
            const text = written.length === 1 ? written.toUpperCase() : written;
            return { glyph: text, name: text };
          }
          const [glyph, name, macGlyph, macName] = known;
          return {
            glyph: (mac && macGlyph) || glyph,
            name: (mac && macName) || name,
          };
        }),
    );
}

const never = () => () => {};

// A page may be rendered when the application is built, where there is no
// browser: there the keys are those of a keyboard that is not a Mac's.
function useMac() {
  return React.useSyncExternalStore(
    never,
    () => /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent),
    () => false,
  );
}

// Tells that a keyboard shortcut exists. It only shows the keys: it does
// not listen to them. `keys` joins what is pressed together with `+` and
// separates what is pressed in turn with a space; `Mod` is Command on a
// Mac and Control elsewhere.
function KeybindingHint({
  keys,
  format = "condensed",
  variant = "default",
  size = "default",
  className,
  ...props
}: Omit<React.ComponentProps<"kbd">, "children"> & {
  keys: string;
  // `full` writes the names of the keys, for running text.
  format?: "condensed" | "full";
  // `onEmphasis` is for a filled surface: a primary button, a tooltip.
  variant?: "default" | "onEmphasis";
  size?: "default" | "sm";
}) {
  const mac = useMac();
  return (
    <kbd
      data-slot="kbd"
      data-variant={variant}
      data-size={size}
      className={cn(
        "inline-flex items-center gap-1 font-sans text-xs font-medium whitespace-nowrap data-[size=sm]:text-[0.625rem]",
        className,
      )}
      {...props}
    >
      {read(keys, mac).map((chord, at) => (
        <React.Fragment key={at}>
          {at > 0 && <span className="sr-only">then</span>}
          <span
            data-slot="kbd-chord"
            className={cn(
              "inline-flex h-5 min-w-5 items-center justify-center gap-0.5 rounded-sm border px-1",
              size === "sm" && "h-4 min-w-4 px-0.5",
              variant === "default"
                ? "border-border bg-muted text-muted-foreground"
                : "border-current/25 bg-current/10",
            )}
          >
            {chord.map((key, index) =>
              format === "full" ? (
                <React.Fragment key={index}>
                  {index > 0 && <span aria-hidden>+</span>}
                  <span>{key.name}</span>
                </React.Fragment>
              ) : (
                <React.Fragment key={index}>
                  <span className="sr-only">{key.name}</span>
                  <span aria-hidden>{key.glyph}</span>
                </React.Fragment>
              ),
            )}
          </span>
        </React.Fragment>
      ))}
    </kbd>
  );
}

export { KeybindingHint };
