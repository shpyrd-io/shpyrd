"use client";

import * as React from "react";
import { Menu } from "lucide-react";
import { cn } from "cn";
import { AnchoredOverlay } from "./anchored-overlay";
import { Button } from "./button";
import {
  NavigationMenu,
  NavigationMenuContent,
  NavigationMenuItem,
  NavigationMenuLink,
  NavigationMenuList,
  NavigationMenuTrigger,
} from "./navigation-menu";
import { NavList, NavListGroup, NavListItem } from "./nav-list";

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
}) {
  // A `header`, or a `div` inside a layout that has one; either takes the same props.
  const Root = as as React.ElementType;
  const [open, setOpen] = React.useState(false);
  const many = links.length > 0;

  return (
    <Root
      data-slot="site-header"
      className={cn(
        "@container/site-header border-b bg-background/85 backdrop-blur-md",
        sticky && "sticky top-0 z-40",
        className,
      )}
      {...props}
    >
      <div
        className={cn(
          "mx-auto flex h-14 w-full items-center gap-3 px-4 @3xl/site-header:px-6",
          widths[width],
        )}
      >
        {many && (
          <AnchoredOverlay
            open={open}
            onOpenChange={setOpen}
            width="small"
            className="px-0 py-3"
            anchor={
              <Button
                variant="ghost"
                size="icon"
                icon={<Menu />}
                aria-label={menuLabel}
                className="@2xl/site-header:hidden"
              />
            }
          >
            {/* A link chosen in the menu closes it: the page it goes to may be
                drawn in the same document, where nothing else would. */}
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
          </AnchoredOverlay>
        )}

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
            className="ml-4 hidden @2xl/site-header:flex"
          >
            <NavigationMenuList>
              {links.map((item, i) =>
                isGroup(item) ? (
                  <NavigationMenuItem key={i}>
                    <NavigationMenuTrigger
                      data-current={
                        columnsOf(item).some((c) =>
                          c.links.some((e) => current(entryOf(e).link) === "page"),
                        ) || undefined
                      }
                      className="data-current:text-foreground"
                    >
                      {item.label}
                    </NavigationMenuTrigger>
                    <NavigationMenuContent>
                      <div
                        className="grid gap-x-2 gap-y-1"
                        style={{
                          gridTemplateColumns: `repeat(${columnsOf(item).length}, minmax(15rem, 1fr))`,
                        }}
                      >
                        {columnsOf(item).map((column, c) => (
                          <div key={c} className="grid content-start gap-0.5">
                            {column.label && (
                              <p className="px-2.5 pt-1.5 pb-1 text-xs font-medium text-muted-foreground">
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
                      className="h-7 items-center rounded-md px-2.5 py-0 text-[0.8rem] font-medium text-muted-foreground hover:text-foreground aria-[current=page]:bg-muted aria-[current=page]:text-foreground"
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
    </Root>
  );
}

export { SiteHeader };
