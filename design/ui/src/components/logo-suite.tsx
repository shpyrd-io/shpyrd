import * as React from "react";
import { cn } from "cn";
import { LogoCarousel } from "./logo-carousel";

// A section that says who is behind a product or what it works with, after
// Primer Brand's LogoSuite: a heading, a line under it if needed, and the
// logos in one bar, spread evenly and in grey so that none outshouts the rest.
// On a narrow screen the bar wraps onto more lines.
//
// With `marquee` the bar is a carousel instead: the same logos going by
// slowly, for a list too long to show at once.
//
// Its parts are props rather than children it looks for, so a page rendered
// when the application is built draws the same thing as the browser does.
function LogoSuite({
  className,
  as: Heading = "h2",
  heading,
  description,
  logos,
  align = "center",
  marquee = false,
  ...props
}: Omit<React.ComponentProps<"section">, "children"> & {
  // The section's own heading is `h2`; under another section's, `h3`.
  as?: "h2" | "h3";
  heading: React.ReactNode;
  description?: React.ReactNode;
  // The logos, each as it is to be drawn: an image, or a mark and a name.
  logos: React.ReactNode[];
  align?: "center" | "start";
  // Let the logos go by sideways instead of standing in a bar.
  marquee?: boolean;
}) {
  const centred = align === "center";

  return (
    <section
      data-slot="logo-suite"
      data-align={align}
      className={cn("grid gap-10", className)}
      {...props}
    >
      <div className={cn("grid max-w-2xl gap-3", centred ? "mx-auto justify-items-center text-center" : "justify-items-start")}>
        <Heading data-slot="logo-suite-heading" className="font-heading text-2xl font-semibold tracking-tight text-balance @2xl:text-3xl">
          {heading}
        </Heading>
        {description && (
          <p data-slot="logo-suite-description" className="text-lg text-muted-foreground text-pretty">
            {description}
          </p>
        )}
      </div>

      {marquee ? (
        <LogoCarousel logos={logos} />
      ) : (
        <ul
          data-slot="logo-suite-logos"
          className={cn(
            "flex flex-wrap items-center gap-x-12 gap-y-8",
            centred ? "justify-center" : "justify-between",
          )}
        >
          {logos.map((logo, i) => (
            <li
              key={i}
              className="flex items-center text-muted-foreground opacity-70 grayscale transition-[opacity,filter,color] duration-normal hover:text-foreground hover:opacity-100 hover:grayscale-0"
            >
              {logo}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

export { LogoSuite };
