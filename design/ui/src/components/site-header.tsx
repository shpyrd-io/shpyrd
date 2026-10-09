"use client";

import * as React from "react";
import { cn } from "cn";
import { MobileNavigation, MobileNavigationTrigger, MobileNavigationContent } from "./mobile-navigation";
import { buttonVariants } from "./button";
import {
  NavigationMenu,
  NavigationMenuContent,
  NavigationMenuItem,
  NavigationMenuLink,
  NavigationMenuList,
  NavigationMenuTrigger,
} from "./navigation-menu";
import { NavList, NavListGroup, NavListItem } from "./nav-list";
import { glass, glassHolding } from "../lib/glass";

// How wide what is in the bar may be: the same sizes as the content of a page
// layout, so the brand lines up with the page's text under it. The bar itself,
// its ground and its line, always goes from edge to edge.
const widths = {
  full: "max-w-none",
  medium: "max-w-3xl",
  // 1080px: wide enough for three columns and a picture beside a hero,
  // narrow enough that a big screen still reads as one page.
  large: "max-w-[67.5rem]",
  xlarge: "max-w-7xl",
} as const;

type Link = React.ReactElement<{ "aria-current"?: React.AriaAttributes["aria-current"] }>;

// A page in a menu: the link, and, for the panel in the bar, a line on what
// it is and an icon. The folded menu shows only the link.
export type SiteHeaderEntry = Link | { link: Link; description?: React.ReactNode; icon?: React.ReactElement };

// Pages that belong together under one name: in the bar a word that opens
// them in a panel, in the folded menu a group with that name over it. When
// there are several kinds of them, `columns` puts each kind in a column with
// its own heading, and each is a group of its own when folded.
export type SiteHeaderColumn = { label: string; links: SiteHeaderEntry[] };
export type SiteHeaderGroup =
  | { label: string; links: SiteHeaderEntry[] }
  | { label: string; columns: SiteHeaderColumn[] };

// Either shape, as columns: a plain group is one column with no heading.
const columnsOf = (group: SiteHeaderGroup): { label?: string; links: SiteHeaderEntry[] }[] =>
  "columns" in group ? group.columns : [{ links: group.links }];

const entryOf = (entry: SiteHeaderEntry) =>
  React.isValidElement(entry) ? { link: entry as Link } : entry;

const isGroup = (item: Link | SiteHeaderGroup): item is SiteHeaderGroup =>
  !React.isValidElement(item);

const current = (link: Link) => link.props["aria-current"];

// In the floating panel, the word under the pointer is lit from below: an
// orange light inside the panel, from its foot up to the word. The panel is
// 3.75rem high and a word 2.5rem, so the light reaches 0.625rem down to the edge.
const glowUnder =
  "relative after:pointer-events-none after:absolute after:inset-x-[-45%] after:-bottom-2.5 after:h-5 after:bg-[radial-gradient(ellipse_50%_100%_at_50%_100%,rgb(255_79_0/0.16),transparent)] after:opacity-0 after:transition-opacity after:duration-300 hover:after:opacity-100 data-[state=open]:after:opacity-100";

