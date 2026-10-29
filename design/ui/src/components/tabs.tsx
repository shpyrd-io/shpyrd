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
        // A switch between what a page shows, in the look of the
        // SegmentedNav's pill (2026-10-28): the bar sunk like the switch's
        // track, the open tab lit by a raised light that slides to it, an
        // orange mark under its word. The tabs share the bar equally.
        pill: "relative grid auto-cols-fr grid-flow-col gap-1 rounded-xl bg-muted shadow-[inset_0_1px_3px_rgb(0_0_0/0.12),inset_0_-1px_0_rgb(255_255_255/0.5)] dark:shadow-[inset_0_1px_3px_rgb(0_0_0/0.5),inset_0_-1px_0_rgb(255_255_255/0.06)]",
      },
      // The pill's sizes: `md` for a panel's switch, `lg` for a slide's.
      size: {
        md: "group-data-horizontal/tabs:data-[variant=pill]:h-11 data-[variant=pill]:p-1",
        lg: "group-data-horizontal/tabs:data-[variant=pill]:h-14 data-[variant=pill]:p-1.5",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "md",
    },
  },
);

function TabsList({
  className,
  variant = "default",
  size = "md",
  children,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.List> &
  VariantProps<typeof tabsListVariants>) {
  // The pill's light: where the open tab is in the bar, measured, so it can
  // slide from one tab to the next. The first place is taken at once.
  const list = React.useRef<HTMLDivElement>(null);
  const [light, setLight] = React.useState<{ x: number; width: number } | null>(
    null,
  );
  const [moving, setMoving] = React.useState(false);
  const pill = variant === "pill";
  React.useLayoutEffect(() => {
    const el = list.current;
    if (!pill || !el) return;
    const place = () => {
      const open = el.querySelector<HTMLElement>(
        '[data-slot="tabs-trigger"][data-state="active"]',
      );
      setLight(open ? { x: open.offsetLeft, width: open.offsetWidth } : null);
    };
    place();
    const sized = new ResizeObserver(place);
    sized.observe(el);
    const changed = new MutationObserver(place);
    changed.observe(el, {
      attributes: true,
      attributeFilter: ["data-state"],
      subtree: true,
    });
    return () => {
      sized.disconnect();
      changed.disconnect();
    };
  }, [pill]);
  React.useEffect(() => {
    if (!light || moving) return;
    const frame = requestAnimationFrame(() => setMoving(true));
    return () => cancelAnimationFrame(frame);
  }, [light, moving]);

  return (
    <TabsPrimitive.List
      ref={list}
      data-slot="tabs-list"
      data-variant={variant}
      data-size={size}
      data-lit={pill && light ? "" : undefined}
      className={cn(tabsListVariants({ variant, size }), className)}
      {...props}
    >
      {pill && light && (
        <span
          aria-hidden={true}
          data-slot="tabs-light"
          className={cn(
            // The orange mark of the docs' list, lying down, under the word.
            "pointer-events-none absolute left-0 rounded-[calc(var(--radius-xl)-4px)] bg-background shadow-[0_1px_2px_rgb(0_0_0/0.12),0_2px_6px_rgb(0_0_0/0.06)] dark:bg-accent dark:shadow-[0_1px_2px_rgb(0_0_0/0.5)]",
            "after:absolute after:bottom-1 after:left-1/2 after:h-[3px] after:w-4 after:-translate-x-1/2 after:rounded-[2px] after:bg-primary",
            size === "lg" ? "inset-y-1.5" : "inset-y-1",
            moving && "transition-[translate,width] duration-slow ease-move",
          )}
          style={{ translate: `${light.x}px 0`, width: light.width }}
        />
      )}
      {children}
    </TabsPrimitive.List>
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
        // The pill's tabs: over its light, which does the lighting; before
        // it has found its place the open tab lights itself.
        "group-data-[variant=pill]/tabs-list:z-10 group-data-[variant=pill]/tabs-list:h-full group-data-[variant=pill]/tabs-list:rounded-[calc(var(--radius-xl)-4px)] group-data-[variant=pill]/tabs-list:text-muted-foreground group-data-[variant=pill]/tabs-list:hover:text-foreground group-data-[variant=pill]/tabs-list:data-active:text-foreground group-data-[variant=pill]/tabs-list:data-active:shadow-none dark:group-data-[variant=pill]/tabs-list:data-active:border-transparent",
        "group-data-[lit]/tabs-list:data-active:bg-transparent dark:group-data-[lit]/tabs-list:data-active:bg-transparent",
        "group-data-[size=md]/tabs-list:group-data-[variant=pill]/tabs-list:gap-2 group-data-[size=md]/tabs-list:group-data-[variant=pill]/tabs-list:px-5 group-data-[size=md]/tabs-list:group-data-[variant=pill]/tabs-list:text-base",
        "group-data-[size=lg]/tabs-list:group-data-[variant=pill]/tabs-list:gap-2.5 group-data-[size=lg]/tabs-list:group-data-[variant=pill]/tabs-list:px-7 group-data-[size=lg]/tabs-list:group-data-[variant=pill]/tabs-list:text-lg group-data-[size=lg]/tabs-list:group-data-[variant=pill]/tabs-list:[&_svg:not([class*='size-'])]:size-5",
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
