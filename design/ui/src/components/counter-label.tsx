import * as React from "react";
import { cn } from "cn";

// How many there are of something, in a small pill after its name: the
// processes of a project, the releases in a tab, what is unread.
function CounterLabel({
  className,
  variant = "default",
  ...props
}: React.ComponentProps<"span"> & {
  // `primary` is for what asks to be seen: what is unread, what failed.
  variant?: "default" | "primary";
}) {
  return (
    <span
      data-slot="counter-label"
      data-variant={variant}
      className={cn(
        "inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full px-1.5 text-xs leading-5 font-medium tabular-nums",
        variant === "primary"
          ? "bg-primary text-primary-foreground"
          : "bg-foreground/10 text-muted-foreground",
        className,
      )}
      {...props}
    />
  );
}

export { CounterLabel };
