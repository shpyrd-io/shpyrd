import * as React from "react";
import { cn } from "cn";
import { glass } from "../lib/glass";

// A row of text and a picture, after Primer Brand's River: on one side a
// heading, a few lines and where to go next; on the other a picture, which
// gets the larger part of the row. Rows of these, one under another, with the
// text on alternate sides, are how a page walks through what a product does.
//
// As a card the whole row sits in a large panel of the site's glass, to stand
// apart from the page.
//
// On a narrow screen the row is one column and the text always comes first,
// whichever side it is on when wide.
//
// Its parts are props rather than children it looks for, so a page rendered
// when the application is built draws the same thing as the browser does.
function River({
  className,
  as: Heading = "h2",
  heading,
  description,
  link,
  picture,
  align = "start",
  variant = "plain",
  ...props
}: Omit<React.ComponentProps<"section">, "title" | "children"> & {
  // A page's sections have `h2`; under another section's, `h3`.
  as?: "h2" | "h3";
  heading: React.ReactNode;
  description?: React.ReactNode;
  // Where to go next: one link or button, with an arrow if it likes.
  link?: React.ReactNode;
  // What the text is about: an image, or an app window.
  picture: React.ReactNode;
  // Which side the text is on: `start` is the left, `end` the right.
  align?: "start" | "end";
  variant?: "plain" | "card";
}) {
  const end = align === "end";

  return (
    <section
      data-slot="river"
      data-align={align}
      data-variant={variant}
      className={cn(
        "@container/river",
        variant === "card" && cn(glass, "p-6 @3xl/river:p-12"),
        className,
      )}
      {...props}
    >
      <div
        className={cn(
          "grid items-center gap-10 @3xl/river:gap-16",
          end
            ? "@3xl/river:grid-cols-[1.3fr_1fr]"
            : "@3xl/river:grid-cols-[1fr_1.3fr]",
        )}
      >
        <div
          data-slot="river-text"
          className={cn("grid content-center justify-items-start gap-5", end && "@3xl/river:order-2")}
        >
          <Heading
            data-slot="river-heading"
            className="font-heading text-3xl font-semibold tracking-tight text-balance @3xl/river:text-4xl @5xl/river:text-5xl"
          >
            {heading}
          </Heading>
          {description && (
            <p data-slot="river-description" className="text-lg text-muted-foreground text-pretty">
              {description}
            </p>
          )}
          {link && (
            <div data-slot="river-link" className="mt-1">
              {link}
            </div>
          )}
        </div>

        <div
          data-slot="river-picture"
          className={cn("min-w-0", end && "@3xl/river:order-1")}
        >
          {picture}
        </div>
      </div>
    </section>
  );
}

export { River };
