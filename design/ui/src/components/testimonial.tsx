import * as React from "react";
import { cn } from "cn";
import { glass } from "../lib/glass";
import { Avatar } from "./avatar";

// What a customer said about us, with who said it. As a feature it is the one
// big quote of a section, centred and quiet, with the part that matters lit.
// As a card it is one of a row of short ones, in the site's glass.
//
// The quote and the person are props rather than children it looks for, so a
// page rendered when the application is built draws the same as the browser.

// The part of a quote that matters most. In a feature it takes the ink of the
// page (or the brand orange); in a card it is the part read in full colour.
function TestimonialHighlight({
  className,
  tone = "ink",
  ...props
}: React.ComponentProps<"span"> & {
  tone?: "ink" | "brand";
}) {
  return (
    <span
      data-slot="testimonial-highlight"
      data-tone={tone}
      className={cn(tone === "brand" ? "text-primary" : "text-foreground", className)}
      {...props}
    />
  );
}

function Testimonial({
  className,
  variant = "feature",
  quote,
  name,
  role,
  avatar,
  ...props
}: Omit<React.ComponentProps<"figure">, "children"> & {
  variant?: "feature" | "card";
  // Wrap the part to light in `TestimonialHighlight`.
  quote: React.ReactNode;
  name: string;
  // Their role, their company, or both: "Head of Ops, Northwind".
  role?: React.ReactNode;
  // A picture of them. Without one, none is drawn.
  avatar?: string;
}) {
  const feature = variant === "feature";

  return (
    <figure
      data-slot="testimonial"
      data-variant={variant}
      className={cn(
        "m-0 flex flex-col",
        feature ? "items-center gap-10 text-center" : cn(glass, "h-full justify-between gap-8 p-6"),
        className,
      )}
      {...props}
    >
      <blockquote
        data-slot="testimonial-quote"
        className={cn(
          "m-0 text-balance text-muted-foreground",
          feature ? "max-w-4xl font-heading text-2xl leading-snug sm:text-3xl lg:text-4xl" : "text-base",
        )}
      >
        {quote}
      </blockquote>

      <figcaption
        data-slot="testimonial-person"
        className={cn("flex items-center gap-3", feature && "flex-col gap-3")}
      >
        {avatar && (
          <Avatar
            data-slot="testimonial-avatar"
            src={avatar}
            alt={name}
            size={feature ? 48 : 40}
          />
        )}
        <div className={cn("grid gap-0.5", feature && "justify-items-center")}>
          <span
            data-slot="testimonial-name"
            className={cn(
              feature
                ? "font-heading text-base font-semibold text-foreground"
                : "font-mono text-xs tracking-wide text-foreground uppercase",
            )}
          >
            {name}
          </span>
          {role && (
            <span
              data-slot="testimonial-role"
              className={cn(
                "text-muted-foreground",
                feature ? "text-sm" : "font-mono text-xs tracking-wide uppercase",
              )}
            >
              {role}
            </span>
          )}
        </div>
      </figcaption>
    </figure>
  );
}

// Cards in a row: one across when narrow, two, then four, all as tall as the
// tallest so their people line up along the bottom.
function TestimonialGrid({ className, children, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="testimonial-grid"
      className={cn("@container/testimonials", className)}
      {...props}
    >
      <div className="grid auto-rows-fr gap-4 @xl/testimonials:grid-cols-2 @5xl/testimonials:grid-cols-4">
        {children}
      </div>
    </div>
  );
}

export { Testimonial, TestimonialGrid, TestimonialHighlight };
