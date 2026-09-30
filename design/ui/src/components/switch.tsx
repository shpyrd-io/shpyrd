"use client";

import * as React from "react";
import { cn } from "cn";
import { Loader2 } from "lucide-react";
import { Switch as SwitchPrimitive } from "radix-ui";

// A setting that is on or off, and takes effect at once. In a form that
// is sent later, a checkbox is the thing. Beside the knob a word says
// how it is; it is for the eye only, the switch itself says its state.

const sizes = {
  default: {
    track: "h-6 w-11",
    thumb: "size-5 data-[state=checked]:translate-x-5",
    label: "text-sm",
  },
  sm: {
    track: "h-5 w-9",
    thumb: "size-4 data-[state=checked]:translate-x-4",
    label: "text-xs",
  },
} as const;

function Switch({
  className,
  size = "default",
  loading = false,
  statusLabel = true,
  statusLabelPosition = "start",
  checked,
  defaultChecked = false,
  onCheckedChange,
  disabled,
  ...props
}: React.ComponentProps<typeof SwitchPrimitive.Root> & {
  size?: keyof typeof sizes;
  // What was asked is on its way: the switch waits, and says so.
  loading?: boolean;
  // The word beside the knob: `On` or `Off`.
  statusLabel?: boolean;
  statusLabelPosition?: "start" | "end";
}) {
  // Without `checked` the switch keeps its own state, for the word.
  const [own, setOwn] = React.useState(defaultChecked);
  const on = checked ?? own;
  return (
    <span
      data-slot="switch"
      data-state={on ? "checked" : "unchecked"}
      className={cn(
        "inline-flex items-center gap-2",
        statusLabelPosition === "end" && "flex-row-reverse",
        className,
      )}
    >
      {statusLabel && (
        <span
          aria-hidden
          data-slot="switch-status"
          className={cn("flex items-center gap-1 font-medium select-none", sizes[size].label, (disabled || loading) && "opacity-50")}
        >
          {loading && <Loader2 className="size-3.5 animate-spin" />}
          {on ? "On" : "Off"}
        </span>
      )}
      <SwitchPrimitive.Root
        data-slot="switch-track"
        checked={on}
        disabled={disabled || loading}
        aria-busy={loading || undefined}
        onCheckedChange={(next) => {
          setOwn(next);
          onCheckedChange?.(next);
        }}
        className={cn(
          "peer inline-flex shrink-0 items-center rounded-full border-2 border-transparent transition-colors outline-none focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:bg-primary data-[state=unchecked]:bg-input dark:data-[state=unchecked]:bg-input/80",
          sizes[size].track,
        )}
        {...props}
      >
        <SwitchPrimitive.Thumb
          data-slot="switch-thumb"
          className={cn(
            "pointer-events-none block rounded-full bg-background shadow-sm transition-transform data-[state=unchecked]:translate-x-0",
            sizes[size].thumb,
          )}
        />
      </SwitchPrimitive.Root>
    </span>
  );
}

export { Switch };
