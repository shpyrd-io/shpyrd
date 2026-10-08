import * as React from "react";
import { cn } from "cn";

// The customers a product is proud of, going by sideways and slowly, for ever:
// a row of logos in grey that take their colour when the pointer is on one,
// and stop while it is. The ends of the row fade into the page rather than cut.
//
// The list is drawn twice, and the row moves by one copy so that the second
// takes the place of the first without a jump; the copy is hidden from a
// screen reader. Where the reader has asked for less motion the row stands
// still, as a wrapped line of the logos, once.
//
// Its parts are props rather than children it looks for, so a page rendered
// when the application is built draws the same thing as the browser does.
function LogoCarousel({
  className,
  logos,
  label,
  speed = 40,
  ...props
}: Omit<React.ComponentProps<"div">, "children"> & {
  // The logos, each as it is to be drawn: an image, or a mark and a name.
  logos: React.ReactNode[];
  // A line over the row, quiet: "Trusted by teams at".
  label?: React.ReactNode;
  // How long one trip of the row takes, in seconds; more is slower.
  speed?: number;
}) {
  const row = (hidden: boolean) => (
    <ul
      aria-hidden={hidden || undefined}
      className={cn(
        "flex shrink-0 items-center gap-x-14 pr-14",
        // At rest the row is a line of its own, wrapped and centred.
        "motion-reduce:flex-wrap motion-reduce:justify-center motion-reduce:gap-y-6 motion-reduce:pr-0",
        hidden && "motion-reduce:hidden",
      )}
    >
      {logos.map((logo, i) => (
        <li
          key={i}
          className="flex shrink-0 items-center text-muted-foreground opacity-70 grayscale transition-[opacity,filter,color] duration-normal hover:text-foreground hover:opacity-100 hover:grayscale-0"
        >
          {logo}
        </li>
      ))}
    </ul>
  );

  return (
    <div
      data-slot="logo-carousel"
      className={cn("grid justify-items-center gap-6", className)}
      {...props}
    >
      {label && (
        <p data-slot="logo-carousel-label" className="text-center text-sm text-muted-foreground">
          {label}
        </p>
      )}
      <div
        data-slot="logo-carousel-viewport"
        className="group/carousel w-full overflow-hidden [mask-image:linear-gradient(to_right,transparent,#000_12%,#000_88%,transparent)] motion-reduce:[mask-image:none]"
      >
        <div
          data-slot="logo-carousel-track"
          style={{ "--marquee-duration": `${speed}s` } as React.CSSProperties}
          className="flex w-max animate-marquee group-hover/carousel:[animation-play-state:paused] motion-reduce:w-full motion-reduce:animate-none motion-reduce:justify-center"
        >
          {row(false)}
          {row(true)}
        </div>
      </div>
    </div>
  );
}

export { LogoCarousel };
