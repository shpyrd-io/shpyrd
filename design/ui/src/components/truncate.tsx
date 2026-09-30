import * as React from "react";
import { cn } from "cn";

// Text that does not fit is cut, with an ellipsis where it was cut. The
// whole text is kept in `title`, for who points at it.
function Truncate({
  as: Comp = "div",
  className,
  maxWidth = 125,
  inline = false,
  expandable = false,
  title,
  style,
  children,
  ...props
}: React.ComponentProps<"div"> & {
  as?: "div" | "span";
  // How wide the text may be: pixels, or what CSS takes (`10ch`, `100%`).
  maxWidth?: number | string;
  // Along the line of the text around it, not on a line of its own.
  inline?: boolean;
  // The whole text shows while the pointer is over it.
  expandable?: boolean;
}) {
  return (
    <Comp
      data-slot="truncate"
      title={title ?? (typeof children === "string" ? children : undefined)}
      className={cn(
        "max-w-(--truncate-max) truncate",
        inline ? "inline-block align-top" : "block",
        expandable && "hover:max-w-none",
        className,
      )}
      style={
        {
          "--truncate-max":
            typeof maxWidth === "number" ? `${maxWidth}px` : maxWidth,
          ...style,
        } as React.CSSProperties
      }
      {...props}
    >
      {children}
    </Comp>
  );
}

export { Truncate };
