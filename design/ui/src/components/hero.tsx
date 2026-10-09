import * as React from "react";
import { cn } from "cn";

const sizes = {
  medium: "text-3xl @2xl/hero:text-4xl",
  card: "text-3xl @2xl/hero:text-4xl",
  "card-spacious": "text-3xl @2xl/hero:text-4xl",
  large: "text-4xl @2xl/hero:text-5xl",
  // The front page of a site: the heading as big as the room allows.
  xlarge: "text-5xl @2xl/hero:text-6xl @4xl/hero:text-7xl",
  // A page's own title: larger than its sections' titles (48px), smaller
  // than the home page's (72px).
  page: "text-4xl @2xl/hero:text-5xl @4xl/hero:text-6xl",
} as const;

// The banner at the top of a page: what the page is, in as few words as it
// takes, and the one or two things to do about it.
//
// Its parts are props rather than children it looks for, so a page rendered
// when the application is built draws the same thing as the browser does.
// The heading is limited to two lines by the words you give it, not by code:
// a hero that needs three is a hero that says too much.
function Hero({
  className,
  as: Heading = "h1",
  label,
  heading,
  description,
  actions,
  note,
  image,
  align = "start",
  variant = "large",
  ...props
}: Omit<React.ComponentProps<"section">, "title"> & {
  // A page has one `h1`; a hero inside a page that has one takes `h2`.
  as?: "h1" | "h2";
  // What comes over the heading: where this sits, or who it is for.
  label?: React.ReactNode;
  heading: React.ReactNode;
  // Three or four lines at most; it introduces, it does not explain.
  description?: React.ReactNode;
  // What to do about it: one or two buttons.
  actions?: React.ReactNode;
  // The fine print of the actions, under them: what it costs, what it needs.
  note?: React.ReactNode;
  // A picture beside the words. Centred heroes have no room for one.
  image?: React.ReactNode;
  // "auto" is centred while the hero is one column, a phone's width, and
  // starts at the left once there is room for the picture beside the words.
  align?: "start" | "center" | "auto";
  // `card` leaves outer padding to Card/CardContent.
  // `card-spacious` adds 8px / 16px evenly, giving a default Card 24px / 32px insets.
  variant?: keyof typeof sizes;
}) {
  const centred = align === "center";
  const auto = align === "auto";
  const beside = Boolean(image) && !centred;

  return (
    <section
      data-slot="hero"
      data-align={align}
      data-variant={variant}
      className={cn("@container/hero", className)}
      {...props}
    >
      {/* The grid is inside the container, not on it: an element cannot answer
          a query about its own width. */}
      <div
        className={cn(
          "grid items-center gap-x-12 gap-y-10",
          variant === "card-spacious" ? "p-2 @sm/hero:p-4" : variant !== "card" && "py-12 @4xl/hero:py-16",
          beside && "@3xl/hero:grid-cols-2",
        )}
      >
        <div
          className={cn(
            "grid gap-5",
            centred && "justify-items-center text-center",
            auto && "justify-items-center text-center @3xl/hero:justify-items-start @3xl/hero:text-start",
          )}
        >
          {label && (
            <div data-slot="hero-label" className="text-sm text-muted-foreground">
              {label}
            </div>
          )}

          <Heading
            data-slot="hero-heading"
            className={cn(
              "max-w-[20ch] font-heading leading-[1.1] font-semibold tracking-tight text-balance",
              sizes[variant],
            )}
          >
            {heading}
          </Heading>

          {description && (
            <p data-slot="hero-description" className="max-w-prose text-lg text-muted-foreground">
              {description}
            </p>
          )}

          {actions && (
            <div
              data-slot="hero-actions"
              className={cn(
                // Buttons line up by their tops: one may carry a line under it
                // (the Add to button's "or install manually").
                "flex flex-wrap items-start gap-3",
                variant === "card-spacious" && "[&_[data-slot=button]]:max-w-full [&_[data-slot=button]]:whitespace-normal [&_[data-slot=button]]:h-auto [&_[data-slot=button]]:min-h-8 [&_[data-slot=button]]:py-1.5",
                centred && "justify-center",
                auto && "justify-center @3xl/hero:justify-start",
              )}
            >
              {actions}
            </div>
          )}

          {note && (
            <p data-slot="hero-note" className="max-w-prose text-sm text-muted-foreground">
              {note}
            </p>
          )}
        </div>

        {beside && (
          <div data-slot="hero-image" className="min-w-0 [&_img]:w-full">
            {image}
          </div>
        )}
      </div>
    </section>
  );
}

export { Hero };
