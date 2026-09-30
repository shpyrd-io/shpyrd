"use client";

import * as React from "react";
import { cn } from "cn";
import { Popover as PopoverPrimitive } from "radix-ui";

const widths = {
  auto: "w-auto",
  small: "w-64",
  medium: "w-80",
  large: "w-120",
  xlarge: "w-160",
  xxlarge: "w-240",
} as const;

const heights = {
  auto: "h-auto",
  xsmall: "h-48",
  small: "h-64",
  medium: "h-80",
  large: "h-108",
  xlarge: "h-150",
} as const;

// An anchor that opens a floating box beside itself. It opens with a
// click or the keyboard; it closes with Escape, a click outside or the
// anchor again. It never grows past the edge of the window.
function AnchoredOverlay({
  anchor,
  children,
  className,
  side = "bottom",
  align = "start",
  sideOffset = 4,
  alignOffset = 0,
  width = "auto",
  height = "auto",
  open,
  defaultOpen,
  onOpenChange,
  ...props
}: React.ComponentProps<typeof PopoverPrimitive.Content> &
  Pick<
    React.ComponentProps<typeof PopoverPrimitive.Root>,
    "open" | "defaultOpen" | "onOpenChange"
  > & {
    // What opens the overlay, and what it is placed beside: a button.
    anchor: React.ReactElement;
    width?: keyof typeof widths;
    height?: keyof typeof heights;
  }) {
  return (
    <PopoverPrimitive.Root
      data-slot="anchored-overlay"
      open={open}
      defaultOpen={defaultOpen}
      onOpenChange={onOpenChange}
    >
      <PopoverPrimitive.Trigger data-slot="anchored-overlay-anchor" asChild>
        {anchor}
      </PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          data-slot="anchored-overlay-content"
          side={side}
          align={align}
          sideOffset={sideOffset}
          alignOffset={alignOffset}
          className={cn(
            "z-50 max-h-(--radix-popover-content-available-height) max-w-(--radix-popover-content-available-width) origin-(--radix-popover-content-transform-origin) overflow-auto rounded-xl bg-popover p-4 text-sm text-popover-foreground shadow-lg ring-1 ring-foreground/10 duration-fast outline-none data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-95",
            widths[width],
            heights[height],
            className,
          )}
          {...props}
        >
          {children}
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  );
}

// Something inside the overlay that closes it: a button.
function AnchoredOverlayClose({
  ...props
}: React.ComponentProps<typeof PopoverPrimitive.Close>) {
  return (
    <PopoverPrimitive.Close data-slot="anchored-overlay-close" {...props} />
  );
}

export { AnchoredOverlay, AnchoredOverlayClose };
