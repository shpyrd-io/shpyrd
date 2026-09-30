"use client";

import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "cn";
import { Slot, Tabs as TabsPrimitive } from "radix-ui";
import { CounterLabel } from "./counter-label";

// Tabs go along a line, and only so: a list down the side is a `NavList`,
// which looks the same and goes to another page.
function Tabs({
  className,
  ...props
}: Omit<React.ComponentProps<typeof TabsPrimitive.Root>, "orientation">) {
  return (
    <TabsPrimitive.Root
      data-slot="tabs"
      className={cn(
        "group/tabs flex gap-2 data-horizontal:flex-col",
        className,
      )}
      {...props}
      orientation="horizontal"
    />
  );
}

const tabsListVariants = cva(
  "group/tabs-list inline-flex w-fit items-center justify-center rounded-lg p-[3px] text-muted-foreground group-data-horizontal/tabs:h-8 data-[variant=line]:rounded-none",
  {
    variants: {
      variant: {
        default: "bg-muted",
        // As wide as its place, over a line: the tab that is open is
        // underlined on it.
        line: "gap-2 bg-transparent p-0 group-data-horizontal/tabs:h-10 group-data-horizontal/tabs:w-full group-data-horizontal/tabs:items-start group-data-horizontal/tabs:justify-start group-data-horizontal/tabs:border-b group-data-horizontal/tabs:pt-1",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

function TabsList({
  className,
  variant = "default",
  ...props
}: React.ComponentProps<typeof TabsPrimitive.List> &
  VariantProps<typeof tabsListVariants>) {
  return (
    <TabsPrimitive.List
      data-slot="tabs-list"
      data-variant={variant}
      className={cn(tabsListVariants({ variant }), className)}
      {...props}
    />
  );
}

function TabsTrigger({
  className,
  icon,
  counter,
  children,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Trigger> & {
  // An icon before the text.
  icon?: React.ReactElement;
  // How many there are of what the tab shows, after the text.
  counter?: React.ReactNode;
}) {
  return (
    <TabsPrimitive.Trigger
      data-slot="tabs-trigger"
      className={cn(
        "relative inline-flex h-[calc(100%-1px)] flex-1 items-center justify-center gap-1.5 rounded-md border border-transparent px-1.5 py-0.5 text-sm font-medium whitespace-nowrap text-foreground/60 transition-all hover:text-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-1 focus-visible:outline-ring disabled:pointer-events-none disabled:opacity-50 has-data-[icon=inline-end]:pr-1 has-data-[icon=inline-start]:pl-1 dark:text-muted-foreground dark:hover:text-foreground group-data-[variant=default]/tabs-list:data-active:shadow-sm group-data-[variant=line]/tabs-list:data-active:shadow-none [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
        "group-data-[variant=line]/tabs-list:h-8 group-data-[variant=line]/tabs-list:flex-none group-data-[variant=line]/tabs-list:bg-transparent group-data-[variant=line]/tabs-list:px-2 group-data-[variant=line]/tabs-list:hover:bg-muted group-data-[variant=line]/tabs-list:data-active:bg-transparent dark:group-data-[variant=line]/tabs-list:data-active:border-transparent dark:group-data-[variant=line]/tabs-list:data-active:bg-transparent",
        "data-active:bg-background data-active:text-foreground dark:data-active:border-input dark:data-active:bg-input/30 dark:data-active:text-foreground",
        // The underline of `line`: it is drawn over the line of the list.
        "after:absolute after:rounded-full after:bg-primary after:opacity-0 after:transition-opacity group-data-horizontal/tabs:after:inset-x-0 group-data-horizontal/tabs:after:bottom-[-5px] group-data-horizontal/tabs:after:h-0.5 group-data-[variant=line]/tabs-list:data-active:after:opacity-100",
        className,
      )}
      {...props}
    >
      {icon && <Slot.Root data-icon="inline-start">{icon}</Slot.Root>}
      {children}
      {counter !== undefined && (
        <CounterLabel data-slot="tabs-counter" data-icon="inline-end">
          {counter}
        </CounterLabel>
      )}
    </TabsPrimitive.Trigger>
  );
}

function TabsContent({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Content>) {
  return (
    <TabsPrimitive.Content
      data-slot="tabs-content"
      className={cn("flex-1 text-sm outline-none", className)}
      {...props}
    />
  );
}

export { Tabs, TabsList, TabsTrigger, TabsContent, tabsListVariants };
