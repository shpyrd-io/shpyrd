import * as React from "react";
import { cn } from "cn";
import { Slot } from "radix-ui";

// The colours of the badge of each type: those of the status badge.
const types = {
  neutral: "bg-muted text-muted-foreground",
  primary: "bg-primary text-primary-foreground",
  success: "bg-success text-success-foreground",
  info: "bg-info text-info-foreground",
  warning: "bg-warning text-warning-foreground",
  error: "bg-destructive text-destructive-foreground",
} as const;

// What happened, in the order it happened, down a line that ties one
// thing to the next.
function Timeline({
  className,
  clip = false,
  ...props
}: React.ComponentProps<"ol"> & {
  // The line does not go over the first item, under the last, or both.
  clip?: boolean | "start" | "end" | "both";
}) {
  return (
    <ol
      data-slot="timeline"
      data-clip={clip === true ? "both" : clip || undefined}
      className={cn("flex flex-col", className)}
      {...props}
    />
  );
}

function TimelineItem({
  className,
  children,
  icon,
  type = "neutral",
  condensed = false,
  actions,
  ...props
}: React.ComponentProps<"li"> & {
  // What is in the badge, on the line: an icon.
  icon?: React.ReactElement;
  type?: keyof typeof types;
  // Less room over and under, and the badge is only its icon.
  condensed?: boolean;
  // What can be done with the item, at the end of it.
  actions?: React.ReactNode;
}) {
  return (
    <li
      data-slot="timeline-item"
      data-condensed={condensed}
      className={cn(
        "relative ml-4 flex gap-3 py-4 text-sm before:absolute before:inset-y-0 before:left-0 before:w-0.5 before:bg-border data-[condensed=true]:py-2",
        "in-data-[clip=both]:first:before:top-4 in-data-[clip=start]:first:before:top-4",
        "in-data-[clip=both]:last:before:bottom-[calc(100%-3rem)] in-data-[clip=end]:last:before:bottom-[calc(100%-3rem)]",
        "data-[condensed=true]:in-data-[clip=both]:last:before:bottom-[calc(100%-2rem)] data-[condensed=true]:in-data-[clip=end]:last:before:bottom-[calc(100%-2rem)]",
        className,
      )}
      {...props}
    >
      <span
        data-slot="timeline-badge"
        data-type={type}
        className={cn(
          "relative z-10 flex shrink-0 items-center justify-center rounded-full [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
          condensed
            ? "my-2 -ml-[7px] size-4 bg-background text-muted-foreground"
            : cn("-ml-[15px] size-8 ring-2 ring-background", types[type]),
        )}
      >
        {icon && <Slot.Root aria-hidden={true}>{icon}</Slot.Root>}
      </span>
      <div
        data-slot="timeline-body"
        className="mt-1.5 min-w-0 flex-auto text-muted-foreground [&_a]:font-medium [&_a]:text-foreground [&_a]:hover:underline [&_strong]:font-medium [&_strong]:text-foreground"
      >
        {children}
      </div>
      {actions && (
        <div data-slot="timeline-actions" className="flex shrink-0 items-start gap-2">
          {actions}
        </div>
      )}
    </li>
  );
}

// A gap in the line, between what belongs together and what comes after.
// It is only drawn: it says nothing to who cannot see it.
function TimelineBreak({ className, ...props }: React.ComponentProps<"li">) {
  return (
    <li
      data-slot="timeline-break"
      role="presentation"
      aria-hidden={true}
      className={cn(
        "relative z-10 h-6 border-t-2 border-border bg-background",
        className,
      )}
      {...props}
    />
  );
}

export { Timeline, TimelineBreak, TimelineItem };
