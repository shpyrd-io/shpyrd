import * as React from "react";
import { cn } from "cn";

// A short piece of code in the middle of a text: a command, the name of
// a file, a key and its value. It is as big as the text around it.
function InlineCode({
  className,
  wrap = true,
  ...props
}: React.ComponentProps<"code"> & {
  // It may go to the next line. Without it, it stays together: for what
  // is short, as `/mcp`.
  wrap?: boolean;
}) {
  return (
    <code
      data-slot="inline-code"
      className={cn(
        "rounded-sm bg-muted px-[0.3em] py-[0.1em] font-mono text-[0.875em] text-foreground",
        wrap ? "wrap-anywhere" : "whitespace-nowrap",
        className,
      )}
      {...props}
    />
  );
}

export { InlineCode };
