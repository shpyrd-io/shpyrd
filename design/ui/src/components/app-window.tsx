import * as React from "react";
import { cn } from "cn";

// A desktop application as it sits on a screen: the window's title bar, with
// its three buttons and the app's name, and what the app shows. Like the
// browser frame, it is a picture of an app drawn with the page's own parts, so
// it follows the theme and does not go stale like a screenshot.
//
// Its parts are props rather than children it looks for, so a page rendered
// when the application is built draws the same thing as the browser does.
function AppWindow({
  className,
  title,
  detail,
  footer,
  children,
  ...props
}: Omit<React.ComponentProps<"figure">, "title"> & {
  // The app's name in the title bar, with its icon if it has one.
  title: React.ReactNode;
  // After the name, quieter: the open document, the folder, the account.
  detail?: React.ReactNode;
  // What stays at the bottom of the window: a box to type in, a status line.
  footer?: React.ReactNode;
}) {
  return (
    <figure
      data-slot="app-window"
      className={cn(
        "flex flex-col overflow-hidden rounded-xl border bg-background shadow-md",
        className,
      )}
      {...props}
    >
      <div
        data-slot="app-window-bar"
        className="grid shrink-0 grid-cols-[auto_1fr_auto] items-center gap-3 border-b bg-muted px-3 py-2"
      >
        <span aria-hidden={true} className="flex gap-1.5">
          <span className="size-2.5 rounded-full bg-border" />
          <span className="size-2.5 rounded-full bg-border" />
          <span className="size-2.5 rounded-full bg-border" />
        </span>
        <span
          data-slot="app-window-title"
          className="flex min-w-0 items-center justify-center gap-2 text-xs"
        >
          <span className="flex items-center gap-1.5 font-medium [&_svg]:size-3.5">{title}</span>
          {detail && <span className="truncate text-muted-foreground">{detail}</span>}
        </span>
        {/* Balances the three buttons, so the title sits in the middle. */}
        <span aria-hidden={true} className="w-10" />
      </div>
      <div data-slot="app-window-content" className="flex min-h-0 flex-1 flex-col">
        {children}
      </div>
      {footer && (
        <div data-slot="app-window-footer" className="shrink-0 border-t px-3 py-3">
          {footer}
        </div>
      )}
    </figure>
  );
}

export { AppWindow };
