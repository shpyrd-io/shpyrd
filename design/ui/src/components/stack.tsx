import * as React from "react";
import { cn } from "cn";

// Things one after the other, with the same gap between them: down the
// page, or along a line.

type Space = "none" | "tight" | "condensed" | "cozy" | "normal" | "spacious";

const gaps: Record<Space, string> = {
  none: "gap-0",
  tight: "gap-1",
  condensed: "gap-2",
  cozy: "gap-3",
  normal: "gap-4",
  spacious: "gap-6",
};

const paddings: Record<Space, string> = {
  none: "p-0",
  tight: "p-1",
  condensed: "p-2",
  cozy: "p-3",
  normal: "p-4",
  spacious: "p-6",
};

const aligns = {
  stretch: "items-stretch",
  start: "items-start",
  center: "items-center",
  end: "items-end",
  baseline: "items-baseline",
} as const;

const justifies = {
  start: "justify-start",
  center: "justify-center",
  end: "justify-end",
  "space-between": "justify-between",
  "space-evenly": "justify-evenly",
} as const;

function Stack({
  className,
  direction = "vertical",
  gap = "normal",
  align = "stretch",
  justify = "start",
  wrap = "nowrap",
  padding = "none",
  ...props
}: React.ComponentProps<"div"> & {
  direction?: "vertical" | "horizontal";
  gap?: Space;
  // Across the direction: the height of each, along a line.
  align?: keyof typeof aligns;
  // Along the direction: where the room that is left goes.
  justify?: keyof typeof justifies;
  // What does not fit goes to the next line.
  wrap?: "wrap" | "nowrap";
  padding?: Space;
}) {
  return (
    <div
      data-slot="stack"
      data-direction={direction}
      className={cn(
        "flex",
        direction === "vertical" ? "flex-col" : "flex-row",
        wrap === "wrap" ? "flex-wrap" : "flex-nowrap",
        gaps[gap],
        aligns[align],
        justifies[justify],
        paddings[padding],
        className,
      )}
      {...props}
    />
  );
}

// One of the things of a stack, when it has to take the room that is
// left, or to keep its size.
function StackItem({
  className,
  grow = false,
  shrink = true,
  ...props
}: React.ComponentProps<"div"> & {
  grow?: boolean;
  shrink?: boolean;
}) {
  return (
    <div
      data-slot="stack-item"
      className={cn(
        "min-w-0",
        grow ? "grow" : "grow-0",
        shrink ? "shrink" : "shrink-0",
        className,
      )}
      {...props}
    />
  );
}

export { Stack, StackItem };
