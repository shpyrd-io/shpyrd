"use client";

import * as React from "react";
import { ArrowUp } from "lucide-react";
import { cn } from "cn";
import { Button } from "./button";
import { glass } from "../lib/glass";

// How wide what is in the foot may be: the sizes of the site header and of the
// content of a page layout, so the three line up. The foot's line always goes
// from edge to edge.
const widths = {
  full: "max-w-none",
  medium: "max-w-3xl",
  large: "max-w-[67.5rem]",
  xlarge: "max-w-7xl",
} as const;

type Link = React.ReactElement;

export type MinimalFooterSocial = {
  // Said to who cannot see the mark: "shpyrd on GitHub".
  label: string;
  href: string;
  // The network's mark, drawn in `currentColor`.
  icon: React.ReactElement;
};

// The foot of a site, after Primer Brand's MinimalFooter: the few links that
// matter and where else to find you, then the mark and one line of fine print.
// Two rows on a wide screen; on a narrow one everything is stacked.
//
// Inside a layout that has a foot of its own (`PageLayoutFooter`) it is a
// `div`: a `footer` may not hold another.
//
// Its parts are props rather than children it looks for, so a page rendered
// when the application is built draws the same thing as the browser does.
function MinimalFooter({
  className,
  as = "footer",
  width = "full",
  links = [],
  social = [],
  logo,
  note,
  footnotes,
  backToTop = false,
  variant = "line",
  ...props
}: React.ComponentProps<"footer"> & {
  as?: "footer" | "div";
  width?: keyof typeof widths;
  // Pages, as links: `<a>` or the router's own.
  links?: Link[];
  // Where else to find you, as icons at the end of the first row.
  social?: MinimalFooterSocial[];
  // The mark, usually a link home, at the start of the second row.
  logo?: React.ReactNode;
  // One line beside the mark: the copyright, the licence.
  note?: React.ReactNode;
  // Small print over the rows: sources, conditions.
  footnotes?: React.ReactNode;
  // A button at the end of the second row that goes back to the top.
  backToTop?: boolean;
  // `line` sits under a line from edge to edge; `floating` is a panel of
  // frosted glass held off the edges, like the site header's.
  variant?: "line" | "floating";
}) {
  // A `footer`, or a `div` inside a layout that has one; either takes the same props.
  const Root = as as React.ElementType;
  const floating = variant === "floating";
  return (
    <Root
      data-slot="minimal-footer"
      data-variant={variant}
      className={cn(
        "@container/minimal-footer text-sm",
        floating ? "px-3 pb-3 @3xl/minimal-footer:px-6 @3xl/minimal-footer:pb-4" : "border-t",
        className,
      )}
      {...props}
    >
      <div
        className={cn(
          "mx-auto grid w-full gap-6 px-4 py-8 @3xl/minimal-footer:px-6",
          floating && cn(glass, "@3xl/minimal-footer:px-8"),
          widths[width],
        )}
      >
        {footnotes && (
          <div data-slot="minimal-footer-footnotes" className="max-w-prose text-xs text-muted-foreground">
            {footnotes}
          </div>
        )}

        {(links.length > 0 || social.length > 0) && (
          <div className="flex flex-col gap-4 @2xl/minimal-footer:flex-row @2xl/minimal-footer:items-center">
            {links.length > 0 && (
              <nav aria-label="Footer" data-slot="minimal-footer-links">
                <ul className="flex flex-wrap gap-x-5 gap-y-2">
                  {links.map((link, i) => (
                    <li
                      key={i}
                      className="text-muted-foreground [&>a]:underline-offset-4 [&>a]:transition-colors [&>a:hover]:text-primary"
                    >
                      {link}
                    </li>
                  ))}
                </ul>
              </nav>
            )}
            {social.length > 0 && (
              <ul
                data-slot="minimal-footer-social"
                className="flex gap-1 @2xl/minimal-footer:ml-auto"
              >
                {social.map((s) => (
                  <li key={s.href}>
                    <Button variant="ghost" size="icon" asChild aria-label={s.label} className="hover:bg-transparent hover:text-primary dark:hover:bg-transparent">
                      <a href={s.href}>{s.icon}</a>
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}

        <div className="flex items-center gap-3 text-muted-foreground">
          {logo && <div data-slot="minimal-footer-logo" className="shrink-0">{logo}</div>}
          {note && <p data-slot="minimal-footer-note" className="min-w-0">{note}</p>}
          {backToTop && <BackToTop className="ml-auto" />}
        </div>
      </div>
    </Root>
  );
}

// Back to the top of the page, and, for the keyboard, to its main content: a
// reader who pressed it should not have to tab through the header again.
export function BackToTop({ className }: { className?: string }) {
  function onClick(event: React.MouseEvent) {
    const less = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    window.scrollTo({ top: 0, behavior: less ? "auto" : "smooth" });
    // A click from the keyboard has no pointer position.
    if (event.detail === 0) {
      const main = document.querySelector("main") ?? document.body;
      if (!main.hasAttribute("tabindex")) main.setAttribute("tabindex", "-1");
      main.focus({ preventScroll: true });
    }
  }
  return (
    <Button
      variant="ghost"
      size="sm"
      iconEnd={<ArrowUp />}
      className={cn("hover:bg-transparent hover:text-primary dark:hover:bg-transparent", className)}
      onClick={onClick}
    >
      Back to top
    </Button>
  );
}

export { MinimalFooter };
