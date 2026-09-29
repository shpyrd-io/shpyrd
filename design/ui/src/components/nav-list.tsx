"use client";

import * as React from "react";
import { cn } from "cn";
import { Slot } from "radix-ui";
import { ChevronDownIcon } from "lucide-react";
import { Button } from "./button";

// The level of the titles of the groups: one under the heading of the list.
const GroupLevel = React.createContext<"h3" | "h4">("h3");

const row =
  "group/nav-item relative flex min-h-8 w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors outline-none hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50 data-[current=true]:bg-muted data-[current=true]:font-medium data-[current=true]:before:absolute data-[current=true]:before:inset-y-1.5 data-[current=true]:before:-left-2 data-[current=true]:before:w-1 data-[current=true]:before:rounded-full data-[current=true]:before:bg-primary [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4";

function Before({ children }: { children?: React.ReactNode }) {
  if (!children) return null;
  return (
    <span
      data-slot="nav-list-icon"
      className="text-muted-foreground group-data-[current=true]/nav-item:text-foreground"
    >
      {children}
    </span>
  );
}

function After({ children }: { children?: React.ReactNode }) {
  if (!children) return null;
  return (
    <span
      data-slot="nav-list-icon-end"
      className="ml-auto inline-flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground"
    >
      {children}
    </span>
  );
}

// A vertical list of the links of a navigation. It tells which page is
// the current one: give that item `aria-current="page"`.
function NavList({
  className,
  children,
  heading,
  headingLevel: Heading = "h2",
  headingHidden = false,
  ...props
}: React.ComponentProps<"nav"> & {
  // The heading of the list. It gives the navigation its name.
  heading?: React.ReactNode;
  headingLevel?: "h2" | "h3";
  // The heading is there for who cannot see, and not drawn.
  headingHidden?: boolean;
}) {
  const id = React.useId();
  return (
    <GroupLevel value={Heading === "h3" ? "h4" : "h3"}>
      <nav
        data-slot="nav-list"
        aria-labelledby={heading ? id : undefined}
        className={cn("text-sm", className)}
        {...props}
      >
        {heading && (
          <Heading
            id={id}
            data-slot="nav-list-heading"
            className={cn(
              "px-4 pb-2 font-heading text-base leading-snug font-medium",
              headingHidden && "sr-only",
            )}
          >
            {heading}
          </Heading>
        )}
        <ul className="flex flex-col gap-0.5 px-2">{children}</ul>
      </nav>
    </GroupLevel>
  );
}

// Items that belong together, under a title. A line is drawn over every
// group but the first.
function NavListGroup({
  title,
  hideDivider = false,
  className,
  children,
  ...props
}: Omit<React.ComponentProps<"li">, "title"> & {
  title?: React.ReactNode;
  hideDivider?: boolean;
}) {
  const Title = React.use(GroupLevel);
  const id = React.useId();
  return (
    <li
      data-slot="nav-list-group"
      className={cn(
        "mt-2 first:mt-0",
        !hideDivider && "border-t pt-2 first:border-t-0 first:pt-0",
        className,
      )}
      {...props}
    >
      {title && (
        <Title
          id={id}
          data-slot="nav-list-group-title"
          className="px-2 py-1.5 text-xs font-medium text-muted-foreground"
        >
          {title}
        </Title>
      )}
      <ul aria-labelledby={title ? id : undefined} className="flex flex-col gap-0.5">
        {children}
      </ul>
    </li>
  );
}

// A link of the list.
function NavListItem({
  className,
  children,
  icon,
  iconEnd,
  action,
  asChild = false,
  "aria-current": current,
  ...props
}: React.ComponentProps<"a"> & {
  // An icon before the text.
  icon?: React.ReactElement;
  // What comes after the text: an icon, a count, a keybinding hint.
  iconEnd?: React.ReactNode;
  // Something to do with the item, beside it: a `NavListAction`.
  action?: React.ReactNode;
  asChild?: boolean;
}) {
  const Comp = asChild ? Slot.Root : "a";
  return (
    <li data-slot="nav-list-item" className="flex items-center gap-0.5">
      <Comp
        aria-current={current}
        data-current={
          current !== undefined && current !== false && current !== "false"
        }
        className={cn(row, className)}
        {...props}
      >
        <Before>{icon}</Before>
        {asChild ? (
          <Slot.Slottable>{children}</Slot.Slottable>
        ) : (
          <span className="min-w-0 truncate">{children}</span>
        )}
        <After>{iconEnd}</After>
      </Comp>
      {action}
    </li>
  );
}

// An item that opens and closes the items under it. It is a button, not
// a link: it goes nowhere by itself.
function NavListSubNav({
  className,
  children,
  title,
  icon,
  iconEnd,
  defaultOpen = false,
  ...props
}: Omit<React.ComponentProps<"button">, "title"> & {
  title: React.ReactNode;
  icon?: React.ReactElement;
  iconEnd?: React.ReactNode;
  defaultOpen?: boolean;
}) {
  const id = React.useId();
  const [open, setOpen] = React.useState(defaultOpen);
  return (
    <li data-slot="nav-list-sub-nav">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={id}
        className={cn(row, className)}
        onClick={() => setOpen(!open)}
        {...props}
      >
        <Before>{icon}</Before>
        <span className="min-w-0 truncate">{title}</span>
        <After>
          {iconEnd}
          <ChevronDownIcon className="transition-transform group-aria-expanded/nav-item:rotate-180" />
        </After>
      </button>
      <ul
        id={id}
        hidden={!open}
        className="mt-0.5 ml-4 flex flex-col gap-0.5 border-l pl-2"
      >
        {children}
      </ul>
    </li>
  );
}

// Something to do with an item, beside it: pin it, remove it.
function NavListAction({
  label,
  icon,
  className,
  ...props
}: Omit<
  React.ComponentProps<typeof Button>,
  "children" | "icon" | "iconEnd" | "aria-label"
> & {
  // What the action is called: it has no text of its own.
  label: string;
  icon: React.ReactElement;
}) {
  return (
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label={label}
      title={label}
      icon={icon}
      className={cn("text-muted-foreground", className)}
      {...props}
    />
  );
}

// A line between items, where a group with a title is too much.
function NavListDivider({ className, ...props }: React.ComponentProps<"li">) {
  return (
    <li
      role="separator"
      data-slot="nav-list-divider"
      className={cn("my-2 h-px bg-border", className)}
      {...props}
    />
  );
}

export {
  NavList,
  NavListAction,
  NavListDivider,
  NavListGroup,
  NavListItem,
  NavListSubNav,
};
