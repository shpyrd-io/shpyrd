import * as React from "react";
import { cn } from "cn";

// The areas of a page: header, content, pane, sidebar and footer. Each
// part goes to its place whatever the order it is written in. Under 768
// pixels of room the parts go one under the other; the room is that of
// the layout itself, not of the window.

type Space = "none" | "condensed" | "normal";
type Divider = "none" | "line";
type Hidden = boolean | { narrow?: boolean; regular?: boolean };
type Width = "small" | "medium" | "large";

const containerWidths = {
  full: "max-w-none",
  medium: "max-w-3xl",
  large: "max-w-5xl",
  xlarge: "max-w-7xl",
} as const;

const paddings: Record<Space, string> = {
  none: "p-0",
  condensed: "p-4",
  normal: "p-4 @3xl/page-layout:p-6",
};

const columnGaps: Record<Space, string> = {
  none: "[--column-gap:0px]",
  condensed: "[--column-gap:--spacing(4)]",
  normal: "[--column-gap:--spacing(4)] @3xl/page-layout:[--column-gap:--spacing(6)]",
};

const rowGaps: Record<Space, string> = {
  none: "[--row-gap:0px]",
  condensed: "[--row-gap:--spacing(4)]",
  normal: "[--row-gap:--spacing(4)] @3xl/page-layout:[--row-gap:--spacing(6)]",
};

const widths: Record<Width, string> = {
  small: "@3xl/page-layout:w-60",
  medium: "@3xl/page-layout:w-74",
  large: "@3xl/page-layout:w-80",
};

// A part at the side: over or under the content when narrow, beside it
// when there is room.
const sides = {
  start: {
    place: "mb-(--row-gap) @3xl/page-layout:mr-(--column-gap) @3xl/page-layout:mb-0",
    line: "border-b @3xl/page-layout:border-r @3xl/page-layout:border-b-0",
  },
  end: {
    place: "mt-(--row-gap) @3xl/page-layout:mt-0 @3xl/page-layout:ml-(--column-gap)",
    line: "border-t @3xl/page-layout:border-t-0 @3xl/page-layout:border-l",
  },
} as const;

function hide(hidden: Hidden) {
  if (hidden === true) return "hidden";
  if (hidden === false) return "";
  return cn(
    hidden.narrow && "@max-3xl/page-layout:hidden",
    hidden.regular && "@3xl/page-layout:hidden",
  );
}

function PageLayout({
  className,
  containerWidth = "xlarge",
  padding = "normal",
  columnGap = "normal",
  rowGap = "normal",
  children,
  ...props
}: React.ComponentProps<"div"> & {
  // How wide the page may be.
  containerWidth?: keyof typeof containerWidths;
  // The room around the page.
  padding?: Space;
  columnGap?: Space;
  rowGap?: Space;
}) {
  return (
    <div
      data-slot="page-layout"
      className={cn("@container/page-layout", className)}
      {...props}
    >
      <div
        className={cn(
          "mx-auto flex min-h-[inherit] flex-col @3xl/page-layout:grid @3xl/page-layout:grid-cols-[auto_auto_minmax(0,1fr)_auto_auto] @3xl/page-layout:grid-rows-[auto_1fr_auto] @3xl/page-layout:[grid-template-areas:'sidebar-start_header_header_header_sidebar-end'_'sidebar-start_pane-start_content_pane-end_sidebar-end'_'sidebar-start_footer_footer_footer_sidebar-end']",
          containerWidths[containerWidth],
          paddings[padding],
          columnGaps[columnGap],
          rowGaps[rowGap],
        )}
      >
        {children}
      </div>
    </div>
  );
}

function PageLayoutHeader({
  className,
  padding = "none",
  divider = "none",
  hidden = false,
  ...props
}: Omit<React.ComponentProps<"header">, "hidden"> & {
  padding?: Space;
  divider?: Divider;
  hidden?: Hidden;
}) {
  return (
    <header
      data-slot="page-layout-header"
      className={cn(
        "order-2 mb-(--row-gap) [grid-area:header]",
        divider === "line" && "border-b",
        paddings[padding],
        hide(hidden),
        className,
      )}
      {...props}
    />
  );
}

