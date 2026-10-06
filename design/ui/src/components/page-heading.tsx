import * as React from "react";
import { cn } from "cn";

const sizes = {
  subtitle: "text-xl",
  medium: "text-2xl",
  large: "text-3xl",
} as const;

// The heading of a page: its title, what explains it, and what can be
// done with the page. Only the title is required.
function PageHeading({
  className,
  as: Title = "h1",
  title,
  description,
  notes,
  icon,
  iconEnd,
  actions,
  context,
  variant = "medium",
  border = false,
  ...props
}: Omit<React.ComponentProps<"div">, "title"> & {
  // A page has one `h1`; a heading inside the page takes `h2`.
  as?: "h1" | "h2" | "h3";
  title: React.ReactNode;
  description?: React.ReactNode;
  notes?: React.ReactNode;
  // An icon before the title.
  icon?: React.ReactElement;
  // What comes after the title: a badge, a name in code.
  iconEnd?: React.ReactNode;
  // What can be done with the page: buttons, at the end.
  actions?: React.ReactNode;
  // Where the page is, over the title: breadcrumbs, a link to the parent.
  context?: React.ReactNode;
  variant?: keyof typeof sizes;
  // A line under the heading.
  border?: boolean;
}) {
  return (
    <div
      data-slot="page-heading"
      className={cn("grid gap-2", border && "border-b pb-4", className)}
      {...props}
    >
      {context && (
        <div data-slot="page-heading-context" className="text-sm">
          {context}
        </div>
      )}
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
        <div className="grid min-w-0 gap-1">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <Title
              data-slot="page-heading-title"
              className={cn(
                "flex items-center gap-2 font-heading leading-tight font-medium [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-[0.9em]",
                sizes[variant],
              )}
            >
              {icon}
              {title}
            </Title>
            {(iconEnd || notes) && (
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                {iconEnd}
                {notes && <div data-slot="page-heading-notes" className="text-xs text-muted-foreground">{notes}</div>}
              </div>
            )}
          </div>
          {description && (
            <p
              data-slot="page-heading-description"
              className="max-w-prose text-muted-foreground"
            >
              {description}
            </p>
          )}
        </div>
        {actions && (
          <div
            data-slot="page-heading-actions"
            className="flex shrink-0 flex-wrap items-center gap-2"
          >
            {actions}
          </div>
        )}
      </div>
    </div>
  );
}

export { PageHeading };
