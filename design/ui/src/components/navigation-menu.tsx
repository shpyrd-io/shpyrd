"use client";

import * as React from "react";
import { ChevronDown } from "lucide-react";
import { NavigationMenu as Primitive } from "radix-ui";
import { cn } from "cn";

// The menus of a site's navigation: a word in the bar that opens a panel of
// pages under it, each with a line on what it is. One panel serves them all:
// moving from one word to the next changes what it holds and eases it to the
// new size, and the pages slide in from the side the pointer came from. It
// opens to the pointer, a click or the keyboard.
//
// A dropdown menu is for actions on something; this is for going to a page.

function NavigationMenu({
  className,
  children,
  viewport = true,
  ...props
}: React.ComponentProps<typeof Primitive.Root> & {
  // The shared panel under the bar. Without it, each menu opens on its own.
  viewport?: boolean;
}) {
  return (
    <Primitive.Root
      data-slot="navigation-menu"
      data-viewport={viewport}
      className={cn("group/navigation-menu relative flex max-w-max flex-1 items-center", className)}
      {...props}
    >
      {children}
      {viewport && <NavigationMenuViewport />}
    </Primitive.Root>
  );
}

function NavigationMenuList({ className, ...props }: React.ComponentProps<typeof Primitive.List>) {
  return (
    <Primitive.List
      data-slot="navigation-menu-list"
      className={cn("group flex flex-1 list-none items-center gap-1", className)}
      {...props}
    />
  );
}

function NavigationMenuItem({ className, ...props }: React.ComponentProps<typeof Primitive.Item>) {
  return <Primitive.Item data-slot="navigation-menu-item" className={cn("relative", className)} {...props} />;
}

// The word in the bar that opens a menu, with a chevron that turns when it is
// open.
function NavigationMenuTrigger({
  className,
  children,
  ...props
}: React.ComponentProps<typeof Primitive.Trigger>) {
  return (
    <Primitive.Trigger
      data-slot="navigation-menu-trigger"
      className={cn(
        "group inline-flex h-7 items-center gap-1 rounded-md px-2.5 text-[0.8rem] font-medium text-muted-foreground outline-none transition-colors duration-fast ease-move",
        "hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring data-[state=open]:bg-muted data-[state=open]:text-foreground",
        className,
      )}
      {...props}
    >
      {children}
      <ChevronDown
        aria-hidden={true}
        className="size-3.5 transition-transform duration-normal ease-move group-data-[state=open]:rotate-180"
      />
    </Primitive.Trigger>
  );
}

// What a menu holds. The pages slide in from where the pointer came from and
// out to where it goes.
function NavigationMenuContent({ className, ...props }: React.ComponentProps<typeof Primitive.Content>) {
  return (
    <Primitive.Content
      data-slot="navigation-menu-content"
      className={cn(
        "top-0 left-0 w-full p-2 md:absolute md:w-auto",
        "data-[motion^=from-]:animate-in data-[motion^=from-]:fade-in data-[motion^=to-]:animate-out data-[motion^=to-]:fade-out",
        "data-[motion=from-end]:slide-in-from-right-52 data-[motion=from-start]:slide-in-from-left-52 data-[motion=to-end]:slide-out-to-right-52 data-[motion=to-start]:slide-out-to-left-52",
        "duration-normal ease-move",
        className,
      )}
      {...props}
    />
  );
}

// The shared panel: it takes the size of the menu it holds, and eases to the
// next one's.
function NavigationMenuViewport({ className, ...props }: React.ComponentProps<typeof Primitive.Viewport>) {
  return (
    <div className="absolute top-full left-0 isolate z-50 flex justify-center">
      <Primitive.Viewport
        data-slot="navigation-menu-viewport"
        className={cn(
          "relative mt-2 h-(--radix-navigation-menu-viewport-height) w-full origin-top overflow-hidden rounded-xl bg-popover text-popover-foreground shadow-lg ring-1 ring-foreground/10 md:w-(--radix-navigation-menu-viewport-width)",
          "transition-[width,height] duration-normal ease-move",
          "data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95",
          className,
        )}
        {...props}
      />
    </div>
  );
}

// A page in a menu: its name, and a line on what it is, with an icon if it
// has one. The link itself is given (`<a>` or the router's), as `asChild`.
function NavigationMenuLink({
  className,
  title,
  description,
  icon,
  children,
  ...props
}: Omit<React.ComponentProps<typeof Primitive.Link>, "title"> & {
  title?: React.ReactNode;
  description?: React.ReactNode;
  icon?: React.ReactElement;
}) {
  const plain = title === undefined && description === undefined;
  const layout = plain ? null : (
    <>
      {icon && (
        <span
          aria-hidden={true}
          className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-md bg-muted text-muted-foreground ring-1 ring-foreground/5 [&_svg]:size-4"
        >
          {icon}
        </span>
      )}
      <span className="grid gap-0.5">
        <span className="font-medium text-foreground">{title}</span>
        {description && (
          <span className="line-clamp-2 text-xs leading-snug text-muted-foreground">{description}</span>
        )}
      </span>
    </>
  );
  // With `asChild` the given link is the item, and the name and line go in it.
  const content =
    layout && props.asChild && React.isValidElement(children)
      ? React.cloneElement(children as React.ReactElement<{ children?: React.ReactNode }>, {}, layout)
      : (layout ?? children);
  return (
    <Primitive.Link
      data-slot="navigation-menu-link"
      className={cn(
        "flex gap-3 rounded-lg p-2.5 text-sm outline-none transition-colors duration-fast ease-move",
        "hover:bg-muted focus-visible:bg-muted focus-visible:ring-2 focus-visible:ring-ring data-[active=true]:bg-muted/60",
        className,
      )}
      {...props}
    >
      {content}
    </Primitive.Link>
  );
}

export {
  NavigationMenu,
  NavigationMenuContent,
  NavigationMenuItem,
  NavigationMenuLink,
  NavigationMenuList,
  NavigationMenuTrigger,
  NavigationMenuViewport,
};
