import * as React from "react";
import { cn } from "cn";
import { glass } from "../lib/glass";

// One of the things a section is telling you, standing beside its siblings:
// what it is, a line about it, and where to read more. Three or four across
// is the shape it is drawn for; more than that wants another row.
//
// Its parts are props rather than children it looks for, so a page rendered
// when the application is built draws the same thing as the browser does.
//
// As a card it sits in the site's glass, its icon beside its name, in the
// type of the home page's blocks.
function Pillar({
  className,
  as: Heading = "h3",
  icon,
  heading,
  description,
  link,
  align = "start",
  variant = "plain",
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
  variant?: "plain" | "card";
}) {
  const centred = align === "center";
  const card = variant === "card";

  return (
    <div
      data-slot="pillar"
      data-align={align}
      data-variant={variant}
      className={cn(
        "grid content-start gap-2",
        card && cn(glass, "p-6"),
        centred && "justify-items-center text-center",
        className,
      )}
      {...props}
    >
      {icon && !card && (
        <div
          data-slot="pillar-icon"
          className="text-primary [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-5"
        >
          {icon}
        </div>
      )}

      <Heading
        data-slot="pillar-heading"
        className={cn(
          "font-heading font-semibold",
          card &&
            "flex items-center gap-2 text-base text-foreground [&_svg]:size-[18px] [&_svg]:shrink-0 [&_svg]:text-primary",
        )}
      >
        {card && icon}
        {heading}
      </Heading>

      {description && (
        <p data-slot="pillar-description" className={cn("text-muted-foreground", card && "text-sm")}>
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