function PageLayoutContent({
  as = "main",
  className,
  width = "full",
  padding = "none",
  hidden = false,
  ...props
}: Omit<React.ComponentProps<"main">, "hidden"> & {
  // `main` is the content of the page; a layout inside another takes `div`.
  as?: "main" | "div" | "section";
  // How wide the content may be, in the room it has.
  width?: keyof typeof containerWidths;
  padding?: Space;
  hidden?: Hidden;
}) {
  const Comp = as as React.ElementType;
  return (
    <Comp
      data-slot="page-layout-content"
      className={cn(
        "order-4 mx-auto w-full min-w-0 flex-1 [grid-area:content]",
        containerWidths[width],
        paddings[padding],
        hide(hidden),
        className,
      )}
      {...props}
    />
  );
}

// What goes beside the content, between the header and the footer.
function PageLayoutPane({
  className,
  position = "end",
  width = "medium",
  padding = "none",
  divider = "none",
  sticky = false,
  offsetHeader = 0,
  hidden = false,
  style,
  ...props
}: Omit<React.ComponentProps<"div">, "hidden"> & {
  position?: "start" | "end";
  width?: Width;
  padding?: Space;
  divider?: Divider;
  // It stays in sight while the content scrolls.
  sticky?: boolean;
  // The room a header that also stays in sight takes over it.
  offsetHeader?: number | string;
  hidden?: Hidden;
}) {
  return (
    <div
      data-slot="page-layout-pane"
      data-position={position}
      className={cn(
        position === "start"
          ? "order-3 [grid-area:pane-start]"
          : "order-5 [grid-area:pane-end]",
        sides[position].place,
        divider === "line" && sides[position].line,
        widths[width],
        paddings[padding],
        sticky &&
          "@3xl/page-layout:sticky @3xl/page-layout:top-(--offset-header) @3xl/page-layout:max-h-[calc(100svh-var(--offset-header))] @3xl/page-layout:self-start @3xl/page-layout:overflow-y-auto",
        hide(hidden),
        className,
      )}
      style={{
        "--offset-header":
          typeof offsetHeader === "number" ? `${offsetHeader}px` : offsetHeader,
        ...style,
      } as React.CSSProperties}
      {...props}
    />
  );
}

// What goes beside everything, from the top of the page to its bottom.
// For it to reach the bottom, the layout has to be as tall as the window:
// `className="min-h-svh"` on the `PageLayout`.
function PageLayoutSidebar({
  className,
  position = "start",
  width = "medium",
  padding = "none",
  divider = "none",
  sticky = false,
  hidden = false,
  ...props
}: Omit<React.ComponentProps<"aside">, "hidden"> & {
  position?: "start" | "end";
  width?: Width;
  padding?: Space;
  divider?: Divider;
  // It stays in sight, as tall as the window, while the page scrolls.
  sticky?: boolean;
  hidden?: Hidden;
}) {
  return (
    <aside
      data-slot="page-layout-sidebar"
      data-position={position}
      className={cn(
        position === "start"
          ? "order-1 [grid-area:sidebar-start]"
          : "order-7 [grid-area:sidebar-end]",
        sides[position].place,
        divider === "line" && sides[position].line,
        widths[width],
        paddings[padding],
        sticky &&
          "@3xl/page-layout:sticky @3xl/page-layout:top-0 @3xl/page-layout:h-svh @3xl/page-layout:self-start @3xl/page-layout:overflow-y-auto",
        hide(hidden),
        className,
      )}
      {...props}
    />
  );
}

function PageLayoutFooter({
  className,
  padding = "none",
  divider = "none",
  hidden = false,
  ...props
}: Omit<React.ComponentProps<"footer">, "hidden"> & {
  padding?: Space;
  divider?: Divider;
  hidden?: Hidden;
}) {
  return (
    <footer
      data-slot="page-layout-footer"
      className={cn(
        "order-6 mt-(--row-gap) [grid-area:footer]",
        divider === "line" && "border-t",
        paddings[padding],
        hide(hidden),
        className,
      )}
      {...props}
    />
  );
}

export {
  PageLayout,
  PageLayoutContent,
  PageLayoutFooter,
  PageLayoutHeader,
  PageLayoutPane,
  PageLayoutSidebar,
};
