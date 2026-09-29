import * as React from "react";
import { cn } from "cn";
import { Badge } from "./badge";

// The colours of each type: of the box, of the text and of the dot.
const types = {
  success: {
    box: "border-success/30 bg-success/10",
    text: "text-success",
    dot: "bg-success",
  },
  info: {
    box: "border-info/30 bg-info/10",
    text: "text-info",
    dot: "bg-info",
  },
  warning: {
    box: "border-warning/30 bg-warning/10",
    text: "text-warning",
    dot: "bg-warning",
  },
  error: {
    box: "border-destructive/30 bg-destructive/10",
    text: "text-destructive",
    dot: "bg-destructive",
  },
  neutral: {
    box: "border-border bg-muted",
    text: "text-muted-foreground",
    dot: "bg-muted-foreground",
  },
} as const;

// A badge that tells how something is: a dot, a word and, when there is
// one, a quantity in grey. `secondary` is for names: `web 1/1`.
function StatusBadge({
  className,
  type = "neutral",
  variant = "default",
  qty,
  live = false,
  children,
  ...props
}: Omit<React.ComponentProps<typeof Badge>, "variant"> & {
  type?: keyof typeof types;
  variant?: "default" | "secondary";
  qty?: React.ReactNode;
  // The dot pulses: what is told is still going on.
  live?: boolean;
}) {
  const colours = types[type];
  return (
    <Badge
      variant="outline"
      data-slot="status-badge"
      data-type={type}
      className={cn(
        "gap-1.5",
        colours.box,
        variant === "default"
          ? colours.text
          : "rounded-md font-mono font-normal text-foreground",
        className,
      )}
      {...props}
    >
      <span
        aria-hidden
        className={cn(
          "size-1.5 shrink-0 rounded-full",
          variant === "default" ? "bg-current" : colours.dot,
          live && "animate-pulse",
        )}
      />
      {children}
      {qty !== undefined && (
        <span className="text-muted-foreground tabular-nums">{qty}</span>
      )}
    </Badge>
  );
}

export { StatusBadge };
