"use client";

import * as React from "react";
import { cn } from "../lib/cn";

// A number chosen along a line, the number on its knob (Giovani, 2026-10-28): the track drawn as a
// ProgressBar draws its bar, the part chosen in the brand's orange and the
// rest only outlined, the whole width; the knob is a tile in the primary
// button's orange (Giovani, 2026-10-28: square, as the deck's tiles), and
// carries the number. The browser's range input does the work under it,
// unseen, so the mouse, a finger and the keys all move it.
function KnobSlider({
  className,
  value,
  onValueChange,
  min = 0,
  max = 100,
  step = 1,
  format = (v) => v.toLocaleString("en"),
  ...props
}: Omit<React.ComponentProps<"input">, "value" | "onChange" | "type" | "min" | "max" | "step"> & {
  value: number;
  onValueChange: (value: number) => void;
  min?: number;
  max?: number;
  step?: number;
  // How the number on the knob is written.
  format?: (value: number) => string;
}) {
  const share = max > min ? Math.min(1, Math.max(0, (value - min) / (max - min))) : 0;
  return (
    <div data-slot="knob-slider" className={cn("group/slider relative h-10", className)}>
      {/* The track: the part chosen, then what is left, as ProgressBar. */}
      <span aria-hidden="true" className="absolute inset-x-0 top-1/2 flex h-2 -translate-y-1/2 gap-0.5">
        <span className="min-w-1 rounded-[3px] border border-primary bg-primary/25" style={{ width: `calc(1.25rem + (100% - 2.5rem) * ${share})` }} />
        <span className="min-w-1 flex-1 rounded-[3px] border border-border" />
      </span>
      {/* The knob, with the number. */}
      <span
        aria-hidden="true"
        className="pointer-events-none absolute top-0 grid size-10 place-items-center rounded-lg bg-primary text-sm font-semibold text-primary-foreground tabular-nums shadow-[0_4px_12px_-4px_rgb(255_79_0/0.5)] transition-[box-shadow,scale] duration-fast ease-move group-hover/slider:shadow-[0_6px_16px_-4px_rgb(255_79_0/0.65)] group-has-[:focus-visible]/slider:ring-[3px] group-has-[:focus-visible]/slider:ring-ring/50 group-has-[:active]/slider:scale-105 dark:shadow-[0_4px_14px_-4px_rgb(255_79_0/0.6)]"
        style={{ left: `calc((100% - 2.5rem) * ${share})` }}
      >
        {format(value)}
      </span>
      <input
        data-slot="knob-slider-input"
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        onChange={(e) => onValueChange(Number(e.target.value))}
        // Its own knob as wide as the one drawn, so they move together.
        className="absolute inset-0 size-full cursor-pointer appearance-none opacity-0 [&::-moz-range-thumb]:size-10 [&::-webkit-slider-thumb]:size-10 [&::-webkit-slider-thumb]:appearance-none"
        {...props}
      />
    </div>
  );
}

export { KnobSlider };
