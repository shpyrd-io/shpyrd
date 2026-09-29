import * as React from "react";
import { cn } from "cn";

// What is shown where there is nothing yet: it tells what would be here
// and how to go on. Every part but the title is optional.
function Blankslate({
  className,
  graphic,
  title,
  description,
  action,
  secondaryAction,
  border = false,
  ...props
}: Omit<React.ComponentProps<"div">, "title"> & {
  // An icon or a drawing, over the title.
  graphic?: React.ReactNode;
  title: React.ReactNode;
  description?: React.ReactNode;
  // The main thing to do: a button.
  action?: React.ReactNode;
  // Another way to go on, under the action: a link.
  secondaryAction?: React.ReactNode;
  border?: boolean;
}) {
  return (
    <div
      data-slot="blankslate"
      data-border={border}
      className={cn(
        "flex flex-col items-center gap-4 px-6 py-10 text-center text-sm data-[border=true]:rounded-xl data-[border=true]:ring-1 data-[border=true]:ring-foreground/10",
        className,
      )}
      {...props}
    >
      {graphic && (
        <div
          data-slot="blankslate-graphic"
          className="text-muted-foreground [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-8"
        >
          {graphic}
        </div>
      )}
      <div className="flex max-w-md flex-col gap-1">
        <div
          data-slot="blankslate-title"
          className="font-heading text-base leading-snug font-medium"
        >
          {title}
        </div>
        {description && (
          <div
            data-slot="blankslate-description"
            className="text-balance text-muted-foreground"
          >
            {description}
          </div>
        )}
      </div>
      {(action || secondaryAction) && (
        <div
          data-slot="blankslate-actions"
          className="flex flex-col items-center gap-1"
        >
          {action}
          {secondaryAction}
        </div>
      )}
    </div>
  );
}

export { Blankslate };
