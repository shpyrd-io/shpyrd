import * as React from "react";
import { cn } from "cn";
import { ChevronRight } from "lucide-react";
import { Slot } from "radix-ui";

// Where the page is, among the pages over it: each item leads one level
// up, and the last is the page itself.
function Breadcrumbs({
  className,
  children,
  "aria-label": label = "Breadcrumbs",
  ...props
}: React.ComponentProps<"nav">) {
  return (
    <nav
      data-slot="breadcrumbs"
      aria-label={label}
      className={cn("text-sm", className)}
      {...props}
    >
      <ol className="flex flex-wrap items-center gap-x-2 gap-y-1">{children}</ol>
    </nav>
  );
}

// A level. With `href` it is a link; without, it is only a name.
// `selected` marks the page itself.
function BreadcrumbsItem({
  className,
  selected = false,
  asChild = false,
  href,
  ...props
}: React.ComponentProps<"a"> & {
  selected?: boolean;
  asChild?: boolean;
}) {
  const Comp: React.ElementType = asChild
    ? Slot.Root
    : href === undefined
      ? "span"
      : "a";
  return (
    <li
      data-slot="breadcrumbs-item"
      className="group/breadcrumb inline-flex items-center gap-2"
    >
      <ChevronRight aria-hidden="true" className="size-3 text-muted-foreground group-first/breadcrumb:hidden" />
      <Comp
        href={href}
        aria-current={selected ? "page" : undefined}
        data-selected={selected}
        className={cn(
          "rounded-sm text-muted-foreground outline-none focus-visible:ring-3 focus-visible:ring-ring/50 data-[selected=true]:font-medium data-[selected=true]:text-foreground data-[selected=true]:hover:no-underline [a]:text-primary [a]:underline-offset-4 [a]:hover:underline",
          className,
        )}
        {...props}
      />
    </li>
  );
}

export { Breadcrumbs, BreadcrumbsItem };
