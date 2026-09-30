import * as React from "react";
import { Lock } from "lucide-react";
import { cn } from "cn";

// A screen as someone would see it in their browser: the window, its address,
// and what is at that address. It is a picture of a page, drawn with the page's
// own parts, so it follows the theme and never goes out of date like a
// screenshot does.
//
// Its parts are props rather than children it looks for, so a page rendered
// when the application is built draws the same thing as the browser does.
function BrowserFrame({
  className,
  address,
  secure = true,
  children,
  ...props
}: React.ComponentProps<"figure"> & {
  // What is in the address bar, without the scheme.
  address: React.ReactNode;
  // The padlock before the address.
  secure?: boolean;
}) {
  return (
    <figure
      data-slot="browser-frame"
      className={cn("overflow-hidden rounded-xl border bg-background shadow-sm", className)}
      {...props}
    >
      <div
        data-slot="browser-frame-bar"
        className="flex items-center gap-3 border-b bg-muted px-3 py-2"
      >
        <span aria-hidden={true} className="flex gap-1.5">
          <span className="size-2.5 rounded-full bg-border" />
          <span className="size-2.5 rounded-full bg-border" />
          <span className="size-2.5 rounded-full bg-border" />
        </span>
        <span
          data-slot="browser-frame-address"
          className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md bg-background px-2.5 py-1 font-mono text-xs text-muted-foreground"
        >
          {secure && <Lock aria-label="Secure" className="size-3 shrink-0" />}
          <span className="truncate">{address}</span>
        </span>
      </div>
      <div data-slot="browser-frame-content">{children}</div>
    </figure>
  );
}

export { BrowserFrame };
