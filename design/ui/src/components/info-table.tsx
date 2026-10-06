import * as React from "react";
import { cn } from "cn";

// How many columns there are, for the room the table has: the room is
// that of the table itself, not of the window.
const columnsOf = {
  1: "grid-cols-1",
  2: "grid-cols-1 @sm/info-table:grid-cols-2",
  3: "grid-cols-1 @sm/info-table:grid-cols-2 @xl/info-table:grid-cols-3",
  4: "grid-cols-1 @sm/info-table:grid-cols-2 @xl/info-table:grid-cols-3 @3xl/info-table:grid-cols-4",
} as const;

const spans = {
  2: "@sm/info-table:col-span-2",
  3: "@sm/info-table:col-span-2 @xl/info-table:col-span-3",
  full: "col-span-full",
} as const;

// What is known of something, as names and their values. In a `grid` each
// name is over its value; in `rows` the name is at the start of a line
// and the value at its end.
function InfoTable({
  className,
  children,
  title,
  columns = 3,
  layout = "grid",
  ...props
}: Omit<React.ComponentProps<"div">, "title"> & {
  title?: React.ReactNode;
  // The most columns there may be; there are fewer when the room is little.
  columns?: keyof typeof columnsOf;
  layout?: "grid" | "rows" | "horizontal";
}) {
  const id = React.useId();
  return (
    <div
      data-slot="info-table"
      className={cn("@container/info-table grid gap-4", className)}
      {...props}
    >
      {title && (
        <div
          id={id}
          data-slot="info-table-title"
          className="font-heading text-base leading-snug font-semibold"
        >
          {title}
        </div>
      )}
      <dl
        data-layout={layout}
        aria-labelledby={title ? id : undefined}
        className={cn(
          "grid text-sm",
          layout === "grid" ? cn("gap-x-6 gap-y-4", columnsOf[columns]) : layout === "horizontal" ? "flex flex-wrap gap-x-7 gap-y-5" : "gap-2",
        )}
      >
        {children}
      </dl>
    </div>
  );
}

function InfoTableItem({
  className,
  children,
  label,
  mono = false,
  truncate = false,
  span,
  ...props
}: React.ComponentProps<"div"> & {
  label: React.ReactNode;
  // The value is a name of something, an address, a number of a series.
  mono?: boolean;
  // A value that does not fit is cut, and not taken to the next line.
  truncate?: boolean;
  // The columns the item takes, in a grid.
  span?: keyof typeof spans;
}) {
  const empty = children === undefined || children === null || children === "";
  return (
    <div
      data-slot="info-table-item"
      className={cn(
        "grid min-w-0 content-start gap-0.5 in-data-[layout=rows]:flex in-data-[layout=rows]:items-baseline in-data-[layout=rows]:justify-between in-data-[layout=rows]:gap-3",
        span && spans[span],
        className,
      )}
      {...props}
    >
      <dt className="text-xs text-muted-foreground in-data-[layout=rows]:shrink-0 in-data-[layout=rows]:text-sm">
        {label}
      </dt>
      <dd
        title={typeof children === "string" ? children : undefined}
        className={cn(
          "min-w-0 leading-5 in-data-[layout=rows]:truncate in-data-[layout=rows]:text-right",
          truncate ? "truncate" : "wrap-break-word",
          mono && "font-mono text-xs leading-5",
          empty && "text-muted-foreground",
        )}
      >
        {empty ? "—" : children}
      </dd>
    </div>
  );
}

export { InfoTable, InfoTableItem };
