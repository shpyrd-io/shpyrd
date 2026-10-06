import * as React from "react";
import { cn } from "cn";

const sizes = {
  medium: "text-xl @2xl/section-intro:text-2xl",
  large: "text-2xl @2xl/section-intro:text-3xl",
} as const;

// What opens a section of a page: what it is about, in one line, and at most
// a paragraph saying why. It is not a hero — a page has one of those, at the
// top — and it is not meant to follow another of itself.
//
// Its parts are props rather than children it looks for, so a page rendered
// when the application is built draws the same thing as the browser does.
function SectionIntro({
  className,
  as: Heading = "h2",
  label,
  heading,
  description,
  link,
  align = "start",
  variant = "large",
  ...props
}: Omit<React.ComponentProps<"div">, "title"> & {
  // A page has one `h1`, so a section takes `h2` unless it is nested deeper.
  as?: "h2" | "h3";
  // What comes over the heading: which part of the story this is.
  label?: React.ReactNode;
  heading: React.ReactNode;
  // One paragraph. If it needs two, the section itself should say the rest.
  description?: React.ReactNode;
  // Where to read more: one link, after the description.
  link?: React.ReactNode;
  align?: "start" | "center";
  variant?: keyof typeof sizes;
}) {
  const centred = align === "center";

  return (
    <div
      data-slot="section-intro"
      data-align={align}
      className={cn("@container/section-intro", className)}
    >
      <div
        className={cn("grid gap-3", centred && "justify-items-center text-center")}
        {...props}
      >
        {label && (
          <div data-slot="section-intro-label" className="text-sm text-muted-foreground">
            {label}
          </div>
        )}

        <Heading
          data-slot="section-intro-heading"
          className={cn(
            "max-w-[24ch] font-heading leading-tight font-semibold tracking-tight text-balance",
            sizes[variant],
          )}
        >
          {heading}
        </Heading>

        {description && (
          <p data-slot="section-intro-description" className="max-w-prose text-muted-foreground">
            {description}
          </p>
        )}

        {link && (
          <div data-slot="section-intro-link" className="mt-1">
            {link}
          </div>
        )}
      </div>
    </div>
  );
}

export { SectionIntro };
