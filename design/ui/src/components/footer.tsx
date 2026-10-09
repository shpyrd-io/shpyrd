"use client";

import * as React from "react";
import { cn } from "cn";
import { Button } from "./button";
import { BackToTop, type MinimalFooterSocial } from "./minimal-footer";

// The foot of a site's main page, its home: the full one. The minimal footer
// (minimal-footer.tsx) is the small one, for every other page: landings,
// secondary sites.
//
// On the left the brand: its wordmark, one line on what it is, the way in and
// where else to find it. On the right the site's map, in columns under small
// headings. Under both, past a line as fine as the foot's own, the fine print
// and the way back to the top. On a narrow screen everything is stacked, the
// columns two by two.
//
// Like the minimal footer, inside a layout with a foot of its own it is a
// `div`, and its parts are props, not children it looks for.

const widths = {
  full: "max-w-none",
  medium: "max-w-3xl",
  large: "max-w-[67.5rem]",
  xlarge: "max-w-7xl",
} as const;

export type FooterColumn = {
  title: string;
  // Pages, as links: `<a>` or the router's own.
  links: React.ReactElement[];
};

function Footer({
  className,
  as = "footer",
  width = "full",
  logo,
  tagline,
  action,
  columns = [],
  social = [],
  note,
  backToTop = false,
  ...props
}: React.ComponentProps<"footer"> & {
  as?: "footer" | "div";
  width?: keyof typeof widths;
  // The wordmark, usually a link home.
  logo?: React.ReactNode;
  // One line under it, on what the site is.
  tagline?: React.ReactNode;
  // The way in, under the line: a button.
  action?: React.ReactNode;
  columns?: FooterColumn[];
  social?: MinimalFooterSocial[];
  // The fine print at the foot: the copyright, the licence.
  note?: React.ReactNode;
  backToTop?: boolean;
}) {
  const Root = as as React.ElementType;
  return (
    <Root data-slot="footer" className={cn("@container/footer border-t text-sm", className)} {...props}>
      <div className={cn("mx-auto grid w-full gap-12 px-4 pt-14 pb-8 @3xl/footer:px-6", widths[width])}>
        <div className="grid gap-10 @3xl/footer:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] @3xl/footer:gap-16">
          <div data-slot="footer-brand" className="grid content-start justify-items-start gap-4">
            {logo}
            {tagline && <p className="max-w-[36ch] text-muted-foreground">{tagline}</p>}
            {action && <div className="mt-1">{action}</div>}
            {social.length > 0 && (
              <ul data-slot="footer-social" className="-ml-2 flex gap-1">
                {social.map((s) => (
                  <li key={s.href}>
                    <Button
                      variant="ghost"
                      size="icon"
                      asChild
                      aria-label={s.label}
                      className="text-muted-foreground hover:bg-transparent hover:text-primary dark:hover:bg-transparent"
                    >
                      <a href={s.href}>{s.icon}</a>
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </div>

          {columns.length > 0 && (
            <nav
              aria-label="Footer"
              data-slot="footer-columns"
              className="grid grid-cols-2 gap-x-6 gap-y-10 @3xl/footer:grid-cols-[repeat(auto-fit,minmax(9rem,1fr))]"
            >
              {columns.map((col) => (
                <div key={col.title} className="grid content-start gap-4">
                  <p className="font-heading text-sm font-semibold text-foreground">{col.title}</p>
                  <ul className="grid gap-2.5">
                    {col.links.map((link, i) => (
                      <li
                        key={i}
                        className="text-muted-foreground [&>a]:transition-colors [&>a:hover]:text-primary"
                      >
                        {link}
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </nav>
          )}
        </div>

        {(note || backToTop) && (
          <div
            data-slot="footer-bottom"
            className="flex items-center gap-3 border-t border-foreground/8 pt-6 text-muted-foreground dark:border-foreground/20"
          >
            {note && <p className="min-w-0">{note}</p>}
            {backToTop && <BackToTop className="ml-auto" />}
          </div>
        )}
      </div>
    </Root>
  );
}

export { Footer };
