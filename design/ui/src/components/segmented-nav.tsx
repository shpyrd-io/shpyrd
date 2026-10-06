"use client";

import * as React from "react";
import { cn } from "cn";

type Link = React.ReactElement<{ "aria-current"?: React.AriaAttributes["aria-current"] }>;

// The pages of a section, side by side on one bar, the open one lit by a pill
// that slides to it: after Primer Brand's tabs, for going between pages. Tabs
// change a panel on the page they are on; this goes to another page, so it is
// a navigation, and its items are links.
//
// For the pill to slide from one page to the next, the bar has to outlive the
// page: put it in the section's layout, not in each page. Until it has
// measured where the open page is (and where the page is built), the open
// link is lit by itself, so the bar is right before any script runs.
//
// When the bar is wider than its room it scrolls sideways, with no scrollbar,
// and keeps the open page in view.
//
// Its parts are props rather than children it looks for, so a page rendered
// when the application is built draws the same thing as the browser does.
function SegmentedNav({
  className,
  links,
  variant = "pill",
  align = "center",
  "aria-label": label,
  ...props
}: Omit<React.ComponentProps<"nav">, "children"> & {
  // The pages, as links: `<a>` or the router's own. The open one carries
  // `aria-current="page"`.
  links: Link[];
  // `pill` lights the open page from behind; `underline` draws a line under it.
  variant?: "pill" | "underline";
  align?: "start" | "center";
  // What these pages are, for who cannot see the bar.
  "aria-label": string;
}) {
  const list = React.useRef<HTMLUListElement>(null);
  const [light, setLight] = React.useState<{ x: number; width: number } | null>(null);
  // The first place the pill takes is taken at once; after that it slides.
  const [moving, setMoving] = React.useState(false);
  const open = links.findIndex((l) => l.props["aria-current"] === "page");

  React.useLayoutEffect(() => {
    const ul = list.current;
    if (!ul) return;
    const place = () => {
      // The link's own box is inside its item; the item is placed in the bar.
      const item = ul.querySelector<HTMLElement>('[aria-current="page"]')?.closest("li");
      if (!item) return setLight(null);
      setLight({ x: item.offsetLeft, width: item.offsetWidth });
      // Keep the open page in view when the bar scrolls.
      const left = item.offsetLeft - (ul.clientWidth - item.offsetWidth) / 2;
      ul.scrollTo({ left, behavior: "auto" });
    };
    place();
    const watch = new ResizeObserver(place);
    watch.observe(ul);
    return () => watch.disconnect();
  }, [open]);

  React.useEffect(() => {
    if (!light || moving) return;
    const frame = requestAnimationFrame(() => setMoving(true));
    return () => cancelAnimationFrame(frame);
  }, [light, moving]);

  const pill = variant === "pill";

  return (
    <nav
      data-slot="segmented-nav"
      data-variant={variant}
      aria-label={label}
      className={cn("flex min-w-0", align === "center" && "justify-center", className)}
      {...props}
    >
      <ul
        ref={list}
        className={cn(
          "relative flex max-w-full overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden",
          // The bar is sunk like the switch's track, the light raised like its knob.
          pill
            ? "gap-1 rounded-xl bg-muted p-1 shadow-[inset_0_1px_3px_rgb(0_0_0/0.12),inset_0_-1px_0_rgb(255_255_255/0.5)] dark:shadow-[inset_0_1px_3px_rgb(0_0_0/0.5),inset_0_-1px_0_rgb(255_255_255/0.06)]"
            : "gap-2 border-b",
        )}
      >
        {light && (
          <span
            aria-hidden={true}
            data-slot="segmented-nav-light"
            className={cn(
              "pointer-events-none absolute left-0",
              pill
                ? "inset-y-1 rounded-control bg-linear-to-b from-white to-[#f2f2f2] shadow-[inset_0_1px_0_rgb(255_255_255),0_1px_2px_rgb(0_0_0/0.18),0_2px_5px_rgb(0_0_0/0.08)] dark:from-[#3a3a3a] dark:to-[#2a2a2a] dark:shadow-[inset_0_1px_0_rgb(255_255_255/0.08),0_1px_2px_rgb(0_0_0/0.5)]"
                : "bottom-0 h-0.5 rounded-full bg-primary",
              moving && "transition-[translate,width] duration-slow ease-move",
            )}
            style={{ translate: `${light.x}px 0`, width: light.width }}
          />
        )}
        {links.map((link, i) => (
          <li key={i} className="relative shrink-0">
            {React.cloneElement(link as React.ReactElement<{ className?: string }>, {
              className: cn(
                "flex h-9 items-center rounded-control px-4 text-sm font-medium whitespace-nowrap text-muted-foreground outline-none transition-colors duration-fast ease-move hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring",
                "aria-[current=page]:text-foreground",
                !pill && "h-10 rounded-none px-3",
                // Before the pill has found its place, the open page lights itself.
                !light && pill && "aria-[current=page]:bg-white aria-[current=page]:shadow-sm dark:aria-[current=page]:bg-[#333]",
                !light && !pill && "aria-[current=page]:shadow-[inset_0_-2px_0_var(--primary)]",
                (link.props as { className?: string }).className,
              ),
            })}
          </li>
        ))}
      </ul>
    </nav>
  );
}

export { SegmentedNav };
