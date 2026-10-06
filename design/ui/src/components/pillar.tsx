import * as React from "react";
import { cn } from "cn";

// One of the things a section is telling you, standing beside its siblings:
// what it is, a line about it, and where to read more. Three or four across
// is the shape it is drawn for; more than that wants another row.
//
// Its parts are props rather than children it looks for, so a page rendered
// when the application is built draws the same thing as the browser does.
function Pillar({
  className,
  as: Heading = "h3",
  icon,
  heading,
  description,
  link,
  align = "start",
  ...props
}: Omit<React.ComponentProps<"div">, "title"> & {
  // A section's own heading is `h2`, so the things under it are `h3`.
  as?: "h3" | "h4";
  // A cue for what this is about, over the heading.
  icon?: React.ReactElement;
  heading: React.ReactNode;
  description?: React.ReactNode;
  // Where to read more: one link, at the foot.
  link?: React.ReactNode;
  align?: "start" | "center";
}) {
  const centred = align === "center";

  return (
    <div
      data-slot="pillar"
      data-align={align}
      className={cn(
        "grid content-start gap-2",
        centred && "justify-items-center text-center",
        className,
      )}
      {...props}
    >
      {icon && (
        <div
          data-slot="pillar-icon"
          className="text-primary [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-5"
        >
          {icon}
        </div>
      )}

      <Heading data-slot="pillar-heading" className="font-heading font-semibold">
        {heading}
      </Heading>

      {description && (
        <p data-slot="pillar-description" className="text-muted-foreground">
          {description}
        </p>
      )}

      {link && (
        <div data-slot="pillar-link" className="mt-1">
          {link}
        </div>
      )}
    </div>
  );
}

export { Pillar };
