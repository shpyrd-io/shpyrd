"use client";

import * as React from "react";
import { cn } from "cn";
import { Slider as SliderPrimitive } from "radix-ui";

// A number chosen along a line, or two: a value, or the bounds of a
// range, dragged with the pointer or moved a step at a time with the
// arrow keys. The slider is the hand on the line, not the figure: the
// number itself is said beside it, by an input or a word. The knob is
// the switch's, raised off a sunken track, so the two read as one family.

function Slider({
  className,
  value,
  defaultValue,
  min = 0,
  max = 100,
  thumbLabels,
  "aria-label": label,
  "aria-describedby": describedBy,
  ...props
}: Omit<React.ComponentProps<typeof SliderPrimitive.Root>, "aria-label"> & {
  // What each thumb is called, for who cannot see: one name per value,
  // `["At least", "At most"]` for a range.
  thumbLabels?: string[];
  // The name of the one thumb of a single value.
  "aria-label"?: string;
}) {
  // One thumb for each value given, or for each default.
  const count = Math.max(1, (value ?? defaultValue ?? [min]).length);
  return (
    <SliderPrimitive.Root
      data-slot="slider"
      value={value}
      defaultValue={defaultValue}
      min={min}
      max={max}
      className={cn(
        "relative flex w-full touch-none items-center select-none data-[disabled]:opacity-50 data-[orientation=vertical]:h-full data-[orientation=vertical]:min-h-44 data-[orientation=vertical]:w-auto data-[orientation=vertical]:flex-col",
        className,
      )}
      {...props}
    >
      <SliderPrimitive.Track
        data-slot="slider-track"
        className={cn(
          // Sunk into the page, like the switch's track.
          "relative grow overflow-hidden rounded-full bg-input data-[orientation=horizontal]:h-1.5 data-[orientation=horizontal]:w-full data-[orientation=vertical]:h-full data-[orientation=vertical]:w-1.5 dark:bg-input/80",
          "shadow-[inset_0_1px_2px_rgb(0_0_0/0.2)] dark:shadow-[inset_0_1px_2px_rgb(0_0_0/0.55)]",
        )}
      >
        <SliderPrimitive.Range
          data-slot="slider-range"
          className="absolute bg-primary data-[orientation=horizontal]:h-full data-[orientation=vertical]:w-full"
        />
      </SliderPrimitive.Track>
      {Array.from({ length: count }, (_, i) => (
        <SliderPrimitive.Thumb
          key={i}
          data-slot="slider-thumb"
          aria-label={thumbLabels?.[i] ?? (count === 1 ? label : undefined)}
          aria-describedby={describedBy}
          className={cn(
            // Raised off the track: the switch's knob, round.
            "block size-4 shrink-0 rounded-full border border-primary/60 bg-linear-to-b from-white to-[#ececec] transition-[box-shadow] outline-none",
            "shadow-[inset_0_1px_0_rgb(255_255_255),0_1px_2px_rgb(0_0_0/0.25),0_2px_5px_rgb(0_0_0/0.15)]",
            "hover:ring-3 hover:ring-ring/30 focus-visible:ring-3 focus-visible:ring-ring/50 data-[disabled]:pointer-events-none",
          )}
        />
      ))}
    </SliderPrimitive.Root>
  );
}

export { Slider };