// The bar over every page of a site: who it is, the few pages that matter,
// and what to do. It stays at the top as the page scrolls, so the way in is
// never further than the top of the screen.
//
// When its own room is too narrow for the links, they fold into a menu; the
// actions stay, so keep them to what fits beside the brand on a phone. The
// room is the header's, not the window's: `@container/site-header`, which the
// actions may query too.
//
// Inside a layout that has a header of its own (`PageLayoutHeader`), it is a
// `div` there and that header is what stays at the top: an element can only
// stick inside the box that holds it, and a `header` may not hold another.
//
// Its parts are props rather than children it looks for, so a page rendered
// when the application is built draws the same thing as the browser does.
function SiteHeader({
  className,
  as = "header",
  sticky = true,
  width = "full",
  start,
  links = [],
  actions,
  menuLabel = "Menu",
  variant = "bar",
  fold = "narrow",
  ...props
}: React.ComponentProps<"header"> & {
  as?: "header" | "div";
  // It stays at the top as the page scrolls.
  sticky?: boolean;
  // Give it the `width` of the page's content, and the two line up.
  width?: keyof typeof widths;
  // What is at the left: the brand, or where the page is.
  start?: React.ReactNode;
  // The pages, as links: `<a>` or the router's own, or a group of them. The
  // one for the page that is open carries `aria-current="page"`. Each is drawn
  // twice, in the bar and in the menu, so a link must not hold state of its own.
  links?: (Link | SiteHeaderGroup)[];
  // What to do, at the right: icon buttons, then one primary button last.
  actions?: React.ReactNode;
  // The name of the button that opens the menu, for who cannot see its icon.
  menuLabel?: string;
  // `bar` goes from edge to edge with a line under it; `floating` is a panel
  // of frosted glass held off the edges, the page passing under it.
  variant?: "bar" | "floating";
  // When the links fold into the menu: below a narrow bar (42rem), or, for
  // a bar with many words, below a wide one (72rem).
  fold?: "narrow" | "wide";
}) {
  // A `header`, or a `div` inside a layout that has one; either takes the same props.
  const Root = as as React.ElementType;
  const [open, setOpen] = React.useState(false);
  const many = links.length > 0;
  const floating = variant === "floating";
  // Written out whole, so the classes are found when the styles are built.
  const folded = fold === "wide" ? "@6xl/site-header:hidden" : "@2xl/site-header:hidden";
  const unfolded = fold === "wide" ? "hidden @6xl/site-header:flex" : "hidden @2xl/site-header:flex";

  return (
    <MobileNavigation open={open} onOpenChange={setOpen}>
    <Root
      data-slot="site-header"
      data-variant={variant}
      className={cn(
        "@container/site-header",
        floating ? "px-3 pt-3 @3xl/site-header:px-6 @3xl/site-header:pt-4" : "border-b bg-background/85 backdrop-blur-md",
        sticky && "sticky top-0 z-40",
        className,
      )}
      {...props}
    >
      <div
        className={cn(
          "mx-auto flex h-14 w-full items-center gap-3 px-4 @3xl/site-header:px-6",
          floating &&
// The bar holds the menus' panel, which has to blur the page too.
cn(glassHolding, "h-15 @3xl/site-header:pr-3 @3xl/site-header:pl-6"),
          widths[width],
        )}
      >
        {many && <MobileNavigationTrigger label={menuLabel} className={folded} />}

        {start && (
          <div data-slot="site-header-start" className="flex min-w-0 items-center gap-3">
            {start}
          </div>
        )}

        {many && (
          // One panel under the bar serves every menu: moving from one word to
          // the next changes what it holds and eases it to the new size.
          <NavigationMenu
            data-slot="site-header-links"
            aria-label="Site"
            className={cn(unfolded, floating ? "ml-10" : "ml-4")}
            // It opens as soon as the pointer rests on the word, rather than
            // the fifth of a second that makes one click it instead.
            delayDuration={50}
            // Floating, the panel opens as far under the bar as the bar is
            // from the top of the page (12px, and the 10px from these words
            // to the bar's edge), in the bar's own glass: its ground, edge,
            // light and shadow.
            viewportClassName={floating ? cn(glass, "mt-5.5") : undefined}
          >
            {/* The words of a site stand a little apart from one another. */}
            <NavigationMenuList className="gap-5">
              {links.map((item, i) =>
                isGroup(item) ? (
                  <NavigationMenuItem key={i}>
                    <NavigationMenuTrigger
                      data-current={
                        columnsOf(item).some((c) =>
                          c.links.some((e) => current(entryOf(e).link) === "page"),
                        ) || undefined
                      }
                      // The links of a site are nav buttons at the large size,
                      // not the small items of an application's menus; the
                      // menu's grounds go, the colour of the text does it all.
                      className={cn(
                        buttonVariants({ variant: "nav", size: "lg" }),
                        "px-0 hover:bg-transparent data-[state=open]:bg-transparent data-[state=open]:text-primary data-current:text-primary",
                        floating && glowUnder,
                      )}
                    >
                      {item.label}
                    </NavigationMenuTrigger>
                    {/* Room inside the panel: around it, between its columns
                        and between its pages, so it reads as a menu of a site
                        rather than an application's. */}
                    <NavigationMenuContent className="p-4">
                      <div
                        className="grid gap-x-6 gap-y-1"
                        style={{
                          gridTemplateColumns: `repeat(${columnsOf(item).length}, minmax(17rem, 1fr))`,
                        }}
                      >
                        {columnsOf(item).map((column, c) => (
                          <div key={c} className="grid content-start gap-1.5">
                            {column.label && (
                              <p className="px-3 pt-2 pb-2 text-xs font-medium text-muted-foreground">
                                {column.label}
                              </p>
                            )}
                            {column.links.map((entry, j) => {
                              const { link, description, icon } = entryOf(entry);
                              return (
                                <NavigationMenuLink
                                  key={j}
                                  asChild
                                  active={current(link) === "page"}
                                  title={(link.props as { children?: React.ReactNode }).children}
                                  description={description}
                                  icon={icon}
                                  // Under the pointer, the icon turns orange in a white tile.
                                  className="gap-4 p-3 [&>[aria-hidden]]:size-10 [&>[aria-hidden]]:transition-colors [&>[aria-hidden]_svg]:size-5 [&>span:last-child]:gap-1 hover:[&>[aria-hidden]]:bg-background hover:[&>[aria-hidden]]:text-primary focus-visible:[&>[aria-hidden]]:bg-background focus-visible:[&>[aria-hidden]]:text-primary"
                                >
                                  {link}
                                </NavigationMenuLink>
                              );
                            })}
                          </div>
                        ))}
                      </div>
                    </NavigationMenuContent>
                  </NavigationMenuItem>
                ) : (
                  <NavigationMenuItem key={i}>
                    <NavigationMenuLink
                      asChild
                      active={current(item) === "page"}
                      className={cn(
                        buttonVariants({ variant: "nav", size: "lg" }),
                        "p-0 hover:bg-transparent data-[active=true]:bg-transparent",
                        floating && glowUnder,
                      )}
                    >
                      {item}
                    </NavigationMenuLink>
                  </NavigationMenuItem>
                ),
              )}
            </NavigationMenuList>
          </NavigationMenu>
        )}

        {actions && (
          <div data-slot="site-header-actions" className="ml-auto flex shrink-0 items-center gap-1">
            {actions}
          </div>
        )}
      </div>
      {many && <MobileNavigationContent className={cn(folded, floating && "mx-auto mt-2 rounded-2xl bg-background/95 ring-1 ring-foreground/8 backdrop-blur-xl dark:bg-card/95", floating && widths[width])}>
            <NavList aria-label={menuLabel} onClick={() => setOpen(false)}>
              {links.map((item, i) =>
                isGroup(item) ? (
                  columnsOf(item).map((column, c) => (
                    <NavListGroup key={`${i}-${c}`} title={column.label ?? item.label}>
                      {column.links.map((entry, j) => {
                        const { link } = entryOf(entry);
                        return (
                          <NavListItem key={j} asChild aria-current={current(link)}>
                            {link}
                          </NavListItem>
                        );
                      })}
                    </NavListGroup>
                  ))
                ) : (
                  <NavListItem key={i} asChild aria-current={current(item)}>
                    {item}
                  </NavListItem>
                ),
              )}
            </NavList>
      </MobileNavigationContent>}
    </Root>
    </MobileNavigation>
  );
}

export { SiteHeader };
