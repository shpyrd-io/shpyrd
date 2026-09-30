"use client";

import * as React from "react";
import { cn } from "cn";
import { Check, Pipette } from "lucide-react";
import { AnchoredOverlay } from "./anchored-overlay";
import { Input } from "./input";

// A colour, chosen: a round swatch that opens the choices, and the hex
// beside it, typed or read. The choices are a few colours the
// application offers, and any colour at all through the browser's own
// picker.

export const isHex = (value: string) => /^#[0-9a-f]{6}$/i.test(value.trim());

function Swatch({
  color,
  selected = false,
  label,
  onClick,
  className,
}: {
  color: string;
  selected?: boolean;
  label: string;
  onClick?: () => void;
  className?: string;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      aria-pressed={selected}
      title={label}
      onClick={onClick}
      className={cn(
        "inline-flex size-7 items-center justify-center rounded-full ring-1 ring-foreground/15 ring-inset transition-shadow outline-none hover:ring-2 focus-visible:ring-3 focus-visible:ring-ring/50 aria-pressed:ring-2 aria-pressed:ring-foreground [&_svg]:size-3.5",
        className,
      )}
      style={{ backgroundColor: color }}
    >
      {selected && <Check className="text-primary-foreground drop-shadow-sm" />}
    </button>
  );
}

function ColorPicker({
  className,
  value,
  defaultValue = "",
  onChange,
  presets = [],
  placeholder = "#ff4f00",
  disabled,
  id,
  "aria-invalid": invalid,
  ...props
}: Omit<React.ComponentProps<"div">, "onChange" | "defaultValue"> & {
  // The hex, `#rrggbb`. Empty is no colour.
  value?: string;
  defaultValue?: string;
  onChange?: (value: string) => void;
  // Colours offered first, as hex.
  presets?: string[];
  placeholder?: string;
  disabled?: boolean;
  id?: string;
  "aria-invalid"?: boolean | "true" | "false";
}) {
  const [own, setOwn] = React.useState(defaultValue);
  const current = value ?? own;
  const set = (next: string) => {
    setOwn(next);
    onChange?.(next);
  };
  const valid = current === "" || isHex(current);
  const shown = isHex(current) ? current.trim() : undefined;
  const native = React.useRef<HTMLInputElement>(null);

  return (
    <div
      data-slot="color-picker"
      className={cn("flex items-center gap-2", className)}
      {...props}
    >
      <AnchoredOverlay
        width="auto"
        className="grid gap-3 p-3"
        anchor={
          <button
            type="button"
            aria-label="Choose a colour"
            disabled={disabled}
            className="relative inline-flex size-8 shrink-0 items-center justify-center rounded-full ring-1 ring-foreground/15 ring-inset outline-none focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-50"
            style={{ backgroundColor: shown }}
          >
            {/* Without a colour, the swatch is empty: a line across it says so. */}
            {!shown && (
              <span aria-hidden className="absolute inset-0 rounded-full bg-[linear-gradient(135deg,transparent_46%,var(--destructive)_46%,var(--destructive)_54%,transparent_54%)]" />
            )}
          </button>
        }
      >
        {presets.length > 0 && (
          <div className="grid grid-cols-6 gap-2">
            {presets.map((preset) => (
              <Swatch
                key={preset}
                color={preset}
                label={preset}
                selected={shown?.toLowerCase() === preset.toLowerCase()}
                onClick={() => set(preset)}
              />
            ))}
          </div>
        )}
        <label className="flex cursor-pointer items-center gap-2 text-sm">
          <span className="inline-flex size-7 items-center justify-center rounded-full bg-muted text-muted-foreground ring-1 ring-foreground/15 ring-inset [&_svg]:size-3.5">
            <Pipette />
          </span>
          Any colour
          {/* The browser's own picker, reached through the label. */}
          <input
            ref={native}
            type="color"
            className="sr-only"
            value={shown ?? "#000000"}
            onChange={(event) => set(event.target.value)}
          />
        </label>
      </AnchoredOverlay>
      <Input
        id={id}
        value={current}
        placeholder={placeholder}
        disabled={disabled}
        aria-invalid={invalid ?? (!valid || undefined)}
        spellCheck={false}
        onChange={(event) => set(event.target.value)}
        className="w-32 font-mono"
      />
    </div>
  );
}

export { ColorPicker, Swatch };
