"use client";

import * as React from "react";
import { cn } from "cn";
import { Check, Loader2 } from "lucide-react";
import { Switch as SwitchPrimitive } from "radix-ui";

// A setting that is on or off, and takes effect at once. In a form that
// is sent later, a checkbox is the thing. Beside the knob a word says
// how it is; it is for the eye only, the switch itself says its state.

const sizes = {
  default: {
    track: "h-6 w-11",
    thumb: "size-5 data-[state=checked]:translate-x-5",
    check: "size-3.5",
    label: "text-sm",
  },
  sm: {
    track: "h-5 w-9",
    thumb: "size-4 data-[state=checked]:translate-x-4",
    check: "size-3",
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
          // Sunk into the page: shaded at the top inside, lit at the foot.
          "peer inline-flex shrink-0 items-center rounded-control p-0.5 transition-[background-color,box-shadow] outline-none focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:bg-primary data-[state=unchecked]:bg-input dark:data-[state=unchecked]:bg-input/80",
          "shadow-[inset_0_1px_3px_rgb(0_0_0/0.22),inset_0_-1px_0_rgb(255_255_255/0.5)] data-[state=checked]:shadow-[inset_0_1px_3px_rgb(0_0_0/0.3),inset_0_-1px_0_rgb(255_255_255/0.2)] dark:shadow-[inset_0_1px_3px_rgb(0_0_0/0.6),inset_0_-1px_0_rgb(255_255_255/0.06)] dark:data-[state=checked]:shadow-[inset_0_1px_3px_rgb(0_0_0/0.35),inset_0_-1px_0_rgb(255_255_255/0.15)]",
          sizes[size].track,
        )}
        {...props}
      >
        <SwitchPrimitive.Thumb
          data-slot="switch-thumb"
          className={cn(
            // Raised off the track: a soft light from above, a shadow beneath.
            "pointer-events-none flex items-center justify-center rounded-[3px] bg-linear-to-b from-white to-[#ececec] text-primary transition-transform data-[state=unchecked]:translate-x-0",
            "shadow-[inset_0_1px_0_rgb(255_255_255),0_1px_2px_rgb(0_0_0/0.25),0_2px_5px_rgb(0_0_0/0.15)]",
            sizes[size].thumb,
          )}
        >
          {/* On, the knob says so with a tick, for who does not tell colours apart. */}
          {on && <Check aria-hidden="true" strokeWidth={3} className={sizes[size].check} />}
        </SwitchPrimitive.Thumb>
      </SwitchPrimitive.Root>
    </span>
  );
}

export { Switch };
